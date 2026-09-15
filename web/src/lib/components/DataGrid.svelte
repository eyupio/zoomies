<script module lang="ts">
  import type { Snippet } from 'svelte';
  import type { LucideIcon } from '@lucide/svelte';

  /** What the grid asks the server for. It lives in the URL, so a view is shareable. */
  export interface GridQuery {
    limit: number;
    offset: number;
    sort: string;
    order: 'asc' | 'desc';
  }

  /** What the fetcher gives back: this page of rows, and how many there are in total. */
  export interface GridPage<T> {
    items: T[];
    total: number;
  }

  export interface GridColumn<T> {
    /** Also the `sort` value sent to the API, and the key column visibility persists under. */
    id: string;
    header: string;
    /** Plain text for the cell. Used when no `cell` snippet is given. */
    value?: (row: T) => string;
    /** Rich cell content -- a badge, a link, a duration. */
    cell?: Snippet<[T]>;
    sortable?: boolean;
    /** Whether the operator may hide it. Defaults to true. */
    hideable?: boolean;
    /**
     * How much of the frame this column is worth, as a `rem` or `px` measure.
     *
     * It is a share rather than a measure: the grid never scrolls sideways, so
     * the columns divide whatever width there is and a declared width says how
     * generously. A column that leaves it out gets an ordinary share.
     */
    width?: string;
    /**
     * Take `width` as a measure rather than a share.
     *
     * A column of controls cannot be narrower than the controls in it -- a
     * button squeezed to two thirds of itself is a button half outside its
     * column -- so it takes its width outright and the columns of text divide
     * whatever is left.
     */
    fixed?: boolean;
    align?: 'start' | 'end';
    /**
     * Whether this column's cell may paint outside its column.
     *
     * A cell is one line, cut off at its column's edge, so that one long
     * runner name cannot make every row in the table three lines tall. The
     * exception is a cell that opens something anchored inside it -- a menu, a
     * popover -- which has to be let out or it is cut off at the row's edge.
     */
    overflows?: boolean;
    /**
     * `wide` columns are the ones a narrow desktop does without.
     *
     * Twelve columns in the 660 pixels a 768px window leaves is 55 pixels
     * each, which fits and says nothing. The columns that answer the page's
     * own question stay; the rest wait for the room to show them properly, and
     * come back in full on a phone, where every row is a card and there is a
     * whole line for each of them.
     */
    priority?: 'wide';
  }

  export interface BulkAction {
    id: string;
    label: string;
    icon?: LucideIcon;
    danger?: boolean;
    /**
     * This action opens something rather than doing something to a set, so it
     * only makes sense for one row. It is offered whatever is selected, and
     * disabled with a reason while more than one row is ticked -- an operator
     * who ticked a row to act on it should not have to work out that the
     * button they want is on a menu somewhere else.
     */
    single?: boolean;
    /**
     * Act on the selected ids. Once it settles the selection is cleared, so
     * the same rows cannot be acted on twice by accident; resolve `false` to
     * keep it -- a confirmation the operator cancelled, say.
     */
    run: (ids: string[]) => void | boolean | Promise<void | boolean>;
  }
</script>

<!--
  The grid.

  TanStack Table's core builds the column and row models and owns column
  visibility; every pixel of markup is ours, because a table is where this
  product's density and keyboard behaviour actually live.

  Everything is server side: the fetcher receives the query, returns the page
  and the total, and the query itself lives in the URL so a filtered view can be
  pasted to a colleague. Sorting, paging and filters therefore never touch the
  rows in the browser.

  Keyboard: up and down move between rows, Enter opens, Space selects,
  Shift with the arrows extends the selection, Home and End jump to the ends.
