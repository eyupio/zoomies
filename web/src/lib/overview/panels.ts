/**
 * The Overview's body, as a list.
 *
 * The page under the band is one grid drawn from this list in this order, so
 * there is one place that says what the Overview is made of and nothing in the
 * route lays a panel by hand. A later change lets an account save an order and
 * a hidden set: it reads them over this list, which is why an id here is
 * stable and never renamed, and why a panel's absence in a fleet state is a
 * rule on the panel and not a conditional in the template.
 */

export type OverviewPanelId =
  'attention' | 'pools' | 'failures' | 'jobs' | 'events' | 'history' | 'diagnostics';

export interface OverviewPanel {
  /** Stable, never renamed: a saved layout names panels by it. */
  id: OverviewPanelId;
  title: string;
  /** Where it sits on a desktop: across both columns, or in the two-thirds or one-third column. */
  place: 'full' | 'main' | 'side';
  /** The fleet states in which the panel is not drawn at all. */
  absentWhen?: (fleet: { setupPending: boolean }) => boolean;
}

/**
 * The first-run checklist is the whole message while nothing is configured:
 * a problems card and a failures panel beside it would be two more things
 * explaining the same emptiness.
 */
const firstRun = (fleet: { setupPending: boolean }): boolean => fleet.setupPending;

export const OVERVIEW_PANELS: readonly OverviewPanel[] = [
  { id: 'attention', title: 'Needs attention', place: 'full', absentWhen: firstRun },
  { id: 'pools', title: 'Pools', place: 'full' },
  { id: 'failures', title: 'Recent failures', place: 'full', absentWhen: firstRun },
  { id: 'jobs', title: 'Active jobs', place: 'main' },
  { id: 'events', title: 'Recent events', place: 'side' },
  { id: 'history', title: 'History', place: 'full' },
  { id: 'diagnostics', title: 'Diagnostics', place: 'full' },
];

/** The panels drawn in this fleet state, in the list's order. */
export function visiblePanels(
  panels: readonly OverviewPanel[],
  fleet: { setupPending: boolean },
): OverviewPanel[] {
  return panels.filter((panel) => !panel.absentWhen?.(fleet));
}

/** Problems the attention card shows before it hands over to the drawer. */
export const ATTENTION_SHOWN = 3;
/** Pool cards before the rest become a count and a link to the Pools page. */
export const POOLS_SHOWN = 6;

/**
 * The first few of a list in the order given, and how many it left out, so
 * what a card says ("and 4 more") is the same arithmetic wherever it is said.
 */
export function cap<T>(items: readonly T[], limit: number): { shown: T[]; more: number } {
  return { shown: items.slice(0, limit), more: Math.max(items.length - limit, 0) };
}
