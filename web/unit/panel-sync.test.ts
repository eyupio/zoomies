import { test } from 'node:test';
import assert from 'node:assert/strict';
import { panelLayoutsAfterSync } from '../src/lib/state/panel-sync.ts';
import type { PanelLayout } from '../src/lib/overview/panels.ts';

const arranged: PanelLayout = {
  order: ['pools', 'attention', 'failures', 'jobs', 'events', 'history', 'diagnostics'],
  hidden: ['history'],
};
const browser = { overview: arranged };

test("the account's layouts win when it has a document", () => {
  const out = panelLayoutsAfterSync({}, { overview: arranged }, 'u1', 'u1');
  assert.deepEqual(out, browser);
});

test('a reset made in another browser reaches this one: the same account with no document clears the copy', () => {
  // The account's document carries no `panels` once every page is at its
  // default, so "no document" on a browser this account has used before is
  // what a reset elsewhere looks like, and the stale arrangement must go.
  assert.deepEqual(panelLayoutsAfterSync(browser, undefined, 'u1', 'u1'), {});
});

test('a browser last used by somebody else never carries their arrangement into this account', () => {
  assert.deepEqual(panelLayoutsAfterSync(browser, undefined, 'u2', 'u1'), {});
});

test('a browser with no previous account keeps its copy, which the first save carries', () => {
  assert.deepEqual(panelLayoutsAfterSync(browser, undefined, undefined, 'u1'), browser);
});
