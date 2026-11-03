/**
 * The three figures a closed host card keeps in view: how much of the machine's
 * CPU, memory and disk is in use.
 *
 * They are the capacity map's own arithmetic (hostSeries.ts), not a second
 * opinion, so the newest point on that chart and this card are one figure by
 * construction. Amber is drawn where the product itself acts on a figure, by
 * the rule it uses for that resource: the scheduler's pressure line for CPU
 * (without the CPU it lent to runners), the host reserve it holds new starts at
 * for memory, and the doctor's low-disk rule for the disk.
 *
 * Route-only, like health-card.ts: the card is the only caller.
 */
import type { Host, HostSample } from '$lib/api/types';
import { NO_VALUE, formatMegabytes, formatNumber } from '$lib/format';
import { PRESSURE, liveSample, metricValue } from '$lib/insights/hostSeries';
import { throttled } from '$lib/status';
import { DISK_LOW_FLOOR_MB, DISK_LOW_PERCENT, diskIsLow } from './health-card';

export type VitalKey = 'cpu' | 'memory' | 'disk';

export interface Vital {
  key: VitalKey;
  /** "CPU", "Memory", "Disk". */
  label: string;
  /** The share of the machine in use, 0 to 100 and unrounded; null where it is not measured. */
  percent: number | null;
  /** The share as the card says it: "34%", or the absent-value mark. */
  value: string;
  /** What the share is of: "8 vCPU", "6.4 / 16 GB". Empty where the machine's size is unknown. */
  detail: string;
  /** Past the line where the product itself starts to act on it, by its own rule for this resource. */
  tight: boolean;
  /** One sentence for the tooltip and for assistive technology, so colour is never the only cue. */
  summary: string;
}

export interface Vitals {
  /** Always CPU, memory and disk in that order, so the card's layout does not move. */
  items: Vital[];
  /** Why nothing could be measured, when nothing could; empty while any figure is. */
  unavailable: string;
}

const LABEL: Record<VitalKey, string> = { cpu: 'CPU', memory: 'Memory', disk: 'Disk' };
const KEYS: readonly VitalKey[] = ['cpu', 'memory', 'disk'];

const MB_PER_GB = 1024;
const MB_PER_TB = 1024 * MB_PER_GB;

/**
 * "6.4 / 16 GB", or "16 GB" with nothing used to pair it with. Written in
 * terabytes from one up, because the pair sits in a narrow column and
 * "1,024 / 2,048 GB" does not fit it. One decimal under ten and none above:
 * 6.4 is worth its digit, 158.2 is noise.
 *
 * Under a tenth of a terabyte used, the pair stays in gigabytes: 20 GB of a
 * terabyte would round to "0 / 1 TB", which says less than the share beside it,
 * and a tenth is the least that survives the decimal place.
 */
function sizeText(totalMb: number, usedMb: number | null): string {
  const tb = totalMb >= MB_PER_TB && (usedMb === null || usedMb >= MB_PER_TB / 10);
  const per = tb ? MB_PER_TB : MB_PER_GB;
  const figure = (mb: number) => {
    const n = mb / per;
    return formatNumber(n < 10 ? Math.round(n * 10) / 10 : Math.round(n));
  };
  const unit = tb ? 'TB' : 'GB';
  return usedMb === null
    ? `${figure(totalMb)} ${unit}`
    : `${figure(usedMb)} / ${figure(totalMb)} ${unit}`;
}

function detailOf(key: VitalKey, host: Host, measured: boolean): string {
  switch (key) {
    case 'cpu':
      return (host.cpus ?? 0) > 0 ? `${formatNumber(host.cpus)} vCPU` : '';
    case 'memory': {
      const total = host.memory_mb ?? 0;
      if (total <= 0) return '';
      const used = Math.max(0, total - (host.usage?.memory_available_mb ?? 0));
      return sizeText(total, measured ? used : null);
    }
    case 'disk': {
      const total = host.disk_total_mb ?? 0;
      if (total <= 0) return '';
      const used = Math.max(0, total - (host.disk_free_mb ?? 0));
      return sizeText(total, measured ? used : null);
    }
  }
}

/** Why a figure is missing, in words that say what to do about it where there is something to do. */
function reasonOf(key: VitalKey, host: Host, silent: boolean): string {
  if (silent) return 'No heartbeat, so no figure is shown as current.';
  if (host.resources_known === false)
    return "This agent has not reported its machine's size, usage or disk. Upgrade the agent and the figures appear on its next heartbeat.";
  if (key === 'disk') return 'This agent has not reported its disk.';
  // A throttle outlives the sample that put it there: the rung stands on the last
  // measurements and is lifted after ten minutes without a fresh one, so "cannot
  // be throttled" would be untrue of a host whose card says it is.
  if (!host.usage_fresh)
    return throttled(host.throttle)
      ? 'Current usage unavailable. The throttle stands on its last measurements and lifts after ten minutes without a fresh one.'
      : 'Current usage unavailable. Placement uses configured capacity and reservations, and this host cannot be throttled.';
  return key === 'cpu'
    ? 'This agent has not reported CPU usage.'
    : 'This agent has not reported available memory.';
}

/**
 * Steal is worth a word on a host's card only once it is more than a
 * measurement's noise: a physical machine reports a measured zero, and a
 * hypervisor with room reports a point or two.
 */
