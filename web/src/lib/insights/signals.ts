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
): FleetSample[] {
  const byMinute: Record<number, FleetSample> = {};
  const end = Math.floor(now / 60_000) * 60_000;
  for (const point of [...history, ...live]) {
    const at = Math.floor(new Date(point.at ?? '').getTime() / 60_000) * 60_000;
    if (Number.isFinite(at) && at >= end - 59 * 60_000 && at <= end) byMinute[at] = point;
  }
  return Object.entries(byMinute)
    .sort(([a], [b]) => Number(a) - Number(b))
    .map(([, point]) => point);
}
