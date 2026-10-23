import { test } from 'node:test';
import assert from 'node:assert/strict';
import { demandMore, demandStats, type DemandInput } from '../src/lib/bands/demand-stats.ts';

const labels = { ready: 'Ready', expedited: 'Run now', paused: 'Paused', deleted: 'Removed' };
const base: DemandInput = {
  queued: 7,
  running: 4,
  queuedWarning: true,
  counts: { ready: 5, expedited: 1, paused: 2 },
  oldest: null,
  poolId: '',
  labels,
};

test('the strip says how much is queued, what is running, then each provisioning status', () => {
  const stats = demandStats(base);
  assert.deepEqual(
    stats.map((s) => [s.label, s.value]),
    [
      ['Jobs queued', '7'],
      ['Running', '4'],
      ['Ready', '5'],
      ['Run now', '1'],
      ['Paused', '2'],
      // A status the answer does not mention is none, not unknown.
      ['Removed', '0'],
    ],
  );
  assert.equal(stats[0]?.tone, 'warning');
  assert.equal(stats[1]?.href, '/runners?state=busy');
  assert.equal(stats[2]?.href, '/queue?provisioning=ready');
});

test('a count that has not arrived is not drawn as zero', () => {
  const stats = demandStats({ ...base, counts: null });
  assert.equal(stats[2]?.value, undefined);
  assert.equal(stats[5]?.value, undefined);
  assert.equal(stats[0]?.value, '7');
  assert.equal(stats[1]?.value, '4');
  assert.equal(demandStats({ ...base, running: null })[1]?.value, undefined);
});

test('one queued job is singular, and the oldest shows only when there is one', () => {
  assert.equal(demandStats({ ...base, queued: 1 })[0]?.label, 'Job queued');
  assert.equal(
    demandStats(base).some((s) => s.label === 'Oldest demand'),
    false,
  );
  const withOldest = demandStats({ ...base, oldest: '2026-01-01T00:00:00Z' });
  assert.equal(withOldest.at(-1)?.since, '2026-01-01T00:00:00Z');
});

test('a pool filter narrows every link to that pool', () => {
  const stats = demandStats({ ...base, poolId: 'pool 1' });
  assert.equal(stats[0]?.href, '/jobs?state=queued&pool_id=pool%201');
  assert.equal(stats[1]?.href, '/runners?state=busy&pool_id=pool%201');
  assert.equal(stats[3]?.href, '/queue?provisioning=expedited&pool_id=pool%201');
  assert.equal(demandMore('pool 1').href, '/queue?pool_id=pool%201');
  assert.equal(demandMore('').href, '/queue');
});
