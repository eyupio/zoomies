/**
 * Per-operator preferences: whether the navigation is collapsed, which columns
 * a grid shows, how many rows it asks for, and which one-off notices have been
 * put away.
 *
 * Every localStorage access is wrapped, because private-browsing modes throw on
 * access rather than returning null, and a dashboard that will not boot in a
 * private window is a dashboard someone cannot demo.
 */

import { getOwnPreferences, replaceOwnPreferences } from '../api/client';
import type { PanelPreferences, TableLayoutPreference } from '../api/types';
import {
  DEFAULT_LAYOUT,
  OVERVIEW_PANELS,
  isDefaultLayout,
  readPanelLayout,
  type PanelLayout,
} from '../overview/panels';
import { bandSettingsDocument, readBandSettings } from '../bands/settings';
import {
  DEFAULT_STATUS_STYLE,
  isStatusStyle,
  resolveStatusStyle,
  type StatusStyle,
} from './status-style';
import { DEFAULT_ELI_STYLE, resolveEliStyle, type EliStyle } from './eli-style';

import {
  DEFAULT_LOGO_TREATMENT,
  isLogoTreatment,
  resolveLogoTreatment,
  type LogoTreatment,
} from './logo-treatment';
import { defaultBandSize, type BandPage, type BandSize } from '../bands/pages';
import { ALL_BAND_PAGES } from '../bands/scope';
import { DEFAULT_BAND_LOGO_SIZE, isBandLogoSize, type BandLogoSize } from '../bands/band-logo';
import { storage } from './storage';

const PREFS_KEY = 'zoomies.prefs';
const PREFS_ACCOUNT_KEY = 'zoomies.prefs.account';
/** Read by the inline script in index.html before first paint. */
const NAV_KEY = 'zoomies.nav.collapsed';

// Re-exported so the many modules that read it from here keep doing so; it lives
// apart because a page that wants only localStorage, such as the status page,
// must not have to take the API client this module imports along with it.
export { storage };

/**
 * A choice this browser has made, read back: the stored JSON while it is
 * still one of the things that choice can be, and the fallback otherwise. A
 * value left behind by an older build -- a window that no longer exists, a
 * figure since renamed -- is not a reason to open a panel with nothing in it.
 */
export function remembered<T>(key: string, fallback: T, valid: (v: unknown) => v is T): T {
  try {
    const raw = storage.get(key);
    if (raw === null) return fallback;
    const parsed: unknown = JSON.parse(raw);
    return valid(parsed) ? parsed : fallback;
  } catch {
    return fallback;
  }
}

/** Remember a choice the moment it is made. */
export function remember(key: string, value: unknown): void {
  storage.set(key, JSON.stringify(value));
}

export interface GridPrefs {
  /** Column ids the operator has hidden. Stored as the exception, so new columns appear. */
  hidden?: string[];
  /**
   * Columns that start hidden which the operator has switched on. The mirror of
   * `hidden`, and kept apart from it so that a column added later as hidden by
   * default stays hidden for an operator who had already saved a `hidden` list.
   */
  shown?: string[];
  pageSize?: number;
  /** Column widths in CSS pixels, keyed by the stable column id. */
  widths?: Record<string, number>;
  /**
   * Column ids in the operator's preferred order. New columns are appended,
   * so a saved layout does not make a later release's column disappear.
   */
  order?: string[];
  /**
   * How this grid lays a row out on a phone, when the operator has said. Absent
   * means the default below, so changing that default moves every grid nobody
   * has decided for.
   */
  view?: GridView;
}

/**
 * How a grid lays a row out on a phone.
 *
 * `rows` keeps the table: one line per row, the columns at the widths they
 * declare, and the frame scrolling sideways to reach the ones that do not fit.
 * It is the same shape every wider window shows, so a fleet looks the same on a
 * phone as on the desk it was learned at, and scanning five runners for the one
 * that is different is what it is good at. `cards` gives each row a card and
 * each value a line of its own, which is how ten columns are read in 360 pixels
 * with nothing truncated.
 *
 * Rows is the default at every width, which is what makes it one product rather
 * than two; the operator who would rather read down says so, per grid or for
 * all of them, and is remembered.
 */
