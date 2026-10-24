import { test } from 'node:test';
import assert from 'node:assert/strict';
import { usageLine, usageStats, usageTotals } from '../src/lib/bands/usage-stats.ts';
import type { UsageRow } from '../src/lib/api/types.ts';

const row = (over: Partial<UsageRow> = {}): UsageRow => ({
  key: 'pool-a',
  jobs: 10,
  jobs_started: 8,
  jobs_completed: 8,
  job_execution_seconds: 36_000,
  allocated_runner_seconds: 72_000,
  average_queue_wait_seconds: 120,
  peak_concurrency: 3,
  succeeded: 7,
  failed: 1,
  cancelled: 0,
  unknown: 0,
  ...over,
});

test('the totals add every row, and a wait is weighted by the jobs that started', () => {
  const total = usageTotals([
    row(),
    row({ key: 'pool-b', jobs_started: 2, average_queue_wait_seconds: 30 }),
  ]);
  assert.equal(total.jobs, 20);
  assert.equal(total.started, 10);
  assert.equal(total.completed, 16);
  assert.equal(total.succeeded, 14);
  assert.equal(total.seconds, 72_000);
  assert.equal(total.allocated, 144_000);
  // 8 jobs waited 120s and 2 waited 30s.
  assert.equal(total.wait, 1020);
  assert.equal(total.wait / total.started, 102);
});

test('the figures are the page: queued, success, failures, hours and the average wait', () => {
  const stats = usageStats(usageTotals([row()]), true);
  assert.deepEqual(
    stats.map((s) => [s.label, s.value]),
    [
      ['Jobs queued', '10'],
      ['Success rate', '87.5%'],
      ['Failed jobs', '1'],
      ['Execution hours', '10.00'],
      ['Average queue wait', '2m 00s'],
      ['Runner busy share', '50.0%'],
    ],
  );
});

test('every figure points at the report on the page when a section is given', () => {
  const stats = usageStats(usageTotals([row()]), true, 'usage-detail');
  assert.deepEqual(
    stats.map((s) => s.section),
    Array.from({ length: stats.length }, () => 'usage-detail'),
  );
});

test('a grouping that cannot attribute runner time has no busy share', () => {
  const stats = usageStats(usageTotals([row({ allocated_runner_seconds: null })]), false);
  assert.equal(
    stats.some((s) => s.label === 'Runner busy share'),
    false,
  );
});

test('a missing success rate is the shared placeholder, and a short runner history is not claimed as a share', () => {
  const stats = usageStats(
    usageTotals([
      row({
        jobs_completed: 0,
        succeeded: 0,
        failed: 0,
        allocated_runner_seconds: 1,
      }),
    ]),
    true,
  );
  assert.equal(stats[1]?.value, '--');
  assert.equal(stats[5]?.value, '--');
});

test('the sentence is made from the same totals, and an empty range says so', () => {
  const quiet = usageLine(usageTotals([]), 'today');
  assert.equal(quiet.headline, 'Nothing was queued in this range.');
  assert.equal(quiet.accent, '');

  const line = usageLine(usageTotals([row()]), 'today');
  assert.equal(line.headline, '10 jobs queued today.');
  assert.equal(line.accent, '87.5% succeeded.');
  assert.equal(line.lede, '1 failed. 10.00 execution hours.');
});

test('a range with jobs but no finished ones does not invent a success rate', () => {
  const line = usageLine(
    usageTotals([row({ jobs_completed: 0, succeeded: 0, failed: 0 })]),
    'in this range',
  );
  assert.equal(line.accent, 'Nothing has finished yet.');
});
