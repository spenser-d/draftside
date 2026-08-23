// Package app coordinates Sleeper data, draft state, recommendations, and persistence.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"draftside/internal/domain"
	"draftside/internal/engine"
	"draftside/internal/live"
	"draftside/internal/sleeper"
	"draftside/internal/store"
)

type Service struct {
	sleeper  *sleeper.Client
	watchers *live.Registry
	store    *store.Store
	samples  int

	mu               sync.Mutex
	players          []domain.Player
	playersExpiresAt time.Time
	references       map[string]referenceCache
	recommendations  map[string]*engine.Recommendation
}

type referenceCache struct {
	users     []sleeper.LeagueUser
	league    sleeper.League
	expiresAt time.Time
}

func NewService(client *sleeper.Client, watchers *live.Registry, persistence *store.Store, samples int) *Service {
	if samples < 32 {
		samples = 192
	}
	return &Service{
		sleeper: client, watchers: watchers, store: persistence, samples: samples,
		references: make(map[string]referenceCache), recommendations: make(map[string]*engine.Recommendation),
	}
}

type DraftCandidate struct {
	DraftID    string  `json:"draftId"`
	LeagueID   *string `json:"leagueId"`
	LeagueName string  `json:"leagueName"`
	Status     string  `json:"status"`
	Season     string  `json:"season"`
	Type       string  `json:"type"`
	TeamCount  int     `json:"teamCount"`
	RoundCount int     `json:"roundCount"`
	StartTime  *int64  `json:"startTime"`
}

type Discovery struct {
	UserID      string           `json:"userId"`
	DisplayName string           `json:"displayName"`
	Candidates  []DraftCandidate `json:"candidates"`
}

type IdentifierKind string

const (
	IdentifierAmbiguous IdentifierKind = "ambiguous"
	IdentifierDraft     IdentifierKind = "draft"
	IdentifierLeague    IdentifierKind = "league"
)

type DraftReference struct {
	ID   string
	Kind IdentifierKind
}

var errInvalidDraftReference = errors.New("invalid Sleeper league or draft reference")

func (service *Service) Discover(ctx context.Context, username, directInput string) (Discovery, error) {
	user, err := service.sleeper.User(ctx, username)
	if err != nil {
		return Discovery{}, err
	}
	if directInput != "" {
		draft, err := service.resolveDraft(ctx, directInput)
		if err != nil {
			return Discovery{}, err
		}
		name := metadataString(draft.Metadata, "name", "Sleeper Draft")
		if draft.LeagueID != "" {
			if league, leagueErr := service.sleeper.League(ctx, draft.LeagueID); leagueErr == nil && league.Name != "" {
				name = league.Name
			}
		}
		return Discovery{user.UserID, user.DisplayName, []DraftCandidate{candidate(draft, name)}}, nil
	}
	state, err := service.sleeper.NFLState(ctx)
	if err != nil {
		return Discovery{}, err
	}
	seasons := uniqueStrings(state.LeagueSeason, state.LeagueCreateSeason, state.Season, state.PreviousSeason)
	uniqueDrafts := make(map[string]sleeper.Draft)
	for _, season := range seasons {
		drafts, draftErr := service.sleeper.UserDrafts(ctx, user.UserID, season)
		if draftErr != nil {
			return Discovery{}, draftErr
		}
		for _, draft := range drafts {
			uniqueDrafts[draft.DraftID] = draft
		}
	}
	leagueNames := make(map[string]string)
	for _, draft := range uniqueDrafts {
		if draft.LeagueID == "" || leagueNames[draft.LeagueID] != "" {
			continue
		}
		league, leagueErr := service.sleeper.League(ctx, draft.LeagueID)
		if leagueErr == nil {
			leagueNames[draft.LeagueID] = league.Name
		}
	}
	candidates := make([]DraftCandidate, 0, len(uniqueDrafts))
	for _, draft := range uniqueDrafts {
		name := metadataString(draft.Metadata, "name", "Mock Draft")
		if draft.LeagueID != "" {
			name = leagueNames[draft.LeagueID]
			if name == "" {
				name = "Sleeper League"
			}
		}
		candidates = append(candidates, candidate(draft, name))
	}
	sort.Slice(candidates, func(i, j int) bool {
		left, right := statusPriority(candidates[i].Status), statusPriority(candidates[j].Status)
		if left != right {
			return left < right
		}
		return pointerInt(candidates[i].StartTime) > pointerInt(candidates[j].StartTime)
	})
	return Discovery{user.UserID, user.DisplayName, candidates}, nil
}

