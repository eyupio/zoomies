import { test } from 'node:test';
import assert from 'node:assert/strict';
import { countAxis, spreadLabels, timeTicks } from '../src/lib/insights/plot.ts';

test('labels fall on round local times, not on evenly spaced odd minutes', () => {
  // A day ending at 11:04 is labelled at the multiples of six hours.
  const end = new Date(2026, 2, 1, 11, 4).getTime();
  const start = end - 24 * 60 * 60_000;
  const ticks = timeTicks(start, end, 5).map((at) => new Date(at).getHours());
  assert.deepEqual(ticks, [12, 18, 0, 6]);
  // An hour is labelled at the quarter hours.
  const hour = timeTicks(end - 60 * 60_000, end, 5).map((at) => new Date(at).getMinutes());
  assert.deepEqual(hour, [15, 30, 45, 0]);
  // A minute is labelled at the tens of seconds, and five minutes by the minute.
  const minute = timeTicks(end - 50_000, end, 5).map((at) => new Date(at).getSeconds());
  assert.deepEqual(minute, [10, 20, 30, 40, 50, 0]);
  const five = timeTicks(end - 290_000, end, 5).map((at) => new Date(at).getMinutes());
  assert.deepEqual(five, [0, 1, 2, 3, 4]);
});

test('labels at the ends of the lines are pushed apart, and back up from the bottom', () => {
  // Three lines ending within a few pixels of each other get three labels a
  // label's height apart, in the order the lines are in.
  assert.deepEqual(spreadLabels([100, 102, 104], 14, 0, 200), [100, 114, 128]);
  // The callers' order is kept whatever order the lines end in.
  assert.deepEqual(spreadLabels([104, 100], 14, 0, 200), [114, 100]);
  // Against the bottom the stack walks back up rather than running off.
  assert.deepEqual(spreadLabels([196, 198], 14, 0, 200), [186, 200]);
  // Lines far enough apart are labelled exactly where they end.
  assert.deepEqual(spreadLabels([20, 120], 14, 0, 200), [20, 120]);
  assert.deepEqual(spreadLabels([], 14, 0, 200), []);
});

test('a count axis is labelled in round numbers, and never in halves of a job', () => {
  // Seventeen queued jobs is a chart to twenty, labelled every five: an axis
  // that simply stopped at the peak printed 4.25 and 8.5 on its gridlines.
  const seventeen = countAxis(17);
  assert.equal(seventeen.ceiling, 20);
  assert.deepEqual(seventeen.values, [0, 5, 10, 15, 20]);
  // A quiet fleet still gets an axis with room above the line, so a queue of
  // one is a step rather than the whole height of the panel.
  assert.deepEqual(countAxis(0).values, [0, 1]);
  assert.deepEqual(countAxis(2).values, [0, 1, 2]);
  // Big fleets round the same way, by powers of ten rather than by widening
  // the step until the labels collide.
  assert.equal(countAxis(1200).ceiling, 1500);
  assert.deepEqual(countAxis(1200).values, [0, 500, 1000, 1500]);
  // The peak is always under the ceiling, whatever it is.
  for (const peak of [1, 3, 7, 9, 40, 41, 99, 101, 2500, 9999])
    assert.ok(countAxis(peak).ceiling >= peak, `${peak}`);
});
