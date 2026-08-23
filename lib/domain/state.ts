import { pickNoForOriginalRoster, plannedPick } from './order.ts';
import type { DraftConfig, DraftParticipant, DraftState, MadePick } from './types.ts';

export interface RemotePick {
  pickNo: number;
  playerId: string;
  rosterId: string | null;
  pickedByUserId: string | null;
  isKeeper: boolean;
  metadata: Record<string, unknown>;
}

export interface TradedPick {
  season: string;
  round: number;
  originalRosterId: string;
  ownerRosterId: string;
}

export interface BuildDraftStateInput {
  config: DraftConfig;
  status: string;
  season: string;
  participants: DraftParticipant[];
  trackedUserId: string;
  remotePicks: RemotePick[];
  tradedPicks: TradedPick[];
}

export function buildDraftState(input: BuildDraftStateInput): DraftState {
  const ownerOverrides = new Map<number, string>();
  for (const trade of input.tradedPicks) {
    if (trade.season !== input.season || trade.round < 1 || trade.round > input.config.roundCount) continue;
    const pickNo = pickNoForOriginalRoster(input.config, input.participants, trade.round, trade.originalRosterId);
    ownerOverrides.set(pickNo, trade.ownerRosterId);
  }

  const picksByNumber = new Map<number, MadePick>();
  const draftedPlayerIds = new Set<string>();
  const picks = [...input.remotePicks]
    .sort((a, b) => a.pickNo - b.pickNo)
    .map((remote) => {
      if (picksByNumber.has(remote.pickNo)) throw new Error(`Duplicate pick number ${remote.pickNo}.`);
      if (draftedPlayerIds.has(remote.playerId)) throw new Error(`Player ${remote.playerId} was drafted twice.`);
      const plan = plannedPick(input.config, input.participants, remote.pickNo, ownerOverrides);
      const made: MadePick = {
        ...plan,
        playerId: remote.playerId,
        pickedByRosterId: remote.rosterId ?? plan.ownerRosterId,
        pickedByUserId: remote.pickedByUserId,
        isKeeper: remote.isKeeper,
        metadata: remote.metadata,
      };
      picksByNumber.set(made.pickNo, made);
      draftedPlayerIds.add(made.playerId);
      return made;
    });

  const participant = input.participants.find((item) => item.userIds.includes(input.trackedUserId));
  return {
    config: input.config,
    status: input.status,
    participants: input.participants,
    trackedUserId: input.trackedUserId,
    trackedRosterId: participant?.rosterId ?? null,
    picks,
    picksByNumber,
    draftedPlayerIds,
    ownerOverrides,
  };
}

export function currentPick(state: DraftState) {
  const total = state.config.teamCount * state.config.roundCount;
  for (let pickNo = 1; pickNo <= total; pickNo += 1) {
    if (!state.picksByNumber.has(pickNo)) return plannedPick(state.config, state.participants, pickNo, state.ownerOverrides);
  }
  return null;
}

export function hasBoardGap(state: DraftState): boolean {
  const highest = state.picks.reduce((max, pick) => Math.max(max, pick.pickNo), 0);
  for (let pickNo = 1; pickNo <= highest; pickNo += 1) {
    if (!state.picksByNumber.has(pickNo)) return true;
  }
  return false;
}

export function nextTrackedPick(state: DraftState, afterPickNo = 0) {
  if (!state.trackedRosterId) return null;
  const total = state.config.teamCount * state.config.roundCount;
  for (let pickNo = Math.max(1, afterPickNo + 1); pickNo <= total; pickNo += 1) {
    if (state.picksByNumber.has(pickNo)) continue;
    const plan = plannedPick(state.config, state.participants, pickNo, state.ownerOverrides);
    if (plan.ownerRosterId === state.trackedRosterId) return plan;
  }
  return null;
}

export function draftClock(state: DraftState) {
  const current = currentPick(state);
  const next = nextTrackedPick(state);
  return {
    current,
    nextUserPick: next,
    picksBeforeUser: current && next ? next.pickNo - current.pickNo : null,
    isUserOnClock: Boolean(current && state.trackedRosterId && current.ownerRosterId === state.trackedRosterId),
    recommendationSafe: state.status === 'drafting' && current !== null && !hasBoardGap(state),
  };
}
