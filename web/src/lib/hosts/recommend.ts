/**
 * What to suggest for a host's capacity and reserve, from what the machine is
 * and what a runner in this fleet asks for.
 *
 * Pure, and imported by a unit test. The numbers are a starting point an
 * operator can see the reasoning of, not a promise: the scheduler still
 * places by fit, and a host above its recommendation is one where jobs can
 * wait on slots that read as free.
 */
import type { Pool } from '../api/types';

/** The machine, as the host reported it. Zero means it has not said. */
export interface HostShape {
  cpus: number;
  memoryMb: number;
}

/** What one runner asks for: the largest ask across the enabled pools. */
export interface RunnerAsk {
  cpus: number;
  memoryMb: number;
  /** Where the ask came from, for the sentence under the slider. */
  source: 'pools' | 'default';
  /**
   * Whether the ask was doubled because a slot is a pair. It is the figure
   * that needs explaining: an operator who typed 2 cores on the pool and
   * reads 4 here has to be told which of the two is wrong, and neither is.
   */
  pair: boolean;
}

/** The default a pool created today gets: two cores and four gigabytes. */
export const DEFAULT_ASK: RunnerAsk = { cpus: 2, memoryMb: 4096, source: 'default', pair: false };

export function runnerAsk(pools: readonly Pool[]): RunnerAsk {
  let cpus = 0;
  let memoryMb = 0;
  // Whether the figure that won is a pair's, which is the only thing the
  // sentence has to explain. A dind pool that does not win says nothing here:
  // this function has no host in hand, so it cannot know whether that pool
  // places on the machine being adjusted, and inflating every host in the
  // fleet for one pool would be a recommendation nobody could act on.
  // `host.overprovisioned` is the half that does know, host by host.
  let cpuPair = false;
  let memoryPair = false;
  let anyPair = false;
  for (const pool of pools) {
    if (pool.enabled !== true) continue;
    // A docker-in-docker slot is two containers, not one: the backend gives
    // the sidecar the same limits as the runner, so a slot of such a pool
    // asks for twice what the pool says.
    const containers = pool.docker_mode === 'dind' ? 2 : 1;
    anyPair = anyPair || containers > 1;
    const wantCpus = (pool.resources?.cpus ?? 0) * containers;
    const wantMemoryMb = (pool.resources?.memory_mb ?? 0) * containers;
    if (wantCpus > cpus) {
      cpus = wantCpus;
      cpuPair = containers > 1;
    }
    if (wantMemoryMb > memoryMb) {
      memoryMb = wantMemoryMb;
      memoryPair = containers > 1;
    }
  }
  if (cpus <= 0 && memoryMb <= 0) {
    // Nothing was set anywhere, so the default answers -- twice over where a
    // pool gives its jobs a daemon, since its pair is given the host's share
    // on each container. This is the shape that sizes a host wrong most
    // often, because there is no figure on the pool to double.
    if (!anyPair) return DEFAULT_ASK;
    return {
      cpus: DEFAULT_ASK.cpus * 2,
      memoryMb: DEFAULT_ASK.memoryMb * 2,
      source: 'default',
      pair: true,
    };
  }
  return {
    cpus: cpus > 0 ? cpus : DEFAULT_ASK.cpus,
    memoryMb: memoryMb > 0 ? memoryMb : DEFAULT_ASK.memoryMb,
    source: 'pools',
    pair: cpuPair || memoryPair,
  };
}

/**
 * Cores to hold back for the machine itself: one on anything up to sixteen,
 * then one more for every sixteen. Never the whole machine.
 */
export function recommendedReserveCores(cpus: number): number {
  if (cpus <= 1) return 0;
  return Math.min(cpus - 1, Math.max(1, Math.ceil(cpus / 16)));
}

/**
 * Memory to hold back: a tenth of the machine, between one and eight
 * gigabytes, on the nearest notch of the slider -- so the recommendation is
 * somewhere the slider can actually stop.
 */
