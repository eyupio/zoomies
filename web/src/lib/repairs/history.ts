import type { Query } from '../api/types';
import { formatNumber, pluralise } from '../format';

/*
  What the Recent repairs history needs to know that is not about drawing it:
  which filters there are, what each one sends, and where the page should be
  after rows leave it. The server does the filtering and the paging, so this
  file never looks at a repair's fields beyond its state.
*/

export type RepairGroup = 'all' | 'attention' | 'active' | 'passed';
export type RepairTrigger = 'any' | 'automatic' | 'requested';

export interface RepairFilters {
  group: RepairGroup;
  repo: string;
  trigger: RepairTrigger;
}

export const NO_REPAIR_FILTERS: RepairFilters = { group: 'all', repo: '', trigger: 'any' };

/**
 * Whether any filter is set. It decides between "No repairs yet" and "No
 * repairs match these filters", which an operator acts on differently, so a
 * repository box holding only spaces is not a filter: it is not sent either.
 */
export const filtered = (f: RepairFilters): boolean =>
  f.group !== 'all' || f.repo.trim() !== '' || f.trigger !== 'any';

const IN_PROGRESS = ['queued', 'working', 'checking'];

/** A repair Eli is still on, which cannot be retried or cleared from the history. */
export const isActive = (state: string): boolean => IN_PROGRESS.includes(state);

export const stateLabel = (state: string) =>
  ({
    queued: 'Queued',
    working: 'Investigating',
    checking: 'Waiting for CI',
    succeeded: 'Checks passed',
    checks_failed: 'Checks failed',
    interrupted: 'Interrupted',
    unverified: 'Unverified',
    superseded: 'Newer commit',
    blocked: 'Needs attention',
    failed: 'Could not repair',
  })[state] ?? state;

/**
 * The filters as the selection endpoint takes them: what a select-all means is
 * everything the same filters would list, with no page in it. A default is
 * left out rather than spelled out, so the page never depends on the server
 * agreeing that `group=all` means no filter.
 */
export function selectionQuery(f: RepairFilters): Query<'selectEliRepairs'> {
  const query: Query<'selectEliRepairs'> = {};
  if (f.group !== 'all') query.group = f.group;
  const repo = f.repo.trim();
  if (repo) query.repo = repo;
  if (f.trigger !== 'any') query.trigger = f.trigger;
  return query;
}

export function listQuery(
  f: RepairFilters,
  limit: number,
  offset: number,
): Query<'listEliRepairs'> {
  return { ...selectionQuery(f), limit, offset };
}

/**
 * Where to read from once `cleared` rows have left a history of `total`: the
 * page the operator was on if it still has a row, otherwise the last page that
 * does. Staying put would show an empty list beside a total that says there
 * are repairs, and the empty state is for a history with none.
 */
export function offsetAfterClear(
  offset: number,
  limit: number,
  total: number,
  cleared: number,
): number {
  const remaining = Math.max(0, total - cleared);
  if (limit <= 0 || remaining === 0) return 0;
  const lastStart = Math.floor((remaining - 1) / limit) * limit;
  return Math.max(0, Math.min(offset, lastStart));
}

/** What the closed History section still says, from the list it last read. */
export interface RepairHistorySummary {
  loaded: boolean;
  error: string;
  total: number;
  /** The newest repair on the first page, when there is one. */
  latest?: string;
  /** Whether the total is narrowed by a filter. */
  filtered: boolean;
}

/** What the bulk route will take in one request. A longer list is refused outright. */
export const BULK_LIMIT = 500;

/** The ids cut into requests the bulk route accepts, in the order they were chosen. */
export function batches<T>(items: readonly T[], size: number): T[][] {
  const parts: T[][] = [];
  for (let start = 0; start < items.length; start += size)
    parts.push(items.slice(start, start + size));
  return parts;
}

export interface ClearReport {
  cleared: number;
  skipped: { id: string; error: string }[];
}

/**
 * What a clear did, from the route's answer for each id. The route answers 200
 * however many it refused, so a partial failure is only visible here; the
 * skipped ones keep the order they were asked in. `unattempted` are the ids a
 * clear never reached because a later request failed: they are listed after the
 * refused ones, since saying "0 skipped" would leave them nowhere on the screen.
 */
export function clearReport(
  results: readonly { id: string; ok: boolean; error?: string }[],
  unattempted: readonly string[] = [],
): ClearReport {
  const report: ClearReport = { cleared: 0, skipped: [] };
  for (const result of results) {
    if (result.ok) report.cleared++;
    else report.skipped.push({ id: result.id, error: result.error || 'not cleared' });
  }
  for (const id of unattempted) report.skipped.push({ id, error: 'not attempted' });
  return report;
}

/**
 * What a clear did, in one line: what the outcome shows and what a screen
 * reader is told. One function so the two cannot say different things.
 */
export function clearSummary(cleared: number, skipped: number): string {
  const done = `${pluralise(cleared, 'repair')} cleared from the history`;
  return skipped > 0 ? `${done}, ${formatNumber(skipped)} skipped` : done;
}

/**
 * What the confirmation says. The count comes first because after a select-all
 * it is the one thing the operator cannot see from the dialog: up to 500
 * repairs, most of them on pages they never looked at. The page has no way to
 * bring a cleared repair back, so that is said here too. The rest is what
 * clearing leaves alone: a cleared repair is hidden and kept, still the record
 * the guard against Eli repairing its own commits reads, and the deduplication
 * that stops the same webhook delivery being answered twice.
 */
export function clearConsequences(count: number): readonly string[] {
  const how = `${formatNumber(count)} ${count === 1 ? 'repair' : 'repairs'}`;
  return [
    `${how} will stop showing in this list, and this cannot be undone from this page.`,
    'Eli will not repair its own commits from them again.',
  ];
}
