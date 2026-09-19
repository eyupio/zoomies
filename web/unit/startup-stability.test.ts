import assert from 'node:assert/strict';
import test from 'node:test';
import type { Problem, Setting, Settings } from '../src/lib/api/types';
import { startupFix } from '../src/lib/pools/startupStability';
const problem = (code: string) => ({ code }) as Problem;
const row = (key: string, extra: Partial<Setting> = {}) =>
  ({ key, editable: true, source: 'database', pending: false, ...extra }) as Setting;
test('one-time fix respects pinned, pending and deliberately tuned settings', () => {
  const settings: Settings = {
    settings: [
      row('scheduler.default_runner_limits'),
      row('scheduler.host_throttling', { source: 'environment' }),
      row('agent.bootstrap_cpu_grace', { pending: true }),
      row('runners.docker_wait', { value: '8m' }),
    ],
  };
  const fix = startupFix(settings, [
    problem('scheduler.default_runner_limits_off'),
    problem('scheduler.host_throttling_off'),
    problem('agent.bootstrap_cpu_grace_short'),
  ]);
  assert.deepEqual(fix.changes, { 'scheduler.default_runner_limits': true });
  assert.deepEqual(fix.blocked, ['scheduler.host_throttling']);
  assert.deepEqual(fix.pending, ['agent.bootstrap_cpu_grace']);
});
test('provisioning remedy respects a long readiness wait and is empty after fixing', () => {
  const settings: Settings = {
    settings: [row('scheduler.provision_timeout'), row('runners.docker_wait', { value: '1h' })],
  };
  assert.deepEqual(startupFix(settings, [problem('scheduler.provision_timeout_short')]).changes, {
    'scheduler.provision_timeout': '75m',
  });
  assert.deepEqual(startupFix(settings, []).changes, {});
});
test('missing or read-only settings are excluded', () => {
  const settings: Settings = { settings: [row('runners.docker_wait', { editable: false })] };
  const fix = startupFix(settings, [
    problem('scheduler.default_runner_limits_off'),
    problem('runners.docker_wait_short'),
  ]);
  assert.deepEqual(fix.changes, {});
  assert.equal(fix.blocked.length, 2);
});
