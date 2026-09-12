/**
 * How a finished job is counted. Kept apart from the state map in status.ts,
 * which imports icon components and so cannot be loaded outside a browser,
 * because the activity matrix's arithmetic counts jobs the same way and is
 * tested in Node.
 */

/** The conclusions GitHub counts as a job going wrong, as the failed filter does. */
export const FAILING_CONCLUSIONS: ReadonlySet<string> = new Set([
  'failure',
  'timed_out',
  'startup_failure',
]);

/** The four ways a completed job is counted, matching the store's split. */
export type Outcome = 'succeeded' | 'failed' | 'cancelled' | 'unknown';

/**
 * Whether a job went wrong on either side: a failing conclusion, or a runner
 * that stopped under it -- including one GitHub still believes is running.
 */
export function jobFailed(job: {
  conclusion?: string | null;
  runner_fault?: string | null;
}): boolean {
  return Boolean(job.runner_fault) || FAILING_CONCLUSIONS.has((job.conclusion ?? '').toLowerCase());
}

/**
 * Which of the four counts a finished job lands in. Unknown is never a
 * success: a job GitHub stopped reporting is not a job that went well.
 */
export function outcomeOf(job: {
  conclusion?: string | null;
  runner_fault?: string | null;
}): Outcome {
  if (jobFailed(job)) return 'failed';
  const conclusion = (job.conclusion ?? '').toLowerCase();
  if (conclusion === 'success') return 'succeeded';
  if (conclusion === 'cancelled' || conclusion === 'skipped') return 'cancelled';
  return 'unknown';
}
