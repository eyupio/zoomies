import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  addDays,
  bucketIndex,
  calendar,
  calendarWindow,
  describe,
  emptyBucket,
  failureLevel,
  fillWindow,
  foldJob,
  headline,
  hourGrid,
  level,
  mergeHistories,
  newSeen,
  paint,
  rangeWindow,
  scaleOf,
  summarise,
  type ActivityBucket,
} from '../src/lib/insights/activity.ts';

function bucket(from: string, fields: Partial<ActivityBucket> = {}): ActivityBucket {
  return { ...emptyBucket(from), ...fields };
}

test('a calendar of weeks ends today and starts on a Monday, in local time', () => {
  // A Wednesday, so the current week is three days in and four days short.
  const now = new Date(2026, 8, 9, 15, 30);
  const w = calendarWindow(4, now);
  assert.equal(w.from.getDay(), 1, 'starts on a Monday');
  assert.equal(w.from.getHours(), 0, 'starts at local midnight');
  assert.equal(w.from.getDate(), 17, 'three weeks before the Monday of this week');
  assert.equal(w.days, 24, 'three whole weeks and the three days of this one');
  // Elapsed rather than calendar time: exactly `days` buckets of 24 hours.
  assert.equal(w.to.getTime() - w.from.getTime(), 24 * 86_400_000);
});

test('bucket i is calendar day i even across a clock change', () => {
  // The last Sunday of October is when British clocks go back, so this window
  // has a 25-hour day in it and the elapsed buckets drift an hour behind local
  // midnight after it. The calendar places by index, so a bucket still lands on
  // the day it is mostly in.
  const first = new Date(2026, 9, 19); // Monday 19 October
  const buckets = fillWindow([], first, 14, 'day');
  const columns = calendar(buckets, first);
  assert.equal(columns.length, 2);
  const secondMonday = columns[1]?.cells[0];
  assert.equal(secondMonday?.date.getDate(), 26);
  assert.equal(secondMonday?.date.getMonth(), 9);
  const lastSunday = columns[1]?.cells[6];
  assert.equal(lastSunday?.date.getDate(), 1);
  assert.equal(lastSunday?.date.getMonth(), 10, 'the fortnight ends on 1 November');
  // October is named over the first column and not again over the second: a
  // label marks where a month begins, and the second week is the same month.
  assert.ok(columns[0]?.label?.startsWith('Oct'));
  assert.equal(columns[1]?.label, null);
});

test('a window that starts mid-week leaves the earlier weekdays blank', () => {
  const first = new Date(2026, 8, 9); // a Wednesday
  const columns = calendar(fillWindow([], first, 5, 'day'), first);
  assert.equal(columns.length, 1);
  assert.deepEqual(
    columns[0]?.cells.map((c) => (c ? c.date.getDate() : null)),
    [null, null, 9, 10, 11, 12, 13],
  );
});

test('the month label gives way when two would sit side by side', () => {
  // 31 August is a Monday; the next column starts 7 September. Both begin a
  // new month by the rule, and the first yields so the labels do not overlap.
  const first = new Date(2026, 7, 31);
  const columns = calendar(fillWindow([], first, 21, 'day'), first);
  assert.deepEqual(
    columns.map((c) => c.label),
    [null, 'Sept', null].map((l) => (l === 'Sept' ? columns[1]?.label : l)),
  );
  assert.ok(columns[1]?.label?.startsWith('Sep'));
});

test('the API is laid over an empty window, so a quiet fortnight still has its squares', () => {
  const first = new Date(2026, 8, 7);
  const series = fillWindow(
    [bucket(new Date(2026, 8, 9).toISOString(), { succeeded: 3, failed: 1 })],
    first,
    14,
    'day',
  );
  assert.equal(series.length, 14);
  assert.equal(series[2]?.succeeded, 3);
  assert.equal(series[2]?.failed, 1);
  assert.equal(series[3]?.succeeded, 0);
  // A bucket from outside the window is ignored rather than crashing the page.
  const outside = fillWindow([bucket(new Date(2026, 0, 1).toISOString())], first, 2, 'day');
  assert.equal(outside.length, 2);
});

test('groups are added back together into one fleet series, oldest first', () => {
  const at = new Date(2026, 8, 9).toISOString();
  const later = new Date(2026, 8, 10).toISOString();
  const rows = [
    { key: 'a', history: [bucket(later, { queued: 1 }), bucket(at, { succeeded: 2 })] },
    { key: 'b', history: [bucket(at, { succeeded: 1, failed: 1, capacity_samples: 60 })] },
  ];
  const merged = mergeHistories(rows as never);
  assert.equal(merged.length, 2);
  assert.equal(merged[0]?.from, at);
  assert.equal(merged[0]?.succeeded, 3);
  assert.equal(merged[0]?.failed, 1);
  assert.equal(merged[0]?.capacity_samples, 60);
  assert.equal(merged[1]?.queued, 1);
});

