/**
 * The state map.
 *
 * Every status rendered anywhere in Zoomies comes from here, so an operator
 * learns one vocabulary. Each entry carries three things, and the shape is not
 * optional: colour is never the sole carrier of meaning (docs/ui-guidelines.md
 * section 1.1 and the accessibility checklist in section 6).
 *
 *   tone   which status token pair to use (`--z-busy`, `--z-busy-subtle`, ...)
 *   shape  the glyph StatusDot draws -- filled, hollow, dashed, slash, triangle, square
 *   icon   the Lucide icon Badge and the detail pages use
 */
import {
  Ban,
  Circle,
  CircleStop,
  CircleCheck,
  CircleDashed,
  CircleMinus,
  CircleSlash,
  CircleX,
  Clock,
  Cloud,
  Info,
  Minus,
  Dog,
  Pause,
  PawPrint,
  Play,
  Rabbit,
  Squirrel,
  Trash2,
  TriangleAlert,
} from '@lucide/svelte';
import type { LucideIcon } from '@lucide/svelte';
import DogSitting from './icons/DogSitting.svelte';
import type {
  APIToken,
  Host,
  HostThrottle,
  JobEventKind,
  JobState,
  JoinToken,
  MachineState,
  ProvisioningStatus,
  Pool,
  Runner,
  RunnerState,
  Severity,
} from './api/types';

/** The six status hues from the token file. Nothing else may use them. */
export type StatusTone = 'idle' | 'busy' | 'pending' | 'draining' | 'danger' | 'neutral';

/**
 * The shape half of the encoding. `square` extends the five in the guidelines
 * for terminal states, which need to be distinguishable from `draining` at a
 * glance without inventing a colour.
 */
export type StatusShape = 'filled' | 'hollow' | 'dashed' | 'slash' | 'triangle' | 'square';

export interface StatusMeta {
  /** Stable key, useful for `data-` attributes and tests. */
  key: string;
  /** Sentence-case label, shown next to the shape. */
  label: string;
  tone: StatusTone;
  shape: StatusShape;
  icon: LucideIcon;
  /** The token holding this tone's foreground colour. */
  colour: string;
  /** The token for a badge or chart fill in this tone. */
  subtle: string;
  /** The token for a hairline in this tone. */
  border: string;
  /** One line an operator can act on. Used in tooltips and empty states. */
  hint?: string;
}

/** The CSS custom properties for a tone. */
export function toneTokens(tone: StatusTone): { colour: string; subtle: string; border: string } {
  return {
    colour: `var(--z-${tone})`,
    subtle: `var(--z-${tone}-subtle)`,
    border: `var(--z-${tone}-border)`,
  };
}

function meta(
  key: string,
  label: string,
  tone: StatusTone,
  shape: StatusShape,
  icon: LucideIcon,
  hint?: string,
): StatusMeta {
  return { key, label, tone, shape, icon, ...toneTokens(tone), hint };
}

/* -- runners -------------------------------------------------------------- */

const RUNNER: Record<RunnerState, StatusMeta> = {
  provisioning: meta(
    'provisioning',
    'Provisioning',
    'pending',
    'dashed',
    CircleDashed,
    'The host is creating the container.',
  ),
  registering: meta(
    'registering',
    'Registering',
    'pending',
    'dashed',
    CircleDashed,
    'Waiting for GitHub to accept the runner.',
  ),
  idle: meta('idle', 'Idle', 'idle', 'hollow', Circle, 'Registered and waiting for a job.'),
  busy: meta('busy', 'Busy', 'busy', 'filled', Play, 'Running a job right now.'),
  draining: meta(
    'draining',
    'Draining',
    'draining',
    'slash',
    CircleSlash,
    'Finishing its current job, then exiting. No new work is sent to it.',
  ),
  failed: meta(
    'failed',
    'Failed',
    'danger',
    'triangle',
    TriangleAlert,
    'The runner stopped unexpectedly. Its message says why.',
  ),
  removed: meta(
    'removed',
    'Removed',
    'neutral',
    'square',
    CircleMinus,
    'Gone, and deregistered from GitHub.',
  ),
};

