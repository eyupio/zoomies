<!--
  The Usage band's picture: the chosen range's execution hours, one column per
  interval of the report, summed across its groups. The figures beside it say
  how many hours the range held; this says when they were spent, which is the
  question that sends somebody to the chart under it.

  It reads the report the page already fetched, so it costs no request and
  changes with the range and the grouping as the figures do. The columns are
  the report's own intervals, drawn as they come, because the page's chart
  reads the same buckets and the two must agree. In the brand's blue, as every
  picture on the stage is, with the tallest named in words.
-->
<script lang="ts">
  import type { UsageRow } from '$lib/api/types';
  import { usageColumns } from './usage-columns';

  interface Props {
    rows: readonly UsageRow[];
  }
  let { rows }: Props = $props();

  const SHORT = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short' });
  const CLOCK = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' });

  const data = $derived(usageColumns(rows));
  /** Hours as the band's figures write them. */
  const hours = (h: number) => `${h.toFixed(h >= 10 ? 0 : 1)} h`;
  /** A day's worth of columns is told by the clock, a longer range by the date. */
  const spansDays = $derived.by(() => {
    const c = data.columns;
    if (c.length < 2) return false;
    return c[c.length - 1]!.at - c[0]!.at > 36 * 3_600_000;
  });
  const label = (at: number) =>
    spansDays ? SHORT.format(new Date(at)) : CLOCK.format(new Date(at));
  const first = $derived(data.columns[0] ?? null);
  const last = $derived(data.columns[data.columns.length - 1] ?? null);
  const middle = $derived(data.columns[Math.floor(data.columns.length / 2)] ?? null);
</script>

{#if data.columns.length >= 2}
  <div class="usage" data-testid="band-usage-columns">
    <div class="head">
      <span class="title">Execution hours across the range</span>
      <span class="summary"
        >{data.peak ? `peak ${hours(data.peak.hours)} at ${label(data.peak.at)}` : 'no work'}</span
      >
    </div>
    <div
      class="cols"
      role="img"
      aria-label={`${data.columns.length} intervals${data.peak ? `, the busiest ${hours(data.peak.hours)} at ${label(data.peak.at)}` : ', none with work'}`}
    >
      {#each data.columns as col (col.at)}
        <i
          class:quiet={col.hours === 0}
          style:height={data.max > 0
            ? `${Math.max(3, (col.hours / data.max) * 100).toFixed(1)}%`
            : '3%'}
          title={`${label(col.at)}: ${hours(col.hours)}, ${col.completed} finished`}
        ></i>
      {/each}
    </div>
    <div class="axis" aria-hidden="true">
      <span>{first ? label(first.at) : ''}</span>
      <span>{middle && middle !== first && middle !== last ? label(middle.at) : ''}</span>
      <span>{last ? label(last.at) : ''}</span>
    </div>
  </div>
{/if}

<style>
  .usage {
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
  .cols {
    display: flex;
    align-items: flex-end;
    gap: var(--z-nudge-2);
    height: var(--z-space-16);
    min-width: 0;
  }
  .cols i {
    display: block;
    flex: 1 1 0;
    min-width: 0;
    border-radius: var(--z-nudge-2) var(--z-nudge-2) 0 0;
    background: var(--z-brand-runner-blue);
  }
  .cols i.quiet {
    background: var(--z-panel-raised);
    box-shadow: inset 0 0 0 var(--z-border-width) var(--z-panel-border);
  }
  .axis {
    display: grid;
    grid-template-columns: 1fr 1fr 1fr;
    font-size: var(--z-text-2xs);
    color: var(--z-panel-text-subtle);
    font-variant-numeric: tabular-nums;
  }
  .axis span:nth-child(2) {
    text-align: center;
  }
  .axis span:last-child {
    text-align: end;
  }
</style>