export const GRID_VIEWS = ['cards', 'rows'] as const;
export type GridView = (typeof GRID_VIEWS)[number];
export const DEFAULT_GRID_VIEW: GridView = 'rows';

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
  /**
   * How far back the Overview's activity matrix looks. An operator who
   * widens it to the year should find it that way tomorrow morning; with no
   * choice recorded it starts at today, the range the fleet is running in.
   */
  activityRange?: ActivityRangeKey;
  /**
   * Which of the Overview feed's categories this browser shows, where a choice
   * has been made. Only the choices are stored, never the defaults: a category
   * added by a later release then arrives with the default its author gave it
   * rather than switched off by a preference written before it existed.
   */
  feed?: Record<string, boolean>;
  /**
   * The phone layout a grid takes when the operator has not chosen one for it
   * itself. One setting for the whole app: an operator who prefers to scan rows
   * prefers it on every page, and the per-grid choice is for the page that is
   * the exception.
   */
  gridView?: GridView;
  /**
   * Whether runner and job status use the fleet's own dog-park vocabulary and
   * the animated avatar, rather than plain state names and a standard icon.
   * Still written, derived from `statusStyle`, so an older build opened in
   * the same browser keeps what this one chose; read only when `statusStyle`
   * is missing.
   */
  quirkyStatus?: boolean;
  /**
   * The vocabulary and illustration status is drawn with. Off -- plain words
   * and icons -- until an operator chooses otherwise in Appearance.
   */
  statusStyle?: StatusStyle;
  /**
   * How Eli answers: how long, and for whom. Kept here so the choice is made
   * once, and synced to the account so it holds on the phone as at the desk.
   */
  eliStyle?: Partial<EliStyle>;
  logoTreatment?: LogoTreatment;
  /**
   * Pages whose band this browser has switched off. The exception, as `hidden` is for
   * columns, so a band added later is on for everyone.
   */
  hiddenBands?: string[];
  /**
   * The size an operator chose for a page's band, where it is not the page's default.
   * Overrides only, as `hiddenBands` is, so a default that changes later reaches everyone
   * who never chose.
   */
  bandSizes?: Record<string, string>;
  /** Whether the dog is drawn in the band. Off by default, and the default for every page. */
  bandLogo?: boolean;
  /** How large the dog in the band is drawn; `auto` follows the band's size. */
  bandLogoSize?: BandLogoSize;
  /**
   * Pages whose dog is drawn or hidden against the default above. The exception,
   * as `hiddenBands` is, so a page keeps following the default until it is told
   * otherwise and a default that moves later reaches everyone who never chose.
   */
  bandLogoPages?: Record<string, boolean>;
  /** Pages whose dog size is not the default above. */
  bandLogoSizes?: Record<string, string>;
  /**
   * How this browser arranged each page's panels, by page, as the account
   * document carries it. Read forgivingly, like the bands: a panel this build
   * no longer has is dropped and one it gained is drawn in its default place.
   */
  panelLayouts?: Record<string, unknown>;
}

/** The quick ranges the activity matrix offers, as the buttons name them. */
export const ACTIVITY_RANGES = ['1d', '7d', '30d', '90d', '1y'] as const;
export type ActivityRangeKey = (typeof ACTIVITY_RANGES)[number];

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

import { PANEL_PAGES, type PanelPage } from './panel-pages';
import { panelLayoutsAfterSync } from './panel-sync';
export type { PanelPage } from './panel-pages';

/** The layouts a stored or received document describes, for the pages this build can arrange. */
function readPanelLayouts(raw: unknown): Partial<Record<PanelPage, PanelLayout>> {
  const out: Partial<Record<PanelPage, PanelLayout>> = {};
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return out;
  for (const page of PANEL_PAGES) {
    const entry = (raw as Record<string, unknown>)[page];
    if (entry === undefined) continue;
    const layout = readPanelLayout(entry, OVERVIEW_PANELS);
    if (!isDefaultLayout(layout)) out[page] = layout;
  }
  return out;
}

/** The document the account keeps: only the pages somebody arranged. */
function panelLayoutsDocument(layouts: Partial<Record<PanelPage, PanelLayout>>): PanelPreferences {
  const out: PanelPreferences = {};
  for (const page of PANEL_PAGES) {
    const layout = layouts[page];
    if (layout) out[page] = { order: [...layout.order], hidden: [...layout.hidden] };
  }
  return out;
}

/** The row counts a grid offers. */
export const PAGE_SIZES = [25, 50, 100, 200] as const;
export const DEFAULT_PAGE_SIZE = 50;

