import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Schemas } from '../src/lib/api/types.ts';
import {
  BEHAVIOURS,
  MAX_ATTEMPTS,
  behaviourOf,
  capabilitiesFor,
  checkOutcome,
  failingChecks,
  pauseRepositoryCopy,
  pauseWatchCopy,
  readCheckEvidence,
  readConflictEvidence,
  repositoryNames,
  stopAllCopy,
  stopWatchCopy,
  summariseRepository,
  summariseWatch,
  watchState,
  watchingOrder,
  watchingState,
  type Watch,
} from '../src/lib/repairs/watches.ts';

const NOW = Date.parse('2026-11-06T12:00:00Z');

function watch(over: Partial<Watch> = {}): Watch {
  return {
    id: 'watch-1',
    installation_id: 'ins_1',
    repo: 'acme/widgets',
    pull_number: 17,
    owner_id: 'user-1',
    checks: true,
    conflicts: true,
    enabled: true,
    created_at: '2026-11-06T09:00:00Z',
    updated_at: '2026-11-06T11:59:00Z',
    ...over,
  };
}

function decision(over: Partial<Schemas['EliRepairWatchRepository']> = {}) {
  return {
    id: 'dec-1',
    installation_id: 'ins_1',
    repo: 'acme/widgets',
    owner_id: 'user-1',
    checks: true,
    conflicts: true,
    enabled: true,
    created_at: '2026-11-06T09:00:00Z',
    updated_at: '2026-11-06T11:59:00Z',
    ...over,
  };
}

const checks = (state: string, over: Record<string, unknown> = {}): string =>
  JSON.stringify({
    state,
    total: 2,
    truncated: false,
    checks: [
      { name: 'test', status: 'completed', conclusion: 'success' },
      { name: 'lint', status: 'completed', conclusion: 'failure', app: 'github-actions' },
    ],
    ...over,
  });

test('the three behaviours are the two capabilities, and nothing else decides them', () => {
  assert.deepEqual(
    BEHAVIOURS.map((b) => b.value),
    ['babysit', 'checks', 'conflicts'],
  );
  assert.deepEqual(capabilitiesFor('babysit'), ['checks', 'conflicts']);
  assert.deepEqual(capabilitiesFor('checks'), ['checks']);
  assert.deepEqual(capabilitiesFor('conflicts'), ['conflicts']);
  assert.equal(behaviourOf(true, true), 'babysit');
  assert.equal(behaviourOf(true, false), 'checks');
  assert.equal(behaviourOf(false, true), 'conflicts');
  // A row recording neither capability is not a fourth behaviour: guessing one
  // would describe work nobody asked for.
  assert.equal(behaviourOf(false, false), null);
});

test('a watch is stopped by its stop reason and paused by its pause reason', () => {
  assert.equal(watchState(watch()), 'enabled');
  assert.equal(watchState(watch({ enabled: false })), 'paused');
  assert.equal(
    watchState(watch({ enabled: false, stop_reason: 'The pull request was closed.' })),
    'stopped',
  );
  // A resumed watch keeps the reason it was paused with, and is running.
  assert.equal(watchState(watch({ enabled: true, paused_reason: 'Paused yesterday.' })), 'enabled');
});

test('unreadable evidence is no evidence, not a crash and not a pass', () => {
  assert.equal(readCheckEvidence(undefined), null);
  assert.equal(readCheckEvidence('[]'), null);
  assert.equal(readCheckEvidence('{}'), null);
  assert.equal(readCheckEvidence('not json'), null);
  const parsed = readCheckEvidence(checks('failed'));
  assert.equal(parsed?.state, 'failed');
  assert.equal(parsed?.checks.length, 2);
  // A row with no name is dropped, because a check nobody can name is not a
  // check a person can act on.
  const unnamed = readCheckEvidence(
    JSON.stringify({ state: 'failed', checks: [{ conclusion: 'failure' }] }),
  );
  assert.equal(unnamed?.checks.length, 0);
  assert.equal(
    readCheckEvidence(JSON.stringify({ state: 'passed', checks: [{ name: 'a' }], total: 9 }))
      ?.total,
    9,
  );
});

test('mergeability that was never read is not a promise that there is no conflict', () => {
  assert.equal(readConflictEvidence('{}'), null);
  assert.equal(readConflictEvidence('{"mergeable":true}'), null);
  const read = readConflictEvidence(
    JSON.stringify({
      number: 17,
      head_sha: 'abc',
      base_ref: 'main',
      base_sha: 'def',
      mergeable: false,
      mergeable_known: true,
      merge_state: 'dirty',
    }),
  );
  assert.equal(read?.mergeable, false);
  assert.equal(read?.baseRef, 'main');
  assert.equal(read?.mergeState, 'dirty');
});

