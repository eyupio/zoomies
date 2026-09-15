import type { FleetSample, Host, Pool, Stats } from '../api/types';

export function finite(value: number | undefined): number | null {
  return value !== undefined && Number.isFinite(value) ? Math.max(0, value) : null;
}
export function percent(used: number | null, total: number | null): number | null {
  return used !== null && total !== null && total > 0 ? (100 * used) / total : null;
}

/** Read controller aggregates, never the paginated runner cache. */
export function poolSignals(pools: readonly Pool[], stats: Stats | null) {
  return pools.map((pool) => {
    const latest = stats?.pools?.find((p) => p.pool_id === pool.id);
    const queued = finite(latest?.queued ?? pool.queued_jobs);
    const live = finite(latest?.live ?? pool.counts?.live);
    const max = finite(latest?.max ?? pool.max_runners);
    return {
      pool,
      queued,
      live,
      max,
      busy: finite(latest?.busy ?? pool.counts?.busy),
      idle: finite(latest?.idle ?? pool.counts?.idle),
      atCeiling:
        pool.enabled === true &&
        queued !== null &&
        queued > 0 &&
        live !== null &&
        max !== null &&
        max > 0 &&
        live >= max,
      headroom: live !== null && max !== null ? Math.max(0, max - live) : null,
    };
  });
}

/** Slot headroom is not a promise that a particular runner fits. */
export function hostSignals(host: Host) {
  const capacity = finite(host.capacity);
  const used = finite(host.active_runners);
  const free =
    finite(host.free) ?? (capacity !== null && used !== null ? Math.max(0, capacity - used) : null);
  const eligible = host.healthy === true && host.cordoned !== true && host.incompatible !== true;
  const state = host.incompatible
    ? 'Incompatible'
    : host.healthy === false
      ? 'Offline'
      : host.cordoned
        ? 'Cordoned'
        : host.healthy !== true
          ? 'Unknown'
          : free === 0
            ? 'No free slots'
            : 'Available slots';
  return {
    capacity,
    used,
    free,
    eligible,
    state,
    cpu:
      host.resources_known === true && host.reserved_known === true
        ? percent(finite(host.reserved_cpus), finite(host.allocatable_cpus))
        : null,
    memory:
      host.resources_known === true && host.reserved_known === true
        ? percent(finite(host.reserved_memory_mb), finite(host.allocatable_memory_mb))
        : null,
    diskFree:
      host.resources_known === true && (host.disk_total_mb ?? 0) > 0
        ? percent(finite(host.disk_free_mb), finite(host.disk_total_mb))
        : null,
  };
}

export interface SignalPoint {
  at: number;
  value: number | null;
}
/** Fill missing minutes with null; never draw an outage as zero demand. */
export function minuteSeries(
  points: readonly SignalPoint[],
  now: number,
  minutes = 60,
): SignalPoint[] {
  const end = Math.floor(now / 60_000) * 60_000;
  const byMinute: Record<number, number | null> = {};
  for (const p of points)
    if (Number.isFinite(p.at)) byMinute[Math.floor(p.at / 60_000) * 60_000] = p.value;
  return Array.from({ length: minutes }, (_, i) => {
    const at = end - (minutes - 1 - i) * 60_000;
    return { at, value: byMinute[at] ?? null };
  });
}

/** Keep one sample per minute. Later inputs win, so streamed data beats a fetch. */
export function mergeSamples(
  history: readonly FleetSample[],
  live: readonly FleetSample[],
  now: number,
  minutes = 60,
): FleetSample[] {
  const byMinute: Record<number, FleetSample> = {};
  const end = Math.floor(now / 60_000) * 60_000;
  for (const point of [...history, ...live]) {
    const at = Math.floor(new Date(point.at ?? '').getTime() / 60_000) * 60_000;
    if (Number.isFinite(at) && at >= end - (minutes - 1) * 60_000 && at <= end)
      byMinute[at] = point;
  }
  return Object.entries(byMinute)
    .sort(([a], [b]) => Number(a) - Number(b))
    .map(([, point]) => point);
}

/**
 * A minute series folded into wider intervals, each carrying the peak of its
 * minutes. Peaks rather than means because a queue that hit twelve for two
 * minutes is what an operator looking at a day wants to see, and a mean of
 * fifteen minutes would round it away. An interval with no observed minute
 * stays a gap.
 */
export function foldMinutes(points: readonly SignalPoint[], step: number): SignalPoint[] {
  if (step <= 1) return [...points];
  const out: SignalPoint[] = [];
  for (let i = 0; i < points.length; i += step) {
    const slice = points.slice(i, i + step);
    const known = slice.map((p) => p.value).filter((v): v is number => v !== null);
    out.push({
      at: slice[0]?.at ?? 0,
      value: known.length ? Math.max(...known) : null,
    });
  }
  return out;
}

/* -- what the fleet trend draws ------------------------------------------- */

export type SignalKey = 'queue' | 'running' | 'idle' | 'busy' | 'live';

