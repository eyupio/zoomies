<!--
  The Pools band's picture: each enabled pool as a bar, busy over live against
  its ceiling, with the floor as a tick. The sentence says one pool is at its
  ceiling; this shows which, and how far the rest are from theirs.

  The Overview's pool panel draws the same figures in status colour. On the
  stage there is none: busy is the brand's blue, live its half-shade, and the
  ceiling is where the bar ends, so a full bar is a pool that cannot grow. A
  pool at its ceiling with work waiting comes first and its edge is drawn in
  white, since the picture exists to show that one. Each row is a link to its
  pool, and past six pools the rest are a count.
-->
<script lang="ts">
  import type { Pool, Stats } from '$lib/api/types';
  import { href } from '$lib/router';
  import { poolBars } from './pool-bars';

  interface Props {
    pools: readonly Pool[];
    stats: Stats | null;
  }
  let { pools, stats }: Props = $props();

  const bars = $derived(poolBars(pools, stats));
  const pct = (part: number, whole: number) =>
    whole > 0 ? `${Math.min(100, (part / whole) * 100).toFixed(1)}%` : '0%';
</script>

{#if bars.rows.length > 0}
  <div class="bars" data-testid="band-pool-bars">
    <div class="head">
      <span class="title">Pools against their ceilings</span>
      <span class="summary">{bars.summary}</span>
    </div>
    <ul class="rows">
      {#each bars.rows as row (row.id)}
        <li class="row" class:ceiling={row.atCeiling}>
          <a
            class="name"
            href={href(`/pools/${row.id}`)}
            aria-label={`${row.name}: ${row.note}`}
            title={row.name}>{row.name}</a
          >
          <span class="track" aria-hidden="true">
            <i class="live" style:width={pct(row.live, Math.max(row.max, row.live))}></i>
            <i class="busy" style:width={pct(row.busy, Math.max(row.max, row.live))}></i>
            {#if row.min > 0}<i class="tick" style:left={pct(row.min, Math.max(row.max, row.live))}
              ></i>{/if}
          </span>
          <span class="note" aria-hidden="true">{row.note}</span>
        </li>
      {/each}
    </ul>
    <div class="foot">
      <span class="legend" aria-hidden="true">
        <span><i class="sw busy"></i>Busy</span>
        <span><i class="sw live"></i>Live</span>
        <span><i class="sw tick"></i>Minimum</span>
        <span>Bar end is the maximum</span>
      </span>
      {#if bars.more > 0}<span class="more"
          >and {bars.more} more {bars.more === 1 ? 'pool' : 'pools'}</span
        >{/if}
    </div>
  </div>
{/if}

<style>
  .bars {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
    min-width: 0;
    width: 100%;
    color: var(--z-panel-text-muted);
  }
  .head,
  .foot {
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
  .summary,
  .more {
    font-size: var(--z-text-xs);
    color: var(--z-panel-text-subtle);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .rows {
    display: grid;
    grid-template-columns: auto minmax(6rem, 1fr) auto;
    gap: var(--z-space-2) var(--z-space-3);
    align-items: center;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .row {
    display: contents;
  }
  .name {
    max-width: 12rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    color: var(--z-panel-text);
    text-decoration: none;
    border-radius: var(--z-radius-sm);
  }
  .name:hover {
    text-decoration: underline;
  }
  .name:focus-visible {
    outline: var(--z-focus-width) solid var(--z-brand-runner-blue);
    outline-offset: var(--z-focus-offset);
  }
  .track {
    position: relative;
    display: block;
    height: var(--z-space-2);
    border-radius: var(--z-radius-sm);
    background: var(--z-panel-raised);
    box-shadow: inset 0 0 0 var(--z-border-width) var(--z-panel-border);
    overflow: hidden;
  }
  .track i {
    position: absolute;
    inset-block: 0;
    inset-inline-start: 0;
    display: block;
  }
  .track .live {
    background: color-mix(in srgb, var(--z-brand-runner-blue) 38%, var(--z-panel-raised));
  }
  .track .busy {
    background: var(--z-brand-runner-blue);
  }
  .track .tick {
    width: var(--z-border-width-thick);
    background: var(--z-panel-text);
    opacity: 0.7;
  }
  /* The one the picture is for: its edge in white, since it has reached it. */
  .ceiling .track {
    box-shadow: inset 0 0 0 var(--z-border-width) var(--z-panel-text);
  }
  .note {
    font-size: var(--z-text-2xs);
    color: var(--z-panel-text-subtle);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-1) var(--z-space-3);
    font-size: var(--z-text-2xs);
    color: var(--z-panel-text-subtle);
    align-items: center;
  }
  .legend span {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
  }
  .sw {
    display: inline-block;
    width: var(--z-space-2);
    height: var(--z-space-2);
    border-radius: var(--z-nudge-2);
  }
  .sw.busy {
    background: var(--z-brand-runner-blue);
  }
  .sw.live {
    background: color-mix(in srgb, var(--z-brand-runner-blue) 38%, var(--z-panel-raised));
  }
  .sw.tick {
    width: var(--z-border-width-thick);
    background: var(--z-panel-text);
    opacity: 0.7;
  }
</style>
