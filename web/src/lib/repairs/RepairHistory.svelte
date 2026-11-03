<!--
  The History of repairs, a page at a time.

  The controller does the filtering and the paging, so this card holds one page
  and a total, never the whole history. It reads on its own ten-second timer
  with the filters and page the operator has chosen, because a refresh that
  forgot them would throw somebody back to the first page of everything while
  they were reading page three of one repository.

  Repairs are chosen here and cleared from the history in one go, behind a Select
  button so the list reads as a record until somebody means to edit it. Clearing
  hides a repair and nothing else, which the confirmation says in so many words;
  the chosen ids are one set that outlives the refresh and a change of page,
  because choosing is slow work, and is dropped by a change of filter, because
  what was chosen may not be in the new list at all.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { SvelteSet } from 'svelte/reactivity';
  import { ApiError, bulkEliRepairAction, listEliRepairs, selectEliRepairs } from '$lib/api/client';
  import type { Schemas } from '$lib/api/types';
  import { formatNumber } from '$lib/format';
  import { prefs, remember, remembered } from '$lib/state/prefs.svelte';
  import { toasts } from '$lib/state/toasts.svelte';
  import Button from '$lib/components/Button.svelte';
  import Checkbox from '$lib/components/Checkbox.svelte';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import Pagination from '$lib/components/Pagination.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import Select from '$lib/components/Select.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import {
    BULK_LIMIT,
    NO_REPAIR_FILTERS,
    batches,
    clearConsequences,
    clearReport,
    clearSummary,
    filtered,
    isActive,
    listQuery,
    offsetAfterClear,
    selectionQuery,
    type RepairFilters,
    type RepairGroup,
    type RepairTrigger,
  } from './history';
  import ClearOutcome from './ClearOutcome.svelte';
  import RepairRow from './RepairRow.svelte';
  import type { RepairHistorySummary } from './history';

  interface Props {
    /** Render only the card's contents, for a section that supplies its heading. */
    bare?: boolean;
    /** What a closed History section can say about the list it last read. */
    onsummary?: (summary: RepairHistorySummary) => void;
  }

  let { bare = false, onsummary }: Props = $props();

  const GRID = 'eli-repairs';
  /** Long enough that a word is typed before it is searched for. */
  const PAUSE_MS = 300;
  /** Long enough for a live region to be seen empty before it is written to. */
  const ANNOUNCE_AFTER_MS = 100;

  let filters = $state<RepairFilters>({ ...NO_REPAIR_FILTERS });
  /** What the box holds, which runs ahead of `filters.repo` by the pause. */
  let repoText = $state('');
  /** Derived, as the other lists do, because the preferences arrive after sign-in. */
  const limit = $derived(prefs.pageSize(GRID));
  let offset = $state(0);
  let items = $state<Schemas['EliRepair'][]>([]);
  let total = $state(0);
  /**
   * The filters the rows and the total above were read with. What the card
   * draws goes by these and not by `filters`, which is a step ahead while a
   * read is in flight: the controls are what an operator is using to widen a
   * search that matched nothing, and neither they nor the empty-state wording
   * may change under them until the wider answer is here.
   */
  let answered = $state<RepairFilters>({ ...NO_REPAIR_FILTERS });
  let loaded = $state(false);
  let error = $state('');
  /** The filter fields are a disclosure, not a permanent row of three controls. */
  const FILTERS_KEY = 'zoomies.repairs.filters.open';
  let showFilterFields = $state(
    remembered(FILTERS_KEY, false, (value): value is boolean => typeof value === 'boolean'),
  );
  function toggleFilterFields(): void {
    showFilterFields = !showFilterFields;
    remember(FILTERS_KEY, showFilterFields);
  }
  let pause: ReturnType<typeof setTimeout> | undefined;
  let reading: AbortController | null = null;
  /**
   * Which read is the latest. A slow answer to a page the operator has already
   * left must not repaint over the page they are on, and an abort alone does
   * not promise that: the answer may already be in flight to the parser.
   */
  let latest = 0;

  /**
   * The repairs chosen, in the order they were chosen. One set serves the row
   * boxes, the header box and a select-all, so the three cannot disagree about
   * what is chosen.
   */
  const chosen = new SvelteSet<string>();
  /**
   * Whether the boxes are showing. Off, the history is a record to read and every
   * row is a link; on, the rows can be chosen. Turning it off drops what was chosen.
   */
  let selecting = $state(false);
  /**
   * What the last select-all took, while the set is still that: how many
   * repairs the filters could clear in all, and whether the server stopped
   * short of them. Any other change to the set makes the count plain again,
   * since it is no longer "everything".
   */
  let everything = $state<{ total: number; truncated: boolean } | null>(null);
  let taking = $state(false);
  let taken: AbortController | null = null;
  let confirmOpen = $state(false);
  /**
   * The ids the dialog was opened for. The dialog counts these and a confirm
   * clears exactly these, never the live set, so what is cleared is what the
   * operator was told and nothing that landed behind the modal.
   */
  let toClear = $state<string[]>([]);
  /** What the last clear did, which stays until it is dismissed. */
  let report = $state<{ cleared: number; skipped: { label: string; error: string }[] } | null>(
    null,
  );
  /**
   * What a screen reader is told about the last clear. A live region says
   * something when its text changes, and two clears can end in the same words,
   * so it is emptied first and written a beat later.
   */
  let announcement = $state('');
  let announcing: ReturnType<typeof setTimeout> | undefined;

  function announce(words: string): void {
    clearTimeout(announcing);
    announcement = '';
    announcing = setTimeout(() => (announcement = words), ANNOUNCE_AFTER_MS);
  }

  async function load(): Promise<void> {
    reading?.abort();
    const controller = new AbortController();
    reading = controller;
    const mine = ++latest;
    const asked = { ...filters };
    try {
      const page = await listEliRepairs(listQuery(asked, limit, offset), controller.signal);
      if (mine !== latest) return;
      if (page.items.length === 0 && page.total > 0 && offset > 0) {
        // The page was emptied under the operator (a refresh after rows left
        // it, say). The history is not empty, so step back rather than say so.
        // Only a lower offset is a step: an answer that is empty at an offset
        // inside the total (a count and a list that disagree) would otherwise
        // be asked for again, for ever, with no pause between.
        const back = offsetAfterClear(offset, limit, page.total, 0);
        if (back < offset) {
          offset = back;
          return await load();
        }
      }
      items = page.items;
      total = page.total;
      answered = asked;
      loaded = true;
      error = '';
    } catch (cause) {
      if (mine !== latest) return;
      error = cause instanceof ApiError ? cause.message : 'Could not read the repair history.';
    }
  }

  /** The page header's Refresh, and whatever else changed what the history holds. */
  export async function reload(): Promise<void> {
    await load();
  }

  export function reviewNeedsAction(): void {
    showFilterFields = true;
    refilter({ group: 'needs_action', repo: '', trigger: 'any' });
    repoText = '';
  }

  /** Any change to what is listed starts again from the first page. */
  function refilter(change: Partial<RepairFilters>): void {
    clearTimeout(pause);
    filters = { ...filters, repo: repoText, ...change };
    offset = 0;
    unchoose();
    void load();
  }

  function toggleSelecting(): void {
    selecting = !selecting;
    if (!selecting) unchoose();
  }

  /** Nothing chosen, and no select-all still on its way to choose something. */
  function unchoose(): void {
    taken?.abort();
    taking = false;
    chosen.clear();
    everything = null;
  }

  function choose(id: string, on: boolean): void {
    if (on) chosen.add(id);
    else chosen.delete(id);
    everything = null;
  }

  function chooseShown(on: boolean): void {
    for (const id of clearableShown) choose(id, on);
  }

  /**
   * Every repair the filters match that can be cleared, past the page shown.
   * It asks by the filters the visible answer was read with: the total on the
   * button belongs to them, and so must what the button chooses.
   */
  async function takeEverything(): Promise<void> {
    taken?.abort();
    const controller = new AbortController();
    taken = controller;
    taking = true;
    try {
      const picked = await selectEliRepairs(selectionQuery(answered), controller.signal);
      if (controller.signal.aborted) return;
      // Replaced, not added to: the ids are already everything that matches, and
      // adding a row from further back could take the list past what one clear
      // may carry for no reason the operator can see.
      chosen.clear();
      for (const id of picked.ids) chosen.add(id);
      everything = { total: picked.total, truncated: picked.truncated };
    } catch (cause) {
      if (!controller.signal.aborted)
        toasts.fromError(cause, 'Could not select the matching repairs');
    } finally {
      if (taken === controller) taking = false;
    }
  }

  function askToClear(): void {
    toClear = [...chosen];
    confirmOpen = true;
  }

  /**
   * Hides the repairs the dialog counted from the history. The route answers for
   * each id, so a repair that started running since it was listed is skipped and
   * reported rather than failing the lot.
   */
  async function clear(): Promise<boolean> {
    const ids = toClear;
    const names = new Map(items.map((r) => [r.id, `${r.repo} #${r.pull_number || '?'}`]));
    const results: { id: string; ok: boolean; error?: string }[] = [];
    let failure: unknown;
    let sent = 0;
    // One request holds 500 ids at most, and a select-all plus a row from a later
    // page can pass that.
    for (const part of batches(ids, BULK_LIMIT)) {
      try {
        results.push(...(await bulkEliRepairAction({ action: 'clear', ids: part })).results);
        sent += part.length;
      } catch (cause) {
        failure = cause;
        break;
      }
    }
    // Nothing was cleared, so the dialog and the selection stay for another go.
    if (failure && sent === 0) {
      toasts.fromError(failure, 'Could not clear those repairs');
      return false;
    }
    // A request that fell over may or may not have been applied, so everything
    // from it on is reported as not attempted and stays chosen. Trying again is
    // safe: clearing a repair that is already cleared is a success.
    const left = ids.slice(sent);
    const outcome = clearReport(results, left);
    if (failure)
      toasts.fromError(
        failure,
        `Cleared ${formatNumber(outcome.cleared)} of ${formatNumber(ids.length)} repairs before a request failed`,
      );
    report = {
      cleared: outcome.cleared,
      skipped: outcome.skipped.map((s) => ({ label: names.get(s.id) ?? s.id, error: s.error })),
    };
    announce(clearSummary(outcome.cleared, outcome.skipped.length));
    unchoose();
    for (const id of left) chosen.add(id);
    // The page the operator was on may have gone with the rows.
    offset = offsetAfterClear(offset, limit, total, outcome.cleared);
    void load();
    return true;
  }

  function typed(text: string): void {
    clearTimeout(pause);
    pause = setTimeout(() => {
      if (text.trim() !== filters.repo.trim()) refilter({ repo: text });
    }, PAUSE_MS);
  }

  function clearFilters(): void {
    repoText = '';
    refilter({ ...NO_REPAIR_FILTERS });
  }

  /** Nothing to filter in an empty history, so the controls wait for a repair. */
  const showFilters = $derived(filtered(answered) || total > 0);
  /**
   * The rows on show are still the previous filters' while a read for new ones is
   * out. Nothing may be chosen from them: it would sit in the set under filters
   * it does not belong to, and the bar would count a repair nobody can see ticked.
   */
  const waiting = $derived(
    filters.group !== answered.group ||
      filters.repo !== answered.repo ||
      filters.trigger !== answered.trigger,
  );
  /** The shown repairs that can be cleared: those Eli is not still on. */
  const clearableShown = $derived(items.filter((r) => !isActive(r.state)).map((r) => r.id));
  const shownChosen = $derived(clearableShown.filter((id) => chosen.has(id)).length);
  const allShownChosen = $derived(
    clearableShown.length > 0 && shownChosen === clearableShown.length,
  );
  /** Once the page is chosen and more match, the rest can be taken in one step. */
  const canTakeEverything = $derived(allShownChosen && total > items.length && everything === null);
  const howMany = $derived(
    everything?.truncated
      ? `${formatNumber(chosen.size)} of ${formatNumber(everything.total)} selected`
      : `${formatNumber(chosen.size)} selected`,
  );
  /** What a screen reader hears of the selection: the count and nothing to press. */
  const counted = $derived(chosen.size > 0 ? howMany : '');

  // The closed section's line reads from the same list the page does, so it
  // cannot say a different number from the rows behind it.
  $effect(() => {
    onsummary?.({
      loaded,
      error,
      total,
      latest: items[0]?.created_at,
      filtered: filtered(answered),
    });
  });

  onMount(() => {
    void load();
    const timer = setInterval(() => void load(), 10000);
    return () => {
      clearInterval(timer);
      clearTimeout(pause);
      clearTimeout(announcing);
      latest++;
      reading?.abort();
      taken?.abort();
    };
  });
