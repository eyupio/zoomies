/**
 * Which host cards an operator has opened, remembered per browser.
 *
 * A card is closed unless its host is on this list, so the list holds the
 * exceptions and a host nobody has touched costs nothing. It is written when a
 * choice is made and never on sight, for the reason the capacity map gives:
 * a browser that had merely looked would otherwise have "chosen".
 */
export const EXPANDED_KEY = 'zoomies.hosts.expanded';

/**
 * Whether a stored value is still something to trust. A value left by an older
 * build, or edited by hand, falls back to every card closed rather than
 * throwing on the first card that asks.
 */
export function isIdList(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((id) => typeof id === 'string');
}

/**
 * The list after one card is opened or closed. Hosts that no longer exist are
 * dropped on the way past: machines come and go as pools scale, and without
 * this the list would keep an id for every host anyone ever opened. It returns
 * a new list and leaves the one it was given alone, because the page replaces
 * its state with the answer and an edit in place would repaint nothing.
 */
export function setExpanded(
  ids: readonly string[],
  id: string,
  open: boolean,
  known: readonly string[],
): string[] {
  const kept = ids.filter((held) => held !== id && known.includes(held));
  return open ? [...kept, id] : kept;
}
