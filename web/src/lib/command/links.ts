/**
 * Where every figure and cell on the command centre leads.
 *
 * Kept in one plain module so the destinations are held by a test rather than
 * scattered through components: a number that says "3 jobs wait" and opens the
 * wrong list is wrong in a way no layout review will catch.
 */
import type { Problem } from '../api/types';
import { problemTarget } from '../problems/targets';

export const RUNNING_HREF = '/jobs?state=in_progress';
export const QUEUE_HREF = '/queue';
export const RUNNERS_HREF = '/runners';
// The Overview's History disclosure is the section with the id `history`, and
// its own links reach it with this hash.
export const HISTORY_HREF = '/#history';

/**
 * Jobs finished since an instant: the records behind a figure measured over a
 * window, so the list a figure opens is the set it was worked out from.
 */
export function finishedSinceHref(since: Date): string {
  return `/jobs?state=completed&since=${encodeURIComponent(since.toISOString())}`;
}

// Ids come from the API but a link is a URL: encode so one with a slash or a
// space cannot name a different page.
export function hostHref(id: string): string {
  return `/hosts/${encodeURIComponent(id)}`;
}

export function runnerHref(id: string): string {
  return `/runners/${encodeURIComponent(id)}`;
}

/** The page a problem's "open" link goes to: the problems drawer's own rule. */
export function problemHref(problem: Problem): string | null {
  return problemTarget(problem)?.href ?? null;
}