-->
<script lang="ts" generics="T extends Record<string, unknown>">
  import { untrack } from 'svelte';
  import { Columns3 } from '@lucide/svelte';
  import {
    columnVisibilityFeature,
    createTable,
    tableFeatures,
    type ColumnDef,
  } from '@tanstack/svelte-table';
  import { layers } from '../keys';
  import { prefs, type GridView } from '../state/prefs.svelte';
  import { viewport } from '../state/viewport.svelte';
  import { router } from '../router';
  import Button from './Button.svelte';
  import Checkbox from './Checkbox.svelte';
  import EmptyState from './EmptyState.svelte';
  import ErrorState from './ErrorState.svelte';
  import Pagination from './Pagination.svelte';
  import Segmented from './Segmented.svelte';
  import Skeleton from './Skeleton.svelte';

  interface Props {
    /** Stable id. Column visibility and page size persist under it. */
    gridId: string;
    /** Names the table for assistive technology: "Runners". */
    label: string;
    columns: ReadonlyArray<GridColumn<T>>;
    /** Server-side fetch. Called whenever the query, the filters or `liveKey` change. */
    fetcher: (query: GridQuery, signal: AbortSignal) => Promise<GridPage<T>>;
    rowId: (row: T) => string;
    /** Anything serialisable. A change refetches from the first page. */
    filters?: unknown;
    defaultSort?: string;
    defaultOrder?: 'asc' | 'desc';
    selectable?: boolean;
    bulkActions?: ReadonlyArray<BulkAction>;
    /** What Enter and a click on a row do. */
    onopen?: (row: T) => void;
    /** Called with every page as it lands, for warming the fleet cache. */
    onrows?: (rows: T[], total: number) => void;
    /**
     * Bump to refetch. Pages pass the fleet's `shape`, so SSE keeps the grid
     * live without a round trip for every heartbeat; the grid itself refreshes
     * at most about once a second however fast the key moves.
     */
    liveKey?: number;
    noun?: string;
    emptyTitle?: string;
    emptyDescription?: string;
    /** The action that fills an empty grid. */
    emptyAction?: Snippet;
    class?: string;
  }

  let {
    gridId,
    label,
    columns,
    fetcher,
    rowId,
    filters,
    defaultSort = '',
    defaultOrder = 'desc',
    selectable = false,
    bulkActions = [],
    onopen,
    onrows,
    liveKey = 0,
    noun = 'rows',
    emptyTitle = `No ${noun} yet`,
    emptyDescription,
    emptyAction,
    class: className = '',
  }: Props = $props();

  /* -- query state, held in the URL --------------------------------------- */

  const limit = $derived(router.paramNumber('limit', prefs.pageSize(gridId)));
  const offset = $derived(router.paramNumber('offset', 0));
  const sort = $derived(router.param('sort', defaultSort));
  const order = $derived<'asc' | 'desc'>(
    router.param('order', defaultOrder) === 'asc' ? 'asc' : 'desc',
  );

  const request = $derived({
    query: { limit, offset, sort, order } satisfies GridQuery,
    filterKey: JSON.stringify(filters ?? null),
  });

  let rows = $state<T[]>([]);
  let total = $state(0);
  let loading = $state(true);
  /**
   * True once the first request has answered, either way. The skeleton is
   * for the page that has nothing to show yet; a later fetch -- a live
   * refresh, a filter change -- keeps whatever is on screen until its answer
   * lands, because a grid that flashes back to grey bars every time an event
   * arrives is a grid nobody can read.
   */
  let settled = $state(false);
  let error = $state<unknown>(null);
  let lastFilterKey = '';

  /** A short debounce so a burst of changes costs one request, not forty. */
  const DEBOUNCE_MS = 120;
  /**
   * How often a live refresh may run, and how long the grid will go without
   * one while frames keep arriving. A trailing debounce alone was reset by
   * every frame, so a stream busier than one frame per 120ms -- a crash loop,
   * a busy fleet's heartbeats -- meant the timer never fired and the page
   * never refreshed; below that rate it was a round trip per frame.
   */
  const LIVE_EVERY_MS = 1000;

  /** The request in flight, so a newer one can end it. */
  let inflight: AbortController | null = null;
  /** The query the rows on screen answer, for a live refresh to repeat. */
  let current: GridQuery | null = null;
  let liveTimer: ReturnType<typeof setTimeout> | null = null;
  let lastLiveAt = 0;
  /** A live change landed while a fetch was in flight: refresh once it lands. */
  let liveAgain = false;

  function fetchNow(query: GridQuery): void {
    inflight?.abort();
    const controller = new AbortController();
    inflight = controller;
    loading = true;
    void (async () => {
      try {
        const page = await fetcher(query, controller.signal);
        if (controller.signal.aborted) return;
        rows = page.items;
        total = page.total;
        error = null;
        onrows?.(page.items, page.total);
      } catch (cause) {
        if (controller.signal.aborted) return;
        if (cause instanceof DOMException && cause.name === 'AbortError') return;
        error = cause;
      } finally {
        if (!controller.signal.aborted) {
          loading = false;
          settled = true;
          if (inflight === controller) inflight = null;
          if (liveAgain) {
            liveAgain = false;
            scheduleLive();
          }
        }
      }
    })();
  }

  /**
   * Refresh for a live change: soon, but not more than once a second, and
   * never by abandoning a request that is already on its way -- under a
   * sustained stream that would be a page that never finishes loading.
   */
  function scheduleLive(): void {
    if (liveTimer !== null) return;
    const wait = Math.max(DEBOUNCE_MS, lastLiveAt + LIVE_EVERY_MS - Date.now());
    liveTimer = setTimeout(() => {
      liveTimer = null;
      if (!current) return;
      if (inflight) {
        liveAgain = true;
        return;
      }
      lastLiveAt = Date.now();
      fetchNow(current);
    }, wait);
  }

  $effect(() => {
    const { query, filterKey } = request;
    // A filter change means the operator is looking at a different set: page
    // one, and nothing selected from the old set still ticked out of sight.
    if (lastFilterKey && lastFilterKey !== filterKey) {
      selected = [];
      if (query.offset !== 0) {
        lastFilterKey = filterKey;
        router.setQuery({ offset: null });
        return;
      }
    }
    lastFilterKey = filterKey;
    current = query;

    // A live refresh waiting for the old query would answer the wrong
    // question; the fetch below covers it.
    if (liveTimer !== null) {
      clearTimeout(liveTimer);
      liveTimer = null;
    }
    loading = true;
    const timer = setTimeout(() => fetchNow(query), DEBOUNCE_MS);
    return () => {
      clearTimeout(timer);
      // The rows on screen must never answer a query the operator has left.
      inflight?.abort();
      inflight = null;
    };
  });

  // The first value of liveKey is the one the initial fetch already answers;
  // every later change is a live event worth a refresh.
  let liveSeen = false;
  $effect(() => {
    void liveKey;
    untrack(() => {
      if (!liveSeen) {
        liveSeen = true;
        return;
      }
      scheduleLive();
    });
  });

  $effect(() => () => {
    if (liveTimer !== null) clearTimeout(liveTimer);
    inflight?.abort();
  });

  /* -- the table model ------------------------------------------------------ */

  const features = tableFeatures({ columnVisibilityFeature });

  // Read once on mount: the operator's stored choice is the starting point, and
  // it is theirs to change from there.
  let visibility = $state<Record<string, boolean>>(
    untrack(() =>
      Object.fromEntries(columns.map((c) => [c.id, prefs.isColumnVisible(gridId, c.id)])),
    ),
  );

  const definitions = $derived(
    columns.map(
      (column) =>
        ({
          id: column.id,
          header: column.header,
          accessorFn: (row: T) => (column.value ? column.value(row) : ''),
        }) as ColumnDef<typeof features, T, unknown>,
    ),
  );

  const table = createTable<typeof features, T>({
    features,
    get data() {
      return rows;
    },
    get columns() {
      return definitions;
    },
    getRowId: (row: T) => rowId(row),
    state: {
      get columnVisibility() {
        return visibility;
      },
    },
  });

  /* -- how wide the columns are ---------------------------------------------
   * The table never scrolls sideways, so the frame's width is the whole budget
   * and the columns divide it. A declared width is a share of that budget
   * rather than a measure, except where a column says it is `fixed`, and the
   * arithmetic is done here rather than in CSS because a percentage inside a
   * `calc()` does not resolve for a column of a fixed-layout table -- Chrome
   * quietly falls back to dividing the width equally, which throws away every
   * proportion the pages asked for.
   * ---------------------------------------------------------------------- */

  let frame = $state<HTMLDivElement | null>(null);
  let frameWidth = $state(0);
  /** What a rem is worth here, read once: the root font size is not ours to assume. */
  let remPx = $state(16);

  $effect(() => {
    const element = frame;
    if (!element) return;
    /*
      Re-read with every measurement rather than once: a `fixed` column is
      emitted in absolute pixels, so a root font size that changed under the
      page -- a browser's own font setting, which Chrome applies live -- would
      leave those columns sized against a rem that no longer exists. A font
      change resizes the frame too, so the observer is already firing.
    */
    const readRem = (): void => {
      const root = parseFloat(getComputedStyle(document.documentElement).fontSize);
      if (Number.isFinite(root) && root > 0) remPx = root;
    };
    readRem();
    /*
      Measured in the observer, acted on a frame later, and coalesced: a resize
      notification is delivered inside the browser's own rendering steps, and
      writing state there lays the frame out a second time -- which is both the
      "ResizeObserver loop" warning and a frame of work charged to whatever
      else was queued. A frame's delay in a column width nobody can see is the
      cheaper side of that trade.
    */
    let queued = 0;
    const measure = (): void => {
      queued = 0;
      readRem();
      frameWidth = element.clientWidth;
    };
    const observer = new ResizeObserver(() => {
      if (queued === 0) queued = requestAnimationFrame(measure);
    });
    observer.observe(element);
    frameWidth = element.clientWidth;
    return () => {
      observer.disconnect();
      if (queued !== 0) cancelAnimationFrame(queued);
    };
  });

  /** What a column with nothing to say about its width is worth. */
  const DEFAULT_SHARE_REM = 8;
  /** The tick column, which is a control rather than a column of data. */
  const PICK_SHARE_REM = 2.5;

  /**
   * A declared width in pixels.
   *
   * Anything the browser could be trusted to parse but this cannot -- a
   * `clamp()`, a percentage -- falls back to an ordinary share rather than to
   * a NaN, which would take the whole table's arithmetic with it.
   */
  function share(width: string | undefined, rem: number): number {
    const measure = width ? /^([\d.]+)(rem|px)$/.exec(width.trim()) : null;
    const value = measure ? Number(measure[1]) : Number.NaN;
    if (!Number.isFinite(value) || value <= 0) return DEFAULT_SHARE_REM * rem;
    return measure![2] === 'px' ? value : value * rem;
  }

  /*
   * Which band the window is in, from the app's one listener rather than two
   * more per grid. Asked of script at all -- rather than left to a media query,
   * where a responsive rule belongs -- because the column widths are worked out
   * here: a column hidden by CSS would still have been given its share of the
   * frame, and the columns left would be narrower than the space they actually
   * have. `viewport.phone` is the card threshold; below it there is no band,
   * because every column has a line of its own again.
   */
  const narrowDesktop = $derived(viewport.narrow);
  /*
    Which of the two phone layouts this grid is in, and the operator's answer
    wins: `cards` gives every value a line of its own, `rows` keeps the table
    and lets the frame scroll to the columns that do not fit. The choice is
    only asked below the phone threshold, because above it there is room for
    the table and the cards never appear.
  */
  const view = $derived<GridView>(prefs.gridViewFor(gridId));
  const cards = $derived(viewport.phone && view === 'cards');
  /*
    The table on a phone, which is what a grid does unless it has been told
    otherwise: the same shape as on a desktop, so the page an operator learned
    at a desk is the page they get on a phone. The columns cannot divide 360
    pixels between them and still say anything, so here alone a declared width
    is taken as the measure it names and the frame scrolls sideways to reach
    the rest -- the trade this layout makes, and the reason Cards is one press
    away. The page around it still does not scroll: the frame clips.
  */
  const phoneRows = $derived(viewport.phone && view === 'rows');

  const byId = $derived(new Map(columns.map((c) => [c.id, c])));
  const visibleColumns = $derived(
    table
      .getVisibleLeafColumns()
      .map((c) => byId.get(c.id))
      .filter((c): c is GridColumn<T> => Boolean(c))
      .filter((c) => !(narrowDesktop && c.priority === 'wide')),
  );
  const modelRows = $derived(table.getRowModel().rows);

  /**
   * Every visible column's width, in the proportions the declared widths ask
   * for and adding up to exactly the frame.
   *
   * Under a fixed layout a declared width is taken literally, so nine columns
   * that each know what they are worth add up to a table wider than the window
   * -- which is the sideways scroll this grid is not supposed to have. Sharing
   * the frame out instead keeps the same relative weights, fills the width
   * exactly at any size, and means no column is ever squeezed to nothing while
   * another keeps its full measure.
   */
  const columnWidths = $derived.by(() => {
    const none = { pick: undefined, columns: visibleColumns.map(() => undefined) };
    // A card has no columns to divide, and a width left on the heading strip
    // would squeeze the sort controls into the shapes of columns that are no
    // longer there.
    if (cards || frameWidth <= 0) return none;

    const wanted = visibleColumns.map((column) => share(column.width, remPx));
    // The tick is a control like any other, so it is measured rather than shared.
    const pick = selectable ? PICK_SHARE_REM * remPx : 0;
    /*
      Scrolling, so there is no frame to divide: every column gets exactly what
      it asked for. The widths are still emitted rather than left to the
      automatic layout, or the longest repository name on the page would decide
      how wide the table is and the ellipsis could never fire.
    */
    if (phoneRows) {
      return {
        pick: selectable ? `${pick}px` : undefined,
        columns: wanted.map((value) => `${value}px`),
      };
    }
    const measured =
      pick +
      visibleColumns.reduce((sum, column, index) => sum + (column.fixed ? wanted[index]! : 0), 0);
    const shared = visibleColumns.reduce(
      (sum, column, index) => sum + (column.fixed ? 0 : wanted[index]!),
      0,
    );
    if (shared <= 0) return none;

    // What the measured columns leave, divided in the proportions the rest
    // asked for. Never below zero: a frame narrower than the controls in it is
    // a frame the phone's card layout has already taken over.
    const spare = Math.max(0, frameWidth - measured);
    const of = (value: number) => `${((value / shared) * spare).toFixed(2)}px`;
    return {
      pick: selectable ? `${pick}px` : undefined,
      columns: visibleColumns.map((column, index) =>
        column.fixed ? `${wanted[index]!}px` : of(wanted[index]!),
      ),
    };
  });

  /**
   * How wide the table is when it scrolls, so it is not squeezed back into the
   * frame. Only the phone's row layout has one: everywhere else the table is
   * exactly the frame, which is the point of dividing the width.
   */
  const tableWidth = $derived.by(() => {
    if (!phoneRows) return undefined;
    const pick = selectable ? PICK_SHARE_REM * remPx : 0;
    const total = visibleColumns.reduce((sum, column) => sum + share(column.width, remPx), pick);
    return total > 0 ? `${total.toFixed(2)}px` : undefined;
  });

  function toggleColumn(id: string, visible: boolean): void {
    visibility = { ...visibility, [id]: visible };
    prefs.setColumnVisible(gridId, id, visible);
  }

  /* -- sorting and paging --------------------------------------------------- */

  function toggleSort(column: GridColumn<T>): void {
    if (!column.sortable) return;
    const next = sort === column.id && order === 'desc' ? 'asc' : 'desc';
    router.setQuery({ sort: column.id, order: next, offset: null });
  }

  function goTo(nextOffset: number): void {
    router.setQuery({ offset: nextOffset || null });
  }

  function setLimit(nextLimit: number): void {
    prefs.setPageSize(gridId, nextLimit);
    router.setQuery({ limit: nextLimit, offset: null });
  }

  /* -- selection ------------------------------------------------------------- */

  let selected = $state<string[]>([]);
  let anchor = $state(-1);
  let focused = $state(-1);
  let body = $state<HTMLTableSectionElement | null>(null);

  const pageIds = $derived(modelRows.map((r) => r.id));
  const allSelected = $derived(pageIds.length > 0 && pageIds.every((id) => selected.includes(id)));
  const someSelected = $derived(selected.length > 0 && !allSelected);

  function isSelected(id: string): boolean {
    return selected.includes(id);
  }

  function setSelected(id: string, on: boolean): void {
    selected = on ? [...new Set([...selected, id])] : selected.filter((s) => s !== id);
  }

  function toggleAll(): void {
    selected = allSelected
      ? selected.filter((id) => !pageIds.includes(id))
      : [...new Set([...selected, ...pageIds])];
  }

  async function runBulk(action: BulkAction): Promise<void> {
    const ids = selected;
    if ((await action.run(ids)) !== false) selected = [];
  }

  function selectRange(from: number, to: number): void {
    const [lo, hi] = from < to ? [from, to] : [to, from];
    const ids = pageIds.slice(lo, hi + 1);
    selected = [...new Set([...selected, ...ids])];
  }

  function focusRow(index: number): void {
    if (index < 0 || index >= modelRows.length) return;
    focused = index;
    queueMicrotask(() => body?.querySelector<HTMLElement>(`[data-row="${index}"]`)?.focus());
  }

  function openRow(index: number): void {
    const row = modelRows[index];
    if (row && onopen) onopen(row.original);
  }

  function onBodyKeydown(event: KeyboardEvent): void {
    // Only the row itself. A link, a copy button or a menu inside a cell has
    // its own meaning for Enter, Space and the arrows, and swallowing them
    // here opened the row instead of the link and moved the grid's focus
    // while a menu was open.
    if (!(event.target instanceof HTMLTableRowElement)) return;
    const index = focused;
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault();
        if (event.shiftKey && selectable && index >= 0)
          selectRange(anchor >= 0 ? anchor : index, Math.min(index + 1, modelRows.length - 1));
        focusRow(index + 1);
        break;
      case 'ArrowUp':
        event.preventDefault();
        if (event.shiftKey && selectable && index >= 0)
          selectRange(anchor >= 0 ? anchor : index, Math.max(index - 1, 0));
        focusRow(index - 1);
        break;
      case 'Home':
        event.preventDefault();
        focusRow(0);
        break;
      case 'End':
        event.preventDefault();
        focusRow(modelRows.length - 1);
        break;
      case 'Enter':
        event.preventDefault();
        openRow(index);
        break;
      case ' ':
        if (!selectable) return;
        event.preventDefault();
        {
          const id = pageIds[index];
          if (id) {
            setSelected(id, !isSelected(id));
            anchor = index;
          }
        }
        break;
      default:
        break;
    }
  }

  /* -- the column chooser ----------------------------------------------------- */

  let chooserOpen = $state(false);
  let chooser = $state<HTMLDivElement | null>(null);

  $effect(() => {
    if (!chooserOpen) return;
    const layer = layers.push('dropdown', () => (chooserOpen = false));
    const onDocument = (event: MouseEvent) => {
      if (!chooser?.contains(event.target as Node)) chooserOpen = false;
    };
    document.addEventListener('mousedown', onDocument);
    return () => {
      layers.remove(layer);
      document.removeEventListener('mousedown', onDocument);
    };
  });

  const hideable = $derived(columns.filter((c) => c.hideable !== false));
  const isEmpty = $derived(settled && !error && modelRows.length === 0);
