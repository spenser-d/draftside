import type { DraftFormat, DraftParticipant, Player, Position } from '@/lib/domain/types';
import type { RemotePick, TradedPick } from '@/lib/domain/state';
import type {
  SleeperDraft,
  SleeperLeagueUser,
  SleeperPick,
  SleeperPlayer,
  SleeperTradedPick,
} from './types';

export function normalizeFormat(type: string): DraftFormat | null {
  if (type === 'snake' || type === 'linear' || type === 'third_round_reversal') return type;
  return null;
}

export function normalizeParticipants(draft: SleeperDraft, users: SleeperLeagueUser[]): DraftParticipant[] {
  const userNames = new Map(users.map((user) => [String(user.user_id), user.metadata?.team_name || user.display_name || user.username]));
  const order = draft.draft_order ?? {};
  const usersBySlot = new Map<number, string[]>();
  for (const [userId, rawSlot] of Object.entries(order)) {
    const slot = Number(rawSlot);
    usersBySlot.set(slot, [...(usersBySlot.get(slot) ?? []), String(userId)]);
  }

  return Array.from({ length: Number(draft.settings.teams) }, (_, index) => {
    const slot = index + 1;
    const userIds = usersBySlot.get(slot) ?? [];
    const rosterId = draft.slot_to_roster_id?.[String(slot)] != null
      ? String(draft.slot_to_roster_id[String(slot)])
      : `slot:${slot}`;
    return {
      slot,
      rosterId,
      userIds,
      displayName: userIds.map((id) => userNames.get(id)).find(Boolean) ?? `Team ${slot}`,
    };
  });
}

export function normalizePicks(picks: SleeperPick[]): RemotePick[] {
  return picks.map((pick) => ({
    pickNo: Number(pick.pick_no),
    playerId: String(pick.player_id),
    rosterId: pick.roster_id == null ? null : String(pick.roster_id),
    pickedByUserId: pick.picked_by ? String(pick.picked_by) : null,
    isKeeper: Boolean(pick.is_keeper),
    metadata: pick.metadata ?? {},
  }));
}

export function normalizeTrades(picks: SleeperTradedPick[]): TradedPick[] {
  return picks.map((pick) => ({
    season: String(pick.season),
    round: Number(pick.round),
    originalRosterId: String(pick.roster_id),
    ownerRosterId: String(pick.owner_id),
  }));
}

const positions = new Set<Position>(['QB', 'RB', 'WR', 'TE', 'K', 'DEF']);

export function normalizePlayers(raw: Record<string, SleeperPlayer>): Player[] {
  return Object.entries(raw).flatMap(([id, player]) => {
    const rawPosition = player.position ?? player.fantasy_positions?.[0];
    if (!rawPosition || !positions.has(rawPosition as Position)) return [];
    const firstName = player.first_name ?? '';
    const lastName = player.last_name ?? '';
    const fullName = player.full_name ?? `${firstName} ${lastName}`.trim();
    if (!fullName) return [];
    return [{
      id: String(player.player_id ?? id),
      firstName,
      lastName,
      fullName,
      position: rawPosition as Position,
      team: player.team ?? null,
      active: player.active !== false,
      status: player.status ?? null,
      injuryStatus: player.injury_status ?? null,
      searchRank: Number.isFinite(Number(player.search_rank)) ? Number(player.search_rank) : 9999,
    }];
  });
}
