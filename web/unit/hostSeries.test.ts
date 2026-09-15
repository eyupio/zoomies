import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  METRICS,
  PRESSURE,
  WINDOWS,
  bridgeFor,
  hostLines,
  hostSeries,
  indexAtX,
  leadMetric,
  lineRuns,
  liveSample,
  mergeHostSamples,
  metricText,
  metricValue,
  nearestHost,
  overflowCeiling,
  peakOf,
  plotFrame,
  scaleX,
  scaleY,
  spreadLabels,
  timeTicks,
  windowSlots,
} from '../src/lib/insights/hostSeries.ts';
import type { Host, HostSample } from '../src/lib/api/types.ts';

const sample = (over: Partial<HostSample> = {}): HostSample => ({
  host_id: 'host_a',
  at: '2026-03-01T09:00:00Z',
  capacity: 4,
  active_runners: 3,
  cpu_percent: 72,
  load_average_1m: 12,
  cpus: 8,
  memory_mb: 16384,
  memory_available_mb: 4096,
  allocatable_cpus: 7,
  allocatable_memory_mb: 15000,
  reserved_cpus: 6,
  reserved_memory_mb: 12000,
  disk_total_mb: 1000,
  disk_free_mb: 250,
  ...over,
});

test('every metric is a share of the machine, and only load may pass 100', () => {
  const s = sample();
  assert.equal(metricValue(s, 'cpu'), 72);
  assert.equal(metricValue(s, 'memory'), 75);
  assert.equal(metricValue(s, 'load'), 150);
  assert.equal(metricValue(s, 'slots'), 75);
  assert.equal(Math.round(metricValue(s, 'cpu_committed') ?? 0), 86);
  assert.equal(metricValue(s, 'memory_committed'), 80);
  assert.equal(metricValue(s, 'disk'), 75);
  assert.equal(metricText(s, 'slots'), '3 / 4 slots');
});

test('a figure the host never reported is a gap, not a zero', () => {
  // An agent too old to measure: absent, and drawn as nothing rather than as
  // an idle machine.
  const s = sample({
    cpu_percent: undefined,
    memory_available_mb: undefined,
    load_average_1m: undefined,
    reserved_cpus: undefined,
    disk_total_mb: 0,
  });
  assert.equal(metricValue(s, 'cpu'), null);
  assert.equal(metricValue(s, 'memory'), null);
  assert.equal(metricValue(s, 'load'), null);
  assert.equal(metricValue(s, 'cpu_committed'), null);
  assert.equal(metricValue(s, 'disk'), null);
  assert.equal(metricText(s, 'cpu'), 'Not measured');
  // A measured zero is a zero.
  assert.equal(metricValue(sample({ memory_available_mb: 16384 }), 'memory'), 0);
  assert.equal(metricValue(sample({ capacity: 0 }), 'slots'), null);
});

test('the live point comes from the host view and drops a stale measurement', () => {
  const host: Host = {
    id: 'host_a',
    capacity: 6,
    effective_capacity: 3,
    active_runners: 2,
    usage: { cpu_percent: 40, memory_available_mb: 100 },
    usage_fresh: false,
    resources_known: true,
    reserved_known: true,
    reserved_cpus: 4,
    allocatable_cpus: 8,
    cpus: 8,
    memory_mb: 1000,
  };
  const now = Date.UTC(2026, 2, 1, 9, 30);
  const live = liveSample(host, now);
  // A throttled host is measured against the slots it is actually taking.
  assert.equal(live.capacity, 3);
  assert.equal(live.cpu_percent, undefined);
  assert.equal(metricValue(live, 'cpu'), null);
  assert.equal(metricValue(live, 'cpu_committed'), 50);
  assert.equal(metricValue(liveSample({ ...host, usage_fresh: true }, now), 'cpu'), 40);
});

