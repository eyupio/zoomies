import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  parseStatus,
  pickStatus,
  poolStatusChips,
  poolsEyebrow,
} from '../src/lib/pools/status-chips.ts';

const pools = [{ enabled: true }, { enabled: false }, {}, { enabled: true }];

test('the chips are Enabled and Disabled, counted from the pools, a pool with no flag enabled', () => {
  assert.deepEqual(poolStatusChips(pools, true), [
    { value: 'enabled', label: 'Enabled', count: 3 },
    { value: 'disabled', label: 'Disabled', count: 1 },
  ]);
});

test('until the fleet cache has loaded the chips carry no count and the card no eyebrow', () => {
  // The grid's own fetch usually answers first and warms the cache with the
  // filtered page, so a count read then would describe the page, not the fleet.
  assert.deepEqual(
    poolStatusChips(pools, false).map((chip) => chip.count),
    [undefined, undefined],
  );
  assert.equal(poolsEyebrow(pools, false), undefined);
});

test('a status the address bar spells that the product lacks is no filter', () => {
  assert.equal(parseStatus('enabled'), 'enabled');
  assert.equal(parseStatus('disabled'), 'disabled');
  assert.equal(parseStatus(''), '');
  assert.equal(parseStatus('bogus'), '');
  assert.equal(parseStatus('Enabled'), '');
});

test('the eyebrow is how many pools there are and how many are enabled, or nothing with none', () => {
  assert.equal(poolsEyebrow(pools, true), '4 pools, 3 enabled');
  assert.equal(poolsEyebrow([{ enabled: true }], true), '1 pool, 1 enabled');
  assert.equal(poolsEyebrow([], true), undefined);
});

test('the status is one value: pressing a chip takes it, pressing it again or All clears it', () => {
  // The chips toggle a selection; the page's status parameter holds one value
  // and never two, so the press is read as "the chip that changed".
  assert.equal(pickStatus('', ['enabled']), 'enabled');
  assert.equal(pickStatus('enabled', ['enabled', 'disabled']), 'disabled');
  assert.equal(pickStatus('enabled', []), '');
  assert.equal(pickStatus('disabled', ['disabled', 'enabled']), 'enabled');
});