func (service *Service) resolveDraft(ctx context.Context, input string) (sleeper.Draft, error) {
	reference, err := ParseDraftReference(input)
	if err != nil {
		return sleeper.Draft{}, err
	}
	switch reference.Kind {
	case IdentifierDraft:
		return service.sleeper.Draft(ctx, reference.ID)
	case IdentifierLeague:
		return service.draftForLeague(ctx, reference.ID)
	default:
		draft, draftErr := service.sleeper.Draft(ctx, reference.ID)
		if draftErr == nil {
			return draft, nil
		}
		if !IsNotFound(draftErr) {
			return sleeper.Draft{}, draftErr
		}
		return service.draftForLeague(ctx, reference.ID)
	}
}

func (service *Service) draftForLeague(ctx context.Context, leagueID string) (sleeper.Draft, error) {
	league, err := service.sleeper.League(ctx, leagueID)
	if err != nil {
		return sleeper.Draft{}, err
	}
	if league.DraftID != "" {
		draft, draftErr := service.sleeper.Draft(ctx, league.DraftID)
		if draftErr == nil {
			return draft, nil
		}
		if !IsNotFound(draftErr) {
			return sleeper.Draft{}, draftErr
		}
	}
	drafts, err := service.sleeper.LeagueDrafts(ctx, leagueID)
	if err != nil {
		return sleeper.Draft{}, err
	}
	drafts = relevantDrafts(drafts)
	if len(drafts) == 0 {
		return sleeper.Draft{}, &sleeper.APIError{Status: 404, Path: "/league/" + leagueID + "/drafts"}
	}
	return drafts[0], nil
}

type PlayerView struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Position     string  `json:"position"`
	NFLTeam      *string `json:"nflTeam"`
	InjuryStatus *string `json:"injuryStatus"`
}

type ReasonView struct {
	Label  string `json:"label"`
	Detail string `json:"detail"`
}

type RecommendationView struct {
	Status        string             `json:"status"`
	Player        PlayerView         `json:"player"`
	Strength      string             `json:"strength"`
	PrimaryReason string             `json:"primaryReason"`
	Reasons       []ReasonView       `json:"reasons"`
	Backups       []PlayerView       `json:"backups"`
	GeneratedAt   string             `json:"generatedAt"`
	ModelVersion  string             `json:"modelVersion"`
	Simulation    *engine.Simulation `json:"simulation,omitempty"`
}

type LiveDraftView struct {
	Draft               DraftView           `json:"draft"`
	Connection          ConnectionView      `json:"connection"`
	Turn                TurnView            `json:"turn"`
	Recommendation      *RecommendationView `json:"recommendation"`
	RecentPicks         []RecentPickView    `json:"recentPicks"`
	UserRoster          *RosterView         `json:"userRoster"`
	TeamsBeforeNextPick []TeamWindowView    `json:"teamsBeforeNextPick"`
	Persistence         struct {
		Saved bool `json:"saved"`
	} `json:"persistence"`
}

type DraftView struct {
	ID              string `json:"id"`
	LeagueName      string `json:"leagueName"`
	FormatLabel     string `json:"formatLabel"`
	TeamCount       int    `json:"teamCount"`
	Round           int    `json:"round"`
	PickInRound     int    `json:"pickInRound"`
	OverallPick     int    `json:"overallPick"`
	Status          string `json:"status"`
	StateVersion    string `json:"stateVersion"`
	SleeperDraftURL string `json:"sleeperDraftUrl"`
}

type ConnectionView struct {
	State        string `json:"state"`
	LastSyncedAt string `json:"lastSyncedAt"`
}

type TurnView struct {
	State             string  `json:"state"`
	CurrentRosterName string  `json:"currentRosterName"`
	UserRosterName    *string `json:"userRosterName"`
	UserNextPick      *int    `json:"userNextPick"`
	PicksUntilUser    *int    `json:"picksUntilUser"`
}

type RecentPickView struct {
	PickNumber   int        `json:"pickNumber"`
	Label        string     `json:"label"`
	RosterName   string     `json:"rosterName"`
	Player       PlayerView `json:"player"`
	IsUserPick   bool       `json:"isUserPick"`
	IsTradedPick bool       `json:"isTradedPick"`
}

