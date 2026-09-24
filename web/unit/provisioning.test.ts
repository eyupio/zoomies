/**
 * The provisioning controls the Queue page and the Workflows page share: which
 * of Run now, Pause, Resume and Delete a job's row offers, and why each is
 * refused when it is. Three pages read this, so a refusal that is right here
 * is right on all of them, and one that is wrong is wrong three times over.
 *
 * `status.ts` imports Lucide components, which cannot load outside a browser;
 * the hook below swaps them for plain functions, as the other tests do.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { registerHooks } from 'node:module';

const LUCIDE = 'zoomies-test:lucide-stub';
// Both files' icons: the actions' glyphs are the buttons' own, and the state
// map deliberately shares none of them, so neither list covers the other.
const iconNames = [
  ...new Set(
    ['../src/lib/status.ts', '../src/lib/jobs/provisioning.ts'].flatMap((file) =>
      (
        /import\s*\{([^}]*)\}\s*from\s*'@lucide\/svelte'/.exec(
          readFileSync(new URL(file, import.meta.url), 'utf8'),
        )?.[1] ?? ''
      )
        .split(',')
        .map((name) => name.trim())
        .filter((name) => name !== ''),
    ),
  ),
];

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

const {
  jobProvisioningActions,
  PROVISIONING_ACTIONS,
  PROVISIONING_IN_FORCE,
  provisioningStatus,
  provisioningUnavailable,
  RUN_PROVISIONING_ACTIONS,
} = await import('../src/lib/jobs/provisioning.ts');

const queued = { state: 'queued' as const, matched: true, hosted: false };

test('a queued job offers every action but the one already in force', () => {
  const noop = () => {};
  const on = (job: Parameters<typeof jobProvisioningActions>[0]) =>
    Object.fromEntries(jobProvisioningActions(job, noop).map((a) => [a.id, a]));

  const ready = on(queued);
  assert.equal(ready.resume.disabled, true);
  assert.equal(ready.resume.reason, PROVISIONING_IN_FORCE.resume);
  for (const id of ['run_now', 'pause', 'delete']) assert.equal(ready[id].disabled, false, id);

  const expedited = on({ ...queued, provision_now: true });
  assert.equal(expedited.run_now.disabled, true);
  assert.equal(expedited.resume.disabled, false);

  const paused = on({ ...queued, provisioning: 'paused' });
  assert.equal(paused.pause.disabled, true);
  assert.equal(paused.pause.reason, PROVISIONING_IN_FORCE.pause);
  // Run now on a paused job resumes it as well, so it is offered.
  assert.equal(paused.run_now.disabled, false);

  const removed = on({ ...queued, provisioning: 'deleted' });
  assert.equal(removed.delete.disabled, true);
  assert.equal(removed.resume.disabled, false);
});

test('a job that raises no demand refuses every action with the same reason', () => {
  // Nothing to hold once something has started it, and nothing here to hurry
  // for a job whose labels all name somebody else's runners. The refusal is
  // one sentence on every button rather than four different ones.
  for (const job of [
    { ...queued, state: 'in_progress' as const },
    { ...queued, state: 'completed' as const },
    { ...queued, state: 'waiting' as const },
    { ...queued, matched: false, hosted: true },
    { ...queued, cancel_requested_at: '2026-09-20T12:00:00Z' },
  ]) {
    const why = provisioningUnavailable(job);
    assert.ok(why, JSON.stringify(job));
    for (const a of jobProvisioningActions(job, () => {})) {
      assert.equal(a.disabled, true, `${a.id} on ${JSON.stringify(job)}`);
      assert.equal(a.reason, why);
    }
  }
  assert.equal(provisioningUnavailable(queued), undefined);
});

test('the run offers the Queue page’s actions less removal, in its order', () => {
  // A removal is a decision about one job, made where the Removed view can
  // undo it. The other three keep the Queue page's order, so an operator's
  // hand finds Pause in the same place on either page.
  assert.deepEqual(
    RUN_PROVISIONING_ACTIONS.map((a) => a.id),
    PROVISIONING_ACTIONS.map((a) => a.id).filter((id) => id !== 'delete'),
  );
  assert.deepEqual(
    PROVISIONING_ACTIONS.map((a) => a.id),
    ['run_now', 'pause', 'resume', 'delete'],
  );
});

test('the status is read the way the Queue page reads it', () => {
  assert.equal(provisioningStatus({}), 'ready');
  assert.equal(provisioningStatus({ provision_now: true }), 'expedited');
  assert.equal(provisioningStatus({ provisioning: 'paused', provision_now: true }), 'paused');
  assert.equal(provisioningStatus({ provisioning: 'deleted' }), 'deleted');
});
