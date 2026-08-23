// Package domain models draft order and live board state.
package domain

import (
	"fmt"
	"sort"
	"strings"
)

type DraftFormat string

const (
	Snake              DraftFormat = "snake"
	Linear             DraftFormat = "linear"
	ThirdRoundReversal DraftFormat = "third_round_reversal"
)

type Position string

const (
	QB  Position = "QB"
	RB  Position = "RB"
	WR  Position = "WR"
	TE  Position = "TE"
	K   Position = "K"
	DEF Position = "DEF"
)

var CorePositions = []Position{QB, RB, WR, TE}

type Config struct {
	DraftID    string
	TeamCount  int
	RoundCount int
	Format     DraftFormat
}

type Participant struct {
	Slot        int
	RosterID    string
	UserIDs     []string
	DisplayName string
}

type RemotePick struct {
	PickNo         int            `json:"pickNo"`
	PlayerID       string         `json:"playerId"`
	RosterID       string         `json:"rosterId"`
	PickedByUserID string         `json:"pickedByUserId"`
	Keeper         bool           `json:"isKeeper"`
	Metadata       map[string]any `json:"metadata"`
}

type Trade struct {
	Season           string `json:"season"`
	Round            int    `json:"round"`
	OriginalRosterID string `json:"originalRosterId"`
	OwnerRosterID    string `json:"ownerRosterId"`
}

type PlannedPick struct {
	PickNo           int
	Round            int
	PickInRound      int
	OriginSlot       int
	OriginalRosterID string
	OwnerRosterID    string
}

type Pick struct {
	PlannedPick
	PlayerID         string
	PickedByRosterID string
	PickedByUserID   string
	Keeper           bool
	Metadata         map[string]any
}

type Player struct {
	ID           string
	FirstName    string
	LastName     string
	FullName     string
	Position     Position
	Team         string
	Active       bool
	Status       string
	InjuryStatus string
	SearchRank   int
}

type State struct {
	Config           Config
	Status           string
	Season           string
	Participants     []Participant
	TrackedUserID    string
	TrackedRosterID  string
	Picks            []Pick
	PicksByNumber    map[int]Pick
	DraftedPlayerIDs map[string]struct{}
	OwnerOverrides   map[int]string
}

type BuildInput struct {
	Config        Config
	Status        string
	Season        string
	Participants  []Participant
	TrackedUserID string
	RemotePicks   []RemotePick
	Trades        []Trade
}

type Clock struct {
	Current            *PlannedPick
	NextUserPick       *PlannedPick
	PicksBeforeUser    *int
	UserOnClock        bool
	RecommendationSafe bool
}

func ParseFormat(raw string) (DraftFormat, bool) {
	format := DraftFormat(raw)
	switch format {
	case Snake, Linear, ThirdRoundReversal:
		return format, true
	default:
		return "", false
	}
}

func direction(format DraftFormat, round int) bool {
	if format == Linear {
		return true
	}
	if format == Snake {
		return round%2 == 1
	}
	if round == 1 {
		return true
	}
	if round == 2 || round == 3 {
		return false
	}
	return round%2 == 0
}

func Planned(config Config, participants []Participant, pickNo int, overrides map[int]string) (PlannedPick, error) {
	total := config.TeamCount * config.RoundCount
	if pickNo < 1 || pickNo > total {
		return PlannedPick{}, fmt.Errorf("pick %d is outside the draft board", pickNo)
	}
	round := (pickNo-1)/config.TeamCount + 1
	pickInRound := (pickNo-1)%config.TeamCount + 1
	originSlot := pickInRound
	if !direction(config.Format, round) {
		originSlot = config.TeamCount - pickInRound + 1
	}
	var participant *Participant
	for i := range participants {
		if participants[i].Slot == originSlot {
			participant = &participants[i]
			break
		}
	}
	if participant == nil {
		return PlannedPick{}, fmt.Errorf("draft slot %d has no participant", originSlot)
	}
	owner := participant.RosterID
	if override := overrides[pickNo]; override != "" {
		owner = override
	}
	return PlannedPick{pickNo, round, pickInRound, originSlot, participant.RosterID, owner}, nil
}

func PickNoForOriginalRoster(config Config, participants []Participant, round int, rosterID string) (int, error) {
	var slot int
	for _, participant := range participants {
		if participant.RosterID == rosterID {
			slot = participant.Slot
			break
		}
	}
	if slot == 0 {
		return 0, fmt.Errorf("unknown original roster %s", rosterID)
	}
	position := slot
	if !direction(config.Format, round) {
		position = config.TeamCount - slot + 1
	}
	return (round-1)*config.TeamCount + position, nil
}

