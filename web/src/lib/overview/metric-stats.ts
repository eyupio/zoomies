/**
 * The four figures the Overview band carries: what is waiting, what is running,
 * what exists to run it, and how long the queue makes people wait.
 *
 * Plain functions, as the band sentences beside them are, so each figure can be
 * held to its input by a test. They are the figures the tile row under the hero
 * used to show, moved into the band so the page starts on its work rather than
 * on a second row of numbers, and each is a way into the records it counts.
 */
import type { Stats } from '$lib/api/types';
import type { BandStat } from '$lib/bands/stats';
import { formatDuration, formatNumber, parseGoDuration, pluralise } from '$lib/format';

export interface OverviewMetricInput {
  stats: Stats | null | undefined;
  /**
   * Count every job GitHub reports, not only the ones this fleet has a hand
   * in. The same scope the panels below the band are in, so the figures and the
   * lists under them cannot disagree about what they mean.
   */
  others: boolean;
  /** The instant the figures are read, which anchors the wait window's start. */
  now: number;
}

/** Runners that exist or are on their way, which is what a sample counts. */
export function liveRunners(stats: Stats | null | undefined): number {
  const r = stats?.runners;
  if (!r) return 0;
  return (
    (r.provisioning ?? 0) + (r.registering ?? 0) + (r.idle ?? 0) + (r.busy ?? 0) + (r.draining ?? 0)
  );
}

/**
 * Jobs a runner is working on right now.
 *
 * This fleet's own answer is its busy runners, which is what the Runners page
 * counts and what the figure opens. The jobs table is GitHub's report, and a
 * completion delivery can arrive minutes after the runner has already reported
 * it exited: counting the rows in progress then leaves the Overview saying
 * seven jobs are running beside a Runners page with one busy runner, which is
 * two answers to one question. The runner's own state is the fleet's present
 * tense, so the fleet scope uses it.
 *
 * With other runners in scope every job GitHub reports is counted instead,
 * because a hosted or vendor runner is not one of this fleet's and has no row
 * here to be busy.
 */
export function runningJobs(stats: Stats | null | undefined, others: boolean): number {
  if (!stats) return 0;
  return others ? (stats.running_jobs ?? 0) : (stats.runners?.busy ?? 0);
}

const ALL = '&all=true';
const BUSY = '/runners?state=busy#runners-list';
const RUNNING_ALL = `/jobs?state=in_progress${ALL}`;
const LIVE = '/runners?state=provisioning&state=registering&state=idle&state=busy#runners-list';
const AN_HOUR = 60 * 60 * 1000;

/**
 * What the live-runners figure leaves out: the slots it fills, which is the
 * number that explains a queue that is not moving. Kept beside the count rather
 * than in a sentence under the band, where it used to sit.
 */
function slotHint(stats: Stats): string | undefined {
  const hosts = stats.hosts;
  if (!hosts) return undefined;
  if ((hosts.total ?? 0) === 0) return 'No hosts connected';
  return `${hosts.used ?? 0} of ${hosts.capacity ?? 0} slots on ${pluralise(hosts.healthy ?? 0, 'connected host')}`;
}

export function overviewMetrics(input: OverviewMetricInput): BandStat[] {
  const { stats, others, now } = input;
  if (!stats) return [];

  const fleet = others ? stats : (stats.fleet ?? stats);
  const queued = fleet.queued_jobs ?? 0;
  const running = runningJobs(stats, others);
  const live = liveRunners(stats);

  // Undefined rather than zero when there is nothing to time. The controller
  // answers 0 for a median it has no samples for, and a median of "0ms" claims
  // instant work where the no-value placeholder is the truth. A queue wait
  // needs a job that has started on the side of the line being counted, so the
  // gate reads the jobs table rather than the busy runners the figure shows.
  const started = (fleet.running_jobs ?? 0) + (fleet.completed ?? 0);
  const medianWait = started > 0 ? fleet.median_wait_ms : undefined;
  const p95Wait = started > 0 ? fleet.p95_wait_ms : undefined;

  // The wait is measured over the window the controller reported, not one this
  // page guesses: a fleet configured for a day would otherwise send its reader
  // to an hour of completed jobs and call it the figure they came from.
  const windowMs = parseGoDuration(stats.window) ?? AN_HOUR;
  const since = new Date(now - windowMs).toISOString();

  return [
    {
      label: 'Queued jobs',
      value: formatNumber(queued),
      hint: 'Jobs waiting for a runner. Open the queue.',
      // The Queue's default view is this fleet's queued work minus whatever an
      // operator removed from it, exactly the set the figure counts. With other
      // runners switched on the figure counts queues this fleet does not own,
      // which the Queue has nothing to say about, so that is the Jobs page.
      href: others ? `/jobs?state=queued${ALL}` : '/queue',
    },
    {
      label: 'Running jobs',
      value: formatNumber(running),
      hint: 'Runners executing a job right now. Open them.',
      // The busy runners this fleet is running them on, and the anchor that
      // lands the list rather than the band above it. With other runners in
      // scope the figure counts jobs on machines this fleet has no row for, so
      // it opens the jobs themselves instead of a list that cannot hold them.
      href: others ? RUNNING_ALL : BUSY,
    },
    {
      label: 'Live runners',
      value: formatNumber(live),
      hint: others
        ? 'Runners that exist or are on their way. Runners and hosts belong to this fleet, whichever scope is chosen.'
        : 'Runners that exist or are on their way. Open them.',
      // The four live states, not the removed history a bare /runners includes.
      href: LIVE,
      detail: slotHint(stats),
    },
    {
      label: 'Median queue wait',
      value: formatDuration(medianWait),
      hint: 'How long a job waited before a runner picked it up. Open the jobs behind it.',
      // A wait is only measured on a job that finished, so the figure's evidence
      // is the completed jobs in the same window.
      href: `/jobs?state=completed&since=${encodeURIComponent(since)}${others ? ALL : ''}`,
      detail: p95Wait === undefined ? undefined : `p95 ${formatDuration(p95Wait)}`,
    },
  ];
}
