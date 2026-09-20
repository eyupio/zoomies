/**
 * The fault vocabulary: whose a failure is, and what kind of failure it was.
 *
 * GitHub records a job whose runner died exactly as it records a job whose
 * tests failed. The server decides which is which -- it is the only side that
 * can -- and sends `fault_domain`, `fault_kind` and `fault_fix` on every job.
 * This file is the browser's half: the short label a badge shows and the
 * sentence a panel shows, neither of which belongs in a Go string.
 *
 * It is kept apart from status.ts for the same reason outcomes.ts is: that file
 * imports icon components and cannot be loaded outside a browser, and these
 * labels are wanted by tests that run in Node.
 */
import type { FaultKind } from '$lib/api/types';

/** Whose a failure is. Empty on a job that did not fail. */
export type FaultDomain = 'fleet' | 'workflow' | '';

interface FaultMeta {
  /** Two or three words, for a badge beside the outcome. */
  label: string;
  /** What happened, for the panel. The remedy comes from the server. */
  detail: string;
}

/**
 * Every category, in the order the docs list them: the ones that happen to a
 * running job first, then the ones that stop a runner ever taking one.
 *
 * The detail says what happened rather than what to do. What to do is
 * `fault_fix`, which the server sends with the job: one table of remedies, read
 * by the UI, the CLI and the problems drawer, rather than three that drift.
 */
const FAULTS: Record<FaultKind, FaultMeta> = {
  host_lost: {
    label: 'Host lost',
    detail:
      'The host stopped answering while it was running this job, and the runner went with the machine.',
  },
  out_of_memory: {
    label: 'Out of memory',
    detail: 'The runner was killed for exceeding its memory limit.',
  },
  out_of_disk: {
    label: 'Out of disk',
    detail: 'The host ran out of room. This job was the one running when the disk filled.',
  },
  removed: {
    label: 'Removed',
    detail: 'Somebody removed this runner with force while it was working.',
  },
  image: {
    label: 'Image',
    detail: 'The runner image could not be pulled or would not start.',
  },
  registration: {
    label: 'Registration',
    detail: 'GitHub would not register the runner, so it had nothing to attach to.',
  },
  backend: {
    label: 'Container backend',
    detail: 'The container backend on the host refused the work or did not answer.',
  },
  container_conflict: {
    label: 'Container name conflict',
    detail:
      'A container still occupies this name, and it could not be safely reclaimed. Check its ownership and whether another agent uses the same daemon.',
  },
  backend_busy: {
    label: 'Backend overloaded',
    detail:
      'The container backend on the host is there and did not answer in time, or was too slow to finish creating a container. The daemon is running; the machine is carrying more work than it can keep up with.',
  },
  config: {
    label: 'Configuration',
    detail:
      'The runner refused a setting it was given. Every runner in this pool will do the same until it is changed.',
  },
  runner_exited: {
    label: 'Runner stopped',
    detail: 'The runner stopped and the fleet could not narrow down why.',
  },
};

/**
 * The label for a category. A kind this build does not know falls back to its
 * own identifier made readable, rather than to nothing: a fleet running a newer
 * controller should show an unfamiliar word, not an empty badge.
 */
export function faultLabel(kind: string | null | undefined): string {
  if (!kind) return '';
  const known = FAULTS[kind as FaultKind];
  if (known) return known.label;
  return kind.replace(/_/g, ' ').replace(/^./, (c) => c.toUpperCase());
}

/** What happened, for a panel. Empty for a category this build does not know. */
export function faultDetail(kind: string | null | undefined): string {
  if (!kind) return '';
  return FAULTS[kind as FaultKind]?.detail ?? '';
}

/** Every category this build knows, for a filter's options. */
export const FAULT_KINDS = Object.keys(FAULTS) as FaultKind[];

/**
 * Whose a failure is, from the fields the server sends.
 *
 * It reads `fault_domain` and falls back to the same rule the server applies,
 * because a job delivered by an older controller carries the prose and not the
 * domain, and reading such a job as the workflow's would blame somebody for a
 * failure that was ours.
 */
export function faultDomain(job: {
  fault_domain?: string | null;
  fault_kind?: string | null;
  runner_fault?: string | null;
  conclusion?: string | null;
}): FaultDomain {
  if (job.fault_domain === 'fleet' || job.fault_domain === 'workflow') return job.fault_domain;
  if (job.fault_kind || job.runner_fault) return 'fleet';
  return '';
}

/** Whether this deployment, rather than the workflow, is why a job went wrong. */
export function fleetFailed(job: {
  fault_domain?: string | null;
  fault_kind?: string | null;
  runner_fault?: string | null;
}): boolean {
  return faultDomain(job) === 'fleet';
}
