/**
 * What each item on the command centre says when it is hovered or focused: a
 * title and a few label and value rows, for the shared tooltip's structured
 * card.
 *
 * Pure, so every row and its order is held by a test. A value that is not
 * known is left out rather than drawn as an empty row or a dash, because a
 * tooltip is the place for what is known about a thing, and a card of dashes
 * reads as something that failed to load. Everything a workflow names (a job,
 * a repository, a branch) is passed through as a plain string: the bubble puts
 * it on the page as text.
 */
import type { Host, Job, Problem, Runner, Stats } from '../api/types';
import type { FeedEntry } from '../feed/entries';
import {
  capitalise,
  formatBytes,
  formatDuration,
  formatMegabytes,
  formatNumber,
  formatPercent,
  pluralise,
  relativeTime,
  toMillis,
} from '../format';
import { readableJobName } from '../jobs/name';
import { MAX_THROTTLE_LEVEL, runnerStatus, throttled } from '../status';
import { row, type TipDetail, type TipRow } from '../tooltip-detail';
import { lastWindow, measured, type HeroFigure } from './figures';
import { finishedOutcome } from './jobs-merge';
import { freeSlots, type Lane } from './lanes';
import { elapsedMs } from './time';

/** How many pools the ring's card lists before it says how many more there are. */
export const RING_POOLS_SHOWN = 6;

/** A count as text, only when there is one. */
function count(value: number | undefined | null): string | undefined {
  return value === undefined || value === null ? undefined : formatNumber(value);
}

/** "4m 10s ago", or nothing when the time is not usable. */
function ago(since: string | null | undefined, now: number): string | undefined {
  const ms = elapsedMs(since ?? undefined, now);
  return ms === null ? undefined : `${formatDuration(ms)} ago`;
}

/** A share given out of a hundred, as every percentage on the page is written. */
function percent(value: number | undefined): string | undefined {
  return value === undefined ? undefined : formatPercent(value / 100);
}

/** The job's where: repository, workflow and branch, in that order. */
function jobPlace(job: Job | undefined): TipRow[] {
  return [
    ...row('Repository', job?.repo),
    ...row('Workflow', job?.workflow),
    ...row('Branch', job?.head_branch),
  ];
}

/**
 * The runner's CPU against what it was given. The container runtime counts a
 * whole CPU as a hundred, so 150 on two CPUs is three quarters of them.
 */
function runnerCpu(runner: Runner): string | undefined {
  if (runner.cpu_percent === undefined) return undefined;
  const cpus = runner.allocated_cpus;
  if (!cpus || cpus <= 0) return percent(runner.cpu_percent);
  return `${formatPercent(runner.cpu_percent / (cpus * 100))} of ${pluralise(cpus, 'CPU')}`;
}

function runnerMemory(runner: Runner): string | undefined {
  if (runner.memory_bytes === undefined) return undefined;
  const used = formatBytes(runner.memory_bytes);
  const given = runner.allocated_memory_mb;
  return given && given > 0 ? `${used} of ${formatMegabytes(given)}` : used;
}

export interface RunnerTipContext {
  /** The lane's host name, which a runner record does not always carry. */
  hostName?: string;
  now: number;
  /** The operator's choice of status words, as every status on the page reads it. */
  quirky?: boolean;
}

/** A runner tile: the job it is running, or what it is doing without one. */
export function runnerTip(runner: Runner, context: RunnerTipContext): TipDetail {
  const job = runner.current_job;
  // The tile's own words: a runner with no job is idle unless it is going away.
  const title = job
    ? readableJobName(job.job_name)
    : runner.state === 'draining'
      ? 'Draining'
      : runner.state === 'failed'
        ? 'Failed'
        : 'Idle';
  const running = elapsedMs(job?.started_at ?? undefined, context.now);
  return {
    title,
    rows: [
      ...row('Runner', runner.name),
      ...row('State', runner.state ? runnerStatus(runner.state, context.quirky).label : undefined),
      ...row('Pool', runner.pool_name),
      ...row('Host', context.hostName || runner.host_name),
      ...jobPlace(job),
      ...row('Running for', running === null ? undefined : formatDuration(running)),
      ...row('CPU', runnerCpu(runner)),
      ...row('Memory', runnerMemory(runner)),
      ...row('Jobs handled', count(runner.jobs_handled)),
      ...row('Image', runner.image),
    ],
  };
}

const STEP: Partial<Record<NonNullable<Runner['state']>, string>> = {
  provisioning: 'Provisioning',
  registering: 'Registering',
};

/** A starting tile: which step the runner is at, and how long since it was asked for. */
export function startingTip(
  runner: Runner,
  context: Pick<RunnerTipContext, 'hostName' | 'now'>,
): TipDetail {
  return {
    title: STEP[runner.state ?? 'provisioning'] ?? 'Starting',
    rows: [
      ...row('Runner', runner.name),
      ...row('Pool', runner.pool_name),
      ...row('Host', context.hostName || runner.host_name),
      ...row('Started', ago(runner.created_at, context.now)),
    ],
  };
}