export function recommendedReserveMemoryMb(memoryMb: number): number {
  if (memoryMb <= 1024) return 0;
  const tenth = Math.min(memoryMb - 512, Math.max(1024, Math.min(8192, memoryMb * 0.1)));
  let best = 0;
  for (const n of memoryNotches(memoryMb)) {
    if (Math.abs(n - tenth) <= Math.abs(best - tenth)) best = n;
  }
  return best;
}

/**
 * Disk to hold back for the machine: a tenth of the filesystem, between the
 * scheduler's own two-gigabyte floor and sixty-four gigabytes, on the nearest
 * notch of the slider. Runner checkouts and caches land here, so the reserve
 * is what stops a full disk turning into a job that fails part-way through.
 */
export function recommendedReserveDiskMb(diskTotalMb: number): number {
  if (diskTotalMb <= 4096) return 0;
  const tenth = Math.min(diskTotalMb - 2048, Math.max(2048, Math.min(65536, diskTotalMb * 0.1)));
  let best = 0;
  for (const n of diskNotches(diskTotalMb)) {
    if (Math.abs(n - tenth) <= Math.abs(best - tenth)) best = n;
  }
  return best;
}

/** How many runners of this ask fit on what is left after the reserve. At least one. */
export function recommendedCapacity(
  host: HostShape,
  reserveCores: number,
  reserveMb: number,
  ask: RunnerAsk,
): number {
  const byCpu = host.cpus > 0 ? Math.floor((host.cpus - reserveCores) / ask.cpus) : Infinity;
  const byMemory =
    host.memoryMb > 0 ? Math.floor((host.memoryMb - reserveMb) / ask.memoryMb) : Infinity;
  const fit = Math.min(byCpu, byMemory);
  return Number.isFinite(fit) ? Math.max(1, fit) : 0;
}

/** The notches of the capacity slider: every count up to a ceiling worth seeing. */
export function capacityCeiling(host: HostShape, recommended: number, current: number): number {
  const sized = host.cpus > 0 ? Math.max(host.cpus, recommended * 2) : Math.max(8, current * 2);
  return Math.max(4, sized, current);
}

/** The notches of the memory slider: half a gigabyte, then doubling-ish steps, then gigabytes. */
export function memoryNotches(memoryMb: number): number[] {
  if (memoryMb <= 512) return [0];
  const top = memoryMb - 512;
  const out = [0];
  const steps = [512, 1024, 2048, 3072, 4096, 6144, 8192, 12288, 16384, 24576, 32768];
  for (const s of steps) if (s <= top) out.push(s);
  let next = 49152;
  while (next <= top) {
    out.push(next);
    next += 16384;
  }
  return out;
}

/**
 * The notches of the disk slider: two gigabytes -- the scheduler's floor --
 * and then doublings. A disk is two orders of magnitude larger than memory,
 * and a slider that stepped it in gigabytes would be a thousand notches long.
 */
export function diskNotches(diskTotalMb: number): number[] {
  if (diskTotalMb <= 4096) return [0];
  const top = diskTotalMb - 2048;
  const out = [0];
  for (let step = 2048; step <= top; step *= 2) out.push(step);
  return out;
}

/** "4 GB", "512 MB". */
export function memoryLabel(mb: number): string {
  if (mb === 0) return 'none';
  if (mb < 1024) return `${mb} MB`;
  return `${(mb / 1024).toLocaleString(undefined, { maximumFractionDigits: 1 })} GB`;
}

export interface Overcommit {
  /** Cores the runners would ask for, over what is placeable. Zero when they fit. */
  cpus: number;
  memoryMb: number;
}

/** What `capacity` runners of the ask would take beyond the room the reserve leaves. */
export function overcommit(
  host: HostShape,
  capacity: number,
  reserveCores: number,
  reserveMb: number,
  ask: RunnerAsk,
): Overcommit {
  return {
    cpus: host.cpus > 0 ? Math.max(0, capacity * ask.cpus - (host.cpus - reserveCores)) : 0,
    memoryMb:
      host.memoryMb > 0 ? Math.max(0, capacity * ask.memoryMb - (host.memoryMb - reserveMb)) : 0,
  };
}
