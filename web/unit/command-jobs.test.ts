import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Job, Runner } from '../src/lib/api/types.ts';
import {
  FINISHED_SHOWN,
  FINISHED_WINDOW_MS,
  PROBLEMS_SHOWN,
  WAITING_SHOWN,
  finishedOutcome,
  mergeFinished,
  mergeWaiting,
  pruneFinished,
  replayFrames,
  unlistedAfter,
  waitingSummary,
} from '../src/lib/command/jobs-merge.ts';

const now = Date.parse('2026-11-10T14:00:00Z');
const ago = (ms: number) => new Date(now - ms).toISOString();

const queued = (id: string, waitedMs: number, extra: Partial<Job> = {}): Job => ({
  id,
  state: 'queued',
  matched: true,
  pool_id: 'p1',
  pool_name: 'precision',
  queued_at: ago(waitedMs),
  ...extra,
});
const finished = (id: string, endedMs: number, extra: Partial<Job> = {}): Job => ({
  id,
  state: 'completed',
  conclusion: 'success',
  matched: true,
  runner_id: `r-${id}`,
  queued_at: ago(endedMs + 60_000),
  started_at: ago(endedMs + 30_000),
  completed_at: ago(endedMs),
  duration_ms: 30_000,
  ...extra,
});
const ids = (list: readonly Job[]) => list.map((j) => j.id);

test('the rail shows five waiting, three problems and five finished, over ten minutes', () => {
  assert.equal(WAITING_SHOWN, 5);
  assert.equal(PROBLEMS_SHOWN, 3);
  assert.equal(FINISHED_SHOWN, 5);
  assert.equal(FINISHED_WINDOW_MS, 10 * 60 * 1000);
});

test('a queued job joins the waiting list, longest wait first', () => {
  let list = mergeWaiting([], queued('a', 10_000));
  list = mergeWaiting(list, queued('b', 90_000));
  list = mergeWaiting(list, queued('c', 30_000));
  assert.deepEqual(ids(list), ['b', 'c', 'a']);
});

test('a job that starts leaves the waiting list', () => {
  const list = [queued('a', 10_000), queued('b', 20_000)];
  const started = { ...queued('a', 10_000), state: 'in_progress' as const, started_at: ago(0) };
  assert.deepEqual(ids(mergeWaiting(list, started)), ['b']);
});

// The Queue page's default view leaves out what an operator removed and what
// somebody else's runners will run; a frame must not put either back.
test('a removed job and a job for somebody else’s runners never join the waiting list', () => {
  assert.deepEqual(ids(mergeWaiting([], queued('a', 1, { provisioning: 'deleted' }))), []);
  assert.deepEqual(
    ids(mergeWaiting([], queued('b', 1, { matched: false, hosted: true, pool_id: '' }))),
    [],
  );
  // A job nothing claims and that names nobody else's runners is still ours.
  assert.deepEqual(
    ids(mergeWaiting([], queued('c', 1, { matched: false, hosted: false, pool_id: '' }))),
    ['c'],
  );
});

test('a job that finishes joins the finished list, newest first', () => {
  let list = mergeFinished([], finished('a', 120_000), now);
  list = mergeFinished(list, finished('b', 10_000), now);
  list = mergeFinished(list, finished('c', 60_000), now);
  assert.deepEqual(ids(list), ['b', 'c', 'a']);
});

test('a finished job older than ten minutes is dropped', () => {
  const list = mergeFinished([], finished('old', FINISHED_WINDOW_MS + 1_000), now);
  assert.deepEqual(ids(list), []);
  const kept = [finished('new', 1_000), finished('stale', FINISHED_WINDOW_MS + 1)];
  assert.deepEqual(ids(pruneFinished(kept, now)), ['new']);
});

test('the same job arriving twice is one row', () => {
  const once = mergeWaiting([], queued('a', 5_000));
  assert.deepEqual(ids(mergeWaiting(once, queued('a', 5_000))), ['a']);
  const done = mergeFinished([], finished('b', 5_000), now);
  assert.deepEqual(ids(mergeFinished(done, finished('b', 5_000, { conclusion: 'failure' }), now)), [
    'b',
  ]);
  assert.equal(
    mergeFinished(done, finished('b', 5_000, { conclusion: 'failure' }), now)[0]!.conclusion,
    'failure',
  );
});

test('a frame that would move a job backwards is ignored', () => {
  // Deliveries are at-least-once and can arrive out of order: a late
  // in_progress frame for a job already finished must not take it off the
  // list, and must not change the row it has.
  const done = mergeFinished([], finished('a', 5_000), now);
  const late: Job = { ...finished('a', 5_000), state: 'in_progress', completed_at: null };
  const after = mergeFinished(done, late, now);
  assert.deepEqual(ids(after), ['a']);
  assert.equal(after[0]!.state, 'completed');

  // The same on the waiting list: a queued job does not go back to waiting.
  const list = mergeWaiting([], queued('b', 5_000));
  const held: Job = { ...queued('b', 5_000), state: 'waiting' };
  assert.equal(mergeWaiting(list, held)[0]!.state, 'queued');
});

test('a finished job a hosted runner ran is not one of the fleet’s', () => {
  const theirs = finished('a', 1_000, { matched: false, runner_id: undefined });
  assert.deepEqual(ids(mergeFinished([], theirs, now)), []);
});

