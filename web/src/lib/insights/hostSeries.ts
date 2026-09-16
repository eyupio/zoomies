/**
 * The capacity map's arithmetic: every host figure as a share of the machine,
 * so seven different measurements can sit on one 0-100% axis and be read
 * against each other.
 *
 * Pure, and imported by a unit test, so nothing here touches the DOM or the
 * clock: the caller passes `now`.
 */
import type { Host, HostSample } from '../api/types';
import type { SeriesPoint } from './plot';

export type MetricKey =
  'cpu' | 'memory' | 'load' | 'slots' | 'cpu_committed' | 'memory_committed' | 'disk';

export interface Metric {
  key: MetricKey;
  /** The chip and the legend. */
  label: string;
  /** Beside a figure, where the chip's words would not fit: "CPU 72%". */
  short: string;
  /** What the figure is a share of, for the tooltip and assistive text. */
  hint: string;
  /**
   * How the line is drawn. Hosts take the colour, so a metric has to carry
   * its identity in the stroke, exactly as the usage chart's executing and
   * allocated lines do: `''` is solid.
   */
  dash: string;
  /** Measured by the agent, as opposed to promised by the scheduler. */
  measured: boolean;
}

/** In the order the chips appear: what is happening first, what is promised after. */
export const METRICS: readonly Metric[] = [
  {
    key: 'cpu',
    label: 'CPU used',
    short: 'CPU',
    hint: 'of the whole machine, measured',
    dash: '',
    measured: true,
  },
  {
    key: 'memory',
    label: 'Memory used',
    short: 'Memory',
    hint: 'of the whole machine, measured',
    dash: '8 4',
    measured: true,
  },
  {
    key: 'load',
    label: 'Load per CPU',
    short: 'Load',
    hint: 'one-minute load average against the CPU count; over 100% is a queue behind the cores',
    dash: '2 4',
    measured: true,
  },
  {
    key: 'slots',
    label: 'Runner slots',
    short: 'Slots',
    hint: 'runners on the host against the slots it is taking',
    dash: '12 4 2 4',
    measured: false,
  },
  {
    key: 'cpu_committed',
    label: 'CPU committed',
    short: 'CPU rsv',
    hint: 'what live runners have reserved, of what may be placed on',
    dash: '4 3',
    measured: false,
  },
  {
    key: 'memory_committed',
    label: 'Memory committed',
    short: 'Mem rsv',
    hint: 'what live runners have reserved, of what may be placed on',
    dash: '1 3',
    measured: false,
  },
  {
    key: 'disk',
    label: 'Disk used',
    short: 'Disk',
    hint: 'of the work directory filesystem',
    dash: '16 6',
    measured: false,
  },
];

export const DEFAULT_METRICS: readonly MetricKey[] = ['cpu', 'slots'];

function share(used: number | null | undefined, total: number | null | undefined): number | null {
  if (used === null || used === undefined || total === null || total === undefined) return null;
  if (!Number.isFinite(used) || !Number.isFinite(total) || total <= 0) return null;
  return Math.max(0, (100 * used) / total);
}

/**
 * One sample's figure for a metric, as a percentage, or null where the host
 * has not said. Only load may exceed 100: a load of twice the CPUs is a real
 * thing and the reason a throttle steps up, so it is not clipped away.
 */
export function metricValue(s: HostSample, metric: MetricKey): number | null {
  switch (metric) {
    case 'cpu':
      return s.cpu_percent === undefined ? null : Math.max(0, Math.min(100, s.cpu_percent));
    case 'memory':
      return s.memory_available_mb === undefined || !s.memory_mb
        ? null
        : share(s.memory_mb - s.memory_available_mb, s.memory_mb);
    case 'load':
      return s.load_average_1m === undefined ? null : share(s.load_average_1m, s.cpus);
    case 'slots':
      return share(s.active_runners, s.capacity);
    case 'cpu_committed':
      return s.reserved_cpus === undefined ? null : share(s.reserved_cpus, s.allocatable_cpus);
    case 'memory_committed':
      return s.reserved_memory_mb === undefined
        ? null
        : share(s.reserved_memory_mb, s.allocatable_memory_mb);
    case 'disk':
      return !s.disk_total_mb
        ? null
        : share(s.disk_total_mb - (s.disk_free_mb ?? 0), s.disk_total_mb);
  }
}

