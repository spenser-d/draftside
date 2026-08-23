// Package sleeper provides a typed client and response normalization.
package sleeper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"draftside/internal/domain"
)

type Client struct {
	baseURL string
	http    *http.Client
}

type APIError struct {
	Status int
	Path   string
}

func (err *APIError) Error() string {
	return fmt.Sprintf("Sleeper returned %d for %s", err.Status, err.Path)
}

func NewClient(baseURL string) *Client {
	return NewClientWithHTTPClient(baseURL, nil)
}

func NewClientWithHTTPClient(baseURL string, httpClient *http.Client) *Client {
	if baseURL == "" {
		baseURL = "https://api.sleeper.app/v1"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 8 * time.Second}
	}
	return &Client{strings.TrimRight(baseURL, "/"), httpClient}
}

func (client *Client) get(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("accept", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &APIError{response.StatusCode, path}
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	return decoder.Decode(target)
}

type User struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

type NFLState struct {
	Season             string `json:"season"`
	PreviousSeason     string `json:"previous_season"`
	LeagueSeason       string `json:"league_season"`
	LeagueCreateSeason string `json:"league_create_season"`
}

type Draft struct {
	DraftID        string                     `json:"draft_id"`
	LeagueID       string                     `json:"league_id"`
	Season         string                     `json:"season"`
	Type           string                     `json:"type"`
	Status         string                     `json:"status"`
	StartTime      *int64                     `json:"start_time"`
	Settings       DraftSettings              `json:"settings"`
	Metadata       map[string]any             `json:"metadata"`
	DraftOrder     map[string]int             `json:"draft_order"`
	SlotToRosterID map[string]json.RawMessage `json:"slot_to_roster_id"`
}

type DraftSettings struct {
	Teams     int `json:"teams"`
	Rounds    int `json:"rounds"`
	PickTimer int `json:"pick_timer"`
}

type Pick struct {
	DraftID  string          `json:"draft_id"`
	PlayerID string          `json:"player_id"`
	PickNo   int             `json:"pick_no"`
	PickedBy string          `json:"picked_by"`
	RosterID json.RawMessage `json:"roster_id"`
	Keeper   bool            `json:"is_keeper"`
	Metadata map[string]any  `json:"metadata"`
}

type TradedPick struct {
	Season   string          `json:"season"`
	Round    int             `json:"round"`
	RosterID json.RawMessage `json:"roster_id"`
	OwnerID  json.RawMessage `json:"owner_id"`
}

type League struct {
	LeagueID        string             `json:"league_id"`
	DraftID         string             `json:"draft_id"`
	Name            string             `json:"name"`
	Season          string             `json:"season"`
	RosterPositions []string           `json:"roster_positions"`
	ScoringSettings map[string]float64 `json:"scoring_settings"`
}

type LeagueUser struct {
	UserID      string            `json:"user_id"`
	DisplayName string            `json:"display_name"`
	Username    string            `json:"username"`
	Metadata    map[string]string `json:"metadata"`
}

type RawPlayer struct {
	PlayerID         string   `json:"player_id"`
	FirstName        string   `json:"first_name"`
	LastName         string   `json:"last_name"`
	FullName         string   `json:"full_name"`
	Position         string   `json:"position"`
	FantasyPositions []string `json:"fantasy_positions"`
	Team             string   `json:"team"`
	Active           *bool    `json:"active"`
	Status           string   `json:"status"`
	InjuryStatus     string   `json:"injury_status"`
	SearchRank       int      `json:"search_rank"`
}

func (client *Client) User(ctx context.Context, input string) (User, error) {
	var result User
	err := client.get(ctx, "/user/"+url.PathEscape(input), &result)
	return result, err
}

func (client *Client) NFLState(ctx context.Context) (NFLState, error) {
	var result NFLState
	err := client.get(ctx, "/state/nfl", &result)
	return result, err
}

func (client *Client) UserDrafts(ctx context.Context, userID, season string) ([]Draft, error) {
	var result []Draft
	err := client.get(ctx, "/user/"+url.PathEscape(userID)+"/drafts/nfl/"+url.PathEscape(season), &result)
	return result, err
}

func (client *Client) Draft(ctx context.Context, draftID string) (Draft, error) {
	var result Draft
	err := client.get(ctx, "/draft/"+url.PathEscape(draftID), &result)
	return result, err
}

func (client *Client) Picks(ctx context.Context, draftID string) ([]Pick, error) {
	var result []Pick
	err := client.get(ctx, "/draft/"+url.PathEscape(draftID)+"/picks", &result)
	return result, err
}

func (client *Client) TradedPicks(ctx context.Context, draftID string) ([]TradedPick, error) {
	var result []TradedPick
	err := client.get(ctx, "/draft/"+url.PathEscape(draftID)+"/traded_picks", &result)
	return result, err
}

func (client *Client) League(ctx context.Context, leagueID string) (League, error) {
	var result League
	err := client.get(ctx, "/league/"+url.PathEscape(leagueID), &result)
	return result, err
}

func (client *Client) LeagueDrafts(ctx context.Context, leagueID string) ([]Draft, error) {
	var result []Draft
	err := client.get(ctx, "/league/"+url.PathEscape(leagueID)+"/drafts", &result)
	return result, err
}

func (client *Client) LeagueUsers(ctx context.Context, leagueID string) ([]LeagueUser, error) {
	var result []LeagueUser
	err := client.get(ctx, "/league/"+url.PathEscape(leagueID)+"/users", &result)
	return result, err
}

func (client *Client) Players(ctx context.Context, position string) (map[string]RawPlayer, error) {
	var result map[string]RawPlayer
	err := client.get(ctx, "/players/nfl?position="+url.QueryEscape(position)+"&active=true", &result)
	return result, err
}

func RawID(value json.RawMessage) string {
	if len(value) == 0 || string(value) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(value, &text) == nil {
		return text
	}
	var number json.Number
	if json.Unmarshal(value, &number) == nil {
		return number.String()
	}
	return strings.Trim(string(value), `"`)
}

func NormalizePicks(raw []Pick) []domain.RemotePick {
	result := make([]domain.RemotePick, 0, len(raw))
	for _, pick := range raw {
		result = append(result, domain.RemotePick{
			PickNo: pick.PickNo, PlayerID: pick.PlayerID, RosterID: RawID(pick.RosterID),
			PickedByUserID: pick.PickedBy, Keeper: pick.Keeper, Metadata: pick.Metadata,
		})
	}
	return result
}

func NormalizeTrades(raw []TradedPick) []domain.Trade {
	result := make([]domain.Trade, 0, len(raw))
	for _, trade := range raw {
		result = append(result, domain.Trade{
			Season: trade.Season, Round: trade.Round, OriginalRosterID: RawID(trade.RosterID), OwnerRosterID: RawID(trade.OwnerID),
		})
	}
	return result
}

func NormalizeParticipants(draft Draft, users []LeagueUser) []domain.Participant {
	names := make(map[string]string)
	for _, user := range users {
		name := user.Metadata["team_name"]
		if name == "" {
			name = user.DisplayName
		}
		if name == "" {
			name = user.Username
		}
		names[user.UserID] = name
	}
	usersBySlot := make(map[int][]string)
	for userID, slot := range draft.DraftOrder {
		usersBySlot[slot] = append(usersBySlot[slot], userID)
	}
	result := make([]domain.Participant, 0, draft.Settings.Teams)
	for slot := 1; slot <= draft.Settings.Teams; slot++ {
		userIDs := usersBySlot[slot]
		display := "Team " + strconv.Itoa(slot)
		for _, userID := range userIDs {
			if names[userID] != "" {
				display = names[userID]
				break
			}
		}
		rosterID := "slot:" + strconv.Itoa(slot)
		if raw, ok := draft.SlotToRosterID[strconv.Itoa(slot)]; ok {
			if resolved := RawID(raw); resolved != "" {
				rosterID = resolved
			}
		}
		result = append(result, domain.Participant{Slot: slot, RosterID: rosterID, UserIDs: userIDs, DisplayName: display})
	}
	return result
}

func NormalizePlayers(rawMaps ...map[string]RawPlayer) []domain.Player {
	merged := make(map[string]RawPlayer)
	for _, raw := range rawMaps {
		for id, player := range raw {
			merged[id] = player
		}
	}
	result := make([]domain.Player, 0, len(merged))
	for id, player := range merged {
		position := strings.ToUpper(player.Position)
		if position == "" && len(player.FantasyPositions) > 0 {
			position = strings.ToUpper(player.FantasyPositions[0])
		}
		pos := domain.Position(position)
		if !domain.IsCorePosition(pos) && pos != domain.K && pos != domain.DEF {
			continue
		}
		fullName := player.FullName
		if fullName == "" {
			fullName = strings.TrimSpace(player.FirstName + " " + player.LastName)
		}
		if fullName == "" {
			continue
		}
		playerID := player.PlayerID
		if playerID == "" {
			playerID = id
		}
		active := player.Active == nil || *player.Active
		rank := player.SearchRank
		if rank <= 0 {
			rank = 9999
		}
		result = append(result, domain.Player{
			ID: playerID, FirstName: player.FirstName, LastName: player.LastName, FullName: fullName,
			Position: pos, Team: player.Team, Active: active, Status: player.Status,
			InjuryStatus: player.InjuryStatus, SearchRank: rank,
		})
	}
	return result
}