test('merging keeps one sample per host per minute and the newer input wins', () => {
  const now = Date.UTC(2026, 2, 1, 9, 59, 30);
  const history = [
    sample({ at: '2026-03-01T09:58:00Z', cpu_percent: 10 }),
    sample({ at: '2026-03-01T08:00:00Z', cpu_percent: 99 }), // outside the hour
    sample({ host_id: 'host_b', at: '2026-03-01T09:58:20Z', cpu_percent: 50 }),
  ];
  const live = [sample({ at: '2026-03-01T09:58:40Z', cpu_percent: 20 })];
  const merged = mergeHostSamples(history, live, now, 3600);
  assert.deepEqual([...merged.keys()].sort(), ['host_a', 'host_b']);
  const a = merged.get('host_a') ?? [];
  assert.equal(a.length, 1);
  assert.equal(a[0]?.cpu_percent, 20);

  const series = hostSeries(a, 'cpu', now, 3600, 60);
  assert.equal(series.length, 60);
  assert.equal(series[58]?.value, 20);
  assert.equal(series[59]?.value, null);
  // Folded into five-minute peaks, the gap either side stays a gap.
  const folded = hostSeries(a, 'cpu', now, 3600, 300);
  assert.equal(folded.length, 12);
  assert.equal(folded[11]?.value, 20);
  assert.equal(folded[10]?.value, null);
});

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

test('a short window keeps every ten-second slot and folds nothing away', () => {
  // The controller writes one sample a minute, but a heartbeat is thirty
  // seconds and the stream carries each one: a window drawn ten seconds to a
  // point shows both, where a minute-by-minute line kept only the later.
  const now = Date.UTC(2026, 2, 1, 9, 59, 55);
  const live = [
    sample({ at: '2026-03-01T09:59:00Z', cpu_percent: 10 }),
    sample({ at: '2026-03-01T09:59:30Z', cpu_percent: 40 }),
    sample({ at: '2026-03-01T09:59:34Z', cpu_percent: 45 }), // same slot as the one before
    sample({ at: '2026-03-01T09:58:50Z', cpu_percent: 99 }), // before the minute
  ];
  const fine = mergeHostSamples([], live, now, 60, 10).get('host_a') ?? [];
  assert.deepEqual(
    fine.map((s) => s.cpu_percent),
    [10, 45],
  );
  // By the minute the same samples are one point: the last one wins.
  const coarse = mergeHostSamples([], live, now, 60, 60).get('host_a') ?? [];
  assert.deepEqual(
    coarse.map((s) => s.cpu_percent),
    [45],
  );

  const series = hostSeries(fine, 'cpu', now, 60, 10);
  assert.equal(series.length, 6);
  assert.deepEqual(
    series.map((p) => p.value),
    [10, null, null, 45, null, null],
  );
  // The window ends on the current slot, so its right-hand edge is now.
  const slots = windowSlots(now, 60, 10);
  assert.equal(slots.end, Date.UTC(2026, 2, 1, 9, 59, 50));
  assert.equal(slots.start, Date.UTC(2026, 2, 1, 9, 59, 0));
  assert.equal(slots.count, 6);
  // And a wide window still ends on the current minute, a whole bucket back.
  const day = windowSlots(now, 86400, 900);
  assert.equal(day.end, Date.UTC(2026, 2, 1, 9, 59));
  assert.equal(day.count, 96);
  assert.equal(day.start, day.end - (96 * 900 - 60) * 1000);
});

test('a fine window bridges a minute of silence and a minute-wide one bridges a minute', () => {
  // At ten seconds a point the stored samples are six points apart and a
  // heartbeat three, so both join; ninety seconds, which is a lost host,
  // does not. By the minute the rule is the one the comment on BRIDGE gives.
  assert.equal(bridgeFor(10), 6);
  assert.equal(bridgeFor(60), 1);
  assert.equal(bridgeFor(900), 1);
  const at = (i: number) => Date.UTC(2026, 2, 1, 9, 0, i * 10);
  const v = (i: number, value: number | null) => ({ at: at(i), value });
  const points = [v(0, 10), ...Array.from({ length: 5 }, (_, i) => v(i + 1, null)), v(6, 20)];
  assert.deepEqual(
    lineRuns(points, bridgeFor(10)).map((run) => run.map((p) => p.i)),
    [[0, 6]],
  );
  const lost = [v(0, 10), ...Array.from({ length: 8 }, (_, i) => v(i + 1, null)), v(9, 20)];
  assert.deepEqual(
    lineRuns(lost, bridgeFor(10)).map((run) => run.map((p) => p.i)),
    [[0], [9]],
  );
  // Every window's span is a whole number of its points.
  for (const w of WINDOWS) assert.equal(w.seconds % w.bucket, 0, w.value);
});

