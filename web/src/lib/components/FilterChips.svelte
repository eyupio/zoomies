<!--
  The primary filter of a list as a row of pills: All, then one per value,
  each with its count when the count is true of the rows under it. More than
  one can be in force at once, because an operator usually wants two or three
  states together ("busy and draining"); All is the empty selection. A chip in
  force says so with aria-pressed, a tick and the weight of its words, never
  by colour alone, so the state survives greyscale and forced colours.
-->
<script lang="ts" module>
  export interface FilterChipOption<V extends string = string> {
    value: V;
    label: string;
    /** Shown muted after the label when known. */
    count?: number;
  }
</script>

<script lang="ts" generics="T extends string">
  import { Check } from '@lucide/svelte';
  import { toggleChip } from './chip-selection';

  interface Props {
    options: readonly FilterChipOption<T>[];
    /** The values in force. Empty is "All". */
    selected: readonly T[];
    /** What is being filtered, for the group's accessible name: "Filter by runner state". */
    label: string;
    /** The first chip's words. */
    allLabel?: string;
    /** The count on the All chip, when known. */
    total?: number;
    onchange: (selected: T[]) => void;
    class?: string;
  }
  let {
    options,
    selected,
    label,
    allLabel = 'All',
    total,
    onchange,
    class: className = '',
  }: Props = $props();
  const allPressed = $derived(selected.length === 0);
</script>

<div class="chips {className}" role="group" aria-label={label}>
  <button type="button" class="chip" aria-pressed={allPressed} onclick={() => onchange([])}>
    {#if allPressed}<Check size={12} aria-hidden="true" />{/if}
    <span>{allLabel}</span>
    {#if total !== undefined}<span class="count">{total}</span>{/if}
  </button>
  {#each options as option (option.value)}
    {@const pressed = selected.includes(option.value)}
    <button
      type="button"
      class="chip"
      aria-pressed={pressed}
      onclick={() => onchange(toggleChip(selected, option.value))}
    >
      {#if pressed}<Check size={12} aria-hidden="true" />{/if}
      <span>{option.label}</span>
      {#if option.count !== undefined}<span class="count">{option.count}</span>{/if}
    </button>
  {/each}
</div>

<style>
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    min-height: var(--z-space-10);
    padding: var(--z-space-1) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border-strong);
    border-radius: var(--z-radius-full);
    background: var(--z-surface-raised);
    font: inherit;
    font-size: var(--z-text-sm);
    color: var(--z-text);
    cursor: pointer;
  }
  .chip:hover {
    background: var(--z-surface-hover);
  }
  .chip[aria-pressed='true'] {
    border-color: var(--z-text);
    background: var(--z-surface-sunken);
    font-weight: var(--z-weight-semibold);
  }
  .chip:focus-visible {
    outline: var(--z-focus-width) solid var(--z-focus-colour);
    outline-offset: var(--z-focus-offset);
  }
  .count {
    color: var(--z-text-muted);
    font-variant-numeric: tabular-nums;
  }
  @media (max-width: 768px) {
    .chip {
      min-height: var(--z-control-touch);
    }
  }
</style>