const LIMITED_BY: Record<NonNullable<Host['slots_limited_by']>, string> = {
  cpu: 'CPU',
  memory: 'Memory',
  capacity: 'Capacity',
};

/** Why a throttled host is throttled, in the controller's words where it gave some. */
function throttleWords(host: Host): string | undefined {
  if (!throttled(host.throttle)) return undefined;
  return (
    host.throttle_reason ||
    host.throttle?.reason ||
    `Step ${host.throttle?.level} of ${MAX_THROTTLE_LEVEL}`
  );
}

/** The free cell: how much room is left, and what decides how much room there is. */
export function freeTip(lane: Pick<Lane, 'host' | 'slots' | 'free'>): TipDetail {
  const { host, slots, free } = lane;
  return {
    title: freeSlots(free),
    rows: [
      { label: 'In use', value: `${formatNumber(slots - free)} of ${pluralise(slots, 'slot')}` },
      ...row('Limited by', host.slots_limited_by ? LIMITED_BY[host.slots_limited_by] : undefined),
      ...row('Throttle', throttleWords(host)),
    ],
  };
}

/** The lane's status in the same order its note picks one: the worst first. */
function hostState(host: Host): string {
  if (host.healthy === false) return 'Not answering';
  if (throttled(host.throttle)) {
    return `Throttled, step ${host.throttle?.level} of ${MAX_THROTTLE_LEVEL}`;
  }
  if (host.cordoned) return 'Cordoned';
  return 'Healthy';
}

/**
 * A host lane's header. The machine's measurements are only given while a
 * recent sample stands behind them, as the header's own CPU figure is: a stale
 * figure on a wall display reads as the host being calm.
 */
export function hostTip(lane: Pick<Lane, 'host' | 'slots' | 'free'>): TipDetail {
  const { host, slots, free } = lane;
  const usage = host.usage_fresh ? host.usage : undefined;
  const load = usage?.load_average_1m;
  return {
    title: host.name || host.id || 'Host',
    rows: [
      { label: 'Status', value: hostState(host) },
      { label: 'Slots', value: `${formatNumber(slots - free)} of ${formatNumber(slots)} in use` },
      ...row('CPU', percent(usage?.cpu_percent)),
      ...row('Load average', load === undefined ? undefined : load.toFixed(2)),
      ...row('I/O wait', percent(usage?.io_wait_percent)),
      ...row(
        'Memory available',
        usage?.memory_available_mb === undefined
          ? undefined
          : formatMegabytes(usage.memory_available_mb),
      ),
      ...row('Size class', host.size_class ? capitalise(host.size_class.class) : undefined),
      ...row('Agent version', host.version),
    ],
  };
}

/**
 * The ring: the slots by what is in them, then the pools by how busy they
 * are. Counted from the same lanes the deck draws, so the card and the tiles
 * cannot disagree.
 */
export function ringTip(lanes: readonly Lane[], opens: string): TipDetail {
  let slots = 0;
  let free = 0;
  let busy = 0;
  let starting = 0;
  let idle = 0;
  const pools = new Map<string, { busy: number; live: number }>();
  for (const lane of lanes) {
    slots += lane.slots;
    free += lane.free;
    for (const cell of lane.cells) {
      const runner = cell.runner;
      if (!runner) continue;
      if (cell.kind === 'starting') starting += 1;
      else if (runner.state === 'busy') busy += 1;
      else if (runner.state === 'idle') idle += 1;
      if (!runner.pool_name) continue;
      const pool = pools.get(runner.pool_name) ?? { busy: 0, live: 0 };
      pool.live += 1;
      if (runner.state === 'busy') pool.busy += 1;
      pools.set(runner.pool_name, pool);
    }
  }
  const byLoad = [...pools].sort(
    ([an, a], [bn, b]) => b.live - a.live || b.busy - a.busy || an.localeCompare(bn),
  );
  const shown = byLoad.slice(0, RING_POOLS_SHOWN);
  const rest = byLoad.length - shown.length;
  return {
    title: 'Slots in use',
    rows: [
      { label: 'Busy', value: `${formatNumber(busy)} of ${formatNumber(slots)}` },
      { label: 'Starting', value: formatNumber(starting) },
      { label: 'Idle', value: formatNumber(idle) },
      { label: 'Free', value: formatNumber(free) },
      ...shown.map(([name, pool]) => ({
        label: name,
        value: `${formatNumber(pool.busy)} of ${pluralise(pool.live, 'runner')} busy`,
      })),
      ...(rest > 0 ? [{ label: 'Other pools', value: `and ${formatNumber(rest)} more` }] : []),
      { label: 'Opens', value: opens },
    ],
  };
}

/** The stats window in words: "Last hour", "Last 24 hours". */
function windowRow(stats: Stats | null | undefined): TipRow {
  return { label: 'Window', value: capitalise(lastWindow(stats?.window).words) };
}

function duration(ms: number | undefined): string | undefined {
  return ms === undefined ? undefined : formatDuration(ms);
}