test('a line is drawn across one missing minute and broken by two', () => {
  // The sampler a moment late for a minute, or a tab that slept through one,
  // is not a host that stopped reporting; a line broken at every such minute
  // would read as a machine flickering in and out of existence.
  const at = (i: number) => Date.UTC(2026, 2, 1, 9, i);
  const v = (i: number, value: number | null) => ({ at: at(i), value });
  const points = [v(0, 10), v(1, null), v(2, 30), v(3, null), v(4, null), v(5, 50), v(6, 60)];
  const runs = lineRuns(points);
  assert.deepEqual(
    runs.map((run) => run.map((p) => p.i)),
    [
      [0, 2],
      [5, 6],
    ],
  );
  // The bridged minute is not in the run: nothing claims a figure for it.
  assert.deepEqual(
    runs[0]?.map((p) => p.value),
    [10, 30],
  );
  // Leading and trailing gaps are no run at all.
  assert.deepEqual(
    lineRuns([v(0, null), v(1, 5), v(2, null)]).map((run) => run.map((p) => p.i)),
    [[1]],
  );
  assert.deepEqual(lineRuns([v(0, null), v(1, null)]), []);
  // With no bridging every gap breaks the line, which is what the fold's
  // wider intervals used to do too.
  assert.deepEqual(
    lineRuns(points, 0).map((run) => run.map((p) => p.i)),
    [[0], [2], [5, 6]],
  );
});

test('only load past the cores opens a lane, and its ceiling is a round number', () => {
  // Every other figure stops at 100, so a chart of them has no lane; a load
  // of 8.4 times the CPUs draws in a lane scaled to 850, not one that
  // stretches every other line into a strip.
  assert.equal(overflowCeiling(null), 0);
  assert.equal(overflowCeiling(100), 0);
  assert.equal(overflowCeiling(72), 0);
  assert.equal(overflowCeiling(101), 150);
  assert.equal(overflowCeiling(840), 850);
  assert.equal(overflowCeiling(Number.NaN), 0);
});

/* -- what the drawing is built from --------------------------------------- */

test("the lead metric is the first chosen one in the chips' order, whatever order it was chosen in", () => {
  // The headline, the end labels and the wash all follow one measurement,
  // and it has to be the same one however the operator reached the choice.
  assert.equal(leadMetric(['slots', 'cpu'])?.key, 'cpu');
  assert.equal(leadMetric(['disk', 'memory_committed'])?.key, 'memory_committed');
  assert.equal(leadMetric([]), null);
});

test('a line per chosen metric for every host that is not hidden, and colour follows the host', () => {
  const now = Date.parse('2026-03-01T09:00:00Z');
  const hosts = [{ id: 'host_a' }, { id: 'host_b' }, { id: 'host_c' }] as Host[];
  const byHost = new Map([
    ['host_a', [sample({ host_id: 'host_a', at: new Date(now).toISOString(), cpu_percent: 10 })]],
    ['host_c', [sample({ host_id: 'host_c', at: new Date(now).toISOString(), cpu_percent: 90 })]],
  ]);
  const metrics = METRICS.filter((m) => m.key === 'cpu' || m.key === 'slots');
  const lines = hostLines(hosts, ['host_b'], byHost, metrics, now, 3600, 60);
  assert.deepEqual(
    lines.map((l) => l.id),
    ['host_a:cpu', 'host_a:slots', 'host_c:cpu', 'host_c:slots'],
  );
  // Hiding the second host does not repaint the third: a reader who learnt
  // that host_c is the third colour is not misled by a filter.
  assert.equal(lines[2]?.tone, 'var(--z-chart-3)');
  assert.deepEqual(lines[2]?.last, { i: 59, value: 90 });
  assert.equal(lines[0]?.last?.value, 10);
});