class Prefs {
  #navCollapsed = $state(false);
  #grids = $state<Record<string, GridPrefs>>({});
  #dismissed = $state<string[]>([]);
  #otherRunners = $state(false);
  #activityRange = $state<ActivityRangeKey>('1d');
  #gridView = $state<GridView>(DEFAULT_GRID_VIEW);
  #gridViewChosen = $state(false);
  #feed = $state<Record<string, boolean>>({});
  #statusStyle = $state<StatusStyle>(DEFAULT_STATUS_STYLE);
  #eliStyle = $state<EliStyle>(DEFAULT_ELI_STYLE);
  #logoTreatment = $state<LogoTreatment>(DEFAULT_LOGO_TREATMENT);
  #hiddenBands = $state<BandPage[]>([]);
  #bandSizes = $state<Partial<Record<BandPage, BandSize>>>({});
  #bandLogo = $state(false);
  #bandLogoSize = $state<BandLogoSize>(DEFAULT_BAND_LOGO_SIZE);
  #bandLogoPages = $state<Partial<Record<BandPage, boolean>>>({});
  #bandLogoSizes = $state<Partial<Record<BandPage, BandLogoSize>>>({});
  #panelLayouts = $state<Partial<Record<PanelPage, PanelLayout>>>({});
  #accountSync = false;
  // Counts the syncs begun and the accounts disconnected, so a read of the
  // account that is overtaken by a sign-out, or by another account's sign-in,
  // can be told from the current one and dropped.
  #syncGen = 0;
  #accountSave: ReturnType<typeof setTimeout> | null = null;

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
    // Validated, not trusted: the stored value is whatever this browser last
    // wrote, and a range this build no longer offers falls back to the day.
    this.#activityRange = (ACTIVITY_RANGES as readonly string[]).includes(
      stored.activityRange ?? '',
    )
      ? (stored.activityRange as ActivityRangeKey)
      : '1d';
    // Validated for the same reason, and for one more: a grid told to lay
    // itself out in a way this build does not have would render nothing.
    this.#gridViewChosen = (GRID_VIEWS as readonly string[]).includes(stored.gridView ?? '');
    this.#gridView = (GRID_VIEWS as readonly string[]).includes(stored.gridView ?? '')
      ? (stored.gridView as GridView)
      : DEFAULT_GRID_VIEW;
    this.#feed = stored.feed ?? {};
    this.#statusStyle = resolveStatusStyle(stored);
    this.#eliStyle = resolveEliStyle(stored.eliStyle);
    this.#logoTreatment = resolveLogoTreatment(stored.logoTreatment);
    // One reader for both copies: the browser's record and the account document
    // share a shape, and every word in it is something an older or newer build
    // may have written. A page or a cut this build does not know is dropped, not
    // carried into a control that cannot honour it.
    const bands = readBandSettings({
      hidden: stored.hiddenBands,
      sizes: stored.bandSizes,
      logo: stored.bandLogo,
      logo_size: stored.bandLogoSize,
      logo_pages: stored.bandLogoPages,
      logo_sizes: stored.bandLogoSizes,
    });
    this.#hiddenBands = bands.hidden;
    this.#bandSizes = bands.sizes;
    this.#bandLogo = bands.logo;
    this.#bandLogoSize = bands.logoSize;
    this.#bandLogoPages = bands.logoPages;
    this.#bandLogoSizes = bands.logoSizes;
    this.#panelLayouts = readPanelLayouts(stored.panelLayouts);
    this.#applyNav();
  }

  /** How this person arranged a page's panels; the default where they never did. */
  panelLayout(page: PanelPage): PanelLayout {
    return this.#panelLayouts[page] ?? DEFAULT_LAYOUT;
  }

  setPanelLayout(page: PanelPage, layout: PanelLayout): void {
    const next = { ...this.#panelLayouts };
    // The default is an absence, as a band's default cut is, so a default that
    // moves later reaches everyone who never arranged the page.
    if (isDefaultLayout(layout)) delete next[page];
    else next[page] = readPanelLayout(layout, OVERVIEW_PANELS);
    this.#panelLayouts = next;
    this.#persist();
  }

  resetPanelLayout(page: PanelPage): void {
    this.setPanelLayout(page, DEFAULT_LAYOUT);
  }

  /** How Eli answers this person. */
  get eliStyle(): EliStyle {
    return this.#eliStyle;
  }

  set eliStyle(value: EliStyle) {
    this.#eliStyle = resolveEliStyle(value);
    this.#persist();
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

  /** How far back the Overview's activity matrix looks. */
  get activityRange(): ActivityRangeKey {
    return this.#activityRange;
  }

  set activityRange(value: ActivityRangeKey) {
    this.#activityRange = value;
    this.#persist();
  }

  /**
   * The phone layout grids take unless one has been told otherwise. Set in
   * Settings; each grid's own toggle overrides it for that grid alone.
   */
  get gridViewChosen(): boolean {
    return this.#gridViewChosen;
  }

  get gridView(): GridView {
    return this.#gridView;
  }

  set gridView(value: GridView) {
    this.#gridView = value;
    this.#gridViewChosen = true;
    this.#persist();
  }

  /**
   * What this browser has decided about one of the Overview feed's categories,
   * or undefined where it has decided nothing. The default belongs to the
   * category rather than here, so a release that changes its mind about one
   * reaches every browser that has not overruled it.
   */
  feedChoice(category: string): boolean | undefined {
    return this.#feed[category];
  }

  setFeedChoice(category: string, on: boolean): void {
    this.#feed = { ...this.#feed, [category]: on };
    this.#persist();
  }

  /** A saved table/global choice wins over the caller's style default. */
  gridViewFor(gridId: string, fallback: GridView = DEFAULT_GRID_VIEW): GridView {
    return this.#grids[gridId]?.view ?? (this.#gridViewChosen ? this.#gridView : fallback);
  }

  setGridView(gridId: string, view: GridView): void {
    this.#grids = { ...this.#grids, [gridId]: { ...this.#grids[gridId], view } };
    this.#persist();
  }

  /**
   * Bring account-scoped table layouts into this browser after sign-in. The
   * first account seen migrates the older browser-only layouts; switching to a
   * different account never copies the previous person's layout into it.
   */
  async syncAccount(userID: string): Promise<void> {
    const gen = ++this.#syncGen;
    try {
      const remote = await getOwnPreferences();
      // Signing in does not wait for this answer, so it can arrive after the
      // person has signed out or somebody else has signed in. It describes the
      // previous account and must not be applied to this one.
      if (gen !== this.#syncGen) return;
      const layouts = remote.table_layouts ?? {};
      const previous = storage.get(PREFS_ACCOUNT_KEY);
      // How Eli answers is the account's choice where one has been made: a
      // person who chose at the desk finds it on the phone. With none made,
      // this browser's choice stands and is what the first save carries.
      if (remote.assistant) this.#eliStyle = resolveEliStyle(remote.assistant);
      /*
        The bands are the account's where it has a document: the cut chosen at
        the desk is the one the phone draws. When it has none, a browser that
        was last used by somebody else must not carry that person's bands into
        this account; a browser with no previous account keeps its choices and
        is what the first save carries.
      */
      // The panel layouts: the account's where it has a document, and
      // otherwise cleared on any browser an account has used, because the
      // document carries no `panels` at the default and that is what a reset
      // made in another browser looks like from here (panel-sync.ts).
      this.#panelLayouts = panelLayoutsAfterSync(
        this.#panelLayouts,
        remote.panels ? readPanelLayouts(remote.panels) : undefined,
        previous ?? undefined,
        userID,
      );
      if (remote.bands) {
        const bands = readBandSettings(remote.bands);
        this.#hiddenBands = bands.hidden;
        this.#bandSizes = bands.sizes;
        this.#bandLogo = bands.logo;
        this.#bandLogoSize = bands.logoSize;
        this.#bandLogoPages = bands.logoPages;
        this.#bandLogoSizes = bands.logoSizes;
      } else if (previous && previous !== userID) {
        const bands = readBandSettings({});
        this.#hiddenBands = bands.hidden;
        this.#bandSizes = bands.sizes;
        this.#bandLogo = bands.logo;
        this.#bandLogoSize = bands.logoSize;
        this.#bandLogoPages = bands.logoPages;
        this.#bandLogoSizes = bands.logoSizes;
      }
      if (Object.keys(layouts).length > 0) {
        // The server document is a replacement, not an overlay. Clearing the
        // local layout fields first means a reset made on another browser is a
        // reset here too, and no layout leaks between two accounts sharing one
        // browser. Browser-only choices such as visibility and page size stay.
        const grids: Record<string, GridPrefs> = Object.fromEntries(
          Object.entries(this.#grids).map(([gridID, grid]) => {
            const { widths: _widths, order: _order, ...rest } = grid;
            return [gridID, rest];
          }),
        );
        for (const [gridID, layout] of Object.entries(layouts)) {
          grids[gridID] = {
            ...grids[gridID],
            widths: layout.widths ? { ...layout.widths } : undefined,
            order: layout.order ? [...layout.order] : undefined,
          };
        }
        this.#grids = grids;
      } else if (previous) {
        this.#grids = Object.fromEntries(
          Object.entries(this.#grids).map(([gridID, grid]) => {
            const { widths: _widths, order: _order, ...rest } = grid;
            return [gridID, rest];
          }),
        );
      }
      storage.set(PREFS_ACCOUNT_KEY, userID);
      this.#accountSync = true;
      this.#persist();
      // An existing installation has browser layouts, or band choices from
      // before the account carried them, and no server document; the first
      // successful sync upgrades them without waiting for a gesture.
      const nothingToMigrate = Object.keys(layouts).length > 0 && remote.bands !== undefined;
      if (!previous && !nothingToMigrate) this.#scheduleAccountSave(0);
    } catch {
      // Browser preferences remain fully functional while the controller is
      // offline or while an older controller has no account endpoint.
      if (gen === this.#syncGen) this.#accountSync = false;
    }
  }

  disconnectAccount(): void {
    this.#syncGen++;
    this.#accountSync = false;
    if (this.#accountSave !== null) clearTimeout(this.#accountSave);
    this.#accountSave = null;
  }

  /** Column ids this grid is hiding. */
  hiddenColumns(gridId: string): string[] {
    return this.#grids[gridId]?.hidden ?? [];
  }

  /**
   * A column with `hiddenByDefault` is visible only once the operator has
   * switched it on; every other column is visible unless they hid it.
   */
  isColumnVisible(gridId: string, columnId: string, hiddenByDefault = false): boolean {
    if (hiddenByDefault) return (this.#grids[gridId]?.shown ?? []).includes(columnId);
    return !this.hiddenColumns(gridId).includes(columnId);
  }

  setColumnVisible(
    gridId: string,
    columnId: string,
    visible: boolean,
    hiddenByDefault = false,
  ): void {
    if (hiddenByDefault) {
      const shown = (this.#grids[gridId]?.shown ?? []).filter((id) => id !== columnId);
      this.#grids = {
        ...this.#grids,
        [gridId]: { ...this.#grids[gridId], shown: visible ? [...shown, columnId] : shown },
      };
      this.#persist();
      return;
    }
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

  /** A column's saved width, when the operator has resized it. */
  columnWidth(gridId: string, columnId: string): number | undefined {
    const value = this.#grids[gridId]?.widths?.[columnId];
    return typeof value === 'number' && Number.isFinite(value) && value > 0 ? value : undefined;
  }

  setColumnWidth(gridId: string, columnId: string, width: number): void {
    if (!Number.isFinite(width) || width <= 0) return;
    const current = this.#grids[gridId] ?? {};
    this.#grids = {
      ...this.#grids,
      [gridId]: {
        ...current,
        widths: { ...current.widths, [columnId]: Math.round(width) },
      },
    };
    this.#persist();
  }

  clearColumnWidth(gridId: string, columnId: string): void {
    const current = this.#grids[gridId];
    if (!current?.widths?.[columnId]) return;
    const widths = { ...current.widths };
    delete widths[columnId];
    this.#grids = { ...this.#grids, [gridId]: { ...current, widths } };
    this.#persist();
  }

  /**
   * Known columns in the saved order, followed by anything this build added.
   * Unknown ids are discarded rather than reserving invisible gaps forever.
   */
  columnOrder(gridId: string, columnIds: readonly string[]): string[] {
    const known = new Set(columnIds);
    const saved = (this.#grids[gridId]?.order ?? []).filter(
      (id, index, all) => known.has(id) && all.indexOf(id) === index,
    );
    return [...saved, ...columnIds.filter((id) => !saved.includes(id))];
  }

  setColumnOrder(gridId: string, order: readonly string[]): void {
    const current = this.#grids[gridId] ?? {};
    this.#grids = { ...this.#grids, [gridId]: { ...current, order: [...order] } };
    this.#persist();
  }

  resetColumnLayout(gridId: string): void {
    const current = this.#grids[gridId];
    if (!current) return;
    const { widths: _widths, order: _order, ...rest } = current;
    this.#grids = { ...this.#grids, [gridId]: rest };
    this.#persist();
  }

  /**
   * Whether runner and job status shows the fleet's dog-park vocabulary and
   * the animated avatar. Off swaps both for plain state names and a standard
   * icon, everywhere they appear.
   */
  get quirkyStatus(): boolean {
    return this.#statusStyle !== 'off';
  }

  set quirkyStatus(value: boolean) {
    this.#statusStyle = value ? (this.#statusStyle === 'off' ? 'cute' : this.#statusStyle) : 'off';
    this.#persist();
  }

  /** Whether this page's band is showing: on unless this browser has switched it off. */
  bandShown(page: BandPage): boolean {
    return !this.#hiddenBands.includes(page);
  }

  setBandShown(page: BandPage, shown: boolean): void {
    this.applyBandShown([page], shown);
  }

  /** Switch the bands on or off across the pages a change was scoped to. */
  applyBandShown(pages: readonly BandPage[], shown: boolean): void {
    const hidden: Record<string, boolean> = {};
    for (const page of this.#hiddenBands) hidden[page] = true;
    for (const page of pages) {
      if (shown) delete hidden[page];
      else hidden[page] = true;
    }
    // Kept in the order Appearance lists them, so the saved document is stable
    // and two browsers that made the same choice write the same bytes.
    this.#hiddenBands = ALL_BAND_PAGES.filter((page) => hidden[page]);
    this.#persist();
  }

  /** The size this page's band stands at: the operator's choice, else the page's default. */
  bandSize(page: BandPage): BandSize {
    return this.#bandSizes[page] ?? defaultBandSize();
  }

  setBandSize(page: BandPage, size: BandSize): void {
    this.applyBandSize([page], size);
  }

  /** Give one cut to every page a change was scoped to, in a single save. */
  applyBandSize(pages: readonly BandPage[], size: BandSize): void {
    const next = { ...this.#bandSizes };
    for (const page of pages) {
      // Choosing the default again is un-choosing, so the page follows it if it
      // ever moves.
      if (size === defaultBandSize()) delete next[page];
      else next[page] = size;
    }
    this.#bandSizes = next;
    this.#persist();
  }

  /** Whether this page's band draws the dog. */
  bandLogoShown(page: BandPage): boolean {
    return this.#bandLogoPages[page] ?? this.#bandLogo;
  }

  setBandLogoShown(page: BandPage, shown: boolean): void {
    this.applyBandLogoShown([page], shown);
  }

  applyBandLogoShown(pages: readonly BandPage[], shown: boolean): void {
    const next = { ...this.#bandLogoPages };
    for (const page of pages) {
      if (shown === this.#bandLogo) delete next[page];
      else next[page] = shown;
    }
    this.#bandLogoPages = next;
    this.#persist();
  }

  /** The size of this page's dog, which follows the default until it is told otherwise. */
  bandLogoSizeFor(page: BandPage): BandLogoSize {
    return this.#bandLogoSizes[page] ?? this.#bandLogoSize;
  }

  setBandLogoSizeFor(page: BandPage, size: BandLogoSize): void {
    this.applyBandLogoSize([page], size);
  }

  applyBandLogoSize(pages: readonly BandPage[], size: BandLogoSize): void {
    const next = { ...this.#bandLogoSizes };
    for (const page of pages) {
      if (size === this.#bandLogoSize) delete next[page];
      else next[page] = size;
    }
    this.#bandLogoSizes = next;
    this.#persist();
  }

  /**
   * Both halves of the dog in one save: the band's own control asks whether it
   * is drawn and how large, and applying that as two writes would put a
   * half-made choice on the account and repaint the band twice.
   */
  applyBandLogo(pages: readonly BandPage[], logo: { shown: boolean; size: BandLogoSize }): void {
    const shown = { ...this.#bandLogoPages };
    const sizes = { ...this.#bandLogoSizes };
    for (const page of pages) {
      if (logo.shown === this.#bandLogo) delete shown[page];
      else shown[page] = logo.shown;
      if (logo.size === this.#bandLogoSize) delete sizes[page];
      else sizes[page] = logo.size;
    }
    this.#bandLogoPages = shown;
    this.#bandLogoSizes = sizes;
    this.#persist();
  }

  /** Whether this page's dog differs from the default set in Appearance. */
  bandLogoCustom(page: BandPage): boolean {
    return page in this.#bandLogoPages || page in this.#bandLogoSizes;
  }

  /** Put one page's dog back on the default set in Appearance. */
  clearBandLogoOverride(page: BandPage): void {
    const shown = { ...this.#bandLogoPages };
    const sizes = { ...this.#bandLogoSizes };
    delete shown[page];
    delete sizes[page];
    this.#bandLogoPages = shown;
    this.#bandLogoSizes = sizes;
    this.#persist();
  }

  /**
   * The dog's default. Setting it is a choice for every page, not a new default
   * for pages that have not chosen: the switch says "Show the logo in the
   * band", and a page that quietly kept the old answer would make it a lie.
   */
  get bandLogo(): boolean {
    return this.#bandLogo;
  }

  set bandLogo(value: boolean) {
    this.#bandLogo = value;
    this.#bandLogoPages = {};
    this.#persist();
  }

  /** How large the dog in the band is drawn; `auto` follows the band's size. */
  get bandLogoSize(): BandLogoSize {
    return this.#bandLogoSize;
  }

  set bandLogoSize(value: BandLogoSize) {
    if (!isBandLogoSize(value)) return;
    this.#bandLogoSize = value;
    this.#bandLogoSizes = {};
    this.#persist();
  }

  get logoTreatment(): LogoTreatment {
    return this.#logoTreatment;
  }

  set logoTreatment(value: LogoTreatment) {
    if (!isLogoTreatment(value)) return;
    this.#logoTreatment = value;
    this.#persist();
  }

  get statusStyle(): StatusStyle {
    return this.#statusStyle;
  }

  set statusStyle(value: StatusStyle) {
    if (!isStatusStyle(value)) return;
    this.#statusStyle = value;
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

  /** Whether choices are being kept on the signed-in account as well as in this browser. */
  get accountSynced(): boolean {
    return this.#accountSync;
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
        activityRange: this.#activityRange,
        // Saving a logo or vocabulary must not freeze a previously unchosen grid default.
        gridView: this.#gridViewChosen ? this.#gridView : undefined,
        feed: this.#feed,
        quirkyStatus: this.quirkyStatus,
        statusStyle: this.#statusStyle,
        eliStyle: this.#eliStyle,
        logoTreatment: this.#logoTreatment,
        hiddenBands: this.#hiddenBands,
        bandSizes: this.#bandSizes,
        bandLogo: this.#bandLogo,
        bandLogoSize: this.#bandLogoSize,
        bandLogoPages: this.#bandLogoPages,
        bandLogoSizes: this.#bandLogoSizes,
        panelLayouts: this.#panelLayouts,
      } satisfies StoredPrefs),
    );
    this.#scheduleAccountSave();
  }

  #scheduleAccountSave(wait = 350): void {
    if (!this.#accountSync) return;
    if (this.#accountSave !== null) clearTimeout(this.#accountSave);
    this.#accountSave = setTimeout(() => {
      this.#accountSave = null;
      const tableLayouts: Record<string, TableLayoutPreference> = {};
      for (const [gridID, grid] of Object.entries(this.#grids)) {
        if (!grid.widths && !grid.order) continue;
        tableLayouts[gridID] = {
          ...(grid.widths ? { widths: grid.widths } : {}),
          ...(grid.order ? { order: grid.order } : {}),
        };
      }
      /*
        Every band choice goes with the same request that carries the layouts.
        The document is a replacement, so leaving the bands out of it would
        delete them on the account the moment somebody dragged a column.
      */
      void replaceOwnPreferences({
        table_layouts: tableLayouts,
        assistant: this.#eliStyle,
        bands: bandSettingsDocument({
          hidden: this.#hiddenBands,
          sizes: this.#bandSizes,
          logo: this.#bandLogo,
          logoSize: this.#bandLogoSize,
          logoPages: this.#bandLogoPages,
          logoSizes: this.#bandLogoSizes,
        }),
        panels: panelLayoutsDocument(this.#panelLayouts),
      }).catch(() => {
        // The local copy is authoritative until the next successful sign-in;
        // a layout gesture should never turn into an error toast.
      });
    }, wait);
  }
}

export const prefs = new Prefs();
