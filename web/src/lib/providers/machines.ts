/**
 * What a rented machine is doing, worked out from the row alone.
 *
 * Pure, and in a `.ts` rather than a component, for the reason
 * `insights/signals.ts` is: the arithmetic behind "this one has been
 * bootstrapping for eleven minutes" and "these three need a person" is what
 * goes wrong, and it is testable here without a browser.
 *
 * Nothing here reads a clock of its own. `now` is passed in, so a test can say
 * what time it is and a component can hand it the same tick it renders with.
 */
import type { Machine, MachineState, Provider } from '../api/types';

/**
 * The lifecycle as the band draws it: the states a machine passes through on
 * its way to being a host. Deleting, deleted, failed and quarantined are exits
 * from the flow rather than steps of it, and are counted separately.
 */
export const MACHINE_FLOW: readonly MachineState[] = [
  'planned',
  'creating',
  'starting',
  'bootstrapping',
  'enrolling',
  'ready',
  'draining',
];

/** The states a machine is in while the fleet is still waiting for it. */
const PENDING: ReadonlySet<MachineState> = new Set([
  'planned',
  'creating',
  'starting',
  'bootstrapping',
  'enrolling',
]);

/** The phases a timeline entry can carry, as a sentence for the page. */
const PHASES: Readonly<Record<string, string>> = {
  planned: 'Planned',
  creating: 'Creating the machine',
  created: 'Machine created',
  starting: 'Powering on',
  bootstrapped: 'Agent installed',
  enrolled: 'Enrolled with the controller',
  ready: 'Ready for work',
  draining: 'Draining',
  deleting: 'Deleting',
  deleted: 'Resource confirmed gone',
};

/**
 * What a phase is called on the page. Two consecutive rows both reading
 * "Creating" say nothing, and those rows are exactly the diagnosis: a clone
 * that never finished is a problem at the hypervisor, and a machine that
 * cloned and never enrolled is a problem inside the guest.
 */
export function phaseLabel(phase: string | undefined): string {
  if (!phase) return 'Unknown';
  return PHASES[phase] ?? phase.charAt(0).toUpperCase() + phase.slice(1);
}

/**
 * The state a timeline phase belongs to, so a row is drawn in the colour the
 * rest of the product gives that part of a machine's life. The timeline has
 * more entries than there are states -- "created" and "creating" are two marks
 * on one state -- because what an operator wants to know is which half of a
 * slow create is slow.
 */
const PHASE_STATES: Readonly<Record<string, MachineState>> = {
  planned: 'planned',
  creating: 'creating',
  created: 'creating',
  starting: 'starting',
  bootstrapped: 'bootstrapping',
  enrolled: 'enrolling',
  ready: 'ready',
  draining: 'draining',
  deleting: 'deleting',
  deleted: 'deleted',
};

export function phaseState(phase: string | undefined): MachineState | undefined {
  return phase ? PHASE_STATES[phase] : undefined;
}

export interface MachineSignals {
  /** Whether the fleet is still waiting for this machine to become a host. */
  pending: boolean;
  /** Whether it holds a resource somebody is being billed for. */
  owns: boolean;
  /** How long it has been in its current state, or null when that is unknown. */
  elapsedMs: number | null;
  /** The operation in flight, as a word, or '' when none is. */
  operation: string;
  /**
   * Whether an operator has to look at this one. It is deliberately not "the
   * state is failed": a machine whose ownership nothing could confirm, and one
   * whose last call went out and was never answered, are both running
   * hardware nothing will tidy up on its own.
   */
  needsReview: boolean;
  /** Why, in one sentence, or '' when it does not. */
  reviewReason: string;
  /** The row's own reading of whether a delete would be safe. */
  safeToDelete: boolean;
  safeToDeleteWhy: string;
}

export function machineSignals(machine: Machine, now: number = Date.now()): MachineSignals {
  const since = machine.updated_at ? Date.parse(machine.updated_at) : Number.NaN;
  const state = machine.state;
  return {
    pending: state !== undefined && PENDING.has(state),
    owns: Boolean(machine.resource_id) && !machine.deleted_at,
    elapsedMs: Number.isFinite(since) ? Math.max(0, now - since) : null,
    operation: machine.operation ?? '',
    ...review(machine),
    safeToDelete: machine.safe_to_delete === true,
    safeToDeleteWhy: machine.safe_to_delete_why ?? '',
  };
}

function review(machine: Machine): { needsReview: boolean; reviewReason: string } {
  if (machine.state === 'quarantined') {
    return {
      needsReview: true,
      reviewReason:
        machine.ownership_error ||
        'Nothing could prove this resource is ours, so nothing here will act on it.',
    };
  }
  if (machine.ownership_error) return { needsReview: true, reviewReason: machine.ownership_error };
  if (machine.state === 'failed') {
    return {
      needsReview: true,
      reviewReason:
        machine.provider_error ||
        machine.bootstrap_error ||
        'This machine never became a host. Its resource may still exist.',
    };
  }
  if (machine.outcome_unknown) {
    return {
      needsReview: true,
      reviewReason: 'A call went out and was never answered, so what happened is not known yet.',
    };
  }
  return { needsReview: false, reviewReason: '' };
}

/** Every machine an operator has to look at, in the order they were listed. */
export function machinesNeedingReview(machines: readonly Machine[]): Machine[] {
  return machines.filter((m) => review(m).needsReview);
}

export interface LifecycleStep {
  state: MachineState;
  count: number;
}

/**
 * How many machines are at each step of the flow.
 *
 * Deleted machines are left out rather than counted as zero: a fleet that has
 * been running for a month has hundreds of them, and a band whose largest
 * number is the history is a band nobody reads.
 */
export function lifecycleCounts(machines: readonly Machine[]): LifecycleStep[] {
  const counts = new Map<MachineState, number>();
  for (const state of MACHINE_FLOW) counts.set(state, 0);
  for (const machine of machines) {
    const state = machine.state;
    if (state === undefined || !counts.has(state)) continue;
    counts.set(state, (counts.get(state) ?? 0) + 1);
  }
  return MACHINE_FLOW.map((state) => ({ state, count: counts.get(state) ?? 0 }));
}

/**
 * What a machine has cost so far, or null when its provider has no opinion.
 *
 * A provider with no price is the common case -- a hypervisor in a cupboard
 * has no hourly rate -- and inventing one would be worse than saying nothing.
 */
export function machineCost(
  machine: Machine,
  provider: Pick<Provider, 'cost_per_machine_hour'> | undefined,
  now: number = Date.now(),
): number | null {
  const rate = provider?.cost_per_machine_hour ?? 0;
  if (rate <= 0) return null;
  const from = machine.created_at ? Date.parse(machine.created_at) : Number.NaN;
  if (!Number.isFinite(from)) return null;
  const to = machine.deleted_at ? Date.parse(machine.deleted_at) : now;
  if (!Number.isFinite(to)) return null;
  return (Math.max(0, to - from) / 3_600_000) * rate;
}

/**
 * A cost as a figure. Two decimals, because that is what an invoice has, and
 * an empty string rather than "0.00" when there is no price to report -- a
 * zero reads as free, which is a different claim from "nobody said".
 */
export function formatCost(value: number | null): string {
  return value === null ? '' : value.toFixed(2);
}
