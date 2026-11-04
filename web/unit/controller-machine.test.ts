import assert from 'node:assert/strict';
import test from 'node:test';
import {
  machineCardShown,
  machineFigures,
  type ControllerMachine,
} from '../src/lib/hosts/machine.ts';

const sampledAt = '2026-11-03T12:00:00Z';

function machine(over: Partial<ControllerMachine> = {}): ControllerMachine {
  return {
    cpus: 4,
    memory_mb: 8192,
    usage_fresh: true,
    usage: {
      cpu_percent: 68,
      steal_percent: 36,
      load_average_1m: 2.5,
      memory_available_mb: 5120,
      sampled_at: sampledAt,
    },
    ...over,
  };
}

// A controller with an embedded agent is a host, and its card already says all of
// this; drawing the machine again would count it twice. A controller whose agents
// are all elsewhere has no card of its own, which is the gap this fills.
test('the machine is drawn only for a controller with no agent of its own', () => {
  assert.equal(machineCardShown([], machine()), true);
  assert.equal(machineCardShown([{ embedded: false }, {}], machine()), true);
  assert.equal(machineCardShown([{ embedded: true }, { embedded: false }], machine()), false);
});

test('nothing is drawn before the controller has a figure to say', () => {
  assert.equal(machineCardShown([], null), false);
  // Two samples are needed to take a share of an interval, so the first has none.
  assert.equal(machineCardShown([], { usage_fresh: false }), false);
});

test('the figures come in the order a person reads a machine and say what they count', () => {
  const figures = machineFigures(machine());
  assert.deepEqual(
    figures.map((f) => f.key),
    ['cpu', 'steal', 'load', 'memory'],
  );
  assert.equal(figures[0]?.value, '68%');
  assert.equal(figures[1]?.value, '36%');
  assert.match(figures[1]?.detail ?? '', /hypervisor/);
  assert.match(figures[2]?.detail ?? '', /4 CPUs/);
  assert.match(figures[3]?.detail ?? '', /^Of /);
});

// Steal is said only once it is more than a healthy hypervisor leaves, as a host
// card says it; and a figure the controller did not measure is left out, never
// drawn as zero, because a machine that did not measure steal is not one that was
// never starved.
test('steal below the noise and figures never measured are left out, not drawn as zero', () => {
  const quiet = machineFigures(
    machine({ usage: { cpu_percent: 30, steal_percent: 2, sampled_at: sampledAt } }),
  );
  assert.deepEqual(
    quiet.map((f) => f.key),
    ['cpu'],
  );
  const unmeasured = machineFigures(machine({ usage: { cpu_percent: 30, sampled_at: sampledAt } }));
  assert.equal(
    unmeasured.some((f) => f.key === 'steal'),
    false,
  );
  assert.deepEqual(machineFigures({ usage_fresh: false }), []);
});

// A machine at 70% with the controller at 0.1% is busy with something else, and
// that is the answer the figure exists to give, so a small share is said and not
// left out for being low. A zero that was measured is a fact; an absent field,
// from a controller too old to send one, is not, and is never drawn as zero.
test("the controller's own share is said when it is small, and left out when it was never sent", () => {
  const withShare = machineFigures(
    machine({
      usage: {
        cpu_percent: 70,
        process_cpu_percent: 0.1,
        steal_percent: 36,
        load_average_1m: 2.5,
        memory_available_mb: 5120,
        sampled_at: sampledAt,
      },
    }),
  );
  assert.deepEqual(
    withShare.map((f) => f.key),
    ['cpu', 'controller', 'steal', 'load', 'memory'],
  );
  assert.equal(withShare[1]?.value, '0.1%');
  assert.match(withShare[1]?.detail ?? '', /controller process itself/);

  const measuredZero = machineFigures(
    machine({ usage: { cpu_percent: 5, process_cpu_percent: 0, sampled_at: sampledAt } }),
  );
  assert.equal(measuredZero.find((f) => f.key === 'controller')?.value, '0%');

  const older = machineFigures(machine());
  assert.equal(
    older.some((f) => f.key === 'controller'),
    false,
  );
});
