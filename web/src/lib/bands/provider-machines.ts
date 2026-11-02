/**
 * The Providers band's picture: each provider's machines as squares against
 * its ceiling, from the providers and machines the page already lists.
 *
 * Plain functions, so the squares are held to the band's sentence by a test:
 * "2 machines are owned, up to 4 at once" has to be two solid squares and two
 * hollow ones, whatever the layout does.
 */
import type { Machine, Provider } from '../api/types';

/** What a square is: a machine in one of its lives, or room for one. */
export type MachineKind = 'owned' | 'starting' | 'leaving' | 'review' | 'headroom';

export interface MachineRow {
  id: string;
  name: string;
  squares: MachineKind[];
  note: string;
  paused: boolean;
}

export interface MachineRows {
  rows: MachineRow[];
  more: number;
  summary: string;
}

export const MAX_PROVIDERS = 5;
/** Squares a row draws before the rest become a count: a ceiling of fifty is a figure. */
export const MAX_SQUARES = 16;

const ORDER: Record<MachineKind, number> = {
  review: 0,
  owned: 1,
  starting: 2,
  leaving: 3,
  headroom: 4,
};

export function machineKind(state: Machine['state']): MachineKind | null {
  switch (state) {
    case 'ready':
      return 'owned';
    case 'planned':
    case 'creating':
    case 'starting':
    case 'bootstrapping':
    case 'enrolling':
      return 'starting';
    case 'draining':
    case 'deleting':
      return 'leaving';
    case 'failed':
    case 'quarantined':
      return 'review';
    default:
      // A deleted machine is history, not a square.
      return null;
  }
}

export function providerMachines(
  providers: readonly Provider[],
  machines: readonly Machine[],
): MachineRows {
  const byProvider = new Map<string, MachineKind[]>();
  for (const machine of machines) {
    const kind = machineKind(machine.state);
    if (!kind) continue;
    const list = byProvider.get(machine.provider_id) ?? [];
    list.push(kind);
    byProvider.set(machine.provider_id, list);
  }
  const rows: MachineRow[] = providers
    .map((provider) => {
      const held = (byProvider.get(provider.id) ?? []).sort((a, b) => ORDER[a] - ORDER[b]);
      const ceiling = provider.max_machines ?? 0;
      const squares = [
        ...held,
        ...Array<MachineKind>(Math.max(0, ceiling - held.length)).fill('headroom'),
      ];
      const counts = (kind: MachineKind) => held.filter((k) => k === kind).length;
      // Owned as the band's sentence counts it: every machine the provider
      // holds, whatever its state, since each is one the fleet is paying for.
      const parts = [`${held.length} of ${ceiling} owned`];
      if (counts('starting') > 0) parts.push(`${counts('starting')} on the way`);
      if (counts('leaving') > 0) parts.push(`${counts('leaving')} leaving`);
      if (counts('review') > 0)
        parts.push(`${counts('review')} ${counts('review') === 1 ? 'needs' : 'need'} review`);
      if (provider.paused) parts.push('paused');
      else if (!provider.enabled) parts.push('disabled');
      return {
        id: provider.id,
        name: provider.name || provider.id,
        squares,
        note: parts.join(' · '),
        paused: provider.paused === true || !provider.enabled,
      };
    })
    .sort((a, b) => {
      const review = (r: MachineRow) => r.squares.filter((s) => s === 'review').length;
      const held = (r: MachineRow) => r.squares.filter((s) => s !== 'headroom').length;
      return review(b) - review(a) || held(b) - held(a) || a.name.localeCompare(b.name);
    });
  const all = rows.flatMap((r) => r.squares);
  const owned = all.filter((s) => s !== 'headroom').length;
  const ceiling = rows.reduce((n, r) => n + r.squares.length, 0);
  const review = all.filter((s) => s === 'review').length;
  const summary =
    rows.length === 0
      ? 'No providers'
      : `${owned} of ${ceiling}${review > 0 ? ` · ${review} ${review === 1 ? 'needs' : 'need'} review` : ''}`;
  return {
    rows: rows.slice(0, MAX_PROVIDERS),
    more: Math.max(0, rows.length - MAX_PROVIDERS),
    summary,
  };
}