/** The figure as the operator would say it: "3 / 6 slots", "12.4 GB of 32 GB". */
export function metricText(s: HostSample, metric: MetricKey): string {
  const pct = metricValue(s, metric);
  if (pct === null) return 'Not measured';
  const gb = (mb: number) =>
    `${(mb / 1024).toLocaleString(undefined, { maximumFractionDigits: 1 })} GB`;
  switch (metric) {
    case 'slots':
      return `${s.active_runners} / ${s.capacity} slots`;
    case 'load':
      return `${(s.load_average_1m ?? 0).toLocaleString(undefined, { maximumFractionDigits: 1 })} on ${s.cpus ?? 0} CPUs`;
    case 'memory':
      return `${gb((s.memory_mb ?? 0) - (s.memory_available_mb ?? 0))} of ${gb(s.memory_mb ?? 0)}`;
    case 'cpu_committed':
      return `${(s.reserved_cpus ?? 0).toLocaleString(undefined, { maximumFractionDigits: 1 })} of ${(s.allocatable_cpus ?? 0).toLocaleString(undefined, { maximumFractionDigits: 1 })} CPUs`;
    case 'memory_committed':
      return `${gb(s.reserved_memory_mb ?? 0)} of ${gb(s.allocatable_memory_mb ?? 0)}`;
    case 'disk':
      return `${gb((s.disk_total_mb ?? 0) - (s.disk_free_mb ?? 0))} of ${gb(s.disk_total_mb ?? 0)}`;
    case 'cpu':
      return `${pct.toFixed(0)}%`;
  }
}

/**
 * The host as it is right now, in the shape of a sample, so the newest point
 * of every series is the same figure the card beside the chart shows. A
 * measurement that has gone stale is left out, as the controller's own
 * sampler leaves it out: a host that stopped reporting draws a gap.
 */
export function liveSample(host: Host, now: number): HostSample {
  const usage = host.usage_fresh ? host.usage : undefined;
  const reserved = host.reserved_known === true && host.resources_known === true;
  return {
    host_id: host.id ?? '',
    at: new Date(now).toISOString(),
    capacity: host.effective_capacity ?? host.capacity ?? 0,
    active_runners: host.active_runners ?? 0,
    cpu_percent: usage?.cpu_percent,
    load_average_1m: usage?.load_average_1m,
    memory_available_mb: usage?.memory_available_mb,
    cpus: host.cpus,
    memory_mb: host.memory_mb,
    allocatable_cpus: host.allocatable_cpus,
    allocatable_memory_mb: host.allocatable_memory_mb,
    reserved_cpus: reserved ? host.reserved_cpus : undefined,
    reserved_memory_mb: reserved ? host.reserved_memory_mb : undefined,
    disk_total_mb: host.disk_total_mb,
    disk_free_mb: host.disk_free_mb,
  };
}

/**
 * The finest slot a sample is kept at, in seconds, for a window whose points
 * fold `bucket` seconds each. The controller writes one sample a minute, so a
 * window drawn by the minute or coarser keeps one sample per minute and folds
 * from there; a window drawn finer than a minute keeps every slot the stream
 * fills, which is what lets the last ten minutes show a heartbeat at a time.
 */
export function grainOf(bucket: number): number {
  return Math.min(bucket, 60);
}

/** The slot a sample falls in: its instant, floored to the grain. */
function slotOf(at: number, grain: number): number {
  return Math.floor(at / (grain * 1000)) * grain * 1000;
}

/**
 * The slots a window covers, `bucket` seconds to a point: the last slot of
 * the last point is the current one, so the right-hand edge of every window
 * is now, and every point is a whole bucket ending on a slot boundary rather
 * than a partial one aligned to the clock.
 */
export function windowSlots(
  now: number,
  seconds: number,
  bucket: number,
): { start: number; end: number; count: number; grain: number } {
  const grain = grainOf(bucket);
  const count = Math.ceil(seconds / bucket);
  const end = slotOf(now, grain);
  const start = end - (count * bucket - grain) * 1000;
  return { start, end, count, grain };
}

