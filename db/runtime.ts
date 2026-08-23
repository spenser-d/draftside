import { env } from 'cloudflare:workers';

let initialized: Promise<void> | null = null;

export function getD1(): D1Database {
  if (!env.DB) throw new Error('The draft database is unavailable.');
  return env.DB;
}

export function ensureDatabase(): Promise<void> {
  if (initialized) return initialized;
  const db = getD1();
  initialized = db.batch([
    db.prepare(`CREATE TABLE IF NOT EXISTS draft_sessions (
      draft_id TEXT PRIMARY KEY,
      user_id TEXT NOT NULL,
      username TEXT NOT NULL,
      league_id TEXT,
      league_name TEXT NOT NULL,
      status TEXT NOT NULL,
      board_hash TEXT NOT NULL,
      state_json TEXT NOT NULL,
      created_at TEXT NOT NULL,
      updated_at TEXT NOT NULL
    )`),
    db.prepare(`CREATE TABLE IF NOT EXISTS draft_picks (
      draft_id TEXT NOT NULL,
      pick_no INTEGER NOT NULL,
      player_id TEXT NOT NULL,
      roster_id TEXT NOT NULL,
      state_json TEXT NOT NULL,
      observed_at TEXT NOT NULL,
      PRIMARY KEY (draft_id, pick_no)
    )`),
    db.prepare(`CREATE TABLE IF NOT EXISTS recommendations (
      draft_id TEXT NOT NULL,
      board_hash TEXT NOT NULL,
      player_id TEXT NOT NULL,
      score INTEGER NOT NULL,
      result_json TEXT NOT NULL,
      created_at TEXT NOT NULL,
      PRIMARY KEY (draft_id, board_hash)
    )`),
    db.prepare(`CREATE TABLE IF NOT EXISTS cache_entries (
      key TEXT PRIMARY KEY,
      value_json TEXT NOT NULL,
      expires_at TEXT NOT NULL,
      updated_at TEXT NOT NULL
    )`),
  ]).then(() => undefined).catch((error) => {
    initialized = null;
    throw error;
  });
  return initialized;
}
