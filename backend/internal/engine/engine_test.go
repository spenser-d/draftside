package engine

// Recommendation behavior is deterministic for a fixed board and sample count.

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"draftside/internal/domain"
)

func TestRosterUtilityKeepsQuarterbacksOutOfRegularFlex(t *testing.T) {
	players := map[string]domain.Player{
		"qb-1": {ID: "qb-1", Position: domain.QB, SearchRank: 1},
		"qb-2": {ID: "qb-2", Position: domain.QB, SearchRank: 2},
		"rb":   {ID: "rb", Position: domain.RB, SearchRank: 40},
	}
	roster := []string{"qb-1", "qb-2", "rb"}
	scoring := scoringModel{}

	regular := rosterUtility(roster, players, parseRequirements([]string{"QB", "FLEX"}), scoring)
	wantRegular := playerValue(players["qb-1"], scoring) + playerValue(players["rb"], scoring)*.97 + playerValue(players["qb-2"], scoring)*.2
	if math.Abs(regular-wantRegular) > 1e-9 {
		t.Fatalf("regular FLEX utility = %f, want %f; the backup QB must remain on the bench", regular, wantRegular)
	}

	super := rosterUtility(roster, players, parseRequirements([]string{"QB", "FLEX", "SUPER_FLEX"}), scoring)
	wantSuper := playerValue(players["qb-1"], scoring) + playerValue(players["rb"], scoring)*.97 + playerValue(players["qb-2"], scoring)
	if math.Abs(super-wantSuper) > 1e-9 {
		t.Fatalf("superflex utility = %f, want %f; the backup QB must be eligible for SUPER_FLEX", super, wantSuper)
	}
}