const UNKNOWN = meta('unknown', 'Unknown', 'neutral', 'square', CircleMinus);

export function runnerStatus(state: RunnerState | undefined): StatusMeta {
  return state ? (RUNNER[state] ?? UNKNOWN) : UNKNOWN;
}

/** Convenience for a row that has the whole runner to hand. */
export function runnerRowStatus(runner: Pick<Runner, 'state'>): StatusMeta {
  return runnerStatus(runner.state);
}

/** Every runner state with its label, for filter menus. */
export function runnerStatuses(): StatusMeta[] {
  return Object.values(RUNNER);
}

/** Elastic CPU has its own dog-park vocabulary, backed by stable API states. */
export function cpuResourceStatus(state: string | undefined, label?: string): StatusMeta {
  switch (state) {
    case 'maximum_zoomies':
      return meta(state, label ?? 'Squirrel spotted — maximum zoomies', 'busy', 'filled', Squirrel);
    case 'zoomies':
      return meta(state, label ?? 'Rabbit spotted — extra zoomies', 'busy', 'filled', Rabbit);
    case 'throttled':
      return meta(
        state,
        label ?? 'Leash tightened — host under pressure',
        'draining',
        'slash',
        Dog,
      );
    case 'observing':
      return meta(
        state,
        label ?? 'Nose to the wind — watching spare CPU',
        'pending',
        'dashed',
        PawPrint,
      );
    case 'sit_and_stay':
      // A pool with elastic CPU off: the runner is held at exactly its share,
      // not moving and doing as it was told. Neutral rather than idle, because
      // "guaranteed pace" is the elastic pool's word for a runner that may yet
      // be lent something, and this one never will be.
      return meta(
        state,
        label ?? 'Sit and stay — CPU held at its share',
        'neutral',
        'hollow',
        DogSitting,
        'This pool has elastic CPU off, so the runner keeps its share and nothing is lent or taken back.',
      );
    default:
      return meta(
        'guaranteed',
        label ?? 'Steady paws — guaranteed pace',
        'idle',
        'hollow',
        PawPrint,
      );
  }
}

/* -- jobs ----------------------------------------------------------------- */

const JOB_QUEUED = meta(
  'queued',
  'Queued',
  'pending',
  'dashed',
  Clock,
  'Waiting for a runner that answers its labels.',
);
const JOB_RUNNING = meta('in_progress', 'Running', 'busy', 'filled', Play);
const JOB_WAITING = meta(
  'waiting',
  'Waiting',
  'neutral',
  'hollow',
  Clock,
  'Held by GitHub for a deployment review. Nothing runs it until someone approves it.',
);

const CONCLUSIONS: Record<string, StatusMeta> = {
  success: meta('success', 'Success', 'idle', 'filled', CircleCheck),
  failure: meta('failure', 'Failure', 'danger', 'triangle', CircleX),
  cancelled: meta('cancelled', 'Cancelled', 'neutral', 'square', Ban),
  skipped: meta('skipped', 'Skipped', 'neutral', 'hollow', Minus),
  timed_out: meta('timed_out', 'Timed out', 'danger', 'triangle', Clock),
  startup_failure: meta('startup_failure', 'Startup failure', 'danger', 'triangle', CircleX),
  action_required: meta('action_required', 'Action required', 'pending', 'triangle', TriangleAlert),
  neutral: meta('neutral', 'Neutral', 'neutral', 'hollow', Minus),
  stale: meta('stale', 'Stale', 'neutral', 'square', CircleMinus),
};

