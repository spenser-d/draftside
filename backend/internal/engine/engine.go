// Package engine ranks available players and runs draft-room simulations.
package engine

import (
	"hash/fnv"
	"math"
	"math/rand"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"draftside/internal/domain"
)

type Reason struct {
	Label  string `json:"label"`
	Detail string `json:"detail"`
}

type Simulation struct {
	SampleCount     int     `json:"sampleCount"`
	Confidence      float64 `json:"confidence"`
	FollowingPickNo *int    `json:"followingPickNo"`
}

type Recommendation struct {
	Player        domain.Player
	Score         int
	Strength      string
	PrimaryReason string
	Reasons       []Reason
	Backups       []domain.Player
	GeneratedAt   time.Time
	ModelVersion  string
	Simulation    *Simulation
}

type requirements struct {
	fixed map[domain.Position]int
	flex  int
	super int
}

type candidate struct {
	player       domain.Player
	value        float64
	score        float64
	neutralScore float64
}

func Recommend(state *domain.State, players []domain.Player, rosterPositions []string, samples int) *Recommendation {
	return RecommendForLeague(state, players, rosterPositions, nil, samples)
}

func RecommendForLeague(state *domain.State, players []domain.Player, rosterPositions []string, scoringSettings map[string]float64, samples int) *Recommendation {
	clock := state.Clock()
	if !clock.RecommendationSafe {
		return nil
	}
	scoring := newScoringModel(scoringSettings)
	baseline := baseline(state, players, rosterPositions, scoring)
	if baseline == nil || !clock.UserOnClock {
		return baseline
	}
	if samples < 32 {
		samples = 32
	}
	if samples > 1024 {
		samples = 1024
	}
	return simulate(state, players, rosterPositions, samples, baseline, scoring)
}

func baseline(state *domain.State, players []domain.Player, rosterPositions []string, scoring scoringModel) *Recommendation {
	requirements := parseRequirements(rosterPositions)
	available := make([]domain.Player, 0, len(players))
	for _, player := range players {
		if !player.Active || !domain.IsCorePosition(player.Position) {
			continue
		}
		if _, drafted := state.DraftedPlayerIDs[player.ID]; !drafted {
			available = append(available, player)
		}
	}
	sort.Slice(available, func(i, j int) bool { return playerRankLess(available[i], available[j]) })
	if len(available) == 0 {
		return nil
	}
	rosters := rosterPlayerIDs(state)
	playersByID := playerMap(players, state)
	userRoster := rosters[state.TrackedRosterID]
	clock := state.Clock()
	following := state.NextTrackedPick(clock.Current.PickNo)
	threatEnd := clock.NextUserPick
	if clock.UserOnClock {
		threatEnd = following
	}
	demand := map[domain.Position]int{}
	seen := map[string]bool{}
	if clock.Current != nil && threatEnd != nil {
		start := clock.Current.PickNo
		if clock.UserOnClock {
			start++
		}
		for pickNo := start; pickNo < threatEnd.PickNo; pickNo++ {
			if _, made := state.PicksByNumber[pickNo]; made {
				continue
			}
			plan, err := domain.Planned(state.Config, state.Participants, pickNo, state.OwnerOverrides)
			if err != nil || plan.OwnerRosterID == state.TrackedRosterID {
				continue
			}
			for _, position := range domain.CorePositions {
				key := plan.OwnerRosterID + ":" + string(position)
				if !seen[key] && rosterNeed(rosters[plan.OwnerRosterID], position, playersByID, requirements) >= .65 {
					demand[position]++
					seen[key] = true
				}
			}
		}
	}
	scored := make([]candidate, 0, min(160, len(available)))
	for index, player := range available {
		if index >= 160 {
			break
		}
		neutralValue := playerValue(player, scoringModel{})
		value := playerValue(player, scoring)
		need := rosterNeed(userRoster, player.Position, playersByID, requirements)
		gap := positionRankGap(player, available)
		contextValue := need*22 + float64(demand[player.Position])*4 + math.Min(10, float64(gap)*.6)
		score := value + contextValue
		scored = append(scored, candidate{player: player, value: value, score: score, neutralScore: neutralValue + contextValue})
	}
	sort.Slice(scored, func(i, j int) bool { return candidateLess(scored[i], scored[j]) })
	best := scored[0]
	margin := best.score
	if len(scored) > 1 {
		margin -= scored[1].score
	}
	strength := "low"
	if margin >= 10 {
		strength = "medium"
	}
	if margin >= 24 {
		strength = "high"
	}
	reasons := []Reason{}
	neutralBest := scored[0]
	for _, item := range scored[1:] {
		if item.neutralScore > neutralBest.neutralScore || (item.neutralScore == neutralBest.neutralScore && item.player.SearchRank < neutralBest.player.SearchRank) {
			neutralBest = item
		}
	}
	if neutralBest.player.ID != best.player.ID {
		if reason, ok := scoring.reasonForChoice(best.player.Position, []domain.Position{neutralBest.player.Position}); ok {
			reasons = append(reasons, reason)
		}
	}
	if rosterNeed(userRoster, best.player.Position, playersByID, requirements) >= .65 {
		reasons = append(reasons, Reason{"Roster fit", "Fills an open " + string(best.player.Position) + " starter or flex need."})
	}
	if demand[best.player.Position] > 0 {
		reasons = append(reasons, Reason{"Opponent pressure", "Teams in your next-pick window still need this position."})
	}
	if positionRankGap(best.player, available) >= 8 {
		reasons = append(reasons, Reason{"Tier edge", "The next available player at this position is meaningfully lower in the market order."})
	}
	if len(reasons) == 0 {
		reasons = append(reasons, Reason{"Best baseline value", "Highest roster-adjusted value among currently available players."})
	}
	backups := make([]domain.Player, 0, 2)
	for index := 1; index < min(3, len(scored)); index++ {
		backups = append(backups, scored[index].player)
	}
	return &Recommendation{best.player, int(math.Round(best.score * 100)), strength, reasons[0].Detail, reasons, backups, time.Now().UTC(), "go-baseline-0.3", nil}
}

