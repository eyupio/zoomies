import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  ATTENTION_SHOWN,
  OVERVIEW_PANELS,
  POOLS_SHOWN,
  cap,
  visiblePanels,
} from '../src/lib/overview/panels.ts';

test('every panel has a unique id and a side panel follows a main one', () => {
  const ids = OVERVIEW_PANELS.map((p) => p.id);
  assert.equal(new Set(ids).size, ids.length);
  OVERVIEW_PANELS.forEach((p, i) => {
    if (p.place === 'side')
      assert.equal(OVERVIEW_PANELS[i - 1]?.place, 'main', `${p.id} has no main panel before it`);
  });
  assert.deepEqual(ids, [
    'attention',
    'pools',
    'failures',
    'jobs',
    'events',
    'history',
    'diagnostics',
  ]);
});

test('the attention card and the failures stand down during the first run', () => {
  const ids = visiblePanels(OVERVIEW_PANELS, { setupPending: true }).map((p) => p.id);
  assert.deepEqual(ids, ['pools', 'jobs', 'events', 'history', 'diagnostics']);
  assert.equal(
    visiblePanels(OVERVIEW_PANELS, { setupPending: false }).length,
    OVERVIEW_PANELS.length,
  );
});

test('a cap keeps the first few in the order given and counts the rest', () => {
  assert.deepEqual(cap([1, 2, 3, 4, 5], 3), { shown: [1, 2, 3], more: 2 });
  assert.deepEqual(cap([1, 2, 3], 3), { shown: [1, 2, 3], more: 0 });
  assert.deepEqual(cap([], 3), { shown: [], more: 0 });
  assert.equal(ATTENTION_SHOWN, 3);
  assert.equal(POOLS_SHOWN, 6);
});