/** A job's status is its state, except once it is complete, when it is its conclusion. */
export function jobStatus(state: JobState | undefined, conclusion?: string | null): StatusMeta {
  if (state === 'waiting') return JOB_WAITING;
  if (state === 'queued') return JOB_QUEUED;
  if (state === 'in_progress') return JOB_RUNNING;
  if (state === 'completed') {
    const key = (conclusion ?? '').toLowerCase();
    return (
      CONCLUSIONS[key] ?? meta(key || 'completed', 'Completed', 'neutral', 'square', CircleCheck)
    );
  }
  return UNKNOWN;
}

/** A queued job no enabled pool claims. Nothing here will start it. */
export const UNMATCHED: StatusMeta = meta(
  'unmatched',
  'Unmatched',
  'danger',
  'triangle',
  TriangleAlert,
  'No enabled pool here answers these labels, so nothing in this fleet will start it.',
);

/**
 * Whether a job is actually waiting on a pool that does not exist.
 *
 * `matched` alone is not that question. It records only that no enabled pool
 * claims the job's labels, which is equally true of every repository still on
 * GitHub-hosted or vendor runners -- jobs that run perfectly well, just not
 * here. Saying "will never run" about a job that already succeeded turns the
 * Jobs page into a wall of red during exactly the migration this fleet exists
 * to make, so the warning is kept for the one case it is true of: a job still
 * queued, with nothing to hand it to.
 */
export function stuckUnmatched(job: {
  matched?: boolean;
  state?: JobState;
  hosted?: boolean;
}): boolean {
  return job.matched === false && job.state === 'queued' && !job.hosted;
}

/**
 * The operator's vocabulary for a queued job's provisioning demand, in one
 * place: the Queue's status buttons, the Jobs page's filter chip and the badge
 * on a job row all read from here, so the same state is never called two
 * things on two pages. "Removed" rather than "deleted" throughout -- the row
 * is still there, and restoring it is one press.
 */
export const QUEUE_STATUS_LABELS: Record<ProvisioningStatus, string> = {
  ready: 'Ready',
  expedited: 'Run now',
  paused: 'Paused',
  deleted: 'Removed',
};

/**
 * What an operator has done to a queued job's provisioning demand, where they
 * have done anything. Undefined for the ordinary case, which needs no badge.
 *
 * A job stood down from the queue is still `queued` as far as GitHub is
 * concerned -- Zoomies cannot unqueue it -- so a list that showed only the
 * state called it plainly Queued and left the operator's own decision
 * invisible. These say it beside the state wherever a job is listed, which is
 * what keeps a page that shows the job honest about what the fleet will do
 * with it.
 */
export const QUEUE_PAUSED: StatusMeta = meta(
  'paused',
  'Paused',
  'draining',
  'hollow',
  Pause,
  'An operator put this job\u2019s provisioning demand on hold. It is still waiting, and no new runner will be created for it until it is resumed.',
);

export const QUEUE_REMOVED: StatusMeta = meta(
  'removed',
  'Removed',
  'neutral',
  'square',
  Trash2,
  'An operator removed this job from the queue, so it no longer counts as work this fleet is waiting on. Restore it from the Queue page\u2019s Removed view.',
);

/**
 * The badge for a job an operator has stood down, or nothing for one they have
 * not touched. Only a job still queued can carry one: once something has run
 * it, what was done to its demand is history rather than status.
 */
/**
 * A job on its way out because somebody cancelled its workflow run, which
 * GitHub has not yet confirmed.
 *
 * GitHub owns the conclusion, so the row goes on reading `queued` or
 * `in_progress` until its completion delivery lands -- which can be minutes.
 * The job is neither by then: the queued half raises no demand and the running
 * half has had its runner taken away.
 */
export const CANCELLING: StatusMeta = meta(
  'cancelling',
  'Cancelling',
  'neutral',
  'square',
  CircleStop,
  'Its workflow run was cancelled and GitHub has accepted the request. The fleet has already stopped work on it; the conclusion follows when GitHub reports it.',
);