</script>

<div class="grid {className}" class:rows={phoneRows}>
  <div class="toolbar">
    {#if selectable && selected.length > 0}
      <div class="bulk" role="group" aria-label="Actions for the selected {noun}">
        <span class="bulk-count tabular">{selected.length} selected</span>
        {#each bulkActions as action (action.id)}
          {@const tooMany = action.single === true && selected.length > 1}
          <Button
            size="sm"
            variant={action.danger ? 'danger' : 'secondary'}
            icon={action.icon}
            disabled={tooMany}
            title={tooMany ? `${action.label} works on one ${noun} at a time` : undefined}
            onclick={() => void runBulk(action)}
          >
            {action.label}
          </Button>
        {/each}
        <Button size="sm" variant="ghost" onclick={() => (selected = [])}>Clear selection</Button>
      </div>
    {/if}
    {#if viewport.phone}
      <!--
        The layout choice, offered where the layouts differ. It is this grid's
        alone and outlives the visit; Settings holds the default every grid
        nobody has decided for follows.
      -->
      <Segmented
        class="view"
        label="How a {noun.replace(/s$/, '')} is laid out"
        value={view}
        options={[
          { value: 'cards', label: 'Cards', name: 'Every value on its own line' },
          { value: 'rows', label: 'Rows', name: 'The table, scrolling sideways' },
        ]}
        onchange={(next) => prefs.setGridView(gridId, next as GridView)}
      />
    {/if}
    <div class="chooser-wrap" bind:this={chooser}>
      <Button
        size="sm"
        variant="ghost"
        icon={Columns3}
        ariaExpanded={chooserOpen}
        ariaHaspopup="menu"
        ariaControls="{gridId}-columns"
        onclick={() => (chooserOpen = !chooserOpen)}
      >
        Columns
      </Button>
      {#if chooserOpen}
        <div class="chooser" id="{gridId}-columns" role="group" aria-label="Columns to show">
          {#each hideable as column (column.id)}
            <!--
              A column the window is too narrow for is still listed, and still
              takes the operator's choice, because that choice is theirs and
              outlives this window size -- but it says why ticking it changes
              nothing they can see. A tick that does nothing and does not say so
              is the kind of control that gets reported as broken.
            -->
            <Checkbox
              label={column.header}
              description={narrowDesktop && column.priority === 'wide'
                ? 'Shown when the window is wider'
                : undefined}
              checked={visibility[column.id] !== false}
              onchange={(on) => toggleColumn(column.id, on)}
            />
          {/each}
        </div>
      {/if}
    </div>
  </div>

  <div class="scroll" bind:this={frame}>
    <!--
      aria-rowcount is the server's total, not this page's length, so every row
      has to say which of that total it is. Without aria-rowindex a screen
      reader announces "row 3 of 1,284" for the third row of page nine, and the
      count it was given becomes noise. The header is row 1, so the data starts
      at offset + 2.
    -->
    <table
      role="grid"
      aria-label={label}
      aria-rowcount={total}
      style:min-width={tableWidth}
      onkeydown={onBodyKeydown}
    >
      <!--
        The rest of the roles are spelled out for the reason the row's and the
        cell's are: below the phone breakpoint `thead`, `tbody` and the heading
        row all stop being table boxes, and a browser drops an implicit role the
        moment that happens -- which would leave the grid with an
        `aria-rowcount` and nothing to count.
      -->
      <!-- svelte-ignore a11y_no_redundant_roles -->
      <thead role="rowgroup">
        <!-- svelte-ignore a11y_no_redundant_roles -->
        <tr role="row" aria-rowindex={1}>
          {#if selectable}
            <th role="columnheader" class="pick" scope="col" style:width={columnWidths.pick}>
              <Checkbox
                checked={allSelected}
                indeterminate={someSelected}
                ariaLabel="Select every {noun} on this page"
                onchange={toggleAll}
              />
            </th>
          {/if}
          {#each visibleColumns as column, index (column.id)}
            <th
              role="columnheader"
              scope="col"
              style:width={columnWidths.columns[index]}
              class:end={column.align === 'end'}
              class:sortable={column.sortable}
              aria-sort={sort === column.id
                ? order === 'asc'
                  ? 'ascending'
                  : 'descending'
                : undefined}
            >
              {#if column.sortable}
                <button type="button" class="sort" onclick={() => toggleSort(column)}>
                  <span>{column.header}</span>
                  <span class="arrow" aria-hidden="true">
                    {#if sort === column.id}{order === 'asc' ? '↑' : '↓'}{/if}
                  </span>
                </button>
              {:else}
                {column.header}
              {/if}
            </th>
          {/each}
        </tr>
      </thead>
      <!-- svelte-ignore a11y_no_redundant_roles -->
      <tbody role="rowgroup" bind:this={body}>
        {#if !settled}
          {#each Array.from({ length: 8 }, (_, i) => i) as line (line)}
            <!-- svelte-ignore a11y_no_redundant_roles -->
            <tr role="row" class="skeleton-row" aria-rowindex={offset + line + 2}>
              {#if selectable}<td role="gridcell" class="pick"
                  ><Skeleton width="var(--z-control-box)" height="var(--z-control-box)" /></td
                >{/if}
              {#each visibleColumns as column (column.id)}
                <td role="gridcell"
                  ><Skeleton width={column.align === 'end' ? '3rem' : '70%'} height="0.9rem" /></td
                >
              {/each}
            </tr>
          {/each}
        {:else}
          {#each modelRows as row, index (row.id)}
            <!--
              `role` is spelled out on the row and every cell rather than left
              to the table's own display type. On a phone the rows become
              cards, which means `display` is no longer `table-row`, and a
              browser drops the implicit row and cell roles the moment that
              happens -- so the grid would keep its `role="grid"` and lose the
              rows inside it.
            -->
            <tr
              role="row"
              data-row={index}
              aria-rowindex={offset + index + 2}
              tabindex={index === focused || (focused === -1 && index === 0) ? 0 : -1}
              class:selected={isSelected(row.id)}
              class:clickable={Boolean(onopen)}
              aria-selected={selectable ? isSelected(row.id) : undefined}
              onclick={() => {
                focused = index;
                openRow(index);
              }}
              onfocus={() => (focused = index)}
            >
              {#if selectable}
                <!--
                  The row's own click opens the row, so the cell that holds the
                  selection checkbox has to stop the event reaching it. Without
                  this, ticking a row navigates away and selects nothing, which
                  makes bulk selection impossible with a mouse -- the keyboard
                  path is unaffected, which is exactly why it went unnoticed.
                  Every other cell that holds a control does the same.
                -->
                <td role="gridcell" class="pick" onclick={(event) => event.stopPropagation()}>
                  <Checkbox
                    checked={isSelected(row.id)}
                    ariaLabel="Select this row"
                    onchange={(on) => {
                      setSelected(row.id, on);
                      anchor = index;
                      focused = index;
                    }}
                  />
                </td>
              {/if}
              {#each visibleColumns as column (column.id)}
                {@const plain = column.cell ? '' : (column.value?.(row.original) ?? '')}
                <!--
                  `data-label` is what the cell calls itself once the heading
                  row is gone and the row is a card, so a figure is never left
                  on a phone without the word that says what it is.
                -->
                <td role="gridcell" data-label={column.header} class:end={column.align === 'end'}>
                  {#if column.cell}
                    <!--
                      The title is the plain renderer's, for the same reason: a
                      cell cut off at its column's edge has to keep the whole
                      value somewhere, and a column that draws itself still
                      knows what it is worth in words.
                    -->
                    <div
                      class="cell-body"
                      class:loose={column.overflows}
                      title={column.value?.(row.original) || undefined}
                    >
                      {@render column.cell(row.original)}
                    </div>
                  {:else}
                    <!--
                      One line and an ellipsis, with the whole value in a title.
                      A column has a width, and a hyphenated name with nothing
                      stopping it wraps one segment per line -- which makes
                      every row in the grid as tall as the longest name in it.
                    -->
                    <span class="plain" title={plain || undefined}>{plain}</span>
                  {/if}
                </td>
              {/each}
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>

  {#if error}
    <ErrorState {error} onretry={() => router.setQuery({ offset: offset || null })} />
  {:else if isEmpty}
    <EmptyState title={emptyTitle} description={emptyDescription}>
      {#if emptyAction}{@render emptyAction()}{/if}
    </EmptyState>
  {/if}

  <Pagination {total} {limit} {offset} {noun} onpage={goTo} onlimit={setLimit} />
  <p class="sr-only" aria-live="polite">
    {loading ? 'Loading' : `${total} ${noun}`}
  </p>
</div>

<style>
  .grid {
    display: flex;
    flex-direction: column;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
    min-width: 0;
  }
  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-3);
    padding: var(--z-space-2) var(--z-space-3);
    border-bottom: var(--z-border-width) solid var(--z-border);
    min-height: var(--z-space-10);
  }
  .bulk {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
  }

  .bulk-count {
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    color: var(--z-text-muted);
    font-variant-numeric: tabular-nums;
  }
  .chooser-wrap {
    position: relative;
    margin-left: auto;
  }
  .chooser {
    position: absolute;
    right: 0;
    top: calc(100% + var(--z-space-1));
    z-index: var(--z-layer-dropdown);
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
    min-width: 190px;
    padding: var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface-raised);
    box-shadow: var(--z-shadow-md);
  }
  .scroll {
    /*
      Down, never across. A grid that scrolls sideways hides the column an
      operator is looking for behind a gesture nobody makes on a page that has
      already scrolled once, and puts the row's name off the left edge as soon
      as they do -- so the table is made to fit instead, below, and the frame
      is told plainly that it has nothing to scroll horizontally.
    */
    overflow-x: hidden;
    overflow-y: auto;
    max-height: 70vh;
    /*
      Also the containing block for anything absolutely positioned inside it.
      Without this, a visually-hidden `.sr-only` span in a cell 500px along a
      1200px-wide table is positioned against the page instead, escapes this
      frame's clipping, and makes the whole document that wide -- so on a phone
      the page scrolls sideways and the browser zooms out to fit, which is a
      strange amount of damage for a one-pixel span nobody can see.
    */
    position: relative;
  }
  table {
    width: 100%;
    /*
      Fixed, so the frame decides the width and the columns divide it, rather
      than the longest repository name in the page deciding for everybody. Under
      the automatic layout a declared width is a floor and `width: 100%` is a
      floor as well, so a wide row made the table wider than its frame and the
      ellipsis on a truncating cell could never fire -- there was always more
      room to be had by growing. Here a declared width is what the column gets,
      what is left over is shared between the rest, and a cell too small for its
      content says so with an ellipsis and keeps the whole value in its title.
    */
    table-layout: fixed;
    border-collapse: separate;
    border-spacing: 0;
    font-size: var(--z-text-sm);
    font-variant-numeric: tabular-nums;
  }
  thead th {
    position: sticky;
    top: 0;
    z-index: var(--z-layer-sticky);
    padding: var(--z-space-2) var(--z-space-4);
    border-bottom: var(--z-border-width) solid var(--z-border);
    background: var(--z-surface-sunken);
    color: var(--z-text-muted);
    font-size: var(--z-text-2xs);
    font-weight: var(--z-weight-medium);
    text-align: left;
    text-transform: uppercase;
    letter-spacing: var(--z-tracking-wide);
    white-space: nowrap;
    /* A heading is as narrow as its column now, so it truncates like a cell. */
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* The sort button is the heading, so it has to truncate as the heading does. */
  .sort > span:first-child {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  th.end,
  td.end {
    text-align: right;
  }
  th.pick,
  td.pick {
    /* The width is a share of the frame like every other column's, set inline. */
    padding-left: var(--z-space-3);
    padding-right: 0;
  }
  .sort {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
    border: 0;
    padding: 0;
    background: transparent;
    color: inherit;
    font: inherit;
    text-transform: inherit;
    letter-spacing: inherit;
    cursor: pointer;
  }
  .sort:hover {
    color: var(--z-text);
  }
  .arrow {
    display: inline-block;
    min-width: var(--z-space-2);
  }
  tbody td {
    padding: var(--z-space-3) var(--z-space-4);
    border-bottom: var(--z-border-width) solid var(--z-border);
    color: var(--z-text);
    vertical-align: middle;
  }
  /*
    Content is cut off inside the cell rather than by it. The cell itself has
    to stay unclipped -- a menu or a popover opened from a row is anchored in
    one, and a cell that clipped its own overflow would cut it off at the row's
    edge -- so the clipping happens one level in, where a column that opens
    something can say it wants none.

    A row is one line high whatever is in it. A runner name is hyphenated, and
    a cell narrow enough to wrap it puts one segment per line, which makes
    every row in the table as tall as the longest name in it -- the same
    failure the default renderer has always guarded against.
  */
  .cell-body {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    /*
      The clip is pushed a ring's width outwards and pulled straight back, so
      the box that cuts content off is wider than the box that lays it out. A
      link sits flush against its cell's leading edge, and the global ring is
      drawn `--z-focus-offset` clear of it: without this the clip started at
      the link's own edge and a keyboard user saw three sides of a rectangle.
      Padding and margin cancel, so nothing moves and the ellipsis still
      lands where the column ends.
    */
    --z-cell-ring: calc(var(--z-focus-offset) + var(--z-focus-width));
    margin: calc(-1 * var(--z-cell-ring));
    padding: var(--z-cell-ring);
  }
  .cell-body.loose {
    overflow: visible;
  }
  .plain {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  tbody tr:last-child td {
    border-bottom: 0;
  }
  tbody tr.clickable {
    cursor: pointer;
  }
  tbody tr:hover td {
    background: var(--z-surface-hover);
  }
  tbody tr.selected td {
    background: var(--z-accent-subtle);
  }
  tbody tr:focus-visible {
    outline: var(--z-focus-width) solid var(--z-focus-colour);
    outline-offset: calc(-1 * var(--z-focus-offset));
  }
  .skeleton-row td {
    padding: var(--z-space-3) var(--z-space-4);
  }

  /*
    On a phone a dozen columns in 360 pixels is not a table, whatever is done
    to it: every repository is "acme/pl..." and indistinguishable from the next,
    and the figure reached by scrolling belongs to a row that can no longer be
    named. So each row becomes a card and each cell carries its own heading,
    which is what the usage report and the Hosts page already do with the same
    problem. Nothing is dropped and nothing is truncated -- the grid reads down
    instead of across.

    The frame keeps its `overflow-y` and its height so the sticky page around
    it is unchanged; it is the table inside that stops being a table.
  */
  @media (max-width: 768px) {
    .grid:not(.rows) table {
      display: block;
      table-layout: auto;
    }
    /*
      The headings have moved onto the cells, so most of the row of them is
      noise -- but not all of it. What is left is the two things a heading row
      does that a card cannot: the tick that selects every row on the page, and
      the sort, which is the whole of "which of these is slowest?" and the one
      question a phone is as good at asking as a desktop. They become a strip of
      small controls above the cards.

      The rest are taken out of the layout rather than out of the document: a
      screen reader still needs a column header to associate a cell with, and
      removing them outright would leave the grid with rows and no columns.
    */
    .grid:not(.rows) thead {
      display: block;
    }
    .grid:not(.rows) thead tr {
      display: flex;
      align-items: center;
      flex-wrap: wrap;
      gap: var(--z-space-2);
      padding: 0 var(--z-space-3);
    }
    .grid:not(.rows) thead th {
      position: absolute;
      width: var(--z-nudge-1);
      height: var(--z-nudge-1);
      padding: 0;
      border: 0;
      overflow: hidden;
      clip-path: inset(50%);
    }
    .grid:not(.rows) thead th.pick,
    .grid:not(.rows) thead th.sortable {
      position: static;
      display: flex;
      align-items: center;
      width: auto;
      height: auto;
      padding: var(--z-space-2) 0;
      overflow: visible;
      background: none;
      clip-path: none;
    }
    /* Each sort reads as the small control it now is, rather than as the
       heading of a column that is no longer beside it. */
    .grid:not(.rows) thead th.sortable .sort {
      padding: var(--z-space-1) var(--z-space-2);
      border: var(--z-border-width) solid var(--z-border);
      border-radius: var(--z-radius-sm);
      background: var(--z-surface-sunken);
    }
    .grid:not(.rows) thead th[aria-sort] .sort {
      border-color: var(--z-accent-border);
      background: var(--z-accent-subtle);
      color: var(--z-accent);
    }
    .grid:not(.rows) tbody {
      display: flex;
      flex-direction: column;
      gap: var(--z-space-3);
      padding: var(--z-space-3);
      border-top: var(--z-border-width) solid var(--z-border);
    }
    .grid:not(.rows) tbody tr {
      display: block;
      border: var(--z-border-width) solid var(--z-border);
      border-radius: var(--z-radius-md);
      background: var(--z-surface);
    }
    .grid:not(.rows) tbody tr.selected {
      border-color: var(--z-accent-border);
      background: var(--z-accent-subtle);
    }
    /* The hover and selection tints belong to the card now, not to its cells. */
    .grid:not(.rows) tbody tr:hover td,
    .grid:not(.rows) tbody tr.selected td {
      background: none;
    }
    .grid:not(.rows) tbody tr:last-child td {
      border-bottom: 0;
    }
    .grid:not(.rows) tbody td {
      display: flex;
      align-items: baseline;
      justify-content: space-between;
      gap: var(--z-space-4);
      padding: var(--z-space-2) var(--z-space-3);
      border: 0;
      overflow: visible;
      text-align: right;
    }
    .grid:not(.rows) td.end {
      text-align: right;
    }
    .grid:not(.rows) tbody td::before {
      content: attr(data-label);
      flex: none;
      color: var(--z-text-muted);
      font-size: var(--z-text-2xs);
      font-weight: var(--z-weight-medium);
      text-transform: uppercase;
      letter-spacing: var(--z-tracking-wide);
      text-align: left;
    }
    /*
      The tick is the card's own control rather than a figure in it, so it
      keeps the heading row's wording and sits at the top on its own.
    */
    .grid:not(.rows) td.pick {
      justify-content: flex-start;
      width: auto;
      padding: var(--z-space-3) var(--z-space-3) var(--z-space-2);
      border-bottom: var(--z-border-width) solid var(--z-border);
    }
    .grid:not(.rows) td.pick::before {
      content: 'Select';
    }
    /*
      A value has the width of a card now, so it wraps rather than truncating:
      a name cut short is the thing this layout exists to stop. Both renderers,
      because a cell drawn by a snippet is no less a value than a plain one --
      leaving `.cell-body` clipped here would have kept every badge, label and
      link on one cut-off line while the plain cells beside them wrapped.
    */
    .grid:not(.rows) .plain,
    .grid:not(.rows) .cell-body {
      overflow: visible;
      white-space: normal;
      overflow-wrap: anywhere;
      /*
        Shrinkable, or the value does not wrap at all: these are flex items of
        the card's row, and a flex item refuses to go below the width its
        content wants unless told it may. While the cell clipped, that width
        was nothing and the question never arose; now that it wraps, a runner
        name would otherwise push the whole card wider than the screen.
      */
      min-width: 0;
    }

    /*
      The other phone layout: the table, kept. The columns take the widths they
      declare and the frame scrolls to the ones past the edge -- which the rest
      of this product refuses to do, and does here because it is what the
      operator asked for by pressing Rows. The scrolling is the frame's alone:
      it clips, so the page behind it is the width of the window either way.
    */
    .grid.rows .scroll {
      overflow-x: auto;
    }
    /*
      Three controls where there were one or two, and a phone has no room for
      them side by side once rows are selected. They wrap instead of pushing
      the Columns button off the edge.
    */
    .toolbar {
      flex-wrap: wrap;
    }
  }
</style>
