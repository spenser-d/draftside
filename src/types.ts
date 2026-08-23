export type Position = 'QB' | 'RB' | 'WR' | 'TE' | 'K' | 'DEF';

export interface PlayerView {
  id: string;
  name: string;
  position: Position;
  nflTeam: string | null;
  injuryStatus: string | null;
}

export interface LiveDraftViewModel {
  draft: {
    id: string;
    leagueName: string;
    formatLabel: string;
    teamCount: number;
    round: number;
    pickInRound: number;
    overallPick: number;
    status: string;
    stateVersion: string;
    sleeperDraftUrl: string;
  };
  connection: { state: 'live' | 'delayed' | 'offline'; lastSyncedAt: string };
  turn: {
    state: 'waiting' | 'on-deck' | 'on-clock' | 'complete' | 'spectating';
    currentRosterName: string;
    userRosterName: string | null;
    userNextPick: number | null;
    picksUntilUser: number | null;
  };
  recommendation: null | {
    status: 'ready' | 'fallback';
    player: PlayerView;
    strength: 'low' | 'medium' | 'high';
    primaryReason: string;
    reasons: Array<{ label: string; detail: string }>;
    backups: PlayerView[];
    generatedAt: string;
    modelVersion: string;
    simulation?: { sampleCount: number; confidence: number; followingPickNo: number | null };
  };
  recentPicks: Array<{
    pickNumber: number;
    label: string;
    rosterName: string;
    player: PlayerView;
    isUserPick: boolean;
    isTradedPick: boolean;
  }>;
  userRoster: { name: string; players: PlayerView[]; positionCounts: Record<string, number> } | null;
  teamsBeforeNextPick: Array<{ name: string; pickNumbers: number[] }>;
  persistence: { saved: boolean };
}

export interface DraftCandidateView {
  draftId: string;
  leagueId: string | null;
  leagueName: string;
  status: string;
  season: string;
  type: string;
  teamCount: number;
  roundCount: number;
  startTime: number | null;
}
