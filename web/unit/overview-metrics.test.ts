import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Stats } from '../src/lib/api/types.ts';
import { overviewMetrics } from '../src/lib/overview/metric-stats.ts';

const now = Date.parse('2026-10-30T12:00:00Z');

const stats: Stats = {
  window: '1h0m0s',
  queued_jobs: 41,
  running_jobs: 12,
  completed: 200,
  median_wait_ms: 28_000,
  p95_wait_ms: 134_000,
  fleet: {
    queued_jobs: 6,
    running_jobs: 3,
    completed: 80,
    median_wait_ms: 19_000,
    p95_wait_ms: 26_000,
  },
  runners: { provisioning: 1, registering: 2, idle: 3, busy: 2, draining: 1, failed: 4 },
  hosts: { total: 4, healthy: 3, capacity: 10, used: 8 },
};

test('the band carries the four figures the fleet is judged on', () => {
  const figures = overviewMetrics({ stats, others: false, now });
  assert.deepEqual(
    figures.map((f) => [f.label, f.value]),
    [
      ['Queued jobs', '6'],
      ['Running jobs', '3'],
      // Provisioning, registering, idle, busy and draining; not failed, and not
      // the removed history the fleet no longer has.
      ['Live runners', '9'],
      ['Median queue wait', '19s'],
    ],
  );
  // The p95 is why the median is worth showing, and the slots explain a queue
  // that is not moving. Both are visible, not only in a tooltip.
  assert.equal(figures[2]?.detail, '8 of 10 slots on 3 connected hosts');
  assert.equal(figures[3]?.detail, 'p95 26s');
});

test('each figure is a way into the records it counted', () => {
  const figures = overviewMetrics({ stats, others: false, now });
  assert.equal(figures[0]?.href, '/queue');
  assert.equal(figures[1]?.href, '/runners?state=busy#runners-list');
  assert.equal(
    figures[2]?.href,
    '/runners?state=provisioning&state=registering&state=idle&state=busy#runners-list',
  );
  // The wait's evidence is the completed jobs in the window the figure covers,
  // and the window is read from the controller rather than guessed.
  const since = new Date(now - 60 * 60 * 1000).toISOString();
  assert.equal(figures[3]?.href, `/jobs?state=completed&since=${encodeURIComponent(since)}`);
});

test('a median with no job behind it is the no-value placeholder, not zero', () => {
  const empty: Stats = {
    window: '1h0m0s',
    queued_jobs: 0,
    running_jobs: 0,
    completed: 0,
    median_wait_ms: 0,
    p95_wait_ms: 0,
    fleet: { queued_jobs: 0, running_jobs: 0, completed: 0, median_wait_ms: 0, p95_wait_ms: 0 },
    runners: {},
    hosts: { total: 0 },
  };
  const figures = overviewMetrics({ stats: empty, others: false, now });
  assert.equal(figures[3]?.value, '--');
  assert.equal(figures[3]?.detail, undefined);
  assert.equal(figures[2]?.detail, 'No hosts connected');
});

test('with other runners in scope the job figures widen, and the links say so', () => {
  const figures = overviewMetrics({ stats, others: true, now });
  assert.deepEqual(
    figures.slice(0, 2).map((f) => f.value),
    ['41', '12'],
  );
  assert.equal(figures[0]?.href, '/jobs?state=queued&all=true');
  assert.match(figures[3]?.href ?? '', /&all=true$/);
  // Runners and hosts are always this fleet's, so the live figure does not move.
  assert.equal(figures[2]?.value, '9');
  assert.match(figures[2]?.hint ?? '', /belong to this fleet/);
});

test('a window the controller did not report falls back to the page’s own hour', () => {
  const figures = overviewMetrics({ stats: { ...stats, window: undefined }, others: false, now });
  const since = new Date(now - 60 * 60 * 1000).toISOString();
  assert.equal(figures[3]?.href, `/jobs?state=completed&since=${encodeURIComponent(since)}`);
});
