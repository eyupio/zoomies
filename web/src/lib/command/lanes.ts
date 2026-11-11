/**
 * The fleet as lanes: one per host, a cell for each runner on it and one
 * cell that counts the slots left.
 *
 * The same figures as the band slot map, drawn larger and with a link in every
 * cell, so it reads the host's slots and runners through the same helpers and
 * only adds what the wall needs: a stable key for each cell, where the cell
 * leads, and one word about the lane. Pure, so the picture is held to the
 * figures by a test and forty hosts are forty lanes with no layout deciding
 * otherwise.
 */
import type { Host, Job, Runner } from '../api/types';
import { slotsOf } from '../hosts/slots';
import { pluralise } from '../format';
import { throttled } from '../status';
import { hostHref, runnerHref } from './links';

export type CellKind = 'runner' | 'starting' | 'free';

export interface Cell {
  /**
   * The runner's id, or `${host.id}:free` for the free cell, so a tile keeps
   * its identity (and does not replay its entrance) when a runner beside it
   * leaves.
   */
  key: string;
  kind: CellKind;
  href: string;
  runner?: Runner;
  /** How many slots the free cell stands for. Set on the free cell alone. */
  count?: number;
}

export type LaneState = 'ok' | 'full' | 'throttled' | 'unreachable' | 'cordoned';

export interface Lane {
  host: Host;
  href: string;
  cells: Cell[];
  state: LaneState;
  tone: 'pending' | 'danger' | 'draining' | null;
  /** '5 of 6 slots', 'no slots', 'full, 2 jobs wait', 'throttled', 'not answering', 'cordoned'. */
  note: string;
  /**
   * Queued jobs for the pools running here. The same job can count towards
   * more than one lane (a pool spans hosts), so never sum this across lanes.
   */
  waiting: number;
  /** The slots the lane counts: its runners, and the free ones the cell stands for. */
  slots: number;
  /** Free slots, which the free cell carries as its count. */
  free: number;
  /** More cells than a lane draws in one tidy row, so the cells wrap. */
  wide: boolean;
}

export const WIDE_FROM = 4;

/** What the free cell says: "14 free slots", "1 free slot". */
export function freeSlots(count: number): string {
  return pluralise(count, 'free slot');
}

/** A live runner holds a slot; a removed or failed one has let it go. */
function holdsSlot(runner: Runner): boolean {
  return (
    runner.state === 'busy' ||
    runner.state === 'idle' ||
    runner.state === 'draining' ||
    runner.state === 'provisioning' ||
    runner.state === 'registering'
  );
}

function isStarting(runner: Runner): boolean {
  return runner.state === 'provisioning' || runner.state === 'registering';
}

function cellsFor(host: Host & { id: string }, held: readonly Runner[]): Cell[] {
  // A stable order, so a runner finishing does not shuffle the ones beside it.
  const byAge = (a: Runner, b: Runner) =>
    (a.created_at ?? '').localeCompare(b.created_at ?? '') ||
    (a.id ?? '').localeCompare(b.id ?? '');
  const live = held.filter((r) => !isStarting(r)).sort(byAge);
  const starting = held.filter(isStarting).sort(byAge);
  const cells: Cell[] = [];
  const add = (kind: CellKind, runner: Runner) => {
    // Keyed by the runner, so a tile is the same tile however many leave
    // before it. A runner the API sent without an id falls back to its place,
    // which is the best identity it has.
    const key = runner.id ?? `${host.id}:${cells.length}`;
    // A runner without an id still holds its slot and is drawn, but it has no
    // page to open, so its cell opens the host it runs on: the nearest page
    // that says anything about it, rather than a dead link. The tile keeps the
    // runner, so it still shows the job and the state.
    const href = runner.id ? runnerHref(runner.id) : hostHref(host.id);
    cells.push({ key, kind, href, runner });
  };
  for (const r of live) add('runner', r);
  for (const r of starting) add('starting', r);
  // One cell for the room left, however much of it there is: fourteen dashed
  // boxes saying the same thing made an idle host mostly empty outlines. A
  // throttle can lower a host's slots under the runners already on it, and
  // then there is no room and no cell.
  const free = slotsOf(host) - held.length;
  if (free > 0)
    cells.push({ key: `${host.id}:free`, kind: 'free', href: hostHref(host.id), count: free });
  return cells;
}

function noteFor(
  state: LaneState,
  taken: number,
  total: number,
  waiting: number,
): Pick<Lane, 'note' | 'tone'> {
  switch (state) {
    case 'unreachable':
      return { note: 'not answering', tone: 'danger' };
    case 'throttled':
      return { note: 'throttled', tone: 'danger' };
    case 'cordoned':
      return { note: 'cordoned', tone: 'draining' };
    case 'full':
      return waiting > 0
        ? { note: `full, ${waiting} ${waiting === 1 ? 'job waits' : 'jobs wait'}`, tone: 'pending' }
        : { note: 'full', tone: null };
    default:
      // "0 of 0 slots" reads as a sum that went wrong; a host with a capacity
      // of nought and nothing running on it is said plainly.
      if (total === 0) return { note: 'no slots', tone: null };
      return { note: `${taken} of ${total} slots`, tone: null };
  }
}

/**
 * The lanes, largest host first and ties in name order.
 *
 * A queued job counts towards a host when its `pool_name` names a pool with at
 * least one live runner on that host. That is an approximation of where the
 * scheduler would place it (it ignores labels, free slots and host health), and
 * it is used only for the lane's note: the verdict's queue count does not
 * depend on it.
 */
export function lanes(
  hosts: readonly Host[],
  runners: readonly Runner[],
  queued: readonly Job[],
): Lane[] {
  const byHost = new Map<string, Runner[]>();
  for (const runner of runners) {
    if (!runner.host_id || !holdsSlot(runner)) continue;
    const list = byHost.get(runner.host_id) ?? [];
    list.push(runner);
    byHost.set(runner.host_id, list);
  }
  const waitingByPool = new Map<string, number>();
  for (const job of queued) {
    if (!job.pool_name) continue;
    waitingByPool.set(job.pool_name, (waitingByPool.get(job.pool_name) ?? 0) + 1);
  }

  return hosts
    .filter((h): h is Host & { id: string } => !!h.id && h.retired !== true)
    .map((host) => {
      const held = byHost.get(host.id) ?? [];
      const cells = cellsFor(host, held);
      const taken = held.length;
      const free = cells.find((c) => c.kind === 'free')?.count ?? 0;
      const slots = taken + free;
      const pools = new Set(held.map((r) => r.pool_name).filter((p): p is string => !!p));
      let waiting = 0;
      for (const pool of pools) waiting += waitingByPool.get(pool) ?? 0;

      let state: LaneState = 'ok';
      if (host.healthy === false) state = 'unreachable';
      else if (throttled(host.throttle)) state = 'throttled';
      else if (host.cordoned) state = 'cordoned';
      else if (slots > 0 && free === 0) state = 'full';

      return {
        host,
        href: hostHref(host.id),
        cells,
        state,
        ...noteFor(state, taken, slots, waiting),
        waiting,
        slots,
        free,
        wide: cells.length > WIDE_FROM,
      };
    })
    .sort(
      (a, b) =>
        b.slots - a.slots ||
        (a.host.name || a.host.id || '').localeCompare(b.host.name || b.host.id || ''),
    );
}
