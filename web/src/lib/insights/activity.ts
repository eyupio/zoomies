/**
 * The activity matrix's arithmetic: which square an interval is, how it is
 * painted, what its label says, and how a job arriving over the stream lands
 * in it.
 *
 * Pure functions with no DOM and no clock of their own, so the calendar can be
 * tested in Node and so the Overview and the Usage page -- which draw the same
 * squares from the same usage buckets -- cannot drift in what a square means.
 */
import type { Job, UsageRow } from '../api/types';
import { formatNumber, pluralise, toMillis } from '../format';
import { outcomeOf } from '../outcomes';

export { outcomeOf };

/** One interval of the usage history, exactly as `GET /usage` reports it. */
export type ActivityBucket = NonNullable<UsageRow['history']>[number];

/** The API's two interval widths: hourly up to 48 hours, daily beyond. */
export type Interval = 'hour' | 'day';

/**
 * What the squares are coloured by. Outcomes is the default and the one the
 * Overview opens on; the others recolour the same squares by a different
 * figure, so a queue that backs up every Monday or a pool that hits its
 * ceiling every night is a shape rather than a table.
 */
export type ActivityMode = 'outcomes' | 'queue' | 'runtime' | 'capacity';

export const MODES: ReadonlyArray<{ value: ActivityMode; label: string }> = [
  { value: 'outcomes', label: 'Job outcomes' },
  { value: 'queue', label: 'Queued jobs' },
  { value: 'runtime', label: 'Runner time' },
  { value: 'capacity', label: 'Capacity reached' },
];

/** Four steps of colour above "nothing", as a contribution graph has. */
export type Level = 0 | 1 | 2 | 3 | 4;

/**
 * What kind of square this is. Colour never carries it alone: a failing square
 * has a hole in it, a waiting square is hollow, and an unobserved one is
 * dashed, so the calendar reads in greyscale and to anyone who cannot tell
 * the green from the red.
 */
export type CellKind = 'quiet' | 'healthy' | 'failing' | 'waiting' | 'active' | 'unsampled';

/** The status hues a square may use. Fixed meanings; see docs/ui-guidelines.md. */
export type CellTone = 'idle' | 'danger' | 'pending' | 'busy';

export interface CellPaint {
  kind: CellKind;
  tone: CellTone;
  level: Level;
}

export const HOUR_MS = 3_600_000;
export const DAY_MS = 24 * HOUR_MS;

/** The width of one bucket, in milliseconds. */
export function intervalWidth(interval: Interval): number {
  return interval === 'day' ? DAY_MS : HOUR_MS;
}

export function emptyBucket(from: string): ActivityBucket {
  return {
    from,
    queued: 0,
    started: 0,
    succeeded: 0,
    failed: 0,
    cancelled: 0,
    unknown: 0,
    execution_seconds: 0,
    allocated_seconds: 0,
    capacity_samples: 0,
    capacity_reached: 0,
  };
}

/** Add one bucket's figures into another, in place. */
export function addInto(target: ActivityBucket, source: ActivityBucket): void {
  target.queued += source.queued;
  target.started += source.started;
  target.succeeded += source.succeeded;
  target.failed += source.failed;
  target.cancelled += source.cancelled;
  target.unknown += source.unknown;
  target.execution_seconds += source.execution_seconds;
  target.allocated_seconds += source.allocated_seconds;
  target.capacity_samples += source.capacity_samples;
  target.capacity_reached += source.capacity_reached;
}

/** How many jobs finished in a bucket, however they finished. */
export function completedIn(b: ActivityBucket): number {
  return b.succeeded + b.failed + b.cancelled + b.unknown;
}

/**
 * Every row's history summed into one series, oldest first. The report is
 * grouped -- by pool, by repository -- and the matrix is about the fleet, so
 * the groups are added back together here.
 */
export function mergeHistories(rows: readonly UsageRow[]): ActivityBucket[] {
  const map = new Map<number, ActivityBucket>();
  for (const row of rows) {
    for (const b of row.history ?? []) {
      const at = toMillis(b.from);
      if (at === null) continue;
      const existing = map.get(at);
      if (existing) addInto(existing, b);
      else map.set(at, { ...b });
    }
  }
  return [...map.entries()].sort((a, b) => a[0] - b[0]).map(([, b]) => b);
}

