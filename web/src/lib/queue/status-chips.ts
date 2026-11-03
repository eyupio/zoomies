/**
 * The provisioning statuses as the Queue page's filter chips, and the card's
 * eyebrow. Pure, so the counts and the words are held by unit tests. The
 * counts are the list's own, from every page of the rows the other filters
 * leave, which is why a status chip's count is true of the rows under it even
 * when a repository or pool filter is in force.
 */
import { PROVISIONING_STATUSES, type ProvisioningStatus } from '$lib/api/types';
import { QUEUE_STATUS_LABELS } from '$lib/status';

/** The counts per status GET /provisioning reports beside its page. */
export type QueueCounts = Partial<Record<ProvisioningStatus, number>>;

export interface QueueChip {
  value: ProvisioningStatus;
  label: string;
  count?: number;
}

/** The chips in the product's order, each counted once the first page has answered. */
export function queueStatusChips(counts: QueueCounts): QueueChip[] {
  return PROVISIONING_STATUSES.map((status) => {
    const chip: QueueChip = { value: status, label: QUEUE_STATUS_LABELS[status] };
    const count = counts[status];
    if (typeof count === 'number') chip.count = count;
    return chip;
  });
}

/**
 * What the chips show pressed. The page writes every status out in full when
 * nothing narrows, so "all four" reads back as the All chip rather than as
 * four pressed ones.
 */
export function queueChipSelection(
  provisioning: readonly ProvisioningStatus[],
): ProvisioningStatus[] {
  const every = PROVISIONING_STATUSES.every((status) => provisioning.includes(status));
  return every ? [] : [...provisioning];
}

/** Everything counted, removed included: the All chip's figure. */
export function queueTotal(counts: QueueCounts): number | undefined {
  if (!counted(counts)) return undefined;
  return PROVISIONING_STATUSES.reduce((sum, status) => sum + (counts[status] ?? 0), 0);
}

/** The card's eyebrow, in the words the demand breakdown uses, or nothing until counted. */
export function queueEyebrow(counts: QueueCounts): string | undefined {
  if (!counted(counts)) return undefined;
  const active = (counts.ready ?? 0) + (counts.expedited ?? 0);
  return `${active} active demand, ${counts.paused ?? 0} on hold`;
}

function counted(counts: QueueCounts): boolean {
  return PROVISIONING_STATUSES.some((status) => typeof counts[status] === 'number');
}
