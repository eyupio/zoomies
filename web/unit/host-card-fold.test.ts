import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isIdList, setExpanded } from '../src/lib/hosts/card-fold.ts';

test('a stored choice is trusted only while it is a list of host ids', () => {
  // A value left by an older build, or edited by hand, must fall back to every
  // card closed rather than throw on the first card that asks.
  assert.equal(isIdList([]), true);
  assert.equal(isIdList(['host_a', 'host_b']), true);
  for (const bad of [null, undefined, 'host_a', 7, {}, [1], ['host_a', 2], [null]]) {
    assert.equal(isIdList(bad), false, JSON.stringify(bad));
  }
});

test('opening a card remembers it once, however many times it is opened', () => {
  const known = ['host_a', 'host_b', 'host_c'];
  const once = setExpanded([], 'host_b', true, known);
  assert.deepEqual(once, ['host_b']);
  assert.deepEqual(setExpanded(once, 'host_b', true, known), ['host_b']);
  assert.deepEqual(setExpanded(once, 'host_a', true, known), ['host_b', 'host_a']);
});

test('closing a card forgets it and leaves the others open', () => {
  const known = ['host_a', 'host_b', 'host_c'];
  assert.deepEqual(setExpanded(['host_a', 'host_b'], 'host_a', false, known), ['host_b']);
  assert.deepEqual(
    setExpanded(['host_b'], 'host_c', false, known),
    ['host_b'],
    'not open: no change',
  );
});

test('hosts that no longer exist are forgotten the next time a choice is made', () => {
  // Machines come and go as pools scale, so without this the list would keep an
  // id for every host anyone ever opened.
  const remembered = ['host_gone', 'host_a'];
  assert.deepEqual(setExpanded(remembered, 'host_b', true, ['host_a', 'host_b']), [
    'host_a',
    'host_b',
  ]);
  assert.deepEqual(setExpanded(remembered, 'host_a', false, ['host_a']), []);
});

test('the list it is handed is never changed', () => {
  // The page replaces its state with the answer. A function that edited the old
  // array in place would hand back the very array already held, and nothing
  // would repaint.
  const held = ['host_a'];
  const next = setExpanded(held, 'host_b', true, ['host_a', 'host_b']);
  assert.deepEqual(held, ['host_a']);
  assert.notEqual(next, held);
});
