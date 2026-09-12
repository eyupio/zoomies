import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  hostSignals,
  poolSignals,
  minuteSeries,
  mergeSamples,
} from '../src/lib/insights/signals.ts';

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