export const STEAL_SHOWN_PERCENT = 5;

/**
 * The share of CPU time the hypervisor kept from the host, or null where there
 * is nothing to say: not measured (an older agent, or a kernel that does not
 * report it), or too small to matter. It is not read as zero, because "CPU usage"
 * counts it as busy and a host that is being starved would otherwise look like
 * one that is simply hard at work.
 */
export function stealShown(percent: number | undefined): number | null {
  return percent !== undefined && percent >= STEAL_SHOWN_PERCENT ? percent : null;
}

/** The CPU elastic CPU has lent to runners, as a share of the whole host. Zero where none is. */
function lentOf(host: Host): number {
  return Math.max(host.usage?.lent_cpu_percent ?? 0, 0);
}

/**
 * What the host holds back of its memory: the machine less what may be placed on
 * it, which is the operator's reserve or the floor under it, whichever is larger.
 * Read from the two figures the API sends, so the card never has to know the
 * floors (5% with a 512 MB minimum, and a gigabyte more where the controller
 * runs). A reserve that covers the machine arrives as no allocatable memory at
 * all, which is the whole machine held back.
 */
function reserveOf(host: Host): number {
  return Math.max((host.memory_mb ?? 0) - (host.allocatable_memory_mb ?? 0), 0);
}

/**
 * Whether the scheduler would act on this figure, by the rule it uses for that
 * resource. The three are not one rule, and a card that drew one line through
 * all of them would be amber where the scheduler does nothing:
 *
 * - CPU: from 85%, counted without what elastic CPU lent to runners. That load
 *   is the plan's own choice, and counting it would have a boost trip the very
 *   limit that then withdraws it.
 * - Memory: not a share at all. New starts are held when available memory is at
 *   or below the host's reserve, so a host 94% used can have room to spare.
 * - Disk: the doctor's rule for a low disk.
 *
 * The comparison is on the raw figure, as the scheduler's is, and the value
 * beside the bar is that figure rounded: 84.6% reads "85%" and is not past
 * the line. Keep in step with internal/scheduler/headroom.go.
 */
function pressed(key: VitalKey, host: Host, sample: HostSample): boolean {
  switch (key) {
    case 'cpu': {
      const cpu = sample.cpu_percent;
      return cpu !== undefined && cpu - lentOf(host) >= PRESSURE;
    }
    case 'memory': {
      const available = sample.memory_available_mb;
      return available !== undefined && (host.memory_mb ?? 0) > 0 && available <= reserveOf(host);
    }
    case 'disk':
      return diskIsLow(host.disk_free_mb ?? 0, host.disk_total_mb ?? 0);
  }
}

/**
 * What a figure's colour means, in words, so that colour is never the only cue.
 * Empty where there is nothing to add.
 */
function noteOf(key: VitalKey, host: Host, sample: HostSample, tight: boolean): string {
  switch (key) {
    case 'disk':
      return tight
        ? `Low: the Work directory free space check warns below ${DISK_LOW_PERCENT}% or ${DISK_LOW_FLOOR_MB / MB_PER_GB} GB free.`
        : '';
    case 'memory':
      return tight
        ? `Available memory is at or below the ${formatMegabytes(reserveOf(host))} reserve, so the scheduler holds new starts here.`
        : '';
    case 'cpu': {
      const lent = lentOf(host);
      if (tight)
        return lent > 0
          ? `From ${PRESSURE}% of the CPU not lent to runners the scheduler places one runner at a time here.`
          : `From ${PRESSURE}% the scheduler places one runner at a time here.`;
      // Busy, but only because runners were lent CPU. A bar at 92% that is not
      // amber would otherwise look like a mistake, and the capacity map beside
      // it still draws the raw figure.
      return lent > 0 && (sample.cpu_percent ?? 0) >= PRESSURE
        ? `${formatNumber(Math.round(lent))}% of it is CPU lent to runners, which the scheduler does not count as pressure.`
        : '';
    }
  }
}

function summaryOf(item: Omit<Vital, 'summary'>, reason: string, note: string): string {
  if (item.percent === null) return `${item.label}: not shown as current. ${reason}`;
  const sentence = `${item.label}: ${item.value} ${item.key === 'disk' ? 'full' : 'in use'}${item.detail ? `, ${item.detail}` : ''}.`;
  return note ? `${sentence} ${note}` : sentence;
}

export function hostVitals(host: Host, now: number): Vitals {
  // A host that has stopped sending heartbeats shows nothing as current. The
  // disk figure is whatever the agent last said, and beside "No heartbeat since
  // twenty minutes ago" it would read as now.
  const silent = host.healthy === false;
  const sample = liveSample(host, now);
  const items = KEYS.map((key): Vital => {
    const percent = silent ? null : metricValue(sample, key);
    const tight = percent !== null && pressed(key, host, sample);
    const item = {
      key,
      label: LABEL[key],
      percent,
      value: percent === null ? NO_VALUE : `${Math.round(percent)}%`,
      detail: detailOf(key, host, percent !== null),
      tight,
    };
    return {
      ...item,
      summary: summaryOf(item, reasonOf(key, host, silent), noteOf(key, host, sample, tight)),
    };
  });
  const measured = items.some((item) => item.percent !== null);
  return { items, unavailable: measured ? '' : reasonOf('cpu', host, silent) };
}
