<!--
  The week as a punch card, on the Overview's band: seven rows of twenty-four
  squares, a row a day and a square an hour, darker blue the more jobs finished
  in it. The stage says what is wrong; this says when the fleet is busy, which
  is the other thing an operator opening the page fifty times a day is reading
  it for, and it stands where the dog does when the dog is hidden, which it is
  by default.

  It is the Activity matrix's 7d range at a glance and nothing more: the band is
  white and the brand's blue only, so the squares are blue by density and an
  hour with failures is punched through, as the matrix draws a failing square,
  rather than coloured red. The full-colour grid with its four colourings, its
  ranges and its hour-by-hour detail stays in History; a square here is a way
  into it, and the Overview takes the click to that section with the hour
  selected.

  The series is the same window the History panel draws for 7d, fetched the
  same way (GET /usage grouped by pool, the one grouping with capacity figures,
  summed back into a fleet total) and kept moving the same way: every
  `job.updated` frame is folded into its hour, a reconnect ends in one
  reconciling fetch, and a tab left open past midnight moves the window on. The
  two panels fetch independently rather than through a shared store, because
  the History panel's range is the operator's choice and is usually not 7d.

  When the fetch fails the card draws nothing: the band is the page's answer,
  and an error state here would shout about a picture while History, lower
  down, already offers the retry.
