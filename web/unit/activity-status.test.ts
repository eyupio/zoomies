import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { registerHooks } from 'node:module';

const LUCIDE = 'zoomies-test:lucide-stub';
const iconNames = (
  /import\s*\{([^}]*)\}\s*from\s*'@lucide\/svelte'/.exec(
    readFileSync(new URL('../src/lib/status.ts', import.meta.url), 'utf8'),
  )?.[1] ?? ''
)
  .split(',')
  .map((name) => name.trim())
  .filter((name) => name !== '');

registerHooks({
  resolve(specifier, context, next) {
    if (specifier === '@lucide/svelte')
      return { url: LUCIDE, format: 'module', shortCircuit: true };
    return next(specifier, context);
  },
  load(url, context, next) {
    if (url === LUCIDE)
      return {
        format: 'module',
        shortCircuit: true,
        source: iconNames.map((name) => `export function ${name}() {}`).join('\n'),
      };
    return next(url, context);
  },
});

const { queuedActivity, workflowActivity } = await import('../src/lib/jobs/activity-status.ts');

test('queue cancellation overrides priority and pause without claiming work is running', () => {
  for (const provisioning of ['ready', 'paused', 'expedited', 'deleted'] as const) {
    assert.equal(
      queuedActivity({ provisioning, cancel_requested_at: '2026-09-20T12:00:00Z' }).status.key,
      'cancelling',
    );
  }
  assert.equal(queuedActivity({}).label, 'Sit, wait');
  assert.equal(queuedActivity({ provision_now: true }).status.label, 'Run now');
  assert.equal(
    queuedActivity({ provisioning: 'paused', provision_now: true }).status.label,
    'Paused',
  );
  assert.notEqual(queuedActivity({ provisioning: 'expedited' }).motion, 'busy');
});

test('workflow pack preserves outcomes and cancellation precedence', () => {
  assert.equal(workflowActivity({ state: 'in_progress' }).motion, 'busy');
  assert.equal(
    workflowActivity({ state: 'in_progress', cancelling: true }).status.key,
    'cancelling',
  );
  assert.equal(workflowActivity({ state: 'completed', conclusion: 'success' }).label, 'Good pack!');
  for (const conclusion of ['failure', 'timed_out', 'startup_failure']) {
    const result = workflowActivity({ state: 'completed', conclusion });
    assert.equal(result.status.key, conclusion);
    assert.equal(result.motion, 'failed');
  }
  assert.equal(workflowActivity({ state: 'completed', conclusion: 'cancelled' }).motion, 'removed');
  assert.equal(workflowActivity({}).motion, 'unknown');
});

test('turning the kennel vocabulary off gives the plain status word instead, motion unchanged', () => {
  assert.equal(queuedActivity({}, false).label, 'Ready');
  assert.equal(queuedActivity({ provisioning: 'paused' }, false).label, 'Paused');
  assert.equal(queuedActivity({ provisioning: 'deleted' }, false).label, 'Removed');
  assert.equal(queuedActivity({ provision_now: true }, false).label, 'Run now');
  assert.equal(
    queuedActivity({ cancel_requested_at: '2026-09-20T12:00:00Z' }, false).label,
    'Cancelling',
  );
  assert.equal(queuedActivity({}, false).motion, queuedActivity({}).motion);

  assert.equal(workflowActivity({ state: 'in_progress' }, false).label, 'Running');
  assert.equal(
    workflowActivity({ state: 'completed', conclusion: 'success' }, false).label,
    'Success',
  );
  assert.equal(
    workflowActivity({ state: 'in_progress' }, false).motion,
    workflowActivity({ state: 'in_progress' }).motion,
  );
});
