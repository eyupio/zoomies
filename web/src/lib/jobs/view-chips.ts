/**
 * The Jobs page's status views as filter chips (Workflows reads the same views
 * and follows). Pure, so the
 * words and the counts are held by unit tests. A view is one whole answer
 * (`views.ts`), pressed one at a time: the chips read it back as the one
 * pressed chip, All as the All chip, and a filter no view describes as
 * nothing pressed at all, which is the truth the view row told too.
 */
import type { FilterChipOption } from '$lib/components/FilterChips.svelte';
import { pickOne } from '$lib/components/chip-selection';
import { JOB_VIEWS, type JobView } from './views';

/** The band's figures the chips can count with: what is running and what waits. */
export interface JobViewCounts {
  running_jobs?: number;
  queued_jobs?: number;
}

export type JobViewChip = FilterChipOption<Exclude<JobView, 'all'>> & { hint: string };

/**
 * The views in the product's order, All left to the All chip. Only Running
 * and Queued have a figure the band already counts, and only while nothing
 * else narrows the rows: the figures are the fleet's, and a repository or pool
 * filter would make them wrong for the list under them.
 */
export function jobViewChips(stats: JobViewCounts | undefined, counted: boolean): JobViewChip[] {
  return JOB_VIEWS.filter((view) => view.id !== 'all').map((view) => {
    const chip: JobViewChip = {
      value: view.id as Exclude<JobView, 'all'>,
      label: view.label,
      hint: view.hint,
    };
    const count =
      !counted || !stats
        ? undefined
        : view.id === 'running'
          ? stats.running_jobs
          : view.id === 'queued'
            ? stats.queued_jobs
            : undefined;
    if (typeof count === 'number') chip.count = count;
    return chip;
  });
}

/** What the chips show pressed for the view in force. */
export function viewSelection(view: JobView | ''): {
  selected: Exclude<JobView, 'all'>[];
  none: boolean;
} {
  if (view === '') return { selected: [], none: true };
  if (view === 'all') return { selected: [], none: false };
  return { selected: [view], none: false };
}

/** The view a press lands on: the chip that changed, or All when the selection empties. */
export function pickView(current: JobView | '', next: readonly Exclude<JobView, 'all'>[]): JobView {
  const picked = pickOne(current === 'all' ? '' : current, next);
  return picked === '' ? 'all' : picked;
}
