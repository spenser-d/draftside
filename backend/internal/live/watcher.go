// Package live maintains shared, coalesced live-draft snapshots.
package live

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"draftside/internal/domain"
	"draftside/internal/sleeper"
)

type Health string

const (
	Starting               Health = "starting"
	Live                   Health = "live"
	Delayed                Health = "delayed"
	Offline                Health = "offline"
	Complete               Health = "complete"
	maxConsecutiveFailures        = 3
)

type Snapshot struct {
	Draft       sleeper.Draft       `json:"draft"`
	Picks       []domain.RemotePick `json:"picks"`
	Trades      []domain.Trade      `json:"trades"`
	Fingerprint string              `json:"fingerprint"`
}

type State struct {
	DraftID             string
	Revision            int64
	Snapshot            *Snapshot
	Health              Health
	LastAttemptAt       time.Time
	LastSuccessAt       time.Time
	LastChangeAt        time.Time
	ConsecutiveFailures int
	LastError           error
}

type Source func(context.Context, string) (*Snapshot, error)

func SleeperSource(client *sleeper.Client) Source {
	return func(ctx context.Context, draftID string) (*Snapshot, error) {
		var draft sleeper.Draft
		var picks []sleeper.Pick
		var trades []sleeper.TradedPick
		var draftErr, picksErr, tradesErr error
		var wait sync.WaitGroup
		wait.Add(3)
		go func() { defer wait.Done(); draft, draftErr = client.Draft(ctx, draftID) }()
		go func() { defer wait.Done(); picks, picksErr = client.Picks(ctx, draftID) }()
		go func() { defer wait.Done(); trades, tradesErr = client.TradedPicks(ctx, draftID) }()
		wait.Wait()
		if draftErr != nil {
			return nil, draftErr
		}
		if picksErr != nil {
			return nil, picksErr
		}
		if tradesErr != nil {
			return nil, tradesErr
		}
		snapshot := &Snapshot{Draft: draft, Picks: sleeper.NormalizePicks(picks), Trades: sleeper.NormalizeTrades(trades)}
		if err := snapshot.canonicalize(draftID); err != nil {
			return nil, err
		}
		return snapshot, nil
	}
}

func (snapshot *Snapshot) canonicalize(draftID string) error {
	if snapshot.Draft.DraftID != draftID {
		return fmt.Errorf("watcher for %s received draft %s", draftID, snapshot.Draft.DraftID)
	}
	pickNumbers := make(map[int]struct{})
	playerIDs := make(map[string]struct{})
	for _, pick := range snapshot.Picks {
		if pick.PickNo < 1 || pick.PlayerID == "" {
			return fmt.Errorf("invalid pick %d", pick.PickNo)
		}
		if _, exists := pickNumbers[pick.PickNo]; exists {
			return fmt.Errorf("duplicate pick number %d", pick.PickNo)
		}
		if _, exists := playerIDs[pick.PlayerID]; exists {
			return fmt.Errorf("player %s was drafted twice", pick.PlayerID)
		}
		pickNumbers[pick.PickNo] = struct{}{}
		playerIDs[pick.PlayerID] = struct{}{}
	}
	payload, err := json.Marshal(struct {
		Draft  sleeper.Draft       `json:"draft"`
		Picks  []domain.RemotePick `json:"picks"`
		Trades []domain.Trade      `json:"trades"`
	}{snapshot.Draft, snapshot.Picks, snapshot.Trades})
	if err != nil {
		return err
	}
	digest := sha256.Sum256(payload)
	snapshot.Fingerprint = hex.EncodeToString(digest[:12])
	return nil
}

type Watcher struct {
	draftID  string
	source   Source
	interval time.Duration

	mu                sync.RWMutex
	state             State
	inFlight          chan struct{}
	cancel            context.CancelFunc
	done              chan struct{}
	running           bool
	onTerminalFailure func(*Watcher)
}

func NewWatcher(draftID string, source Source, interval time.Duration) *Watcher {
	if interval < 500*time.Millisecond {
		interval = 500 * time.Millisecond
	}
	return &Watcher{
		draftID: draftID, source: source, interval: interval,
		state: State{DraftID: draftID, Health: Starting}, done: make(chan struct{}),
	}
}

