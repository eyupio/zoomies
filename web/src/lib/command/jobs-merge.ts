/**
 * The command centre's rail, as pure functions: which jobs are waiting, which
 * have just finished, and how a frame from the event stream changes either.
 *
 * Kept free of runes so the order of a list and the refusal to move a job
 * backwards are held by a test. Webhook deliveries are at-least-once and can
 * arrive out of order, and a wall display that let a late frame put a finished
 * job back into the queue would show work nobody is waiting for.
 */
import type { Job, JobState, Runner } from '../api/types';
import { toMillis } from '../format';
import { elapsedMs } from './time';

export const WAITING_SHOWN = 5;
export const PROBLEMS_SHOWN = 3;
export const FINISHED_SHOWN = 5;
export const FINISHED_WINDOW_MS = 10 * 60 * 1000;

const RANK: Record<JobState, number> = { waiting: 0, queued: 1, in_progress: 2, completed: 3 };
const rank = (job: Job): number => (job.state ? RANK[job.state] : -1);

/**
 * Whether a waiting job is one the Queue page's default view lists: queued
 * and not started, not removed by an operator, and not a job whose labels all
 * name somebody else's runners (the `managed` rule for a queued job).
 */
function waitsHere(job: Job): boolean {
  return (
    job.state === 'queued' &&
    !job.started_at &&
    job.provisioning !== 'deleted' &&
    (job.matched === true || job.hosted !== true)
  );
}

/** A finished job is the fleet's when a pool claimed it or a runner here ran it. */
function finishedHere(job: Job): boolean {
  return job.state === 'completed' && (job.matched === true || !!job.runner_id);
}

const at = (time: string | null | undefined): number => toMillis(time) ?? 0;

function longestFirst(a: Job, b: Job): number {
  return at(a.queued_at) - at(b.queued_at) || (a.id ?? '').localeCompare(b.id ?? '');
}

function newestFirst(a: Job, b: Job): number {
  return at(b.completed_at) - at(a.completed_at) || (a.id ?? '').localeCompare(b.id ?? '');
}

/** True when the list already holds this job at a later point in its life. */
function movesBackwards(list: readonly Job[], frame: Job): boolean {
  const held = list.find((j) => j.id === frame.id);
  return held !== undefined && rank(frame) < rank(held);
}

export function mergeWaiting(list: readonly Job[], frame: Job): Job[] {
  if (!frame.id || movesBackwards(list, frame)) return [...list];
  const rest = list.filter((j) => j.id !== frame.id);
  return waitsHere(frame) ? [...rest, frame].sort(longestFirst) : rest;
}

/** The finished jobs still inside the window, so a list on a wall never grows stale rows. */
export function pruneFinished(list: readonly Job[], now: number): Job[] {
  const since = now - FINISHED_WINDOW_MS;
  return list.filter((j) => at(j.completed_at) >= since);
}

export function mergeFinished(list: readonly Job[], frame: Job, now: number): Job[] {
  if (!frame.id || movesBackwards(list, frame) || !finishedHere(frame)) {
    return pruneFinished(list, now);
  }
  const rest = list.filter((j) => j.id !== frame.id);
  return pruneFinished([...rest, frame].sort(newestFirst), now);
}

export interface RailLists {
  waiting: Job[];
  finished: Job[];
}

/**
 * The fetched lists with the frames that landed while the fetch ran laid over
 * them. Those frames are newer than anything the fetch returns: after a
 * reconnect the stream's replay arrives while the lists are being fetched
 * again, and a fetch laid down last would put back a job that started in the
 * gap.
 */
export function replayFrames(fetched: RailLists, frames: readonly Job[], now: number): RailLists {
  let waiting = fetched.waiting;
  let finished = fetched.finished;
  for (const frame of frames) {
    waiting = mergeWaiting(waiting, frame);
    finished = mergeFinished(finished, frame, now);
  }
  return { waiting, finished };
}

const FAILED = new Set(['failure', 'timed_out', 'startup_failure']);
const NEUTRAL_LABELS: Record<string, string> = { cancelled: 'Cancelled', skipped: 'Skipped' };

export interface FinishedOutcome {
  kind: 'succeeded' | 'failed' | 'neutral';
  /** The mark's accessible name: the shape carries it, so it must be said too. */
  label: string;
}

export function finishedOutcome(job: Pick<Job, 'conclusion'>): FinishedOutcome {
  const conclusion = (job.conclusion ?? '').toLowerCase();
  if (conclusion === 'success') return { kind: 'succeeded', label: 'Succeeded' };
  if (FAILED.has(conclusion)) return { kind: 'failed', label: 'Failed' };
  return { kind: 'neutral', label: NEUTRAL_LABELS[conclusion] ?? 'Finished' };
}

export interface WaitingSummary {
  /** Jobs that want a runner, have none on its way, and have no slot to take. */
  withoutSlot: number;
  longestWaitMs: number;
  /** The pool the longest of those waits for, when it names one. */
  pool: string | null;
}

/**
 * What the verdict needs from the waiting list.
 *
 * A paused job and one whose run is being cancelled raise no demand, as the
 * Queue page's reasons say, so neither waits for a slot. A job of a pool with
 * a runner starting or standing idle is covered by it, one runner for each
 * job, because every runner here is ephemeral and takes exactly one job. What
 * is left waits without a slot when the fleet has no free slot at all, or
 * when no pool claims it, since then no free slot could ever take it. Counting
 * every uncovered job while slots stood free would turn the headline to "a
 * queue" for the second between a job arriving and its runner being asked
 * for, which a wall display would flicker with all day.
 */
export function waitingSummary(
  waiting: readonly Job[],
  runners: readonly Runner[],
  free: number,
  now: number,
): WaitingSummary {
  const cover = new Map<string, number>();
  for (const r of runners) {
    if (!r.pool_id) continue;
    if (r.state === 'provisioning' || r.state === 'registering' || r.state === 'idle') {
      cover.set(r.pool_id, (cover.get(r.pool_id) ?? 0) + 1);
    }
  }
  let withoutSlot = 0;
  let pool: string | null = null;
  let longestWaitMs = 0;
  for (const job of [...waiting].sort(longestFirst)) {
    // A paused job waits because an operator held it, which is no queue.
    if (job.provisioning === 'paused') continue;
    longestWaitMs = Math.max(longestWaitMs, elapsedMs(job.queued_at, now) ?? 0);
    if (job.cancel_requested_at) continue;
    const left = job.pool_id ? (cover.get(job.pool_id) ?? 0) : 0;
    if (job.pool_id && left > 0) {
      cover.set(job.pool_id, left - 1);
      continue;
    }
    if (free > 0 && job.pool_id) continue;
    if (withoutSlot === 0) pool = job.pool_name || null;
    withoutSlot += 1;
  }
  return { withoutSlot, longestWaitMs, pool };
}