test('a success is a tick, a failure a triangle, and the rest the neutral mark', () => {
  assert.deepEqual(finishedOutcome(finished('a', 1)), { kind: 'succeeded', label: 'Succeeded' });
  for (const conclusion of ['failure', 'timed_out', 'startup_failure']) {
    assert.deepEqual(finishedOutcome(finished('b', 1, { conclusion })), {
      kind: 'failed',
      label: 'Failed',
    });
  }
  assert.deepEqual(finishedOutcome(finished('c', 1, { conclusion: 'cancelled' })), {
    kind: 'neutral',
    label: 'Cancelled',
  });
  assert.deepEqual(finishedOutcome(finished('d', 1, { conclusion: 'skipped' })), {
    kind: 'neutral',
    label: 'Skipped',
  });
});

const starting = (pool_id: string): Runner => ({
  id: `s-${pool_id}`,
  pool_id,
  state: 'provisioning',
});

test('a job a starting runner is on its way for is not one waiting without a slot', () => {
  const jobs = [queued('a', 60_000), queued('b', 30_000)];
  const summary = waitingSummary(jobs, [starting('p1')], 0, now);
  // One runner is starting for the pool, so one of the two is covered.
  assert.equal(summary.withoutSlot, 1);
  assert.equal(summary.longestWaitMs, 60_000);
  assert.equal(summary.pool, 'precision');
});

test('with a free slot somewhere, only a job no pool claims is waiting without one', () => {
  const unclaimed = queued('u', 10_000, { pool_id: '', pool_name: '', matched: false });
  const summary = waitingSummary([queued('a', 60_000), unclaimed], [], 2, now);
  assert.equal(summary.withoutSlot, 1);
  assert.equal(summary.pool, null);
});

test('a paused or cancelling job raises no demand, so it waits for no slot', () => {
  const jobs = [
    queued('a', 60_000, { provisioning: 'paused' }),
    queued('b', 30_000, { cancel_requested_at: ago(1_000) }),
  ];
  const summary = waitingSummary(jobs, [], 0, now);
  assert.equal(summary.withoutSlot, 0);
  // The cancelling job is still waiting on the list; the paused one is held
  // on purpose, and is the next test's subject.
  assert.equal(summary.longestWaitMs, 30_000);
});

// A paused job waits because an operator said so. Counting its wait would
// have the verdict report a long queue that nobody but the operator made.
test('a paused job is not the longest wait', () => {
  const jobs = [queued('a', 600_000, { provisioning: 'paused' }), queued('b', 20_000)];
  assert.equal(waitingSummary(jobs, [], 0, now).longestWaitMs, 20_000);
  const onlyPaused = [queued('a', 600_000, { provisioning: 'paused' })];
  assert.equal(waitingSummary(onlyPaused, [], 0, now).longestWaitMs, 0);
});

test('nothing waiting is no wait at all', () => {
  assert.deepEqual(waitingSummary([], [], 0, now), {
    withoutSlot: 0,
    longestWaitMs: 0,
    pool: null,
  });
});

// After a reconnect the lists are fetched again, and the stream's replay
// lands while that fetch is in flight. The fetch answers from before those
// frames, so laying it down last would put a job that started in the gap back
// on the waiting list.
test('a frame received during a fetch wins over the fetched row for the same job', () => {
  const fetched = {
    waiting: [queued('a', 60_000), queued('b', 30_000)],
    finished: [finished('c', 120_000)],
  };
  const startedA: Job = { ...queued('a', 60_000), state: 'in_progress', started_at: ago(1_000) };
  const doneB = finished('b', 2_000, { queued_at: ago(30_000) });
  const failedC = finished('c', 120_000, { conclusion: 'failure' });
  const out = replayFrames(fetched, [startedA, doneB, failedC], now);
  assert.deepEqual(ids(out.waiting), []);
  assert.deepEqual(ids(out.finished), ['b', 'c']);
  assert.equal(out.finished[1]!.conclusion, 'failure');
});

test('with no frames held, the fetched lists stand as they are', () => {
  const fetched = { waiting: [queued('a', 60_000)], finished: [finished('c', 1_000)] };
  const out = replayFrames(fetched, [], now);
  assert.deepEqual(ids(out.waiting), ['a']);
  assert.deepEqual(ids(out.finished), ['c']);
});

// The waiting read is capped, and the count past the cap comes from its
// total. A frame for one of those jobs (queued before the fetch, never held)
// that puts it in the list moves it from the count into the list; counting it
// in both would read one too many until the next fetch. A job queued after
// the fetch was in no total, so it only adds to the list.
test('a job past the cap that joins the list moves out of the unlisted count, never below nought', () => {
  const fetchedAt = now - 60_000;
  const held = [queued('a', 120_000)];
  // Queued ten minutes ago, before the fetch: it was one of the unlisted.
  assert.equal(unlistedAfter(3, held, queued('z', 600_000), fetchedAt), 2);
  assert.equal(unlistedAfter(0, held, queued('z', 600_000), fetchedAt), 0);
  // Already held: nothing moves, however many times its frame arrives.
  assert.equal(unlistedAfter(3, held, queued('a', 120_000), fetchedAt), 3);
  // Queued after the fetch: a new job, which no total counted.
  assert.equal(unlistedAfter(3, held, queued('n', 1_000), fetchedAt), 3);
  // A frame that does not put the job in the list moves nothing.
  assert.equal(
    unlistedAfter(
      3,
      held,
      queued('s', 600_000, { state: 'in_progress', started_at: ago(5) }),
      fetchedAt,
    ),
    3,
  );
});