test('hourly buckets are placed by the local hour they started at', () => {
  const first = new Date(2026, 8, 9);
  const series = fillWindow(
    [bucket(new Date(2026, 8, 9, 14).toISOString(), { succeeded: 4 })],
    first,
    48,
    'hour',
  );
  const rows = hourGrid(series);
  assert.equal(rows.length, 2);
  assert.equal(rows[0]?.cells[14]?.bucket.succeeded, 4);
  assert.equal(rows[0]?.cells[13]?.bucket.succeeded, 0);
  assert.equal(rows[1]?.date.getDate(), 10);
});

test('darkness is quarters of the busiest square, and a failure share has fixed bands', () => {
  assert.equal(level(0, 100), 0);
  assert.equal(level(1, 100), 1, 'anything at all is at least the first step');
  assert.equal(level(25, 100), 1);
  assert.equal(level(26, 100), 2);
  assert.equal(level(100, 100), 4);
  assert.equal(level(5, 0), 0, 'a window with nothing in it has no darkest square');
  assert.equal(failureLevel(0, 50), 0);
  assert.equal(failureLevel(1, 50), 1);
  assert.equal(failureLevel(10, 50), 2);
  assert.equal(failureLevel(20, 50), 3);
  assert.equal(failureLevel(25, 50), 4);
  assert.equal(failureLevel(1, 0), 4, 'a failure with no completions is all failure');
});

test('outcomes paint a square by what finished, and say which kind it is', () => {
  const series = [
    bucket('a', { succeeded: 40 }),
    bucket('b', { succeeded: 9, failed: 1 }),
    bucket('c', { queued: 3 }),
    bucket('d'),
    bucket('e', { succeeded: 10 }),
  ];
  const scale = scaleOf(series);
  assert.equal(scale.completed, 40);
  assert.deepEqual(paint(series[0]!, 'outcomes', scale), {
    kind: 'healthy',
    tone: 'idle',
    level: 4,
  });
  assert.deepEqual(paint(series[4]!, 'outcomes', scale), {
    kind: 'healthy',
    tone: 'idle',
    level: 1,
  });
  // One failure in ten is the lightest red, and still red: a failure is never
  // hidden inside a busy green day.
  assert.deepEqual(paint(series[1]!, 'outcomes', scale), {
    kind: 'failing',
    tone: 'danger',
    level: 2,
  });
  assert.deepEqual(paint(series[2]!, 'outcomes', scale), {
    kind: 'waiting',
    tone: 'pending',
    level: 4,
  });
  assert.equal(paint(series[3]!, 'outcomes', scale).kind, 'quiet');
});

test('the other modes recolour by their own figure', () => {
  const series = [
    bucket('a', {
      queued: 8,
      execution_seconds: 3600,
      capacity_samples: 100,
      capacity_reached: 30,
    }),
    bucket('b', { queued: 2, execution_seconds: 900 }),
  ];
  const scale = scaleOf(series);
  assert.deepEqual(paint(series[0]!, 'queue', scale), {
    kind: 'active',
    tone: 'pending',
    level: 4,
  });
  assert.deepEqual(paint(series[1]!, 'queue', scale), {
    kind: 'active',
    tone: 'pending',
    level: 1,
  });
  assert.deepEqual(paint(series[1]!, 'runtime', scale), { kind: 'active', tone: 'busy', level: 1 });
  assert.deepEqual(paint(series[0]!, 'capacity', scale), {
    kind: 'active',
    tone: 'pending',
    level: 3,
  });
  // No telemetry is not the same as never blocked.
  assert.equal(paint(series[1]!, 'capacity', scale).kind, 'unsampled');
});