/**
 * A complete series for a window: one bucket per interval from `from`, with
 * whatever the API reported laid over it.
 *
 * The API leaves out a group with nothing to say and a window with no jobs at
 * all comes back with no history whatsoever, so the empty squares of a quiet
 * fortnight have to be made here rather than read.
 */
export function fillWindow(
  buckets: readonly ActivityBucket[],
  from: Date,
  count: number,
  interval: Interval,
): ActivityBucket[] {
  const width = intervalWidth(interval);
  const start = from.getTime();
  const out: ActivityBucket[] = [];
  for (let i = 0; i < count; i++) out.push(emptyBucket(new Date(start + i * width).toISOString()));
  for (const b of buckets) {
    const at = toMillis(b.from);
    if (at === null) continue;
    const i = Math.floor((at - start) / width);
    const target = out[i];
    if (target) addInto(target, b);
  }
  return out;
}

/** Midnight at the start of `at`'s local day. */
export function startOfLocalDay(at: Date): Date {
  return new Date(at.getFullYear(), at.getMonth(), at.getDate());
}

/** The same local time, `days` calendar days later. Survives daylight saving. */
export function addDays(at: Date, days: number): Date {
  return new Date(at.getFullYear(), at.getMonth(), at.getDate() + days, at.getHours());
}

/** The calendar date, `YYYY-MM-DD`, in local time: what the Jobs and Usage pages filter by. */
export function localDate(at: Date): string {
  const two = (n: number) => String(n).padStart(2, '0');
  return `${at.getFullYear()}-${two(at.getMonth() + 1)}-${two(at.getDate())}`;
}

/** Monday is 0. A calendar that starts on Sunday is a calendar for shops. */
export function weekday(at: Date): number {
  return (at.getDay() + 6) % 7;
}

export interface CalendarWindow {
  /** Midnight at the start of the first Monday, local time. */
  from: Date;
  /** Exactly `days` elapsed days after `from`, so every bucket has a day. */
  to: Date;
  days: number;
}

/**
 * The window a calendar of `weeks` columns ending today covers: from the
 * Monday `weeks - 1` weeks ago, through the end of today.
 *
 * `to` is elapsed rather than calendar time on purpose. The API cuts elapsed
 * 24-hour buckets from `from`, so asking for a `to` that is a whole number of
 * them is what keeps bucket `i` and calendar day `i` the same thing across a
 * daylight-saving change -- an hour adrift within the day, never a day out.
 */
export function calendarWindow(weeks: number, now: Date = new Date()): CalendarWindow {
  const today = startOfLocalDay(now);
  const monday = addDays(today, -weekday(today));
  const from = addDays(monday, -7 * (weeks - 1));
  const tomorrow = addDays(today, 1);
  const days = Math.round((tomorrow.getTime() - from.getTime()) / DAY_MS);
  return { from, to: new Date(from.getTime() + days * DAY_MS), days };
}

export interface RangeWindow {
  /** Midnight at the start of the first day, local time. */
  from: Date;
  /** `count` whole buckets after `from`, elapsed rather than calendar time. */
  to: Date;
  /** How many buckets the window is. */
  count: number;
  interval: Interval;
}

/**
 * The window of the last `days` days, today included, cut into buckets of
 * the given width. A day of hours is a row of twenty-four squares; a week of
 * them is the punch card that shows when the fleet is busy.
 */
export function rangeWindow(days: number, interval: Interval, now: Date = new Date()): RangeWindow {
  const from = addDays(startOfLocalDay(now), -(days - 1));
  const count = interval === 'day' ? days : days * 24;
  return { from, to: new Date(from.getTime() + count * intervalWidth(interval)), count, interval };
}

export interface CalendarCell {
  /** Index into the series this cell was cut from. */
  index: number;
  /** The local calendar day. */
  date: Date;
  bucket: ActivityBucket;
}

export interface CalendarColumn {
  /** The month's short name, on the column where a month begins. */
  label: string | null;
  /** Seven slots, Monday first; null where the window has no such day. */
  cells: Array<CalendarCell | null>;
}

