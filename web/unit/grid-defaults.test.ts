import { test } from 'node:test';
import assert from 'node:assert/strict';
import { hasLayout, pinShown, shownAfterUpgrade } from '../src/lib/state/grid-defaults.ts';

test('a grid record with any saved layout counts as arranged', () => {
  assert.equal(hasLayout(undefined), false);
  assert.equal(hasLayout({}), false);
  assert.equal(hasLayout({ pageSize: 50 }), false);
  assert.equal(hasLayout({ hidden: ['host'] }), true);
  assert.equal(hasLayout({ widths: { name: 240 } }), true);
  assert.equal(hasLayout({ order: ['name', 'state'] }), true);
});

test('a layout saved before a column became hidden by default keeps that column on the screen', () => {
  // The record has a layout and no `shown` list, so it is from before; the
  // operator arranged this grid and sees no change.
  assert.deepEqual(shownAfterUpgrade({ hidden: ['host'] }, ['created_at', 'cpu']), [
    'created_at',
    'cpu',
  ]);
});

test('a record that already says what is shown, or has no layout, is left alone', () => {
  assert.equal(shownAfterUpgrade({ hidden: ['host'], shown: [] }, ['created_at']), undefined);
  assert.equal(shownAfterUpgrade({ shown: ['cpu'] }, ['created_at']), undefined);
  assert.equal(shownAfterUpgrade(undefined, ['created_at']), undefined);
  assert.equal(shownAfterUpgrade({ pageSize: 25 }, ['created_at']), undefined);
});

test('a layout write pins the shown list, so a later default applies to a grid arranged after it', () => {
  assert.deepEqual(pinShown({ hidden: ['host'] }), { hidden: ['host'], shown: [] });
  assert.deepEqual(pinShown({ shown: ['cpu'], widths: { name: 1 } }), {
    shown: ['cpu'],
    widths: { name: 1 },
  });
  assert.deepEqual(pinShown(undefined), { shown: [] });
});
