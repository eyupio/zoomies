import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Installation } from '../src/lib/api/types.ts';
import { LOW_SHARE, quotaBars, resetWords } from '../src/lib/bands/quota-bars.ts';

const now = Date.UTC(2026, 10, 1, 12, 0, 0);
const inst = (id: string, extra: Partial<Installation> = {}): Installation =>
  ({ id, target: id, target_type: 'org', ...extra }) as Installation;

// The band's figure counts the connections below a tenth; the bar edged in
// white has to be the same connection, by the same line.
test('the lowest connection comes first, and a tenth is the line the band uses', () => {
  const bars = quotaBars(
    [inst('full'), inst('low')],
    {
      full: { limit: 5000, remaining: 4800 },
      low: { limit: 5000, remaining: 400, reset_at: new Date(now + 23 * 60_000).toISOString() },
    },
    now,
  );
  assert.equal(bars.rows[0]!.id, 'low');
  assert.equal(bars.rows[0]!.low, true);
  assert.equal(bars.rows[0]!.note, '400 of 5,000 left · resets in 23 min · low');
  assert.equal(bars.rows[1]!.low, false);
  assert.equal(bars.summary, '1 of 2 low');
  assert.ok(400 / 5000 < LOW_SHARE);
});

// No reading and no quota are opposite news, so an unread connection is
// neither a full bar nor an empty one.
test('a connection without a reading is drawn as unread, after the ones that have one', () => {
  const bars = quotaBars(
    [inst('unread'), inst('read')],
    { read: { limit: 100, remaining: 90 } },
    now,
  );
  assert.deepEqual(
    bars.rows.map((r) => [r.id, r.share]),
    [
      ['read', 0.9],
      ['unread', null],
    ],
  );
  assert.equal(bars.rows[1]!.note, 'no reading yet');
  assert.equal(bars.summary, 'the connection has quota in hand');
  assert.equal(quotaBars([inst('x')], {}, now).summary, 'No quota readings yet');
});

test('the reset is said in minutes, then hours, and not at all once it has passed', () => {
  assert.equal(resetWords(new Date(now + 5 * 60_000).toISOString(), now), 'resets in 5 min');
  assert.equal(resetWords(new Date(now + 90 * 60_000).toISOString(), now), 'resets in 1 h 30 min');
  assert.equal(resetWords(new Date(now - 60_000).toISOString(), now), '');
  assert.equal(resetWords(undefined, now), '');
});
