/**
 * Where a change made on one band should land.
 *
 * A size icon used to mean "this page", which was right until an operator set
 * the same cut on eight bands one after another. The choice now travels with
 * the change: this page, a set of pages picked one at a time, or every band.
 * The picker is a shared module so the band and the dog ask the question the
 * same way, in the same words.
 */

import { BAND_PAGES, type BandPage } from './pages';

export const BAND_SCOPES = ['page', 'selected', 'all'] as const;
export type BandScope = (typeof BAND_SCOPES)[number];

/** Every page with a band, in the order Appearance lists them. */
export const ALL_BAND_PAGES: readonly BandPage[] = BAND_PAGES.map((page) => page.key);

export const BAND_SCOPE_OPTIONS = [
  {
    value: 'page',
    label: 'This page',
    description: 'Only the band you are looking at.',
  },
  {
    value: 'selected',
    label: 'Choose pages',
    description: 'Pick the bands one at a time.',
  },
  {
    value: 'all',
    label: 'Every page',
    description: 'Every page that has a band.',
  },
] as const;

/** The pages a scope covers, in the order Appearance lists them. */
export function pagesForScope(
  scope: BandScope,
  origin: BandPage,
  selected: readonly BandPage[],
): BandPage[] {
  if (scope === 'all') return [...ALL_BAND_PAGES];
  if (scope === 'selected') return ALL_BAND_PAGES.filter((page) => selected.includes(page));
  return [origin];
}

/** A page's name as the rest of the product says it. */
export function bandPageLabel(page: BandPage): string {
  return BAND_PAGES.find((entry) => entry.key === page)?.label ?? page;
}

/**
 * "Runners", "Runners and Pools", "Runners, Pools and Jobs", or the first three
 * and a count: a sentence an operator can read at a glance, not a list that
 * pushes the button off the panel.
 */
export function bandPagesSentence(pages: readonly BandPage[]): string {
  const labels = pages.map(bandPageLabel);
  if (labels.length === 0) return 'no pages';
  const shown = labels.length > 3 ? labels.slice(0, 2) : labels;
  const rest = labels.length - shown.length;
  if (rest === 0) {
    return shown.length === 1
      ? (shown[0] ?? '')
      : `${shown.slice(0, -1).join(', ')} and ${shown[shown.length - 1] ?? ''}`;
  }
  return `${shown.join(', ')} and ${rest} more`;
}