export interface Signal {
  key: SignalKey;
  /** The chip and the legend. */
  label: string;
  /** Beside a figure, where the chip's words would not fit: "Queued 12". */
  short: string;
  /** What the figure counts, for the tooltip and assistive text. */
  hint: string;
  /**
   * The status colour the fleet already uses for that state everywhere else:
   * an operator who has learnt that amber is pending and teal is busy reads
   * this chart without a legend.
   */
  tone: string;
  /**
   * How the line is drawn. Colour says which state, the stroke says whether
   * the figure counts jobs or runners, so the two busy figures -- the jobs
   * running and the runners running them -- are one colour and can be read
   * against each other. `''` is solid.
   */
  dash: string;
  /** Jobs come from GitHub; runners are ours. It is what the stroke says. */
  jobs: boolean;
}

/** In the order the chips appear: the demand first, then what is meeting it. */
export const SIGNALS: readonly Signal[] = [
  {
    key: 'queue',
    label: 'Queued jobs',
    short: 'Queued',
    hint: 'jobs waiting for a runner',
    tone: 'var(--z-pending)',
    dash: '',
    jobs: true,
  },
  {
    key: 'running',
    label: 'Running jobs',
    short: 'Running',
    hint: 'jobs a runner has picked up',
    tone: 'var(--z-busy)',
    dash: '8 4',
    jobs: true,
  },
  {
    key: 'idle',
    label: 'Idle runners',
    short: 'Idle',
    hint: 'runners registered and waiting for work',
    tone: 'var(--z-idle)',
    dash: '',
    jobs: false,
  },
  {
    key: 'busy',
    label: 'Busy runners',
    short: 'Busy',
    hint: 'runners executing a job',
    tone: 'var(--z-busy)',
    dash: '',
    jobs: false,
  },
  {
    key: 'live',
    label: 'Live runners',
    short: 'Live',
    hint: 'every runner short of removed: provisioning, registering, idle, busy, draining',
    tone: 'var(--z-neutral)',
    dash: '2 4',
    jobs: false,
  },
];

// Demand and the two halves of the answer to it. Queued against idle is the
// question the page is opened with -- is anything waiting, and was there
// anything free -- and running is what says whether the fleet is working or
// merely awake.
export const DEFAULT_SIGNALS: readonly SignalKey[] = ['queue', 'running', 'idle'];

/**
 * One sample's figure for a signal. `others` chooses between the jobs the
 * whole organisation reported and the ones this fleet is responsible for;
 * runner counts are always ours, because nobody else's runners are visible
 * from here.
 */
export function signalValue(sample: FleetSample, key: SignalKey, others: boolean): number | null {
  switch (key) {
    case 'queue':
      return (others ? sample.queued_jobs : sample.fleet_queued_jobs) ?? null;
    case 'running':
      return (others ? sample.running_jobs : sample.fleet_running_jobs) ?? null;
    case 'idle':
      return sample.idle_runners ?? null;
    case 'busy':
      return sample.busy_runners ?? null;
    case 'live':
      return sample.total_runners ?? null;
  }
}

/** The signal the chart leads with: the first chosen one, in the chips' order. */
export function leadSignal(enabled: readonly SignalKey[]): Signal | null {
  return SIGNALS.find((s) => enabled.includes(s.key)) ?? null;
}

/** One signal's line over the window, as the chart receives it. */
export interface SignalLine {
  signal: Signal;
  points: SignalPoint[];
  /** The newest observed point, for the label at the end and the pulse. */
  last: { i: number; value: number } | null;
}

/** A line for every chosen signal, in the chips' order. */
export function signalLines(
  samples: readonly FleetSample[],
  enabled: readonly SignalKey[],
  others: boolean,
  now: number,
  minutes: number,
  step: number,
): SignalLine[] {
  return SIGNALS.filter((signal) => enabled.includes(signal.key)).map((signal) => {
    const points = foldMinutes(
      minuteSeries(
        samples.map((s) => ({
          at: new Date(s.at ?? '').getTime(),
          value: signalValue(s, signal.key, others),
        })),
        now,
        minutes,
      ),
      step,
    );
    let last: { i: number; value: number } | null = null;
    points.forEach((p, i) => {
      if (p.value !== null) last = { i, value: p.value };
    });
    return { signal, points, last };
  });
}

/**
 * The observed points of a line, grouped into the runs one stroke joins. An
 * interval nobody sampled is left out, so the line passes from the reading
 * before to the reading after and no marker claims a figure for it: a gap is
 * a gap, and never a zero, because an outage and an empty queue are opposite
 * news.
 */
export function signalRuns(points: readonly SignalPoint[]): Array<{ i: number; value: number }[]> {
  const out: Array<{ i: number; value: number }[]> = [];
  let run: { i: number; value: number }[] = [];
  points.forEach((p, i) => {
    if (p.value === null) {
      if (run.length) out.push(run);
      run = [];
    } else run.push({ i, value: p.value });
  });
  if (run.length) out.push(run);
  return out;
}