test('the peak in view names the line and the point it came from', () => {
  const now = Date.parse('2026-03-01T09:00:00Z');
  const at = (minutesAgo: number) => new Date(now - minutesAgo * 60_000).toISOString();
  const byHost = new Map([
    [
      'host_a',
      [
        sample({ at: at(30), cpu_percent: 40 }),
        sample({ at: at(10), cpu_percent: 95 }),
        sample({ at: at(0), cpu_percent: 50 }),
      ],
    ],
    ['host_b', [sample({ host_id: 'host_b', at: at(5), cpu_percent: 80 })]],
  ]);
  const hosts = [{ id: 'host_a' }, { id: 'host_b' }] as Host[];
  const lines = hostLines(hosts, [], byHost, [METRICS[0]!], now, 3600, 60);
  const peak = peakOf(lines, 'cpu');
  assert.equal(peak?.line.host.id, 'host_a');
  assert.equal(peak?.value, 95);
  assert.equal(peak?.i, 49);
  assert.equal(peakOf(lines, 'memory'), null);
  assert.equal(PRESSURE, 85);
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

test('the frame is drawn one unit to a pixel, and stacked plots share their gutters', () => {
  const wide = plotFrame(1200);
  const phone = plotFrame(400);
  assert.equal(wide.W, 1200);
  assert.equal(wide.narrow, false);
  assert.equal(phone.narrow, true);
  assert.ok(phone.H > wide.H, 'a phone gets a taller drawing');
  // The gutters are the same width in every plot, so one x axis serves a
  // stack of per-host plots.
  const compact = plotFrame(1200, { compact: true, axis: false });
  assert.equal(compact.LEFT, wide.LEFT);
  assert.equal(compact.RIGHT, wide.RIGHT);
  assert.ok(compact.H < wide.H);
  assert.ok(compact.BOTTOM > wide.BOTTOM - wide.H + compact.H, 'no axis band without an axis');
  // A lane for load past the cores sits above the axis and pushes 100% down.
  const laned = plotFrame(1200, { lane: true });
  assert.equal(laned.AXIS_TOP, laned.TOP + laned.LANE);
  assert.equal(wide.AXIS_TOP, wide.TOP);
});

test('values map to the axis, and only past 100 into the lane', () => {
  const frame = plotFrame(800, { lane: true });
  const y = scaleY(frame, 200);
  assert.equal(y(0), frame.BOTTOM);
  assert.equal(y(100), frame.AXIS_TOP);
  assert.equal(y(200), frame.TOP);
  assert.equal(y(150), frame.TOP + frame.LANE / 2);
  // Without a lane a value past 100 is clipped to the top of the axis.
  assert.equal(scaleY(plotFrame(800), 0)(150), plotFrame(800).AXIS_TOP);
  const x = scaleX(frame, 5);
  assert.equal(x(0), frame.LEFT);
  assert.equal(x(4), frame.RIGHT);
  assert.equal(indexAtX(frame, 5, frame.LEFT), 0);
  assert.equal(indexAtX(frame, 5, frame.RIGHT), 4);
  assert.equal(indexAtX(frame, 5, frame.RIGHT + 500), 4);
  assert.equal(indexAtX(frame, 5, frame.LEFT + frame.SPAN / 2), 2);
});

test('pointing at a line names its host, and pointing at nothing names none', () => {
  const now = Date.parse('2026-03-01T09:00:00Z');
  const at = new Date(now).toISOString();
  const byHost = new Map([
    ['host_a', [sample({ host_id: 'host_a', at, cpu_percent: 20 })]],
    ['host_b', [sample({ host_id: 'host_b', at, cpu_percent: 80 })]],
  ]);
  const hosts = [{ id: 'host_a' }, { id: 'host_b' }] as Host[];
  const lines = hostLines(hosts, [], byHost, [METRICS[0]!], now, 3600, 60);
  const frame = plotFrame(800);
  const y = scaleY(frame, 0);
  assert.equal(nearestHost(lines, 59, y(78), y, 12), 'host_b');
  assert.equal(nearestHost(lines, 59, y(24), y, 12), 'host_a');
  assert.equal(nearestHost(lines, 59, y(50), y, 12), null);
  // A minute nobody observed has no line to point at.
  assert.equal(nearestHost(lines, 10, y(80), y, 12), null);
});