/**
 * One of the hero's three figures. The gates are the figure's own: a wait is
 * only a figure once a job has started, and a timing of zero means nothing was
 * measured, so neither becomes a row the figure itself would not show.
 */
export function figureTip(figure: HeroFigure, stats: Stats | null | undefined): TipDetail {
  const own = stats?.fleet ?? stats ?? null;
  const opens = { label: 'Opens', value: figure.opens };
  switch (figure.key) {
    case 'wait': {
      const started = (own?.running_jobs ?? 0) + (own?.completed ?? 0);
      return {
        title: 'Queue wait',
        rows: [
          ...row('Median', started > 0 ? duration(own?.median_wait_ms) : undefined),
          ...row('p95', started > 0 ? duration(own?.p95_wait_ms) : undefined),
          windowRow(stats),
          opens,
        ],
      };
    }
    case 'start':
      return {
        title: 'Runner start',
        rows: [
          ...row('Median', duration(measured(stats?.p50_startup_ms))),
          ...row('p95', duration(measured(stats?.p95_startup_ms))),
          ...row('Registration median', duration(measured(stats?.p50_registration_ms))),
          ...row('Registration p95', duration(measured(stats?.p95_registration_ms))),
          windowRow(stats),
          opens,
        ],
      };
    case 'succeeded':
      return {
        title: figure.label,
        rows: [
          ...row('Succeeded', count(own?.succeeded)),
          ...row('Failed', count(own?.failed)),
          ...row('Cancelled', count(own?.cancelled)),
          ...row('Other', count(own?.unknown)),
          ...row('Finished', count(own?.completed)),
          windowRow(stats),
          opens,
        ],
      };
  }
}

/** The hour chart: where each line is now and the highest it reached. */
export function hourTip(
  points: readonly { running: number; queued: number }[],
  opens: string,
): TipDetail {
  const last = points[points.length - 1];
  const peak = (key: 'running' | 'queued') => Math.max(...points.map((p) => p[key]));
  return {
    title: 'Last hour',
    rows: [
      ...(last
        ? [
            { label: 'Running now', value: formatNumber(last.running) },
            { label: 'Running peak', value: formatNumber(peak('running')) },
            { label: 'Queued now', value: formatNumber(last.queued) },
            { label: 'Queued peak', value: formatNumber(peak('queued')) },
          ]
        : []),
      { label: 'Opens', value: opens },
    ],
  };
}

/** What the Queue page says about a waiting job: its short label and its sentence. */
export interface QueueActivity {
  label: string;
  detail: string;
}

/** A waiting row: what the job is, where it comes from, and why it waits. */
export function waitingTip(job: Job, activity: QueueActivity, now: number): TipDetail {
  return {
    title: readableJobName(job.job_name),
    rows: [
      ...jobPlace(job),
      ...row('Labels', job.labels?.length ? job.labels.join(', ') : undefined),
      ...row('Queued', ago(job.queued_at, now)),
      ...row('Status', activity.label),
      ...row('Why', activity.detail),
      ...row('Pool', job.pool_name),
    ],
  };
}

/**
 * A problem row: the whole of what the rail cuts to a line. `target` is the
 * name of what it is about (a host's or a pool's), where the page knows it.
 */
export function problemTip(problem: Problem, target?: string | null): TipDetail {
  const kind = problem.target_kind ? capitalise(problem.target_kind) : '';
  const name = target || problem.target_id;
  return {
    title: problem.title,
    rows: [
      ...row('Detail', problem.detail),
      ...row('Fix', problem.fix),
      ...row('Target', name ? [kind, name].filter(Boolean).join(' ') : undefined),
    ],
  };
}

/** A finished row: how it ended, how long it took, and where it ran. */
export function finishedTip(job: Job, hostName?: string | null): TipDetail {
  const outcome = finishedOutcome(job);
  return {
    title: readableJobName(job.job_name),
    rows: [
      { label: 'Result', value: outcome.label },
      ...row('Took', typeof job.duration_ms === 'number' ? formatDuration(job.duration_ms) : null),
      ...row(
        'Waited',
        typeof job.queue_wait_ms === 'number' ? formatDuration(job.queue_wait_ms) : null,
      ),
      ...row('Failed step', outcome.kind === 'failed' ? job.failed_step?.name : undefined),
      ...row('Runner', job.runner_name),
      ...row('Host', hostName),
      ...jobPlace(job),
    ],
  };
}

const CLOCK = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' });

/** A ticker entry: the time as the strip shows it, and how long ago that was. */
export function tickerTip(
  entry: Pick<FeedEntry, 'title' | 'detail' | 'at' | 'target'>,
  now: number,
): TipDetail {
  const at = toMillis(entry.at);
  return {
    title: entry.title,
    rows: [
      ...row('Detail', entry.detail),
      ...row('When', at === null ? undefined : `${CLOCK.format(at)}, ${relativeTime(at, now)}`),
      ...row('About', entry.target?.label),
    ],
  };
}
