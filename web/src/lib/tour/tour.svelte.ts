/**
 * A tour, running: it measures the items in a scrolling frame, holds each
 * page for its dwell and moves to the next, and stands still while somebody
 * is using the list.
 *
 * Content that moves by itself has to be stoppable, so the tour holds while
 * the pointer is over the frame or its pager and has moved within the last
 * minute, while the keyboard's focus is
 * inside either, while its Pause is pressed, while the tab is hidden and
 * while its owner says it is busy. It never fights a hand: a page is found
 * from where the frame is scrolled to, so somebody who scrolled is carried on
 * from there. Under reduced motion a page is swapped rather than slid.
 */
import { untrack } from 'svelte';
import { DWELL_MS, shown, stopAt, stops, tourLabel, type Span, type TourNoun } from './stops';

const TICK_MS = 200;
/**
 * How long a pointer may lie still over the frame and still hold the tour. A
 * hand reading a row moves; a mouse left where a drawer was closed does not,
 * and on a wall display nobody is there to move it away again.
 */
const POINTER_REST_MS = 60_000;

export interface TourOptions {
  /** The items a page is made of, in order: the frame's lanes, a table's rows. */
  items: (frame: HTMLElement) => HTMLElement[];
  /** Whether the tour may run at all. Read as it runs; call `refresh` when it changes. */
  enabled?: () => boolean;
  /** Something of the owner's is open or selected, and moving the list would lose it. */
  busy?: () => boolean;
  /**
   * The room under the last item while the tour is off, which the frame's
   * whole height is asked about. The pager's room is padding the frame takes
   * only while touring, so asking of the padded height would turn the tour on
   * and off as that padding came and went.
   */
  restingFoot?: (style: CSSStyleDeclaration) => number;
  /** How many items there are in all, when the frame holds one page of more. */
  total?: () => number;
  /** The list's own word, and where this page of it starts. */
  noun?: () => TourNoun;
  /**
   * The list goes on past what the frame holds. Asked at the last page; an
   * owner that answers true has moved to its next page of items itself, and
   * the tour starts again at the top of whatever arrives.
   */
  next?: () => boolean;
  /** There is somewhere for `next` to go, so one page that fits is still toured. */
  more?: () => boolean;
}

export class Tour {
  /** The scroll offsets it stops at; one of them means nothing here to page. */
  stops = $state.raw<number[]>([0]);
  page = $state(0);
  label = $state('');
  paused = $state(false);
  /** How far through this page's dwell, nought to one. */
  progress = $state(0);
  /** The pointer or the keyboard's focus is on the frame or the pager. */
  #engaged = $state(false);
  #on = $state(false);
  /** Whether there is anything to tour: more than one page here, or more to fetch. */
  get active(): boolean {
    return this.#on && (this.stops.length > 1 || (this.options.more?.() ?? false));
  }
  get held(): boolean {
    return this.paused || this.#engaged;
  }

  #node: HTMLElement | null = null;
  #spans: Span[] = [];
  #view = 0;
  #elapsed = 0;
  /** How many pointers and keyboard focuses are on the frame or the pager. */
  #holds = 0;

  constructor(readonly options: TourOptions) {}

  toggle = (): void => {
    this.paused = !this.paused;
  };

