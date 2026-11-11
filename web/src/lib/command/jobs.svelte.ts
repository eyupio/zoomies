/**
 * The jobs the command centre's rail shows: what waits, and what finished in
 * the last ten minutes. One fetch each, then kept current by `job.updated`.
 *
 * It is the command centre's own, and lives only while the page does: no
 * other page wants a ten-minute window, and a store left running behind a
 * closed page would keep a wall display's worth of jobs in every tab.
 */
import { ApiError, listJobs, listProvisioning } from '../api/client';
import { events } from '../api/sse';
import type { Job } from '../api/types';
import { QUEUE_DEFAULT_QUERY } from '../queue/default-query';
import { clock } from './clock.svelte';
import {
  FINISHED_WINDOW_MS,
  mergeFinished,
  mergeWaiting,
  pruneFinished,
  replayFrames,
} from './jobs-merge';

// The most a request may ask for. A longer queue is still counted, from the
// total the read reports, but only this many of its jobs are held.
const WAITING_LIMIT = 500;
const FINISHED_LIMIT = 50;

let waiting = $state.raw<Job[]>([]);
let finished = $state.raw<Job[]>([]);
/**
 * Jobs in the window the first fetch did not return. They are the oldest of
 * the window, because the fetch is newest first, so the moment any job it did
 * return ages out they have all gone too, and this goes to zero.
 */
let unfetched = $state(0);
/**
 * Queued jobs past the fetch's cap: the newest of the queue, since the fetch is
 * oldest first. They are counted from the read's total so "and N more" is the
 * queue's length, not the cap's. Frames do not move it (a job that leaves the
 * queue from here was never listed, and one that joins it is listed), so it
 * stands until the next fetch, which a refresh or a reconnect makes.
 */
let unlisted = $state(0);
let error = $state.raw<ApiError | null>(null);
/**
 * True once a fetch has answered, either way. Until then the rail draws its
 * rows' shapes: an empty list read before anything arrived would say "nothing
 * is waiting" about a queue nobody has looked at yet.
 */
let loaded = $state(false);
let started = false;
let pending: AbortController | null = null;
/**
 * Frames that landed while a fetch was in flight. They are drawn at once, on
 * the lists the page holds, and laid again over the fetched lists when the
 * fetch answers, because they are newer than anything it returns.
 */
let held: Job[] = [];
let stopFrames: (() => void) | null = null;
let stopPruning: (() => void) | null = null;

function prune(now: number): void {
  const kept = pruneFinished(finished, now);
  if (kept.length === finished.length) return;
  finished = kept;
  unfetched = 0;
}

async function load(): Promise<void> {
  pending?.abort();
  const abort = new AbortController();
  pending = abort;
  held = [];
  const since = new Date(Date.now() - FINISHED_WINDOW_MS).toISOString();
  try {
    const [queue, done] = await Promise.all([
      listProvisioning(
        { ...QUEUE_DEFAULT_QUERY, sort: 'queued_at', order: 'asc', limit: WAITING_LIMIT },
        abort.signal,
      ),
      listJobs(
        {
          state: ['completed'],
          completed_since: since,
          managed: true,
          // Full jobs, not `include_steps=false`: that returns summaries, which
          // carry neither the runner nor the claim the drawer and the merge read,
          // and a row opened from the list would be a different job from the
          // same row once a frame replaced it. Fifty jobs' steps is the price.
          sort: 'completed_at',
          order: 'desc',
          limit: FINISHED_LIMIT,
        },
        abort.signal,
      ),
    ]);
    if (pending !== abort) return;
    // Built through the merges, so a fetched row obeys the same rules as a frame.
    const fetchedWaiting = (queue.items ?? []).reduce<Job[]>(mergeWaiting, []);
    const now = Date.now();
    const items = done.items ?? [];
    const fetchedFinished = items.reduce<Job[]>((list, job) => mergeFinished(list, job, now), []);
    ({ waiting, finished } = replayFrames(
      { waiting: fetchedWaiting, finished: fetchedFinished },
      held,
      now,
    ));
    held = [];
    pending = null;
    unfetched = Math.max(0, (done.total ?? 0) - items.length);
    unlisted = Math.max(0, (queue.total ?? 0) - (queue.items ?? []).length);
    error = null;
    loaded = true;
  } catch (e) {
    if (abort.signal.aborted || pending !== abort) return;
    // The held frames are already on the lists the page keeps showing.
    held = [];
    pending = null;
    error =
      e instanceof ApiError
        ? e
        : new ApiError({ status: 0, code: 'internal', message: 'Could not load the jobs.' });
    loaded = true;
  }
}

export const commandJobs = {
  get waiting(): Job[] {
    return waiting;
  },
  get finished(): Job[] {
    return finished;
  },
  /** How many wait: the jobs held, and any past the fetch's cap. */
  get waitingCount(): number {
    return waiting.length + unlisted;
  },
  get finishedCount(): number {
    return finished.length + unfetched;
  },
  get error(): ApiError | null {
    return error;
  },
  get loaded(): boolean {
    return loaded;
  },
  start(): void {
    if (started) return;
    started = true;
    stopFrames = events.subscribe('job.updated', (job) => {
      if (pending) held.push(job);
      waiting = mergeWaiting(waiting, job);
      finished = mergeFinished(finished, job, Date.now());
    });
    // Pruned on the page's own clock, once a minute: the clock stops while
    // the stream is down, and so does the window, with everything else.
    let minute = -1;
    stopPruning = $effect.root(() => {
      $effect(() => {
        const now = clock.now;
        const next = Math.floor(now / 60_000);
        if (next === minute) return;
        minute = next;
        prune(now);
      });
    });
    void load();
  },
  stop(): void {
    if (!started) return;
    started = false;
    stopFrames?.();
    stopPruning?.();
    stopFrames = stopPruning = null;
    pending?.abort();
    pending = null;
    held = [];
    // A return visit starts from the skeleton, not from the last visit's
    // lists, which nothing has kept current since.
    waiting = [];
    finished = [];
    unfetched = 0;
    unlisted = 0;
    error = null;
    loaded = false;
  },
  /** Fetch both lists afresh: after a failure, or when the stream returns from a gap. */
  retry(): Promise<void> {
    return load();
  },
};
