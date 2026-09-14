import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  hostSeries,
  lineRuns,
  liveSample,
  mergeHostSamples,
  metricText,
  metricValue,
  overflowCeiling,
  timeTicks,
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
  const merged = mergeHostSamples(history, live, now, 60);
  assert.deepEqual([...merged.keys()].sort(), ['host_a', 'host_b']);
  const a = merged.get('host_a') ?? [];
  assert.equal(a.length, 1);
  assert.equal(a[0]?.cpu_percent, 20);

  const series = hostSeries(a, 'cpu', now, 60, 1);
  assert.equal(series.length, 60);
  assert.equal(series[58]?.value, 20);
  assert.equal(series[59]?.value, null);
  // Folded into five-minute peaks, the gap either side stays a gap.
  const folded = hostSeries(a, 'cpu', now, 60, 5);
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
