package live

// Watcher tests cover concurrency and last-good-board behavior.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"draftside/internal/domain"
	"draftside/internal/sleeper"
)

func testSnapshot(playerID string) *Snapshot {
	snapshot := &Snapshot{
		Draft: sleeper.Draft{DraftID: "draft", Status: "drafting"},
		Picks: []domain.RemotePick{{PickNo: 1, PlayerID: playerID}},
	}
	_ = snapshot.canonicalize("draft")
	return snapshot
}

func TestWatcherCoalescesAndRevises(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	watcher := NewWatcher("draft", func(context.Context, string) (*Snapshot, error) {
		calls.Add(1)
		<-release
		return testSnapshot("p1"), nil
	}, time.Second)
	results := make(chan State, 2)
	go func() { state, _ := watcher.Poll(context.Background()); results <- state }()
	go func() { state, _ := watcher.Poll(context.Background()); results <- state }()
	time.Sleep(10 * time.Millisecond)
	close(release)
	first, second := <-results, <-results
	if calls.Load() != 1 || first.Revision != 1 || second.Revision != 1 {
		t.Fatalf("calls=%d revisions=%d,%d", calls.Load(), first.Revision, second.Revision)
	}
}

func TestWatcherPreservesLastGoodBoard(t *testing.T) {
	var call int
	watcher := NewWatcher("draft", func(context.Context, string) (*Snapshot, error) {
		call++
		if call == 1 {
			return testSnapshot("p1"), nil
		}
		return nil, context.DeadlineExceeded
	}, time.Second)
	_, _ = watcher.Poll(context.Background())
	state, err := watcher.Poll(context.Background())
	if err == nil || state.Health != Delayed || state.Snapshot.Picks[0].PlayerID != "p1" || state.Revision != 1 {
		t.Fatalf("unexpected state: %+v (%v)", state, err)
	}
}

func TestRegistryEvictsWatcherAfterPermanentFailures(t *testing.T) {
	var calls atomic.Int32
	registry := NewRegistry(context.Background(), func(context.Context, string) (*Snapshot, error) {
		calls.Add(1)
		return nil, context.DeadlineExceeded
	}, 500*time.Millisecond)
	defer registry.Close()

	first := registry.Get("missing-draft")
	select {
	case <-first.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("failed watcher did not stop after the failure limit")
	}
	if state := first.State(); state.ConsecutiveFailures != maxConsecutiveFailures || state.Health != Offline {
		t.Fatalf("unexpected terminal state: %+v", state)
	}
	if calls.Load() != maxConsecutiveFailures {
		t.Fatalf("calls=%d; want %d", calls.Load(), maxConsecutiveFailures)
	}
	registry.mu.Lock()
	_, retained := registry.watchers["missing-draft"]
	registry.mu.Unlock()
	if retained {
		t.Fatal("terminal watcher remained in the registry after its loop exited")
	}

	second := registry.Get("missing-draft")
	if second == first {
		t.Fatal("registry retained a permanently failed watcher")
	}
}

func TestWatcherKeepsRetryingAfterFailuresWhenBoardExists(t *testing.T) {
	var calls atomic.Int32
	watcher := NewWatcher("draft", func(context.Context, string) (*Snapshot, error) {
		if calls.Add(1) == 1 {
			return testSnapshot("p1"), nil
		}
		return nil, context.DeadlineExceeded
	}, 500*time.Millisecond)
	watcher.Start(context.Background())
	defer watcher.Stop()

	deadline := time.After(2500 * time.Millisecond)
	for calls.Load() < maxConsecutiveFailures+1 {
		select {
		case <-deadline:
			t.Fatalf("calls=%d; watcher did not keep retrying after a last-good board", calls.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
	state := watcher.State()
	if state.Snapshot == nil || state.ConsecutiveFailures < maxConsecutiveFailures || state.Health != Offline {
		t.Fatalf("unexpected stale-board state: %+v", state)
	}
	select {
	case <-watcher.Done():
		t.Fatal("watcher stopped despite retaining a last-good board")
	default:
	}
	watcher.Stop()
	select {
	case <-watcher.Done():
	case <-time.After(time.Second):
		t.Fatal("watcher did not stop after cancellation")
	}
}