func simulate(state *domain.State, players []domain.Player, rosterPositions []string, samples int, fallback *Recommendation, scoring scoringModel) *Recommendation {
	requirements := parseRequirements(rosterPositions)
	playersByID := playerMap(players, state)
	market := make([]domain.Player, 0, len(players))
	for _, player := range players {
		if player.Active && domain.IsCorePosition(player.Position) {
			if _, drafted := state.DraftedPlayerIDs[player.ID]; !drafted {
				market = append(market, player)
			}
		}
	}
	sort.Slice(market, func(i, j int) bool { return playerRankLess(market[i], market[j]) })
	candidates := pruneCandidates(state, market, playersByID, requirements, scoring, 10)
	if len(candidates) < 2 {
		return fallback
	}
	clock := state.Clock()
	following := state.NextTrackedPick(clock.Current.PickNo)
	horizonEnd := clock.Current.PickNo + 1
	var followingPickNo *int
	if following != nil {
		horizonEnd = following.PickNo
		value := following.PickNo
		followingPickNo = &value
	}
	initialRosters := rosterPlayerIDs(state)
	seed := hashSeed(state.Config.DraftID + ":" + state.Status + ":" + strconv.Itoa(len(state.Picks)))

	type scenarioResult struct{ outcomes []float64 }
	jobs := make(chan int)
	results := make(chan scenarioResult, samples)
	workers := min(max(1, runtime.GOMAXPROCS(0)), 8)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for scenario := range jobs {
				outcomes := make([]float64, len(candidates))
				for index, candidate := range candidates {
					random := rand.New(rand.NewSource(seed + int64(scenario+1)*1_000_003 + int64(index+1)*97))
					outcomes[index] = simulateBranch(state, candidate.player, market, playersByID, requirements, initialRosters, horizonEnd, scoring, random)
				}
				results <- scenarioResult{outcomes}
			}
		}()
	}
	go func() {
		for scenario := 0; scenario < samples; scenario++ {
			jobs <- scenario
		}
		close(jobs)
		wait.Wait()
		close(results)
	}()

	sums := make([]float64, len(candidates))
	wins := make([]int, len(candidates))
	for result := range results {
		winner := 0
		for index, outcome := range result.outcomes {
			sums[index] += outcome
			if outcome > result.outcomes[winner] {
				winner = index
			}
		}
		wins[winner]++
	}
	type ranked struct {
		candidate   candidate
		mean, share float64
	}
	rankedResults := make([]ranked, len(candidates))
	for index, item := range candidates {
		rankedResults[index] = ranked{item, sums[index] / float64(samples), float64(wins[index]) / float64(samples)}
	}
	sort.Slice(rankedResults, func(i, j int) bool {
		if rankedResults[i].mean != rankedResults[j].mean {
			return rankedResults[i].mean > rankedResults[j].mean
		}
		if rankedResults[i].share != rankedResults[j].share {
			return rankedResults[i].share > rankedResults[j].share
		}
		return playerRankLess(rankedResults[i].candidate.player, rankedResults[j].candidate.player)
	})
	best := rankedResults[0]
	margin := best.mean
	if len(rankedResults) > 1 {
		margin -= rankedResults[1].mean
	}
	strength := "low"
	if best.share >= .52 {
		strength = "medium"
	}
	if best.share >= .7 {
		strength = "high"
	}
	backups := make([]domain.Player, 0, 2)
	for index := 1; index < min(3, len(rankedResults)); index++ {
		backups = append(backups, rankedResults[index].candidate.player)
	}
	reasons := []Reason{
		{"Simulated edge", "Produced the strongest expected roster outcome across the candidate field."},
		{"Opponent response", "Models every opposing selection before your following turn."},
		{"Next-pick value", "Accounts for the best player likely to remain at your next selection."},
	}
	alternativePositions := make([]domain.Position, 0, min(2, len(rankedResults)-1))
	for index := 1; index < min(3, len(rankedResults)); index++ {
		alternativePositions = append(alternativePositions, rankedResults[index].candidate.player.Position)
	}
	if reason, ok := scoring.reasonForChoice(best.candidate.player.Position, alternativePositions); ok {
		reasons = append([]Reason{reason}, reasons...)
	}
	return &Recommendation{
		Player: best.candidate.player, Score: int(math.Round(best.mean * 1000)), Strength: strength,
		PrimaryReason: "Best expected roster outcome across fast draft-room simulations.", Reasons: reasons,
		Backups: backups, GeneratedAt: time.Now().UTC(), ModelVersion: "go-monte-carlo-0.3",
		Simulation: &Simulation{samples, best.share, followingPickNo},
	}
}

