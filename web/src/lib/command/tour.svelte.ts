/**
 * The deck's tour, running: it measures the lanes, holds each page for its
 * dwell and moves to the next, and stands still while somebody is using the
 * deck.
 *
 * Content that moves by itself has to be stoppable, so the tour holds while
 * the pointer is over the deck or its pager, while focus is inside either,
 * while its Pause is pressed and while the tab is hidden. It never fights a
 * hand: a page is found from where the deck is scrolled to, so somebody who
 * scrolled is carried on from there. Under reduced motion a page is swapped
 * rather than slid.
 */
import { untrack } from 'svelte';
import { DWELL_MS, shown, stopAt, stops, tourLabel, type Span } from './tour';

const TICK_MS = 200;

export class DeckTour {
  /** The scroll offsets it stops at; one of them means there is no tour. */
  stops = $state.raw<number[]>([0]);
  page = $state(0);
  label = $state('');
  paused = $state(false);
  /** How far through this page's dwell, nought to one. */
  progress = $state(0);
  /** The pointer or focus is on the deck or the pager. */
  #engaged = $state(false);
  readonly active = $derived(this.stops.length > 1);
  readonly held = $derived(this.paused || this.#engaged);

  #node: HTMLElement | null = null;
  #spans: Span[] = [];
  #view = 0;
  #elapsed = 0;
  /** How many pointers and keyboard focuses are on the deck or the pager. */
  #holds = 0;

  toggle = (): void => {
    this.paused = !this.paused;
  };

  /** The pager counts as the deck: reading it should not move what it describes. */
  engage = (node: HTMLElement): (() => void) => {
    const on = { pointer: false, focus: false };
    const set = (what: keyof typeof on, to: boolean) => {
      if (on[what] === to) return;
      on[what] = to;
      this.#holds += to ? 1 : -1;
      this.#engaged = this.#holds > 0;
    };
    const over = () => set('pointer', true);
    const out = () => set('pointer', false);
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
    node.addEventListener('pointerleave', out);
    node.addEventListener('focusin', focus);
    node.addEventListener('focusout', blur);
    return () => {
      node.removeEventListener('pointerenter', over);
      node.removeEventListener('pointerleave', out);
      node.removeEventListener('focusin', focus);
      node.removeEventListener('focusout', blur);
      out();
      set('focus', false);
    };
  };

  /** The deck itself: measured, scrolled and listened to. */
  attach = (node: HTMLElement): (() => void) => {
    this.#node = node;
    const measure = () => this.#measure();
    const resized = new ResizeObserver(measure);
    resized.observe(node);
    // A runner arriving makes a lane taller without the deck changing size.
    const changed = new MutationObserver(measure);
    changed.observe(node, { childList: true, subtree: true });
    const scrolled = () => this.#place();
    // A hand on the deck starts the dwell again, so the page somebody just
    // scrolled to is not taken away a second later.
    const touched = () => this.#restart();
    node.addEventListener('scroll', scrolled, { passive: true });
    node.addEventListener('wheel', touched, { passive: true });
    node.addEventListener('touchstart', touched, { passive: true });
    const disengage = this.engage(node);
    const timer = setInterval(() => this.#tick(), TICK_MS);
    // An attachment runs in an effect, and measuring reads the stops it then
    // writes: tracked, the deck would be attached again by its own measuring.
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
      this.stops = [0];
    };
  };

  #measure(): void {
    const node = this.#node;
    if (!node) return;
    const lanes = Array.from(node.querySelectorAll<HTMLElement>(':scope > section'));
    const style = getComputedStyle(node);
    // Stacked on a phone the deck does not scroll, the page does: no tour.
    if (lanes.length === 0 || style.overflowY === 'visible') {
      this.#spans = [];
      this.stops = [0];
      return;
    }
    const lead = lanes[0]!.offsetTop;
    this.#spans = lanes.map((lane) => ({
      top: lane.offsetTop - lead,
      bottom: lane.offsetTop - lead + lane.offsetHeight,
    }));
    const content = this.#spans.at(-1)!.bottom;
    const padTop = parseFloat(style.paddingTop) || 0;
    const padBottom = parseFloat(style.paddingBottom) || 0;
    // Whether there is a tour is asked of the deck's whole height, and its
    // pages are cut to the height the pager leaves. The pager's room is
    // padding the deck takes only while touring, so asking the first question
    // of the padded height would turn the tour on and off as it came and went.
    const fits = content <= node.clientHeight - padTop * 2 + 1;
    const was = this.active;
    this.#view = node.clientHeight - padTop - padBottom;
    const next = fits ? [0] : stops(this.#spans, this.#view, content);
    if (next.length !== this.stops.length || next.some((stop, i) => stop !== this.stops[i]))
      this.stops = next;
    if (this.active !== was) {
      this.#restart();
      // The padding has changed with the tour; measure again with it.
      requestAnimationFrame(() => this.#measure());
    }
    this.#place();
  }

  #place(): void {
    const node = this.#node;
    if (!node) return;
    this.page = stopAt(this.stops, node.scrollTop);
    this.label = tourLabel(shown(this.#spans, node.scrollTop, this.#view), this.#spans.length);
  }

  #restart(): void {
    this.#elapsed = 0;
    this.progress = 0;
  }

  #tick(): void {
    const node = this.#node;
    if (!node || !this.active || this.held || document.hidden) return;
    this.#elapsed += TICK_MS;
    if (this.#elapsed < DWELL_MS) {
      this.progress = this.#elapsed / DWELL_MS;
      return;
    }
    this.#restart();
    const next = (stopAt(this.stops, node.scrollTop) + 1) % this.stops.length;
    const calm = matchMedia('(prefers-reduced-motion: reduce)').matches;
    node.scrollTo({ top: this.stops[next]!, behavior: calm ? 'auto' : 'smooth' });
  }
}