test('the checks that did not pass are the ones Eli acts on', () => {
  const evidence = readCheckEvidence(checks('failed'));
  const failing = failingChecks(evidence);
  assert.deepEqual(
    failing.map((c) => c.name),
    ['lint'],
  );
  assert.equal(checkOutcome(failing[0]!), 'failure');
  assert.equal(checkOutcome(evidence!.checks[0]!), 'passed');
  // A check still running has no outcome yet, and is not a failure.
  assert.equal(
    checkOutcome({ name: 'x', status: 'in_progress', conclusion: '', app: '', url: '' }),
    'in_progress',
  );
  assert.equal(failingChecks(null).length, 0);
});

test('an observation that failed is reported as unknown rather than as clean', () => {
  const before = summariseWatch(watch({ check_state: 'unknown', check_evidence: '[]' }), NOW);
  assert.equal(before.observed, false);
  assert.equal(before.activity, 'No successful observation yet');
  assert.ok(before.attention.some((line) => line.includes('No checks have been read')));

  const after = summariseWatch(
    watch({
      check_state: 'unknown',
      check_evidence: '[]',
      last_observed_at: '2026-11-06T11:50:00Z',
    }),
    NOW,
  );
  assert.equal(after.activity, 'The last check read failed');
  assert.ok(after.attention.some((line) => line.includes('could not be read')));
  // And the "next" line never claims there is nothing to do.
  assert.ok(!after.next.startsWith('Nothing'));
});

test('failed checks say how many attempts are used and what is next', () => {
  const first = summariseWatch(
    watch({ check_state: 'failed', check_evidence: checks('failed'), chain_attempts: 1 }),
    NOW,
  );
  assert.equal(first.activity, `Repairing failed checks; 1 attempt of ${MAX_ATTEMPTS} used`);
  assert.ok(first.intended.startsWith('Repair the failed checks'));
  assert.equal(first.attention.length, 0);

  const stopped = summariseWatch(
    watch({
      check_state: 'failed',
      check_evidence: checks('failed'),
      chain_attempts: MAX_ATTEMPTS,
      chain_stopped_reason:
        'Eli has made three repair attempts for this watched pull request. A person needs to look at the remaining failure.',
    }),
    NOW,
  );
  assert.equal(stopped.attention.length, 1);
  assert.ok(stopped.next.startsWith('A person needs to look'));
  // A stopped chain is not an intention to try again.
  assert.ok(!stopped.intended.includes('Repair the failed'));
});

test('a watch that only follows conflicts reports its mergeability and not its checks', () => {
  const summary = summariseWatch(
    watch({
      checks: false,
      check_state: undefined,
      conflict_evidence: JSON.stringify({
        base_ref: 'main',
        mergeable: false,
        mergeable_known: true,
      }),
    }),
    NOW,
  );
  assert.equal(summary.behaviour, 'conflicts');
  assert.equal(summary.observed, true);
  assert.equal(summary.activity, 'Conflicts with main');
  assert.ok(summary.intended.includes('resolve the supported conflicts'));
  assert.ok(summary.next.includes('checked again before anything is published'));

  const unknown = summariseWatch(watch({ checks: false, conflict_evidence: '{}' }), NOW);
  assert.equal(unknown.observed, false);
  assert.equal(unknown.activity, 'No successful observation yet');
});

test('a paused or stopped watch keeps its reasons and asks to be resumed', () => {
  const paused = summariseWatch(
    watch({ enabled: false, paused_reason: 'Paused by an administrator.' }),
    NOW,
  );
  assert.equal(paused.state, 'paused');
  assert.equal(paused.activity, 'Paused by an administrator.');
  assert.equal(paused.intended, 'Nothing, until watching is resumed.');
  assert.ok(paused.next.startsWith('Resume'));

  const stopped = summariseWatch(
    watch({ enabled: false, stop_reason: 'The pull request was closed.' }),
    NOW,
  );
  assert.equal(stopped.state, 'stopped');
  assert.equal(stopped.activity, 'The pull request was closed.');
  assert.deepEqual(stopped.attention, ['Watching was stopped.']);
});

