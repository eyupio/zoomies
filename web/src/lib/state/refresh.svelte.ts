/**
 * The one refresh: what the button in the page header does, and what `R` does.
 *
 * Zoomies stays current on its own -- a single SSE stream feeds the fleet cache
 * and the grids refetch off it -- so this is not how the screen keeps up. It is
 * for the moments the stream cannot answer for itself: a laptop opened after an
 * hour shut, a change somebody else made through the API, a controller that was
 * restarting when the last reconcile went out. Being able to ask costs one
 * request and settles the question; not being able to ask is what makes an
 * operator reload the whole application instead.
 *
 * A page says what refreshing means for it by rendering the control, and the
 * control registers the handler here so the keyboard shortcut and the button
 * can never disagree about what they do. Pages that have nothing to fetch --
 * the wizards, the not-found page -- register nothing and show no button,
 * because a control that does nothing is worse than no control.
 *
 * One refresh at a time. A second press while the first is in flight joins it
 * rather than starting another; a page that fans out to five requests must not
 * be able to send fifteen because somebody pressed the button three times.
 */
import { toasts } from './toasts.svelte';

/** What a page does when it is asked to refresh. May be async; may throw. */
export type RefreshHandler = () => void | Promise<void>;

/**
 * The spin is held for at least this long.
 *
 * A refresh that answers in twenty milliseconds -- which is the common case
 * against a controller on the same machine -- would otherwise flick the icon
 * once and leave the operator genuinely unsure whether anything happened. The
 * delay is the feedback, not the work.
 */
const MIN_SPIN_MS = 420;

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

class Refresh {
  #handler = $state.raw<RefreshHandler | null>(null);
  #busy = $state(false);
  #at = $state<number | null>(null);
  /** Bumped once per completed refresh, for anything that announces them. */
  #count = $state(0);
  #running: Promise<void> | null = null;

  /** True when the page on screen has said what refreshing means for it. */
  get available(): boolean {
    return this.#handler !== null;
  }

  get busy(): boolean {
    return this.#busy;
  }

  /** When the last refresh finished, or null if this page has not had one. */
  get at(): number | null {
    return this.#at;
  }

  get count(): number {
    return this.#count;
  }

  /**
   * Register this page's handler. Returns the teardown; call it from an
   * `$effect`, which is also what makes leaving a page take its button away.
   */
  register(handler: RefreshHandler): () => void {
    this.#handler = handler;
    this.#at = null;
    this.#count = 0;
    return () => {
      if (this.#handler !== handler) return;
      this.#handler = null;
      this.#at = null;
      this.#count = 0;
    };
  }

  /** Refresh the page on screen. Safe to call when there is nothing to do. */
  run(): Promise<void> {
    if (this.#running) return this.#running;
    const handler = this.#handler;
    if (!handler) return Promise.resolve();
    this.#busy = true;
    const done = this.#cycle(handler).finally(() => {
      this.#busy = false;
      this.#running = null;
    });
    this.#running = done;
    return done;
  }

  async #cycle(handler: RefreshHandler): Promise<void> {
    const started = Date.now();
    try {
      await handler();
    } catch (cause) {
      // Most handlers report their own failures -- a grid renders its error,
      // the fleet cache keeps the last truth on screen. This is for the ones
      // that throw anyway, so a press is never silently swallowed.
      toasts.fromError(cause, 'This page could not be refreshed');
    } finally {
      const spent = Date.now() - started;
      if (spent < MIN_SPIN_MS) await sleep(MIN_SPIN_MS - spent);
      this.#at = Date.now();
      this.#count += 1;
    }
  }
}

/** The one refresh. Registered by whichever page is on screen. */
export const refresh = new Refresh();
