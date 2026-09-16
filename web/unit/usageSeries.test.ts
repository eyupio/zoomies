/*
 * The usage chart's figures. The arithmetic is here rather than in the
 * component so that "this day has not happened yet" and "the fleet had
 * nowhere to put a runner" can be tested without a browser.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { emptyBucket, type ActivityBucket } from '../src/lib/insights/activity.ts';
import {
  capacityRuns,
  inProgress,
  usageFigures,
  usageFormat,
  usageLines,
} from '../src/lib/insights/usageSeries.ts';

const DAY = 86_400_000;
const first = new Date(2026, 8, 14).getTime();
const day = (i: number, fill: Partial<ActivityBucket> = {}): ActivityBucket => ({
  ...emptyBucket(new Date(first + i * DAY).toISOString()),
  ...fill,
});
const figure = (key: string) => usageFigures('jobs', true).filter((f) => f.key === key);

/*
 * A report runs to the end of its last day, so a window chosen today has
 * hours in it that have not happened. Drawn as zeroes, they are a line that
 * dives to the floor and stays there -- a fleet that stopped, rather than one
 * whose week is not over.
 */
test('an interval that has not begun is a gap, and the one being filled is drawn', () => {
  const buckets = [day(0, { queued: 4 }), day(1, { queued: 2 }), day(2), day(3)];
  const [queued] = usageLines(buckets, figure('queued'), first + DAY + 3_600_000);
  assert.deepEqual(
    queued?.points.map((p) => p.value),
    [4, 2, null, null],
  );
  // The newest reading is the interval being filled, which is what the label
  // at the end of the line and the meters below are showing.
  assert.deepEqual(queued?.last, { i: 1, value: 2 });
  // An interval with nothing in it is still an interval: zero jobs queued on
  // a day that happened is news, and a gap is not.
  const quiet = usageLines([day(0)], figure('queued'), first + DAY);
  assert.deepEqual(
    quiet[0]?.points.map((p) => p.value),
    [0],
  );

  assert.equal(inProgress(day(1), 'day', first + DAY + 60_000), true);
  assert.equal(inProgress(day(0), 'day', first + DAY + 60_000), false);
  assert.equal(inProgress(day(2), 'day', first + DAY + 60_000), false);
});

/*
 * A runner idles on behalf of a pool and never on behalf of a repository, so
 * at those groupings its hours are not the group's to claim. The table drops
 * the column; the chart drops the line, rather than drawing somebody else's
 * time as this repository's.
 */
test('allocated runner time is only drawn where it can be attributed', () => {
  assert.deepEqual(
    usageFigures('runtime', true).map((f) => f.key),
    ['execution', 'allocated'],
  );
  assert.deepEqual(
    usageFigures('runtime', false).map((f) => f.key),
    ['execution'],
  );
  // The job counts belong to whoever ran them, at every grouping.
  assert.deepEqual(usageFigures('jobs', false).length, usageFigures('jobs', true).length);

  // Hours, not seconds: the axis is read in the unit the tiles above use.
  const [executing] = usageLines(
    [day(0, { execution_seconds: 5400 })],
    usageFigures('runtime', true),
    first + DAY,
  );
  assert.equal(executing?.points[0]?.value, 1.5);
});

/*
 * The band is the fleet trend's starvation shading, drawn from what the
 * report already counts: the minutes a pool had work to place and nowhere to
 * put it.
 */
test('the capacity band is a spell, and an interval nobody observed ends it', () => {
  const buckets = [
    day(0, { capacity_samples: 10, capacity_reached: 2 }),
    day(1, { capacity_samples: 10, capacity_reached: 1 }),
    day(2, { capacity_samples: 10 }),
    day(3, { capacity_samples: 0 }),
    day(4, { capacity_samples: 10, capacity_reached: 5 }),
  ];
  assert.deepEqual(capacityRuns(buckets), [
    { from: 0, to: 1 },
    { from: 4, to: 4 },
  ]);
  // A report with no capacity observations at all -- a repository grouping,
  // or a fleet that predates the counting -- shades nothing rather than
  // claiming the fleet was never short.
  assert.deepEqual(capacityRuns([day(0), day(1)]), []);
  assert.deepEqual(capacityRuns([]), []);
});

/* Colour alone never tells two lines apart; nor does the stroke alone. */
test('no two figures on one chart are drawn the same way, and hours keep their fractions', () => {
  for (const measure of ['jobs', 'runtime'] as const) {
    const drawn = new Set<string>();
    for (const f of usageFigures(measure, true)) {
      const mark = `${f.tone} ${f.dash}`;
      assert.ok(!drawn.has(mark), `${f.key} is drawn exactly like another figure`);
      drawn.add(mark);
      assert.match(f.tone, /^var\(--z-/, `${f.key} must take a token, not a colour`);
    }
  }
  // A count of jobs is whole; a quarter of an hour of runner time is not.
  assert.equal(usageFormat('jobs')(3), usageFormat('runtime')(3));
  assert.notEqual(usageFormat('jobs')(1.23456), usageFormat('runtime')(1.23456));
  assert.ok(!usageFormat('runtime')(1.23456).includes('456'));
});