</script>

{#snippet content()}
  <!--
    The card's two live regions, in the page from the start and empty until there
    is something to say. A region that is inserted already holding its words is
    announced by some screen readers and not by others, and the outcome of a clear
    arrives after the button that caused it has gone. Nothing a person presses is
    inside either: a box or a button in a live region is announced by name
    whenever it appears.
  -->
  <output class="sr-only" aria-live="polite">{counted}</output>
  <output class="sr-only" aria-live="polite">{announcement}</output>
  {#if showFilters}
    <div class="history-toolbar pad">
      <Button
        variant="secondary"
        size="sm"
        ariaExpanded={showFilterFields}
        onclick={toggleFilterFields}>Filters</Button
      >
      {#if filtered(answered)}
        <button class="active-filter" type="button" onclick={clearFilters}
          >Filters active <span aria-hidden="true">×</span><span class="sr-only"
            >Clear the filters</span
          ></button
        >
      {/if}
      <span class="toolbar-spacer"></span>
      {#if bare && loaded && total > 0}
        <Button variant="ghost" onclick={toggleSelecting}>{selecting ? 'Done' : 'Select'}</Button>
      {/if}
    </div>
    {#if showFilterFields}
      <div class="filters pad">
        <Field id="repair-filter" label="Show"
          ><Select
            id="repair-filter"
            value={filters.group}
            onchange={(value) => refilter({ group: value as RepairGroup })}
            options={[
              { value: 'all', label: 'All repairs' },
              { value: 'attention', label: 'Needs attention' },
              { value: 'needs_action', label: 'Latest repair needs review' },
              { value: 'active', label: 'In progress' },
              { value: 'passed', label: 'Checks passed' },
            ]}
          /></Field
        >
        <Field id="repair-repository" label="Repository"
          ><Input
            id="repair-repository"
            bind:value={repoText}
            oninput={(event) => typed((event.currentTarget as HTMLInputElement).value)}
            placeholder="acme/site"
            autocomplete="off"
            spellcheck={false}
          /></Field
        >
        <Field id="repair-trigger" label="Triggered by"
          ><Select
            id="repair-trigger"
            value={filters.trigger}
            onchange={(value) => refilter({ trigger: value as RepairTrigger })}
            options={[
              { value: 'any', label: 'Any' },
              { value: 'automatic', label: 'Automatic' },
              { value: 'requested', label: 'Requested' },
            ]}
          /></Field
        >
      </div>
    {/if}
  {/if}
  {#if error}<p role="alert" class="error pad">{error}</p>{/if}
  {#if report}
    <div class="pad">
      <ClearOutcome
        cleared={report.cleared}
        skipped={report.skipped}
        ondismiss={() => (report = null)}
      />
    </div>
  {/if}
  {#if !loaded}
    {#if !error}<div class="pad"><Skeleton lines={3} /></div>{/if}
  {:else if total === 0}
    <div class="pad">
      {#if filtered(answered)}
        <EmptyState
          compact
          title="No repairs match these filters"
          description="Nothing in the history answers all of them at once. Removing one will widen the search."
        >
          <Button variant="secondary" onclick={clearFilters}>Clear filters</Button>
        </EmptyState>
      {:else}
        <EmptyState
          compact
          title="No repairs yet"
          description="Repairs appear when a linked user asks or an automatic policy encounters a failed PR job."
        />
      {/if}
    </div>
  {:else}
    {#if selecting}
      <div class="bar" role="group" aria-label="Repair selection">
        <Checkbox
          checked={allShownChosen}
          indeterminate={shownChosen > 0 && !allShownChosen}
          disabled={waiting || clearableShown.length === 0}
          reason={!waiting && clearableShown.length === 0
            ? 'Every repair shown is still running.'
            : undefined}
          ariaLabel={items.length === 1
            ? 'Select the repair shown'
            : `Select the ${items.length} repairs shown`}
          onchange={chooseShown}
        />
        {#if chosen.size > 0}
          <strong class="count">{howMany}</strong>
          <div class="actions">
            {#if canTakeEverything}
              <Button
                size="sm"
                variant="secondary"
                loading={taking}
                onclick={() => void takeEverything()}
                >Select all {formatNumber(total)} matching</Button
              >
            {/if}
            <!-- The count is not settled while a select-all is out, so neither is what a clear would clear. -->
            <Button
              size="sm"
              variant="secondary"
              disabled={taking}
              reason={taking ? 'Still choosing the matching repairs.' : undefined}
              onclick={askToClear}>Clear from history</Button
            >
            <Button size="sm" variant="ghost" onclick={unchoose}>Clear selection</Button>
          </div>
        {:else}
          <span class="hint">Select repairs to clear them from the history.</span>
        {/if}
      </div>
    {/if}
    <!-- The same columns as a row's, so the words sit over what they name. -->
    <div class="head" class:selecting aria-hidden="true">
      <span>Repair</span><span>Failure</span><span>Triggered by</span><span>When</span><span></span>
    </div>
    <div class="history">
      {#each items as r (r.id)}
        <RepairRow
          repair={r}
          {selecting}
          selected={chosen.has(r.id)}
          {waiting}
          onselect={(on) => choose(r.id, on)}
        />
      {/each}
    </div>
    <div class="pad">
      <Pagination
        {total}
        {limit}
        {offset}
        noun="repairs"
        onpage={(next) => {
          offset = next;
          void load();
        }}
        onlimit={(size) => {
          prefs.setPageSize(GRID, size);
          offset = 0;
          void load();
        }}
      />
    </div>
  {/if}
{/snippet}

{#if bare}
  {@render content()}
{:else}
  <Panel
    title="History"
    description="Every attempt on the repositories you can see. Open one for the failure, the outcome and what Eli used."
    flush
  >
    {#snippet actions()}
      {#if loaded && total > 0}
        <Button variant="ghost" onclick={toggleSelecting}>{selecting ? 'Done' : 'Select'}</Button>
      {/if}
    {/snippet}
    {@render content()}
  </Panel>
{/if}

<!--
  It says what clearing leaves alone because that is the part that surprises: the
  rows go from this list, and the limit and the guard still read them.
-->
<ConfirmDialog
  bind:open={confirmOpen}
  title="Clear from history"
  description="The repairs stop showing in this list."
  consequences={clearConsequences(toClear.length)}
  confirmLabel="Clear from history"
  tone="default"
  onconfirm={clear}
/>

<style>
  .pad {
    padding: var(--z-space-4) var(--z-space-5) 0;
  }
  .pad:last-child {
    padding-bottom: var(--z-space-4);
  }
  .history-toolbar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--z-space-2);
    padding-top: var(--z-space-2);
    padding-bottom: var(--z-space-2);
    border-bottom: var(--z-border-width) solid var(--z-border);
    background: var(--z-surface-sunken);
  }
  .history-toolbar .toolbar-spacer {
    flex: 1;
  }
  .active-filter {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
    min-height: var(--z-control-touch);
    padding: 0 var(--z-space-2);
    border: var(--z-border-width) solid var(--z-accent-border);
    border-radius: var(--z-radius-full);
    background: var(--z-accent-subtle);
    color: var(--z-accent);
    font-size: var(--z-text-2xs);
    cursor: pointer;
  }
  .filters {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--z-space-4);
    padding-bottom: var(--z-space-4);
  }
  /* A phone has no room beside the box, so the actions wrap under the count. */
  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-3);
    padding: var(--z-space-2) var(--z-space-5);
    border-top: var(--z-border-width) solid var(--z-border);
    background: var(--z-surface-sunken);
    font-size: var(--z-text-sm);
  }
  .hint {
    color: var(--z-text-muted);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
    margin-left: auto;
  }
  /* Column titles over the rows' columns. Keep the template in step with RepairRow's. */
  .head {
    display: grid;
    grid-template-columns:
      minmax(0, 2.2fr) minmax(0, 1.7fr) minmax(0, 0.9fr) minmax(0, 0.8fr)
      1rem;
    gap: var(--z-space-4);
    padding: var(--z-space-2) var(--z-space-5);
    border-top: var(--z-border-width) solid var(--z-border);
    background: var(--z-surface-sunken);
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-semibold);
    letter-spacing: var(--z-tracking-wider);
    text-transform: uppercase;
  }
  .head.selecting {
    grid-template-columns:
      2rem minmax(0, 2.2fr) minmax(0, 1.7fr) minmax(0, 0.9fr) minmax(0, 0.8fr)
      1rem;
  }
  .head.selecting > :first-child {
    grid-column: 2;
  }
  .history {
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .error {
    color: var(--z-danger);
  }
  @media (max-width: 860px) {
    .head {
      display: none;
    }
  }
  @media (max-width: 640px) {
    .pad {
      padding-right: var(--z-space-4);
      padding-left: var(--z-space-4);
    }
    .bar {
      padding-right: var(--z-space-4);
      padding-left: var(--z-space-4);
    }
    /* Two short controls share a line and the repository box has the next to itself. */
    .filters {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .filters > :global(:nth-child(2)) {
      grid-column: 1 / -1;
      order: 3;
    }
    .actions {
      margin-left: 0;
    }
  }
</style>
