/**
 * The kinds of thing the audit log's filter chips offer, and the card's
 * eyebrow. Pure, so the words are held by unit tests. Kinds are not an
 * endpoint of their own: the page gathers them from the rows that have been
 * through it, so the chips grow as the operator reads.
 */
import type { FilterChipOption } from '$lib/components/FilterChips.svelte';
import { pluralise } from '$lib/format';

/**
 * The kinds seen so far, plus the one in force when the rows have not shown
 * it: a log narrowed to one kind by a pasted address shows that kind's rows
 * alone, and a pressed chip has to exist to be pressed.
 */
export function auditKindChips(kinds: readonly string[], selected: string): FilterChipOption[] {
  const all = selected && !kinds.includes(selected) ? [...kinds, selected] : [...kinds];
  return all.map((kind) => ({ value: kind, label: kind }));
}

/** How many events the filters leave, or nothing until the first page has said. */
export function auditEyebrow(total: number | undefined): string | undefined {
  if (total === undefined) return undefined;
  return `${pluralise(total, 'event')}, newest first`;
}
