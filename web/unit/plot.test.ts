import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  countAxis,
  nearestSeries,
  seriesLine,
  seriesRuns,
  spreadLabels,
  timeTicks,
  trendFrame,
  trendIndexAtX,
  trendX,
  trendY,
  type Series,
} from '../src/lib/insights/plot.ts';

/** A figure and its readings, as a panel hands them to a plot. */
function line(key: string, values: Array<number | null>) {
  const series: Series = { key, label: key, hint: key, tone: 'red', dash: '' };
  return seriesLine(
    series,
    values.map((value, i) => ({ at: i * 60_000, value })),
  );
}

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
  // A usage report is read over a fortnight or a quarter, and there the step
  // is whole days: about five labels, every one of them a local midnight.
  for (const days of [14, 30, 90, 365]) {
    const window = timeTicks(end - days * 86_400_000, end, 5);
    assert.ok(window.length <= 6, `${days} days: ${window.length} labels`);
    assert.ok(window.length >= 3, `${days} days: ${window.length} labels`);
    for (const at of window) assert.equal(new Date(at).getHours(), 0);
  }
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

test('a fractional axis is for the units that divide, and never for a count of jobs', () => {
  // Twenty minutes of runner time against a whole-hour axis is a flat line on
  // the baseline; in quarters of an hour it is a chart.
  const third = countAxis(0.33, 4, true);
  assert.deepEqual(third.values, [0, 0.1, 0.2, 0.3, 0.4]);
  assert.equal(third.ceiling, 0.4);
  // The gridlines are the numbers an operator would have written: a tenth of
  // an hour is 0.1 and never 0.30000000000000004.
  for (const value of third.values) assert.equal(String(value).length <= 3, true, String(value));
  // Past an hour it rounds like any other axis, fractional or not.
  assert.deepEqual(countAxis(7, 4, true).values, countAxis(7).values);
  // The same peak asked for in whole units stays whole.
  assert.deepEqual(countAxis(0.33).values, [0, 1]);
  // Nothing to scale to is still an axis, so an empty chart has a baseline.
  assert.deepEqual(countAxis(0, 4, true), { ceiling: 1, values: [0, 1] });
});

/*
 * The runs are what makes a gap a gap: an interval nobody sampled is left out
 * of the stroke, so the line passes over it rather than diving to zero. An
 * outage and an empty queue are opposite news.
 */
test('a line is stroked in runs of what was observed, and the newest reading is found', () => {
  assert.deepEqual(seriesRuns(line('q', [1, null, 2, 3]).points), [
    [{ i: 0, value: 1 }],
    [
      { i: 2, value: 2 },
      { i: 3, value: 3 },
    ],
  ]);
  assert.deepEqual(seriesRuns([]), []);
  // The newest reading is the last observed one, not the last point: a line
  // that stopped reporting is labelled at the value it stopped at.
  assert.deepEqual(line('q', [1, 4, null]).last, { i: 1, value: 4 });
  assert.equal(line('q', [null, null]).last, null);
});

/*
 * The frame is drawn at the width it has, one unit to a pixel, rather than a
 * fixed picture stretched to fit: the old chart drew 760 units into whatever
 * width the panel had, so its eleven-unit axis text arrived on a wide screen
 * at nineteen pixels and read as a heading.
 */
test('a trend is drawn one unit to a pixel, and its gutter grows with the figures', () => {
  const wide = trendFrame(1200, { digits: 3 });
  assert.equal(wide.W, 1200);
  assert.equal(wide.narrow, false);
  // A four-figure axis needs a wider gutter than a one-figure one, or "1,500"
  // prints over the gridlines.
  assert.ok(trendFrame(1200, { digits: 5 }).LEFT > wide.LEFT);
  // A phone gets a taller drawing and never a narrower one than it can hold.
  assert.ok(trendFrame(360).narrow);
  assert.ok(trendFrame(360).H > wide.H);
  assert.equal(trendFrame(40).W, 280);

  const x = trendX(wide, 60);
  const y = trendY(wide, 20);
  assert.equal(x(0), wide.LEFT);
  assert.equal(x(59), wide.RIGHT);
  assert.equal(y(0), wide.BOTTOM);
  assert.equal(y(20), wide.TOP);
  // A figure past the ceiling is drawn at it rather than off the top.
  assert.equal(y(200), wide.TOP);
  // The pointer lands on the point it is nearest, and never off the ends.
  assert.equal(trendIndexAtX(wide, 60, x(12) + 2), 12);
  assert.equal(trendIndexAtX(wide, 60, -500), 0);
  assert.equal(trendIndexAtX(wide, 60, 5000), 59);

  // Pointing at a line is how a reader asks what it is: the nearest within
  // the tolerance answers, and nothing answers from too far away.
  const lines = [line('queue', [2]), line('idle', [18])];
  assert.equal(nearestSeries(lines, 0, y(18), y, 10), 'idle');
  assert.equal(nearestSeries(lines, 0, y(2), y, 10), 'queue');
  assert.equal(nearestSeries(lines, 0, y(10), y, 10), null);
  // A gap answers nothing: there is no line there to point at.
  assert.equal(nearestSeries([line('queue', [null])], 0, y(0), y, 10), null);
});