-->
<script lang="ts">
  import { tick, untrack } from 'svelte';
  import { getUsage } from '$lib/api/client';
  import { events } from '$lib/api/sse';
  import type { Job } from '$lib/api/types';
  import { fleet } from '$lib/state/fleet.svelte';
  import { formatNumber, onClockTick, pluralise } from '$lib/format';
  import { managedJob } from '$lib/status';
  import {
    completedIn,
    describe,
    fillWindow,
    foldJob,
    hourGrid,
    level,
    mergeHistories,
    newSeen,
    rangeWindow,
    scaleOf,
    summarise,
    type ActivityBucket,
    type CalendarCell,
    type RangeWindow,
  } from '$lib/insights/activity';

  interface Props {
    /** An hour was chosen: the start of it, for the page to show its detail. */
    onpick?: (at: Date) => void;
    /**
     * How many days the card draws, today included: the Overview's week, or
     * the Jobs band's single row of today. A day is drawn larger, since one
     * row has the height seven share.
     */
    days?: number;
    /** What a press on an hour does, for the grid's accessible name. */
    pickHint?: string;
  }
  let { onpick, days = 7, pickHint = 'Pick an hour for its detail in History.' }: Props = $props();

  const DAY = new Intl.DateTimeFormat(undefined, { weekday: 'short' });
  const HOURS = ['00:00', '06:00', '12:00', '18:00'];
  const title = $derived(days === 1 ? 'Today, by the hour' : `The last ${days} days, by the hour`);

  // The empty window first, so the grid has its shape before the figures arrive.
  // The day count is fixed for a card's life, so its first read is deliberate.
  const initial = untrack(() => rangeWindow(days, 'hour'));
  let range = $state.raw<RangeWindow>(initial);
  let series = $state.raw<ActivityBucket[]>(fillWindow([], initial.from, initial.count, 'hour'));
  let fetchedAt = $state(0);
  let loading = $state(true);
  let failed = $state(false);
  let now = $state(Date.now());
  let seen = newSeen();
  let pending: AbortController | null = null;
  let root: HTMLElement | null = $state(null);

  export async function reload(): Promise<void> {
    pending?.abort();
    const controller = new AbortController();
    pending = controller;
    const next = rangeWindow(days, 'hour');
    const started = Date.now();
    try {
      const report = await getUsage(
        {
          from: next.from.toISOString(),
          to: next.to.toISOString(),
          group_by: 'pool',
          interval: 'hour',
        },
        controller.signal,
      );
      if (controller.signal.aborted || pending !== controller) return;
      range = next;
      series = fillWindow(mergeHistories(report.items ?? []), next.from, next.count, 'hour');
      seen = newSeen();
      fetchedAt = started;
      failed = false;
    } catch {
      if (controller.signal.aborted || pending !== controller) return;
      failed = true;
    } finally {
      if (!controller.signal.aborted && pending === controller) loading = false;
    }
  }

  $effect(() => {
    void reload();
    return () => pending?.abort();
  });

  // One reconciling fetch after a gap in the stream: the replay buffer is finite.
  let previous: string | null = null;
  $effect(() => {
    const status = fleet.connection;
    if (previous !== null && previous !== 'live' && status === 'live') void reload();
    previous = status;
  });

  // Today's squares move as the stream reports this fleet's jobs.
  $effect(() =>
    events.subscribe('job.updated', (job: Job) => {
      if (!managedJob(job)) return;
      const next = foldJob(series, 'hour', job, fetchedAt, seen);
      if (next) series = next;
    }),
  );

  // The current hour's outline moves with the clock, and a tab open past
  // midnight gets tomorrow's row rather than folding its jobs into nothing.
  $effect(() =>
    onClockTick((at) => {
      now = at;
      if (!loading && at >= range.to.getTime()) void reload();
    }),
  );

  const rows = $derived(hourGrid(series));
  const scale = $derived(scaleOf(series));
  const totals = $derived(summarise(series));
  const summary = $derived(
    loading
      ? 'Loading'
      : totals.completed === 0
        ? `No jobs finished ${days === 1 ? 'today' : 'this week'}`
        : totals.failed > 0
          ? `${pluralise(totals.completed, 'job')} finished, ${formatNumber(totals.failed)} failed`
          : `${pluralise(totals.completed, 'job')} finished`,
  );
  /** The hour the clock is in, which is the square outlined and the one the keyboard lands on. */
  const current = $derived.by(() => {
    const at = new Date(now);
    const hour = at.getHours();
    const day = rows.find(
      (row) => row.date.getDate() === at.getDate() && row.date.getMonth() === at.getMonth(),
    );
    return day?.cells[hour] ?? null;
  });
  let focusIndex = $state<number | null>(null);
  const tabStop = $derived(focusIndex ?? current?.index ?? 0);

  function isFuture(cell: CalendarCell): boolean {
    return cell.date.getTime() > now;
  }

  function pick(cell: CalendarCell): void {
    if (isFuture(cell)) return;
    focusIndex = cell.index;
    onpick?.(cell.date);
  }

  /** The square `dr` rows and `dc` columns from this one, skipping the window's gaps. */
  function step(cell: CalendarCell, dr: number, dc: number): CalendarCell | null {
    const r = rows.findIndex((row) => row.cells.includes(cell));
    const c = rows[r]?.cells.indexOf(cell) ?? -1;
    if (r < 0 || c < 0) return null;
    let row = r + dr;
    let col = c + dc;
    while (row >= 0 && row < rows.length && col >= 0 && col < 24) {
      const next = rows[row]?.cells[col];
      if (next) return next;
      row += dr;
      col += dc;
    }
    return null;
  }

  async function moveFocus(cell: CalendarCell | null): Promise<void> {
    if (!cell) return;
    focusIndex = cell.index;
    await tick();
    root?.querySelector<HTMLElement>(`[data-index="${cell.index}"]`)?.focus();
  }

  function onKeydown(event: KeyboardEvent, cell: CalendarCell): void {
    switch (event.key) {
      case 'ArrowRight':
        void moveFocus(step(cell, 0, 1));
        break;
      case 'ArrowLeft':
        void moveFocus(step(cell, 0, -1));
        break;
      case 'ArrowDown':
        void moveFocus(step(cell, 1, 0));
        break;
      case 'ArrowUp':
        void moveFocus(step(cell, -1, 0));
        break;
      case 'Home':
        void moveFocus(rows.find((row) => row.cells.includes(cell))?.cells.find(Boolean) ?? null);
        break;
      case 'End':
        void moveFocus(
          [...(rows.find((row) => row.cells.includes(cell))?.cells ?? [])]
            .reverse()
            .find(Boolean) ?? null,
        );
        break;
      case 'Enter':
      case ' ':
        pick(cell);
        break;
      default:
        return;
    }
    event.preventDefault();
  }
</script>