export function queueStatus(job: {
  state?: JobState;
  provisioning?: string;
  cancel_requested_at?: string | null;
}): StatusMeta | undefined {
  // Cancelling outranks the rest, and is the one that applies to a running
  // job as well. A cancellation also pauses the run's queued jobs, so without
  // this a job somebody cancelled would be reported as one an operator had
  // merely put on hold.
  if (job.state !== 'queued' && job.state !== 'in_progress') return undefined;
  if (job.cancel_requested_at) return CANCELLING;
  if (job.state !== 'queued') return undefined;
  if (job.provisioning === 'paused') return QUEUE_PAUSED;
  if (job.provisioning === 'deleted') return QUEUE_REMOVED;
  return undefined;
}

/**
 * A job no runner of this fleet ran. The Overview's panels show these only
 * when asked to: GitHub reports every job in the repositories an installation
 * covers, and on an organisation that also uses hosted, vendor or another
 * self-hosted provider's runners, most of them are somebody else's.
 */
export const ELSEWHERE: StatusMeta = meta(
  'elsewhere',
  'Elsewhere',
  'neutral',
  'hollow',
  Cloud,
  'No runner of this fleet ran this job. It is listed because GitHub reports every job in the repositories this installation covers.',
);

/** A job one of this fleet's runners picked up. */
export function ranHere(job: { runner_id?: string }): boolean {
  return Boolean(job.runner_id);
}

/**
 * Whether this fleet has a hand in a job that is not queued: a pool claims its
 * labels, or a runner here ran it. It mirrors the server's `managed` filter,
 * and lives next to the places that use it so a panel's live frames and its
 * fetch cannot disagree about what belongs on the page -- which they did.
 */
export function managedJob(job: {
  matched?: boolean;
  pool_id?: string;
  runner_id?: string;
}): boolean {
  return Boolean(job.matched || job.pool_id || job.runner_id);
}

/**
 * A job whose labels all name runners somebody else operates: GitHub's own, or
 * a hosted-runner vendor's. It runs there, and is never stuck on this fleet's
 * account however long it queues.
 */
export const HOSTED: StatusMeta = meta(
  'hosted',
  'Hosted elsewhere',
  'neutral',
  'hollow',
  Cloud,
  "Its labels name GitHub's own runners or a hosted-runner vendor's, so it runs there rather than on this fleet.",
);

/**
 * A job whose runner stopped under it. GitHub records the job as an ordinary
 * failure; this badge is how the fleet owns up to having caused it.
 */
export const RUNNER_LOST: StatusMeta = meta(
  'runner_lost',
  'Runner lost',
  'danger',
  'triangle',
  TriangleAlert,
  "The runner executing this job stopped before GitHub reported the job over. The failure is the fleet's, not the workflow's.",
);

/**
 * Whether a job went wrong on either side. The rule lives in outcomes.ts, so
 * the activity matrix can count jobs the same way without loading the icons
 * this file imports; it is re-exported here because this is where every page
 * looks for it.
 */
export { jobFailed } from './outcomes';

/**
 * One step of a job, coloured like the job it belongs to: a step still running
 * is busy, one that has not started is pending, and a finished one takes its
 * conclusion.
 */
export function stepStatus(step: { status?: string; conclusion?: string }): StatusMeta {
  if (step.status === 'completed' || step.conclusion) {
    return jobStatus('completed', step.conclusion);
  }
  if (step.status === 'in_progress') return JOB_RUNNING;
  return meta('queued', 'Not started', 'pending', 'hollow', Clock);
}

/* -- the job timeline ----------------------------------------------------- */

