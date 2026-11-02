/**
 * The Workflows band's picture: the work in flight by repository, from the
 * runs the page's grid already fetched. A run's job counts say how many are
 * running and how many wait; summed by repository they say whose work the
 * fleet is doing.
 *
 * Plain functions, so the rows add up to the band's own figures under a test.
 */
import type { WorkflowRun } from '../api/types';

export type FlightKind = 'running' | 'waiting';

export interface FlightRow {
  repo: string;
  squares: FlightKind[];
  running: number;
  waiting: number;
  note: string;
}

export interface FlightRows {
  rows: FlightRow[];
  more: number;
  summary: string;
}

export const MAX_FLIGHT_ROWS = 5;
export const MAX_FLIGHT_SQUARES = 12;

export function inFlight(runs: readonly WorkflowRun[]): FlightRows {
  const byRepo = new Map<string, { running: number; waiting: number }>();
  for (const run of runs) {
    const repo = run.repo || 'unknown repository';
    const jobs = run.jobs ?? {};
    const running = jobs.in_progress ?? 0;
    const waiting = (jobs.queued ?? 0) + (jobs.waiting ?? 0);
    if (running + waiting === 0) continue;
    const entry = byRepo.get(repo) ?? { running: 0, waiting: 0 };
    entry.running += running;
    entry.waiting += waiting;
    byRepo.set(repo, entry);
  }
  const rows: FlightRow[] = [...byRepo.entries()]
    .map(([repo, c]) => ({
      repo,
      squares: [
        ...Array<FlightKind>(c.running).fill('running'),
        ...Array<FlightKind>(c.waiting).fill('waiting'),
      ],
      running: c.running,
      waiting: c.waiting,
      note: [
        c.running > 0 ? `${c.running} running` : '',
        c.waiting > 0 ? `${c.waiting} waiting` : '',
      ]
        .filter(Boolean)
        .join(' · '),
    }))
    .sort(
      (a, b) => b.running + b.waiting - (a.running + a.waiting) || a.repo.localeCompare(b.repo),
    );
  const running = rows.reduce((n, r) => n + r.running, 0);
  const waiting = rows.reduce((n, r) => n + r.waiting, 0);
  const summary =
    rows.length === 0 ? 'Nothing in flight' : `${running} running · ${waiting} waiting`;
  return {
    rows: rows.slice(0, MAX_FLIGHT_ROWS),
    more: Math.max(0, rows.length - MAX_FLIGHT_ROWS),
    summary,
  };
}
