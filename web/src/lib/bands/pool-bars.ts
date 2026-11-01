/**
 * The bars the Pools band draws beside its sentence: one per enabled pool,
 * busy over live against the pool's ceiling, from the pools and stats the
 * fleet cache already holds.
 *
 * Plain functions, as the band sentences are, so the picture is held to the
 * figures by a test: a bar drawn at its ceiling over a sentence that said none
 * was there would be wrong in a way a layout change is very good at hiding.
 */
import type { Pool, Stats } from '../api/types';
import { poolSignals } from '../insights/signals';

export interface PoolBar {
  id: string;
  name: string;
  busy: number;
  live: number;
  min: number;
  max: number;
  /** Enabled, with work waiting and nowhere to put a runner. */
  atCeiling: boolean;
  /** The bar in words, for its note and its accessible name. */
  note: string;
}

export interface PoolBars {
  rows: PoolBar[];
  /** Enabled pools past the ones shown. */
  more: number;
  /** The fleet in a few words: "9 of 12 live · 1 at its ceiling". */
  summary: string;
}

/** How many pools a band shows before it says "and n more". */
export const MAX_BARS = 6;

/**
 * Enabled pools only, since a disabled pool has no ceiling to be against, the
 * ones at their ceiling first because that is what the picture exists to
 * show, then the fullest.
 */
export function poolBars(pools: readonly Pool[], stats: Stats | null): PoolBars {
  const rows: PoolBar[] = poolSignals(pools, stats)
    .filter((signal) => signal.pool.enabled === true && signal.pool.id)
    .map((signal) => {
      const live = signal.live ?? 0;
      const max = signal.max ?? 0;
      const busy = signal.busy ?? 0;
      const min = signal.pool.min_runners ?? 0;
      const parts = [`${busy} busy`, `${live} of ${max} live`];
      if (signal.atCeiling) parts.push('at its ceiling');
      else if (signal.queued) parts.push(`${signal.queued} queued`);
      return {
        id: signal.pool.id ?? '',
        name: signal.pool.name || signal.pool.id || 'Unnamed pool',
        busy,
        live,
        min,
        max,
        atCeiling: signal.atCeiling,
        note: parts.join(' · '),
      };
    })
    .sort((a, b) => {
      if (a.atCeiling !== b.atCeiling) return a.atCeiling ? -1 : 1;
      const share = (r: PoolBar) => (r.max > 0 ? r.live / r.max : 0);
      return share(b) - share(a) || a.name.localeCompare(b.name);
    });
  const live = rows.reduce((sum, r) => sum + r.live, 0);
  const max = rows.reduce((sum, r) => sum + r.max, 0);
  const ceiling = rows.filter((r) => r.atCeiling).length;
  const summary =
    rows.length === 0
      ? 'No pools enabled'
      : `${live} of ${max} live${ceiling > 0 ? ` · ${ceiling} at ${ceiling === 1 ? 'its' : 'their'} ceiling` : ''}`;
  return { rows: rows.slice(0, MAX_BARS), more: Math.max(0, rows.length - MAX_BARS), summary };
}