const JOB_EVENTS: Record<JobEventKind, StatusMeta> = {
  queued: JOB_QUEUED,
  waiting: JOB_WAITING,
  approved: meta(
    'approved',
    'Approved',
    'pending',
    'dashed',
    Clock,
    'The deployment review passed. The queue wait starts here.',
  ),
  claimed: meta('claimed', 'Claimed', 'idle', 'hollow', Circle),
  unmatched: UNMATCHED,
  started: JOB_RUNNING,
  completed: meta('completed', 'Completed', 'neutral', 'square', CircleCheck),
  runner_lost: RUNNER_LOST,
  runner_returned: meta(
    'runner_returned',
    'Runner returned',
    'idle',
    'hollow',
    CircleCheck,
    'The runner reported lost is alive after all and this job is still running on it. The entry above it stands as what the fleet believed at the time.',
  ),
  cancel_requested: meta(
    'cancel_requested',
    'Cancellation requested',
    'pending',
    'slash',
    CircleSlash,
    'Zoomies asked GitHub to cancel the workflow run and is waiting for GitHub to confirm the terminal state.',
  ),
  // Pending rather than danger, and the wording is the reason: this job has
  // not failed. A runner meant for work like it died on the way up, the job is
  // still queued, and the next runner may well run it -- drawn red it would
  // read as an outcome, which is the one thing it is not.
  runner_start_failed: meta(
    'runner_start_failed',
    'A runner failed to start',
    'pending',
    'dashed',
    TriangleAlert,
    'A runner this pool started for work like this job died before it could take one. This job is still queued; a pool that cannot start a container otherwise looks exactly like a pool that is merely busy.',
  ),
  rerun_requested: meta(
    'rerun_requested',
    'Re-run requested',
    'pending',
    'dashed',
    Play,
    "Zoomies asked GitHub to run this run's failed jobs again. They arrive as a new run attempt, so GitHub remains authoritative about what happens next.",
  ),
};

/**
 * The status a timeline entry is drawn with. A `completed` entry takes the
 * job's own conclusion when it is known, so the last mark on a failed job's
 * timeline is red rather than a neutral "it ended".
 */
export function jobEventStatus(
  kind: JobEventKind | undefined,
  conclusion?: string | null,
): StatusMeta {
  if (kind === 'completed' && conclusion) return jobStatus('completed', conclusion);
  return kind ? (JOB_EVENTS[kind] ?? UNKNOWN) : UNKNOWN;
}

/* -- hosts ---------------------------------------------------------------- */

/**
 * The top rung of the throttle ladder, mirroring store.MaxThrottleLevel. Each
 * rung takes a quarter of the host's slots; a fourth would take it to nothing,
 * which is a cordon, and a cordon is the operator's to apply.
 */
export const MAX_THROTTLE_LEVEL = 3;

/** Whether the controller has a host on any rung at all. */
export function throttled(throttle: HostThrottle | undefined | null): boolean {
  return (throttle?.level ?? 0) > 0;
}

/**
 * What a throttled host's runners with a CPU limit are running at, as a
 * percentage of their allocation. Mirrors store.HostThrottle.CPUFactor: a
 * quarter off per rung, floored at half, because the top rung takes slots
 * only -- a job at a quarter of its CPU is a job that never finishes.
 */
export function throttleCpuPercent(throttle: HostThrottle | undefined | null): number {
  const level = Math.min(Math.max(throttle?.level ?? 0, 0), MAX_THROTTLE_LEVEL);
  return Math.max(50, 100 - 25 * level);
}

export function hostStatus(host: Pick<Host, 'healthy' | 'cordoned' | 'throttle'>): StatusMeta {
  if (host.healthy === false) {
    return meta(
      'unreachable',
      'Unreachable',
      'danger',
      'triangle',
      TriangleAlert,
      'No heartbeat recently. Check the agent on this host.',
    );
  }
  if (host.cordoned) {
    return meta(
      'cordoned',
      'Cordoned',
      'draining',
      'slash',
      CircleSlash,
      'Existing runners keep going; no new ones are placed here.',
    );
  }
  if (throttled(host.throttle)) {
    // Its own shape: a throttle is neither a cordon (the fleet did it, and
    // will undo it) nor unreachable (the agent is talking; that is how the
    // fleet knows). The step is in the hint because the badge is the one
    // place on the card an operator hovers to ask "how bad".
    return meta(
      'throttled',
      'Throttled',
      'pending',
      'dashed',
      CircleDashed,
      `Throttled, step ${host.throttle?.level} of ${MAX_THROTTLE_LEVEL}: the fleet is taking fewer new runners here after sustained pressure. It lifts one step after five minutes of calm.`,
    );
  }
  return meta('healthy', 'Healthy', 'idle', 'hollow', Circle);
}

