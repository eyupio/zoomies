/**
 * The three figures a closed host card keeps in view: how much of the machine's
 * CPU, memory and disk is in use.
 *
 * They are the capacity map's own arithmetic (hostSeries.ts), not a second
 * opinion, so the newest point on that chart and this card are one figure by
 * construction. Amber is drawn at the lines the rest of the product already
 * draws it: the scheduler's pressure line for CPU and memory, and the doctor's
 * low-disk rule for the disk.
 *
 * Route-only, like health-card.ts: the card is the only caller.
 */
import type { Host } from '$lib/api/types';
import { NO_VALUE, formatNumber } from '$lib/format';
import { PRESSURE, liveSample, metricValue } from '$lib/insights/hostSeries';
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
  /** Past the line where it starts to cost the fleet something. */
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
 */
function sizeText(totalMb: number, usedMb: number | null): string {
  const tb = totalMb >= MB_PER_TB;
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
  if (!host.usage_fresh)
    return 'Current usage unavailable. Placement uses configured capacity and reservations, and this host cannot be throttled.';
  return key === 'cpu'
    ? 'This agent has not reported CPU usage.'
    : 'This agent has not reported available memory.';
}

function summaryOf(item: Omit<Vital, 'summary'>, reason: string): string {
  if (item.percent === null) return `${item.label}: not shown as current. ${reason}`;
  const sentence = `${item.label}: ${item.value} ${item.key === 'disk' ? 'full' : 'in use'}${item.detail ? `, ${item.detail}` : ''}.`;
  if (!item.tight) return sentence;
  return item.key === 'disk'
    ? `${sentence} Low: the Work directory free space check warns below ${DISK_LOW_PERCENT}% or ${DISK_LOW_FLOOR_MB / MB_PER_GB} GB free.`
    : `${sentence} From ${PRESSURE}% the scheduler places one runner at a time here.`;
}

export function hostVitals(host: Host, now: number): Vitals {
  // A host that has stopped sending heartbeats shows nothing as current. The
  // disk figure is whatever the agent last said, and beside "No heartbeat since
  // twenty minutes ago" it would read as now.
  const silent = host.healthy === false;
  const sample = liveSample(host, now);
  const items = KEYS.map((key): Vital => {
    const percent = silent ? null : metricValue(sample, key);
    const tight =
      percent !== null &&
      (key === 'disk'
        ? diskIsLow(host.disk_free_mb ?? 0, host.disk_total_mb ?? 0)
        : percent >= PRESSURE);
    const item = {
      key,
      label: LABEL[key],
      percent,
      value: percent === null ? NO_VALUE : `${Math.round(percent)}%`,
      detail: detailOf(key, host, percent !== null),
      tight,
    };
    return { ...item, summary: summaryOf(item, reasonOf(key, host, silent)) };
  });
  const measured = items.some((item) => item.percent !== null);
  return { items, unavailable: measured ? '' : reasonOf('cpu', host, silent) };
}
