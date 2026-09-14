/**
 * The capacity map's arithmetic: every host figure as a share of the machine,
 * so seven different measurements can sit on one 0-100% axis and be read
 * against each other.
 *
 * Pure, and imported by a unit test, so nothing here touches the DOM or the
 * clock: the caller passes `now`.
 */
import type { Host, HostSample } from '../api/types';
import { foldMinutes, minuteSeries, type SignalPoint } from './signals';

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

/** Samples keyed by host, one per minute inside the window; later inputs win. */
export function mergeHostSamples(
  history: readonly HostSample[],
  live: readonly HostSample[],
  now: number,
  minutes: number,
): Map<string, HostSample[]> {
  const end = Math.floor(now / 60_000) * 60_000;
  const start = end - (minutes - 1) * 60_000;
  const byHost = new Map<string, Map<number, HostSample>>();
  for (const s of [...history, ...live]) {
    const at = Math.floor(new Date(s.at ?? '').getTime() / 60_000) * 60_000;
    if (!Number.isFinite(at) || at < start || at > end) continue;
    let minutesOf = byHost.get(s.host_id);
    if (!minutesOf) byHost.set(s.host_id, (minutesOf = new Map()));
    minutesOf.set(at, s);
  }
  const out = new Map<string, HostSample[]>();
  for (const [id, minutesOf] of byHost) {
    out.set(
      id,
      [...minutesOf.entries()].sort(([a], [b]) => a - b).map(([, s]) => s),
    );
  }
  return out;
}

/**
 * One host's line for one metric: a point per interval carrying the peak of
 * its minutes, and a gap wherever nothing was observed.
 */
export function hostSeries(
  samples: readonly HostSample[],
  metric: MetricKey,
  now: number,
  minutes: number,
  step: number,
): SignalPoint[] {
  const points = samples.map((s) => ({
    at: new Date(s.at ?? '').getTime(),
    value: metricValue(s, metric),
  }));
  return foldMinutes(minuteSeries(points, now, minutes), step);
}

/** The windows on offer: how far back, and how many minutes one point folds. */
export const WINDOWS = [
  { value: '1h', label: '1h', name: 'The last hour, minute by minute', minutes: 60, step: 1 },
  { value: '6h', label: '6h', name: 'The last 6 hours, in 5-minute peaks', minutes: 360, step: 5 },
  {
    value: '24h',
    label: '24h',
    name: 'The last 24 hours, in 15-minute peaks',
    minutes: 1440,
    step: 15,
  },
  { value: '7d', label: '7d', name: 'The last 7 days, in hourly peaks', minutes: 10080, step: 60 },
] as const;
export type WindowKey = (typeof WINDOWS)[number]['value'];

/**
 * Where to put the labels along the bottom: on round times -- the hour, the
 * quarter hour, midnight -- rather than at four evenly spaced instants that
 * happen to be 11:19 and 19:19. The step is the smallest of the candidates
 * that fits about five labels in the window, and the ticks are the multiples
 * of it in local time, so a day's chart is labelled at 00:00, 06:00, 12:00.
 */
export function timeTicks(start: number, end: number, target = 5): number[] {
  const MINUTE = 60_000;
  const steps = [5, 10, 15, 30, 60, 120, 180, 360, 720, 1440].map((m) => m * MINUTE);
  const span = end - start;
  const step = steps.find((s) => span / s <= target) ?? steps[steps.length - 1]!;
  // Multiples of the step in local time, which is what the labels say.
  const offset = new Date(start).getTimezoneOffset() * MINUTE;
  const first = Math.ceil((start - offset) / step) * step + offset;
  const out: number[] = [];
  for (let at = first; at <= end; at += step) out.push(at);
  return out;
}

/** Series colours are the categorical ramp, in the order the hosts are listed. */
export function hostTone(index: number): string {
  return `var(--z-chart-${(index % 6) + 1})`;
}
