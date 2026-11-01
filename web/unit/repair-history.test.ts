import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  BULK_LIMIT,
  NO_REPAIR_FILTERS,
  batches,
  clearConsequences,
  clearReport,
  clearSummary,
  filtered,
  isActive,
  listQuery,
  offsetAfterClear,
  selectionQuery,
  stateLabel,
} from '../src/lib/repairs/history';

// The server treats a missing parameter as "no filter" and a spelled-out
// default as a value it must recognise, so the page leaves the defaults out
// rather than send `group=all` and depend on the server agreeing about it.
test('the list query leaves out the defaults and trims the repository', () => {
  assert.deepEqual(listQuery({ group: 'all', repo: ' site ', trigger: 'any' }, 50, 0), {
    repo: 'site',
    limit: 50,
    offset: 0,
  });
  assert.deepEqual(listQuery(NO_REPAIR_FILTERS, 25, 75), { limit: 25, offset: 75 });
  assert.deepEqual(listQuery({ group: 'attention', repo: '', trigger: 'automatic' }, 100, 100), {
    group: 'attention',
    trigger: 'automatic',
    limit: 100,
    offset: 100,
  });
});

// A box holding only spaces is not a filter; sending repo=%20%20 would match
// nothing and leave an operator looking at an empty history with no idea why.
test('a repository box holding only spaces is not sent', () => {
  assert.deepEqual(listQuery({ group: 'all', repo: '   ', trigger: 'any' }, 50, 0), {
    limit: 50,
    offset: 0,
  });
});

// The selection endpoint takes the same filters as the list and nothing about
// paging, because a select-all means everything the filters match.
test('the selection query carries the filters and no paging', () => {
  assert.deepEqual(selectionQuery(NO_REPAIR_FILTERS), {});
  assert.deepEqual(selectionQuery({ group: 'passed', repo: ' widgets', trigger: 'requested' }), {
    group: 'passed',
    repo: 'widgets',
    trigger: 'requested',
  });
});

// "Filtered" decides between "No repairs yet" and "No repairs match these
// filters", which are different facts that an operator acts on differently.
test('a filter counts as set when any of the three is away from its default', () => {
  assert.equal(filtered(NO_REPAIR_FILTERS), false);
  assert.equal(filtered({ ...NO_REPAIR_FILTERS, group: 'active' }), true);
  assert.equal(filtered({ ...NO_REPAIR_FILTERS, repo: 'site' }), true);
  assert.equal(filtered({ ...NO_REPAIR_FILTERS, trigger: 'requested' }), true);
  assert.equal(filtered({ ...NO_REPAIR_FILTERS, repo: '  ' }), false);
});

// These are the states a repair cannot be cleared or retried from, so the set
// must match the server's idea of in progress exactly.
test('a repair is in progress in exactly the queued, working and checking states', () => {
  for (const state of ['queued', 'working', 'checking']) assert.equal(isActive(state), true);
  for (const state of [
    'succeeded',
    'checks_failed',
    'interrupted',
    'unverified',
    'superseded',
    'blocked',
    'failed',
    '',
    'something_new',
  ])
    assert.equal(isActive(state), false, state);
});

test('a state reads as a phrase and an unknown one comes back as it was given', () => {
  assert.equal(stateLabel('checks_failed'), 'Checks failed');
  assert.equal(stateLabel('working'), 'Investigating');
  assert.equal(stateLabel('a_state_from_a_newer_controller'), 'a_state_from_a_newer_controller');
});

// After a clear the page the operator was on may no longer exist. Stepping
// back to the last page that still has rows keeps them looking at repairs
// rather than at an empty list beside a total that says there are some.
test('after a clear the offset steps back only when its page would be empty', () => {
  // 102 matched, 1 cleared: 101 remain, and the page starting at 100 still has a row.
  assert.equal(offsetAfterClear(100, 50, 102, 1), 100);
  // 101 matched, 1 cleared: 100 remain, so the page starting at 100 is empty.
  assert.equal(offsetAfterClear(100, 50, 101, 1), 50);
  assert.equal(offsetAfterClear(100, 50, 101, 2), 50);
  // Everything cleared: the first page, never a negative offset.
  assert.equal(offsetAfterClear(0, 50, 3, 3), 0);
  assert.equal(offsetAfterClear(50, 50, 51, 51), 0);
  // Clearing more than the total says is as good as clearing all of it.
  assert.equal(offsetAfterClear(100, 50, 101, 500), 0);
});

