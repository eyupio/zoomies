import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Host, Job, Runner } from '../src/lib/api/types.ts';
import { WIDE_FROM, freeSlots, lanes } from '../src/lib/command/lanes.ts';

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

test('a host is its runners, then starting ones, then one free cell that counts the rest', () => {
  const [lane] = lanes([host('a', 4)], [runner('a', 'busy'), runner('a', 'provisioning')], []);
  assert.deepEqual(
    lane!.cells.map((c) => c.kind),
    ['runner', 'starting', 'free'],
  );
  assert.equal(lane!.cells[2]!.count, 2);
  assert.equal(lane!.cells[0]!.count, undefined);
  assert.equal(lane!.slots, 4);
  assert.equal(lane!.free, 2);
});

test('a runner cell opens the runner and the free cell opens the host', () => {
  const r = runner('a', 'busy');
  const [lane] = lanes([host('a', 2)], [r], []);
  assert.equal(lane!.cells[0]!.href, `/runners/${r.id}`);
  assert.equal(lane!.cells[1]!.href, '/hosts/a');
  assert.equal(lane!.href, '/hosts/a');
});

// A tile is keyed by its runner, so one leaving takes its own tile away and
// the tiles after it are the same tiles, not repainted ones replaying their
// entrance.
test('the keys are stable when a runner leaves', () => {
  const a = runner('a', 'busy');
  const b = runner('a', 'busy');
  const c = runner('a', 'busy');
  const before = lanes([host('a', 4)], [a, b, c], [])[0]!;
  const after = lanes([host('a', 4)], [a, { ...b, state: 'removed' }, c], [])[0]!;
  assert.deepEqual(
    before.cells.map((x) => x.key),
    [a.id, b.id, c.id, 'a:free'],
  );
  assert.deepEqual(
    after.cells.map((x) => x.key),
    [a.id, c.id, 'a:free'],
  );
  assert.equal(after.cells[2]!.count, 2);
});

test('a full host has no free cell', () => {
  const [lane] = lanes([host('a', 2)], [runner('a', 'busy'), runner('a', 'idle')], []);
  assert.deepEqual(
    lane!.cells.map((c) => c.kind),
    ['runner', 'runner'],
  );
  assert.equal(lane!.free, 0);
});

test('more runners than slots are all shown, and no free cell is invented', () => {
  const [lane] = lanes([host('a', 1)], [runner('a', 'busy'), runner('a', 'busy')], []);
  assert.equal(lane!.cells.length, 2);
  assert.ok(lane!.cells.every((c) => c.kind === 'runner'));
  assert.equal(lane!.state, 'full');
});

test('a wholly idle host is one free cell', () => {
  const [lane] = lanes([host('a', 14)], [], []);
  assert.deepEqual(
    lane!.cells.map((c) => [c.kind, c.key, c.count]),
    [['free', 'a:free', 14]],
  );
  assert.equal(lane!.note, '0 of 14 slots');
});

test('the free cell says how many slots it stands for, one or many', () => {
  assert.equal(freeSlots(1), '1 free slot');
  assert.equal(freeSlots(14), '14 free slots');
  assert.equal(freeSlots(1200), `${(1200).toLocaleString()} free slots`);
});

test('removed and failed runners hold no cell', () => {
  const [lane] = lanes([host('a', 2)], [runner('a', 'removed'), runner('a', 'failed')], []);
  assert.deepEqual(
    lane!.cells.map((c) => [c.kind, c.count]),
    [['free', 2]],
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

// Wide is about the cells drawn, not the slots: an idle host of fourteen slots
// is one cell and shares its row like any small host.
test('a lane with a few cells is not wide, and one with more is', () => {
  const busy = (n: number) => Array.from({ length: n }, () => runner('a', 'busy'));
  assert.equal(lanes([host('a', WIDE_FROM)], busy(WIDE_FROM), [])[0]!.wide, false);
  assert.equal(lanes([host('a', WIDE_FROM + 1)], busy(WIDE_FROM + 1), [])[0]!.wide, true);
  assert.equal(lanes([host('a', 14)], [], [])[0]!.wide, false);
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

// A runner without an id has no page of its own. It still takes its slot, so
// the lane counts it, and its cell opens the host instead of a link to nowhere.
test('a runner the API sent without an id holds its slot, and its cell opens the host', () => {
  const nameless: Runner = { host_id: 'a', state: 'busy' };
  const [lane] = lanes([host('a', 2)], [nameless], []);
  assert.equal(lane!.cells[0]!.kind, 'runner');
  assert.equal(lane!.cells[0]!.runner, nameless);
  assert.equal(lane!.cells[0]!.href, '/hosts/a');
  assert.equal(lane!.note, '1 of 2 slots');
});

test('a host with no slots and no runners says so, not "0 of 0 slots"', () => {
  const [lane] = lanes([host('a', 0)], [], []);
  assert.equal(lane!.cells.length, 0);
  assert.equal(lane!.state, 'ok');
  assert.equal(lane!.note, 'no slots');
  assert.equal(lane!.tone, null);
});

// A keyed list refuses two items with one key, and the API is not the place
// that promises it never sends a runner twice.
test('two runners with one id still get keys of their own', () => {
  const twin: Runner = { id: 'run_twin', host_id: 'a', state: 'busy' };
  const [lane] = lanes([host('a', 3)], [twin, { ...twin }], []);
  const keys = lane!.cells.map((c) => c.key);
  assert.equal(new Set(keys).size, keys.length);
  assert.deepEqual(keys.slice(0, 2), ['run_twin', 'run_twin:1']);
});

test('two runners with no id get keys of their own', () => {
  const [lane] = lanes(
    [host('a', 3)],
    [
      { host_id: 'a', state: 'busy' },
      { host_id: 'a', state: 'idle' },
    ],
    [],
  );
  const keys = lane!.cells.map((c) => c.key);
  assert.equal(new Set(keys).size, keys.length);
  assert.deepEqual(keys, ['a:0', 'a:1', 'a:free']);
});
