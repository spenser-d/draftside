import type {
  SleeperDraft,
  SleeperLeague,
  SleeperLeagueUser,
  SleeperNflState,
  SleeperPick,
  SleeperPlayer,
  SleeperTradedPick,
  SleeperUser,
} from './types';

const BASE_URL = 'https://api.sleeper.app/v1';

export class SleeperApiError extends Error {
  constructor(message: string, public readonly status: number) {
    super(message);
  }
}

async function getJson<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${BASE_URL}${path}`, {
    ...init,
    headers: { accept: 'application/json', ...init?.headers },
  });
  if (!response.ok) {
    throw new SleeperApiError(`Sleeper returned ${response.status} for ${path}.`, response.status);
  }
  return response.json() as Promise<T>;
}

export const sleeperApi = {
  getUser: (input: string) => getJson<SleeperUser>(`/user/${encodeURIComponent(input)}`),
  getNflState: () => getJson<SleeperNflState>('/state/nfl'),
  getUserDrafts: (userId: string, season: string) =>
    getJson<SleeperDraft[]>(`/user/${userId}/drafts/nfl/${season}`),
  getLeagueDrafts: (leagueId: string) => getJson<SleeperDraft[]>(`/league/${leagueId}/drafts`),
  getDraft: (draftId: string) => getJson<SleeperDraft>(`/draft/${draftId}`),
  getPicks: (draftId: string) => getJson<SleeperPick[]>(`/draft/${draftId}/picks`, { cache: 'no-store' }),
  getTradedPicks: (draftId: string) =>
    getJson<SleeperTradedPick[]>(`/draft/${draftId}/traded_picks`, { cache: 'no-store' }),
  getLeague: (leagueId: string) => getJson<SleeperLeague>(`/league/${leagueId}`),
  getLeagueUsers: (leagueId: string) => getJson<SleeperLeagueUser[]>(`/league/${leagueId}/users`),
  getPlayersByPosition: (position: string) =>
    getJson<Record<string, SleeperPlayer>>(`/players/nfl?position=${position}&active=true`),
};
