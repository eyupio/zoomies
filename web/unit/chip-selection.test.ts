import { test } from 'node:test';
import assert from 'node:assert/strict';
import { pickOne, toggleChip } from '../src/lib/components/chip-selection.ts';

test('a chip pressed joins the selection, and pressed again leaves it', () => {
  assert.deepEqual(toggleChip([], 'busy'), ['busy']);
  assert.deepEqual(toggleChip(['busy'], 'idle'), ['busy', 'idle']);
  assert.deepEqual(toggleChip(['busy', 'idle'], 'busy'), ['idle']);
});

test('a one-valued filter reads a press as the chip that changed, and an emptied selection as none', () => {
  // The chips toggle a selection; a parameter that holds one value never two
  // takes the value that was not in force before, so a second chip replaces
  // the first rather than joining it.
  assert.equal(pickOne('', ['pool']), 'pool');
  assert.equal(pickOne('pool', ['pool', 'host']), 'host');
  assert.equal(pickOne('pool', []), '');
  assert.equal(pickOne('host', ['host', 'pool']), 'pool');
});
