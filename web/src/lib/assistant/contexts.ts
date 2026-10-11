/**
 * What each page hands Eli when its Ask Eli button is pressed: the facts the
 * page is already showing, as labelled values, and the question the page is
 * usually opened to answer.
 *
 * They are built here, in one place, and not in the components, because the
 * discipline is the same for all of them and is worth reading in one screen:
 * displayed facts only, named one by one, never a whole API object, and never
 * a credential or an address. A name somebody chose (a job, a repository, a
 * branch) is a displayed fact too, and goes as one; the controller's prompt
 * tells the model that such text is data and not instructions.
 */

import type { EliContext } from './prompts';

/** The job drawer's explanation, as much of it as the context reads. */
export interface WhyFacts {
  summary: string;
  detail?: string;
  fix?: string;
  class: string;
  confidence: string;
  confidence_reason?: string;
  problem_code?: string;
  evidence: readonly { label: string; value: string; unit?: string }[];
  next_steps: readonly { text: string }[];
}

export interface JobFacts {
  id?: string;
  job_name?: string;
  repo?: string;
  workflow?: string;
  state?: string;
  conclusion?: string;
  pool_name?: string;
  runner_name?: string;
  labels?: readonly string[];
}

/**
 * A job and what the fleet made of it. The evidence and the next steps go as
 * two lines rather than a list each, so the snapshot stays the size of a
 * paragraph; Eli has get_job for the whole record.
 */
export function jobContext(
  job: JobFacts,
  why: WhyFacts | null,
  mode: 'failed' | 'waiting',
): EliContext {
  return {
    kind: 'job',
    title: job.job_name || job.id || 'Job',
    facts: {
      'Job ID': job.id,
      Repository: job.repo,
      Workflow: job.workflow,
      State: job.conclusion ? `${job.state} (${job.conclusion})` : job.state,
      Labels: job.labels?.length ? job.labels.join(', ') : undefined,
      Pool: job.pool_name,
      Runner: job.runner_name,
      Verdict: why?.summary,
      Detail: why?.detail,
      Class: why ? `${why.class} (${why.confidence} confidence)` : undefined,
      'Confidence reason': why?.confidence_reason,
      'Problem code': why?.problem_code,
      Evidence: why?.evidence.length
        ? why.evidence.map((e) => `${e.label}: ${e.value}${e.unit ? ` ${e.unit}` : ''}`).join('; ')
        : undefined,
      'Suggested fix': why?.fix,
      'Next steps': why?.next_steps.length
        ? why.next_steps.map((s) => s.text).join(' ')
        : undefined,
    },
    question:
      mode === 'failed'
        ? 'Why did this job fail, and what should I do first?'
        : 'Why is this job still waiting, and what should I do first?',
  };
}

export interface PoolFacts {
  id?: string;
  name?: string;
  enabled?: boolean;
  backend?: string;
  labels?: readonly string[];
  min_runners?: number;
  max_runners?: number;
  queued_jobs?: number;
  /** The labels a provider must carry to buy for this pool; empty allows any. */
  provider_selector?: Record<string, string>;
}

export interface PoolCounts {
  live?: number;
  busy?: number;
  idle?: number;
  provisioning?: number;
  registering?: number;
}

/** A pool's shape and what it is doing right now. */
export function poolContext(pool: PoolFacts, counts: PoolCounts): EliContext {
  const live = counts.live ?? 0;
  const busy = counts.busy ?? 0;
  return {
    kind: 'pool',
    title: pool.name || pool.id || 'Pool',
    facts: {
      'Pool ID': pool.id,
      Enabled: pool.enabled,
      Backend: pool.backend,
      Labels: pool.labels?.length ? pool.labels.join(', ') : undefined,
      'Minimum runners': pool.min_runners,
      'Maximum runners': pool.max_runners,
      'Live runners': live,
      Busy: busy,
      Idle: counts.idle,
      Starting: (counts.provisioning ?? 0) + (counts.registering ?? 0),
      'Utilisation (%)': live > 0 ? Math.round((busy / live) * 100) : undefined,
      'Queued jobs': pool.queued_jobs,
      'Provider selector':
        pool.provider_selector && Object.keys(pool.provider_selector).length
          ? Object.entries(pool.provider_selector)
              .map(([k, v]) => `${k}=${v}`)
              .join(', ')
          : undefined,
    },
    question:
      'Is this pool sized right for what it is being asked to run, and what should I check?',
  };
}

