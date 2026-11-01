import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  ATTENTION_SHOWN,
  DEFAULT_LAYOUT,
  OVERVIEW_PANELS,
  POOLS_SHOWN,
  cap,
  hiddenPanels,
  isDefaultLayout,
  movePanel,
  panelRows,
  placePanel,
  readPanelLayout,
  setHidden,
  visibleLayout,
  type PanelLayout,
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

test('the attention card, the failures and History stand down during the first run', () => {
  const ids = visibleLayout(OVERVIEW_PANELS, DEFAULT_LAYOUT, { setupPending: true }).map(
    (p) => p.id,
  );
  assert.deepEqual(ids, ['pools', 'jobs', 'events', 'diagnostics']);
  assert.equal(
    visibleLayout(OVERVIEW_PANELS, DEFAULT_LAYOUT, { setupPending: false }).length,
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

test('a saved layout is read forgivingly: unknown ids dropped, missing panels appended, duplicates collapsed', () => {
  const layout = readPanelLayout(
    { order: ['pools', 'nope', 'attention', 'pools'], hidden: ['history', 'nope'] },
    OVERVIEW_PANELS,
  );
  assert.deepEqual(layout.order, [
    'pools',
    'attention',
    'failures',
    'jobs',
    'events',
    'history',
    'diagnostics',
  ]);
  assert.deepEqual(layout.hidden, ['history']);
  assert.deepEqual(readPanelLayout(undefined, OVERVIEW_PANELS), DEFAULT_LAYOUT);
  assert.deepEqual(readPanelLayout({ order: 'pools' }, OVERVIEW_PANELS), DEFAULT_LAYOUT);
});

test('the visible layout applies the order, drops the hidden, and the first run still wins', () => {
  const layout: PanelLayout = {
    order: ['history', 'attention', 'pools', 'failures', 'jobs', 'events', 'diagnostics'],
    hidden: ['events'],
  };
  assert.deepEqual(
    visibleLayout(OVERVIEW_PANELS, layout, { setupPending: false }).map((p) => p.id),
    ['history', 'attention', 'pools', 'failures', 'jobs', 'diagnostics'],
  );
  assert.deepEqual(
    visibleLayout(OVERVIEW_PANELS, layout, { setupPending: true }).map((p) => p.id),
    ['pools', 'jobs', 'diagnostics'],
  );
  assert.deepEqual(
    hiddenPanels(OVERVIEW_PANELS, layout, { setupPending: false }).map((p) => p.id),
    ['events'],
  );
});

test('a move swaps neighbours and stops at the ends; a place puts a panel before another or last', () => {
  assert.deepEqual(movePanel(DEFAULT_LAYOUT, 'pools', -1).order.slice(0, 2), [
    'pools',
    'attention',
  ]);
  assert.deepEqual(movePanel(DEFAULT_LAYOUT, 'attention', -1), DEFAULT_LAYOUT);
  assert.deepEqual(movePanel(DEFAULT_LAYOUT, 'diagnostics', 1), DEFAULT_LAYOUT);
  assert.deepEqual(placePanel(DEFAULT_LAYOUT, 'diagnostics', 'attention').order[0], 'diagnostics');
  assert.deepEqual(placePanel(DEFAULT_LAYOUT, 'attention', null).order.at(-1), 'attention');
  assert.deepEqual(setHidden(DEFAULT_LAYOUT, 'pools', true).hidden, ['pools']);
  assert.deepEqual(
    setHidden(setHidden(DEFAULT_LAYOUT, 'pools', true), 'pools', false),
    DEFAULT_LAYOUT,
  );
  assert.equal(isDefaultLayout(DEFAULT_LAYOUT), true);
  assert.equal(isDefaultLayout(setHidden(DEFAULT_LAYOUT, 'pools', true)), false);
});

test('the rows are every present panel in order, each saying whether it is hidden', () => {
  const layout: PanelLayout = { order: [...DEFAULT_LAYOUT.order], hidden: ['pools'] };
  const rows = panelRows(OVERVIEW_PANELS, layout, { setupPending: true });
  assert.deepEqual(
    rows.map((row) => [row.panel.id, row.hidden]),
    [
      ['pools', true],
      ['jobs', false],
      ['events', false],
      ['diagnostics', false],
    ],
  );
});

test('a move swaps with the neighbour that is drawn, stepping over a panel the fleet state leaves out', () => {
  // The first run leaves the failures out, so the panel under Pools on the
  // screen is the jobs: a press on Move down must put Pools under the jobs,
  // not swap it with a panel nobody can see and change nothing in view.
  const drawn = panelRows(OVERVIEW_PANELS, DEFAULT_LAYOUT, { setupPending: true }).map(
    (row) => row.panel.id,
  );
  const down = movePanel(DEFAULT_LAYOUT, 'pools', 1, drawn);
  assert.deepEqual(down.order, [
    'attention',
    'failures',
    'jobs',
    'pools',
    'events',
    'history',
    'diagnostics',
  ]);
  const drawnAfter = panelRows(OVERVIEW_PANELS, down, { setupPending: true }).map(
    (row) => row.panel.id,
  );
  const back = movePanel(down, 'pools', -1, drawnAfter);
  // Up again restores what is drawn; where Pools stands against the absent
  // failures in the saved order is not something anybody can see.
  assert.deepEqual(
    panelRows(OVERVIEW_PANELS, back, { setupPending: true }).map((row) => row.panel.id),
    drawn,
  );
  assert.equal(movePanel(DEFAULT_LAYOUT, 'pools', -1, drawn), DEFAULT_LAYOUT);
});
