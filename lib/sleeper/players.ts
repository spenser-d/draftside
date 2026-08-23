import { readCache, writeCache } from '@/db/drafts';
import type { Player } from '@/lib/domain/types';
import { sleeperApi } from './client';
import { normalizePlayers } from './normalize';
import type { SleeperPlayer } from './types';

const CACHE_TTL_MS = 24 * 60 * 60 * 1000;

export async function getFantasyPlayers(): Promise<Player[]> {
  const positions = ['QB', 'RB', 'WR', 'TE'];
  const rawMaps = await Promise.all(positions.map(async (position) => {
    const key = `sleeper:players:nfl:${position}`;
    const cached = await readCache<Record<string, SleeperPlayer>>(key);
    if (cached) return cached;
    const fresh = await sleeperApi.getPlayersByPosition(position);
    await writeCache(key, fresh, CACHE_TTL_MS);
    return fresh;
  }));
  const merged = Object.assign({}, ...rawMaps) as Record<string, SleeperPlayer>;
  return normalizePlayers(merged);
}
