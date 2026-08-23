// Package store persists draft state and short-lived API caches in SQLite.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"draftside/internal/domain"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type SavedDraft struct {
	DraftID        string
	UserID         string
	Username       string
	LeagueID       string
	LeagueName     string
	Status         string
	BoardHash      string
	State          any
	Picks          []domain.Pick
	Recommendation any
	PlayerID       string
	Score          int
}

func Open(path string) (*Store, error) {
	if path == "" {
		path = "./data/draftside.sqlite"
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", absolute)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	store := &Store{database}
	if err := store.initialize(); err != nil {
		database.Close()
		return nil, err
	}
	return store, nil
}

func (store *Store) initialize() error {
	_, err := store.db.Exec(`
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
CREATE TABLE IF NOT EXISTS draft_sessions (
  draft_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  username TEXT NOT NULL,
  league_id TEXT,
  league_name TEXT NOT NULL,
  status TEXT NOT NULL,
  board_hash TEXT NOT NULL,
  state_revision INTEGER NOT NULL DEFAULT 0,
  state_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (draft_id, user_id)
);
CREATE TABLE IF NOT EXISTS draft_picks (
  draft_id TEXT NOT NULL,
  pick_no INTEGER NOT NULL,
  player_id TEXT NOT NULL,
  roster_id TEXT NOT NULL,
  state_json TEXT NOT NULL,
  observed_at TEXT NOT NULL,
  PRIMARY KEY (draft_id, pick_no)
);
CREATE TABLE IF NOT EXISTS recommendations (
  draft_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  board_hash TEXT NOT NULL,
  player_id TEXT NOT NULL,
  score INTEGER NOT NULL,
  result_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (draft_id, user_id, board_hash)
);
CREATE TABLE IF NOT EXISTS cache_entries (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`)
	return err
}

func (store *Store) Close() error { return store.db.Close() }

func (store *Store) SaveDraft(ctx context.Context, input SavedDraft) error {
	stateJSON, err := json.Marshal(input.State)
	if err != nil {
		return err
	}
	recommendationJSON, err := json.Marshal(input.Recommendation)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	var existingHash string
	queryErr := transaction.QueryRowContext(ctx,
		"SELECT board_hash FROM draft_sessions WHERE draft_id = ? AND user_id = ?", input.DraftID, input.UserID,
	).Scan(&existingHash)
	if queryErr != nil && queryErr != sql.ErrNoRows {
		return queryErr
	}
	boardChanged := queryErr == sql.ErrNoRows || existingHash != input.BoardHash
	_, err = transaction.ExecContext(ctx, `INSERT INTO draft_sessions
  (draft_id,user_id,username,league_id,league_name,status,board_hash,state_revision,state_json,created_at,updated_at)
  VALUES (?,?,?,?,?,?,?,1,?,?,?)
  ON CONFLICT(draft_id,user_id) DO UPDATE SET
    username=excluded.username, league_id=excluded.league_id, league_name=excluded.league_name,
    status=excluded.status, board_hash=excluded.board_hash,
    state_revision=CASE WHEN draft_sessions.board_hash<>excluded.board_hash THEN draft_sessions.state_revision+1 ELSE draft_sessions.state_revision END,
    state_json=excluded.state_json, updated_at=excluded.updated_at`,
		input.DraftID, input.UserID, input.Username, nullable(input.LeagueID), input.LeagueName,
		input.Status, input.BoardHash, string(stateJSON), now, now)
	if err != nil {
		return err
	}
	_, err = transaction.ExecContext(ctx, `INSERT INTO recommendations
  (draft_id,user_id,board_hash,player_id,score,result_json,created_at)
  VALUES (?,?,?,?,?,?,?)
  ON CONFLICT(draft_id,user_id,board_hash) DO UPDATE SET
    player_id=excluded.player_id, score=excluded.score, result_json=excluded.result_json, created_at=excluded.created_at`,
		input.DraftID, input.UserID, input.BoardHash, input.PlayerID, input.Score, string(recommendationJSON), now)
	if err != nil {
		return err
	}
	if boardChanged {
		if _, err := transaction.ExecContext(ctx, "DELETE FROM draft_picks WHERE draft_id = ?", input.DraftID); err != nil {
			return err
		}
		statement, err := transaction.PrepareContext(ctx, `INSERT INTO draft_picks
  (draft_id,pick_no,player_id,roster_id,state_json,observed_at) VALUES (?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer statement.Close()
		for _, pick := range input.Picks {
			encoded, err := json.Marshal(pick)
			if err != nil {
				return err
			}
			if _, err := statement.ExecContext(ctx, input.DraftID, pick.PickNo, pick.PlayerID, pick.PickedByRosterID, string(encoded), now); err != nil {
				return err
			}
		}
	}
	return transaction.Commit()
}

func (store *Store) ReadCache(ctx context.Context, key string, target any) (bool, error) {
	var valueJSON, expiresAt string
	err := store.db.QueryRowContext(ctx,
		"SELECT value_json, expires_at FROM cache_entries WHERE key = ?", key,
	).Scan(&valueJSON, &expiresAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	expires, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil || !expires.After(time.Now()) {
		_, _ = store.db.ExecContext(ctx, "DELETE FROM cache_entries WHERE key = ?", key)
		return false, nil
	}
	if err := json.Unmarshal([]byte(valueJSON), target); err != nil {
		return false, fmt.Errorf("decode cache %s: %w", key, err)
	}
	return true, nil
}

func (store *Store) WriteCache(ctx context.Context, key string, value any, ttl time.Duration) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = store.db.ExecContext(ctx, `INSERT INTO cache_entries (key,value_json,expires_at,updated_at)
  VALUES (?,?,?,?) ON CONFLICT(key) DO UPDATE SET
  value_json=excluded.value_json, expires_at=excluded.expires_at, updated_at=excluded.updated_at`,
		key, string(encoded), now.Add(ttl).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	return err
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
