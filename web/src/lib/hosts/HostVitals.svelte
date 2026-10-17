<!--
  How loaded the machine is: CPU, memory and disk, each a share of the whole
  machine with the figures it is a share of beside it.

  Bars rather than bare numbers, for the reason ResourceBar gives: "how full is
  this" is a question about a proportion, and the answer is in the shape. The
  rows are columns of one grid, so the bars start and end together whatever the
  figures beside them say, and the card reads as a table of three.

  A figure past its line is amber, takes a triangle and says so in its label.
  The line is the scheduler's own for CPU and memory and the doctor's for the
  disk (vitals.ts), so the colour never claims more than the product does. Each
  row's label is a sentence that carries the same figures, and the words around
  it are hidden from assistive technology so nothing is read twice.
-->
<script lang="ts">
  import { TriangleAlert } from '@lucide/svelte';
  import Tooltip from '$lib/components/Tooltip.svelte';
  import type { Vitals } from './vitals';

  interface Props {
    vitals: Vitals;
    /** The host's name, which the group's label carries. */
    name: string;
    class?: string;
  }

  let { vitals, name, class: className = '' }: Props = $props();
</script>

<div class="vitals {className}" role="group" aria-label="Utilisation of {name}">
  {#if vitals.unavailable}
    <!-- One sentence rather than three rows of dashes: the rows would all say
         the same thing, and a card with nothing to measure should say why. -->
    <p class="note">{vitals.unavailable}</p>
  {:else}
    {#each vitals.items as vital (vital.key)}
      <Tooltip bare text={vital.summary}>
        <div
          class="vital"
          class:tight={vital.tight}
          class:unmeasured={vital.percent === null}
          data-testid="host-vital-{vital.key}"
        >
          <span class="label" aria-hidden="true">{vital.label}</span>
          <span class="track" role="img" aria-label={vital.summary}>
            <span class="fill" style="width: {Math.min(100, vital.percent ?? 0)}%"></span>
          </span>
          <span class="value tabular" aria-hidden="true">
            {#if vital.tight}<TriangleAlert size={12} aria-hidden="true" />{/if}{vital.value}
          </span>
          <span class="detail tabular" aria-hidden="true">{vital.detail}</span>
        </div>
      </Tooltip>
    {/each}
  {/if}
</div>

<style>
  /* Each column has a least width, so a row of cards draws its bars the same
     length whatever figures sit beside them: the widest label, a value with its
     warning triangle, and a pair like "481 / 512 GB". A figure longer than that
     widens its column and no more. */
  .vitals {
    display: grid;
    grid-template-columns:
      minmax(3.5rem, auto) minmax(var(--z-space-8), 1fr) minmax(2.75rem, auto)
      minmax(5.25rem, auto);
    align-items: center;
    column-gap: var(--z-space-3);
    row-gap: var(--z-space-2);
    min-width: 0;
  }
  .vital {
    display: grid;
    grid-column: 1 / -1;
    grid-template-columns: subgrid;
    align-items: center;
    min-width: 0;
  }
  .note {
    grid-column: 1 / -1;
    margin: 0;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-subtle);
  }
  .label {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .track {
    display: block;
    height: calc(var(--z-space-1) + var(--z-nudge-2));
    border-radius: var(--z-radius-full);
    background: var(--z-surface-sunken);
    overflow: hidden;
  }
  .fill {
    display: block;
    height: 100%;
    border-radius: var(--z-radius-full);
    background: var(--z-neutral);
    transition:
      width var(--z-motion-base) var(--z-ease),
      background-color var(--z-motion-fast) var(--z-ease);
  }
  /* The same pending hue ResourceBar and the disk figure take for "this is what
     will stop the next runner": one colour for it, wherever it appears. */
  .tight .fill {
    background: var(--z-pending);
  }
  .value {
    display: inline-flex;
    align-items: center;
    justify-content: flex-end;
    gap: var(--z-space-1);
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .tight .value {
    color: var(--z-pending);
  }
  .unmeasured .value {
    font-weight: var(--z-weight-medium);
    color: var(--z-text-subtle);
  }
  .detail {
    justify-self: end;
    font-size: var(--z-text-2xs);
    color: var(--z-text-subtle);
    white-space: nowrap;
  }
  /* Windows high contrast replaces every background with the page's own, which
     would take both the track and its fill and leave a row with no bar. The
     figures beside it say everything either way; this keeps the picture too, in
     the system's colours, and the triangle still marks a figure past its line. */
  @media (forced-colors: active) {
    .track {
      border: var(--z-border-width) solid CanvasText;
    }
    .fill {
      background: Highlight;
    }
  }
</style>
