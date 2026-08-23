export type DraftFormat = 'snake' | 'linear' | 'third_round_reversal';
export type Position = 'QB' | 'RB' | 'WR' | 'TE' | 'K' | 'DEF';

export interface DraftConfig {
  draftId: string;
  teamCount: number;
  roundCount: number;
  format: DraftFormat;
}

export interface DraftParticipant {
  slot: number;
  rosterId: string;
  userIds: string[];
  displayName: string;
}

export interface PlannedPick {
  pickNo: number;
  round: number;
  pickInRound: number;
  originSlot: number;
  originalRosterId: string;
  ownerRosterId: string;
}

export interface MadePick extends PlannedPick {
  playerId: string;
  pickedByRosterId: string;
  pickedByUserId: string | null;
  isKeeper: boolean;
  metadata: Record<string, unknown>;
}

export interface DraftState {
  config: DraftConfig;
  status: string;
  participants: DraftParticipant[];
  trackedUserId: string;
  trackedRosterId: string | null;
  picks: MadePick[];
  picksByNumber: Map<number, MadePick>;
  draftedPlayerIds: Set<string>;
  ownerOverrides: Map<number, string>;
}

export interface Player {
  id: string;
  firstName: string;
  lastName: string;
  fullName: string;
  position: Position;
  team: string | null;
  active: boolean;
  status: string | null;
  injuryStatus: string | null;
  searchRank: number;
}