const MONTH = new Intl.DateTimeFormat(undefined, { month: 'short' });

/**
 * Daily buckets laid out as a contribution graph: a column per week, a row
 * per weekday. `first` is the local day of bucket 0; each later bucket is the
 * next calendar day, which is what the window above guarantees.
 */
export function calendar(buckets: readonly ActivityBucket[], first: Date): CalendarColumn[] {
  const columns: CalendarColumn[] = [];
  const offset = weekday(first);
  buckets.forEach((bucket, index) => {
    const column = Math.floor((index + offset) / 7);
    const row = (index + offset) % 7;
    let col = columns[column];
    if (!col) {
      col = { label: null, cells: Array.from({ length: 7 }, () => null) };
      columns[column] = col;
    }
    col.cells[row] = { index, date: addDays(first, index), bucket };
  });
  monthLabels(columns).forEach((label, c) => {
    columns[c]!.label = label;
  });
  return columns;
}

/**
 * A month's name above the week it begins in, for whichever columns are on
 * screen -- a calendar cut to the width of a phone is labelled for the weeks
 * it shows, not the weeks it was cut from. Two adjacent labels would overlap
 * at this width, so the earlier gives way, which is the first column when the
 * window happens to start in the last days of a month.
 */
export function monthLabels(columns: readonly CalendarColumn[]): Array<string | null> {
  const labels: Array<string | null> = columns.map(() => null);
  let previous = -1;
  columns.forEach((col, c) => {
    const start = col.cells.find((cell) => cell !== null);
    if (!start) return;
    const month = start.date.getMonth() + 12 * start.date.getFullYear();
    if (month !== previous) labels[c] = MONTH.format(start.date);
    previous = month;
  });
  for (let c = 0; c + 1 < labels.length; c++) {
    if (labels[c] && labels[c + 1]) labels[c] = null;
  }
  return labels;
}

export interface HourRow {
  /** Midnight of the local day this row is. */
  date: Date;
  /** Twenty-four slots; null where the window has no such hour. */
  cells: Array<CalendarCell | null>;
}

/**
 * Hourly buckets laid out with a row per local day and a column per hour.
 * Each bucket is placed by the local time it starts at, so a daylight-saving
 * change puts an hour in the slot it was actually lived in.
 */
export function hourGrid(buckets: readonly ActivityBucket[]): HourRow[] {
  const rows = new Map<number, HourRow>();
  buckets.forEach((bucket, index) => {
    const at = toMillis(bucket.from);
    if (at === null) return;
    const date = new Date(at);
    const day = startOfLocalDay(date);
    let row = rows.get(day.getTime());
    if (!row) {
      row = { date: day, cells: Array.from({ length: 24 }, () => null) };
      rows.set(day.getTime(), row);
    }
    const hour = date.getHours();
    const existing = row.cells[hour];
    // The hour a clock change repeats is two buckets in one slot.
    if (existing) addInto(existing.bucket, bucket);
    else row.cells[hour] = { index, date, bucket: { ...bucket } };
  });
  return [...rows.values()].sort((a, b) => a.date.getTime() - b.date.getTime());
}

/**
 * The darkness of a square: nothing, then quarters of the busiest square in
 * the window, as a contribution graph does it. The busiest square is always
 * the darkest, so a fleet that runs ten jobs a day and one that runs a
 * thousand both get a calendar with a full range of colour.
 */
export function level(value: number, max: number): Level {
  if (value <= 0 || max <= 0) return 0;
  return Math.min(4, Math.max(1, Math.ceil((4 * value) / max))) as Level;
}

/**
 * The share of a square's completions that failed, banded. Fixed bands rather
 * than quarters of the worst square: "half of everything failed" should look
 * the same on a good week as on a bad one.
 */
export function failureLevel(failed: number, completed: number): Level {
  if (failed <= 0) return 0;
  const share = completed > 0 ? failed / completed : 1;
  if (share < 0.1) return 1;
  if (share < 0.25) return 2;
  if (share < 0.5) return 3;
  return 4;
}

/** The share of observed pool-minutes that were blocked on capacity, banded the same way. */
export function capacityLevel(reached: number, samples: number): Level {
  if (samples <= 0 || reached <= 0) return 0;
  const share = reached / samples;
  if (share < 0.05) return 1;
  if (share < 0.25) return 2;
  if (share < 0.5) return 3;
  return 4;
}