func BuildState(input BuildInput) (*State, error) {
	overrides := make(map[int]string)
	for _, trade := range input.Trades {
		if trade.Season != input.Season || trade.Round < 1 || trade.Round > input.Config.RoundCount {
			continue
		}
		pickNo, err := PickNoForOriginalRoster(input.Config, input.Participants, trade.Round, trade.OriginalRosterID)
		if err != nil {
			return nil, err
		}
		overrides[pickNo] = trade.OwnerRosterID
	}

	remote := append([]RemotePick(nil), input.RemotePicks...)
	sort.Slice(remote, func(i, j int) bool { return remote[i].PickNo < remote[j].PickNo })
	state := &State{
		Config: input.Config, Status: input.Status, Season: input.Season,
		Participants: input.Participants, TrackedUserID: input.TrackedUserID,
		PicksByNumber: make(map[int]Pick), DraftedPlayerIDs: make(map[string]struct{}),
		OwnerOverrides: overrides,
	}
	for _, participant := range input.Participants {
		for _, userID := range participant.UserIDs {
			if userID == input.TrackedUserID {
				state.TrackedRosterID = participant.RosterID
			}
		}
	}
	for _, incoming := range remote {
		if incoming.PlayerID == "" {
			return nil, fmt.Errorf("pick %d has no player", incoming.PickNo)
		}
		if _, exists := state.PicksByNumber[incoming.PickNo]; exists {
			return nil, fmt.Errorf("duplicate pick number %d", incoming.PickNo)
		}
		if _, exists := state.DraftedPlayerIDs[incoming.PlayerID]; exists {
			return nil, fmt.Errorf("player %s was drafted twice", incoming.PlayerID)
		}
		plan, err := Planned(input.Config, input.Participants, incoming.PickNo, overrides)
		if err != nil {
			return nil, err
		}
		rosterID := incoming.RosterID
		if rosterID == "" {
			rosterID = plan.OwnerRosterID
		}
		pick := Pick{plan, incoming.PlayerID, rosterID, incoming.PickedByUserID, incoming.Keeper, incoming.Metadata}
		state.Picks = append(state.Picks, pick)
		state.PicksByNumber[pick.PickNo] = pick
		state.DraftedPlayerIDs[pick.PlayerID] = struct{}{}
	}
	return state, nil
}

func (state *State) CurrentPick() *PlannedPick {
	for pickNo := 1; pickNo <= state.Config.TeamCount*state.Config.RoundCount; pickNo++ {
		if _, exists := state.PicksByNumber[pickNo]; exists {
			continue
		}
		pick, err := Planned(state.Config, state.Participants, pickNo, state.OwnerOverrides)
		if err == nil {
			return &pick
		}
	}
	return nil
}

func (state *State) HasBoardGap() bool {
	highest := 0
	for _, pick := range state.Picks {
		if pick.PickNo > highest {
			highest = pick.PickNo
		}
	}
	for pickNo := 1; pickNo <= highest; pickNo++ {
		if _, exists := state.PicksByNumber[pickNo]; !exists {
			return true
		}
	}
	return false
}

func (state *State) NextTrackedPick(after int) *PlannedPick {
	if state.TrackedRosterID == "" {
		return nil
	}
	for pickNo := max(1, after+1); pickNo <= state.Config.TeamCount*state.Config.RoundCount; pickNo++ {
		if _, exists := state.PicksByNumber[pickNo]; exists {
			continue
		}
		pick, err := Planned(state.Config, state.Participants, pickNo, state.OwnerOverrides)
		if err == nil && pick.OwnerRosterID == state.TrackedRosterID {
			return &pick
		}
	}
	return nil
}

func (state *State) Clock() Clock {
	current := state.CurrentPick()
	next := state.NextTrackedPick(0)
	var before *int
	if current != nil && next != nil {
		value := next.PickNo - current.PickNo
		before = &value
	}
	onClock := current != nil && state.TrackedRosterID != "" && current.OwnerRosterID == state.TrackedRosterID
	return Clock{
		Current: current, NextUserPick: next, PicksBeforeUser: before, UserOnClock: onClock,
		RecommendationSafe: state.Status == "drafting" && current != nil && !state.HasBoardGap(),
	}
}

func IsCorePosition(position Position) bool {
	for _, item := range CorePositions {
		if item == position {
			return true
		}
	}
	return false
}

func PositionFromMetadata(metadata map[string]any) Position {
	if raw, ok := metadata["position"].(string); ok {
		position := Position(strings.ToUpper(raw))
		if IsCorePosition(position) || position == K || position == DEF {
			return position
		}
	}
	return WR
}
