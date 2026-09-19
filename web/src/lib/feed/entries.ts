/**
 * One line of the Overview's feed, whatever it is about.
 *
 * Every entry is the same four things -- a mark in one of the six status
 * tones, what happened, what it happened to, and when -- so that a feed
 * carrying nine kinds of news still reads as one list rather than nine
 * stacked widgets. The detail line underneath is the controller's own
 * sentence where there is one: the scheduler's reason, the throttle's
 * explanation, the hypervisor's complaint. Paraphrasing those is how a
 * dashboard stops being trustworthy.
 *
 * The tones are the fixed status mapping every other page uses, and they are
 * used for the same meanings: danger for something that went wrong, draining
 * for something going away, pending for something under way or under
 * pressure, idle for something that came good, neutral for a fact.
 */
import {
  Boxes,
  CircleMinus,
  Plug,
  Minus,
  Plus,
  ScrollText,
  ServerCog,
  TrendingDown,
  TrendingUp,
} from '@lucide/svelte';
import type { LucideIcon } from '@lucide/svelte';
import type {
  AuditEvent,
  Host,
  Installation,
  Job,
  Machine,
  MachineState,
  Pool,
  Problem,
  Provider,
  Runner,
  ScalingEvent,
  WebhookDelivery,
} from '../api/types';
import { shortId } from '../format';
import {
  deliveryStatus,
  hostStatus,
  jobStatus,
  machineStatus,
  managedJob,
  runnerStatus,
  severityStatus,
  RUNNER_LOST,
  type StatusTone,
} from '../status';
import type { FeedCategoryID } from './categories';
import type { HostChange, InstallationChange, JobTrouble, ProviderChange } from './changes';

export interface FeedEntry {
  /**
   * Stable for the fact rather than for the frame, so the same thing arriving
   * twice -- a fetch and the replay of the frame it overlapped with -- is one
   * line. A repeated key in a keyed list is a framework error, not a cosmetic
   * one.
   */
  id: string;
  category: FeedCategoryID;
  /** When it happened, as the payload said, or when it arrived. */
  at: string;
  tone: StatusTone;
  icon: LucideIcon;
  /** What happened, in the fewest words that are still true. */
  title: string;
  /** The controller's own sentence about it, where there is one. */
  detail?: string;
  /** What it happened to, and the page that has the whole of it. */
  target?: { label: string; href?: string };
}

/** Now, in the shape every timestamp in the API has. */
export function now(): string {
  return new Date().toISOString();
}

/* -- scaling --------------------------------------------------------------- */

/**
 * A scheduler decision.
 *
 * `index` is only there for a controller old enough to have written a
 * decision without an id; two decisions in the same second on the same pool
 * would otherwise share a key.
 */
export function scalingEntry(event: ScalingEvent, index: number): FeedEntry {
  const from = event.from;
  const to = event.to;
  const direction =
    from === undefined || to === undefined || to === from ? 'hold' : to > from ? 'up' : 'down';
  const counts = from !== undefined && to !== undefined ? ` ${from} → ${to}` : '';
  return {
    id: `scaling:${event.id ?? `${event.pool_id ?? ''}:${event.created_at ?? ''}:${index}`}`,
    category: 'scaling',
    at: event.created_at ?? '',
    tone: direction === 'up' ? 'pending' : direction === 'down' ? 'draining' : 'neutral',
    icon: direction === 'up' ? TrendingUp : direction === 'down' ? TrendingDown : Minus,
    title:
      direction === 'up'
        ? `Scaled up${counts}`
        : direction === 'down'
          ? `Scaled down${counts}`
          : 'Held',
    detail: event.reason || undefined,
    target: {
      label: event.pool_name ?? 'Unnamed pool',
      href: event.pool_id ? `/pools/${event.pool_id}` : undefined,
    },
  };
}

/* -- runners --------------------------------------------------------------- */

/**
 * A runner that failed. The fix is the detail rather than the message where
 * there is one: a runner that died before ever taking a job is the case this
 * exists for, and nothing else in the fleet says why a pool's containers will
 * not start.
 */
export function runnerFailureEntry(runner: Runner): FeedEntry | null {
  if (runner.state !== 'failed' || !runner.id) return null;
  const status = runnerStatus('failed');
  return {
    id: `runner:${runner.id}:failed`,
    category: 'runners',
    at: runner.finished_at ?? runner.created_at ?? '',
    tone: status.tone,
    icon: status.icon,
    title: 'A runner failed',
    detail: runner.fault_fix || runner.message || undefined,
    target: { label: runner.name ?? shortId(runner.id), href: `/runners/${runner.id}` },
  };
}

