/**
 * The provisioning controls, in one place.
 *
 * Run now, Pause, Resume and Delete from queue are what the Queue page is
 * for, and the first three are offered again on the Workflows page -- on a
 * run's own row, for every queued job of it at once, and on each job inside
 * an opened run. Three pages naming one action three ways is how an operator
 * comes to believe there are three actions, so the verbs, the icons, the
 * reason each gives when it is already in force, and what the confirmation
 * says all come from here.
 */
import { Pause, Play, Trash2, Zap } from '@lucide/svelte';
import type { Body, Job } from '$lib/api/types';
import { QUEUE_STATUS_LABELS } from '$lib/status';
import type { RowAction } from '$lib/components/RowActions.svelte';

export type ProvisioningAction = Body<'controlProvisioning'>['action'];
/** The actions a whole run offers: everything but removal, which is a decision about one job. */
export type RunProvisioningAction = Body<'controlWorkflowRunProvisioning'>['action'];

export interface ProvisioningActionMeta {
  id: ProvisioningAction;
  label: string;
  icon: typeof Zap;
  danger?: boolean;
}

export const PROVISIONING_ACTIONS: readonly ProvisioningActionMeta[] = [
  { id: 'run_now', label: 'Run now', icon: Zap },
  { id: 'pause', label: 'Pause', icon: Pause },
  { id: 'resume', label: 'Resume', icon: Play },
  { id: 'delete', label: 'Delete from queue', icon: Trash2, danger: true },
];

/** The three a run's row offers, in the same order the Queue page puts them. */
export const RUN_PROVISIONING_ACTIONS: readonly ProvisioningActionMeta[] =
  PROVISIONING_ACTIONS.filter((a) => a.id !== 'delete');

export function provisioningActionLabel(action: ProvisioningAction | undefined): string {
  return PROVISIONING_ACTIONS.find((a) => a.id === action)?.label ?? 'Update provisioning';
}

/**
 * Why an action is refused on a job it is already in force on. A control
 * that refuses without a reason is a control that gets reported as broken.
 */
export const PROVISIONING_IN_FORCE: Record<ProvisioningAction, string> = {
  run_now: 'Already prioritised to run now.',
  pause: 'Already paused.',
  resume: 'Already provisioning normally, with nothing to restore.',
  delete: 'Already removed from the queue.',
};

/** What the confirmation says the action will do, for one item or many. */
export function provisioningDescription(action: ProvisioningAction | undefined): string {
  switch (action) {
    case 'pause':
      return 'Hold new runner demand for these items until you resume them.';
    case 'delete':
      return 'Remove these items from provisioning demand, so they stop counting as queued work anywhere. You can restore them from the Removed view.';
    case 'run_now':
      return 'Resume and prioritise these items within their pool priority, without waiting for the scale-up delay.';
    default:
      return 'Restore normal provisioning demand and clear any Run now priority.';
  }
}

/** The status a queued job's demand is in, in the Queue page's four words. */
export function provisioningStatus(
  job: Pick<Job, 'provisioning' | 'provision_now'>,
): keyof typeof QUEUE_STATUS_LABELS {
  if (job.provisioning === 'paused' || job.provisioning === 'deleted') return job.provisioning;
  return job.provision_now ? 'expedited' : 'ready';
}

/** Whether an action on a job in this status would change nothing. */
export function provisioningInForce(
  action: ProvisioningAction,
  status: keyof typeof QUEUE_STATUS_LABELS,
): boolean {
  return (
    (action === 'pause' && status === 'paused') ||
    (action === 'delete' && status === 'deleted') ||
    (action === 'run_now' && status === 'expedited') ||
    (action === 'resume' && status === 'ready')
  );
}

/**
 * Why a job cannot have its demand controlled at all, or nothing when it
 * can. Only a queued job this fleet has a hand in raises demand: once
 * something has started it there is nothing to hold, and a job whose labels
 * all name somebody else's runners was never this fleet's to hurry.
 */
export function provisioningUnavailable(
  job: Pick<Job, 'state' | 'matched' | 'hosted' | 'runner_id' | 'cancel_requested_at'>,
): string | undefined {
  if (job.cancel_requested_at)
    return 'Its workflow run is being cancelled; the fleet has already stood it down.';
  if (job.state === 'completed') return 'This job has already finished.';
  if (job.state === 'in_progress') return 'This job is already running.';
  if (job.state === 'waiting')
    return 'This job is waiting for a review or an environment, not for a runner.';
  if (job.hosted && !job.matched && !job.runner_id)
    return 'This job runs on somebody else’s runners, so there is no demand here to shape.';
  return undefined;
}

/**
 * The row actions for one job, in the Queue page's order, each refused with
 * a reason where it would change nothing. `include` narrows the set: the
 * Workflows page leaves removal to the Queue page.
 */
export function jobProvisioningActions(
  job: Pick<
    Job,
    | 'state'
    | 'matched'
    | 'hosted'
    | 'runner_id'
    | 'cancel_requested_at'
    | 'provisioning'
    | 'provision_now'
  >,
  onSelect: (action: ProvisioningAction) => void,
  include: readonly ProvisioningActionMeta[] = PROVISIONING_ACTIONS,
): RowAction[] {
  const unavailable = provisioningUnavailable(job);
  const status = provisioningStatus(job);
  return include.map((a) => {
    const inForce = provisioningInForce(a.id, status);
    return {
      id: a.id,
      label: a.label,
      icon: a.icon,
      danger: a.danger,
      disabled: Boolean(unavailable) || inForce,
      reason: unavailable ?? (inForce ? PROVISIONING_IN_FORCE[a.id] : undefined),
      onSelect: () => onSelect(a.id),
    };
  });
}
