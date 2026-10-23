/**
 * The figures the Usage band carries: what the chosen range did, and how much
 * runner time it took.
 *
 * The report already answers these questions in its table and charts. The
 * figures live here so the page opens on the same sentence as every other
 * fleet page, and so a phone, which never draws the band, still gets the
 * numbers at the top instead of below a wall of controls.
 *
 * Plain functions, as the sentences beside them are, so each figure can be
 * held to its input by a test.
 */
import { formatDuration, formatNumber, NO_VALUE } from '../format';
import type { UsageRow } from '../api/types';
import type { BandStat } from './stats';
import type { BandLine } from './lines';

export interface UsageTotals {
  jobs: number;
  started: number;
  completed: number;
  succeeded: number;
  failed: number;
  cancelled: number;
  unknown: number;
  seconds: number;
  allocated: number;
  wait: number;
}

const count = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

export function usageTotals(rows: readonly UsageRow[]): UsageTotals {
  return rows.reduce(
    (a, r) => ({
      jobs: a.jobs + r.jobs,
      started: a.started + r.jobs_started,
      completed: a.completed + r.jobs_completed,
      succeeded: a.succeeded + (r.succeeded ?? 0),
      failed: a.failed + (r.failed ?? 0),
      cancelled: a.cancelled + (r.cancelled ?? 0),
      unknown: a.unknown + (r.unknown ?? 0),
      seconds: a.seconds + r.job_execution_seconds,
      allocated: a.allocated + (r.allocated_runner_seconds ?? 0),
      wait: a.wait + (r.average_queue_wait_seconds ?? 0) * r.jobs_started,
    }),
    {
      jobs: 0,
      started: 0,
      completed: 0,
      succeeded: 0,
      failed: 0,
      cancelled: 0,
      unknown: 0,
      seconds: 0,
      allocated: 0,
      wait: 0,
    },
  );
}

/**
 * The same figures the page's tiles used to show, in the order the table reads
 * them. `attributable` is false for a repository or workflow grouping, where a
 * runner has no owner to charge, so the busy share is left out rather than
 * claimed.
 */
export function usageStats(total: UsageTotals, attributable: boolean): BandStat[] {
  const stats: BandStat[] = [
    {
      label: 'Jobs queued',
      value: formatNumber(total.jobs),
      hint: `${formatNumber(total.started)} started inside this range.`,
    },
    {
      label: 'Success rate',
      value: total.completed
        ? `${((100 * total.succeeded) / total.completed).toFixed(1)}%`
        : NO_VALUE,
      hint: total.completed
        ? `${formatNumber(total.succeeded)} of ${formatNumber(total.completed)} completed jobs succeeded.`
        : 'Nothing has finished in this range yet.',
    },
    {
      label: 'Failed jobs',
      value: formatNumber(total.failed),
      hint: `${formatNumber(total.cancelled)} cancelled or skipped, ${formatNumber(
        total.unknown,
      )} with no outcome on record.`,
    },
    {
      label: 'Execution hours',
      value: (total.seconds / 3600).toFixed(2),
      hint: 'Running time inside this range.',
    },
    {
      label: 'Average queue wait',
      value: total.started ? formatDuration((1000 * total.wait) / total.started) : NO_VALUE,
      hint: 'Weighted across the jobs that started.',
    },
  ];
  if (attributable) {
    stats.push({
      label: 'Runner busy share',
      // A share only while the runner records are all there. A range that
      // reaches past runner retention has some of its jobs' hours and only
      // some of its runners', and the ratio then means nothing.
      value:
        total.allocated && total.allocated >= total.seconds
          ? `${((100 * total.seconds) / total.allocated).toFixed(1)}%`
          : NO_VALUE,
      hint:
        total.allocated && total.allocated < total.seconds
          ? 'Runner records for part of this range are no longer retained.'
          : `${(total.allocated / 3600).toFixed(2)} allocated runner-hours.`,
    });
  }
  return stats;
}

/**
 * What the band says: how much work the range held, and how it went. `when`
 * is the range as a phrase the sentence can use ("today", "in the last day",
 * "in this range").
 */
export function usageLine(total: UsageTotals, when: string): BandLine {
  if (total.jobs === 0) {
    return {
      headline: 'Nothing was queued in this range.',
      accent: '',
      lede: 'Widen the range, or change what the report is grouped by.',
    };
  }
  return {
    headline: `${count(total.jobs, 'job', 'jobs')} queued ${when}.`,
    accent: total.completed
      ? `${((100 * total.succeeded) / total.completed).toFixed(1)}% succeeded.`
      : 'Nothing has finished yet.',
    lede: `${total.failed} failed. ${(total.seconds / 3600).toFixed(2)} execution hours.`,
  };
}
