package domain

// Draft-order behavior is covered here independently of Sleeper and HTTP.

import "testing"

func participants() []Participant {
	result := make([]Participant, 4)
	for i := range result {
		result[i] = Participant{Slot: i + 1, RosterID: string(rune('1' + i)), UserIDs: []string{"user-" + string(rune('1'+i))}}
	}
	return result
}

func TestDraftOrder(t *testing.T) {
	config := Config{"draft", 4, 4, Snake}
	want := []int{1, 2, 3, 4, 4, 3, 2, 1, 1, 2, 3, 4}
	for index, slot := range want {
		pick, err := Planned(config, participants(), index+1, nil)
		if err != nil || pick.OriginSlot != slot {
			t.Fatalf("pick %d: got slot %d, want %d (%v)", index+1, pick.OriginSlot, slot, err)
		}
	}
}

func TestGapAndTradedClock(t *testing.T) {
	state, err := BuildState(BuildInput{
		Config: Config{"draft", 4, 4, Snake}, Status: "drafting", Season: "2026",
		Participants: participants(), TrackedUserID: "user-2",
		RemotePicks: []RemotePick{{PickNo: 2, PlayerID: "keeper", RosterID: "2", Keeper: true}},
		Trades:      []Trade{{Season: "2026", Round: 1, OriginalRosterID: "1", OwnerRosterID: "2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !state.HasBoardGap() || state.Clock().RecommendationSafe {
		t.Fatal("keeper hole must make the board unsafe")
	}
	if !state.Clock().UserOnClock {
		t.Fatal("tracked roster should own the traded first pick")
	}
}
