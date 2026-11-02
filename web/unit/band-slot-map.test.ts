import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Host, Runner } from '../src/lib/api/types.ts';
import { MAX_ROWS, rowNote, slotMap } from '../src/lib/bands/slot-map.ts';

const host = (id: string, slots: number, extra: Partial<Host> = {}): Host => ({
  id,
  name: id,
  slots,
  capacity: slots,
  healthy: true,
  ...extra,
});
const runner = (host_id: string, state: Runner['state']): Runner => ({
  id: `${host_id}-${state}-${Math.random()}`,
  host_id,
  state,
});

test('a host is a row of its slots, the runners it holds first and the free slots after', () => {
  const map = slotMap(
    [host('a', 4)],
    [runner('a', 'idle'), runner('a', 'busy'), runner('a', 'provisioning')],
    'occupancy',
  );
  assert.deepEqual(map.rows[0]!.slots, ['busy', 'starting', 'idle', 'free']);
  assert.equal(map.rows[0]!.note, '3 of 4 in use · 1 starting');
  assert.equal(map.summary, '3 of 4 in use · 1 free');
});

// A removed or failed runner holds nothing, and a runner whose host the cache
// has not got is not given a row of its own: the band's sentence counts hosts
// from the same cache, and the picture must agree with it.
test('removed and failed runners, and runners on unknown hosts, take no square', () => {
  const map = slotMap(
    [host('a', 2)],
    [runner('a', 'removed'), runner('a', 'failed'), runner('ghost', 'busy')],
    'state',
  );
  assert.deepEqual(map.rows[0]!.slots, ['free', 'free']);
  assert.equal(map.rows.length, 1);
  assert.equal(map.rows[0]!.note, 'no runners');
});

// A throttle can lower a host's slots under the runners already on it. Hiding
// a runner would make the map lie about where the work is.
test('a host holding more runners than slots shows every runner', () => {
  const map = slotMap([host('a', 1)], [runner('a', 'busy'), runner('a', 'busy')], 'occupancy');
  assert.deepEqual(map.rows[0]!.slots, ['busy', 'busy']);
  assert.equal(map.rows[0]!.note, '2 of 2 in use');
});

test('the busiest host comes first, and hosts past the limit become a count', () => {
  const hosts = Array.from({ length: MAX_ROWS + 3 }, (_, i) => host(`h${i}`, 2));
  const runners = [runner('h5', 'busy'), runner('h5', 'busy'), runner('h2', 'busy')];
  const map = slotMap(hosts, runners, 'state');
  assert.equal(map.rows[0]!.id, 'h5');
  assert.equal(map.rows[1]!.id, 'h2');
  assert.equal(map.rows.length, MAX_ROWS);
  assert.equal(map.more, 3);
  assert.equal(map.summary, '3 busy · 0 idle');
});

test('the note says what the page asks: occupancy on Hosts, states on Runners, and health either way', () => {
  const slots = ['busy', 'idle', 'draining', 'free'] as const;
  assert.equal(
    rowNote(slots, 'occupancy', { unreachable: false, cordoned: true }),
    '3 of 4 in use · cordoned',
  );
  assert.equal(
    rowNote(slots, 'state', { unreachable: true, cordoned: true }),
    '1 busy · 1 idle · 1 draining · not heard from',
  );
});

test('a host that has stopped sending heartbeats is marked, and an empty fleet says so', () => {
  const map = slotMap([host('a', 2, { healthy: false })], [], 'occupancy');
  assert.equal(map.rows[0]!.unreachable, true);
  assert.equal(map.rows[0]!.note, '0 of 2 in use · not heard from');
  assert.equal(slotMap([], [], 'occupancy').summary, 'No hosts');
});