func TestSimulationIsStableWhenTiedRanksArriveInDifferentOrders(t *testing.T) {
	participants := make([]domain.Participant, 4)
	for index := range participants {
		participants[index] = domain.Participant{Slot: index + 1, RosterID: fmt.Sprint(index + 1), UserIDs: []string{fmt.Sprintf("user-%d", index+1)}}
	}
	state, err := domain.BuildState(domain.BuildInput{
		Config: domain.Config{DraftID: "tied-ranks", TeamCount: 4, RoundCount: 4, Format: domain.Snake},
		Status: "drafting", Season: "2026", Participants: participants, TrackedUserID: "user-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	positions := []domain.Position{domain.QB, domain.RB, domain.WR, domain.TE}
	players := make([]domain.Player, 40)
	for index := range players {
		players[index] = domain.Player{ID: fmt.Sprintf("p%02d", index+1), FullName: fmt.Sprintf("Player %02d", index+1), Position: positions[index%len(positions)], Active: true, SearchRank: index/2 + 1}
	}
	reversed := append([]domain.Player(nil), players...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}

	first := Recommend(state, players, []string{"QB", "RB", "WR", "TE", "FLEX"}, 64)
	second := Recommend(state, reversed, []string{"QB", "RB", "WR", "TE", "FLEX"}, 64)
	if first == nil || second == nil || first.Player.ID != second.Player.ID {
		t.Fatalf("tied ranks changed recommendation: %+v vs %+v", first, second)
	}
	if len(first.Backups) != len(second.Backups) {
		t.Fatalf("tied ranks changed backup count: %+v vs %+v", first.Backups, second.Backups)
	}
	for index := range first.Backups {
		if first.Backups[index].ID != second.Backups[index].ID {
			t.Fatalf("tied ranks changed backup %d: %q vs %q", index, first.Backups[index].ID, second.Backups[index].ID)
		}
	}
}

func TestRecommendationUsesSimulationOnClock(t *testing.T) {
	participants := make([]domain.Participant, 4)
	for index := range participants {
		participants[index] = domain.Participant{Slot: index + 1, RosterID: fmt.Sprint(index + 1), UserIDs: []string{fmt.Sprintf("user-%d", index+1)}}
	}
	state, err := domain.BuildState(domain.BuildInput{
		Config: domain.Config{DraftID: "draft", TeamCount: 4, RoundCount: 4, Format: domain.Snake},
		Status: "drafting", Season: "2026", Participants: participants, TrackedUserID: "user-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	positions := []domain.Position{domain.RB, domain.WR, domain.QB, domain.TE}
	players := make([]domain.Player, 40)
	for index := range players {
		players[index] = domain.Player{ID: fmt.Sprintf("p%d", index+1), FullName: fmt.Sprintf("Player %d", index+1), Position: positions[index%len(positions)], Active: true, SearchRank: index + 1}
	}
	first := Recommend(state, players, []string{"QB", "RB", "RB", "WR", "WR", "TE", "FLEX"}, 64)
	second := Recommend(state, players, []string{"QB", "RB", "RB", "WR", "WR", "TE", "FLEX"}, 64)
	if first == nil || first.Simulation == nil || first.Player.ID != second.Player.ID {
		t.Fatalf("unexpected recommendations: %+v %+v", first, second)
	}
}

func TestFullPPRCanBreakCloseCrossPositionTie(t *testing.T) {
	state := waitingState(t)
	players := []domain.Player{
		{ID: "qb", FullName: "Quarterback", Position: domain.QB, Active: true, SearchRank: 10},
		{ID: "wr", FullName: "Receiver", Position: domain.WR, Active: true, SearchRank: 13},
	}
	rosterPositions := []string{"QB", "WR"}

	neutral := RecommendForLeague(state, players, rosterPositions, nil, 64)
	fullPPR := RecommendForLeague(state, players, rosterPositions, map[string]float64{"rec": 1}, 64)
	if neutral == nil || fullPPR == nil {
		t.Fatal("expected baseline recommendations")
	}
	if neutral.Player.ID != "qb" {
		t.Fatalf("neutral recommendation = %q, want qb", neutral.Player.ID)
	}
	if fullPPR.Player.ID != "wr" {
		t.Fatalf("full-PPR recommendation = %q, want wr", fullPPR.Player.ID)
	}
	assertScoringReason(t, fullPPR, "Full-PPR")
}

func TestSixPointPassingTouchdownsCanBreakCloseCrossPositionTie(t *testing.T) {
	state := waitingState(t)
	players := []domain.Player{
		{ID: "wr", FullName: "Receiver", Position: domain.WR, Active: true, SearchRank: 10},
		{ID: "qb", FullName: "Quarterback", Position: domain.QB, Active: true, SearchRank: 13},
	}
	rosterPositions := []string{"QB", "WR"}

	neutral := RecommendForLeague(state, players, rosterPositions, nil, 64)
	sixPoint := RecommendForLeague(state, players, rosterPositions, map[string]float64{"pass_td": 6}, 64)
	if neutral == nil || sixPoint == nil {
		t.Fatal("expected baseline recommendations")
	}
	if neutral.Player.ID != "wr" {
		t.Fatalf("neutral recommendation = %q, want wr", neutral.Player.ID)
	}
	if sixPoint.Player.ID != "qb" {
		t.Fatalf("six-point passing-TD recommendation = %q, want qb", sixPoint.Player.ID)
	}
	assertScoringReason(t, sixPoint, "6-point passing touchdowns")
}

func TestAbsentScoringSettingsPreserveNeutralRecommendation(t *testing.T) {
	state := waitingState(t)
	players := []domain.Player{
		{ID: "rb", FullName: "Running Back", Position: domain.RB, Active: true, SearchRank: 10},
		{ID: "wr", FullName: "Receiver", Position: domain.WR, Active: true, SearchRank: 11},
	}
	rosterPositions := []string{"RB", "WR"}

	legacy := Recommend(state, players, rosterPositions, 64)
	empty := RecommendForLeague(state, players, rosterPositions, map[string]float64{}, 64)
	if legacy == nil || empty == nil {
		t.Fatal("expected baseline recommendations")
	}
	if legacy.Player.ID != empty.Player.ID || legacy.Score != empty.Score {
		t.Fatalf("empty settings changed neutral result: %+v vs %+v", legacy, empty)
	}
	for _, reason := range empty.Reasons {
		if reason.Label == "League scoring fit" {
			t.Fatalf("empty settings produced a league-scoring reason: %+v", reason)
		}
	}
}

func waitingState(t *testing.T) *domain.State {
	t.Helper()
	participants := []domain.Participant{
		{Slot: 1, RosterID: "1", UserIDs: []string{"other"}},
		{Slot: 2, RosterID: "2", UserIDs: []string{"tracked"}},
	}
	state, err := domain.BuildState(domain.BuildInput{
		Config: domain.Config{DraftID: "draft", TeamCount: 2, RoundCount: 2, Format: domain.Snake},
		Status: "drafting", Season: "2026", Participants: participants, TrackedUserID: "tracked",
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func assertScoringReason(t *testing.T, recommendation *Recommendation, detailFragment string) {
	t.Helper()
	for _, reason := range recommendation.Reasons {
		if reason.Label == "League scoring fit" && strings.Contains(reason.Detail, detailFragment) {
			return
		}
	}
	t.Fatalf("recommendation does not explain league scoring: %+v", recommendation.Reasons)
}
