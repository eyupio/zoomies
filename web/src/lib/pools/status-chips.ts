/**
 * The pool statuses as the Pools page's filter chips, and the card's eyebrow.
 * Pure, so the counts and the words are held by unit tests. The pools are the
 * fleet cache's, which the page already filters client side, so the counts
 * are true of the rows whatever the search says.
 */
import { pluralise } from '$lib/format';

export type PoolStatusFilter = '' | 'enabled' | 'disabled';

export interface PoolChip {
  value: 'enabled' | 'disabled';
  label: string;
  count: number;
}

interface Flagged {
  enabled?: boolean;
}

/** A pool with no flag is enabled: the API leaves the field out when it is true. */
function isEnabled(pool: Flagged): boolean {
  return pool.enabled !== false;
}

export function poolStatusChips(pools: readonly Flagged[]): PoolChip[] {
  const enabled = pools.filter(isEnabled).length;
  return [
    { value: 'enabled', label: 'Enabled', count: enabled },
    { value: 'disabled', label: 'Disabled', count: pools.length - enabled },
  ];
}

/** The card's eyebrow, or nothing while there are no pools to count. */
export function poolsEyebrow(pools: readonly Flagged[]): string | undefined {
  if (pools.length === 0) return undefined;
  return `${pluralise(pools.length, 'pool')}, ${pools.filter(isEnabled).length} enabled`;
}

/**
 * The one status the page's parameter holds, read from a chip press. The
 * chips toggle a selection, so a press lands as the value that was not in
 * force before, and a selection emptied by All or by pressing the chip again
 * is no filter.
 */
export function pickStatus(
  current: PoolStatusFilter,
  next: readonly ('enabled' | 'disabled')[],
): PoolStatusFilter {
  return next.find((value) => value !== current) ?? '';
}
