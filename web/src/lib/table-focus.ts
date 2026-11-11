/**
 * The address of a table's focus view.
 *
 * Focus is a query parameter on the page's own address, `?focus=<grid id>`,
 * rather than a route of its own: the page that draws the table in focus is the
 * same page, with the same filters, sort and selection, so a view can be pasted,
 * reloaded and reached with Back and Forward, and leaving it is the same address
 * with that one parameter gone. It names the grid rather than saying `1`
 * because a page may hold more than one (a provider's orphan review holds
 * three), and a pasted address has to say which of them was meant.
 *
 * Pure, so a unit test can read it: no router, no runes.
 */

/** The query parameter a focus view is kept in. */
export const FOCUS_PARAM = 'focus';

/** The grid a query string puts in focus, or null when it puts none. */
export function focusedGrid(search: string): string | null {
  const value = new URLSearchParams(search).get(FOCUS_PARAM)?.trim();
  return value ? value : null;
}

/**
 * The address of this page with `gridId` in focus, or with focus left when
 * `gridId` is null. Every other parameter, and the hash, is kept as it is, in
 * the order it was written: the filters are the point of carrying focus on
 * the page's own address.
 */
export function focusAddress(
  pathname: string,
  search: string,
  gridId: string | null,
  hash = '',
): string {
  const params = new URLSearchParams(search);
  if (gridId) params.set(FOCUS_PARAM, gridId);
  else params.delete(FOCUS_PARAM);
  const query = params.toString();
  return `${pathname}${query ? `?${query}` : ''}${hash}`;
}
