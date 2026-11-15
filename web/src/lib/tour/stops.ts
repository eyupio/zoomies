/**
 * A tour: where a list stops when it does not fit the screen it is shown on.
 *
 * A wall display is looked at, not scrolled, so a list taller than its frame
 * (the command centre's hosts, a table's rows in full screen) is shown a page
 * at a time. A page begins at the top of an item and holds every item that
 * fits whole, so nothing is read through a cut edge; the next page begins at
 * the first item the last one could not finish. Pure: the measures are handed
 * in, so the paging is held by a test and no layout is needed to check it.
 */
import { formatNumber } from '../format';

/** One item's place in the list's content, in pixels from its top. */
export interface Span {
  top: number;
  bottom: number;
}

/** How long a page is held before the next, in milliseconds. */
export const DWELL_MS = 10_000;

/** A pixel of slack, since a browser measures lanes in fractions. */
const SLACK = 1;

/**
 * The scroll offsets the tour stops at, first to last. One stop, at nought,
 * means everything fits and there is no tour.
 *
 * The last stop is the end of the content rather than a lane's top, so the
 * last page is full instead of one lane over an empty deck.
 */
export function stops(spans: readonly Span[], view: number, content: number): number[] {
  const end = Math.max(0, content - view);
  if (end <= SLACK || spans.length === 0 || view <= 0) return [0];
  const out = [0];
  let from = 0;
  while (from < spans.length) {
    const top = spans[from]!.top;
    let next = from + 1;
    // A lane taller than the deck still gets a page of its own; the tour
    // moves on from it rather than stopping there for ever.
    while (next < spans.length && spans[next]!.bottom <= top + view + SLACK) next += 1;
    if (next >= spans.length) break;
    const at = spans[next]!.top;
    if (at >= end - SLACK) {
      out.push(end);
      break;
    }
    out.push(at);
    from = next;
  }
  if (out.length === 1) out.push(end);
  return out;
}

/** The stop a scroll offset belongs to: the last one at or before it. */
export function stopAt(all: readonly number[], offset: number): number {
  let index = 0;
  for (let i = 0; i < all.length; i += 1) if (all[i]! <= offset + SLACK) index = i;
  return index;
}

/**
 * The lanes on show at a scroll offset, counted from one: every lane with
 * more than half of itself inside the deck. `null` when there is none.
 */
export function shown(
  spans: readonly Span[],
  offset: number,
  view: number,
): { first: number; last: number } | null {
  let first = 0;
  let last = 0;
  spans.forEach((span, i) => {
    const seen = Math.min(span.bottom, offset + view) - Math.max(span.top, offset);
    if (seen * 2 <= span.bottom - span.top) return;
    if (first === 0) first = i + 1;
    last = i + 1;
  });
  return first === 0 ? null : { first, last };
}

/** What the list holds, and where this page of it starts in the whole. */
export interface TourNoun {
  /** "runners": the list's own word, as its count already says it. */
  plural: string;
  /** "Host", for a list that is all there is; a longer list keeps its plural. */
  singular?: string;
  /** How many items come before the ones measured, on a list fetched in pages. */
  from?: number;
}

const HOSTS: TourNoun = { plural: 'hosts', singular: 'Host' };

/** "Hosts 1 to 4 of 9", "Host 9 of 9", "Runners 51 to 68 of 240". */
export function tourLabel(
  range: { first: number; last: number } | null,
  total: number,
  noun: TourNoun = HOSTS,
): string {
  if (!range) return `${formatNumber(total)} ${noun.plural}`;
  const many = noun.plural.charAt(0).toUpperCase() + noun.plural.slice(1);
  const first = formatNumber(range.first + (noun.from ?? 0));
  const last = formatNumber(range.last + (noun.from ?? 0));
  return first === last
    ? `${noun.singular ?? many} ${first} of ${formatNumber(total)}`
    : `${many} ${first} to ${last} of ${formatNumber(total)}`;
}
