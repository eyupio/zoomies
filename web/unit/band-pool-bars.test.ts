import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Pool, Stats } from '../src/lib/api/types.ts';
import { MAX_BARS, poolBars } from '../src/lib/bands/pool-bars.ts';

const pool = (id: string, extra: Partial<Pool> = {}): Pool => ({
  id,
  name: id,
  enabled: true,
  min_runners: 1,
  max_runners: 4,
  priority: 0,
  ...extra,
});
const stats = (pools: Stats['pools']): Stats => ({ pools }) as Stats;

test('a pool at its ceiling comes first, in white, and the sentence counts it', () => {
  const bars = poolBars(
    [pool('calm'), pool('full', { max_runners: 2 })],
    stats([
      { pool_id: 'calm', live: 1, busy: 0, max: 4, queued: 0 },
      { pool_id: 'full', live: 2, busy: 2, max: 2, queued: 3 },
    ]),
  );
  assert.deepEqual(
    bars.rows.map((r) => [r.id, r.atCeiling]),
    [
      ['full', true],
      ['calm', false],
    ],
  );
  assert.equal(bars.rows[0]!.note, '2 busy · 2 of 2 live · at its ceiling');
  assert.equal(bars.summary, '3 of 6 live · 1 at its ceiling');
});

// A disabled pool has no ceiling to be against, so it is not a bar; the band's
// sentence counts enabled pools and the picture has to agree with it.
test('disabled pools are left out, and a quiet fleet says so', () => {
  const bars = poolBars([pool('off', { enabled: false })], null);
  assert.equal(bars.rows.length, 0);
  assert.equal(bars.summary, 'No pools enabled');
});

test('without stats the pool record stands in, and the fullest pools come first', () => {
  const bars = poolBars(
    [
      pool('a', { counts: { live: 1, busy: 1 }, max_runners: 4 }),
      pool('b', { counts: { live: 3, busy: 0 }, max_runners: 4 }),
    ],
    null,
  );
  assert.deepEqual(
    bars.rows.map((r) => r.id),
    ['b', 'a'],
  );
  assert.equal(bars.rows[1]!.note, '1 busy · 1 of 4 live');
});

test('pools past the limit become a count', () => {
  const pools = Array.from({ length: MAX_BARS + 2 }, (_, i) => pool(`p${i}`));
  const bars = poolBars(pools, null);
  assert.equal(bars.rows.length, MAX_BARS);
  assert.equal(bars.more, 2);
});
