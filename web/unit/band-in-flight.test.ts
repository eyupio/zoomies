import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { WorkflowRun } from '../src/lib/api/types.ts';
import {
  distinctRuns,
  inFlight,
  MAX_FLIGHT_ROWS,
  readAllPages,
} from '../src/lib/bands/in-flight.ts';

const run = (repo: string, jobs: WorkflowRun['jobs']): WorkflowRun => ({ repo, jobs });

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

// The picture is a sum of every active run, so a page smaller than the fleet's
// would make it a sample that reads as a total.
test('a list longer than a page is read to its end', async () => {
  const offsets: number[] = [];
  const all = await readAllPages<number>(async (offset) => {
    offsets.push(offset);
    return {
      items: Array.from({ length: Math.min(500, 1200 - offset) }, (_, i) => offset + i),
      total: 1200,
    };
  });
  assert.deepEqual(offsets, [0, 500, 1000]);
  assert.equal(all.length, 1200);
  assert.equal(all[1199], 1199);
});

test('a list that fits in one page is one request', async () => {
  const offsets: number[] = [];
  const all = await readAllPages<string>(async (offset) => {
    offsets.push(offset);
    return { items: ['a', 'b'], total: 2 };
  });
  assert.deepEqual(offsets, [0]);
  assert.deepEqual(all, ['a', 'b']);
});

// A list that shrinks while it is read must not hold the loop open: the total
// said more, and the page after it had nothing.
test('an empty page ends the read whatever the total says', async () => {
  const offsets: number[] = [];
  const all = await readAllPages<string>(async (offset) => {
    offsets.push(offset);
    return offset === 0 ? { items: ['a', 'b', 'c'], total: 10 } : { items: [], total: 10 };
  });
  assert.deepEqual(offsets, [0, 3]);
  assert.deepEqual(all, ['a', 'b', 'c']);
});

test('a page with no items is an empty list', async () => {
  assert.deepEqual(await readAllPages<string>(async () => ({ total: 0 })), []);
});

// A run queued while the list is being read pushes the last row of one page
// onto the top of the next.
test('a run seen on two pages is counted once', () => {
  const twice = { repo: 'acme/api', github_run_id: 7, jobs: { in_progress: 2 } };
  const runs = distinctRuns([
    twice,
    { repo: 'acme/api', github_run_id: 8, jobs: { queued: 1 } },
    twice,
    { repo: 'acme/site', github_run_id: 7, jobs: { queued: 1 } },
  ]);
  assert.equal(runs.length, 3);
  const data = inFlight(runs);
  assert.equal(data.summary, '2 running · 2 waiting');
});

test('runs with no identity to compare are all kept', () => {
  assert.equal(distinctRuns([{ jobs: { queued: 1 } }, { jobs: { queued: 1 } }]).length, 2);
});