export interface FindingFacts {
  code: string;
  severity: string;
  subject?: string;
  title: string;
  detail: string;
  fix: string;
  evidence?: readonly { kind: string; label?: string; ref: string }[];
}

/**
 * One Kennel Club finding about one repository. The agent prompt the finding
 * carries is left out: it is written for a coding agent working in the
 * repository, and Eli has kennel_findings for it.
 */
export function findingContext(repository: string, finding: FindingFacts): EliContext {
  const seen = (finding.evidence ?? []).map((e) => `${e.kind}: ${e.label || e.ref}`);
  return {
    kind: 'kennel finding',
    title: finding.title,
    facts: {
      Repository: repository,
      Code: finding.code,
      Severity: finding.severity,
      About: finding.subject || undefined,
      Detail: finding.detail,
      'What to change': finding.fix,
      'Where it was seen': seen.length ? seen.join('; ') : undefined,
    },
    question:
      'Explain this finding, how much it matters for this repository, and how I would fix it.',
  };
}

export interface UsageRowFacts {
  key: string;
  jobs: number;
  jobs_completed: number;
  job_execution_seconds: number;
  allocated_runner_seconds: number | null;
  average_queue_wait_seconds: number | null;
  peak_concurrency: number;
  estimated_cost?: number;
}

export interface UsageFacts {
  grouping: string;
  since: string;
  until: string;
  /** The one group the page is focused on, or empty for all of them. */
  entity?: string;
  rows: readonly UsageRowFacts[];
  /** Whether runner time can be attributed at this grouping at all. */
  attributable: boolean;
}

/**
 * The usage report's totals, not its rows: a report is a table, and a table
 * pasted into a question is read by nobody. Eli has get_usage for the rows.
 */
export function usageContext(usage: UsageFacts): EliContext {
  const sum = (pick: (r: UsageRowFacts) => number | null | undefined): number =>
    usage.rows.reduce((total, r) => total + (pick(r) ?? 0), 0);
  const hours = (seconds: number): number => Math.round((seconds / 3600) * 100) / 100;
  const executed = sum((r) => r.job_execution_seconds);
  const allocated = usage.attributable ? sum((r) => r.allocated_runner_seconds) : null;
  const priced = usage.attributable && usage.rows.some((r) => r.estimated_cost !== undefined);
  const waits = usage.rows
    .map((r) => r.average_queue_wait_seconds)
    .filter((w): w is number => w !== null);
  return {
    kind: 'usage report',
    title: usage.entity ? `${usage.grouping} ${usage.entity}` : `Usage by ${usage.grouping}`,
    facts: {
      From: usage.since,
      To: usage.until,
      'Grouped by': usage.grouping,
      Groups: usage.rows.length,
      Jobs: sum((r) => r.jobs),
      'Jobs completed': sum((r) => r.jobs_completed),
      'Execution hours': hours(executed),
      'Runner-hours': allocated === null ? undefined : hours(allocated),
      'Utilisation (%)':
        allocated !== null && allocated > 0 ? Math.round((executed / allocated) * 100) : undefined,
      'Longest mean queue wait (s)': waits.length ? Math.round(Math.max(...waits)) : undefined,
      'Peak concurrency': usage.rows.length
        ? Math.max(...usage.rows.map((r) => r.peak_concurrency))
        : undefined,
      'Estimated cost': priced ? Math.round(sum((r) => r.estimated_cost) * 100) / 100 : undefined,
    },
    question: 'What stands out in this usage, and where is the waste or the waiting?',
  };
}
