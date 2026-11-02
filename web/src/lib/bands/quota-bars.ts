/**
 * The Installations band's picture: how much of each App's hourly API
 * allowance is left, from the readings the page already takes.
 *
 * Plain functions, so the bars agree with the band's own "low API quota"
 * figure: the same tenth is the line, and a test holds them to it.
 */
import type { Installation } from '../api/types';

export interface QuotaReading {
  limit?: number;
  remaining?: number;
  reset_at?: string;
}

export interface QuotaBar {
  id: string;
  name: string;
  /** Remaining over limit, 0 to 1, or null when no reading has been taken. */
  share: number | null;
  /** Below a tenth, which is when the band counts it as low. */
  low: boolean;
  note: string;
}

export interface QuotaBars {
  rows: QuotaBar[];
  more: number;
  summary: string;
}

export const MAX_QUOTA_ROWS = 5;
/** The band's own line for a low quota, as a share of the limit. */
export const LOW_SHARE = 0.1;

/** "resets in 23 min", or nothing when the reading has no reset or it has passed. */
export function resetWords(resetAt: string | undefined, now: number): string {
  if (!resetAt) return '';
  const at = Date.parse(resetAt);
  if (!Number.isFinite(at) || at <= now) return '';
  const minutes = Math.ceil((at - now) / 60_000);
  return minutes < 60
    ? `resets in ${minutes} min`
    : `resets in ${Math.floor(minutes / 60)} h ${minutes % 60} min`;
}

export function quotaBars(
  installations: readonly Installation[],
  rates: Readonly<Record<string, QuotaReading>>,
  now: number,
): QuotaBars {
  const rows: QuotaBar[] = installations
    .filter((row) => row.id)
    .map((row) => {
      const rate = rates[row.id ?? ''];
      const has = rate?.remaining !== undefined && rate.limit !== undefined && rate.limit > 0;
      const share = has ? Math.max(0, Math.min(1, rate.remaining! / rate.limit!)) : null;
      const low = share !== null && share < LOW_SHARE;
      const parts: string[] = [];
      if (has) {
        parts.push(`${rate.remaining!.toLocaleString()} of ${rate.limit!.toLocaleString()} left`);
        const reset = resetWords(rate.reset_at, now);
        if (reset) parts.push(reset);
        if (low) parts.push('low');
      } else {
        parts.push('no reading yet');
      }
      if (row.healthy === false) parts.push('not answering');
      return {
        id: row.id ?? '',
        name: row.target || row.id || 'GitHub App',
        share,
        low,
        note: parts.join(' · '),
      };
    })
    .sort((a, b) => {
      // Lowest first, since that is the one about to break; unread ones last.
      const key = (r: QuotaBar) => (r.share === null ? 2 : r.share);
      return key(a) - key(b) || a.name.localeCompare(b.name);
    });
  const read = rows.filter((r) => r.share !== null);
  const low = rows.filter((r) => r.low).length;
  const summary =
    rows.length === 0
      ? 'No connections'
      : read.length === 0
        ? 'No quota readings yet'
        : low > 0
          ? `${low} of ${read.length} low`
          : `${read.length === 1 ? 'the connection has' : `all ${read.length} have`} quota in hand`;
  return {
    rows: rows.slice(0, MAX_QUOTA_ROWS),
    more: Math.max(0, rows.length - MAX_QUOTA_ROWS),
    summary,
  };
}
