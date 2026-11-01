import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { UsageRow } from '../src/lib/api/types.ts';
import { usageColumns } from '../src/lib/bands/usage-columns.ts';

const bucket = (from: string, seconds: number, succeeded = 0, failed = 0) => ({
  from,
  queued: 0,
  started: 0,
  succeeded,
  failed,
  cancelled: 0,
  unknown: 0,
  execution_seconds: seconds,
  allocated_seconds: 0,
  capacity_samples: 0,
  capacity_reached: 0,
});
const row = (history: ReturnType<typeof bucket>[]): UsageRow =>
  ({
    key: 'x',
    jobs: 0,
    jobs_started: 0,
    jobs_completed: 0,
    job_execution_seconds: 0,
    history,
  }) as unknown as UsageRow;

// The band's columns are the report's intervals summed across its groups, so
// two pools' hours in one interval are one column, as the chart under it draws.
test('groups are summed into one column per interval, with the peak named', () => {
  const data = usageColumns([
    row([bucket('2026-11-01T00:00:00Z', 3600, 2), bucket('2026-11-01T01:00:00Z', 1800)]),
    row([bucket('2026-11-01T00:00:00Z', 7200, 1, 1)]),
  ]);
  assert.deepEqual(
    data.columns.map((c) => [c.hours, c.completed]),
    [
      [3, 4],
      [0.5, 0],
    ],
  );
  assert.equal(data.max, 3);
  assert.equal(data.peak?.at, Date.parse('2026-11-01T00:00:00Z'));
});

test('a range with no work has columns but no peak', () => {
  const data = usageColumns([row([bucket('2026-11-01T00:00:00Z', 0)])]);
  assert.equal(data.columns.length, 1);
  assert.equal(data.peak, null);
  assert.equal(data.max, 0);
  assert.deepEqual(usageColumns([]).columns, []);
});
