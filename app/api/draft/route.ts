import { saveDraftSnapshot } from '@/db/drafts';
import { buildDraftState, draftClock, nextTrackedPick } from '@/lib/domain/state';
import type { Player, Position } from '@/lib/domain/types';
import type { LiveDraftViewModel, PlayerView } from '@/lib/live/types';
import { plannedPick } from '@/lib/domain/order';
import { recommendPlayer } from '@/lib/recommendation';
import { sleeperApi, SleeperApiError } from '@/lib/sleeper/client';
import { normalizeFormat, normalizeParticipants, normalizePicks, normalizeTrades } from '@/lib/sleeper/normalize';
import { getFantasyPlayers } from '@/lib/sleeper/players';

function playerView(player: Player): PlayerView {
  return {
    id: player.id,
    name: player.fullName,
    position: player.position,
    nflTeam: player.team,
    injuryStatus: player.injuryStatus,
  };
}

function metadataPlayer(playerId: string, metadata: Record<string, unknown>): Player {
  const firstName = typeof metadata.first_name === 'string' ? metadata.first_name : '';
  const lastName = typeof metadata.last_name === 'string' ? metadata.last_name : '';
  const rawPosition = typeof metadata.position === 'string' ? metadata.position : 'WR';
  const position = ['QB', 'RB', 'WR', 'TE', 'K', 'DEF'].includes(rawPosition) ? rawPosition as Position : 'WR';
  return {
    id: playerId,
    firstName,
    lastName,
    fullName: `${firstName} ${lastName}`.trim() || `Player ${playerId}`,
    position,
    team: typeof metadata.team === 'string' ? metadata.team : null,
    active: true,
    status: typeof metadata.status === 'string' ? metadata.status : null,
    injuryStatus: typeof metadata.injury_status === 'string' ? metadata.injury_status : null,
    searchRank: 9999,
  };
}

async function hashBoard(value: string): Promise<string> {
  const bytes = new TextEncoder().encode(value);
  const digest = await crypto.subtle.digest('SHA-256', bytes);
  return [...new Uint8Array(digest)].map((byte) => byte.toString(16).padStart(2, '0')).join('').slice(0, 24);
}

