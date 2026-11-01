import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { FleetSample } from '../src/lib/api/types.ts';
import { sparkPath, trendPoints, trendWords, withNow } from '../src/lib/bands/trend.ts';

const MINUTE = 60_000;
const now = Date.UTC(2026, 10, 1, 12, 0, 0);
const sample = (minutesAgo: number, extra: Partial<FleetSample>): FleetSample => ({
  at: new Date(now - minutesAgo * MINUTE).toISOString(),
  queued_jobs: 9,
  running_jobs: 9,
  ...extra,
});

test("the points are this fleet's figures where a sample has them, within the window, oldest first", () => {
  const points = trendPoints(
    [
      sample(5, { fleet_queued_jobs: 2, fleet_running_jobs: 3 }),
      sample(70, { fleet_queued_jobs: 8 }),
      sample(30, {}),
    ],
    now,
  );
  assert.deepEqual(
    points.map((p) => [p.queued, p.running]),
    [
      [9, 9],
      [2, 3],
    ],
  );
  assert.ok(points[0]!.at < points[1]!.at);
});

// The sentence under the band says what is waiting now; the line's last point
// must be the same number, whichever minute the last sample was written in.
test('the newest point is now, replacing a sample from the same minute', () => {
  const base = trendPoints([sample(0, { fleet_queued_jobs: 4 })], now);
  const points = withNow(base, { at: now + 10_000, queued: 6, running: 1 });
  assert.equal(points.length, 1);
  assert.equal(points[0]!.queued, 6);
});

test('a path spans the window in x and the maximum in y, and closes to the baseline', () => {
  const { line, area } = sparkPath(
    [
      { at: 0, value: 0 },
      { at: 50, value: 10 },
      { at: 100, value: 5 },
    ],
    { width: 104, height: 24, from: 0, to: 100, max: 10 },
  );
  assert.equal(line, 'M2 22 L52 2 L102 12');
  assert.equal(area, `${line} L102 22 L2 22 Z`);
  assert.deepEqual(sparkPath([], { width: 10, height: 10, from: 0, to: 1, max: 1 }), {
    line: '',
    area: '',
  });
});

test('the reading compares now with an hour ago', () => {
  assert.equal(trendWords([2, 4, 6], 'Waiting'), 'Waiting up from 2 an hour ago');
  assert.equal(trendWords([9, 3], 'running'), 'running down from 9 an hour ago');
  assert.equal(trendWords([6, 6], 'Waiting'), 'Waiting steady at 6');
  assert.equal(trendWords([], 'Waiting'), 'No Waiting samples yet');
});
