import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  DEFAULT_SIGNALS,
  SIGNALS,
  foldMinutes,
  hostSignals,
  leadSignal,
  poolSignals,
  minuteSeries,
  mergeSamples,
  signalLines,
  signalValue,
  starvedRuns,
} from '../src/lib/insights/signals.ts';
import { seriesPeak, seriesRuns } from '../src/lib/insights/plot.ts';

test('pool queue and ceiling use authoritative stats, not a partial runner list', () => {
  const [p] = poolSignals(
    [{ id: 'p', priority: 0, enabled: true, max_runners: 4, queued_jobs: 0, counts: { live: 1 } }],
    { pools: [{ pool_id: 'p', live: 4, max: 4, queued: 12, idle: 1, busy: 3 }] },
  );
  assert.equal(p?.queued, 12);
  assert.equal(p?.atCeiling, true);
  assert.equal(p?.headroom, 0);
  assert.equal(
    poolSignals(
      [
        {
          id: 'p',
          priority: 0,
          enabled: false,
          max_runners: 4,
          queued_jobs: 3,
          counts: { live: 4 },
        },
      ],
      null,
    )[0]?.atCeiling,
    false,
  );
});
test('host headroom respects offline, cordon and incompatible states and preserves unknown telemetry', () => {
  for (const override of [{ healthy: false }, { cordoned: true }, { incompatible: true }])
    assert.equal(
      hostSignals({ healthy: true, capacity: 4, active_runners: 1, ...override }).eligible,
      false,
    );
  const unknown = hostSignals({ healthy: true, capacity: 4, active_runners: 1 });
  assert.equal(unknown.free, 3);
  assert.equal(unknown.cpu, null);
  assert.equal(unknown.diskFree, null);
  const measured = hostSignals({
    healthy: true,
    capacity: 4,
    active_runners: 5,
    resources_known: true,
    reserved_known: true,
    allocatable_cpus: 4,
    reserved_cpus: 6,
    disk_total_mb: 100,
    disk_free_mb: 5,
  });
  assert.equal(measured.free, 0);
  assert.equal(measured.cpu, 150);
  assert.equal(measured.diskFree, 5);
});
test('minute history preserves gaps and the latest streamed observation without dropping older minutes', () => {
  const now = Date.parse('2026-09-12T12:00:00Z');
  const series = minuteSeries(
    [
      { at: now - 120000, value: 5 },
      { at: now, value: 0 },
    ],
    now,
    3,
  );
  assert.deepEqual(
    series.map((p) => p.value),
    [5, null, 0],
  );
  const merged = mergeSamples(
    [
      { at: new Date(now - 60000).toISOString(), fleet_queued_jobs: 8 },
      { at: new Date(now).toISOString(), fleet_queued_jobs: 1 },
    ],
    Array.from({ length: 1000 }, (_, i) => ({
      at: new Date(now + i).toISOString(),
      fleet_queued_jobs: i,
    })),
    now,
  );
  assert.equal(merged.length, 2);
  assert.equal(merged[0]?.fleet_queued_jobs, 8);
  assert.equal(merged[1]?.fleet_queued_jobs, 999);
});

test('a wider window keeps its minutes, and a folded interval carries its peak', () => {
  const now = Date.now();
  const merged = mergeSamples(
    [{ at: new Date(now - 5 * 3_600_000).toISOString(), fleet_queued_jobs: 8 }],
    [],
    now,
    6 * 60,
  );
  assert.equal(merged.length, 1, 'a sample five hours back is inside a six-hour window');
  assert.equal(mergeSamples(merged, [], now).length, 0, 'and outside the hour');

  const folded = foldMinutes(
    [
      { at: 0, value: 5 },
      { at: 60_000, value: null },
      { at: 120_000, value: 3 },
      { at: 180_000, value: null },
      { at: 240_000, value: null },
      { at: 300_000, value: null },
      { at: 360_000, value: 2 },
    ],
    3,
  );
  assert.deepEqual(folded, [
    { at: 0, value: 5 },
    { at: 180_000, value: null },
    { at: 360_000, value: 2 },
  ]);
});

/*
 * The fleet trend draws several figures at once, and a fold takes the peak of
 * each one separately: a folded point is five readings from the same interval,
 * not one minute's snapshot of five figures. Reading them from one chosen
 * minute -- the minute the lead figure peaked in -- would have said the queue
 * was twelve while nine runners sat idle, which never happened.
 */
