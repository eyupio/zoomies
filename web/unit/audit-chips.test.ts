import { test } from 'node:test';
import assert from 'node:assert/strict';
import { auditEyebrow, auditKindChips } from '../src/lib/audit/kind-chips.ts';

test('the kind chips are the kinds the rows have shown, in order, and the one in force even when the rows have not', () => {
  // A log narrowed to pools by a pasted address shows only pool rows, so the
  // rows alone would never name the kind that is pressed.
  assert.deepEqual(auditKindChips(['host', 'pool'], ''), [
    { value: 'host', label: 'host' },
    { value: 'pool', label: 'pool' },
  ]);
  assert.deepEqual(auditKindChips(['pool'], 'pool'), [{ value: 'pool', label: 'pool' }]);
  assert.deepEqual(auditKindChips(['pool'], 'runner'), [
    { value: 'pool', label: 'pool' },
    { value: 'runner', label: 'runner' },
  ]);
});

test('the eyebrow is how many events the filters leave, or nothing until counted or with none', () => {
  // The order is the grid's to say: When, Actor and Action all sort, so a
  // card that promised newest first would be wrong one click later. An
  // empty result is the empty state's to explain, as on Pools.
  assert.equal(auditEyebrow(1), '1 event');
  assert.equal(auditEyebrow(240), '240 events');
  assert.equal(auditEyebrow(0), undefined);
  assert.equal(auditEyebrow(undefined), undefined);
});