type RosterView struct {
	Name           string         `json:"name"`
	Players        []PlayerView   `json:"players"`
	PositionCounts map[string]int `json:"positionCounts"`
}

type TeamWindowView struct {
	Name        string `json:"name"`
	PickNumbers []int  `json:"pickNumbers"`
}

func (service *Service) DraftView(ctx context.Context, draftID, userID, username string) (LiveDraftView, error) {
	watcher := service.watchers.Get(draftID)
	watcherState, pollErr := watcher.Poll(ctx)
	if watcherState.Snapshot == nil {
		if pollErr == nil {
			pollErr = errors.New("draft watcher returned no board")
		}
		return LiveDraftView{}, pollErr
	}
	snapshot := watcherState.Snapshot
	draft := snapshot.Draft
	format, ok := domain.ParseFormat(draft.Type)
	if !ok {
		return LiveDraftView{}, fmt.Errorf("draft type %q is not supported yet", draft.Type)
	}
	users, league, err := service.referenceData(ctx, draft.LeagueID)
	if err != nil {
		return LiveDraftView{}, err
	}
	players, err := service.playerData(ctx)
	if err != nil {
		return LiveDraftView{}, err
	}
	participants := sleeper.NormalizeParticipants(draft, users)
	state, err := domain.BuildState(domain.BuildInput{
		Config: domain.Config{DraftID: draftID, TeamCount: draft.Settings.Teams, RoundCount: draft.Settings.Rounds, Format: format},
		Status: draft.Status, Season: draft.Season, Participants: participants, TrackedUserID: userID,
		RemotePicks: snapshot.Picks, Trades: snapshot.Trades,
	})
	if err != nil {
		return LiveDraftView{}, err
	}
	clock := state.Clock()
	cacheKey := snapshot.Fingerprint + ":" + userID + ":" + recommendationScoringKey(league.ScoringSettings)
	service.mu.Lock()
	recommendation := service.recommendations[cacheKey]
	service.mu.Unlock()
	if recommendation == nil {
		recommendation = engine.RecommendForLeague(state, players, league.RosterPositions, league.ScoringSettings, service.samples)
		if recommendation != nil {
			service.mu.Lock()
			service.recommendations[cacheKey] = recommendation
			if len(service.recommendations) > 256 {
				service.recommendations = map[string]*engine.Recommendation{cacheKey: recommendation}
			}
			service.mu.Unlock()
		}
	}
	playersByID := make(map[string]domain.Player, len(players))
	for _, player := range players {
		playersByID[player.ID] = player
	}
	resolvePlayer := func(pick domain.Pick) domain.Player {
		if player, exists := playersByID[pick.PlayerID]; exists {
			return player
		}
		return metadataPlayer(pick)
	}
	participantName := func(rosterID string) string {
		for _, participant := range participants {
			if participant.RosterID == rosterID {
				return participant.DisplayName
			}
		}
		return "Unknown team"
	}
	leagueName := league.Name
	if leagueName == "" {
		leagueName = metadataString(draft.Metadata, "name", "Sleeper Draft")
	}
	currentRound, currentInRound, currentPickNo := draft.Settings.Rounds, draft.Settings.Teams, draft.Settings.Teams*draft.Settings.Rounds
	if clock.Current != nil {
		currentRound, currentInRound, currentPickNo = clock.Current.Round, clock.Current.PickInRound, clock.Current.PickNo
	}
	lastSyncedAt := watcherState.LastSuccessAt
	if lastSyncedAt.IsZero() {
		lastSyncedAt = time.Now().UTC()
	}
	view := LiveDraftView{
		Draft: DraftView{
			ID: draftID, LeagueName: leagueName,
			FormatLabel: fmt.Sprintf("%d-team · %s", draft.Settings.Teams, draft.Type), TeamCount: draft.Settings.Teams,
			Round: currentRound, PickInRound: currentInRound, OverallPick: currentPickNo,
			Status: draft.Status, StateVersion: snapshot.Fingerprint,
			SleeperDraftURL: "https://sleeper.com/draft/nfl/" + draftID,
		},
		Connection:          ConnectionView{State: connectionState(watcherState.Health), LastSyncedAt: lastSyncedAt.Format(time.RFC3339Nano)},
		RecentPicks:         []RecentPickView{},
		TeamsBeforeNextPick: []TeamWindowView{},
	}
	turnState := "waiting"
	if draft.Status == "complete" || clock.Current == nil {
		turnState = "complete"
	} else if state.TrackedRosterID == "" {
		turnState = "spectating"
	} else if clock.UserOnClock {
		turnState = "on-clock"
	} else if clock.PicksBeforeUser != nil && *clock.PicksBeforeUser == 1 {
		turnState = "on-deck"
	}
	var userRosterName *string
	if state.TrackedRosterID != "" {
		name := participantName(state.TrackedRosterID)
		userRosterName = &name
	}
	var userNextPick *int
	if clock.NextUserPick != nil {
		value := clock.NextUserPick.PickNo
		userNextPick = &value
	}
	currentRoster := "Unknown team"
	if clock.Current != nil {
		currentRoster = participantName(clock.Current.OwnerRosterID)
	}
	view.Turn = TurnView{
		State: turnState, CurrentRosterName: currentRoster, UserRosterName: userRosterName,
		UserNextPick: userNextPick, PicksUntilUser: clock.PicksBeforeUser,
	}
	if recommendation != nil {
		view.Recommendation = recommendationView(recommendation)
	}

	for index := len(state.Picks) - 1; index >= 0 && len(view.RecentPicks) < 8; index-- {
		pick := state.Picks[index]
		view.RecentPicks = append(view.RecentPicks, RecentPickView{
			PickNumber: pick.PickNo, Label: fmt.Sprintf("%d.%02d", pick.Round, pick.PickInRound),
			RosterName: participantName(pick.PickedByRosterID), Player: playerView(resolvePlayer(pick)),
			IsUserPick: pick.PickedByRosterID == state.TrackedRosterID, IsTradedPick: pick.OriginalRosterID != pick.PickedByRosterID,
		})
	}
	if state.TrackedRosterID != "" {
		roster := &RosterView{Name: participantName(state.TrackedRosterID), Players: []PlayerView{}, PositionCounts: map[string]int{"QB": 0, "RB": 0, "WR": 0, "TE": 0}}
		for _, pick := range state.Picks {
			if pick.PickedByRosterID != state.TrackedRosterID {
				continue
			}
			player := resolvePlayer(pick)
			roster.Players = append(roster.Players, playerView(player))
			roster.PositionCounts[string(player.Position)]++
		}
		view.UserRoster = roster
	}
	windowEnd := clock.NextUserPick
	if clock.UserOnClock && clock.NextUserPick != nil {
		windowEnd = state.NextTrackedPick(clock.NextUserPick.PickNo)
	}
	if clock.Current != nil && windowEnd != nil {
		windows := make(map[string][]int)
		start := clock.Current.PickNo
		if clock.UserOnClock {
			start++
		}
		for pickNo := start; pickNo < windowEnd.PickNo; pickNo++ {
			if _, made := state.PicksByNumber[pickNo]; made {
				continue
			}
			plan, planErr := domain.Planned(state.Config, participants, pickNo, state.OwnerOverrides)
			if planErr == nil && plan.OwnerRosterID != state.TrackedRosterID {
				windows[plan.OwnerRosterID] = append(windows[plan.OwnerRosterID], pickNo)
			}
		}
		for rosterID, picks := range windows {
			view.TeamsBeforeNextPick = append(view.TeamsBeforeNextPick, TeamWindowView{Name: participantName(rosterID), PickNumbers: picks})
		}
		sort.Slice(view.TeamsBeforeNextPick, func(i, j int) bool {
			return view.TeamsBeforeNextPick[i].PickNumbers[0] < view.TeamsBeforeNextPick[j].PickNumbers[0]
		})
	}
	if recommendation != nil {
		err = service.store.SaveDraft(ctx, store.SavedDraft{
			DraftID: draftID, UserID: userID, Username: username, LeagueID: draft.LeagueID, LeagueName: leagueName,
			Status: draft.Status, BoardHash: snapshot.Fingerprint, State: view, Picks: state.Picks,
			Recommendation: view.Recommendation, PlayerID: recommendation.Player.ID, Score: recommendation.Score,
		})
		view.Persistence.Saved = err == nil
	}
	return view, nil
}