test('every figure carries its own peak through a fold, and a missed minute stays a gap', () => {
  const now = Date.parse('2026-09-12T12:02:59Z');
  const at = (minutes: number) => new Date(now - minutes * 60_000).toISOString();
  const lines = signalLines(
    [
      { at: at(2), fleet_queued_jobs: 12, idle_runners: 0 },
      { at: at(1), fleet_queued_jobs: 1, idle_runners: 9 },
    ],
    ['queue', 'idle'],
    false,
    now,
    3,
    3,
  );
  assert.deepEqual(
    lines.map((l) => l.series.key),
    ['queue', 'idle'],
  );
  assert.equal(lines[0]?.points[0]?.value, 12);
  assert.equal(lines[1]?.points[0]?.value, 9);

  // A minute nobody sampled is a gap in the line and never a zero: a
  // controller that was down and an empty queue are opposite news.
  const minutes = signalLines([{ at: at(2), fleet_queued_jobs: 4 }], ['queue'], false, now, 3, 1);
  assert.deepEqual(
    minutes[0]?.points.map((p) => p.value),
    [4, null, null],
  );
  assert.deepEqual(minutes[0]?.last, { i: 0, value: 4 });
  assert.deepEqual(seriesRuns(minutes[0]!.points), [[{ i: 0, value: 4 }]]);
});

/* Whose jobs the two job figures count is the caller's choice; runner counts
   are always this fleet's, because nobody else's runners are visible here. */
test('the job figures follow the fleet or the whole organisation, and the runner figures do not', () => {
  const sample = {
    at: '2026-09-12T12:00:00Z',
    queued_jobs: 20,
    fleet_queued_jobs: 3,
    running_jobs: 40,
    fleet_running_jobs: 5,
    idle_runners: 2,
    busy_runners: 5,
    total_runners: 9,
  };
  assert.equal(signalValue(sample, 'queue', false), 3);
  assert.equal(signalValue(sample, 'queue', true), 20);
  assert.equal(signalValue(sample, 'running', true), 40);
  for (const key of ['idle', 'busy', 'live'] as const)
    assert.equal(signalValue(sample, key, true), signalValue(sample, key, false));
  // A figure the controller did not report is unknown, not zero.
  assert.equal(signalValue({ at: sample.at }, 'live', false), null);
  // The lead figure is the first chosen one in the chips' order, whatever
  // order they were switched on in.
  assert.equal(leadSignal(['live', 'queue'])?.key, 'queue');
  assert.equal(leadSignal([]), null);
  assert.ok(DEFAULT_SIGNALS.every((k) => SIGNALS.some((s) => s.key === k)));
});

/*
 * The shaded band is this chart's pressure line: jobs waiting with nothing
 * idle to take them. A spell of it is one band rather than a picket fence,
 * and an interval nobody sampled ends the spell rather than joining across it
 * -- the fleet may well have recovered inside the minute nobody watched.
 */
test('starvation is queued work with nothing free, and an unobserved interval breaks the spell', () => {
  const line = (key: 'queue' | 'idle', values: Array<number | null>) => ({
    series: SIGNALS.find((s) => s.key === key)!,
    points: values.map((value, i) => ({ at: i * 60_000, value })),
    last: null,
  });
  assert.deepEqual(starvedRuns(line('queue', [1, 2, 0, 4, 5]), line('idle', [0, 0, 0, 0, 0])), [
    { from: 0, to: 1 },
    { from: 3, to: 4 },
  ]);
  assert.deepEqual(starvedRuns(line('queue', [1, null, 1]), line('idle', [0, 0, 0])), [
    { from: 0, to: 0 },
    { from: 2, to: 2 },
  ]);
  // Judged a minute at a time and folded afterwards, so zooming out keeps the
  // band: a quarter of an hour in which any minute ran short is shaded, where
  // folding the figures first would have compared the deepest queue with the
  // most idle runners the interval ever had and found nothing wrong.
  assert.deepEqual(
    starvedRuns(line('queue', [0, 5, 0, 0, 0, 0, 0, 0]), line('idle', [2, 0, 3, 3, 3, 3, 3, 3]), 4),
    [{ from: 0, to: 0 }],
  );
  // Two spells with a clear interval between them stay two bands rather than
  // one long one; two that land in neighbouring intervals are one band, which
  // is what shading a spell rather than its minutes means.
  const twelve = (starved: number[]) =>
    Array.from({ length: 12 }, (_, i) => (starved.includes(i) ? 0 : 3));
  assert.deepEqual(starvedRuns(line('queue', Array(12).fill(1)), line('idle', twelve([0, 9])), 4), [
    { from: 0, to: 0 },
    { from: 2, to: 2 },
  ]);
  assert.deepEqual(starvedRuns(line('queue', Array(12).fill(1)), line('idle', twelve([0, 5])), 4), [
    { from: 0, to: 1 },
  ]);
  // An idle runner, or an unknown count of them, is not starvation.
  assert.deepEqual(starvedRuns(line('queue', [3]), line('idle', [1])), []);
  assert.deepEqual(starvedRuns(line('queue', [3]), line('idle', [null])), []);
  assert.deepEqual(starvedRuns(null, line('idle', [0])), []);

  const peak = seriesPeak([line('queue', [1, 7, 2])]);
  assert.equal(peak?.value, 7);
  assert.equal(peak?.i, 1);
  assert.equal(seriesPeak([]), null);
});
