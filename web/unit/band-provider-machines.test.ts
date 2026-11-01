import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Machine, Provider } from '../src/lib/api/types.ts';
import {
  machineKind,
  MAX_PROVIDERS,
  providerMachines,
} from '../src/lib/bands/provider-machines.ts';

const provider = (id: string, extra: Partial<Provider> = {}): Provider =>
  ({ id, name: id, kind: 'proxmox', enabled: true, max_machines: 4, ...extra }) as Provider;
const machine = (provider_id: string, state: Machine['state']): Machine =>
  ({ id: `${provider_id}-${state}-${Math.random()}`, provider_id, name: 'm', state }) as Machine;

// The sentence says "2 machines are owned, up to 4 at once"; the picture has
// to be two solid squares and two hollow ones, whatever else is true.
test('a provider is its ceiling in squares, the machines it holds first and headroom after', () => {
  const rows = providerMachines(
    [provider('lab')],
    [machine('lab', 'ready'), machine('lab', 'creating'), machine('lab', 'deleted')],
  );
  assert.deepEqual(rows.rows[0]!.squares, ['owned', 'starting', 'headroom', 'headroom']);
  // Owned as the sentence counts it: every machine held, the one on the way included.
  assert.equal(rows.rows[0]!.note, '2 of 4 owned · 1 on the way');
  assert.equal(rows.summary, '2 of 4');
});

test('a machine that failed or was quarantined comes first, and the summary counts it', () => {
  const rows = providerMachines(
    [provider('a'), provider('b', { max_machines: 1 })],
    [machine('a', 'ready'), machine('b', 'failed')],
  );
  assert.equal(rows.rows[0]!.id, 'b');
  assert.deepEqual(rows.rows[0]!.squares, ['review']);
  assert.equal(rows.rows[0]!.note, '1 of 1 owned · 1 needs review');
  assert.equal(rows.summary, '2 of 5 · 1 needs review');
});

test('more machines than the ceiling allows are all drawn, and a paused provider says so', () => {
  const rows = providerMachines(
    [provider('p', { max_machines: 1, paused: true })],
    [machine('p', 'ready'), machine('p', 'ready')],
  );
  assert.deepEqual(rows.rows[0]!.squares, ['owned', 'owned']);
  assert.equal(rows.rows[0]!.paused, true);
  assert.match(rows.rows[0]!.note, /paused$/);
});

test('every machine state maps to one square or none', () => {
  assert.equal(machineKind('ready'), 'owned');
  assert.equal(machineKind('enrolling'), 'starting');
  assert.equal(machineKind('draining'), 'leaving');
  assert.equal(machineKind('quarantined'), 'review');
  assert.equal(machineKind('deleted'), null);
  const many = Array.from({ length: MAX_PROVIDERS + 1 }, (_, i) => provider(`p${i}`));
  assert.equal(providerMachines(many, []).more, 1);
});