func (service *Service) playerData(ctx context.Context) ([]domain.Player, error) {
	service.mu.Lock()
	if len(service.players) > 0 && service.playersExpiresAt.After(time.Now()) {
		players := service.players
		service.mu.Unlock()
		return players, nil
	}
	service.mu.Unlock()
	var cached []domain.Player
	if ok, err := service.store.ReadCache(ctx, "sleeper:players:nfl", &cached); err == nil && ok {
		service.mu.Lock()
		service.players, service.playersExpiresAt = cached, time.Now().Add(30*time.Minute)
		service.mu.Unlock()
		return cached, nil
	}
	positions := []string{"QB", "RB", "WR", "TE"}
	raw := make([]map[string]sleeper.RawPlayer, len(positions))
	errorsByPosition := make([]error, len(positions))
	var wait sync.WaitGroup
	for index, position := range positions {
		wait.Add(1)
		go func() {
			defer wait.Done()
			raw[index], errorsByPosition[index] = service.sleeper.Players(ctx, position)
		}()
	}
	wait.Wait()
	for _, fetchErr := range errorsByPosition {
		if fetchErr != nil {
			return nil, fetchErr
		}
	}
	players := sleeper.NormalizePlayers(raw...)
	_ = service.store.WriteCache(ctx, "sleeper:players:nfl", players, 24*time.Hour)
	service.mu.Lock()
	service.players, service.playersExpiresAt = players, time.Now().Add(30*time.Minute)
	service.mu.Unlock()
	return players, nil
}

