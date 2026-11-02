/**
 * The runner states as the Runners page's filter chips, and the card's
 * eyebrow. Pure, so the counts and the words are held by unit tests: a count
 * is shown only when it is true of the rows under it, and no stats means no
 * figure rather than a figure that is not there.
 */
import { RUNNER_STATES, type RunnerState } from '$lib/api/types';
import { runnerStatus } from '$lib/status';

export interface RunnerStateCounts {
  provisioning?: number;
  registering?: number;
  idle?: number;
  busy?: number;
  draining?: number;
  failed?: number;
  total?: number;
}

export interface StateChip {
  value: RunnerState;
  label: string;
  count?: number;
}

const LIVE: readonly (keyof RunnerStateCounts)[] = [
  'provisioning',
  'registering',
  'idle',
  'busy',
  'draining',
];

/**
 * The chips in the product's order, removed left out because the Include
 * removed switch governs it; counts only when `counted`, since the stats are
 * the fleet's and a pool or host filter would make them wrong for the rows.
 */
export function runnerStateChips(
  stats: RunnerStateCounts | undefined,
  quirky: boolean,
  counted: boolean,
): StateChip[] {
  return RUNNER_STATES.filter((state) => state !== 'removed').map((state) => {
    const chip: StateChip = { value: state, label: runnerStatus(state, quirky).label };
    const count = counted ? stats?.[state as keyof RunnerStateCounts] : undefined;
    if (typeof count === 'number') chip.count = count;
    return chip;
  });
}

/** The runners that are live: provisioning, registering, idle, busy and draining. */
export function liveRunners(stats: RunnerStateCounts | undefined): number | undefined {
  if (!stats) return undefined;
  return LIVE.reduce((sum, state) => sum + (stats[state] ?? 0), 0);
}

/** The card's eyebrow, or nothing until the stats have said. */
export function runnersEyebrow(stats: RunnerStateCounts | undefined): string | undefined {
  if (!stats || typeof stats.total !== 'number') return undefined;
  return `${stats.total} known, ${liveRunners(stats)} live`;
}
