import { draftClock, nextTrackedPick } from './domain/state.ts';
import { plannedPick } from './domain/order.ts';
import type { DraftState, Player, Position } from './domain/types.ts';

const corePositions: Position[] = ['QB', 'RB', 'WR', 'TE'];

export interface Recommendation {
  playerId: string;
  player: Player;
  score: number;
  strength: 'low' | 'medium' | 'high';
  primaryReason: string;
  reasons: Array<{ label: string; detail: string }>;
  backups: Player[];
  generatedAt: string;
  modelVersion: 'baseline-0.1';
}

function positionForPick(state: DraftState, pickNo: number, playersById: Map<string, Player>): Position | null {
  const pick = state.picksByNumber.get(pickNo);
  if (!pick) return null;
  const known = playersById.get(pick.playerId)?.position;
  if (known) return known;
  const metadataPosition = pick.metadata.position;
  return typeof metadataPosition === 'string' && corePositions.includes(metadataPosition as Position)
    ? metadataPosition as Position
    : null;
}

function rosterCounts(state: DraftState, playersById: Map<string, Player>) {
  const counts = new Map<string, Record<Position, number>>();
  for (const participant of state.participants) {
    counts.set(participant.rosterId, { QB: 0, RB: 0, WR: 0, TE: 0, K: 0, DEF: 0 });
  }
  for (const pick of state.picks) {
    const position = positionForPick(state, pick.pickNo, playersById);
    if (!position) continue;
    const roster = counts.get(pick.pickedByRosterId);
    if (roster) roster[position] += 1;
  }
  return counts;
}

function starterRequirements(rosterPositions: string[]) {
  const requirements: Record<Position, number> = { QB: 0, RB: 0, WR: 0, TE: 0, K: 0, DEF: 0 };
  for (const position of rosterPositions) {
    if (position in requirements) requirements[position as Position] += 1;
  }
  if (requirements.QB + requirements.RB + requirements.WR + requirements.TE === 0) {
    return { ...requirements, QB: 1, RB: 2, WR: 2, TE: 1 };
  }
  return requirements;
}

function injuryPenalty(player: Player): number {
  const status = `${player.status ?? ''} ${player.injuryStatus ?? ''}`.toLowerCase();
  if (status.includes('reserve') || status.includes('out')) return 170;
  if (status.includes('doubtful')) return 90;
  if (status.includes('questionable')) return 28;
  return 0;
}

export function recommendPlayer(
  state: DraftState,
  players: Player[],
  rosterPositions: string[],
): Recommendation | null {
  const available = players
    .filter((player) => player.active && corePositions.includes(player.position) && !state.draftedPlayerIds.has(player.id))
    .sort((a, b) => a.searchRank - b.searchRank || a.fullName.localeCompare(b.fullName));
  if (!available.length) return null;

  const playersById = new Map(players.map((player) => [player.id, player]));
  const counts = rosterCounts(state, playersById);
  const requirements = starterRequirements(rosterPositions);
  const userCounts = state.trackedRosterId ? counts.get(state.trackedRosterId) : undefined;
  const clock = draftClock(state);
  const decisionPick = clock.nextUserPick;
  const followingUserPick = decisionPick ? nextTrackedPick(state, decisionPick.pickNo) : null;
  const threatEnd = clock.isUserOnClock
    ? followingUserPick?.pickNo ?? decisionPick?.pickNo ?? clock.current?.pickNo ?? 0
    : decisionPick?.pickNo ?? clock.current?.pickNo ?? 0;
  const threatStart = clock.current?.pickNo ?? 0;

  const opponentNeed = new Map<Position, number>(corePositions.map((position) => [position, 0]));
  const seenRosterPosition = new Set<string>();
  for (let pickNo = threatStart + (clock.isUserOnClock ? 1 : 0); pickNo < threatEnd; pickNo += 1) {
    if (state.picksByNumber.has(pickNo)) continue;
    const owner = plannedPick(state.config, state.participants, pickNo, state.ownerOverrides).ownerRosterId;
    if (owner === state.trackedRosterId) continue;
    const ownerCounts = counts.get(owner);
    for (const position of corePositions) {
      const key = `${owner}:${position}`;
      if (!seenRosterPosition.has(key) && (ownerCounts?.[position] ?? 0) < requirements[position]) {
        opponentNeed.set(position, (opponentNeed.get(position) ?? 0) + 1);
        seenRosterPosition.add(key);
      }
    }
  }

  const nextRankByPosition = new Map<string, number>();
  for (let index = 0; index < available.length; index += 1) {
    const player = available[index];
    const next = available.slice(index + 1).find((candidate) => candidate.position === player.position);
    nextRankByPosition.set(player.id, next?.searchRank ?? player.searchRank + 30);
  }

  const scored = available.slice(0, 160).map((player) => {
    const base = Math.max(0, 520 - Math.min(player.searchRank, 520));
    const openStarter = userCounts ? userCounts[player.position] < requirements[player.position] : false;
    const needBonus = openStarter ? 92 : (['RB', 'WR', 'TE'].includes(player.position) ? 24 : 0);
    const demand = opponentNeed.get(player.position) ?? 0;
    const pressureBonus = Math.min(72, demand * 9);
    const rankGap = Math.max(0, (nextRankByPosition.get(player.id) ?? player.searchRank) - player.searchRank);
    const scarcityBonus = Math.min(42, rankGap * 2);
    const penalty = injuryPenalty(player);
    return { player, score: Math.round(base + needBonus + pressureBonus + scarcityBonus - penalty), openStarter, demand, rankGap, penalty };
  }).sort((a, b) => b.score - a.score || a.player.searchRank - b.player.searchRank);

  const best = scored[0];
  const margin = best.score - (scored[1]?.score ?? best.score);
  const strength: Recommendation['strength'] = margin >= 28 ? 'high' : margin >= 10 ? 'medium' : 'low';
  const reasons: Recommendation['reasons'] = [];
  if (best.openStarter) reasons.push({ label: 'Roster fit', detail: `Fills an open ${best.player.position} starter slot on your current roster.` });
  if (best.demand > 0) reasons.push({ label: 'Opponent pressure', detail: `${best.demand} team${best.demand === 1 ? '' : 's'} in the relevant pick window still need a ${best.player.position}.` });
  if (best.rankGap >= 8) reasons.push({ label: 'Tier edge', detail: `The next available ${best.player.position} is meaningfully lower in the current Sleeper player order.` });
  if (!reasons.length) reasons.push({ label: 'Best baseline value', detail: 'Highest deterministic value among currently available players.' });
  if (best.penalty > 0) reasons.push({ label: 'Availability caution', detail: `Sleeper currently lists this player as ${best.player.injuryStatus ?? best.player.status ?? 'limited'}.` });

  return {
    playerId: best.player.id,
    player: best.player,
    score: best.score,
    strength,
    primaryReason: reasons[0].detail,
    reasons,
    backups: scored.slice(1, 3).map((item) => item.player),
    generatedAt: new Date().toISOString(),
    modelVersion: 'baseline-0.1',
  };
}