func simulateBranch(state *domain.State, selected domain.Player, market []domain.Player, playersByID map[string]domain.Player, requirements requirements, initial map[string][]string, horizonEnd int, scoring scoringModel, random *rand.Rand) float64 {
	rosters := copyRosters(initial)
	available := make(map[string]bool, len(market))
	for _, player := range market {
		available[player.ID] = true
	}
	rosters[state.TrackedRosterID] = append(rosters[state.TrackedRosterID], selected.ID)
	delete(available, selected.ID)
	for pickNo := state.Clock().Current.PickNo + 1; pickNo < horizonEnd; pickNo++ {
		if _, made := state.PicksByNumber[pickNo]; made {
			continue
		}
		plan, err := domain.Planned(state.Config, state.Participants, pickNo, state.OwnerOverrides)
		if err != nil || plan.OwnerRosterID == state.TrackedRosterID {
			continue
		}
		choice := opponentChoice(rosters[plan.OwnerRosterID], available, market, playersByID, requirements, scoring, random)
		if choice.ID == "" {
			continue
		}
		rosters[plan.OwnerRosterID] = append(rosters[plan.OwnerRosterID], choice.ID)
		delete(available, choice.ID)
	}
	if horizonEnd > state.Clock().Current.PickNo+1 {
		if next := bestAvailable(rosters[state.TrackedRosterID], available, market, playersByID, requirements, scoring); next.ID != "" {
			rosters[state.TrackedRosterID] = append(rosters[state.TrackedRosterID], next.ID)
		}
	}
	userUtility := rosterUtility(rosters[state.TrackedRosterID], playersByID, requirements, scoring)
	total, opponents := 0.0, 0
	for _, participant := range state.Participants {
		if participant.RosterID == state.TrackedRosterID {
			continue
		}
		total += rosterUtility(rosters[participant.RosterID], playersByID, requirements, scoring)
		opponents++
	}
	if opponents == 0 {
		return userUtility
	}
	return userUtility - total/float64(opponents)
}