/**
 * Samples keyed by host, one per slot inside the window; later inputs win.
 * `seconds` is how far back the window reaches and `grain` how fine a slot
 * is kept: a minute for the windows drawn by the minute, and finer for the
 * ones that show the last few minutes as the stream delivered them.
 */
export function mergeHostSamples(
  history: readonly HostSample[],
  live: readonly HostSample[],
  now: number,
  seconds: number,
  grain = 60,
): Map<string, HostSample[]> {
  const end = slotOf(now, grain);
  const start = end - (seconds - grain) * 1000;
  const byHost = new Map<string, Map<number, HostSample>>();
  for (const s of [...history, ...live]) {
    const at = slotOf(new Date(s.at ?? '').getTime(), grain);
    if (!Number.isFinite(at) || at < start || at > end) continue;
    let slots = byHost.get(s.host_id);
    if (!slots) byHost.set(s.host_id, (slots = new Map()));
    slots.set(at, s);
  }
  const out = new Map<string, HostSample[]>();
  for (const [id, slots] of byHost) {
    out.set(
      id,
      [...slots.entries()].sort(([a], [b]) => a - b).map(([, s]) => s),
    );
  }
  return out;
}

/**
 * One host's line for one metric: a point per bucket carrying the peak of
 * its slots, and a gap wherever nothing was observed. A spike is never
 * averaged away by a wider window, which is the same promise the fleet trend
 * makes.
 */
export function hostSeries(
  samples: readonly HostSample[],
  metric: MetricKey,
  now: number,
  seconds: number,
  bucket: number,
): SeriesPoint[] {
  const { start, end, count, grain } = windowSlots(now, seconds, bucket);
  const peaks: (number | null)[] = Array.from({ length: count }, () => null);
  for (const s of samples) {
    const at = slotOf(new Date(s.at ?? '').getTime(), grain);
    if (!Number.isFinite(at) || at < start || at > end) continue;
    const i = Math.floor((at - start) / (bucket * 1000));
    const value = metricValue(s, metric);
    if (value === null) continue;
    peaks[i] = Math.max(peaks[i] ?? value, value);
  }
  return peaks.map((value, i) => ({ at: start + i * bucket * 1000, value }));
}

/**
 * How many absent intervals in a row a line is drawn across. One missing
 * minute between two observed ones is the sampler having been a moment late
 * for the minute, or a tab that slept through it, and a line broken at every
 * such minute reads as a machine flickering in and out of existence. Two in
 * a row is a host that has stopped reporting, and that stays a gap: a flat
 * line is what a healthy, quiet machine draws too.
 */
export const BRIDGE = 1;

/**
 * The bridge for a window drawn `bucket` seconds to a point. By the minute
 * it is the one above. Finer than that, the stored samples are still a minute
 * apart and a heartbeat thirty seconds, so the line joins across a minute of
 * silence between observations; the ninety seconds after which the controller
 * counts a host lost stays a gap, as it does at every other resolution.
 */
export function bridgeFor(bucket: number): number {
  return bucket >= 60 ? BRIDGE : Math.ceil(60 / bucket);
}

/**
 * The observed points of a line, grouped into the runs one stroke joins. A
 * point inside a run that was not observed is left out of it, so the line
 * passes straight from the reading before to the reading after and no marker
 * claims a figure for that minute.
 */
export function lineRuns(
  points: readonly SeriesPoint[],
  bridge = BRIDGE,
): { i: number; value: number }[][] {
  const out: { i: number; value: number }[][] = [];
  let run: { i: number; value: number }[] = [];
  let missing = 0;
  points.forEach((p, i) => {
    if (p.value === null) {
      missing += 1;
      return;
    }
    if (run.length && missing > bridge) {
      out.push(run);
      run = [];
    }
    missing = 0;
    run.push({ i, value: p.value });
  });
  if (run.length) out.push(run);
  return out;
}

/**
 * The top of the lane above the axis, or 0 when nothing needs one. Every
 * figure but load is a share of the machine and stops at 100; load may pass
 * it, and the highest load in view sets the lane's own scale, rounded up to
 * the next 50 so the label is a round number.
 */
export function overflowCeiling(peak: number | null): number {
  if (peak === null || !Number.isFinite(peak) || peak <= 100) return 0;
  return Math.ceil(peak / 50) * 50;
}