func (service *Service) referenceData(ctx context.Context, leagueID string) ([]sleeper.LeagueUser, sleeper.League, error) {
	if leagueID == "" {
		return []sleeper.LeagueUser{}, sleeper.League{}, nil
	}
	service.mu.Lock()
	if cached, ok := service.references[leagueID]; ok && cached.expiresAt.After(time.Now()) {
		service.mu.Unlock()
		return cached.users, cached.league, nil
	}
	service.mu.Unlock()
	var users []sleeper.LeagueUser
	var league sleeper.League
	var usersErr, leagueErr error
	var wait sync.WaitGroup
	wait.Add(2)
	go func() { defer wait.Done(); users, usersErr = service.sleeper.LeagueUsers(ctx, leagueID) }()
	go func() { defer wait.Done(); league, leagueErr = service.sleeper.League(ctx, leagueID) }()
	wait.Wait()
	if usersErr != nil {
		return nil, sleeper.League{}, usersErr
	}
	if leagueErr != nil {
		return nil, sleeper.League{}, leagueErr
	}
	service.mu.Lock()
	service.references[leagueID] = referenceCache{users, league, time.Now().Add(5 * time.Minute)}
	service.mu.Unlock()
	return users, league, nil
}

func recommendationView(recommendation *engine.Recommendation) *RecommendationView {
	reasons := make([]ReasonView, len(recommendation.Reasons))
	for index, reason := range recommendation.Reasons {
		reasons[index] = ReasonView{Label: reason.Label, Detail: reason.Detail}
	}
	backups := make([]PlayerView, len(recommendation.Backups))
	for index, player := range recommendation.Backups {
		backups[index] = playerView(player)
	}
	status := "fallback"
	if recommendation.Simulation != nil {
		status = "ready"
	}
	return &RecommendationView{
		Status: status, Player: playerView(recommendation.Player), Strength: recommendation.Strength,
		PrimaryReason: recommendation.PrimaryReason, Reasons: reasons, Backups: backups,
		GeneratedAt: recommendation.GeneratedAt.Format(time.RFC3339Nano), ModelVersion: recommendation.ModelVersion,
		Simulation: recommendation.Simulation,
	}
}

func playerView(player domain.Player) PlayerView {
	return PlayerView{
		ID: player.ID, Name: player.FullName, Position: string(player.Position),
		NFLTeam: stringPointer(player.Team), InjuryStatus: stringPointer(player.InjuryStatus),
	}
}

func metadataPlayer(pick domain.Pick) domain.Player {
	first, _ := pick.Metadata["first_name"].(string)
	last, _ := pick.Metadata["last_name"].(string)
	name := strings.TrimSpace(first + " " + last)
	if name == "" {
		name = "Player " + pick.PlayerID
	}
	team, _ := pick.Metadata["team"].(string)
	return domain.Player{ID: pick.PlayerID, FirstName: first, LastName: last, FullName: name, Position: domain.PositionFromMetadata(pick.Metadata), Team: team, Active: true, SearchRank: 9999}
}

