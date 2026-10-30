import type { Schemas } from '../api/types';
import { parseRepairTarget } from './target';

/*
  The repair page's state, held in its hands rather than in a component: what
  the address says it is about, and which of five things it is showing. It
  replaces the stage machine the old single-card flow kept in journey.ts, which
  also had to follow a running repair; the attempt now has a page of its own.
*/

export type WorkspaceStage = 'checking' | 'blocked' | 'ready' | 'changed' | 'error';

/**
 * What the address names. `run` and `pull` are zero when absent, `job` is a
 * Zoomies job id (the one the person opened, so it can be marked), and `offer`
 * is an offer's id.
 */
export interface RepairTarget {
  repo: string;
  pull: number;
  run: number;
  job: string;
  offer: string;
}

/** A positive whole number, or 0. The address is typed into, so nothing else gets through. */
function whole(value: string | null): number {
  if (!value || !/^[0-9]+$/.test(value)) return 0;
  const n = Number(value);
  return Number.isSafeInteger(n) && n > 0 ? n : 0;
}

export function parseTarget(query: URLSearchParams): RepairTarget {
  const repo = (query.get('repo') ?? '').trim();
  const pull = whole(query.get('pull'));
  const linked = !repo || !pull ? parseRepairTarget(query.get('url') ?? '') : null;
  return {
    repo: repo || (linked?.repo ?? ''),
    pull: pull || (linked?.pull_number ?? 0),
    run: whole(query.get('run')),
    job: (query.get('job') ?? '').trim(),
    offer: (query.get('offer') ?? '').trim(),
  };
}

/** The repair page's address for a target, leaving out whatever is empty or zero. */
export function targetHref(target: Partial<RepairTarget>): string {
  const parts: string[] = [];
  const add = (key: string, value: string | number | undefined): void => {
    if (value === undefined || value === '' || value === 0) return;
    parts.push(`${key}=${encodeURIComponent(String(value))}`);
  };
  add('repo', target.repo);
  add('pull', target.pull);
  add('run', target.run);
  add('job', target.job);
  add('offer', target.offer);
  return parts.length ? `/repairs/new?${parts.join('&')}` : '/repairs/new';
}

/**
 * Whether a failed job can be offered to Eli at all. Eli pushes one commit to an
 * existing pull request's branch and never raises one, so the failure of a push,
 * merge or scheduled run has nothing to repair. A run whose trigger the
 * controller has not read yet is still offered: the repair page says why if it
 * turns out not to be one.
 */
export function eliCanRepairRun(job: { run_event?: string }): boolean {
  const event = job.run_event ?? '';
  return event === '' || event === 'pull_request' || event === 'pull_request_target';
}

const CHANGED = ['fixed', 'closed', 'superseded'] as const;

/**
 * The reason the target is no longer worth repairing, when it is not: the checks
 * passed, the pull request closed, or a newer commit replaced the one that
 * failed. These are answers, not obstacles, and they replace the page's card.
 */
export function changedBlocker(
  r: Schemas['EliReadiness'],
): { code: (typeof CHANGED)[number]; message: string } | undefined {
  for (const blocker of r.blockers) {
    const code = CHANGED.find((c) => c === blocker.code);
    if (code) return { code, message: blocker.message };
  }
  return undefined;
}

/**
 * Which of the five things the page is showing. A read that failed outranks an
 * answer left over from the one before; an answer that is not ready is blocked
 * even when it names no blocker, because the page must not offer a start on it.
 */
export function stageOf(r: Schemas['EliReadiness'] | null, failed: boolean): WorkspaceStage {
  if (failed) return 'error';
  if (!r) return 'checking';
  if (changedBlocker(r)) return 'changed';
  return r.ready && r.blockers.length === 0 ? 'ready' : 'blocked';
}
