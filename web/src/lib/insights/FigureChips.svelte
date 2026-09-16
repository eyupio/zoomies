<!--
  Which figures are on the chart: one chip each, carrying the stroke the
  chart will draw it with, so the row is the legend and the switchboard at
  once. Pointing at a chip singles its line out without going near the chart.

  Both trend panels use this, which is how the fleet activity chart and the
  usage chart stay the same control rather than two that drifted.
-->
<script lang="ts" generics="S extends Series">
  import Stroke from './Stroke.svelte';
  import type { Series } from './plot';

  let {
    label,
    figures,
    enabled,
    group,
    ontoggle,
    onchip,
  }: {
    /** What the group of chips is, for its accessible name. */
    label: string;
    figures: readonly S[];
    enabled: readonly string[];
    /** A hairline is drawn wherever this changes: jobs from runners. */
    group?: (figure: S) => string;
    ontoggle: (key: S['key']) => void;
    /** The chip under the pointer or the focus, for singling its line out. */
    onchip: (key: S['key'] | null) => void;
  } = $props();
</script>

<div class="figures" role="group" aria-label={label}>
  {#each figures as figure, i (figure.key)}
    {#if i > 0 && group && group(figures[i - 1]!) !== group(figure)}
      <span class="divider" aria-hidden="true"></span>
    {/if}
    <button
      type="button"
      class="figure"
      aria-pressed={enabled.includes(figure.key)}
      title={`${figure.label}: ${figure.hint}`}
      onclick={() => ontoggle(figure.key)}
      onpointerenter={() => onchip(figure.key)}
      onpointerleave={() => onchip(null)}
      onfocus={() => onchip(figure.key)}
      onblur={() => onchip(null)}
    >
      <Stroke series={figure} off={!enabled.includes(figure.key)} />
      {figure.label}
    </button>
  {/each}
</div>

<style>
  .figures {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-1);
  }
  .divider {
    width: var(--z-border-width);
    align-self: stretch;
    margin: 0 var(--z-space-1);
    background: var(--z-border);
  }
  .figure {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
    padding: var(--z-nudge-2) var(--z-space-2);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface);
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
    cursor: pointer;
  }
  .figure[aria-pressed='true'] {
    border-color: var(--z-border-strong);
    background: var(--z-surface-sunken);
    color: var(--z-text);
  }
  /* Every target rises to a thumb's height where the pointer is coarse. */
  @media (pointer: coarse) {
    .figure {
      min-height: var(--z-control-touch);
    }
  }
</style>
