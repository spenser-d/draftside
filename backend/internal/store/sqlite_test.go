package store

// Store tests use a temporary SQLite database.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"draftside/internal/domain"
)

func TestCacheAndDraftPersistence(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "draftside.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.WriteCache(ctx, "players", map[string]int{"count": 4}, time.Minute); err != nil {
		t.Fatal(err)
	}
	var cached map[string]int
	if ok, err := store.ReadCache(ctx, "players", &cached); err != nil || !ok || cached["count"] != 4 {
		t.Fatalf("cache: %v %v %+v", ok, err, cached)
	}
	pick := domain.Pick{PlannedPick: domain.PlannedPick{PickNo: 1}, PlayerID: "p1", PickedByRosterID: "1"}
	input := SavedDraft{DraftID: "draft", UserID: "user", Username: "name", LeagueName: "league", Status: "drafting", BoardHash: "v1", State: map[string]any{"ok": true}, Picks: []domain.Pick{pick}, Recommendation: map[string]string{"player": "p1"}, PlayerID: "p1", Score: 1}
	if err := store.SaveDraft(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDraft(ctx, input); err != nil {
		t.Fatal(err)
	}
	var revision int
	if err := store.db.QueryRow("SELECT state_revision FROM draft_sessions WHERE draft_id='draft' AND user_id='user'").Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("revision=%d err=%v", revision, err)
	}
}
