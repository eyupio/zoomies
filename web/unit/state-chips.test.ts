import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  liveRunners,
  runnerStateChips,
  runnersEyebrow,
  toggleChip,
} from '../src/lib/runners/state-chips.ts';

const stats = { provisioning: 1, registering: 1, idle: 3, busy: 3, draining: 1, failed: 1 };

test('the chips are the six states in order, removed left out, with counts when counted', () => {
  const chips = runnerStateChips(stats, false, true);
  assert.deepEqual(
    chips.map((chip) => chip.value),
    ['provisioning', 'registering', 'idle', 'busy', 'draining', 'failed'],
  );
  assert.deepEqual(
    chips.map((chip) => chip.label),
    ['Provisioning', 'Registering', 'Idle', 'Busy', 'Draining', 'Failed'],
  );
  assert.deepEqual(
    chips.map((chip) => chip.count),
    [1, 1, 3, 3, 1, 1],
  );
});

test("a pool or host filter takes the counts away, since the stats are the fleet's", () => {
  // A count beside a chip would disagree with the rows under it, which are
  // one pool's, so the chips say nothing rather than something wrong.
  for (const chip of runnerStateChips(stats, false, false)) assert.equal(chip.count, undefined);
});

test("the quirky setting on gives the kennel's words", () => {
  assert.equal(runnerStateChips(stats, true, true)[0]!.label, 'Walkies!');
});

test('no stats means no eyebrow and chips with no counts, never undefined on the page', () => {
  assert.equal(runnersEyebrow(undefined), undefined);
  assert.equal(liveRunners(undefined), undefined);
  for (const chip of runnerStateChips(undefined, false, true)) {
    assert.equal('count' in chip, false);
  }
});

test('the eyebrow counts known and live', () => {
  assert.equal(runnersEyebrow({ total: 11, ...stats }), '11 known, 9 live');
  assert.equal(liveRunners(stats), 9);
});

test('toggling a chip adds it, toggling again removes it, and All is the empty selection', () => {
  assert.deepEqual(toggleChip([], 'busy'), ['busy']);
  assert.deepEqual(toggleChip(['busy'], 'idle'), ['busy', 'idle']);
  assert.deepEqual(toggleChip(['busy', 'idle'], 'busy'), ['idle']);
});
