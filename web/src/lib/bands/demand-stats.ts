/**
 * The figures the Runners band carries: how much work is waiting, how it divides by
 * provisioning status, and how long the oldest has waited.
 *
 * Plain functions, as the sentences beside them are, so each figure can be held to its
 * input by a test. They are the figures the provisioning strip above the runner list
 * used to show, moved into the band to give the rows that height back.
 */
import { formatNumber } from '../format';
import type { BandMore, BandStat } from './stats';

export type DemandStatus = 'ready' | 'expedited' | 'paused' | 'deleted';
const ORDER: readonly DemandStatus[] = ['ready', 'expedited', 'paused', 'deleted'];

export interface DemandInput {
  /** Jobs waiting for a runner; null or undefined while the figure has not arrived. */
  queued: number | null | undefined;
  /** Runners executing a job now; null or undefined while the figure has not arrived. */
  running: number | null | undefined;
  /**
   * What "Running" does when the runners list is on this page. It narrows that
   * list and jumps to it; without it the figure opens the runners list.
   */
  onPageRunning?: {
    section: string;
    onselect: () => void;
    active?: boolean;
  };
  /** The queue is deep enough to be worth the warning colour on a plain panel. */
  queuedWarning: boolean;
  /** Provisioning counts by status; null until the first answer. */
  counts: Readonly<Record<string, number>> | null;
  /** The instant the oldest queued demand arrived, if any. */
  oldest: string | null;
  /** Narrows every link to one pool; empty for the whole fleet. */
  poolId: string;
  /** What each status is called, from the shared map, so every surface says the same. */
  labels: Readonly<Record<DemandStatus, string>>;
}

const poolQuery = (poolId: string) => (poolId ? `&pool_id=${encodeURIComponent(poolId)}` : '');
const queueQuery = (poolId: string) => (poolId ? `?pool_id=${encodeURIComponent(poolId)}` : '');

export function demandStats(input: DemandInput): BandStat[] {
  const stats: BandStat[] = [
    {
      label: input.queued === 1 ? 'Job queued' : 'Jobs queued',
      value: formatNumber(input.queued),
      hint: 'Jobs waiting for a runner. Open the queue.',
      href: `/queue${queueQuery(input.poolId)}#queue-list`,
      tone: input.queuedWarning ? 'warning' : undefined,
    },
    {
      label: 'Running',
      value:
        input.running === null || input.running === undefined
          ? undefined
          : formatNumber(input.running),
      hint: input.onPageRunning
        ? 'Runners executing a job right now. Show them below.'
        : 'Runners executing a job right now. Open them.',
      ...(input.onPageRunning
        ? {
            section: input.onPageRunning.section,
            onselect: input.onPageRunning.onselect,
            active: input.onPageRunning.active,
          }
        : { href: `/runners?state=busy${poolQuery(input.poolId)}#runners-list` }),
    },
  ];
  for (const status of ORDER) {
    stats.push({
      label: input.labels[status],
      // Not zero while the answer is on its way: zero is a claim.
      value: input.counts ? formatNumber(input.counts[status] ?? 0) : undefined,
      hint: `Provisioning demand that is ${input.labels[status].toLowerCase()}. Open it in the queue.`,
      href: `/queue?provisioning=${status}${poolQuery(input.poolId)}#queue-list`,
    });
  }
  if (input.oldest) {
    stats.push({
      label: 'Oldest demand',
      since: input.oldest,
      hint: 'How long the oldest queued demand has been waiting.',
    });
  }
  return stats;
}

export function demandMore(poolId: string): BandMore {
  return {
    label: 'Manage queue',
    href: `/queue${queueQuery(poolId)}#queue-list`,
  };
}
