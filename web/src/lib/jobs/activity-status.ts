import type { Job, WorkflowRun } from '$lib/api/types';
import { CANCELLING, jobStatus, QUEUE_PAUSED, QUEUE_REMOVED, toneTokens } from '$lib/status';
import type { StatusMeta } from '$lib/status';

export interface ActivityStatus {
  status: StatusMeta;
  label: string;
  motion: string;
  detail: string;
}

export function queuedActivity(
  job: Pick<Job, 'provisioning' | 'provision_now' | 'cancel_requested_at'>,
): ActivityStatus {
  if (job.cancel_requested_at)
    return {
      status: CANCELLING,
      label: 'Coming to heel',
      motion: 'draining',
      detail: CANCELLING.hint ?? 'Cancellation requested.',
    };
  const state = job.provisioning || (job.provision_now ? 'expedited' : 'ready');
  if (state === 'paused')
    return {
      status: QUEUE_PAUSED,
      label: 'Stay there',
      motion: 'throttled',
      detail: QUEUE_PAUSED.hint!,
    };
  if (state === 'deleted')
    return {
      status: QUEUE_REMOVED,
      label: 'Back in the kennel',
      motion: 'removed',
      detail: QUEUE_REMOVED.hint!,
    };
  if (state === 'expedited')
    return {
      status: { ...jobStatus('queued'), key: state, label: 'Run now' },
      label: 'First walkies',
      motion: 'registering',
      detail:
        'Prioritised within pool priority. Still waiting for a runner; this job is not running yet.',
    };
  return {
    status: { ...jobStatus('queued'), key: 'ready', label: 'Ready' },
    label: 'Sit, wait',
    motion: 'idle',
    detail:
      'Queued and ready for normal provisioning. Waiting for a matching runner and available capacity.',
  };
}

export function workflowActivity(
  run: Pick<WorkflowRun, 'state' | 'conclusion' | 'cancelling'>,
): ActivityStatus {
  const status = run.cancelling ? CANCELLING : jobStatus(run.state, run.conclusion);
  if (run.cancelling)
    return {
      status,
      label: 'Calling the pack',
      motion: 'draining',
      detail: 'Cancellation requested. Waiting for the workflow and its jobs to stop.',
    };
  if (run.state === 'queued' || run.state === 'waiting')
    return {
      status,
      label: 'Pack waiting',
      motion: 'idle',
      detail: `${status.label}. The workflow is waiting to run. The pack represents the workflow, not a count of its jobs.`,
    };
  if (run.state === 'in_progress')
    return {
      status,
      label: 'Pack on the move',
      motion: 'busy',
      detail:
        'The workflow is running. Individual jobs may still be queued or already finished; expand the row to see each one.',
    };
  if (run.state === 'completed' && run.conclusion?.toLowerCase() === 'success')
    return {
      status,
      label: 'Good pack!',
      motion: 'registering',
      detail: 'The workflow completed successfully.',
    };
  if (status.tone === 'danger')
    return {
      status,
      label: 'Pack needs help',
      motion: 'failed',
      detail: `${status.label}. Expand the workflow to inspect its jobs and errors.`,
    };
  if (run.state === 'completed')
    return {
      status,
      label: 'Pack resting',
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