/* -- jobs ------------------------------------------------------------------ */

/** A failed job, and whose failure it was. */
export function jobEntry(job: Job, trouble: JobTrouble): FeedEntry | null {
  if (!job.id || !managedJob(job)) return null;
  const lost = trouble === 'runner_lost';
  const status = lost ? RUNNER_LOST : jobStatus('completed', job.conclusion);
  const step = job.failed_step?.name;
  const where = [job.repo, job.head_branch].filter(Boolean).join(' · ');
  return {
    id: `job:${job.id}`,
    category: 'jobs',
    at: job.completed_at ?? job.started_at ?? '',
    tone: 'danger',
    icon: status.icon,
    title: lost ? 'A job’s runner stopped under it' : 'A job failed',
    detail: lost
      ? job.fault_fix || RUNNER_LOST.hint
      : step
        ? `Failed at “${step}”. ${where}`
        : where,
    target: {
      label: `${job.workflow ?? 'Unknown workflow'} / ${job.job_name ?? 'unnamed job'}`,
      href: `/jobs?q=${encodeURIComponent(job.job_name ?? '')}&repo=${encodeURIComponent(job.repo ?? '')}`,
    },
  };
}

/* -- hosts ----------------------------------------------------------------- */

const HOST_NEWS: Record<HostChange, { title: string; tone: StatusTone }> = {
  joined: { title: 'A host joined the fleet', tone: 'idle' },
  unreachable: { title: 'A host stopped answering', tone: 'danger' },
  recovered: { title: 'A host is answering again', tone: 'idle' },
  cordoned: { title: 'A host was cordoned', tone: 'draining' },
  uncordoned: { title: 'A host was uncordoned', tone: 'idle' },
  throttled: { title: 'A host was throttled', tone: 'pending' },
  calm: { title: 'A host’s throttle lifted', tone: 'idle' },
  incompatible: { title: 'A host’s agent is too old for this controller', tone: 'danger' },
};

export function hostEntry(host: Host, change: HostChange, at: string): FeedEntry | null {
  if (!host.id) return null;
  const news = HOST_NEWS[change];
  const status = hostStatus(host);
  return {
    id: `host:${host.id}:${change}:${at}`,
    category: 'hosts',
    at,
    tone: news.tone,
    icon: change === 'joined' ? Plus : status.icon,
    title: news.title,
    detail:
      change === 'throttled'
        ? host.throttle_reason || status.hint
        : change === 'incompatible'
          ? host.incompatible_reason || undefined
          : change === 'unreachable'
            ? status.hint
            : undefined,
    target: { label: host.name ?? shortId(host.id), href: '/hosts' },
  };
}

export function hostRemovedEntry(id: string, name: string | undefined, at: string): FeedEntry {
  return {
    id: `host:${id}:removed:${at}`,
    category: 'hosts',
    at,
    tone: 'neutral',
    icon: CircleMinus,
    title: 'A host was removed',
    target: { label: name ?? shortId(id), href: '/hosts' },
  };
}

/* -- machines and providers ------------------------------------------------- */

const MACHINE_NEWS: Record<string, string> = {
  creating: 'A machine is being rented',
  ready: 'A machine became a host',
  draining: 'A machine is draining',
  deleted: 'A machine was returned',
  failed: 'A machine failed',
  quarantined: 'A machine was quarantined',
};

export function machineEntry(machine: Machine, state: MachineState, at: string): FeedEntry {
  const status = machineStatus(state);
  return {
    id: `machine:${machine.id}:${state}`,
    category: 'machines',
    at: machine.updated_at ?? at,
    tone: status.tone,
    icon: status.icon,
    title: MACHINE_NEWS[state] ?? `A machine is ${status.label.toLowerCase()}`,
    detail:
      machine.provider_error ||
      machine.bootstrap_error ||
      machine.ownership_error ||
      machine.message ||
      undefined,
    target: { label: machine.name, href: `/machines/${machine.id}` },
  };
}

const PROVIDER_NEWS: Record<ProviderChange, { title: string; tone: StatusTone }> = {
  paused: { title: 'A provider was paused', tone: 'draining' },
  resumed: { title: 'A provider was resumed', tone: 'idle' },
  disabled: { title: 'A provider was disabled', tone: 'draining' },
  enabled: { title: 'A provider was enabled', tone: 'idle' },
  unreachable: { title: 'A provider could not be reached', tone: 'danger' },
  reachable: { title: 'A provider answered again', tone: 'idle' },
};

