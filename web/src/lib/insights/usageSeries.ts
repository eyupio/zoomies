/**
 * The usage chart's figures: a report's intervals as lines.
 *
 * Pure, and imported by a unit test, so nothing here touches the DOM or the
 * clock: the caller passes `now`. The matrix draws the same buckets as
 * squares, so what a figure means is settled in one place and the two cannot
 * drift.
 *
 * Two measures rather than one chart, because two units do not share an axis:
 * a count of jobs and a number of hours drawn against the same gridlines
 * would put "twelve" and "twelve hours" at the same height and invite the
 * reader to compare them. Which figures of the chosen measure are drawn is
 * the operator's to say, one chip each.
 */
import { intervalWidth, type ActivityBucket, type Interval } from './activity';
import { seriesLine, type Series, type SeriesLine } from './plot';
import { formatNumber, toMillis } from '../format';

export type UsageMeasure = 'jobs' | 'runtime';

export type UsageKey =
  'queued' | 'succeeded' | 'failed' | 'cancelled' | 'unknown' | 'execution' | 'allocated';

export interface UsageFigure extends Series {
  key: UsageKey;
  measure: UsageMeasure;
  /** Demand, or what became of it. A hairline separates the two. */
  demand: boolean;
  /** What the figure reads from one interval, in the unit its axis carries. */
  read: (bucket: ActivityBucket) => number;
}

/** The two measures, as the control that chooses between them takes them. */
export const USAGE_MEASURES = [
  { value: 'jobs', label: 'Jobs', name: 'Job demand and outcomes, counted per interval' },
  { value: 'runtime', label: 'Runner time', name: 'Executing and allocated runner time, in hours' },
] as const;

/**
 * The figures, in the order their chips appear: demand first, then what
 * became of it.
 *
 * The colours are the console's fixed status colours, so a red line is a
 * failure here as it is everywhere else, and the two pairs that sit within
 * about twenty points of CIEDE2000 of one another -- allocated against
 * queued, cancelled against the greys -- are told apart by their stroke as
 * well: allocated runner time is the envelope the executing line sits
 * inside, and cancelled work is the outcome the fleet had no hand in. Both
 * read as reference quantities, which is what a dashed line already means.
 * See docs/ui-guidelines.md.
 */
export const USAGE_FIGURES: readonly UsageFigure[] = [
  {
    key: 'queued',
    measure: 'jobs',
    demand: true,
    label: 'Queued',
    hint: 'jobs that joined the queue in this interval',
    tone: 'var(--z-accent)',
    dash: '',
    read: (b) => b.queued,
  },
  {
    key: 'succeeded',
    measure: 'jobs',
    demand: false,
    label: 'Succeeded',
    hint: 'jobs that finished successfully',
    tone: 'var(--z-idle)',
    dash: '',
    read: (b) => b.succeeded,
  },
  {
    key: 'failed',
    measure: 'jobs',
    demand: false,
    label: 'Failed',
    hint: 'jobs that finished in a failure',
    tone: 'var(--z-danger)',
    dash: '',
    read: (b) => b.failed,
  },
  {
    key: 'cancelled',
    measure: 'jobs',
    demand: false,
    label: 'Cancelled / skipped',
    hint: 'jobs cancelled or skipped before they could finish',
    tone: 'var(--z-neutral)',
    dash: '6 4',
    read: (b) => b.cancelled,
  },
  {
    key: 'unknown',
    measure: 'jobs',
    demand: false,
    label: 'Unknown',
    hint: 'jobs GitHub reported no conclusion for',
    tone: 'var(--z-pending)',
    dash: '',
    read: (b) => b.unknown,
  },
  {
    key: 'execution',
    measure: 'runtime',
    demand: true,
    label: 'Executing',
    hint: 'hours a job spent running',
    tone: 'var(--z-busy)',
    dash: '',
    read: (b) => b.execution_seconds / 3600,
  },
  {
    key: 'allocated',
    measure: 'runtime',
    demand: false,
    label: 'Allocated',
    hint: 'hours a runner was held for this work, executing or not',
    tone: 'var(--z-accent)',
    dash: '6 4',
    read: (b) => b.allocated_seconds / 3600,
  },
];

/**
 * What the chart opens on. Queued against succeeded and failed is the
 * question the page is opened with -- how much work arrived, and how much of
 * it came out well -- and the two quieter outcomes are a chip away and
 * remembered once switched on.
 */
export const DEFAULT_FIGURES: readonly UsageKey[] = [
  'queued',
  'succeeded',
  'failed',
  'execution',
  'allocated',
];

/**
 * The figures a report can draw. A runner idles on behalf of a pool and never
 * on behalf of a repository or a workflow, so at those groupings allocated
 * time is not the group's to claim -- the table drops the column for the same
 * reason, and a line of somebody else's hours would be worse than no line.
 */
export function usageFigures(measure: UsageMeasure, attributable: boolean): UsageFigure[] {
  return USAGE_FIGURES.filter(
    (f) => f.measure === measure && (attributable || f.key !== 'allocated'),
  );
}

/**
 * A line for every chosen figure, over the report's own intervals.
 *
 * An interval that has not begun is a gap and never a zero: a report to the
 * end of today is a window with hours still in it, and a line that ran along
 * the floor from now until midnight would read as a fleet that had stopped.
 * The interval `now` falls inside is drawn, because it is being filled and
 * an operator watching work land wants to see it land.
 */
export function usageLines(
  buckets: readonly ActivityBucket[],
  figures: readonly UsageFigure[],
  now: number,
): SeriesLine<UsageFigure>[] {
  return figures.map((figure) =>
    seriesLine(
      figure,
      buckets.map((bucket) => {
        const at = toMillis(bucket.from) ?? 0;
        return { at, value: at > now ? null : figure.read(bucket) };
      }),
    ),
  );
}

/** Whether an interval is still being filled, so its figures are not final. */
export function inProgress(bucket: ActivityBucket, interval: Interval, now: number): boolean {
  const at = toMillis(bucket.from);
  return at !== null && at <= now && at + intervalWidth(interval) > now;
}

/**
 * The stretches in view where a pool had nowhere to put a runner.
 *
 * It is the usage chart's version of the fleet trend's starvation band and
 * the capacity map's pressure line: the condition the lines are read against,
 * and the one an operator scanning a fortnight is looking for. Neighbouring
 * intervals come back as one run, so a bad afternoon is one band rather than
 * a picket fence, and an interval with no capacity observations at all ends
 * the run rather than carrying it across the unknown -- the fleet may well
 * have had room in the hour nobody recorded.
 */
export function capacityRuns(
  buckets: readonly ActivityBucket[],
): Array<{ from: number; to: number }> {
  const out: Array<{ from: number; to: number }> = [];
  buckets.forEach((bucket, i) => {
    if (bucket.capacity_samples === 0 || bucket.capacity_reached === 0) return;
    const open = out[out.length - 1];
    if (open && open.to >= i - 1) open.to = i;
    else out.push({ from: i, to: i });
  });
  return out;
}

const HOURS = new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 });

/** Jobs are counted whole; runner time is not, and 0.25 h is a real figure. */
export function usageFormat(measure: UsageMeasure): (value: number) => string {
  return measure === 'runtime' ? (value) => HOURS.format(value) : formatNumber;
}
