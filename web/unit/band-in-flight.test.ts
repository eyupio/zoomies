import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { WorkflowRun } from '../src/lib/api/types.ts';
import { inFlight, MAX_FLIGHT_ROWS } from '../src/lib/bands/in-flight.ts';

const run = (repo: string, jobs: WorkflowRun['jobs']): WorkflowRun =>
  ({ id: `${repo}-${Math.random()}`, repo, jobs }) as unknown as WorkflowRun;

// Two runs in one repository are one row, and the band's figures (running,
// waiting) are the sums across the rows.
test('runs are summed by repository, the busiest first, running before waiting', () => {
  const data = inFlight([
    run('acme/site', { in_progress: 1, queued: 1 }),
    run('acme/api', { in_progress: 1, queued: 2, waiting: 1 }),
    run('acme/api', { in_progress: 1, completed: 3 }),
    run('acme/docs', { completed: 2 }),
  ]);
  assert.deepEqual(
    data.rows.map((r) => [r.repo, r.squares]),
    [
      ['acme/api', ['running', 'running', 'waiting', 'waiting', 'waiting']],
      ['acme/site', ['running', 'waiting']],
    ],
  );
  assert.equal(data.rows[0]!.note, '2 running · 3 waiting');
  assert.equal(data.summary, '3 running · 4 waiting');
});

test('a run with nothing in flight draws nothing, and repositories past the limit become a count', () => {
  assert.equal(inFlight([run('a/b', { completed: 4 })]).rows.length, 0);
  assert.equal(inFlight([]).summary, 'Nothing in flight');
  const many = Array.from({ length: MAX_FLIGHT_ROWS + 2 }, (_, i) => run(`r/${i}`, { queued: 1 }));
  assert.equal(inFlight(many).more, 2);
});