  /** Measure again: what `enabled` answers, or the page of items, has changed. */
  refresh = (): void => {
    untrack(() => this.#measure());
  };

  /** The pager counts as the frame: reading it should not move what it describes. */
  engage = (node: HTMLElement): (() => void) => {
    const on = { pointer: false, focus: false };
    const set = (what: keyof typeof on, to: boolean) => {
      if (on[what] === to) return;
      on[what] = to;
      this.#holds += to ? 1 : -1;
      this.#engaged = this.#holds > 0;
    };
    let rest: ReturnType<typeof setTimeout> | undefined;
    const out = () => {
      clearTimeout(rest);
      set('pointer', false);
    };
    // Every movement holds afresh, so the tour waits for as long as somebody
    // is pointing at the list and no longer than a minute after they stop.
    const over = () => {
      set('pointer', true);
      clearTimeout(rest);
      rest = setTimeout(out, POINTER_REST_MS);
    };
    // Only focus the keyboard put there. A click leaves focus on what was
    // clicked, and a tour that stayed held because Pause had just been let go
    // with a mouse would never start again.
    const focus = (event: FocusEvent) => {
      if ((event.target as Element).matches(':focus-visible')) set('focus', true);
    };
    const blur = (event: FocusEvent) => {
      if (!node.contains(event.relatedTarget as Node | null)) set('focus', false);
    };
    node.addEventListener('pointerenter', over);
    node.addEventListener('pointermove', over);
    node.addEventListener('pointerleave', out);
    node.addEventListener('focusin', focus);
    node.addEventListener('focusout', blur);
    return () => {
      node.removeEventListener('pointerenter', over);
      node.removeEventListener('pointermove', over);
      node.removeEventListener('pointerleave', out);
      node.removeEventListener('focusin', focus);
      node.removeEventListener('focusout', blur);
      out();
      set('focus', false);
    };
  };

  /** The frame itself: measured, scrolled and listened to. */
  attach = (node: HTMLElement): (() => void) => {
    this.#node = node;
    const measure = () => this.#measure();
    const resized = new ResizeObserver(measure);
    resized.observe(node);
    // An item growing, or a page of them arriving, changes what fits without
    // the frame changing size.
    const changed = new MutationObserver(measure);
    changed.observe(node, { childList: true, subtree: true });
    const scrolled = () => this.#place();
    // A hand on the frame starts the dwell again, so the page somebody just
    // scrolled to is not taken away a second later.
    const touched = () => this.#restart();
    node.addEventListener('scroll', scrolled, { passive: true });
    node.addEventListener('wheel', touched, { passive: true });
    node.addEventListener('touchstart', touched, { passive: true });
    const disengage = this.engage(node);
    const timer = setInterval(() => this.#tick(), TICK_MS);
    // An attachment runs in an effect, and measuring reads the stops it then
    // writes: tracked, the frame would be attached again by its own measuring.
    untrack(measure);
    return () => {
      clearInterval(timer);
      disengage();
      resized.disconnect();
      changed.disconnect();
      node.removeEventListener('scroll', scrolled);
      node.removeEventListener('wheel', touched);
      node.removeEventListener('touchstart', touched);
      this.#node = null;
      this.#on = false;
      this.stops = [0];
    };
  };

  #measure(): void {
    const node = this.#node;
    if (!node) return;
    const was = this.active;
    const items = this.options.items(node);
    const style = getComputedStyle(node);
    // Stacked on a phone the frame does not scroll, the page does: no tour.
    this.#on =
      (this.options.enabled?.() ?? true) && items.length > 0 && style.overflowY !== 'visible';
    if (!this.#on) {
      this.#spans = [];
      this.stops = [0];
    } else {
      // What stands over the first item, a frame's padding or a table's
      // heading row held at its top, is the same height over every page.
      const lead = items[0]!.offsetTop;
      this.#spans = items.map((item) => ({
        top: item.offsetTop - lead,
        bottom: item.offsetTop - lead + item.offsetHeight,
      }));
      const content = this.#spans.at(-1)!.bottom;
      const foot = this.options.restingFoot?.(style) ?? 0;
      const fits = content <= node.clientHeight - lead - foot + 1;
      this.#view = node.clientHeight - lead - (parseFloat(style.paddingBottom) || 0);
      const next = fits ? [0] : stops(this.#spans, this.#view, content);
      if (next.length !== this.stops.length || next.some((stop, i) => stop !== this.stops[i]))
        this.stops = next;
    }
    if (this.active !== was) {
      this.#restart();
      // The frame's padding has changed with the tour; measure again with it.
      requestAnimationFrame(() => this.#measure());
    }
    this.#place();
  }

  #place(): void {
    const node = this.#node;
    if (!node) return;
    this.page = stopAt(this.stops, node.scrollTop);
    this.label = tourLabel(
      shown(this.#spans, node.scrollTop, this.#view),
      this.options.total?.() ?? this.#spans.length,
      this.options.noun?.(),
    );
  }

  #restart(): void {
    this.#elapsed = 0;
    this.progress = 0;
  }

  #tick(): void {
    const node = this.#node;
    if (!node || !this.active || this.held || document.hidden) return;
    if (this.options.busy?.()) return;
    this.#elapsed += TICK_MS;
    if (this.#elapsed < DWELL_MS) {
      this.progress = this.#elapsed / DWELL_MS;
      return;
    }
    this.#restart();
    const at = stopAt(this.stops, node.scrollTop);
    const calm = matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (at === this.stops.length - 1 && this.options.next?.()) {
      // The next page of items is on its way; it is read from its top.
      node.scrollTo({ top: 0, behavior: 'auto' });
      return;
    }
    const next = (at + 1) % this.stops.length;
    node.scrollTo({ top: this.stops[next]!, behavior: calm ? 'auto' : 'smooth' });
  }
}
