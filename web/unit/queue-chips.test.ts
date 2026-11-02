import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  queueChipSelection,
  queueEyebrow,
  queueStatusChips,
  queueTotal,
} from '../src/lib/queue/status-chips.ts';

const counts = { ready: 12, expedited: 2, paused: 3, deleted: 1 };

test('the chips are the four statuses in the product order, each with its count', () => {
  const chips = queueStatusChips(counts);
  assert.deepEqual(
    chips.map((chip) => chip.value),
    ['ready', 'expedited', 'paused', 'deleted'],
  );
  assert.deepEqual(
    chips.map((chip) => chip.label),
    ['Ready', 'Run now', 'Paused', 'Removed'],
  );
  assert.deepEqual(
    chips.map((chip) => chip.count),
    [12, 2, 3, 1],
  );
});

test('before the first page has answered the chips carry no count', () => {
  assert.deepEqual(
    queueStatusChips({}).map((chip) => chip.count),
    [undefined, undefined, undefined, undefined],
  );
});

test('every status in force is All, and anything less is the statuses themselves', () => {
  // The page writes all four statuses out when nothing narrows, the way the
  // Jobs page's All button does, so the chips have to read that spelling back
  // as the All chip rather than as four pressed chips.
  assert.deepEqual(queueChipSelection(['ready', 'expedited', 'paused', 'deleted']), []);
  assert.deepEqual(queueChipSelection(['deleted', 'paused', 'ready', 'expedited']), []);
  assert.deepEqual(queueChipSelection(['ready', 'expedited', 'paused']), [
    'ready',
    'expedited',
    'paused',
  ]);
  assert.deepEqual(queueChipSelection(['paused']), ['paused']);
});

test('the eyebrow is the active demand and what is on hold, or nothing until counted', () => {
  assert.equal(queueEyebrow(counts), '14 active demand, 3 on hold');
  assert.equal(
    queueEyebrow({ ready: 0, expedited: 0, paused: 0, deleted: 0 }),
    '0 active demand, 0 on hold',
  );
  assert.equal(queueEyebrow({}), undefined);
});

test('the All chip counts every status, removed included', () => {
  assert.equal(queueTotal(counts), 18);
  assert.equal(queueTotal({}), undefined);
});
