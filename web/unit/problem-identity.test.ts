import { test } from 'node:test';
import assert from 'node:assert/strict';
import { problemKey } from '../src/lib/problems/identity.ts';
import type { Problem } from '../src/lib/api/types.ts';

const problem = (over: Partial<Problem> = {}): Problem => ({
  code: 'pool.no_capacity',
  severity: 'error',
  title: 'pool zoomies-linux-x64 has 21 jobs waiting and nowhere to run them',
  target_kind: 'pool',
  target_id: 'pool_x64',
  ...over,
});

// The count in the title moves every reconciliation pass. If it were part of
// the key, a snooze made at 21 jobs would stop covering the same outage the
// moment a 22nd job queued -- the exact nagging the dismissals exist to stop.
test('a pool.no_capacity problem keeps its identity as the queue length changes', () => {
  const before = problemKey(
    problem({ title: 'pool zoomies-linux-x64 has 21 jobs waiting and nowhere to run them' }),
  );
  const after = problemKey(
    problem({ title: 'pool zoomies-linux-x64 has 22 jobs waiting and nowhere to run them' }),
  );
  assert.equal(before, after);
});

// pool.dangerous is raised once per weakened setting on the same pool, so two
// of them share a code and a target and the title is the only thing telling
// them apart -- folding the title out of the key entirely would collapse
// "host docker socket mounted" and "runners execute as root" into one problem.
test('two pool.dangerous problems on the same pool keep separate identities', () => {
  const socket = problemKey(
    problem({
      code: 'pool.dangerous',
      title: 'pool spaniel: host docker socket mounted: any job on this pool can become root',
    }),
  );
  const root = problemKey(
    problem({
      code: 'pool.dangerous',
      title: 'pool spaniel: runners execute as root inside the container',
    }),
  );
  assert.notEqual(socket, root);
});

test('two problems for different pools never collide regardless of code', () => {
  const first = problemKey(problem({ target_id: 'pool_x64' }));
  const second = problemKey(problem({ target_id: 'pool_arm64' }));
  assert.notEqual(first, second);
});