func (watcher *Watcher) Start(parent context.Context) {
	watcher.mu.Lock()
	if watcher.running {
		watcher.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	watcher.cancel = cancel
	watcher.done = make(chan struct{})
	watcher.running = true
	done := watcher.done
	watcher.mu.Unlock()
	go watcher.loop(ctx, done)
}

func (watcher *Watcher) Stop() {
	watcher.mu.Lock()
	if watcher.cancel != nil {
		watcher.cancel()
		watcher.cancel = nil
	}
	watcher.mu.Unlock()
}

func (watcher *Watcher) loop(ctx context.Context, done chan struct{}) {
	defer func() {
		watcher.mu.Lock()
		if watcher.done == done {
			watcher.running = false
			watcher.cancel = nil
		}
		terminalFailure := watcher.state.Snapshot == nil && watcher.state.ConsecutiveFailures >= maxConsecutiveFailures
		onTerminalFailure := watcher.onTerminalFailure
		watcher.mu.Unlock()
		if terminalFailure && onTerminalFailure != nil {
			onTerminalFailure(watcher)
		}
		close(done)
	}()
	state, _ := watcher.Poll(ctx)
	if watcher.shouldStop(state) {
		return
	}
	ticker := time.NewTicker(watcher.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			state, _ := watcher.Poll(ctx)
			if watcher.shouldStop(state) {
				return
			}
		}
	}
}

func (watcher *Watcher) Poll(ctx context.Context) (State, error) {
	watcher.mu.Lock()
	if pending := watcher.inFlight; pending != nil {
		watcher.mu.Unlock()
		select {
		case <-ctx.Done():
			return watcher.State(), ctx.Err()
		case <-pending:
			state := watcher.State()
			return state, state.LastError
		}
	}
	pending := make(chan struct{})
	watcher.inFlight = pending
	watcher.state.LastAttemptAt = time.Now().UTC()
	watcher.mu.Unlock()

	snapshot, err := watcher.source(ctx, watcher.draftID)
	completedAt := time.Now().UTC()

	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	defer close(pending)
	defer func() { watcher.inFlight = nil }()
	if err != nil {
		watcher.state.ConsecutiveFailures++
		watcher.state.LastError = err
		if watcher.state.Snapshot == nil || watcher.state.ConsecutiveFailures >= maxConsecutiveFailures {
			watcher.state.Health = Offline
		} else {
			watcher.state.Health = Delayed
		}
		return watcher.state, err
	}
	changed := watcher.state.Snapshot == nil || watcher.state.Snapshot.Fingerprint != snapshot.Fingerprint
	watcher.state.Snapshot = snapshot
	watcher.state.LastSuccessAt = completedAt
	watcher.state.LastError = nil
	watcher.state.ConsecutiveFailures = 0
	if changed {
		watcher.state.Revision++
		watcher.state.LastChangeAt = completedAt
	}
	if snapshot.Draft.Status == "complete" {
		watcher.state.Health = Complete
	} else {
		watcher.state.Health = Live
	}
	return watcher.state, nil
}

func (watcher *Watcher) shouldStop(state State) bool {
	return state.Health == Complete || (state.Snapshot == nil && state.ConsecutiveFailures >= maxConsecutiveFailures)
}

func (watcher *Watcher) Done() <-chan struct{} { return watcher.done }

func (watcher *Watcher) State() State {
	watcher.mu.RLock()
	defer watcher.mu.RUnlock()
	return watcher.state
}

type Registry struct {
	ctx      context.Context
	source   Source
	interval time.Duration
	mu       sync.Mutex
	watchers map[string]*Watcher
}

func NewRegistry(ctx context.Context, source Source, interval time.Duration) *Registry {
	return &Registry{ctx, source, interval, sync.Mutex{}, make(map[string]*Watcher)}
}

func (registry *Registry) Get(draftID string) *Watcher {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if watcher := registry.watchers[draftID]; watcher != nil {
		state := watcher.State()
		if state.Snapshot != nil || state.ConsecutiveFailures < maxConsecutiveFailures {
			return watcher
		}
		// Failed watchers have stopped their polling loop. Drop them before
		// creating a replacement so invalid IDs cannot occupy the registry
		// indefinitely or continue polling Sleeper.
		delete(registry.watchers, draftID)
	}
	watcher := NewWatcher(draftID, registry.source, registry.interval)
	watcher.onTerminalFailure = func(completed *Watcher) {
		registry.removeTerminalFailure(draftID, completed)
	}
	registry.watchers[draftID] = watcher
	watcher.Start(registry.ctx)
	return watcher
}

func (registry *Registry) removeTerminalFailure(draftID string, completed *Watcher) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.watchers[draftID] != completed {
		return
	}
	state := completed.State()
	if state.Snapshot == nil && state.ConsecutiveFailures >= maxConsecutiveFailures {
		delete(registry.watchers, draftID)
	}
}

func (registry *Registry) Close() {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for _, watcher := range registry.watchers {
		watcher.Stop()
	}
	registry.watchers = make(map[string]*Watcher)
}

func IsNotFound(err error) bool {
	var apiError *sleeper.APIError
	return errors.As(err, &apiError) && apiError.Status == httpStatusNotFound
}

const httpStatusNotFound = 404