/** The busiest square in each figure, which is what the quarters are cut from. */
export interface Scale {
  completed: number;
  queued: number;
  execution: number;
}

export function scaleOf(buckets: readonly ActivityBucket[]): Scale {
  let completed = 0;
  let queued = 0;
  let execution = 0;
  for (const b of buckets) {
    completed = Math.max(completed, completedIn(b));
    queued = Math.max(queued, b.queued);
    execution = Math.max(execution, b.execution_seconds);
  }
  return { completed, queued, execution };
}

/** How one square is painted under a mode. */
export function paint(b: ActivityBucket, mode: ActivityMode, scale: Scale): CellPaint {
  switch (mode) {
    case 'queue': {
      const l = level(b.queued, scale.queued);
      return { kind: l ? 'active' : 'quiet', tone: 'pending', level: l };
    }
    case 'runtime': {
      const l = level(b.execution_seconds, scale.execution);
      return { kind: l ? 'active' : 'quiet', tone: 'busy', level: l };
    }
    case 'capacity': {
      if (b.capacity_samples === 0) return { kind: 'unsampled', tone: 'pending', level: 0 };
      const l = capacityLevel(b.capacity_reached, b.capacity_samples);
      return { kind: l ? 'active' : 'quiet', tone: 'pending', level: l };
    }
    default: {
      const completed = completedIn(b);
      if (b.failed > 0) {
        return { kind: 'failing', tone: 'danger', level: failureLevel(b.failed, completed) };
      }
      if (completed > 0) {
        return { kind: 'healthy', tone: 'idle', level: level(completed, scale.completed) };
      }
      const waiting = Math.max(b.queued, b.started);
      if (waiting > 0) {
        return { kind: 'waiting', tone: 'pending', level: level(waiting, scale.queued) };
      }
      return { kind: 'quiet', tone: 'idle', level: 0 };
    }
  }
}

/** Whether any square in the series carries capacity telemetry at all. */
export function hasCapacity(buckets: readonly ActivityBucket[]): boolean {
  return buckets.some((b) => b.capacity_samples > 0);
}

/* -- words ---------------------------------------------------------------- */

const DAY_LONG = new Intl.DateTimeFormat(undefined, {
  weekday: 'long',
  day: 'numeric',
  month: 'long',
  year: 'numeric',
});
const DAY_SHORT = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short' });
const CLOCK = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' });

/** "Tuesday 9 September 2026", or "9 Sep, 14:00 to 15:00". */
export function intervalName(at: Date, interval: Interval): string {
  if (interval === 'day') return DAY_LONG.format(at);
  const end = new Date(at.getTime() + HOUR_MS);
  return `${DAY_SHORT.format(at)}, ${CLOCK.format(at)} to ${CLOCK.format(end)}`;
}

/** Seconds as the hours an invoice is written in: "6.4 h". */
export function hours(seconds: number): string {
  return `${(seconds / 3600).toFixed(seconds >= 36_000 ? 0 : 1)} h`;
}

/** The one line a square is summed up in: "38 jobs finished, 2 failed". */
export function headline(b: ActivityBucket): string {
  const completed = completedIn(b);
  if (completed > 0) {
    const finished = `${pluralise(completed, 'job')} finished`;
    return b.failed > 0 ? `${finished}, ${formatNumber(b.failed)} failed` : finished;
  }
  if (b.queued > 0 || b.started > 0) {
    return `${pluralise(Math.max(b.queued, b.started), 'job')} queued, none finished`;
  }
  return 'No jobs';
}

/**
 * Everything a square knows, as one sentence. It is the square's accessible
 * name and the text a screen reader gets, so nothing may live only in the
 * tooltip.
 */
export function describe(b: ActivityBucket, at: Date, interval: Interval): string {
  const parts = [
    `${headline(b)}: ${b.succeeded} succeeded, ${b.failed} failed, ${b.cancelled} cancelled or skipped, ${b.unknown} unknown`,
    `${b.queued} queued, ${b.started} started`,
  ];
  if (b.execution_seconds > 0) parts.push(`${hours(b.execution_seconds)} executing`);
  if (b.capacity_samples > 0) {
    parts.push(
      `${formatNumber(b.capacity_reached)} of ${formatNumber(b.capacity_samples)} observed pool-minutes at capacity`,
    );
  }
  return `${intervalName(at, interval)}. ${parts.join('; ')}.`;
}

