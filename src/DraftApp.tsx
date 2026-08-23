import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { DraftCandidateView, LiveDraftViewModel, PlayerView } from './types';

const demo: LiveDraftViewModel = {
  draft: {
    id: 'demo-draft',
    leagueName: 'Home League',
    formatLabel: '12-team · half-PPR · snake',
    teamCount: 12,
    round: 5,
    pickInRound: 7,
    overallPick: 55,
    status: 'drafting',
    stateVersion: 'demo-54',
    sleeperDraftUrl: 'https://sleeper.com/',
  },
  connection: { state: 'live', lastSyncedAt: new Date().toISOString() },
  turn: {
    state: 'on-clock',
    currentRosterName: 'Your Team',
    userRosterName: 'Your Team',
    userNextPick: 55,
    picksUntilUser: 0,
  },
  recommendation: {
    status: 'fallback',
    player: { id: 'demo-kittle', name: 'George Kittle', position: 'TE', nflTeam: 'SF', injuryStatus: null },
    strength: 'high',
    primaryReason: 'Fills your open tight-end starter slot before a sharp drop in the current tier.',
    reasons: [
      { label: 'Roster fit', detail: 'This fills your open starting tight-end slot.' },
      { label: 'Opponent pressure', detail: 'Three teams in the next draft window still need a tight end.' },
      { label: 'Tier edge', detail: 'The next available tight end is meaningfully lower in the current player order.' },
    ],
    backups: [
      { id: 'demo-rb', name: 'James Cook', position: 'RB', nflTeam: 'BUF', injuryStatus: null },
      { id: 'demo-wr', name: 'DeVonta Smith', position: 'WR', nflTeam: 'PHI', injuryStatus: null },
    ],
    generatedAt: new Date().toISOString(),
    modelVersion: 'baseline-0.1',
  },
  recentPicks: [
    { pickNumber: 54, label: '5.06', rosterName: 'Sunday Scaries', player: { id: 'd1', name: 'DK Metcalf', position: 'WR', nflTeam: 'PIT', injuryStatus: null }, isUserPick: false, isTradedPick: false },
    { pickNumber: 53, label: '5.05', rosterName: 'Fourth & Long', player: { id: 'd2', name: 'David Montgomery', position: 'RB', nflTeam: 'DET', injuryStatus: null }, isUserPick: false, isTradedPick: false },
    { pickNumber: 52, label: '5.04', rosterName: 'Gridiron Theory', player: { id: 'd3', name: 'Joe Burrow', position: 'QB', nflTeam: 'CIN', injuryStatus: null }, isUserPick: false, isTradedPick: true },
  ],
  userRoster: {
    name: 'Your Team',
    players: [
      { id: 'r1', name: 'Amon-Ra St. Brown', position: 'WR', nflTeam: 'DET', injuryStatus: null },
      { id: 'r2', name: 'Jonathan Taylor', position: 'RB', nflTeam: 'IND', injuryStatus: null },
      { id: 'r3', name: 'Drake London', position: 'WR', nflTeam: 'ATL', injuryStatus: null },
      { id: 'r4', name: 'Josh Jacobs', position: 'RB', nflTeam: 'GB', injuryStatus: null },
    ],
    positionCounts: { QB: 0, RB: 2, WR: 2, TE: 0 },
  },
  teamsBeforeNextPick: [
    { name: 'Team Eight', pickNumbers: [56, 65] },
    { name: 'Team Ten', pickNumbers: [58, 63] },
    { name: 'Sunday Scaries', pickNumbers: [60, 61] },
  ],
  persistence: { saved: true },
};

type Connection = { username: string; userId: string; candidate: DraftCandidateView };

function PositionBadge({ player }: { player: PlayerView }) {
  return (
    <span className="inline-flex items-center rounded-md bg-[#e8ff71] px-2 py-1 text-[10px] font-black text-[#15231d]" aria-label={`${player.position}, ${player.nflTeam ?? 'free agent'}`}>
      {player.position} · {player.nflTeam ?? 'FA'}
    </span>
  );
}

