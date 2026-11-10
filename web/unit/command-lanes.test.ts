import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Host, Job, Runner } from '../src/lib/api/types.ts';
import { WIDE_FROM, lanes } from '../src/lib/command/lanes.ts';

const host = (id: string, slots: number, extra: Partial<Host> = {}): Host => ({
  id,
  name: id,
  slots,
  capacity: slots,
  healthy: true,
  ...extra,
});
let serial = 0;
const runner = (host_id: string, state: Runner['state'], extra: Partial<Runner> = {}): Runner => ({
  id: `r${(serial += 1)}`,
  host_id,
  state,
  ...extra,
});
const job = (pool_name: string): Job => ({ id: `j${(serial += 1)}`, state: 'queued', pool_name });

test('a host is one cell for each slot: its runners first, then starting ones, then free', () => {
  const [lane] = lanes([host('a', 4)], [runner('a', 'busy'), runner('a', 'provisioning')], []);
  assert.deepEqual(
    lane!.cells.map((c) => c.kind),
    ['runner', 'starting', 'free', 'free'],
  );
});

test('a runner cell opens the runner and a free cell opens the host', () => {
  const r = runner('a', 'busy');
  const [lane] = lanes([host('a', 2)], [r], []);
  assert.equal(lane!.cells[0]!.href, `/runners/${r.id}`);
  assert.equal(lane!.cells[1]!.href, '/hosts/a');
  assert.equal(lane!.href, '/hosts/a');
});

// A cell is keyed by its place in the lane, so a runner leaving repaints one
// cell instead of sliding the others along and replaying every transition.
test('the cells keep their place when a runner leaves', () => {
  const a = runner('a', 'busy');
  const b = runner('a', 'busy');
  const before = lanes([host('a', 3)], [a, b], [])[0]!;
  const after = lanes([host('a', 3)], [a, runner('a', 'removed')], [])[0]!;
  assert.deepEqual(
    before.cells.map((c) => c.key),
    ['a:0', 'a:1', 'a:2'],
  );
  assert.deepEqual(
    after.cells.map((c) => c.key),
    ['a:0', 'a:1', 'a:2'],
  );
  assert.equal(after.cells[0]!.runner?.id, a.id);
  assert.equal(after.cells[1]!.kind, 'free');
});

test('more runners than slots are all shown, and no free cell is invented', () => {
  const [lane] = lanes([host('a', 1)], [runner('a', 'busy'), runner('a', 'busy')], []);
  assert.equal(lane!.cells.length, 2);
  assert.ok(lane!.cells.every((c) => c.kind === 'runner'));
});

test('removed and failed runners hold no cell', () => {
  const [lane] = lanes([host('a', 2)], [runner('a', 'removed'), runner('a', 'failed')], []);
  assert.deepEqual(
    lane!.cells.map((c) => c.kind),
    ['free', 'free'],
  );
});

test('a retired host has no lane', () => {
  assert.equal(lanes([host('a', 2, { retired: true })], [], []).length, 0);
});

test('the largest host comes first, and ties keep the name order', () => {
  const out = lanes([host('c', 2), host('b', 6), host('a', 2)], [], []);
  assert.deepEqual(
    out.map((l) => l.host.id),
    ['b', 'a', 'c'],
  );
});

test('a host with a few slots is not wide, and one with more is', () => {
  assert.equal(lanes([host('a', WIDE_FROM)], [], [])[0]!.wide, false);
  assert.equal(lanes([host('a', WIDE_FROM + 1)], [], [])[0]!.wide, true);
});

test('a host with room says how much of it is taken', () => {
  const [lane] = lanes([host('a', 6)], [runner('a', 'busy')], []);
  assert.equal(lane!.state, 'ok');
  assert.equal(lane!.note, '1 of 6 slots');
  assert.equal(lane!.tone, null);
});

test('a full host with jobs waiting for a pool that runs on it says how many', () => {
  const runners = [
    runner('a', 'busy', { pool_name: 'precision' }),
    runner('a', 'busy', { pool_name: 'precision' }),
  ];
  const queued = [job('precision'), job('precision'), job('elsewhere')];
  const [lane] = lanes([host('a', 2)], runners, queued);
  assert.equal(lane!.state, 'full');
  assert.equal(lane!.note, 'full, 2 jobs wait');
  assert.equal(lane!.tone, 'pending');
  assert.equal(lane!.waiting, 2);
});

test('a full host with nothing waiting is just full', () => {
  const [lane] = lanes([host('a', 1)], [runner('a', 'busy', { pool_name: 'p' })], []);
  assert.equal(lane!.note, 'full');
  assert.equal(lane!.tone, null);
});

test('a throttled host says so before it says it is full', () => {
  const [lane] = lanes(
    [host('a', 1, { throttle: { level: 1 } })],
    [runner('a', 'busy', { pool_name: 'p' })],
    [job('p')],
  );
  assert.equal(lane!.state, 'throttled');
  assert.equal(lane!.note, 'throttled');
  assert.equal(lane!.tone, 'danger');
});

test('a host that has stopped answering is danger, and a cordoned one is draining', () => {
  const [dead, cordoned] = lanes(
    [host('a', 4, { healthy: false, throttle: { level: 2 } }), host('b', 2, { cordoned: true })],
    [],
    [],
  );
  assert.equal(dead!.state, 'unreachable');
  assert.equal(dead!.note, 'not answering');
  assert.equal(dead!.tone, 'danger');
  assert.equal(cordoned!.state, 'cordoned');
  assert.equal(cordoned!.note, 'cordoned');
  assert.equal(cordoned!.tone, 'draining');
});

test('forty hosts are forty lanes', () => {
  assert.equal(
    lanes(
      Array.from({ length: 40 }, (_, i) => host(`h${i}`, 5)),
      [],
      [],
    ).length,
    40,
  );
});

test('an idle runner is a runner cell with no job', () => {
  const [lane] = lanes([host('a', 2)], [runner('a', 'idle')], []);
  assert.equal(lane!.cells[0]!.kind, 'runner');
  assert.equal(lane!.cells[0]!.runner?.current_job, undefined);
});