test('a repository adds its watches up and carries what needs a person to its own line', () => {
  const one = watch({ pull_number: 1, check_state: 'passed', check_evidence: checks('passed') });
  const two = watch({
    pull_number: 2,
    check_state: 'failed',
    check_evidence: checks('failed'),
    chain_attempts: 2,
    chain_stopped_reason: 'A person needs to look.',
    last_observed_at: '2026-11-06T11:00:00Z',
  });
  const three = watch({ id: 'watch-3', pull_number: 3, enabled: false, stop_reason: 'Closed.' });
  const summary = summariseRepository(decision(), [one, two, three], NOW);
  assert.equal(summary.watching, 2);
  assert.equal(summary.stopped, 1);
  assert.equal(summary.attention, 1);
  assert.equal(summary.activity, '1 pull request needs a person');
  assert.equal(summary.observedAt, '2026-11-06T11:00:00Z');
  assert.equal(summary.behaviour, 'babysit');
  assert.equal(summary.state, 'enabled');
});

test('a repository with no decision is judged by its watches alone', () => {
  const summary = summariseRepository(
    null,
    [
      watch({
        checks: true,
        conflicts: false,
        check_state: 'passed',
        check_evidence: checks('passed'),
      }),
    ],
    NOW,
  );
  assert.equal(summary.behaviour, 'checks');
  assert.equal(summary.state, 'enabled');
  assert.equal(summary.watching, 1);

  const pausedRepo = summariseRepository(null, [watch({ enabled: false })], NOW);
  assert.equal(pausedRepo.state, 'paused');
  assert.equal(pausedRepo.activity, 'Not admitting new pull requests');
});

test('a repository whose observation failed says so instead of all clear', () => {
  const summary = summariseRepository(
    decision(),
    [watch({ check_state: 'unknown', check_evidence: '[]' })],
    NOW,
  );
  assert.equal(summary.observationFailed, true);
  assert.equal(summary.observedAt, undefined);
  assert.equal(summary.attention, 1);
});

test('repositories are one row each whatever the spelling of the name', () => {
  const names = repositoryNames(
    [decision({ repo: 'Acme/Widgets' })],
    [watch({ repo: 'acme/widgets' }), watch({ id: 'w2', repo: 'acme/site' })],
  );
  assert.deepEqual(names, ['Acme/Widgets', 'acme/site']);
});

test('what needs a person is listed before what is being repaired', () => {
  const quiet = watch({ pull_number: 1, check_state: 'passed', check_evidence: checks('passed') });
  const failing = watch({
    id: 'w2',
    pull_number: 2,
    check_state: 'failed',
    check_evidence: checks('failed'),
  });
  const stopped = watch({ id: 'w3', pull_number: 3, enabled: false, stop_reason: 'Closed.' });
  const attention = watch({
    id: 'w4',
    pull_number: 4,
    check_state: 'unknown',
    check_evidence: '[]',
    last_observed_at: '2026-11-06T11:00:00Z',
  });
  assert.deepEqual(
    watchingOrder([quiet, failing, stopped, attention], NOW).map((w) => w.pull_number),
    [4, 2, 1, 3],
  );
});

test('an empty list is quiet only when both reads answered', () => {
  assert.equal(
    watchingState({ loaded: false, failed: false, repositories: 0, watches: 0 }),
    'loading',
  );
  assert.equal(
    watchingState({ loaded: true, failed: true, repositories: 0, watches: 0 }),
    'failed',
  );
  assert.equal(
    watchingState({ loaded: true, failed: false, repositories: 0, watches: 0 }),
    'quiet',
  );
  assert.equal(
    watchingState({ loaded: true, failed: false, repositories: 1, watches: 0 }),
    'working',
  );
  assert.equal(
    watchingState({ loaded: true, failed: false, repositories: 0, watches: 1 }),
    'working',
  );
});

test('a pause promises what happens to work already under way', () => {
  const paused = pauseWatchCopy(watch());
  assert.ok(paused.description.includes('acme/widgets #17'));
  assert.ok(paused.inFlight.includes('stops before it changes the branch'));
  assert.ok(paused.inFlight.includes('already publishing finishes'));
  const stopped = stopWatchCopy(watch());
  assert.ok(stopped.description.includes('cannot be resumed'));
  assert.ok(stopped.inFlight.includes('Admit the pull request again'));

  const repository = pauseRepositoryCopy(decision(), [
    watch({ pull_number: 1 }),
    watch({ id: 'w2', pull_number: 2 }),
    watch({ id: 'w3', pull_number: 3, enabled: false }),
  ]);
  // Only the ones still being followed are counted, because the paused one is
  // already not being followed.
  assert.ok(repository.inFlight.startsWith('2 pull requests already being followed'));
  const all = stopAllCopy([watch({ pull_number: 1 }), watch({ id: 'w2', pull_number: 2 })]);
  assert.equal(all.title, 'Stop watching 2 pull requests');
  assert.equal(all.confirm, 'Stop 2 watches');
  assert.ok(all.inFlight.includes('keep their settings'));
});
