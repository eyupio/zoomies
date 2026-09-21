import { test } from 'node:test';
import assert from 'node:assert/strict';
import { resolveStatusStyle } from '../src/lib/state/status-style';
import { standardMotion } from '../src/lib/runners/standard-motion';

test('upgrading preserves vocabulary opt-out and the existing Cute default', () => {
  assert.equal(resolveStatusStyle({}), 'cute');
  assert.equal(resolveStatusStyle({ quirkyStatus: true }), 'cute');
  assert.equal(resolveStatusStyle({ quirkyStatus: false }), 'off');
  assert.equal(resolveStatusStyle({ statusStyle: 'unexpected', quirkyStatus: false }), 'off');
  assert.equal(resolveStatusStyle({ statusStyle: null }), 'cute');
});

test('an explicit style wins over the legacy boolean on reload', () => {
  assert.equal(resolveStatusStyle({ statusStyle: 'standard', quirkyStatus: false }), 'standard');
  assert.equal(resolveStatusStyle({ statusStyle: 'off', quirkyStatus: true }), 'off');
  assert.equal(resolveStatusStyle({ statusStyle: 'cute', quirkyStatus: false }), 'cute');
});

test('maximum zoomies always runs faster than extra zoomies and normal work', () => {
  const seeds = ['runner-one', 'runner-two', 'workflow-left', 'workflow-right'];
  for (const seed of seeds) {
    const maximum = standardMotion('maximum_zoomies', seed);
    const extra = standardMotion('zoomies', seed);
    const busy = standardMotion('busy', seed);
    assert.ok(maximum.stride < extra.stride && extra.stride < busy.stride);
    assert.ok(maximum.stride >= 0.32 && maximum.stride < 0.34);
    assert.ok(maximum.spin >= 10);
    assert.ok(maximum.spinning && extra.spinning && !busy.spinning);
    assert.deepEqual(standardMotion('maximum_zoomies', seed), maximum);
  }
  assert.notEqual(standardMotion('busy', seeds[0]).phase, standardMotion('busy', seeds[1]).phase);
});

test('waiting and lifecycle states cannot accidentally run or spin', () => {
  for (const state of [
    'idle',
    'provisioning',
    'registering',
    'throttled',
    'draining',
    'failed',
    'removed',
    'unknown',
  ]) {
    const motion = standardMotion(state, 'runner');
    assert.equal(motion.running, false, state);
    assert.equal(motion.spinning, false, state);
    assert.equal(motion.still, ['failed', 'removed', 'unknown'].includes(state), state);
  }
  assert.equal(standardMotion('future-state', 'runner').state, 'unknown');
  assert.equal(standardMotion('future-state', 'runner').still, true);
});
