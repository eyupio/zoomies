import { test } from 'node:test';
import assert from 'node:assert/strict';
import { pickStatus, poolStatusChips, poolsEyebrow } from '../src/lib/pools/status-chips.ts';

const pools = [{ enabled: true }, { enabled: false }, {}, { enabled: true }];

test('the chips are Enabled and Disabled, counted from the pools, a pool with no flag enabled', () => {
  assert.deepEqual(poolStatusChips(pools), [
    { value: 'enabled', label: 'Enabled', count: 3 },
    { value: 'disabled', label: 'Disabled', count: 1 },
  ]);
});

test('the eyebrow is how many pools there are and how many are enabled, or nothing with none', () => {
  assert.equal(poolsEyebrow(pools), '4 pools, 3 enabled');
  assert.equal(poolsEyebrow([{ enabled: true }]), '1 pool, 1 enabled');
  assert.equal(poolsEyebrow([]), undefined);
});

test('the status is one value: pressing a chip takes it, pressing it again or All clears it', () => {
  // The chips toggle a selection; the page's status parameter holds one value
  // and never two, so the press is read as "the chip that changed".
  assert.equal(pickStatus('', ['enabled']), 'enabled');
  assert.equal(pickStatus('enabled', ['enabled', 'disabled']), 'disabled');
  assert.equal(pickStatus('enabled', []), '');
  assert.equal(pickStatus('disabled', ['disabled', 'enabled']), 'enabled');
});
