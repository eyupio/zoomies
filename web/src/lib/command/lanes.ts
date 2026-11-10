/**
 * The fleet as lanes: one per host, one cell per slot.
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
import { throttled } from '../status';
import { hostHref, runnerHref } from './links';

export type CellKind = 'runner' | 'starting' | 'free';

export interface Cell {
  /** `${host.id}:${index}`: a cell keeps its place when a runner beside it leaves. */
  key: string;
  kind: CellKind;
  href: string;
  runner?: Runner;
}

export type LaneState = 'ok' | 'full' | 'throttled' | 'unreachable' | 'cordoned';

export interface Lane {
  host: Host;
  href: string;
  cells: Cell[];
  state: LaneState;
  tone: 'pending' | 'danger' | 'draining' | null;
  /** '5 of 6 slots', 'full, 2 jobs wait', 'throttled', 'not answering', 'cordoned'. */
  note: string;
  waiting: number;
  /** More slots than a lane draws in one tidy row, so the cells wrap. */
  wide: boolean;
}

export const WIDE_FROM = 4;

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
  // A throttle can lower a host's slots under the runners already on it: every
  // runner is shown, and no free cell is invented to pad a lane that is over.
  const total = Math.max(slotsOf(host), held.length);
  const cells: Cell[] = [];
  const add = (kind: CellKind, runner?: Runner) => {
    const key = `${host.id}:${cells.length}`;
    cells.push(
      runner?.id
        ? { key, kind, href: runnerHref(runner.id), runner }
        : { key, kind, href: hostHref(host.id) },
    );
  };
  for (const r of live) add('runner', r);
  for (const r of starting) add('starting', r);
  while (cells.length < total) add('free');
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
      const taken = cells.filter((c) => c.kind !== 'free').length;
      const pools = new Set(held.map((r) => r.pool_name).filter((p): p is string => !!p));
      let waiting = 0;
      for (const pool of pools) waiting += waitingByPool.get(pool) ?? 0;

      let state: LaneState = 'ok';
      if (host.healthy === false) state = 'unreachable';
      else if (throttled(host.throttle)) state = 'throttled';
      else if (host.cordoned) state = 'cordoned';
      else if (cells.length > 0 && taken >= cells.length) state = 'full';

      return {
        host,
        href: hostHref(host.id),
        cells,
        state,
        ...noteFor(state, taken, cells.length, waiting),
        waiting,
        wide: cells.length > WIDE_FROM,
      };
    })
    .sort(
      (a, b) =>
        b.cells.length - a.cells.length ||
        (a.host.name || a.host.id || '').localeCompare(b.host.name || b.host.id || ''),
    );
}
