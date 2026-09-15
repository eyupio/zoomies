/**
 * The Jobs page's status views, which are pure decision logic.
 *
 * The page answers "what is running?" before it is asked anything, and the
 * whole of that promise lives in two functions here: which state filter a view
 * writes, and which view a set of filters reads back as. Both are reached from
 * every link into the page -- the problems panel's, the Overview's, a
 * colleague's -- and a Playwright pass only ever walks the two or three of
 * those that a spec happens to visit. This walks all of them.
 *
 * `views.ts` reaches `status.ts` for each view's shape and colour, and that
 * imports Lucide components, which cannot load outside a browser. The hook
 * below swaps them for plain functions, as the provider tests do, and reads
 * the names out of `status.ts` so adding an icon does not break this file.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { registerHooks } from 'node:module';
import { JOB_STATES } from '../src/lib/api/types.ts';
import type { JobState } from '../src/lib/api/types.ts';

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

const { JOB_VIEWS, DEFAULT_JOB_STATE, currentJobView } = await import('../src/lib/jobs/views.ts');

/** The filter shape `currentJobView` reads, with everything off by default. */
function filters(
  over: Partial<{
    state: JobState[];
    conclusion: string[];
    failed: boolean;
    faulted: boolean;
    unmatched: boolean;
  }> = {},
) {
  return { state: [], conclusion: [], failed: false, faulted: false, unmatched: false, ...over };
}

test('every view reads back as itself', () => {
  // The row of buttons is only honest if writing a view and reading it back
  // gives the same view. A button that wrote a filter no button claims would
  // leave the whole row unpressed the moment it was used.
  for (const view of JOB_VIEWS) {
    assert.equal(currentJobView(view.filters), view.id, `${view.id} does not read back as itself`);
  }
});

test('no view leaves a status key set that another view owns', () => {
  // Each view writes every status key, so pressing one gives exactly what it
  // says rather than half of the view before it.
  for (const view of JOB_VIEWS) {
    const keys = Object.keys(view.filters).sort();
    assert.deepEqual(
      keys,
      ['conclusion', 'failed', 'faulted', 'state', 'unmatched'],
      `${view.id} is partial`,
    );
  }
});

test('the default is the running view', () => {
  // The page opens on what is running, and the state it sends has to be the
  // one the Running button would send, or the button is unpressed on arrival.
  assert.equal(currentJobView(filters({ state: [...DEFAULT_JOB_STATE] })), 'running');
});

test('no state and every state are both "all"', () => {
  // A link that says nothing about status asked for every status, and the All
  // button writes every state out in full so that "all" and "ask me nothing"
  // stay distinguishable in the address bar. Both have to read as All.
  assert.equal(currentJobView(filters()), 'all');
  assert.equal(currentJobView(filters({ state: [...JOB_STATES] })), 'all');
  // Order is not meaning: a hand-edited link is no less the All view.
  assert.equal(currentJobView(filters({ state: [...JOB_STATES].reverse() })), 'all');
});

test('a status no button says leaves every button unpressed', () => {
  // Saying one view is in force when it is not would be a lie the chips above
  // the grid immediately contradict, so the row says nothing instead.
  assert.equal(currentJobView(filters({ state: ['queued', 'completed'] })), '');
  assert.equal(currentJobView(filters({ conclusion: ['success'] })), '');
  assert.equal(currentJobView(filters({ unmatched: true })), '');
  assert.equal(currentJobView(filters({ state: ['in_progress'], failed: true })), '');
  // The two failure views are different questions, and a filter asking both at
  // once is neither of them.
  assert.equal(currentJobView(filters({ failed: true, faulted: true })), '');
});

test("the fleet's own failures are a view of their own, not a narrowing of Failed", () => {
  // "Failed" and "Our failures" answer different questions -- is CI broken,
  // and is *our* CI broken -- and the second is the only one anybody here can
  // fix. Reading one as the other would press the wrong button on arrival from
  // a link that meant the other.
  assert.equal(currentJobView(filters({ faulted: true })), 'faulted');
  assert.notEqual(currentJobView(filters({ failed: true })), 'faulted');
  assert.notEqual(currentJobView(filters({ faulted: true })), 'failed');
});

test('failed is not a state, and does not stack on one', () => {
  // "Failed" is the server's own reckoning of a job that went wrong, runner
  // faults included. It is ANDed with any state beside it, so the view has to
  // clear the state it replaces or it would match only the jobs that are both.
  const failed = JOB_VIEWS.find((v) => v.id === 'failed');
  assert.ok(failed, 'there is a failed view');
  assert.equal(failed.filters.failed, true);
  assert.deepEqual(failed.filters.state, []);
});

test('no view asks for unmatched, which the server reads as queued', () => {
  // `unmatched` forces state='queued' server side, so a view that set it
  // alongside its own state would match nothing at all.
  for (const view of JOB_VIEWS) {
    assert.equal(view.filters.unmatched, false, `${view.id} sets unmatched`);
  }
});

test('every state the API knows is reachable from the row', () => {
  // A state no button can reach is a state an operator can only get to by
  // editing the address bar.
  const reachable = new Set(JOB_VIEWS.flatMap((v) => v.filters.state));
  for (const state of JOB_STATES) {
    assert.ok(reachable.has(state), `no view reaches ${state}`);
  }
});
