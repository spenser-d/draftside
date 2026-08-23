import assert from 'node:assert/strict';
import test from 'node:test';
import { plannedPick } from '../lib/domain/order.ts';
import { buildDraftState, currentPick, draftClock, hasBoardGap } from '../lib/domain/state.ts';
import type { DraftConfig, DraftParticipant } from '../lib/domain/types.ts';

const participants: DraftParticipant[] = Array.from({ length: 4 }, (_, index) => ({
  slot: index + 1,
  rosterId: String(index + 1),
  userIds: [`user-${index + 1}`],
  displayName: `Team ${index + 1}`,
}));

function config(format: DraftConfig['format']): DraftConfig {
  return { draftId: 'draft-1', teamCount: 4, roundCount: 4, format };
}

test('snake order reverses every other round', () => {
  const slots = Array.from({ length: 12 }, (_, index) => plannedPick(config('snake'), participants, index + 1).originSlot);
  assert.deepEqual(slots, [1, 2, 3, 4, 4, 3, 2, 1, 1, 2, 3, 4]);
});

test('linear order remains forward', () => {
  const slots = Array.from({ length: 8 }, (_, index) => plannedPick(config('linear'), participants, index + 1).originSlot);
  assert.deepEqual(slots, [1, 2, 3, 4, 1, 2, 3, 4]);
});

test('third-round reversal keeps round three reversed', () => {
  const slots = Array.from({ length: 16 }, (_, index) => plannedPick(config('third_round_reversal'), participants, index + 1).originSlot);
  assert.deepEqual(slots, [1, 2, 3, 4, 4, 3, 2, 1, 4, 3, 2, 1, 1, 2, 3, 4]);
});

test('current pick is the first hole, not picks length plus one', () => {
  const state = buildDraftState({
    config: config('snake'),
    status: 'drafting',
    season: '2026',
    participants,
    trackedUserId: 'user-3',
    remotePicks: [
      { pickNo: 1, playerId: 'p1', rosterId: '1', pickedByUserId: 'user-1', isKeeper: false, metadata: {} },
      { pickNo: 3, playerId: 'keeper', rosterId: '3', pickedByUserId: 'user-3', isKeeper: true, metadata: {} },
    ],
    tradedPicks: [],
  });
  assert.equal(currentPick(state)?.pickNo, 2);
  assert.equal(hasBoardGap(state), true);
  assert.equal(draftClock(state).recommendationSafe, false);
});

test('traded pick changes the on-clock roster without changing its slot', () => {
  const state = buildDraftState({
    config: config('snake'),
    status: 'drafting',
    season: '2026',
    participants,
    trackedUserId: 'user-2',
    remotePicks: [],
    tradedPicks: [{ season: '2026', round: 1, originalRosterId: '1', ownerRosterId: '2' }],
  });
  assert.equal(currentPick(state)?.originSlot, 1);
  assert.equal(currentPick(state)?.ownerRosterId, '2');
  assert.equal(draftClock(state).isUserOnClock, true);
});

test('duplicate player selections are rejected', () => {
  assert.throws(() => buildDraftState({
    config: config('snake'),
    status: 'drafting',
    season: '2026',
    participants,
    trackedUserId: 'user-1',
    remotePicks: [
      { pickNo: 1, playerId: 'same', rosterId: '1', pickedByUserId: 'user-1', isKeeper: false, metadata: {} },
      { pickNo: 2, playerId: 'same', rosterId: '2', pickedByUserId: 'user-2', isKeeper: false, metadata: {} },
    ],
    tradedPicks: [],
  }), /drafted twice/);
});