func candidate(draft sleeper.Draft, name string) DraftCandidate {
	var leagueID *string
	if draft.LeagueID != "" {
		value := draft.LeagueID
		leagueID = &value
	}
	return DraftCandidate{
		DraftID: draft.DraftID, LeagueID: leagueID, LeagueName: name, Status: draft.Status,
		Season: draft.Season, Type: draft.Type, TeamCount: draft.Settings.Teams,
		RoundCount: draft.Settings.Rounds, StartTime: draft.StartTime,
	}
}

func statusPriority(status string) int {
	switch status {
	case "drafting":
		return 0
	case "pre_draft":
		return 1
	case "complete":
		return 3
	default:
		return 2
	}
}

func connectionState(health live.Health) string {
	switch health {
	case live.Live, live.Complete:
		return "live"
	case live.Offline:
		return "offline"
	default:
		return "delayed"
	}
}

func metadataString(metadata map[string]any, key, fallback string) string {
	if value, ok := metadata[key].(string); ok && value != "" {
		return value
	}
	return fallback
}

func uniqueStrings(values ...string) []string {
	seen := make(map[string]bool)
	result := []string{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func pointerInt(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func relevantDrafts(drafts []sleeper.Draft) []sleeper.Draft {
	result := make([]sleeper.Draft, 0, len(drafts))
	for _, draft := range drafts {
		if draft.DraftID != "" {
			result = append(result, draft)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := statusPriority(result[i].Status), statusPriority(result[j].Status)
		if left != right {
			return left < right
		}
		if result[i].Season != result[j].Season {
			return result[i].Season > result[j].Season
		}
		return pointerInt(result[i].StartTime) > pointerInt(result[j].StartTime)
	})
	return result
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func ParseDraftReference(input string) (DraftReference, error) {
	input = strings.TrimSpace(input)
	if isSleeperID(input) {
		return DraftReference{ID: input, Kind: IdentifierAmbiguous}, nil
	}
	urlInput := input
	lower := strings.ToLower(urlInput)
	if strings.HasPrefix(lower, "sleeper.com/") || strings.HasPrefix(lower, "www.sleeper.com/") || strings.HasPrefix(lower, "api.sleeper.app/") {
		urlInput = "https://" + urlInput
	}
	parsed, err := url.ParseRequestURI(urlInput)
	if err != nil || parsed.Host == "" {
		return DraftReference{}, errInvalidDraftReference
	}
	host := strings.ToLower(parsed.Hostname())
	segments := strings.FieldsFunc(parsed.EscapedPath(), func(r rune) bool { return r == '/' })
	for index, segment := range segments {
		decoded, decodeErr := url.PathUnescape(segment)
		if decodeErr != nil {
			return DraftReference{}, errInvalidDraftReference
		}
		segments[index] = strings.ToLower(decoded)
	}
	switch host {
	case "sleeper.com", "www.sleeper.com":
		if len(segments) >= 2 && segments[0] == "leagues" && isSleeperID(segments[1]) {
			return DraftReference{ID: segments[1], Kind: IdentifierLeague}, nil
		}
		if len(segments) >= 3 && segments[0] == "draft" && segments[1] == "nfl" && isSleeperID(segments[2]) {
			return DraftReference{ID: segments[2], Kind: IdentifierDraft}, nil
		}
	case "api.sleeper.app":
		if len(segments) >= 3 && segments[0] == "v1" && segments[1] == "league" && isSleeperID(segments[2]) {
			return DraftReference{ID: segments[2], Kind: IdentifierLeague}, nil
		}
		if len(segments) >= 3 && segments[0] == "v1" && segments[1] == "draft" && isSleeperID(segments[2]) {
			return DraftReference{ID: segments[2], Kind: IdentifierDraft}, nil
		}
	}
	return DraftReference{}, errInvalidDraftReference
}

func isSleeperID(value string) bool {
	if len(value) < 5 {
		return false
	}
	_, err := strconv.ParseUint(value, 10, 64)
	return err == nil
}

func recommendationScoringKey(settings map[string]float64) string {
	parts := make([]string, 0, 3)
	for _, key := range []string{"rec", "pass_td", "bonus_rec_te"} {
		if value, ok := settings[key]; ok {
			parts = append(parts, key+"="+strconv.FormatFloat(value, 'f', -1, 64))
		}
	}
	return strings.Join(parts, ",")
}

func ExtractDraftID(input string) string {
	reference, err := ParseDraftReference(input)
	if err != nil {
		return ""
	}
	return reference.ID
}

func IsNotFound(err error) bool {
	var apiError *sleeper.APIError
	return errors.As(err, &apiError) && apiError.Status == 404
}
