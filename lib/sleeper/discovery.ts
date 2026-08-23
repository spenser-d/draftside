import { sleeperApi } from './client';
import type { SleeperDraft } from './types';

export interface DraftCandidate {
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

function statusPriority(status: string): number {
  if (status === 'drafting') return 0;
  if (status === 'pre_draft') return 1;
  if (status === 'complete') return 3;
  return 2;
}

export async function discoverDrafts(username: string): Promise<{
  userId: string;
  displayName: string;
  candidates: DraftCandidate[];
}> {
  const [user, nflState] = await Promise.all([
    sleeperApi.getUser(username),
    sleeperApi.getNflState(),
  ]);
  const seasons = [...new Set([
    nflState.league_season,
    nflState.league_create_season,
    nflState.season,
    nflState.previous_season,
  ].filter(Boolean))];

  const draftLists = await Promise.all(seasons.map((season) => sleeperApi.getUserDrafts(user.user_id, season)));
  const uniqueDrafts = new Map<string, SleeperDraft>();
  for (const draft of draftLists.flat()) uniqueDrafts.set(String(draft.draft_id), draft);

  const leagueIds = [...new Set([...uniqueDrafts.values()].map((draft) => draft.league_id).filter(Boolean))] as string[];
  const leagueEntries = await Promise.all(leagueIds.map(async (leagueId) => {
    try {
      const league = await sleeperApi.getLeague(leagueId);
      return [leagueId, league.name] as const;
    } catch {
      return [leagueId, 'Sleeper League'] as const;
    }
  }));
  const leagueNames = new Map(leagueEntries);

  const candidates = [...uniqueDrafts.values()]
    .map((draft): DraftCandidate => ({
      draftId: String(draft.draft_id),
      leagueId: draft.league_id ? String(draft.league_id) : null,
      leagueName: draft.league_id ? leagueNames.get(String(draft.league_id)) ?? 'Sleeper League' : draft.metadata?.name ?? 'Mock Draft',
      status: draft.status,
      season: draft.season,
      type: draft.type,
      teamCount: Number(draft.settings.teams),
      roundCount: Number(draft.settings.rounds),
      startTime: draft.start_time,
    }))
    .sort((a, b) => statusPriority(a.status) - statusPriority(b.status) || (b.startTime ?? 0) - (a.startTime ?? 0));

  return { userId: String(user.user_id), displayName: user.display_name, candidates };
}

export async function inspectDirectDraft(draftId: string, username: string) {
  const [user, draft] = await Promise.all([sleeperApi.getUser(username), sleeperApi.getDraft(draftId)]);
  let leagueName = draft.metadata?.name ?? 'Sleeper Draft';
  if (draft.league_id) {
    try {
      leagueName = (await sleeperApi.getLeague(String(draft.league_id))).name;
    } catch {
      // A mock or deleted league can still have a valid draft board.
    }
  }
  const candidate: DraftCandidate = {
    draftId: String(draft.draft_id),
    leagueId: draft.league_id ? String(draft.league_id) : null,
    leagueName,
    status: draft.status,
    season: draft.season,
    type: draft.type,
    teamCount: Number(draft.settings.teams),
    roundCount: Number(draft.settings.rounds),
    startTime: draft.start_time,
  };
  return { userId: String(user.user_id), displayName: user.display_name, candidates: [candidate] };
}
