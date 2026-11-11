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
 * `gridId` is null. Every other parameter, and the hash, is kept byte for byte
 * in the order it was written: the raw query string is split and spliced
 * rather than parsed and written out again, which would turn `%20` into `+`
 * and make the address the operator pasted a different string from the one
 * they get back. The filters are the point of carrying focus on the page's
 * own address.
 */
export function focusAddress(
  pathname: string,
  search: string,
  gridId: string | null,
  hash = '',
): string {
  const kept = search
    .replace(/^\?/, '')
    .split('&')
    .filter((part) => part !== '' && !isFocusPart(part));
  if (gridId) kept.push(`${FOCUS_PARAM}=${encodeURIComponent(gridId)}`);
  return `${pathname}${kept.length > 0 ? `?${kept.join('&')}` : ''}${hash}`;
}

/** Whether one `key=value` piece of a raw query string is the focus parameter. */
function isFocusPart(part: string): boolean {
  const key = part.split('=', 1)[0] ?? '';
  try {
    return decodeURIComponent(key.replace(/\+/g, ' ')) === FOCUS_PARAM;
  } catch {
    return key === FOCUS_PARAM;
  }
}

/** What an element has to offer for being marked: an attribute set and taken off. */
export interface Markable {
  setAttribute(name: string, value: string): void;
  removeAttribute(name: string): void;
}

/**
 * The focus marks on the page, counted.
 *
 * Two grids on one page share ancestors (the orphan review holds three), and
 * Back from one grid's focus to another's runs the second grid's marking and
 * the first one's cleanup in whichever order the effects happen to run. A mark
 * taken off by the first grid's cleanup while the second still needs it left
 * the rest of the page showing. Counted, an attribute comes off only when the
 * last holder lets it go.
 */
export class FocusMarks<T extends Markable = Markable> {
  #counts = new Map<T, Map<string, number>>();

  /** Mark `node`; returns what takes this one mark back off. */
  mark(node: T, name: string): () => void {
    const names = this.#counts.get(node) ?? new Map<string, number>();
    this.#counts.set(node, names);
    const count = names.get(name) ?? 0;
    names.set(name, count + 1);
    if (count === 0) node.setAttribute(name, '');
    let released = false;
    return () => {
      if (released) return;
      released = true;
      const now = (names.get(name) ?? 1) - 1;
      if (now > 0) {
        names.set(name, now);
        return;
      }
      names.delete(name);
      if (names.size === 0) this.#counts.delete(node);
      node.removeAttribute(name);
    };
  }
}