/** The highest figure across every line, and which line and point it was at. */
export function signalPeak(
  lines: readonly SignalLine[],
): { line: SignalLine; i: number; value: number } | null {
  let best: { line: SignalLine; i: number; value: number } | null = null;
  for (const line of lines)
    line.points.forEach((p, i) => {
      if (p.value !== null && (best === null || p.value > best.value))
        best = { line, i, value: p.value };
    });
  return best;
}

/** The drawing's edges, in pixels, for a trend `width` wide. */
export interface TrendFrame {
  W: number;
  H: number;
  LEFT: number;
  RIGHT: number;
  TOP: number;
  BOTTOM: number;
  SPAN: number;
  /** A phone's shape: taller, and with fewer labels along the bottom. */
  narrow: boolean;
}

/**
 * The frame is drawn at the width the panel has, one unit to one pixel,
 * rather than a fixed picture stretched to fit: the old chart drew 760 units
 * and let the browser scale them, so its eleven-unit axis text arrived on a
 * wide screen at nineteen pixels and read as a heading. The left gutter is
 * sized for the widest figure the axis will print, and the right one holds
 * the newest value at the end of each line.
 */
export function trendFrame(
  width: number,
  options: { digits?: number; labels?: boolean } = {},
): TrendFrame {
  const W = Math.max(280, Math.round(width));
  const narrow = W < 560;
  const H = narrow ? 236 : 196;
  const LEFT = 16 + 7 * Math.max(1, options.digits ?? 3);
  const RIGHT = W - (options.labels === false ? 10 : 46);
  const TOP = 14;
  return { W, H, LEFT, RIGHT, TOP, BOTTOM: H - 26, SPAN: RIGHT - LEFT, narrow };
}

/** The x of point `i`, of `count` across the frame. */
export function trendX(frame: TrendFrame, count: number): (i: number) => number {
  return (i) => frame.LEFT + (i * frame.SPAN) / Math.max(1, count - 1);
}

/** The y of a figure on an axis running from zero to `ceiling`. */
export function trendY(frame: TrendFrame, ceiling: number): (value: number) => number {
  return (value) =>
    frame.BOTTOM - (Math.min(value, ceiling) / Math.max(1, ceiling)) * (frame.BOTTOM - frame.TOP);
}

/** Which point a pointer is over, from where it is across the frame. */
export function trendIndexAtX(frame: TrendFrame, count: number, viewX: number): number {
  const i = Math.round(((viewX - frame.LEFT) / frame.SPAN) * Math.max(1, count - 1));
  return Math.max(0, Math.min(count - 1, i));
}

/**
 * The signal whose line passes nearest a pointer at point `i`, within
 * `tolerance` pixels, or null where none does. Pointing at a line is how a
 * reader asks what it is, and with five of them on one chart the legend
 * alone cannot answer that.
 */
export function nearestSignal(
  lines: readonly SignalLine[],
  i: number,
  viewY: number,
  y: (value: number) => number,
  tolerance: number,
): SignalKey | null {
  let best: { key: SignalKey; distance: number } | null = null;
  for (const line of lines) {
    const v = line.points[i]?.value;
    if (v === null || v === undefined) continue;
    const distance = Math.abs(y(v) - viewY);
    if (distance <= tolerance && (best === null || distance < best.distance))
      best = { key: line.signal.key, distance };
  }
  return best?.key ?? null;
}

/**
 * The stretches in view where jobs were queued with no idle runner to take
 * them. It is this chart's pressure band: the capacity map can draw a line
 * at 85% because every figure on it is a share of a machine, and a count of
 * jobs has no such threshold -- but "something was waiting and nothing was
 * free" is the moment an operator is looking for, and it can be found
 * without knowing what a healthy queue depth is for this fleet.
 *
 * It is judged a minute at a time and then folded, which is why it takes the
 * minute series and the width of a point rather than the drawn lines: a
 * fifteen-minute point carries each figure's peak, and the most idle runners
 * there were at any moment in a quarter of an hour is not an answer to
 * whether anything was free when the queue was deep. Folded the other way
 * round -- was there a minute in this interval when work waited and nothing
 * was free -- the band survives zooming out, which is the zoom an operator
 * asking "did we run short overnight" is on.
 *
 * Contiguous intervals come back as one run, so the chart shades a spell of
 * starvation as one band rather than a picket fence, and an interval nobody
 * sampled ends the run rather than extending it across the unknown.
 */
export function starvedRuns(
  queue: SignalLine | null,
  idle: SignalLine | null,
  step = 1,
): Array<{ from: number; to: number }> {
  if (!queue || !idle) return [];
  const out: Array<{ from: number; to: number }> = [];
  queue.points.forEach((p, minute) => {
    const free = idle.points[minute]?.value;
    if (p.value === null || p.value <= 0 || free === null || free === undefined || free !== 0)
      return;
    const i = Math.floor(minute / Math.max(1, step));
    const open = out[out.length - 1];
    if (open && open.to >= i - 1) open.to = i;
    else out.push({ from: i, to: i });
  });
  return out;
}
