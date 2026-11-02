<!--
  The slot map beside a band's sentence: a row per host, a square per slot.

  Hosts and Runners are the same fourteen slots seen from two sides, so this is
  one picture with two colourings. On Hosts the question is how much of each
  machine is taken: a held slot is solid, a starting one half-shade, a free one
  empty. On Runners the question is what the runners are doing: busy solid, idle
  faint, starting half-shade, draining hollow. Both in the brand's blue, as every
  picture on the stage is, and a host that has stopped sending heartbeats is
  drawn dashed and says so, since colour is never the only carrier.

  A full fleet is a solid block and a sick host is a gap, which is the thing a
  sentence that counts slots cannot show. Each row is a link to its host, so the
  picture is a way in and not only a picture. Past seven hosts the rest are a
  count: a fleet of forty hosts is a figure, not a picture.
-->
<script lang="ts">
  import type { Host, Runner } from '$lib/api/types';
  import Tooltip from '$lib/components/Tooltip.svelte';
  import { href } from '$lib/router';
  import { slotMap } from './slot-map';

  interface Props {
    hosts: readonly Host[];
    runners: readonly Runner[];
    /** The page's question: how much is taken (Hosts) or what the runners do (Runners). */
    detail: 'occupancy' | 'state';
    title?: string;
  }
  let { hosts, runners, detail, title = 'Slots right now' }: Props = $props();

  const map = $derived(slotMap(hosts, runners, detail));
  const legend = $derived(
    detail === 'occupancy'
      ? [
          { kind: 'busy', label: 'In use' },
          { kind: 'starting', label: 'Starting' },
          { kind: 'free', label: 'Free' },
        ]
      : [
          { kind: 'busy', label: 'Busy' },
          { kind: 'idle', label: 'Idle' },
          { kind: 'starting', label: 'Starting' },
          { kind: 'draining', label: 'Draining' },
        ],
  );
</script>

{#if map.rows.length > 0}
  <div class="map" data-testid="band-slot-map" data-detail={detail}>
    <div class="head">
      <span class="title">{title}</span>
      <span class="summary">{map.summary}</span>
    </div>
    <ul class="rows">
      {#each map.rows as row (row.id)}
        <li class="row" class:unreachable={row.unreachable}>
          <Tooltip bare whenTruncated text={row.name}>
            <a class="name" href={href(`/hosts/${row.id}`)} aria-label={`${row.name}: ${row.note}`}
              >{row.name}</a
            >
          </Tooltip>
          <span class="slots" style:--n={row.slots.length} aria-hidden="true">
            {#each row.slots as kind, i (i)}
              <i
                class="sq"
                data-kind={detail === 'occupancy' && kind !== 'free' && kind !== 'starting'
                  ? 'busy'
                  : kind}
              ></i>
            {/each}
          </span>
          <span class="note" aria-hidden="true">{row.note}</span>
        </li>
      {/each}
    </ul>
    <div class="foot">
      <span class="legend" aria-hidden="true">
        {#each legend as item (item.kind)}
          <span><i class="sq" data-kind={item.kind}></i>{item.label}</span>
        {/each}
        <span><i class="sq dashed"></i>Not heard from</span>
      </span>
      {#if map.more > 0}<span class="more"
          >and {map.more} more {map.more === 1 ? 'host' : 'hosts'}</span
        >{/if}
    </div>
  </div>
{/if}

<style>
  /*
    Drawn in the stage's own palette: the panel's muted greys for words and the
    brand's blue for the squares. A square is at most the size of the page's
    small squares and shrinks with the row, so a host of thirty-two slots is a
    longer row of smaller squares rather than one that wraps.
  */
  .map {
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
    grid-template-columns: auto minmax(0, 1fr) auto;
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
  .slots {
    display: grid;
    grid-template-columns: repeat(var(--n), minmax(0, var(--z-space-4)));
    gap: var(--z-nudge-3);
    min-width: 0;
  }
  .note {
    font-size: var(--z-text-2xs);
    color: var(--z-panel-text-subtle);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .sq {
    display: block;
    box-sizing: border-box;
    aspect-ratio: 1;
    width: 100%;
    border-radius: var(--z-nudge-2);
    background: var(--z-panel-raised);
    box-shadow: inset 0 0 0 var(--z-border-width) var(--z-panel-border);
  }
  .sq[data-kind='busy'] {
    background: var(--z-brand-runner-blue);
    box-shadow: none;
  }
  .sq[data-kind='starting'] {
    background: color-mix(in srgb, var(--z-brand-runner-blue) 52%, var(--z-panel-raised));
    box-shadow: none;
  }
  .sq[data-kind='idle'] {
    background: color-mix(in srgb, var(--z-brand-runner-blue) 30%, var(--z-panel-raised));
    box-shadow: none;
  }
  .sq[data-kind='draining'] {
    background: transparent;
    box-shadow: inset 0 0 0 var(--z-border-width) var(--z-brand-runner-blue);
  }
  /* A host no heartbeat has come from: its squares are outlines, whatever they hold. */
  .unreachable .sq,
  .sq.dashed {
    background: transparent;
    box-shadow: none;
    border: var(--z-border-width) dashed var(--z-panel-text-subtle);
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
  .legend .sq {
    width: var(--z-space-2);
    height: var(--z-space-2);
  }
</style>
