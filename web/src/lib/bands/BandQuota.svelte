<!--
  The Installations band's picture: how much of each App's hourly API
  allowance is left, as a bar, with when it resets. The band's figure counts
  the connections below a tenth; this is that tenth drawn, lowest first, since
  quota is what breaks an installation.

  From the readings the page already takes after its list lands, so it costs
  no request of its own. A connection with no reading yet is a dashed track,
  never a full or an empty bar: no reading and no quota are opposite news.
-->
<script lang="ts">
  import type { Installation } from '$lib/api/types';
  import { onClockTick } from '$lib/format';
  import { quotaBars, type QuotaReading } from './quota-bars';

  interface Props {
    installations: readonly Installation[];
    rates: Readonly<Record<string, QuotaReading>>;
  }
  let { installations, rates }: Props = $props();

  let now = $state(Date.now());
  $effect(() => onClockTick((at) => (now = at)));
  const data = $derived(quotaBars(installations, rates, now));
</script>

{#if data.rows.length > 0}
  <div class="pic" data-testid="band-quota">
    <div class="head">
      <span class="title">API quota left this hour</span>
      <span class="summary">{data.summary}</span>
    </div>
    <ul class="rows">
      {#each data.rows as row (row.id)}
        <li class="row" class:low={row.low}>
          <span class="name" title={row.name} aria-label={`${row.name}: ${row.note}`}
            >{row.name}</span
          >
          <span class="track" class:unread={row.share === null} aria-hidden="true">
            {#if row.share !== null}<i style:width={`${(row.share * 100).toFixed(1)}%`}></i>{/if}
          </span>
          <span class="note" aria-hidden="true">{row.note}</span>
        </li>
      {/each}
    </ul>
    <div class="foot">
      <span class="legend" aria-hidden="true"
        >Edged in white below a tenth, which is when the band counts it as low.</span
      >
      {#if data.more > 0}<span class="more"
          >and {data.more} more {data.more === 1 ? 'connection' : 'connections'}</span
        >{/if}
    </div>
  </div>
{/if}

<style>
  .pic {
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
  .more,
  .legend {
    font-size: var(--z-text-xs);
    color: var(--z-panel-text-subtle);
    font-variant-numeric: tabular-nums;
  }
  .legend {
    font-size: var(--z-text-2xs);
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
    max-width: 10rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    color: var(--z-panel-text);
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
    background: var(--z-brand-runner-blue);
  }
  .track.unread {
    background: transparent;
    box-shadow: none;
    border: var(--z-border-width) dashed var(--z-panel-text-subtle);
    box-sizing: border-box;
  }
  .low .track {
    box-shadow: inset 0 0 0 var(--z-border-width) var(--z-panel-text);
  }
  .note {
    font-size: var(--z-text-2xs);
    color: var(--z-panel-text-subtle);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
</style>