func opponentChoice(roster []string, available map[string]bool, market []domain.Player, playersByID map[string]domain.Player, requirements requirements, scoring scoringModel, random *rand.Rand) domain.Player {
	bestScore := math.Inf(-1)
	var best domain.Player
	seen := 0
	for _, player := range market {
		if !available[player.ID] {
			continue
		}
		seen++
		if seen > 48 {
			break
		}
		score := playerValue(player, scoring)/45 + rosterNeed(roster, player.Position, playersByID, requirements)*1.5 + random.NormFloat64()*.25
		if score > bestScore || (score == bestScore && playerRankLess(player, best)) {
			bestScore, best = score, player
		}
	}
	return best
}

func bestAvailable(roster []string, available map[string]bool, market []domain.Player, playersByID map[string]domain.Player, requirements requirements, scoring scoringModel) domain.Player {
	bestUtility := math.Inf(-1)
	var best domain.Player
	seen := 0
	for _, player := range market {
		if !available[player.ID] {
			continue
		}
		seen++
		if seen > 80 {
			break
		}
		utility := rosterUtility(append(append([]string{}, roster...), player.ID), playersByID, requirements, scoring)
		if utility > bestUtility || (utility == bestUtility && playerRankLess(player, best)) {
			bestUtility, best = utility, player
		}
	}
	return best
}

func pruneCandidates(state *domain.State, market []domain.Player, playersByID map[string]domain.Player, requirements requirements, scoring scoringModel, limit int) []candidate {
	roster := rosterPlayerIDs(state)[state.TrackedRosterID]
	scored := make([]candidate, 0, len(market))
	for _, player := range market {
		neutralValue := playerValue(player, scoringModel{})
		value := playerValue(player, scoring)
		needValue := rosterNeed(roster, player.Position, playersByID, requirements) * 20
		scored = append(scored, candidate{player: player, value: value, score: value + needValue, neutralScore: neutralValue + needValue})
	}
	sort.Slice(scored, func(i, j int) bool { return candidateLess(scored[i], scored[j]) })
	selected := make(map[string]candidate)
	for _, position := range domain.CorePositions {
		for _, item := range scored {
			if item.player.Position == position {
				selected[item.player.ID] = item
				break
			}
		}
	}
	for _, item := range scored {
		if len(selected) >= limit {
			break
		}
		selected[item.player.ID] = item
	}
	result := make([]candidate, 0, len(selected))
	for _, item := range scored {
		if chosen, ok := selected[item.player.ID]; ok {
			result = append(result, chosen)
		}
	}
	return result
}

func parseRequirements(positions []string) requirements {
	result := requirements{fixed: map[domain.Position]int{domain.QB: 0, domain.RB: 0, domain.WR: 0, domain.TE: 0}}
	for _, raw := range positions {
		position := strings.ToUpper(raw)
		switch position {
		case "QB", "RB", "WR", "TE":
			result.fixed[domain.Position(position)]++
		case "FLEX", "W/R/T", "REC_FLEX":
			result.flex++
		case "SUPER_FLEX", "SUPERFLEX":
			result.super++
		}
	}
	if result.fixed[domain.QB]+result.fixed[domain.RB]+result.fixed[domain.WR]+result.fixed[domain.TE] == 0 {
		result.fixed[domain.QB], result.fixed[domain.RB], result.fixed[domain.WR], result.fixed[domain.TE], result.flex = 1, 2, 2, 1, 1
	}
	return result
}

func rosterNeed(roster []string, position domain.Position, players map[string]domain.Player, requirements requirements) float64 {
	counts := map[domain.Position]int{}
	for _, id := range roster {
		counts[players[id].Position]++
	}
	if counts[position] < requirements.fixed[position] {
		return 1
	}
	flexOwned := counts[domain.RB] + counts[domain.WR] + counts[domain.TE]
	flexNeeded := requirements.fixed[domain.RB] + requirements.fixed[domain.WR] + requirements.fixed[domain.TE] + requirements.flex
	if position != domain.QB && flexOwned < flexNeeded {
		return .65
	}
	coreOwned := counts[domain.QB] + flexOwned
	if coreOwned < requirements.fixed[domain.QB]+flexNeeded+requirements.super {
		return .5
	}
	if (position == domain.QB || position == domain.TE) && counts[position] > requirements.fixed[position] {
		return -.45
	}
	if position == domain.RB || position == domain.WR {
		return .12
	}
	return 0
}