/* -- machines -------------------------------------------------------------- */

/**
 * Where a rented machine is in its life.
 *
 * The tones are the fixed mapping every other page uses, and deliberately the
 * same ones a runner gets for the same idea: a machine being built reads as
 * pending exactly as a runner being provisioned does, so an operator learns one
 * vocabulary rather than two. `hasRunners` is what separates a machine that has
 * arrived from one that is carrying work -- the row itself cannot say, because
 * a ready machine and a busy one are the same state.
 */
export function machineStatus(state: MachineState | undefined, hasRunners = false): StatusMeta {
  switch (state) {
    case 'planned':
      return meta(
        'planned',
        'Planned',
        'pending',
        'dashed',
        CircleDashed,
        'The row exists and nothing has been created yet.',
      );
    case 'creating':
      return meta(
        'creating',
        'Creating',
        'pending',
        'dashed',
        CircleDashed,
        'The provider is building the machine.',
      );
    case 'starting':
      return meta(
        'starting',
        'Starting',
        'pending',
        'dashed',
        CircleDashed,
        'The machine exists and is being powered on.',
      );
    case 'bootstrapping':
      return meta(
        'bootstrapping',
        'Bootstrapping',
        'pending',
        'dashed',
        CircleDashed,
        'The guest is up and the agent is being installed.',
      );
    case 'enrolling':
      return meta(
        'enrolling',
        'Enrolling',
        'pending',
        'dashed',
        CircleDashed,
        'The agent has its credential and has not joined yet.',
      );
    case 'ready':
      return hasRunners
        ? meta('busy', 'Running work', 'busy', 'filled', Play, 'A host, with runners on it.')
        : meta('ready', 'Ready', 'idle', 'hollow', Circle, 'A host, waiting for work.');
    case 'draining':
      return meta(
        'draining',
        'Draining',
        'draining',
        'slash',
        CircleSlash,
        'Its runners finish; the machine is deleted when the last one does.',
      );
    case 'deleting':
      return meta(
        'deleting',
        'Deleting',
        'draining',
        'slash',
        CircleSlash,
        'A delete was issued and the provider has not confirmed the resource is gone.',
      );
    case 'failed':
      return meta(
        'failed',
        'Failed',
        'danger',
        'triangle',
        TriangleAlert,
        'The lifecycle was given up on. The resource behind it may still exist, and still cost money.',
      );
    case 'quarantined':
      return meta(
        'quarantined',
        'Quarantined',
        'danger',
        'triangle',
        TriangleAlert,
        'Ownership of this resource could not be proved, so nothing here will act on it. Only a person moves it.',
      );
    case 'deleted':
      return meta(
        'deleted',
        'Deleted',
        'neutral',
        'square',
        CircleMinus,
        'The provider confirmed the resource is gone.',
      );
    default:
      return UNKNOWN;
  }
}

/* -- pools ---------------------------------------------------------------- */

export function poolStatus(pool: Pick<Pool, 'enabled'>): StatusMeta {
  return pool.enabled === false
    ? meta(
        'disabled',
        'Disabled',
        'draining',
        'slash',
        CircleSlash,
        'Existing runners drain; no new ones are made.',
      )
    : meta('enabled', 'Enabled', 'idle', 'hollow', Circle);
}

/* -- problems and deliveries ---------------------------------------------- */

export function severityStatus(severity: Severity | undefined): StatusMeta {
  if (severity === 'error') return meta('error', 'Error', 'danger', 'triangle', TriangleAlert);
  if (severity === 'warning')
    return meta('warning', 'Warning', 'pending', 'triangle', TriangleAlert);
  return meta('info', 'Info', 'busy', 'hollow', Info);
}