/**
 * The windows on offer: how far back, in seconds, and how many seconds one
 * point folds. The three shortest are drawn finer than the controller's
 * minute samples, from what the stream delivers while the page is open: a
 * heartbeat is thirty seconds, so ten-second points show each one where a
 * minute-by-minute line would show the last of every two. Their history is
 * still the minute samples, so the first minutes after opening the page are
 * drawn a minute at a time and fill in as the stream arrives.
 */
export const WINDOWS = [
  {
    value: '1m',
    label: '1m',
    name: 'The last minute, in 10-second points',
    seconds: 60,
    bucket: 10,
  },
  {
    value: '5m',
    label: '5m',
    name: 'The last 5 minutes, in 10-second points',
    seconds: 300,
    bucket: 10,
  },
  {
    value: '10m',
    label: '10m',
    name: 'The last 10 minutes, in 10-second points',
    seconds: 600,
    bucket: 10,
  },
  { value: '1h', label: '1h', name: 'The last hour, minute by minute', seconds: 3600, bucket: 60 },
  {
    value: '6h',
    label: '6h',
    name: 'The last 6 hours, in 5-minute peaks',
    seconds: 21600,
    bucket: 300,
  },
  {
    value: '24h',
    label: '24h',
    name: 'The last 24 hours, in 15-minute peaks',
    seconds: 86400,
    bucket: 900,
  },
  {
    value: '7d',
    label: '7d',
    name: 'The last 7 days, in hourly peaks',
    seconds: 604800,
    bucket: 3600,
  },
] as const;
export type WindowKey = (typeof WINDOWS)[number]['value'];
export type Window = (typeof WINDOWS)[number];

/** The furthest back any window reaches, and the furthest any sub-minute one does. */
export const LONGEST_WINDOW: Window = WINDOWS[WINDOWS.length - 1]!;
export const LONGEST_FINE_WINDOW: Window = [...WINDOWS].reverse().find((w) => w.bucket < 60)!;

/** Series colours are the categorical ramp, in the order the hosts are listed. */
export function hostTone(index: number): string {
  return `var(--z-chart-${(index % 6) + 1})`;
}

/* -- what the chart draws from -------------------------------------------- */

/**
 * The share of the machine above which the scheduler treats a host as under
 * pressure: from 85% it places one runner at a time there, and a throttled
 * host does not climb back down while it stays above it. Every figure on the
 * map is a share of the machine, so the same band is drawn for all of them.
 */
export const PRESSURE = 85;

/** One host's line for one metric, as the chart receives it. */
export interface HostLine {
  id: string;
  host: Host;
  /** The host's place in the fleet's list, which is what its colour is. */
  index: number;
  metric: Metric;
  tone: string;
  points: SeriesPoint[];
  /** The newest observed point, or null where the line has none in view. */
  last: { i: number; value: number } | null;
}

/**
 * Every line the chart shows: a line per chosen metric for every host that is
 * not hidden, in the fleet's order so a host keeps its colour when its
 * neighbours are switched off. Colour follows the host, never its rank.
 */
export function hostLines(
  hosts: readonly Host[],
  hidden: readonly string[],
  byHost: ReadonlyMap<string, readonly HostSample[]>,
  metrics: readonly Metric[],
  now: number,
  seconds: number,
  bucket: number,
): HostLine[] {
  const out: HostLine[] = [];
  hosts.forEach((host, index) => {
    if (hidden.includes(host.id ?? '')) return;
    const samples = byHost.get(host.id ?? '') ?? [];
    for (const metric of metrics) {
      const points = hostSeries(samples, metric.key, now, seconds, bucket);
      let last: HostLine['last'] = null;
      for (let i = points.length - 1; i >= 0; i--) {
        const v = points[i]?.value;
        if (v !== null && v !== undefined) {
          last = { i, value: v };
          break;
        }
      }
      out.push({
        id: `${host.id}:${metric.key}`,
        host,
        index,
        metric,
        tone: hostTone(index),
        points,
        last,
      });
    }
  });
  return out;
}

/**
 * The measurement the chart leads with: the first chosen one, in the chips'
 * order. It is the one whose value is written at the end of every line, the
 * one the summary speaks about and the one a lone host's wash is drawn
 * under, so that the chart has a headline even when four things are on it.
 */
