/**
 * The badge a job wears for what has been done to it, which is pure decision
 * logic and the one thing standing between an operator and a list that lies.
 *
 * GitHub keeps calling a job `queued` or `in_progress` until its own
 * completion delivery lands, so a row that showed only the state said the
 * fleet was about to run work that had been cancelled, or removed from the
 * queue, or put on hold. Three sources say it -- the Jobs grid, the drawer and
 * the pool's list -- and all three read this function, so this is where the
 * precedence between them is worth pinning down.
 *
 * `status.ts` imports Lucide components, which cannot load outside a browser.
 * The hook below swaps them for plain functions, as the views test does, and
 * reads the names out of the file so adding an icon does not break this one.
 */
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

const { queueStatus, QUEUE_STATUS_LABELS } = await import('../src/lib/status.ts');

const AT = '2026-01-01T00:00:00Z';

test('a job nobody has touched wears no badge', () => {
  // The ordinary case stays quiet: a badge on every row is a badge nobody
  // reads.
  for (const state of ['queued', 'in_progress'] as const) {
    assert.equal(queueStatus({ state }), undefined, state);
  }
});

test('a stood-down queued job says which way it was stood down', () => {
  assert.equal(queueStatus({ state: 'queued', provisioning: 'paused' })?.label, 'Paused');
  assert.equal(queueStatus({ state: 'queued', provisioning: 'deleted' })?.label, 'Removed');
});

test('a cancellation outranks the pause it causes', () => {
  // Cancelling a run pauses its queued jobs, so a job somebody cancelled
  // carries both marks. Reporting the pause would tell an operator their own
  // cancellation was a hold, and send them looking for the Resume that undoes
  // it.
  assert.equal(
    queueStatus({ state: 'queued', provisioning: 'paused', cancel_requested_at: AT })?.label,
    'Cancelling',
  );
  // And it is the only one that applies to a running job, whose runner the
  // fleet has already taken away.
  assert.equal(queueStatus({ state: 'in_progress', cancel_requested_at: AT })?.label, 'Cancelling');
});

test('a finished job wears none of them', () => {
  // Once something has run it, what was done to its demand is history and the
  // conclusion is the status. A completed job badged "Cancelling" would read
  // as one still on its way out.
  for (const job of [
    { state: 'completed' as const, provisioning: 'deleted' },
    { state: 'completed' as const, cancel_requested_at: AT },
    { state: 'waiting' as const, provisioning: 'paused' },
  ]) {
    assert.equal(queueStatus(job), undefined, JSON.stringify(job));
  }
});

test('the badges and the Queue page read from one vocabulary', () => {
  // The Queue's status buttons, the Jobs page's filter chip and the badge on
  // a row all take their words from this map. Two names for one state is how
  // two pages come to disagree about what an operator did.
  assert.deepEqual(QUEUE_STATUS_LABELS, {
    ready: 'Ready',
    expedited: 'Run now',
    paused: 'Paused',
    deleted: 'Removed',
  });
  assert.equal(
    queueStatus({ state: 'queued', provisioning: 'deleted' })?.label,
    QUEUE_STATUS_LABELS.deleted,
  );
  assert.equal(
    queueStatus({ state: 'queued', provisioning: 'paused' })?.label,
    QUEUE_STATUS_LABELS.paused,
  );
});
