/**
 * The slot map a band draws beside its sentence: a row per host, a square per
 * slot, from the hosts and runners the fleet cache already holds.
 *
 * Plain functions, as the band sentences are, so the picture can be held to the
 * figures by a test: a map that drew four busy squares on a host whose sentence
 * said three of four in use would be wrong in a way a layout change is very
 * good at hiding. Hosts and Runners draw the same fourteen slots from two
 * sides, so one shape serves both and the component chooses the colouring.
 */
import type { Host, Runner } from '../api/types';
import { slotsOf } from '../hosts/slots';

/** What a square is: a live runner's state, or a slot nothing holds. */
export type SlotKind = 'busy' | 'idle' | 'starting' | 'draining' | 'free';

export interface SlotRow {
  id: string;
  name: string;
  /** One entry per slot, busiest first, so a row reads as a filled bar. */
  slots: SlotKind[];
  /** The row in words, for its note and its accessible name. */
  note: string;
  /** No heartbeat recently: the squares are drawn dashed and the row says so. */
  unreachable: boolean;
  cordoned: boolean;
}

export interface SlotMap {
  rows: SlotRow[];
  /** Hosts past the ones shown, when there are more than the band has rows for. */
  more: number;
  /** The whole fleet in a few words: "9 of 12 busy · 3 free". */
  summary: string;
}

/** How many hosts a band shows before it says "and n more": seven rows is a week. */
export const MAX_ROWS = 7;

const ORDER: Record<SlotKind, number> = { busy: 0, draining: 1, starting: 2, idle: 3, free: 4 };

function kindOf(runner: Runner): SlotKind | null {
  switch (runner.state) {
    case 'busy':
      return 'busy';
    case 'idle':
      return 'idle';
    case 'provisioning':
    case 'registering':
      return 'starting';
    case 'draining':
      return 'draining';
    default:
      // Removed and failed runners hold no slot.
      return null;
  }
}

const counts = (slots: readonly SlotKind[]) => {
  const out: Record<SlotKind, number> = { busy: 0, idle: 0, starting: 0, draining: 0, free: 0 };
  for (const s of slots) out[s] += 1;
  return out;
};

/**
 * The note a row carries. `detail` is the page's question: Hosts asks how
 * much of each machine is taken, Runners asks what the runners on it are doing.
 */
export function rowNote(
  slots: readonly SlotKind[],
  detail: 'occupancy' | 'state',
  flags: { unreachable: boolean; cordoned: boolean },
): string {
  const c = counts(slots);
  const taken = slots.length - c.free;
  const parts: string[] = [];
  if (detail === 'occupancy') {
    parts.push(`${taken} of ${slots.length} in use`);
    if (c.starting > 0) parts.push(`${c.starting} starting`);
  } else {
    if (c.busy > 0) parts.push(`${c.busy} busy`);
    if (c.idle > 0) parts.push(`${c.idle} idle`);
    if (c.starting > 0) parts.push(`${c.starting} starting`);
    if (c.draining > 0) parts.push(`${c.draining} draining`);
    if (parts.length === 0) parts.push(slots.length > 0 ? 'no runners' : 'no slots');
  }
  if (flags.unreachable) parts.push('not heard from');
  else if (flags.cordoned) parts.push('cordoned');
  return parts.join(' · ');
}

/**
 * Lay the fleet out as rows of squares. A runner is placed on the host its
 * record names; one on a host the cache has not got (a host just removed, or a
 * runner whose host is still arriving) is left out rather than invented a row,
 * since the band's own sentence counts hosts from the same cache. A host with
 * more live runners than slots (a throttle lowered them under running work)
 * shows every runner, so the row is longer than its slots rather than hiding
 * one; a host with none shows its slots empty.
 *
 * Rows are the hosts with the most in use first, since the question a map
 * answers is where the work is and where the room is, and the ones past
 * `MAX_ROWS` are a count: a fleet of forty hosts is a figure, not a picture.
 */
export function slotMap(
  hosts: readonly Host[],
  runners: readonly Runner[],
  detail: 'occupancy' | 'state',
): SlotMap {
  const byHost = new Map<string, SlotKind[]>();
  for (const runner of runners) {
    const kind = kindOf(runner);
    if (!kind || !runner.host_id) continue;
    const list = byHost.get(runner.host_id) ?? [];
    list.push(kind);
    byHost.set(runner.host_id, list);
  }
  const rows: SlotRow[] = hosts
    .filter((h): h is Host & { id: string } => typeof h.id === 'string' && h.id !== '')
    .map((host) => {
      const held = byHost.get(host.id) ?? [];
      const total = Math.max(slotsOf(host), held.length);
      const slots = [...held, ...Array<SlotKind>(total - held.length).fill('free')].sort(
        (a, b) => ORDER[a] - ORDER[b],
      );
      const unreachable = host.healthy === false;
      const cordoned = host.cordoned === true;
      return {
        id: host.id,
        name: host.name || host.id,
        slots,
        note: rowNote(slots, detail, { unreachable, cordoned }),
        unreachable,
        cordoned,
      };
    })
    .sort((a, b) => {
      const taken = (r: SlotRow) => r.slots.filter((s) => s !== 'free').length;
      return taken(b) - taken(a) || a.name.localeCompare(b.name);
    });
  const all = rows.flatMap((r) => r.slots);
  const c = counts(all);
  const taken = all.length - c.free;
  const summary =
    rows.length === 0
      ? 'No hosts'
      : detail === 'occupancy'
        ? `${taken} of ${all.length} in use · ${c.free} free`
        : `${c.busy} busy · ${c.idle} idle${c.starting > 0 ? ` · ${c.starting} starting` : ''}${c.draining > 0 ? ` · ${c.draining} draining` : ''}`;
  return { rows: rows.slice(0, MAX_ROWS), more: Math.max(0, rows.length - MAX_ROWS), summary };
}
