import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Stats } from '../src/lib/api/types.ts';
import { heroFigures } from '../src/lib/command/figures.ts';
import { NO_VALUE } from '../src/lib/format.ts';

const NOW = Date.UTC(2026, 10, 10, 14, 0);

const figure = (stats: Stats | null, label: RegExp) => {
  const found = heroFigures(stats, NOW).find((f) => label.test(f.label));
  assert.ok(found, `no figure labelled ${label}`);
  return found;
};

test('a fleet that has finished nothing has no success share to show', () => {
  const stats: Stats = { window: '1h', fleet: { completed: 0, succeeded: 0 } };
  assert.equal(figure(stats, /^Succeeded/).value, NO_VALUE);
});

test('the success share is a whole percentage, with the count beside it', () => {
  const stats: Stats = { window: '1h', fleet: { completed: 27, succeeded: 26 } };
  const succeeded = figure(stats, /^Succeeded/);
  assert.equal(succeeded.value, '96%');
  assert.equal(succeeded.note, '26 of 27 finished');
});

// The controller answers zero for a timing it has no samples for, and "0ms"
// would claim instant work.
test('a timing with no samples behind it is shown as no value, not as zero', () => {
  const stats: Stats = {
    window: '1h',
    p50_startup_ms: 0,
    p95_startup_ms: 0,
    fleet: { running_jobs: 0, completed: 0, median_wait_ms: 0, p95_wait_ms: 0 },
  };
  assert.equal(figure(stats, /^Median wait/).value, NO_VALUE);
  assert.equal(figure(stats, /^Median wait/).note, NO_VALUE);
  assert.equal(figure(stats, /^Runner start/).value, NO_VALUE);
  assert.equal(figure(null, /^Runner start/).value, NO_VALUE);
});

test('a measured wait and start show their median and p95', () => {
  const stats: Stats = {
    window: '1h',
    p50_startup_ms: 4000,
    p95_startup_ms: 7000,
    fleet: { completed: 3, median_wait_ms: 19_000, p95_wait_ms: 26_000 },
  };
  assert.equal(figure(stats, /^Median wait/).value, '19s');
  assert.equal(figure(stats, /^Median wait/).note, 'p95 26s');
  assert.equal(figure(stats, /^Runner start/).note, 'p95 7.0s');
});

test("the success figure's label and link follow the stats window", () => {
  const rows: Array<[string | undefined, string, number]> = [
    ['1h0m0s', 'Succeeded, last hour', 3_600_000],
    ['24h0m0s', 'Succeeded, last 24 hours', 86_400_000],
    ['30m', 'Succeeded, last 30 minutes', 1_800_000],
    // A window the page cannot read is the controller's default hour.
    [undefined, 'Succeeded, last hour', 3_600_000],
  ];
  for (const [window, label, ms] of rows) {
    const succeeded = figure({ window, fleet: { completed: 1, succeeded: 1 } }, /^Succeeded/);
    assert.equal(succeeded.label, label, String(window));
    const url = new URL(succeeded.href, 'http://x');
    assert.equal(url.pathname, '/jobs');
    assert.equal(url.searchParams.get('state'), 'completed');
    assert.equal(url.searchParams.get('since'), new Date(NOW - ms).toISOString(), String(window));
    assert.doesNotMatch(succeeded.opens, /today/);
  }
});

test('the wait and start figures open the queue and the runners', () => {
  const figures = heroFigures({ window: '1h' }, NOW);
  assert.deepEqual(
    figures.map((f) => f.href.split('?')[0]),
    ['/queue', '/runners', '/jobs'],
  );
});
