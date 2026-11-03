import assert from 'node:assert/strict';
import test from 'node:test';
import { STEAL_SHOWN_PERCENT, stealShown } from '../src/lib/hosts/vitals.ts';

// CPU usage counts the time a hypervisor kept from a virtual machine as busy, so
// a host that is being starved reads as one hard at work. The card says so, but
// only once it is more than the noise a healthy hypervisor leaves.
test('steal is said once it is more than noise and not before', () => {
  assert.equal(stealShown(STEAL_SHOWN_PERCENT), STEAL_SHOWN_PERCENT);
  assert.equal(stealShown(36), 36);
  assert.equal(stealShown(STEAL_SHOWN_PERCENT - 0.1), null);
  assert.equal(stealShown(0), null);
});

test('a host that did not measure steal says nothing and is not read as zero', () => {
  assert.equal(stealShown(undefined), null);
});
