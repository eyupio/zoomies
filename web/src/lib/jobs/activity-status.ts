import type { Job, WorkflowRun } from '$lib/api/types';
import {
  CANCELLING,
  jobStatus,
  QUEUE_PAUSED,
  QUEUE_REMOVED,
  RUN_FAILING,
  toneTokens,
} from '$lib/status';
import type { StatusMeta } from '$lib/status';

export interface ActivityStatus {
  status: StatusMeta;
  label: string;
  motion: string;
  /**
   * The pose of the rest of a workflow's pack, where it differs from the
   * leader's. A run with a failed job among ones still running is both things
   * at once, and one pose for all three dogs would have to lie about half of it.
   */
  packMotion?: string;
  detail: string;
}

/**
 * `quirky` is passed in by the caller rather than read here -- see the note
 * on `runnerStatus` in `$lib/status` for why this module stays free of it too.
 */
export function queuedActivity(
  job: Pick<Job, 'provisioning' | 'provision_now' | 'cancel_requested_at'>,
  quirky = true,
): ActivityStatus {
  if (job.cancel_requested_at)
    return {
      status: CANCELLING,
      label: quirky ? 'Coming to heel' : CANCELLING.label,
      motion: 'draining',
      detail: CANCELLING.hint ?? 'Cancellation requested.',
    };
  const state = job.provisioning || (job.provision_now ? 'expedited' : 'ready');
  if (state === 'paused')
    return {
      status: QUEUE_PAUSED,
      label: quirky ? 'Stay there' : QUEUE_PAUSED.label,
      motion: 'throttled',
      detail: QUEUE_PAUSED.hint!,
    };
  if (state === 'deleted')
    return {
      status: QUEUE_REMOVED,
      label: quirky ? 'Back in the kennel' : QUEUE_REMOVED.label,
      motion: 'removed',
      detail: QUEUE_REMOVED.hint!,
    };
  if (state === 'expedited') {
    const status = { ...jobStatus('queued'), key: state, label: 'Run now' };
    return {
      status,
      label: quirky ? 'First walkies' : status.label,
      motion: 'registering',
      detail:
        'Prioritised within pool priority. Still waiting for a runner; this job is not running yet.',
    };
  }
  const status = { ...jobStatus('queued'), key: 'ready', label: 'Ready' };
  return {
    status,
    label: quirky ? 'Sit, wait' : status.label,
    motion: 'idle',
    detail:
      'Queued and ready for normal provisioning. Waiting for a matching runner and available capacity.',
  };
}

export function workflowActivity(
  run: Pick<WorkflowRun, 'state' | 'conclusion' | 'cancelling' | 'jobs'>,
  quirky = true,
): ActivityStatus {
  const status = run.cancelling ? CANCELLING : jobStatus(run.state, run.conclusion);
  if (run.cancelling)
    return {
      status,
      label: quirky ? 'Calling the pack' : status.label,
      motion: 'draining',
      detail: 'Cancellation requested. Waiting for the workflow and its jobs to stop.',
    };
  // A failure among jobs still running or waiting is known now, not when the
  // run's own state catches up, so the row turns before GitHub's does. The
  // leader shows the hurt; the pack behind it is still on the move.
  const failed = run.jobs?.failed ?? 0;
  if (run.state !== 'completed' && failed > 0) {
    const total = run.jobs?.total ?? 0;
    const running = run.state === 'in_progress';
    const count = total ? `${failed} of its ${total} jobs` : `${failed} of its jobs`;
    return {
      status: RUN_FAILING,
      label: quirky ? 'Pack in trouble' : RUN_FAILING.label,
      motion: 'failed',
      packMotion: running ? 'busy' : 'idle',
      detail: `${count} ${failed === 1 ? 'has' : 'have'} failed while the rest ${running ? 'are still running' : 'are still waiting to run'}, so the workflow will end in failure unless ${failed === 1 ? 'it is' : 'they are'} re-run. Expand the row to see which.`,
    };
  }
  if (run.state === 'queued' || run.state === 'waiting')
    return {
      status,
      label: quirky ? 'Pack waiting' : status.label,
      motion: 'idle',
      detail: `${status.label}. The workflow is waiting to run. The pack represents the workflow, not a count of its jobs.`,
    };
  if (run.state === 'in_progress')
    return {
      status,
      label: quirky ? 'Pack on the move' : status.label,
      motion: 'busy',
      detail:
        'The workflow is running. Individual jobs may still be queued or already finished; expand the row to see each one.',
    };
  if (run.state === 'completed' && run.conclusion?.toLowerCase() === 'success')
    return {
      status,
      label: quirky ? 'Good pack!' : status.label,
      motion: 'registering',
      detail: 'The workflow completed successfully.',
    };
  if (status.tone === 'danger')
    return {
      status,
      label: quirky ? 'Pack needs help' : status.label,
      motion: 'failed',
      detail: `${status.label}. Expand the workflow to inspect its jobs and errors.`,
    };
  if (run.state === 'completed')
    return {
      status,
      label: quirky ? 'Pack resting' : status.label,
      motion: 'removed',
      detail: `Workflow finished: ${status.label.toLowerCase()}.`,
    };
  return {
    status: { ...status, ...toneTokens('neutral') },
    label: status.label,
    motion: 'unknown',
    detail: 'No recognised workflow status has been reported yet.',
  };
}
