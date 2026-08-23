import { ensureDatabase, getD1 } from './runtime';
import type { MadePick } from '@/lib/domain/types';

export async function readCache<T>(key: string): Promise<T | null> {
  await ensureDatabase();
  const row = await getD1().prepare(
    'SELECT value_json AS valueJson, expires_at AS expiresAt FROM cache_entries WHERE key = ?',
  ).bind(key).first<{ valueJson: string; expiresAt: string }>();
  if (!row || Date.parse(row.expiresAt) <= Date.now()) return null;
  return JSON.parse(row.valueJson) as T;
}

export async function writeCache(key: string, value: unknown, ttlMs: number): Promise<void> {
  await ensureDatabase();
  const now = new Date();
  const expiresAt = new Date(now.getTime() + ttlMs).toISOString();
  await getD1().prepare(`INSERT INTO cache_entries (key, value_json, expires_at, updated_at)
    VALUES (?, ?, ?, ?)
    ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json,
      expires_at = excluded.expires_at, updated_at = excluded.updated_at`)
    .bind(key, JSON.stringify(value), expiresAt, now.toISOString()).run();
}

interface SavedDraftInput {
  draftId: string;
  userId: string;
  username: string;
  leagueId: string | null;
  leagueName: string;
  status: string;
  boardHash: string;
  state: unknown;
  picks: MadePick[];
  recommendation: { playerId: string; score: number };
}

export async function saveDraftSnapshot(input: SavedDraftInput): Promise<void> {
  await ensureDatabase();
  const db = getD1();
  const now = new Date().toISOString();
  const statements = [
    db.prepare(`INSERT INTO draft_sessions
      (draft_id, user_id, username, league_id, league_name, status, board_hash, state_json, created_at, updated_at)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      ON CONFLICT(draft_id) DO UPDATE SET user_id = excluded.user_id,
        username = excluded.username, league_id = excluded.league_id,
        league_name = excluded.league_name, status = excluded.status,
        board_hash = excluded.board_hash, state_json = excluded.state_json,
        updated_at = excluded.updated_at`)
      .bind(input.draftId, input.userId, input.username, input.leagueId, input.leagueName,
        input.status, input.boardHash, JSON.stringify(input.state), now, now),
    db.prepare(`INSERT INTO recommendations
      (draft_id, board_hash, player_id, score, result_json, created_at)
      VALUES (?, ?, ?, ?, ?, ?)
      ON CONFLICT(draft_id, board_hash) DO UPDATE SET player_id = excluded.player_id,
        score = excluded.score, result_json = excluded.result_json, created_at = excluded.created_at`)
      .bind(input.draftId, input.boardHash, input.recommendation.playerId,
        input.recommendation.score, JSON.stringify(input.recommendation), now),
    ...input.picks.map((pick) => db.prepare(`INSERT INTO draft_picks
      (draft_id, pick_no, player_id, roster_id, state_json, observed_at)
      VALUES (?, ?, ?, ?, ?, ?)
      ON CONFLICT(draft_id, pick_no) DO UPDATE SET player_id = excluded.player_id,
        roster_id = excluded.roster_id, state_json = excluded.state_json,
        observed_at = excluded.observed_at`)
      .bind(input.draftId, pick.pickNo, pick.playerId, pick.pickedByRosterId, JSON.stringify(pick), now)),
  ];
  await db.batch(statements);
}
