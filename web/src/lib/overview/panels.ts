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
 * a problems card, a failures panel and a history of nothing beside it would
 * be three more things explaining the same emptiness.
 */
const firstRun = (fleet: { setupPending: boolean }): boolean => fleet.setupPending;

export const OVERVIEW_PANELS: readonly OverviewPanel[] = [
  { id: 'attention', title: 'Needs attention', place: 'full', absentWhen: firstRun },
  { id: 'pools', title: 'Pools', place: 'full' },
  { id: 'failures', title: 'Recent failures', place: 'full', absentWhen: firstRun },
  { id: 'jobs', title: 'Active jobs', place: 'main' },
  { id: 'events', title: 'Recent events', place: 'side' },
  { id: 'history', title: 'History', place: 'full', absentWhen: firstRun },
  { id: 'diagnostics', title: 'Diagnostics', place: 'full' },
];

export const OVERVIEW_PANEL_IDS: readonly OverviewPanelId[] = OVERVIEW_PANELS.map((p) => p.id);

export function isPanelId(value: unknown): value is OverviewPanelId {
  return typeof value === 'string' && (OVERVIEW_PANEL_IDS as readonly string[]).includes(value);
}

/**
 * How one person has arranged the page: the order the panels are drawn in,
 * and the ones not drawn. Kept with the account, so the arrangement made at
 * the desk is the one the phone draws; read forgivingly, because a build
 * that adds a panel must never hide it and a build that removes one must
 * never break a layout that named it.
 */
export interface PanelLayout {
  /** Panel ids in the order drawn. A panel not named is appended in default order. */
  order: OverviewPanelId[];
  /** Panels not drawn. */
  hidden: OverviewPanelId[];
}

export const DEFAULT_LAYOUT: PanelLayout = { order: [...OVERVIEW_PANEL_IDS], hidden: [] };

/** The ids a document names that this build has, each once, in the document's order. */
function knownIds(value: unknown): OverviewPanelId[] {
  if (!Array.isArray(value)) return [];
  return [...new Set(value.filter(isPanelId))];
}

/** The layout a stored or received document describes, the default where it says nothing this build can honour. */
export function readPanelLayout(raw: unknown, panels: readonly OverviewPanel[]): PanelLayout {
  const doc = raw && typeof raw === 'object' ? (raw as Record<string, unknown>) : {};
  const order = knownIds(doc.order);
  for (const panel of panels) if (!order.includes(panel.id)) order.push(panel.id);
  return { order, hidden: knownIds(doc.hidden) };
}

/** The panels drawn: the layout's order, less the hidden, less those the fleet state leaves out. */
export function visibleLayout(
  panels: readonly OverviewPanel[],
  layout: PanelLayout,
  fleet: { setupPending: boolean },
): OverviewPanel[] {
  return ordered(panels, layout).filter(
    (panel) => !layout.hidden.includes(panel.id) && !panel.absentWhen?.(fleet),
  );
}

/** The panels a person hid and could show again now: hidden, and not absent in this fleet state. */
export function hiddenPanels(
  panels: readonly OverviewPanel[],
  layout: PanelLayout,
  fleet: { setupPending: boolean },
): OverviewPanel[] {
  return ordered(panels, layout).filter(
    (panel) => layout.hidden.includes(panel.id) && !panel.absentWhen?.(fleet),
  );
}

function ordered(panels: readonly OverviewPanel[], layout: PanelLayout): OverviewPanel[] {
  const byId = new Map(panels.map((panel) => [panel.id, panel]));
  return readPanelLayout(layout, panels)
    .order.map((id) => byId.get(id))
    .filter((panel): panel is OverviewPanel => panel !== undefined);
}

/** A panel as the body draws it while arranging: its card, or a row saying it is hidden. */
export interface PanelRow {
  panel: OverviewPanel;
  hidden: boolean;
}

/**
 * Every panel present in this fleet state, in layout order, each saying
 * whether it is hidden. The body is drawn from this in one pass, so a hidden
 * panel's row stands where its card would, and a move steps over what the eye
 * sees rather than over the whole order.
 */
export function panelRows(
  panels: readonly OverviewPanel[],
  layout: PanelLayout,
  fleet: { setupPending: boolean },
): PanelRow[] {
  return ordered(panels, layout)
    .filter((panel) => !panel.absentWhen?.(fleet))
    .map((panel) => ({ panel, hidden: layout.hidden.includes(panel.id) }));
}

/**
 * The layout with one panel swapped with its neighbour among the panels
 * drawn (`among`, the whole order when every panel is); unchanged at an end.
 * A panel the fleet state leaves out is stepped over, so a press always
 * changes what is on the screen.
 */
export function movePanel(
  layout: PanelLayout,
  id: OverviewPanelId,
  delta: -1 | 1,
  among: readonly OverviewPanelId[] = layout.order,
): PanelLayout {
  const at = among.indexOf(id);
  const neighbour = among[at + delta];
  if (at < 0 || neighbour === undefined) return layout;
  if (delta < 0) return placePanel(layout, id, neighbour);
  const rest = layout.order.filter((other) => other !== id);
  const after = rest[rest.indexOf(neighbour) + 1] ?? null;
  return placePanel(layout, id, after);
}

/** The layout with one panel placed before another, or last when `before` is null. */
export function placePanel(
  layout: PanelLayout,
  id: OverviewPanelId,
  before: OverviewPanelId | null,
): PanelLayout {
  const order = layout.order.filter((other) => other !== id);
  const at = before === null ? -1 : order.indexOf(before);
  if (at < 0) order.push(id);
  else order.splice(at, 0, id);
  return { order, hidden: [...layout.hidden] };
}

export function setHidden(layout: PanelLayout, id: OverviewPanelId, hidden: boolean): PanelLayout {
  const rest = layout.hidden.filter((other) => other !== id);
  return { order: [...layout.order], hidden: hidden ? [...rest, id] : rest };
}

export function isDefaultLayout(layout: PanelLayout): boolean {
  return (
    layout.hidden.length === 0 &&
    layout.order.length === OVERVIEW_PANEL_IDS.length &&
    layout.order.every((id, i) => id === OVERVIEW_PANEL_IDS[i])
  );
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
