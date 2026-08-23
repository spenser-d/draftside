import type { DraftConfig, DraftFormat, DraftParticipant, PlannedPick } from './types.ts';

export function direction(format: DraftFormat, round: number): 'forward' | 'reverse' {
  if (format === 'linear') return 'forward';
  if (format === 'snake') return round % 2 === 1 ? 'forward' : 'reverse';
  if (round === 1) return 'forward';
  if (round === 2 || round === 3) return 'reverse';
  return round % 2 === 0 ? 'forward' : 'reverse';
}

export function plannedPick(
  config: DraftConfig,
  participants: DraftParticipant[],
  pickNo: number,
  ownerOverrides: Map<number, string> = new Map(),
): PlannedPick {
  const totalPicks = config.teamCount * config.roundCount;
  if (!Number.isInteger(pickNo) || pickNo < 1 || pickNo > totalPicks) {
    throw new Error(`Pick ${pickNo} is outside the draft board.`);
  }

  const round = Math.floor((pickNo - 1) / config.teamCount) + 1;
  const pickInRound = ((pickNo - 1) % config.teamCount) + 1;
  const originSlot = direction(config.format, round) === 'forward'
    ? pickInRound
    : config.teamCount - pickInRound + 1;
  const participant = participants.find((item) => item.slot === originSlot);
  if (!participant) throw new Error(`Draft slot ${originSlot} has no participant.`);

  return {
    pickNo,
    round,
    pickInRound,
    originSlot,
    originalRosterId: participant.rosterId,
    ownerRosterId: ownerOverrides.get(pickNo) ?? participant.rosterId,
  };
}

export function pickNoForOriginalRoster(
  config: DraftConfig,
  participants: DraftParticipant[],
  round: number,
  originalRosterId: string,
): number {
  const participant = participants.find((item) => item.rosterId === originalRosterId);
  if (!participant) throw new Error(`Unknown original roster ${originalRosterId}.`);
  const boardPosition = direction(config.format, round) === 'forward'
    ? participant.slot
    : config.teamCount - participant.slot + 1;
  return (round - 1) * config.teamCount + boardPosition;
}
