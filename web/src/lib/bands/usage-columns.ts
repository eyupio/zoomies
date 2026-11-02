/**
 * The Usage band's columns: the chosen range's execution hours, one column
 * per interval of the report the page already holds, summed across its
 * groups into a fleet total.
 *
 * Plain functions so the columns are held to the report by a test. The
 * report's own intervals are drawn as they come (an hour each for a day, a
 * day each for a quarter), because the page's chart under the band reads
 * the same buckets and the two must agree.
 */
import type { UsageRow } from '../api/types';
import { mergeHistories } from '../insights/activity';
import { toMillis } from '../format';

export interface UsageColumn {
  at: number;
  /** Execution time in the interval, in hours. */
  hours: number;
  /** Jobs that finished in it, for the column's words. */
  completed: number;
}

export interface UsageColumns {
  columns: UsageColumn[];
  /** The tallest column, which is what the rest are drawn against. */
  max: number;
  /** The tallest column's hours and moment, or null for a range with no work. */
  peak: UsageColumn | null;
}

export function usageColumns(rows: readonly UsageRow[]): UsageColumns {
  const columns: UsageColumn[] = mergeHistories(rows)
    .map((b) => ({
      at: toMillis(b.from) ?? NaN,
      hours: b.execution_seconds / 3600,
      completed: b.succeeded + b.failed + b.cancelled + b.unknown,
    }))
    .filter((c) => Number.isFinite(c.at));
  let peak: UsageColumn | null = null;
  for (const c of columns) if (c.hours > 0 && (!peak || c.hours > peak.hours)) peak = c;
  return { columns, max: peak?.hours ?? 0, peak };
}