export function leadMetric(enabled: readonly MetricKey[]): Metric | null {
  return METRICS.find((m) => enabled.includes(m.key)) ?? null;
}

/** The highest value of one metric across every line, and where it was. */
export function peakOf(
  lines: readonly HostLine[],
  metric: MetricKey,
): { line: HostLine; i: number; value: number } | null {
  let best: { line: HostLine; i: number; value: number } | null = null;
  for (const line of lines) {
    if (line.metric.key !== metric) continue;
    line.points.forEach((p, i) => {
      if (p.value !== null && (best === null || p.value > best.value))
        best = { line, i, value: p.value };
    });
  }
  return best;
}

/** The drawing's edges, in pixels, for a plot `width` wide. */
export interface Frame {
  W: number;
  H: number;
  LEFT: number;
  RIGHT: number;
  TOP: number;
  BOTTOM: number;
  SPAN: number;
  /** The lane above the axis for load past the cores, or 0 without one. */
  LANE: number;
  /** Where 100% sits: the top of the drawing, or under the lane. */
  AXIS_TOP: number;
  /** A phone's shape: taller, and with fewer labels along the bottom. */
  narrow: boolean;
}

/**
 * The frame is drawn at the width the panel has, one unit to one pixel,
 * rather than a fixed picture scaled to fit: a label drawn at eleven units
 * and stretched to a wide screen is a label at nineteen pixels, which is
 * what made the old axis look like a heading. The left gutter holds the
 * percentages and the right one the value at the end of each line; both are
 * the same in every plot so the plots stacked in the per-host layout share
 * one x axis.
 */
export function plotFrame(
  width: number,
  options: { compact?: boolean; lane?: boolean; axis?: boolean } = {},
): Frame {
  const W = Math.max(280, Math.round(width));
  const narrow = W < 560;
  const H = options.compact ? (narrow ? 132 : 116) : narrow ? 300 : 248;
  const LANE = options.lane ? (options.compact ? 22 : 40) : 0;
  const LEFT = 42;
  const RIGHT = W - 46;
  const TOP = 12;
  const BOTTOM = H - (options.axis === false ? 10 : 26);
  return {
    W,
    H,
    LEFT,
    RIGHT,
    TOP,
    BOTTOM,
    SPAN: RIGHT - LEFT,
    LANE,
    AXIS_TOP: TOP + LANE,
    narrow,
  };
}

/** The x of point `i`, of `count` across the frame. */
export function scaleX(frame: Frame, count: number): (i: number) => number {
  return (i) => frame.LEFT + (i * frame.SPAN) / Math.max(1, count - 1);
}

/**
 * The y of a value. The axis is 0-100% and stays so; a value past it is
 * drawn in the lane, on the lane's own scale up to `overflow`, so a load of
 * eight times the cores is still a spike and the axis keeps its room.
 */
export function scaleY(frame: Frame, overflow: number): (value: number) => number {
  return (value) => {
    if (value <= 100 || !overflow)
      return frame.AXIS_TOP + (1 - Math.min(value, 100) / 100) * (frame.BOTTOM - frame.AXIS_TOP);
    return frame.AXIS_TOP - ((Math.min(value, overflow) - 100) / (overflow - 100)) * frame.LANE;
  };
}

/** Which point a pointer is over, from where it is across the frame. */
export function indexAtX(frame: Frame, count: number, viewX: number): number {
  const i = Math.round(((viewX - frame.LEFT) / frame.SPAN) * Math.max(1, count - 1));
  return Math.max(0, Math.min(count - 1, i));
}

/**
 * The host whose line passes nearest a pointer at point `i`, within
 * `tolerance` pixels, or null where none does. Pointing at a line is how a
 * reader asks which host it is, and on a chart of twenty lines the legend
 * alone cannot answer that.
 */
export function nearestHost(
  lines: readonly HostLine[],
  i: number,
  viewY: number,
  y: (value: number) => number,
  tolerance: number,
): string | null {
  let best: { id: string; distance: number } | null = null;
  for (const line of lines) {
    const v = line.points[i]?.value;
    if (v === null || v === undefined) continue;
    const distance = Math.abs(y(v) - viewY);
    if (distance <= tolerance && (best === null || distance < best.distance))
      best = { id: line.host.id ?? '', distance };
  }
  return best?.id ?? null;
}
