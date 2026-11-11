/**
 * Which table, if any, has the page to itself.
 *
 * The address says which grid was asked for (`?focus=<grid id>`, see
 * `lib/table-focus.ts`), but only the grid can say it is there: a pasted
 * address whose grid this page does not have must leave the page as it is,
 * not take the navigation away and show nothing in its place. So the grid
 * claims focus once it has marked the page around it, and the shell draws its
 * focus chrome only while a claim is held.
 *
 * The shell also says it is present. The component gallery renders the real
 * grid with no shell around it, and a Focus control there would hide every
 * other component with no bar to come back from.
 */
import { untrack } from 'svelte';

class TableFocus {
  #claimed = $state<string | null>(null);
  #grids = $state<string[]>([]);
  #shell = $state(false);

  /** The grid holding focus, or null when the page is drawn as usual. */
  get active(): string | null {
    return this.#claimed;
  }

  /** The grids on screen, in the order they mounted: `f` focuses the first. */
  get grids(): readonly string[] {
    return this.#grids;
  }

  /** Whether a shell that can draw the focus chrome is mounted. */
  get shell(): boolean {
    return this.#shell;
  }

  /** The shell is here. Returns the teardown. */
  provide(): () => void {
    this.#shell = true;
    return () => {
      this.#shell = false;
    };
  }

  /** A grid is on screen. Returns the teardown, for an `$effect`. */
  register(id: string): () => void {
    // Read untracked: this is called from a grid's effect, and an effect that
    // read the list it then writes would run itself again, forever.
    untrack(() => {
      this.#grids = [...this.#grids, id];
    });
    return () => {
      const at = this.#grids.indexOf(id);
      if (at >= 0) this.#grids = this.#grids.filter((_, index) => index !== at);
    };
  }

  /**
   * A grid has the page. The attribute on the root is what the stylesheet
   * hides the rest of the page by, so it is set and cleared with the claim.
   */
  claim(id: string): () => void {
    this.#claimed = id;
    document.documentElement.dataset.tableFocus = id;
    return () => {
      if (this.#claimed !== id) return;
      this.#claimed = null;
      delete document.documentElement.dataset.tableFocus;
    };
  }
}

export const tableFocus = new TableFocus();