export function providerEntry(
  provider: Provider,
  change: ProviderChange,
  at: string,
): FeedEntry | null {
  if (!provider.id) return null;
  const news = PROVIDER_NEWS[change];
  return {
    id: `provider:${provider.id}:${change}:${at}`,
    category: 'machines',
    at,
    tone: news.tone,
    icon: ServerCog,
    title: news.title,
    detail:
      change === 'unreachable'
        ? provider.last_check_error || undefined
        : change === 'paused'
          ? provider.paused_reason || undefined
          : undefined,
    target: { label: provider.name ?? shortId(provider.id), href: `/providers/${provider.id}` },
  };
}

/* -- pools ------------------------------------------------------------------ */

export function poolEntry(pool: Pool, change: 'created' | 'updated', at: string): FeedEntry | null {
  if (!pool.id) return null;
  return {
    id: `pool:${pool.id}:${change}:${at}`,
    category: 'pools',
    at,
    tone: change === 'created' ? 'idle' : 'neutral',
    icon: change === 'created' ? Plus : Boxes,
    title: change === 'created' ? 'A pool was created' : 'A pool changed',
    target: { label: pool.name ?? shortId(pool.id), href: `/pools/${pool.id}` },
  };
}

export function poolRemovedEntry(id: string, name: string | undefined, at: string): FeedEntry {
  return {
    id: `pool:${id}:deleted:${at}`,
    category: 'pools',
    at,
    tone: 'neutral',
    icon: CircleMinus,
    title: 'A pool was deleted',
    target: { label: name ?? shortId(id), href: '/pools' },
  };
}

/* -- GitHub ------------------------------------------------------------------ */

const INSTALLATION_NEWS: Record<InstallationChange, { title: string; tone: StatusTone }> = {
  connected: { title: 'A GitHub App was connected', tone: 'idle' },
  failing: { title: 'The GitHub App connection is failing', tone: 'danger' },
  working: { title: 'The GitHub App connection is working again', tone: 'idle' },
};

export function installationEntry(
  installation: Installation,
  change: InstallationChange,
  at: string,
): FeedEntry | null {
  if (!installation.id) return null;
  const news = INSTALLATION_NEWS[change];
  return {
    id: `installation:${installation.id}:${change}:${at}`,
    category: 'github',
    at,
    tone: news.tone,
    icon: Plug,
    title: news.title,
    detail: change === 'failing' ? installation.last_error || undefined : undefined,
    target: {
      label: installation.target ?? shortId(installation.id),
      href: '/installations',
    },
  };
}

/**
 * A delivery this controller would not take. An accepted one is the system
 * working, and there are thousands of them.
 */
export function deliveryEntry(delivery: WebhookDelivery): FeedEntry | null {
  if (delivery.status !== 'rejected' && delivery.status !== 'error') return null;
  const status = deliveryStatus(delivery.status);
  const event = [delivery.event, delivery.action].filter(Boolean).join('.');
  return {
    id: `webhook:${delivery.id ?? delivery.delivery_id ?? now()}`,
    category: 'github',
    at: delivery.received_at ?? now(),
    tone: status.tone,
    icon: status.icon,
    title:
      delivery.status === 'rejected'
        ? 'A webhook delivery was rejected'
        : 'A webhook delivery could not be handled',
    detail: delivery.error || status.hint,
    target: {
      label: event || delivery.repo || 'delivery',
      href: '/installations',
    },
  };
}

/* -- problems and the record ------------------------------------------------- */

export function problemEntry(problem: Problem, at: string): FeedEntry {
  const status = severityStatus(problem.severity);
  return {
    id: `problem:${problem.code}:${problem.target_id ?? ''}:${at}`,
    category: 'problems',
    at: problem.since ?? at,
    tone: status.tone,
    icon: status.icon,
    title: problem.title ?? 'A problem was raised',
    detail: problem.fix || problem.detail || undefined,
    target: { label: problem.code ?? 'problem' },
  };
}

/**
 * One recorded action. The action name is printed as it was recorded rather
 * than translated into prose: it is what the Audit page filters on and what a
 * support conversation quotes.
 */
export function auditEntry(event: AuditEvent): FeedEntry | null {
  if (!event.id || !event.action) return null;
  const actor = event.actor_name || event.actor_kind || 'somebody';
  const target =
    event.target_kind && event.target_id
      ? `${event.target_kind} ${shortId(event.target_id)}`
      : undefined;
  return {
    id: `audit:${event.id}`,
    category: 'audit',
    at: event.created_at ?? now(),
    tone: 'neutral',
    icon: ScrollText,
    title: `${actor} ran ${event.action}`,
    detail: target,
    target: {
      label: 'Audit',
      href: `/audit?action=${encodeURIComponent(event.action)}`,
    },
  };
}
