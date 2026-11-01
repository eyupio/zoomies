<!--
  The Providers band's picture: a row per provider, a square per machine it
  may own, against its ceiling. Owned is solid, on its way half-shade, leaving
  faint, headroom hollow, and a machine that needs review is punched through
  as every failure on a stage is. "Up to 4 at once" becomes four squares with
  two filled, which is the sentence drawn.

  From the providers and machines the page already lists, so it costs no
  request and follows the stream as the page does.
-->
<script lang="ts">
  import type { Machine, Provider } from '$lib/api/types';
  import { MAX_SQUARES, providerMachines } from './provider-machines';

  interface Props {
    providers: readonly Provider[];
    machines: readonly Machine[];
  }
  let { providers, machines }: Props = $props();

  const data = $derived(providerMachines(providers, machines));
</script>

{#if data.rows.length > 0}
  <div class="pic" data-testid="band-machines">
    <div class="head">
      <span class="title">Machines against the ceiling</span>
      <span class="summary">{data.summary}</span>
    </div>
    <ul class="rows">
      {#each data.rows as row (row.id)}
        <li class="row" class:paused={row.paused}>
          <span class="name" title={row.name} aria-label={`${row.name}: ${row.note}`}
            >{row.name}</span
          >
          <span class="squares" aria-hidden="true">
            {#each row.squares.slice(0, MAX_SQUARES) as kind, i (i)}
              <i class="sq" data-kind={kind}></i>
            {/each}
            {#if row.squares.length > MAX_SQUARES}<span class="extra"
                >+{row.squares.length - MAX_SQUARES}</span
              >{/if}
          </span>
          <span class="note" aria-hidden="true">{row.note}</span>
        </li>
      {/each}
    </ul>
    <div class="foot">
      <span class="legend" aria-hidden="true">
        <span><i class="sq" data-kind="owned"></i>Owned</span>
        <span><i class="sq" data-kind="starting"></i>On the way</span>
        <span><i class="sq" data-kind="leaving"></i>Leaving</span>
        <span><i class="sq" data-kind="headroom"></i>Headroom</span>
        <span><i class="sq" data-kind="review"></i>Needs review</span>
      </span>
      {#if data.more > 0}<span class="more"
          >and {data.more} more {data.more === 1 ? 'provider' : 'providers'}</span
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
  }
  .paused .name {
    color: var(--z-panel-text-muted);
  }
  .squares {
    display: flex;
    align-items: center;
    gap: var(--z-nudge-3);
    min-width: 0;
    flex-wrap: wrap;
  }
  .extra {
    font-size: var(--z-text-2xs);
    color: var(--z-panel-text-subtle);
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
    width: var(--z-space-4);
    height: var(--z-space-4);
    border-radius: var(--z-nudge-2);
    background: var(--z-panel-raised);
    box-shadow: inset 0 0 0 var(--z-border-width) var(--z-panel-border);
  }
  .sq[data-kind='owned'] {
    background: var(--z-brand-runner-blue);
    box-shadow: none;
  }
  .sq[data-kind='starting'] {
    background: color-mix(in srgb, var(--z-brand-runner-blue) 52%, var(--z-panel-raised));
    box-shadow: none;
  }
  .sq[data-kind='leaving'] {
    background: color-mix(in srgb, var(--z-brand-runner-blue) 30%, var(--z-panel-raised));
    box-shadow: none;
  }
  .sq[data-kind='headroom'] {
    background: transparent;
    box-shadow: inset 0 0 0 var(--z-border-width) var(--z-brand-runner-blue);
  }
  /* Punched through: the one shape a failure has on the stage. */
  .sq[data-kind='review'] {
    background: radial-gradient(
      circle,
      var(--z-panel-bg) 0 var(--z-nudge-2),
      var(--z-brand-runner-blue) var(--z-nudge-3)
    );
    box-shadow: none;
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