function ConnectScreen({ onPreview, onConnected }: {
  onPreview: () => void;
  onConnected: (connection: Connection) => void;
}) {
  const [username, setUsername] = useState('');
  const [draftInput, setDraftInput] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [discovery, setDiscovery] = useState<{ userId: string; candidates: DraftCandidateView[] } | null>(null);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setLoading(true);
    setError('');
    try {
      const response = await fetch('/api/discover', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ username, draftInput }),
      });
      const result = await response.json() as { userId?: string; candidates?: DraftCandidateView[]; error?: string };
      if (!response.ok || !result.userId) throw new Error(result.error || 'Draft discovery failed.');
      const next = { userId: result.userId, candidates: result.candidates ?? [] };
      setDiscovery(next);
      if (next.candidates.length === 1) onConnected({ username, userId: next.userId, candidate: next.candidates[0] });
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Draft discovery failed.');
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="min-h-[calc(100vh-73px)] bg-[#f3f1eb] px-5 py-10 sm:px-8 sm:py-16">
      <div className="mx-auto grid max-w-6xl items-center gap-10 lg:grid-cols-[1.05fr_.95fr]">
        <section>
          <p className="text-xs font-black uppercase tracking-[0.2em] text-[#5b7468]">Built for the clock</p>
          <h1 className="mt-4 max-w-3xl text-5xl font-semibold leading-[0.94] tracking-[-0.065em] text-[#13251f] sm:text-7xl">
            One pick.<br />Right when it matters.
          </h1>
          <p className="mt-6 max-w-xl text-lg leading-8 text-[#5d6d65]">
            Connect a Sleeper draft. Draftside watches every roster, tracks your place in the order, and keeps one tactical recommendation ready.
          </p>
          <div className="mt-8 flex flex-wrap gap-3 text-sm text-[#3f554a]">
            {['Live pick tracking', 'Snake-order aware', 'Saved decisions'].map((item) => (
              <span key={item} className="rounded-full border border-black/10 bg-white px-4 py-2">{item}</span>
            ))}
          </div>
        </section>

        <section className="rounded-[28px] border border-black/10 bg-white p-6 shadow-[0_24px_70px_rgb(23_47_38/12%)] sm:p-8" aria-labelledby="connect-title">
          <p className="text-xs font-black uppercase tracking-[0.18em] text-[#648074]">Enter the room</p>
          <h2 id="connect-title" className="mt-2 text-2xl font-semibold tracking-[-0.04em]">Connect to Sleeper</h2>
          <p className="mt-2 text-sm leading-6 text-[#6a7972]">No password needed. Sleeper’s draft data is read-only.</p>
          <form onSubmit={submit} className="mt-6 space-y-4">
            <label className="block">
              <span className="mb-2 block text-xs font-bold uppercase tracking-[0.1em] text-[#52655c]">Sleeper username</span>
              <input value={username} onChange={(event) => setUsername(event.target.value)} required autoComplete="username" placeholder="your_username" className="h-12 w-full rounded-xl border border-black/15 bg-[#faf9f6] px-4 outline-none transition focus:border-[#2f6f55] focus:ring-4 focus:ring-[#73e2a7]/20" />
            </label>
            <label className="block">
              <span className="mb-2 block text-xs font-bold uppercase tracking-[0.1em] text-[#52655c]">League or draft ID / URL <em className="font-normal normal-case text-[#849087]">optional</em></span>
              <input value={draftInput} onChange={(event) => setDraftInput(event.target.value)} placeholder="Paste either Sleeper link or ID" className="h-12 w-full rounded-xl border border-black/15 bg-[#faf9f6] px-4 outline-none transition focus:border-[#2f6f55] focus:ring-4 focus:ring-[#73e2a7]/20" />
            </label>
            {error && <p role="alert" className="rounded-xl bg-[#fff0eb] px-4 py-3 text-sm font-medium text-[#9a3927]">{error}</p>}
            <button disabled={loading} className="min-h-12 w-full rounded-xl bg-[#17372b] px-5 py-3 font-bold text-white transition hover:bg-[#204b3b] focus:outline-none focus:ring-4 focus:ring-[#73e2a7]/35 disabled:opacity-60">
              {loading ? 'Finding your drafts…' : 'Find my draft'}
            </button>
          </form>
          {discovery && discovery.candidates.length === 0 && (
            <p className="mt-4 rounded-xl bg-[#f3f1eb] p-4 text-sm">No current drafts were found. Paste a league or draft link, or try the sample room.</p>
          )}
          {discovery && discovery.candidates.length > 1 && (
            <div className="mt-5" aria-label="Choose a draft">
              <p className="mb-2 text-xs font-bold uppercase tracking-[0.1em] text-[#52655c]">Choose a draft</p>
              <div className="space-y-2">
                {discovery.candidates.slice(0, 6).map((candidate) => (
                  <button key={candidate.draftId} onClick={() => onConnected({ username, userId: discovery.userId, candidate })} className="flex min-h-14 w-full items-center justify-between rounded-xl border border-black/10 px-4 text-left hover:border-[#2f6f55] focus:outline-none focus:ring-4 focus:ring-[#73e2a7]/25">
                    <span><strong className="block text-sm">{candidate.leagueName}</strong><span className="text-xs text-[#718078]">{candidate.teamCount} teams · {candidate.type}</span></span>
                    <span className="rounded-full bg-[#edf1ed] px-2 py-1 text-[10px] font-bold uppercase">{candidate.status.replace('_', ' ')}</span>
                  </button>
                ))}
              </div>
            </div>
          )}
          <button onClick={onPreview} className="mt-4 min-h-11 w-full rounded-xl px-4 text-sm font-semibold text-[#3c5a4c] underline decoration-[#9eb4a8] underline-offset-4 focus:outline-none focus:ring-4 focus:ring-[#73e2a7]/25">
            Explore a sample live room
          </button>
        </section>
      </div>
    </main>
  );
}