export async function GET(request: Request) {
  const url = new URL(request.url);
  const draftId = url.searchParams.get('draftId')?.trim();
  const userId = url.searchParams.get('userId')?.trim();
  const username = url.searchParams.get('username')?.trim() || 'Sleeper user';
  if (!draftId || !userId) return Response.json({ error: 'Draft ID and user ID are required.' }, { status: 400 });

  try {
    const draft = await sleeperApi.getDraft(draftId);
    const [remotePicks, remoteTrades, users, league, players] = await Promise.all([
      sleeperApi.getPicks(draftId),
      sleeperApi.getTradedPicks(draftId),
      draft.league_id ? sleeperApi.getLeagueUsers(String(draft.league_id)) : Promise.resolve([]),
      draft.league_id ? sleeperApi.getLeague(String(draft.league_id)) : Promise.resolve(null),
      getFantasyPlayers(),
    ]);

    const format = normalizeFormat(draft.type);
    if (!format) return Response.json({ error: `Draft type “${draft.type}” is not supported yet.` }, { status: 422 });
    const participants = normalizeParticipants(draft, users);
    const state = buildDraftState({
      config: {
        draftId,
        teamCount: Number(draft.settings.teams),
        roundCount: Number(draft.settings.rounds),
        format,
      },
      status: draft.status,
      season: draft.season,
      participants,
      trackedUserId: userId,
      remotePicks: normalizePicks(remotePicks),
      tradedPicks: normalizeTrades(remoteTrades),
    });

    const playersById = new Map(players.map((player) => [player.id, player]));
    const getPlayer = (playerId: string, metadata: Record<string, unknown>) =>
      playersById.get(playerId) ?? metadataPlayer(playerId, metadata);
    const clock = draftClock(state);
    const current = clock.current;
    const recommendation = recommendPlayer(state, players, league?.roster_positions ?? []);
    const boardHash = await hashBoard(JSON.stringify({
      status: draft.status,
      picks: state.picks.map((pick) => [pick.pickNo, pick.playerId, pick.pickedByRosterId]),
      trades: [...state.ownerOverrides.entries()],
    }));

    const participantName = (rosterId: string | null | undefined) =>
      participants.find((item) => item.rosterId === rosterId)?.displayName ?? 'Unknown team';
    const userPicks = state.trackedRosterId
      ? state.picks.filter((pick) => pick.pickedByRosterId === state.trackedRosterId)
      : [];
    const positionCounts: Record<string, number> = { QB: 0, RB: 0, WR: 0, TE: 0 };
    for (const pick of userPicks) {
      const position = getPlayer(pick.playerId, pick.metadata).position;
      positionCounts[position] = (positionCounts[position] ?? 0) + 1;
    }

    const followingUserPick = clock.nextUserPick ? nextTrackedPick(state, clock.nextUserPick.pickNo) : null;
    const contextEnd = clock.isUserOnClock ? followingUserPick?.pickNo : clock.nextUserPick?.pickNo;
    const teamsAheadMap = new Map<string, number[]>();
    if (current && contextEnd) {
      for (let pickNo = current.pickNo + (clock.isUserOnClock ? 1 : 0); pickNo < contextEnd; pickNo += 1) {
        if (state.picksByNumber.has(pickNo)) continue;
        const owner = plannedPick(state.config, participants, pickNo, state.ownerOverrides).ownerRosterId;
        if (owner === state.trackedRosterId) continue;
        teamsAheadMap.set(owner, [...(teamsAheadMap.get(owner) ?? []), pickNo]);
      }
    }

    let turnState: LiveDraftViewModel['turn']['state'] = 'waiting';
    if (draft.status === 'complete' || !current) turnState = 'complete';
    else if (!state.trackedRosterId) turnState = 'spectating';
    else if (clock.isUserOnClock) turnState = 'on-clock';
    else if (clock.picksBeforeUser === 1) turnState = 'on-deck';

    const view: LiveDraftViewModel = {
      draft: {
        id: draftId,
        leagueName: league?.name ?? draft.metadata?.name ?? 'Sleeper Draft',
        formatLabel: `${state.config.teamCount}-team · ${draft.type}`,
        teamCount: state.config.teamCount,
        round: current?.round ?? state.config.roundCount,
        pickInRound: current?.pickInRound ?? state.config.teamCount,
        overallPick: current?.pickNo ?? state.config.teamCount * state.config.roundCount,
        status: draft.status,
        stateVersion: boardHash,
        sleeperDraftUrl: `https://sleeper.com/draft/nfl/${draftId}`,
      },
      connection: { state: 'live', lastSyncedAt: new Date().toISOString() },
      turn: {
        state: turnState,
        currentRosterName: participantName(current?.ownerRosterId),
        userRosterName: participantName(state.trackedRosterId),
        userNextPick: clock.nextUserPick?.pickNo ?? null,
        picksUntilUser: clock.picksBeforeUser,
      },
      recommendation: recommendation ? {
        status: 'fallback',
        player: playerView(recommendation.player),
        strength: recommendation.strength,
        primaryReason: recommendation.primaryReason,
        reasons: recommendation.reasons,
        backups: recommendation.backups.map(playerView),
        generatedAt: recommendation.generatedAt,
        modelVersion: recommendation.modelVersion,
      } : null,
      recentPicks: state.picks.slice(-8).reverse().map((pick) => {
        const player = getPlayer(pick.playerId, pick.metadata);
        return {
          pickNumber: pick.pickNo,
          label: `${pick.round}.${String(pick.pickInRound).padStart(2, '0')}`,
          rosterName: participantName(pick.pickedByRosterId),
          player: playerView(player),
          isUserPick: pick.pickedByRosterId === state.trackedRosterId,
          isTradedPick: pick.originalRosterId !== pick.pickedByRosterId,
        };
      }),
      userRoster: state.trackedRosterId ? {
        name: participantName(state.trackedRosterId),
        players: userPicks.map((pick) => playerView(getPlayer(pick.playerId, pick.metadata))),
        positionCounts,
      } : null,
      teamsBeforeNextPick: [...teamsAheadMap.entries()].map(([rosterId, pickNumbers]) => ({
        name: participantName(rosterId),
        pickNumbers,
      })),
      persistence: { saved: false },
    };

    if (recommendation) {
      await saveDraftSnapshot({
        draftId,
        userId,
        username,
        leagueId: draft.league_id ? String(draft.league_id) : null,
        leagueName: view.draft.leagueName,
        status: draft.status,
        boardHash,
        state: view,
        picks: state.picks,
        recommendation,
      });
      view.persistence.saved = true;
    }

    return Response.json(view, { headers: { 'cache-control': 'no-store' } });
  } catch (error) {
    if (error instanceof SleeperApiError && error.status === 404) {
      return Response.json({ error: 'That Sleeper draft is no longer available.' }, { status: 404 });
    }
    console.error('Live draft refresh failed', error);
    return Response.json({ error: 'The live draft could not be refreshed. Your last view is still safe to use.' }, { status: 502 });
  }
}
