/**
 * The pool statuses as the Pools page's filter chips, and the card's eyebrow.
 * Pure, so the counts and the words are held by unit tests. The pools are the
 * fleet cache's, so the counts are the fleet's as the Runners chips' are, and
 * a search that narrows the rows leaves them standing; they are withheld
 * until the cache has loaded, because the grid's own fetch usually answers
 * first and warms the cache with the filtered page.
 */
import { pluralise } from '$lib/format';

export type PoolStatusFilter = '' | 'enabled' | 'disabled';

export interface PoolChip {
  value: 'enabled' | 'disabled';
  label: string;
  count?: number;
}

interface Flagged {
  enabled?: boolean;
}

/** A pool with no flag is enabled: the API leaves the field out when it is true. */
function isEnabled(pool: Flagged): boolean {
  return pool.enabled !== false;
}

/** The address bar's word for the status, or no filter when it is not one the product has. */
export function parseStatus(raw: string): PoolStatusFilter {
  return raw === 'enabled' || raw === 'disabled' ? raw : '';
}

export function poolStatusChips(pools: readonly Flagged[], loaded: boolean): PoolChip[] {
  const enabled = pools.filter(isEnabled).length;
  const counted = (count: number): { count?: number } => (loaded ? { count } : {});
  return [
    { value: 'enabled', label: 'Enabled', ...counted(enabled) },
    { value: 'disabled', label: 'Disabled', ...counted(pools.length - enabled) },
  ];
}

/** The card's eyebrow, or nothing until the cache has loaded and has pools to count. */
export function poolsEyebrow(pools: readonly Flagged[], loaded: boolean): string | undefined {
  if (!loaded || pools.length === 0) return undefined;
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