function LiveRoom({ view, isDemo, error, onExit }: {
  view: LiveDraftViewModel;
  isDemo: boolean;
  error: string;
  onExit: () => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const recommendation = view.recommendation;
  const connectionIsLive = view.connection.state === 'live';
  const connectionIsOffline = view.connection.state === 'offline';
  const staleBoardMessage = error
    ? `${error} Showing the last confirmed board.`
    : connectionIsOffline
      ? 'Sleeper is currently unavailable. Showing the last confirmed board.'
      : 'Sleeper updates are delayed. Showing the last confirmed board.';
  const turnCopy = view.turn.state === 'on-clock'
    ? 'You’re on the clock'
    : view.turn.state === 'on-deck'
      ? 'You’re on deck'
      : view.turn.state === 'complete'
        ? 'Draft complete'
        : view.turn.state === 'spectating'
          ? 'Spectating this draft'
          : `${view.turn.picksUntilUser ?? '—'} picks until your turn`;
  const positionRows = ['QB', 'RB', 'WR', 'TE'];

  return (
    <main className="min-h-[calc(100vh-73px)] bg-[#f3f1eb] px-4 py-4 sm:px-6 lg:px-8 lg:py-7">
      <div className="mx-auto max-w-[1440px]">
        <section className={`mb-4 flex flex-wrap items-center justify-between gap-3 rounded-2xl border px-4 py-3 ${view.turn.state === 'on-clock' ? 'border-[#b1c948] bg-[#e8ff71]' : 'border-black/10 bg-white'}`} role={view.turn.state === 'on-clock' ? 'alert' : 'status'}>
          <div>
            <p className="text-[10px] font-black uppercase tracking-[0.16em] text-[#587064]">Round {view.draft.round} · Pick {view.draft.round}.{String(view.draft.pickInRound).padStart(2, '0')}</p>
            <h1 className="text-xl font-semibold tracking-[-0.035em] sm:text-2xl">{turnCopy}</h1>
          </div>
          <div className="flex items-center gap-2">
            {isDemo && <span className="rounded-full bg-[#172d25] px-3 py-1.5 text-[10px] font-black uppercase tracking-[0.1em] text-white">Sample data</span>}
            <span className="flex items-center gap-2 rounded-full bg-white/65 px-3 py-2 text-xs font-bold">
              <span className={`h-2 w-2 rounded-full ${error || !connectionIsLive ? connectionIsOffline ? 'bg-[#c8493a]' : 'bg-[#df8c32]' : 'bg-[#42a874]'}`} />
              {error || !connectionIsLive ? connectionIsOffline ? 'Offline' : 'Update delayed' : 'Live'}
            </span>
          </div>
        </section>

        {(error || !connectionIsLive) && <p role="alert" className="mb-4 rounded-xl bg-[#fff2dc] px-4 py-3 text-sm font-medium text-[#81531e]">{staleBoardMessage}</p>}

        <div className="grid gap-4 lg:grid-cols-[260px_minmax(420px,1fr)_280px]">
          <aside className="order-2 rounded-[22px] border border-black/10 bg-white p-5 lg:order-1" aria-labelledby="recent-title">
            <div className="flex items-center justify-between">
              <div><p className="text-[10px] font-black uppercase tracking-[0.16em] text-[#698078]">Draft pulse</p><h2 id="recent-title" className="mt-1 font-semibold">Recent picks</h2></div>
              <span className="font-mono text-xs text-[#75837c]">#{view.draft.overallPick}</span>
            </div>
            <ol className="mt-4 divide-y divide-black/8">
              {view.recentPicks.map((pick) => (
                <li key={`${pick.pickNumber}-${pick.player.id}`} className="grid grid-cols-[40px_1fr] gap-3 py-3">
                  <span className="pt-0.5 font-mono text-[11px] font-semibold text-[#77877f]">{pick.label}</span>
                  <div className="min-w-0">
                    <p className="truncate text-sm font-semibold">{pick.player.name}</p>
                    <p className="truncate text-xs text-[#748179]">{pick.rosterName} · {pick.player.position}{pick.isTradedPick ? ' · traded pick' : ''}</p>
                  </div>
                </li>
              ))}
              {!view.recentPicks.length && <li className="py-5 text-sm text-[#718078]">Waiting for the first selection.</li>}
            </ol>
          </aside>

          <section className={`order-1 overflow-hidden rounded-[28px] bg-[#18372b] text-white shadow-[0_22px_60px_rgb(19_47_37/18%)] lg:order-2 ${view.turn.state === 'on-clock' ? 'ring-4 ring-[#e8ff71]/70' : ''}`} aria-labelledby="recommendation-title">
            <div className="flex items-center justify-between border-b border-white/10 px-6 py-4 sm:px-8">
              <p className="text-[11px] font-black uppercase tracking-[0.18em] text-[#9bc6b2]">Recommended pick</p>
              {recommendation && <span className="rounded-full bg-white/8 px-3 py-1 text-[10px] font-bold uppercase tracking-[0.08em] text-[#b8d5c7]">{recommendation.strength} signal</span>}
            </div>
            {recommendation ? (
              <div className="px-6 py-7 sm:px-8 sm:py-9">
                <PositionBadge player={recommendation.player} />
                <h2 id="recommendation-title" className="mt-4 text-5xl font-semibold leading-[0.92] tracking-[-0.065em] sm:text-7xl">{recommendation.player.name}</h2>
                <p className="mt-6 max-w-2xl text-base leading-7 text-white/72">{recommendation.primaryReason}</p>
                {recommendation.player.injuryStatus && (
                  <p className="mt-4 rounded-xl bg-[#fff2dc] px-4 py-3 text-sm font-semibold text-[#6f4318]">Sleeper injury status: {recommendation.player.injuryStatus}</p>
                )}
                <div className="mt-7 flex flex-col gap-3 border-t border-white/12 pt-6 sm:flex-row">
                  <a href={view.draft.sleeperDraftUrl} target="_blank" rel="noreferrer" className="inline-flex min-h-12 flex-1 items-center justify-center rounded-xl bg-[#e8ff71] px-5 font-black text-[#17271f] transition hover:bg-[#f0ff9b] focus:outline-none focus:ring-4 focus:ring-white/35">
                    Open Sleeper to draft
                  </a>
                  <button onClick={() => setExpanded((value) => !value)} aria-expanded={expanded} aria-controls="recommendation-details" className="min-h-12 rounded-xl border border-white/20 px-5 font-bold text-white hover:bg-white/8 focus:outline-none focus:ring-4 focus:ring-white/20">
                    {expanded ? 'Hide reasoning' : 'Why this pick?'}
                  </button>
                </div>
                {expanded && (
                  <div id="recommendation-details" className="mt-5 grid gap-3 rounded-2xl bg-white/7 p-4 sm:grid-cols-3">
                    {recommendation.reasons.map((reason) => (
                      <div key={reason.label}><p className="text-xs font-black uppercase tracking-[0.1em] text-[#b8d5c7]">{reason.label}</p><p className="mt-2 text-sm leading-6 text-white/70">{reason.detail}</p></div>
                    ))}
                  </div>
                )}
                <p className="mt-5 text-[11px] text-white/42">Live decision model · {recommendation.modelVersion} · availability must be verified in Sleeper</p>
                {recommendation.simulation && (
                  <p className="mt-2 text-[11px] text-white/42">
                    {recommendation.simulation.sampleCount} draft-room scenarios · {Math.round(recommendation.simulation.confidence * 100)}% top outcome
                    {recommendation.simulation.followingPickNo ? ` · simulated through pick ${recommendation.simulation.followingPickNo}` : ''}
                  </p>
                )}
              </div>
            ) : (
              <div className="px-6 py-16 text-center sm:px-8"><h2 id="recommendation-title" className="text-2xl font-semibold">Calculating the board</h2><p className="mt-2 text-sm text-white/60">A safe recommendation will appear when the player pool is ready.</p></div>
            )}
          </section>

          <aside className="order-3 rounded-[22px] border border-black/10 bg-white p-5" aria-labelledby="roster-title">
            <p className="text-[10px] font-black uppercase tracking-[0.16em] text-[#698078]">Roster context</p>
            <h2 id="roster-title" className="mt-1 font-semibold">{view.userRoster?.name ?? 'Your roster'}</h2>
            <div className="mt-4 grid grid-cols-4 gap-2 lg:grid-cols-2">
              {positionRows.map((position) => (
                <div key={position} className="rounded-xl bg-[#f3f1eb] p-3"><p className="text-[10px] font-black text-[#78857f]">{position}</p><p className="mt-1 font-mono text-lg font-bold">{view.userRoster?.positionCounts[position] ?? 0}</p></div>
              ))}
            </div>
            <div className="mt-5 border-t border-black/8 pt-4">
              <p className="text-xs font-black uppercase tracking-[0.1em] text-[#64766d]">Teams in the window</p>
              <ul className="mt-3 space-y-3">
                {view.teamsBeforeNextPick.slice(0, 5).map((team) => (
                  <li key={team.name} className="flex items-center justify-between gap-2 text-sm"><span className="truncate font-medium">{team.name}</span><span className="shrink-0 font-mono text-[10px] text-[#78857f]">{team.pickNumbers.length} pick{team.pickNumbers.length === 1 ? '' : 's'}</span></li>
                ))}
                {!view.teamsBeforeNextPick.length && <li className="text-sm text-[#78857f]">No opposing picks before your selection.</li>}
              </ul>
            </div>
          </aside>
        </div>

        <footer className="mt-4 flex flex-wrap items-center justify-between gap-2 px-1 text-[11px] text-[#6c7b74]">
          <p>Synced {new Date(view.connection.lastSyncedAt).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit', second: '2-digit' })} · {view.persistence.saved ? 'Decision saved' : 'Saving pending'}</p>
          <button onClick={onExit} className="min-h-11 px-2 font-bold underline decoration-[#9eaaa4] underline-offset-4 focus:outline-none focus:ring-4 focus:ring-[#73e2a7]/25">Leave draft room</button>
        </footer>
      </div>
    </main>
  );
}

export default function DraftApp() {
  const [connection, setConnection] = useState<Connection | null>(null);
  const [view, setView] = useState<LiveDraftViewModel | null>(null);
  const [isDemo, setIsDemo] = useState(false);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const requestSequence = useRef(0);
  const activeRequest = useRef<{ key: string; controller: AbortController } | null>(null);

  const refresh = useCallback(async (nextConnection: Connection) => {
    const requestKey = `${nextConnection.candidate.draftId}:${nextConnection.userId}`;
    const active = activeRequest.current;
    if (active?.key === requestKey) return;
    active?.controller.abort();
    const sequence = ++requestSequence.current;
    const controller = new AbortController();
    activeRequest.current = { key: requestKey, controller };
    try {
      const params = new URLSearchParams({
        draftId: nextConnection.candidate.draftId,
        userId: nextConnection.userId,
        username: nextConnection.username,
      });
      const response = await fetch(`/api/draft?${params}`, { cache: 'no-store', signal: controller.signal });
      const result = await response.json() as LiveDraftViewModel & { error?: string };
      if (!response.ok) throw new Error(result.error || 'Draft refresh failed.');
      if (sequence !== requestSequence.current || activeRequest.current?.controller !== controller) return;
      setView(result);
      setError('');
    } catch (caught) {
      if (controller.signal.aborted || sequence !== requestSequence.current || activeRequest.current?.controller !== controller) return;
      setError(caught instanceof Error ? caught.message : 'Draft refresh failed.');
    } finally {
      if (activeRequest.current?.controller === controller) {
        activeRequest.current = null;
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    if (!connection || isDemo) return;
    const requestKey = `${connection.candidate.draftId}:${connection.userId}`;
    const timer = window.setInterval(() => void refresh(connection), 2000);
    return () => {
      window.clearInterval(timer);
      if (activeRequest.current?.key === requestKey) {
        activeRequest.current.controller.abort();
        activeRequest.current = null;
      }
    };
  }, [connection, isDemo, refresh]);

  const body = useMemo(() => {
    if (isDemo) return <LiveRoom view={demo} isDemo error="" onExit={() => { setIsDemo(false); setView(null); }} />;
    if (view) return <LiveRoom view={view} isDemo={false} error={error} onExit={() => { setConnection(null); setView(null); setError(''); }} />;
    if (connection && loading) {
      return <main className="grid min-h-[calc(100vh-73px)] place-items-center bg-[#f3f1eb] px-5"><div role="status" className="text-center"><span className="mx-auto block h-3 w-3 rounded-full bg-[#47a875] shadow-[0_0_0_8px_rgb(71_168_117/14%)]" /><h1 className="mt-6 text-2xl font-semibold tracking-[-0.04em]">Building the live board</h1><p className="mt-2 text-sm text-[#6d7b74]">Loading players, picks, rosters, and your draft position.</p></div></main>;
    }
    return <ConnectScreen onPreview={() => setIsDemo(true)} onConnected={(next) => {
      setConnection(next);
      setLoading(true);
      void refresh(next);
    }} />;
  }, [connection, error, isDemo, loading, refresh, view]);

  return (
    <div className="min-h-screen bg-[#f3f1eb] text-[#16221d]">
      <header className="border-b border-white/10 bg-[#13251f] text-white">
        <div className="mx-auto flex h-[73px] max-w-[1440px] items-center justify-between px-5 sm:px-8">
          <button onClick={() => { setConnection(null); setView(null); setIsDemo(false); }} className="text-left focus:outline-none focus:ring-4 focus:ring-[#73e2a7]/30">
            <p className="text-sm font-black uppercase tracking-[0.22em] text-[#e8ff71]">Draftside</p>
            <p className="text-[11px] text-white/55">Live decision room</p>
          </button>
          <p className="hidden text-xs text-white/55 sm:block">Read-only companion for Sleeper drafts</p>
        </div>
      </header>
      {body}
    </div>
  );
}