test('a square says everything it knows in one sentence', () => {
  const at = new Date(2026, 8, 9, 14);
  const b = bucket(at.toISOString(), {
    succeeded: 35,
    failed: 2,
    cancelled: 1,
    queued: 40,
    started: 39,
    execution_seconds: 23_040,
    capacity_samples: 1440,
    capacity_reached: 3,
  });
  assert.equal(headline(b), '38 jobs finished, 2 failed');
  // The date is spelled the way the operator's locale spells it, so the test
  // asks the same formatter rather than assuming a British one.
  const day = new Intl.DateTimeFormat(undefined, {
    weekday: 'long',
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  }).format(at);
  assert.equal(
    describe(b, at, 'day'),
    `${day}. 38 jobs finished, 2 failed: 35 succeeded, 2 failed, 1 cancelled or skipped, 0 unknown; 40 queued, 39 started; 6.4 h executing; 3 of 1,440 observed pool-minutes at capacity.`,
  );
  const clock = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' });
  const short = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short' }).format(at);
  assert.ok(
    describe(b, at, 'hour').startsWith(
      `${short}, ${clock.format(at)} to ${clock.format(new Date(at.getTime() + 3_600_000))}.`,
    ),
  );
  assert.equal(headline(bucket('x', { queued: 2 })), '2 jobs queued, none finished');
  assert.equal(headline(bucket('x')), 'No jobs');
  const totals = summarise([b, bucket('y', { queued: 1 })]);
  assert.equal(totals.completed, 38);
  assert.equal(totals.active, 2);
});

test('a job frame lands in the square its moment belongs to, and only once', () => {
  const first = new Date(2026, 8, 7);
  const series = fillWindow([], first, 7, 'day');
  const fetched = new Date(2026, 8, 9, 12).getTime();
  const seen = newSeen();
  const queuedAt = new Date(2026, 8, 9, 13).toISOString();
  const startedAt = new Date(2026, 8, 9, 13, 1).toISOString();
  const completedAt = new Date(2026, 8, 9, 13, 5).toISOString();

  const queued = foldJob(
    series,
    'day',
    { id: 'job_1', state: 'queued', queued_at: queuedAt } as never,
    fetched,
    seen,
  );
  assert.ok(queued, 'a queued frame after the fetch counts');
  assert.equal(queued[2]?.queued, 1);
  assert.notEqual(queued, series, 'the series is replaced, never mutated');
  assert.equal(series[2]?.queued, 0);

  const again = foldJob(
    queued,
    'day',
    { id: 'job_1', state: 'queued', queued_at: queuedAt } as never,
    fetched,
    seen,
  );
  assert.equal(again, null, 'the same frame twice changes nothing');

  const done = foldJob(
    queued,
    'day',
    {
      id: 'job_1',
      state: 'completed',
      queued_at: queuedAt,
      started_at: startedAt,
      completed_at: completedAt,
      conclusion: 'failure',
    } as never,
    fetched,
    seen,
  );
  assert.ok(done);
  assert.equal(done[2]?.queued, 1, 'still counted once as queued');
  assert.equal(done[2]?.started, 1);
  assert.equal(done[2]?.failed, 1);

  // A job that finished before the fetch is already in the series.
  const old = foldJob(
    done,
    'day',
    {
      id: 'job_0',
      state: 'completed',
      queued_at: new Date(2026, 8, 9, 8).toISOString(),
      started_at: new Date(2026, 8, 9, 8).toISOString(),
      completed_at: new Date(2026, 8, 9, 9).toISOString(),
      conclusion: 'success',
    } as never,
    fetched,
    seen,
  );
  assert.equal(old, null);

  // A runner that died under a job is the fleet's failure, however GitHub
  // concluded it.
  const lost = foldJob(
    done,
    'day',
    {
      id: 'job_2',
      state: 'completed',
      completed_at: completedAt,
      conclusion: 'cancelled',
      runner_fault: 'runner stopped',
    } as never,
    fetched,
    seen,
  );
  assert.equal(lost?.[2]?.failed, 2);
  assert.equal(lost?.[2]?.cancelled, 0);
});

test('a moment outside the series has no square', () => {
  const first = new Date(2026, 8, 7);
  const series = fillWindow([], first, 7, 'day');
  assert.equal(bucketIndex(series, addDays(first, 7).getTime(), 'day'), -1);
  assert.equal(bucketIndex(series, first.getTime() - 1, 'day'), -1);
  assert.equal(bucketIndex(series, addDays(first, 6).getTime() + 1, 'day'), 6);
});

test('a quick range is the last N days, today included, in the width asked for', () => {
  const now = new Date(2026, 8, 9, 15, 30);
  const week = rangeWindow(7, 'hour', now);
  assert.equal(week.from.getDate(), 3, 'six days ago');
  assert.equal(week.from.getHours(), 0, 'from local midnight');
  assert.equal(week.count, 7 * 24, 'a week of hours');
  assert.equal(week.to.getTime() - week.from.getTime(), 7 * 24 * 3_600_000);
  assert.equal(week.interval, 'hour');
  const today = rangeWindow(1, 'hour', now);
  assert.equal(today.from.getDate(), 9);
  assert.equal(today.count, 24);
  const month = rangeWindow(30, 'day', now);
  assert.equal(month.count, 30);
  assert.equal(month.from.getMonth(), 7, 'starts in August');
  assert.equal(month.from.getDate(), 11);
});
