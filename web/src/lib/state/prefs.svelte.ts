/**
 * Per-operator preferences: whether the navigation is collapsed, which columns
 * a grid shows, how many rows it asks for, and which one-off notices have been
 * put away.
 *
 * Every localStorage access is wrapped, because private-browsing modes throw on
 * access rather than returning null, and a dashboard that will not boot in a
 * private window is a dashboard someone cannot demo.
 */

const PREFS_KEY = 'zoomies.prefs';
/** Read by the inline script in index.html before first paint. */
const NAV_KEY = 'zoomies.nav.collapsed';

export const storage = {
  get(key: string): string | null {
    try {
      return localStorage.getItem(key);
    } catch {
      return null;
    }
  },
  set(key: string, value: string): void {
    try {
      localStorage.setItem(key, value);
    } catch {
      // Nothing to do and nothing worth telling the operator: the session simply
      // does not persist.
    }
  },
  remove(key: string): void {
    try {
      localStorage.removeItem(key);
    } catch {
      /* as above */
    }
  },
};

export interface GridPrefs {
  /** Column ids the operator has hidden. Stored as the exception, so new columns appear. */
  hidden?: string[];
  pageSize?: number;
}

interface StoredPrefs {
  navCollapsed?: boolean;
  grids?: Record<string, GridPrefs>;
  /**
   * Whether the Overview's job panels also show jobs no runner of this fleet
   * touched. GitHub reports every job in the repositories an installation
   * covers, so on an organisation that also uses hosted or vendor runners the
   * majority of them are somebody else's. Off by default, because a panel
   * headed "what the fleet is running" should mean it.
   */
  otherRunners?: boolean;
  /**
   * Notices this browser has been told to stop showing. They are stored as a
   * list of ids rather than a flag per notice so a notice that is retired
   * leaves nothing behind, and they live here rather than on the server
   * because a nudge is one operator's business, not the fleet's.
   */
  dismissed?: string[];
}

/** The nav choice recorded under NAV_KEY, or undefined when none has been made. */
function navChoiceFromStorage(): boolean | undefined {
  const raw = storage.get(NAV_KEY);
  return raw === null ? undefined : raw === '1';
}

/**
 * Whether the window is the tablet width the guidelines collapse the nav at.
 *
 * Bounded at both ends. A phone has no sidebar to collapse -- it has a bar
 * along the bottom edge -- so recording "collapsed" for one is recording an
 * answer to a question that was never asked, and the same browser opened on a
 * desktop then starts with a nav the operator never chose to shrink.
 */
function tabletWidth(): boolean {
  try {
    return (
      typeof matchMedia === 'function' &&
      matchMedia('(min-width: 769px) and (max-width: 1180px)').matches
    );
  } catch {
    return false;
  }
}

function load(): StoredPrefs {
  const raw = storage.get(PREFS_KEY);
  if (!raw) return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    return parsed && typeof parsed === 'object' ? (parsed as StoredPrefs) : {};
  } catch {
    return {};
  }
}

/** The row counts a grid offers. */
export const PAGE_SIZES = [25, 50, 100, 200] as const;
export const DEFAULT_PAGE_SIZE = 50;

class Prefs {
  #navCollapsed = $state(false);
  #grids = $state<Record<string, GridPrefs>>({});
  #dismissed = $state<string[]>([]);
  #otherRunners = $state(false);

  constructor() {
    const stored = load();
    // An operator's choice, in either place it may have been recorded, wins.
    // With no choice made, a tablet-width window starts collapsed: the
    // guidelines promise icons-only navigation between 768 and 1180px, and the
    // inline script in index.html applies the same default before first paint.
    const chosen = stored.navCollapsed ?? navChoiceFromStorage();
    this.#navCollapsed = chosen ?? tabletWidth();
    this.#grids = stored.grids ?? {};
    this.#dismissed = stored.dismissed ?? [];
    this.#otherRunners = stored.otherRunners ?? false;
    this.#applyNav();
  }

  get navCollapsed(): boolean {
    return this.#navCollapsed;
  }

  set navCollapsed(value: boolean) {
    this.#navCollapsed = value;
    this.#applyNav();
    storage.set(NAV_KEY, value ? '1' : '0');
    this.#persist();
  }

  toggleNav(): void {
    this.navCollapsed = !this.#navCollapsed;
  }

  /**
   * Whether the Overview's job panels include jobs this fleet had no hand in.
   * One preference for both panels: an operator deciding what "the fleet" means
   * on that page means it for the whole page.
   */
  get otherRunners(): boolean {
    return this.#otherRunners;
  }

  set otherRunners(value: boolean) {
    this.#otherRunners = value;
    this.#persist();
  }

  /** Column ids this grid is hiding. */
  hiddenColumns(gridId: string): string[] {
    return this.#grids[gridId]?.hidden ?? [];
  }

  isColumnVisible(gridId: string, columnId: string): boolean {
    return !this.hiddenColumns(gridId).includes(columnId);
  }

  setColumnVisible(gridId: string, columnId: string, visible: boolean): void {
    const hidden = this.hiddenColumns(gridId).filter((id) => id !== columnId);
    this.setHiddenColumns(gridId, visible ? hidden : [...hidden, columnId]);
  }

  setHiddenColumns(gridId: string, hidden: string[]): void {
    this.#grids = { ...this.#grids, [gridId]: { ...this.#grids[gridId], hidden } };
    this.#persist();
  }

  pageSize(gridId: string, fallback = DEFAULT_PAGE_SIZE): number {
    return this.#grids[gridId]?.pageSize ?? fallback;
  }

  setPageSize(gridId: string, pageSize: number): void {
    this.#grids = { ...this.#grids, [gridId]: { ...this.#grids[gridId], pageSize } };
    this.#persist();
  }

  /** Whether this browser has put a one-off notice away. */
  isDismissed(notice: string): boolean {
    return this.#dismissed.includes(notice);
  }

  dismiss(notice: string): void {
    if (this.#dismissed.includes(notice)) return;
    this.#dismissed = [...this.#dismissed, notice];
    this.#persist();
  }

  #applyNav(): void {
    if (typeof document === 'undefined') return;
    if (this.#navCollapsed) document.documentElement.setAttribute('data-nav', 'collapsed');
    else document.documentElement.removeAttribute('data-nav');
  }

  #persist(): void {
    storage.set(
      PREFS_KEY,
      JSON.stringify({
        navCollapsed: this.#navCollapsed,
        grids: this.#grids,
        dismissed: this.#dismissed,
        otherRunners: this.#otherRunners,
      } satisfies StoredPrefs),
    );
  }
}

export const prefs = new Prefs();
