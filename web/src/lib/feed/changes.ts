/**
 * What in a frame is actually news.
 *
 * Kept apart from `entries.ts`, which imports the state map and its icon
 * components and so cannot be loaded outside a browser, for the same reason
 * `outcomes.ts` is kept apart from `status.ts`: this is the half a person
 * would argue about, and it is tested in Node.
 *
 * The rule every function here exists to enforce is that **a frame is not an
 * event**. A host publishes one whenever a heartbeat moves its CPU reading; a
 * machine publishes one on every step of an operation it is already in; a
 * runner publishes one for each state on the way up. A feed fed by frames is a
 * feed nobody reads -- and worse, it is one an operator turns off, losing the
 * lines that were worth having. So each of these answers the narrower
 * question: what is different about this that a person would want to be told,
 * and nothing that is merely different.
 */
import type {
  Host,
  Installation,
  Job,
  Machine,
  MachineState,
  Problem,
  Provider,
  RunnerState,
} from '../api/types';
import { outcomeOf } from '../outcomes';
import { problemKey } from '../problems/identity';

/* -- hosts ---------------------------------------------------------------- */

/**
 * The five facts about a host worth a line. Everything else a host frame
 * carries -- its load, its free slots, when it last spoke -- moves every
 * thirty seconds and says nothing that has not already been said by the
 * capacity map.
 */
export interface HostSignal {
  healthy: boolean;
  cordoned: boolean;
  /** The throttle rung the controller has it on; 0 for none. */
  throttle: number;
  /** An agent too old to talk to this controller at all. */
  incompatible: boolean;
  /** Whether pressure is holding new runners off this host right now. */
  holding: boolean;
}

export type HostChange =
  | 'joined'
  | 'unreachable'
  | 'recovered'
  | 'cordoned'
  | 'uncordoned'
  | 'throttled'
  | 'calm'
  | 'holding'
  | 'admitting'
  | 'incompatible';

export function hostSignal(host: Host): HostSignal {
  return {
    healthy: host.healthy !== false,
    cordoned: host.cordoned === true,
    throttle: host.throttle?.level ?? 0,
    incompatible: host.incompatible === true,
    holding: Boolean(host.admission_reason),
  };
}

/**
 * What changed between two readings of a host, worst first.
 *
 * A host seen for the first time has joined the fleet, which is news exactly
 * once; the caller is responsible for not calling this for every host in the
 * first snapshot it loads.
 */
export function hostChanges(next: HostSignal, previous: HostSignal | undefined): HostChange[] {
  if (!previous) return ['joined'];
  const out: HostChange[] = [];
  if (next.healthy !== previous.healthy) out.push(next.healthy ? 'recovered' : 'unreachable');
  if (next.incompatible && !previous.incompatible) out.push('incompatible');
  if (next.cordoned !== previous.cordoned) out.push(next.cordoned ? 'cordoned' : 'uncordoned');
  // Every rung is its own line: a host climbing the ladder is a host under
  // worsening pressure, which is the thing an operator is watching for.
  if (next.throttle > previous.throttle) out.push('throttled');
  else if (next.throttle === 0 && previous.throttle > 0) out.push('calm');
  // The hold is not the throttle: a throttle takes slots away for minutes, a
  // hold is this moment's pressure refusing the next start. Both are answers
  // to "why is nothing starting here", and only the transitions are reported
  // -- the reason's wording moves with the load and would flap.
  if (next.holding !== previous.holding) out.push(next.holding ? 'holding' : 'admitting');
  return out;
}

/* -- runners ---------------------------------------------------------------- */

/**
 * The two moments in an ordinary runner's life worth a line.
 *
 * Not the six states it passes through: a fleet of ephemeral runners would
 * write one line per state per job, which is a log rather than a feed. A
 * runner reaching `idle` for the first time is the one that says the pool can
 * actually start containers -- the success the failures are the exception to
 * -- and a runner removed is the end of it. Coming back to idle from a job is
 * neither: the job's own line already said that.
 */
export type RunnerMilestone = 'ready' | 'retired';

export function runnerMilestone(
  next: RunnerState | undefined,
  previous: RunnerState | undefined,
): RunnerMilestone | null {
  if (!next || next === previous) return null;
  if (next === 'idle') return previous === 'busy' ? null : 'ready';
  if (next === 'removed') return 'retired';
  return null;
}

/* -- elastic CPU ------------------------------------------------------------ */

/**
 * A runner's elastic-CPU state changing: it was lent spare CPU, or had it
 * taken back, or the host it is on came under enough pressure to slow it.
 *
 * The state rather than the factor, because the factor moves with every
 * heartbeat and the five states are what the runner's own page shows. A
 * runner seen for the first time says nothing: the state it is already in is
 * not something that just happened.
 */