func rosterUtility(roster []string, players map[string]domain.Player, requirements requirements, scoring scoringModel) float64 {
	pools := map[domain.Position][]float64{}
	for _, id := range roster {
		player, ok := players[id]
		if ok && domain.IsCorePosition(player.Position) {
			pools[player.Position] = append(pools[player.Position], playerValue(player, scoring))
		}
	}
	utility := 0.0
	regularFlex := []float64{}
	superFlex := []float64{}
	for _, position := range domain.CorePositions {
		values := pools[position]
		sort.Sort(sort.Reverse(sort.Float64Slice(values)))
		fixed := min(requirements.fixed[position], len(values))
		for _, value := range values[:fixed] {
			utility += value
		}
		for _, value := range values[fixed:] {
			weight := 1.0
			if position != domain.QB {
				weight = .97
			}
			weighted := value * weight
			if position == domain.QB {
				superFlex = append(superFlex, weighted)
			} else {
				regularFlex = append(regularFlex, weighted)
			}
		}
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(regularFlex)))
	regularStarters := min(requirements.flex, len(regularFlex))
	for _, value := range regularFlex[:regularStarters] {
		utility += value
	}
	superFlex = append(superFlex, regularFlex[regularStarters:]...)
	sort.Sort(sort.Reverse(sort.Float64Slice(superFlex)))
	superStarters := min(requirements.super, len(superFlex))
	for _, value := range superFlex[:superStarters] {
		utility += value
	}
	weights := []float64{.2, .16, .12, .08, .04}
	for index, value := range superFlex[superStarters:] {
		if index >= len(weights) {
			break
		}
		utility += value * weights[index]
	}
	return utility
}

func playerRankLess(left, right domain.Player) bool {
	if left.SearchRank != right.SearchRank {
		return left.SearchRank < right.SearchRank
	}
	if left.ID != right.ID {
		return left.ID < right.ID
	}
	return left.FullName < right.FullName
}

func candidateLess(left, right candidate) bool {
	if left.score != right.score {
		return left.score > right.score
	}
	return playerRankLess(left.player, right.player)
}

func playerValue(player domain.Player, scoring scoringModel) float64 {
	rank := max(1, player.SearchRank)
	multiplier := 1.0
	status := strings.ToLower(player.Status + " " + player.InjuryStatus)
	if strings.Contains(status, "reserve") || strings.Contains(status, "out") {
		multiplier = .25
	} else if strings.Contains(status, "doubtful") {
		multiplier = .6
	} else if strings.Contains(status, "questionable") {
		multiplier = .88
	}
	return 100 * math.Exp(-float64(rank-1)/160) * multiplier * scoring.factor(player.Position)
}

func positionRankGap(player domain.Player, market []domain.Player) int {
	for _, candidate := range market {
		if candidate.Position == player.Position && candidate.SearchRank > player.SearchRank {
			return candidate.SearchRank - player.SearchRank
		}
	}
	return 24
}

func playerMap(players []domain.Player, state *domain.State) map[string]domain.Player {
	result := make(map[string]domain.Player, len(players)+len(state.Picks))
	for _, player := range players {
		result[player.ID] = player
	}
	for _, pick := range state.Picks {
		if _, exists := result[pick.PlayerID]; !exists {
			result[pick.PlayerID] = domain.Player{ID: pick.PlayerID, FullName: pick.PlayerID, Position: domain.PositionFromMetadata(pick.Metadata), Active: true, SearchRank: 9999}
		}
	}
	return result
}

func rosterPlayerIDs(state *domain.State) map[string][]string {
	result := make(map[string][]string)
	for _, participant := range state.Participants {
		result[participant.RosterID] = []string{}
	}
	for _, pick := range state.Picks {
		result[pick.PickedByRosterID] = append(result[pick.PickedByRosterID], pick.PlayerID)
	}
	return result
}

func copyRosters(input map[string][]string) map[string][]string {
	result := make(map[string][]string, len(input))
	for id, roster := range input {
		result[id] = append([]string{}, roster...)
	}
	return result
}

func hashSeed(value string) int64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(value))
	return int64(hash.Sum64() & math.MaxInt64)
}