{#if !failed}
  <div
    class="card"
    class:loading
    class:single={days === 1}
    data-testid="fleet-punch-card"
    aria-busy={loading}
    bind:this={root}
  >
    <div class="head">
      <span class="title">{title}</span>
      <span class="summary">{summary}</span>
    </div>
    <div class="grid" role="grid" aria-label={`${title}. ${pickHint}`}>
      {#each rows as row (row.date.getTime())}
        <div class="row" role="row">
          <span class="day" role="rowheader">{days === 1 ? 'Today' : DAY.format(row.date)}</span>
          {#each row.cells as cell, hour (hour)}
            {#if cell}
              {@const future = isFuture(cell)}
              <div
                role="gridcell"
                class="sq"
                class:current={current?.index === cell.index}
                data-index={cell.index}
                data-level={future ? 0 : level(completedIn(cell.bucket), scale.completed)}
                data-failing={!future && cell.bucket.failed > 0 ? '' : undefined}
                data-future={future ? '' : undefined}
                tabindex={tabStop === cell.index ? 0 : -1}
                aria-label={future
                  ? `${describe(cell.bucket, cell.date, 'hour').split('.')[0]}. Not yet.`
                  : describe(cell.bucket, cell.date, 'hour')}
                aria-disabled={future ? 'true' : undefined}
                aria-current={current?.index === cell.index ? 'time' : undefined}
                onclick={() => pick(cell)}
                onkeydown={(e) => onKeydown(e, cell)}
              ></div>
            {:else}
              <span class="sq blank" role="gridcell" aria-hidden="true"></span>
            {/if}
          {/each}
        </div>
      {/each}
      <div class="axis" aria-hidden="true">
        {#each HOURS as label (label)}<span>{label}</span>{/each}
      </div>
    </div>
  </div>
{/if}

<style>
  /*
    The card takes the width the band gives it and no more: a square is at most
    the size of the page's own small squares and shrinks with the column, so the
    whole week is always on screen. Everything is drawn in the
    stage's own palette: the panel's muted grey for words, the brand's blue for
    the squares.
  */
  .card {
    --cell-max: var(--z-space-4);
  }
  /* One row has the height seven share, so its squares are drawn larger. */
  .card.single {
    --cell-max: var(--z-space-6);
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
    min-width: 0;
    width: 100%;
    color: var(--z-panel-text-muted);
  }
  .head {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: var(--z-space-3);
    flex-wrap: wrap;
  }
  .title {
    font-size: var(--z-text-2xs);
    font-weight: var(--z-weight-semibold);
    letter-spacing: var(--z-tracking-wider);
    text-transform: uppercase;
  }
  .summary {
    font-size: var(--z-text-xs);
    color: var(--z-panel-text-subtle);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .grid {
    display: grid;
    grid-template-columns: auto repeat(24, minmax(0, var(--cell-max)));
    gap: var(--z-nudge-3) var(--z-nudge-2);
    align-items: center;
    justify-content: end;
  }
  .row {
    display: contents;
  }
  .day {
    padding-inline-end: var(--z-space-2);
    font-size: var(--z-text-2xs);
    line-height: 1;
    color: var(--z-panel-text-subtle);
  }
  .sq {
    --mix: 0%;
    --fill: color-mix(in srgb, var(--z-brand-runner-blue) var(--mix), var(--z-panel-raised));
    box-sizing: border-box;
    aspect-ratio: 1;
    width: 100%;
    border-radius: var(--z-nudge-2);
    background: var(--z-panel-raised);
    box-shadow: inset 0 0 0 var(--z-border-width) var(--z-panel-border);
    cursor: pointer;
    transition: transform var(--z-motion-fast) var(--z-ease);
  }
  .sq[data-level='1'] {
    --mix: 30%;
  }
  .sq[data-level='2'] {
    --mix: 52%;
  }
  .sq[data-level='3'] {
    --mix: 76%;
  }
  .sq[data-level='4'] {
    --mix: 100%;
  }
  .sq:not([data-level='0']) {
    background: var(--fill);
    box-shadow: none;
  }
  /* Punched through: the one shape a failure has here, the same as the matrix's. */
  .sq[data-failing]:not([data-level='0']) {
    --hole: var(--z-nudge-2);
    background: radial-gradient(
      circle,
      var(--z-panel-bg) 0 var(--hole),
      var(--fill) calc(var(--hole) + var(--z-nudge-1))
    );
  }
  .sq[data-failing][data-level='0'] {
    box-shadow: inset 0 0 0 var(--z-border-width) var(--z-brand-runner-blue);
  }
  /* Hours still to come: there, so the row is a day, but faint and not a target. */
  .sq[data-future] {
    opacity: 0.35;
    cursor: default;
  }
  .sq.current {
    outline: var(--z-border-width-thick) solid var(--z-brand-runner-blue);
    outline-offset: var(--z-nudge-1);
  }
  .sq:not([data-future]):hover {
    transform: scale(1.25);
  }
  .sq:focus-visible {
    outline: var(--z-focus-width) solid var(--z-brand-white);
    outline-offset: var(--z-focus-offset);
  }
  .blank {
    visibility: hidden;
    cursor: default;
  }
  .axis {
    grid-column: 2 / -1;
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    margin-top: var(--z-nudge-1);
    font-size: var(--z-text-2xs);
    line-height: 1;
    color: var(--z-panel-text-subtle);
    font-variant-numeric: tabular-nums;
  }
  /* Loading: the empty week breathes, which says "coming" without a spinner
     in the hero. Slow on purpose: the motion tokens are for controls. */
  .loading .sq {
    animation: pulse 1.2s var(--z-ease) infinite alternate;
  }
  @keyframes pulse {
    from {
      opacity: 0.6;
    }
    to {
      opacity: 1;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .sq,
    .loading .sq {
      transition: none;
      animation: none;
    }
    .sq:not([data-future]):hover {
      transform: none;
    }
  }
</style>