export function cpuChange(next: string | undefined, previous: string | undefined): string | null {
  if (!next || previous === undefined || next === previous) return null;
  return next;
}

/* -- machines -------------------------------------------------------------- */

/**
 * The states of a rented machine worth a line: it is costing money, it is
 * earning it, it is going away, or it went wrong. The steps between -- being
 * powered on, being bootstrapped, enrolling -- are progress rather than news,
 * and the machine's own page has them with their timings.
 */
const MACHINE_NEWS: ReadonlySet<MachineState> = new Set<MachineState>([
  'creating',
  'ready',
  'draining',
  'deleted',
  'failed',
  'quarantined',
]);

/** The state a machine has reached that is worth telling somebody, or null. */
export function machineChange(
  next: Pick<Machine, 'state'>,
  previous: MachineState | undefined,
): MachineState | null {
  const state = next.state;
  if (!state || state === previous) return null;
  return MACHINE_NEWS.has(state) ? state : null;
}

/* -- jobs ------------------------------------------------------------------ */

/**
 * What a finished job is worth saying, and on whose account.
 *
 * Every finished job is news of some kind -- this is the one category where a
 * frame that changes nothing does not exist, because a job is reported over
 * exactly once. What differs is which category it lands in: the failures are
 * what an operator acts on and stay on by default, and the rest are the panel
 * this feed replaced, which on a fleet finishing a job a minute is the noisiest
 * thing here and is a switch of its own for that reason.
 *
 * `runner_lost` is kept apart from `failed` although GitHub records both as a
 * failure: the runner stopped under the job, so that one is the fleet's own,
 * and telling the two apart is what the Overview is for.
 */
export type JobNews = 'runner_lost' | 'failed' | 'succeeded' | 'cancelled' | 'unknown';

export function jobNews(job: Job): JobNews | null {
  if (job.state !== 'completed') return null;
  if (job.runner_fault) return 'runner_lost';
  const outcome = outcomeOf(job);
  return outcome === 'failed' ? 'failed' : outcome;
}

/** Whether this is one of the two an operator is meant to act on. */
export function jobFailure(news: JobNews): boolean {
  return news === 'failed' || news === 'runner_lost';
}

/* -- providers and installations ------------------------------------------- */

export type ProviderChange =
  'paused' | 'resumed' | 'disabled' | 'enabled' | 'unreachable' | 'reachable';

export interface ProviderSignal {
  enabled: boolean;
  paused: boolean;
  /** Whether the last preflight had something to complain about. */
  failing: boolean;
}

export function providerSignal(provider: Provider): ProviderSignal {
  return {
    enabled: provider.enabled !== false,
    paused: provider.paused === true,
    failing: Boolean(provider.last_check_error),
  };
}

export function providerChanges(
  next: ProviderSignal,
  previous: ProviderSignal | undefined,
): ProviderChange[] {
  // A provider first seen is a provider somebody has just added, and adding
  // one is an audit entry rather than an incident.
  if (!previous) return [];
  const out: ProviderChange[] = [];
  if (next.enabled !== previous.enabled) out.push(next.enabled ? 'enabled' : 'disabled');
  if (next.paused !== previous.paused) out.push(next.paused ? 'paused' : 'resumed');
  if (next.failing !== previous.failing) out.push(next.failing ? 'unreachable' : 'reachable');
  return out;
}

export type InstallationChange = 'connected' | 'failing' | 'working';

export function installationChange(
  next: Installation,
  previous: boolean | undefined,
): InstallationChange | null {
  const healthy = next.healthy !== false;
  if (previous === undefined) return healthy ? null : 'failing';
  if (healthy === previous) return null;
  return healthy ? 'working' : 'failing';
}

/* -- problems -------------------------------------------------------------- */

/**
 * The problems in this report that were not in the last one.
 *
 * Identity is the drawer's, not the prose's, so "5 deliveries were rejected"
 * becoming "6 deliveries were rejected" is the same problem and is reported
 * once. `seen` is updated in place: the caller holds the set for as long as
 * the tab is open, and a problem that clears and comes back is news again.
 */
export function newProblems(items: readonly Problem[], seen: Set<string>): Problem[] {
  const live = new Set(items.map(problemKey));
  for (const key of seen) if (!live.has(key)) seen.delete(key);
  const out: Problem[] = [];
  for (const problem of items) {
    const key = problemKey(problem);
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(problem);
  }
  return out;
}
