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
const { jobEventStatus } = await import('../src/lib/status.ts');

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

test('a run with a failed job says so before GitHub has finished the run', () => {
  // GitHub keeps the run in progress until its last job ends, so the row is
  // the only place the operator sees the failure early. The leader shows it;
  // the rest of the pack keeps the pose the run's own state gives it.
  const failing = workflowActivity({ state: 'in_progress', jobs: { total: 10, failed: 1 } });
  assert.equal(failing.status.key, 'failing');
  assert.equal(failing.status.tone, 'danger');
  assert.equal(failing.label, 'Pack in trouble');
  assert.equal(failing.motion, 'failed');
  assert.equal(failing.packMotion, 'busy');
  assert.match(failing.detail, /1 of its 10 jobs has failed/);

  const queued = workflowActivity({ state: 'queued', jobs: { total: 4, failed: 2 } });
  assert.equal(queued.status.key, 'failing');
  assert.equal(queued.packMotion, 'idle');
  assert.match(queued.detail, /2 of its 4 jobs have failed/);

  assert.equal(
    workflowActivity({ state: 'in_progress', jobs: { failed: 1 } }, false).label,
    'Failing',
  );

  // A cancellation, and a run that has actually finished, still win: the
  // first is the operator's own decision, the second is GitHub's conclusion.
  assert.equal(
    workflowActivity({ state: 'in_progress', cancelling: true, jobs: { failed: 1 } }).status.key,
    'cancelling',
  );
  assert.equal(
    workflowActivity({ state: 'completed', conclusion: 'failure', jobs: { failed: 1 } }).status.key,
    'failure',
  );
  // A re-run that passed leaves nothing failed on the latest attempt.
  assert.equal(workflowActivity({ state: 'in_progress', jobs: { failed: 0 } }).motion, 'busy');
  assert.equal(
    workflowActivity({ state: 'in_progress', jobs: { failed: 0 } }).packMotion,
    undefined,
  );
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

/*
 * The Queue's and the Workflows page's row actions: Run now, Pause, Resume,
 * Delete from queue and Re-run. A status drawn with one of them reads as that
 * action -- a paused job with a Pause glyph beside it looked like a button to
 * pause it.
 */
const ACTION_ICONS = ['Zap', 'Pause', 'Play', 'Trash2', 'RotateCcw'];

test('with the vocabulary off, no queue or workflow status is drawn with an action icon', () => {
  const drawn = [
    ...(['ready', 'paused', 'deleted', 'expedited'] as const).map(
      (provisioning) => queuedActivity({ provisioning }, false).status,
    ),
    queuedActivity({ cancel_requested_at: '2026-09-20T12:00:00Z' }, false).status,
    ...(['queued', 'waiting', 'in_progress'] as const).map(
      (state) => workflowActivity({ state }, false).status,
    ),
    ...['success', 'failure', 'cancelled', 'timed_out', 'skipped'].map(
      (conclusion) => workflowActivity({ state: 'completed', conclusion }, false).status,
    ),
    jobEventStatus('rerun_requested'),
  ];
  for (const status of drawn) {
    assert.ok(!ACTION_ICONS.includes(status.icon.name), `${status.key} is ${status.icon.name}`);
  }
});

test('a job somebody pressed Run now on is not drawn like the ready rows around it', () => {
  assert.notEqual(
    queuedActivity({ provisioning: 'expedited' }, false).status.icon,
    queuedActivity({}, false).status.icon,
  );
  // Still queued, though: the tone says nothing is running yet.
  assert.equal(queuedActivity({ provisioning: 'expedited' }, false).status.tone, 'pending');
});
