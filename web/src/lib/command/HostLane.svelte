<!--
  One host on the command centre: its name, one word about it, a tile for each
  runner on it and one cell for the slots left.

  The lane says its own state where the verdict would otherwise have to name
  it, and takes that state's colour as its border, so the sentence at the top
  and the lane it means are the same colour. The cells are keyed by runner, so
  a tile keeps its identity when one before it leaves; the ones after it close
  up without their position being animated, since motion is for colour and
  the mark, never layout.
-->
<script lang="ts">
  import { CirclePause, Clock, TriangleAlert } from '@lucide/svelte';
  import { formatNumber } from '$lib/format';
  import type { Lane } from './lanes';
  import RunnerTile from './RunnerTile.svelte';

  let { lane }: { lane: Lane } = $props();

  const id = $props.id();
  const name = $derived(lane.host.name || lane.host.id || 'Host');
  // A host's measured CPU is only shown while a recent sample stands behind
  // it; a stale figure on a wall display reads as the host being calm.
  const cpu = $derived(
    lane.host.usage_fresh && lane.host.usage?.cpu_percent !== undefined
      ? `CPU ${formatNumber(Math.round(lane.host.usage.cpu_percent))}%`
      : null,
  );
</script>

<section
  class="lane"
  class:wide={lane.wide}
  data-tone={lane.tone ?? undefined}
  data-state={lane.state}
  aria-labelledby="{id}-name"
  data-testid="command-lane"
>
  <a class="head" href={lane.href}>
    <h3 class="name" id="{id}-name">{name}</h3>
    <span class="note">
      {#if lane.tone === 'pending'}<Clock size={14} aria-hidden="true" />{/if}
      {#if lane.tone === 'danger'}<TriangleAlert size={14} aria-hidden="true" />{/if}
      {#if lane.tone === 'draining'}<CirclePause size={14} aria-hidden="true" />{/if}
      {lane.note}
    </span>
    {#if cpu}<span class="cpu">{cpu}</span>{/if}
  </a>
  <div class="cells">
    {#each lane.cells as cell (cell.key)}
      <RunnerTile {cell} hostName={name} />
    {/each}
  </div>
</section>

<style>
  .lane {
    display: grid;
    align-content: start;
    gap: var(--z-space-2);
    min-width: 0;
    padding: var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-lg);
    background: var(--z-surface);
    transition: border-color var(--z-motion-fast) var(--z-ease);
  }
  .lane.wide {
    grid-column: 1 / -1;
  }
  .lane[data-tone='pending'] {
    border-color: var(--z-pending);
  }
  .lane[data-tone='danger'] {
    border-color: var(--z-danger);
  }
  .lane[data-tone='draining'] {
    border-color: var(--z-draining);
  }
  .head {
    display: flex;
    align-items: baseline;
    gap: var(--z-space-3);
    min-width: 0;
    border-radius: var(--z-radius-sm);
    color: var(--z-text);
    text-decoration: none;
  }
  .head:hover .name {
    color: var(--z-accent);
    text-decoration: underline;
    text-underline-offset: var(--z-underline-offset);
  }
  .head:focus-visible {
    outline: none;
    box-shadow: var(--z-focus-ring);
  }
  .name {
    min-width: 0;
    margin: 0;
    overflow: hidden;
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-semibold);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .note {
    display: inline-flex;
    flex: 1;
    align-items: center;
    gap: var(--z-space-1);
    min-width: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    white-space: nowrap;
  }
  [data-tone='pending'] .note {
    color: var(--z-pending);
  }
  [data-tone='danger'] .note {
    color: var(--z-danger);
  }
  [data-tone='draining'] .note {
    color: var(--z-draining);
  }
  .cpu {
    flex: none;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    font-variant-numeric: tabular-nums;
  }
  .cells {
    display: grid;
    /* A one-off measure: the narrowest a tile can be and still carry a job's
       name and its age on one line. */
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 11rem), 1fr));
    gap: var(--z-space-2);
  }
</style>