export type DeliveryStatus = 'accepted' | 'rejected' | 'error';

export function deliveryStatus(status: DeliveryStatus | undefined): StatusMeta {
  if (status === 'accepted') return meta('accepted', 'Accepted', 'idle', 'filled', CircleCheck);
  if (status === 'rejected') {
    return meta(
      'rejected',
      'Rejected',
      'danger',
      'triangle',
      CircleX,
      'The signature did not verify. Check the webhook secret.',
    );
  }
  if (status === 'error') return meta('error', 'Error', 'danger', 'triangle', TriangleAlert);
  return UNKNOWN;
}

/** GitHub App connection health. */
export function installationStatus(healthy: boolean | undefined): StatusMeta {
  if (healthy === true) return meta('healthy', 'Connected', 'idle', 'hollow', Circle);
  if (healthy === false) {
    return meta(
      'unhealthy',
      'Not connected',
      'danger',
      'triangle',
      TriangleAlert,
      'Verify the installation to see which credential or permission is missing.',
    );
  }
  return meta('unchecked', 'Not checked', 'neutral', 'dashed', CircleDashed);
}

/** A user or token account state. */
export function accountStatus(disabled: boolean | undefined): StatusMeta {
  return disabled
    ? meta('disabled', 'Disabled', 'draining', 'slash', CircleSlash)
    : meta('active', 'Active', 'idle', 'hollow', Circle);
}

/* -- credentials ---------------------------------------------------------- */

/** How soon "expires soon" is: a week, so the warning lands on a working day. */
const EXPIRY_WARNING_MS = 7 * 24 * 60 * 60 * 1000;

/** Where an API token is in its life: revoked, expired, expiring, or good. */
export function apiTokenStatus(
  token: Pick<APIToken, 'revoked' | 'expires_at'>,
  now: number = Date.now(),
): StatusMeta {
  if (token.revoked) {
    return meta(
      'revoked',
      'Revoked',
      'draining',
      'slash',
      CircleSlash,
      'Switched off by an administrator.',
    );
  }
  if (!token.expires_at) {
    return meta(
      'never_expires',
      'Never expires',
      'pending',
      'triangle',
      TriangleAlert,
      'A token with no expiry stays valid until it is revoked.',
    );
  }
  const remaining = new Date(token.expires_at).getTime() - now;
  if (remaining <= 0) return meta('expired', 'Expired', 'neutral', 'square', CircleMinus);
  if (remaining < EXPIRY_WARNING_MS) {
    return meta(
      'expiring',
      'Expires soon',
      'pending',
      'dashed',
      Clock,
      'Create a replacement before it does.',
    );
  }
  return meta('active', 'Active', 'idle', 'hollow', Circle);
}

/** Whether a join token can still enrol a host. */
export function joinTokenStatus(token: Pick<JoinToken, 'used_at' | 'usable'>): StatusMeta {
  if (token.used_at) {
    return meta(
      'used',
      'Used',
      'neutral',
      'square',
      CircleMinus,
      'A join token enrols one host, once.',
    );
  }
  if (token.usable === false) {
    return meta(
      'expired',
      'Expired',
      'draining',
      'slash',
      CircleSlash,
      'Create a new one to add a host.',
    );
  }
  return meta('usable', 'Usable', 'idle', 'hollow', Circle);
}

/** Whether the webhook address answered the controller's own probe. */
export function reachabilityStatus(reachable: boolean | undefined): StatusMeta {
  if (reachable === true) return meta('reachable', 'Reachable', 'idle', 'filled', CircleCheck);
  if (reachable === false) {
    return meta(
      'unreachable',
      'Not reachable',
      'danger',
      'triangle',
      TriangleAlert,
      'Nothing answered at the webhook address, so GitHub cannot deliver to it either.',
    );
  }
  return meta('unchecked', 'Not checked', 'neutral', 'dashed', CircleDashed);
}