// The bar says how many were cleared and the disclosure says which were not and
// why, so the order of the skipped list is the order the operator chose in.
test('a clear report counts what was cleared and lists the rest with their reasons in order', () => {
  assert.deepEqual(
    clearReport([
      { id: 'rep-1', ok: true },
      { id: 'rep-2', ok: false, error: 'still running' },
      { id: 'rep-3', ok: true },
      { id: 'rep-4', ok: false, error: 'not found' },
    ]),
    {
      cleared: 2,
      skipped: [
        { id: 'rep-2', error: 'still running' },
        { id: 'rep-4', error: 'not found' },
      ],
    },
  );
  assert.deepEqual(clearReport([]), { cleared: 0, skipped: [] });
});

// A refusal with no message would otherwise be listed as a bare id and leave an
// operator wondering what was wrong with it.
test('a refusal that carries no reason still says that it was not cleared', () => {
  assert.deepEqual(clearReport([{ id: 'rep-9', ok: false }]).skipped, [
    { id: 'rep-9', error: 'not cleared' },
  ]);
});

// The dialog is the only place an operator is told what clearing leaves
// untouched, so its sentences are held here rather than only in markup. After a
// select-all the count is the one thing they cannot see from where they stand:
// most of the repairs are on other pages.
test('the clear confirmation says how many go, that it cannot be undone here, and what stays', () => {
  const kept = ['Eli will not repair its own commits from them again.'];
  assert.deepEqual(clearConsequences(35), [
    '35 repairs will stop showing in this list, and this cannot be undone from this page.',
    ...kept,
  ]);
  assert.deepEqual(
    clearConsequences(500)[0],
    '500 repairs will stop showing in this list, and this cannot be undone from this page.',
  );
  // One repair is not "1 repairs".
  assert.deepEqual(clearConsequences(1), [
    '1 repair will stop showing in this list, and this cannot be undone from this page.',
    ...kept,
  ]);
});

// A clear that stops part-way has cleared some and not tried the rest. Saying
// "0 skipped" would leave the repairs it never reached nowhere on the screen.
test('repairs a clear never reached are listed after the refused ones as not attempted', () => {
  assert.deepEqual(
    clearReport(
      [
        { id: 'rep-1', ok: true },
        { id: 'rep-2', ok: false, error: 'still running' },
      ],
      ['rep-8', 'rep-9'],
    ),
    {
      cleared: 1,
      skipped: [
        { id: 'rep-2', error: 'still running' },
        { id: 'rep-8', error: 'not attempted' },
        { id: 'rep-9', error: 'not attempted' },
      ],
    },
  );
  assert.deepEqual(clearReport([{ id: 'rep-1', ok: true }], []).skipped, []);
});

// The bulk route takes 1 to 500 ids and refuses a longer list outright, so a
// selection that has grown past that is sent as several requests.
test('a selection past the bulk limit is cut into requests the route accepts', () => {
  assert.equal(BULK_LIMIT, 500);
  const ids = Array.from({ length: 1201 }, (_, i) => `rep-${i}`);
  const parts = batches(ids, BULK_LIMIT);
  assert.deepEqual(
    parts.map((part) => part.length),
    [500, 500, 201],
  );
  assert.deepEqual(parts.flat(), ids);
  assert.deepEqual(batches(['a'], BULK_LIMIT), [['a']]);
  assert.deepEqual(batches([], BULK_LIMIT), []);
});

test('the summary of a clear counts what was cleared and what was skipped', () => {
  assert.equal(clearSummary(1, 0), '1 repair cleared from the history');
  assert.equal(clearSummary(2, 1), '2 repairs cleared from the history, 1 skipped');
  assert.equal(clearSummary(0, 3), '0 repairs cleared from the history, 3 skipped');
  assert.equal(clearSummary(1500, 1200), '1,500 repairs cleared from the history, 1,200 skipped');
});