/** The window's totals, for the panel's header. */
export interface Summary {
  completed: number;
  succeeded: number;
  failed: number;
  cancelled: number;
  unknown: number;
  queued: number;
  execution_seconds: number;
  /** Intervals with any activity at all. */
  active: number;
}

export function summarise(buckets: readonly ActivityBucket[]): Summary {
  const s: Summary = {
    completed: 0,
    succeeded: 0,
    failed: 0,
    cancelled: 0,
    unknown: 0,
    queued: 0,
    execution_seconds: 0,
    active: 0,
  };
  for (const b of buckets) {
    s.succeeded += b.succeeded;
    s.failed += b.failed;
    s.cancelled += b.cancelled;
    s.unknown += b.unknown;
    s.queued += b.queued;
    s.execution_seconds += b.execution_seconds;
    if (completedIn(b) > 0 || b.queued > 0 || b.started > 0) s.active += 1;
  }
  s.completed = s.succeeded + s.failed + s.cancelled + s.unknown;
  return s;
}

/* -- the stream ------------------------------------------------------------- */

/**
 * The jobs already counted since the last fetch, per count. A `job.updated`
 * frame arrives once per change of state and again whenever GitHub adds a
 * detail to a finished job, so without this a job that failed at step three
 * would be counted as failed twice.
 */
export interface Seen {
  queued: Set<string>;
  started: Set<string>;
  completed: Set<string>;
}

export function newSeen(): Seen {
  return { queued: new Set(), started: new Set(), completed: new Set() };
}

/** The bucket a moment falls in, or -1 when it is outside the series. */
export function bucketIndex(
  series: readonly ActivityBucket[],
  at: number,
  interval: Interval,
): number {
  const width = intervalWidth(interval);
  let lo = 0;
  let hi = series.length - 1;
  while (lo <= hi) {
    const mid = (lo + hi) >> 1;
    const start = toMillis(series[mid]?.from) ?? 0;
    if (at < start) hi = mid - 1;
    else if (at >= start + width) lo = mid + 1;
    else return mid;
  }
  return -1;
}

/**
 * Fold one job frame into the series, returning a new series when a square
 * changed and null when nothing did.
 *
 * Only moments after `since` -- when the series was fetched -- are counted:
 * anything earlier is already in it. Counts move; runner time does not, since
 * a frame cannot say how much of a job's running fell inside which hour, and
 * a guess there would be a claim on a page that is read by finance. The next
 * fetch carries the exact figure.
 */
export function foldJob(
  series: readonly ActivityBucket[],
  interval: Interval,
  job: Job,
  since: number,
  seen: Seen,
): ActivityBucket[] | null {
  if (!job.id) return null;
  const id = job.id;
  const touched = new Map<number, ActivityBucket>();
  const bucket = (at: string | null | undefined): ActivityBucket | null => {
    const ms = toMillis(at);
    if (ms === null || ms < since) return null;
    const i = bucketIndex(series, ms, interval);
    if (i < 0) return null;
    let copy = touched.get(i);
    if (!copy) {
      copy = { ...series[i]! };
      touched.set(i, copy);
    }
    return copy;
  };
  if (!seen.queued.has(id)) {
    const b = bucket(job.queued_at);
    if (b) {
      b.queued += 1;
      seen.queued.add(id);
    }
  }
  if (!seen.started.has(id) && (job.state === 'in_progress' || job.state === 'completed')) {
    const b = bucket(job.started_at);
    if (b) {
      b.started += 1;
      seen.started.add(id);
    }
  }
  if (!seen.completed.has(id) && job.state === 'completed') {
    const b = bucket(job.completed_at);
    if (b) {
      b[outcomeOf(job)] += 1;
      seen.completed.add(id);
    }
  }
  if (touched.size === 0) return null;
  return series.map((b, i) => touched.get(i) ?? b);
}
