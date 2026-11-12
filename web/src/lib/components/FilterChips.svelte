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
    /** One line an operator can act on, in the shared tooltip, for a chip whose word is not enough. */
    hint?: string;
  }
</script>

<script lang="ts" generics="T extends string">
  import { Check } from '@lucide/svelte';
  import Tooltip from './Tooltip.svelte';
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
    /**
     * Nothing a chip says is in force, All included: the filter is something
     * the chips cannot express (two states at once, a facet), so pressing All
     * would be a lie. The row is drawn with no chip pressed.
     */
    none?: boolean;
    class?: string;
  }
  let {
    options,
    selected,
    label,
    allLabel = 'All',
    total,
    onchange,
    none = false,
    class: className = '',
  }: Props = $props();
  const allPressed = $derived(!none && selected.length === 0);
</script>

<div class="chips {className}" role="group" aria-label={label}>
  <button type="button" class="chip" aria-pressed={allPressed} onclick={() => onchange([])}>
    {#if allPressed}<Check size={12} aria-hidden="true" />{/if}
    <span>{allLabel}</span>
    {#if total !== undefined}<span class="count">{total}</span>{/if}
  </button>
  {#each options as option (option.value)}
    {@const pressed = !none && selected.includes(option.value)}
    <!-- The hint rides the shared tooltip, bare so the row lays the button out
         as it would without one; with no hint the wrapper is inert, with no
         description and no bubble, so the chips' accessibility tree is the
         button alone either way. -->
    <Tooltip bare text={option.hint ?? ''}>
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
    </Tooltip>
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
    border-radius: var(--z-radius-sm);
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
    /* One line that scrolls sideways rather than wrapping, on every list: a
       row of seven wraps to two or three lines before the first record, and
       when only some pages scrolled the same control behaved two ways.
       The line runs edge to edge of whatever holds it and gives the holder's
       gutter back as inset, so the first chip lines up with the text beside
       it and a chip cut at the edge says there is more. The holder names its
       gutter in --chips-inset (a Panel's toolbar does); with none, the row
       is as wide as its box. */
    .chips {
      flex-wrap: nowrap;
      overflow-x: auto;
      margin-inline: calc(-1 * var(--chips-inset, 0px));
      padding-inline: var(--chips-inset, 0px);
      scroll-padding-inline: var(--chips-inset, 0px);
      scrollbar-width: none;
    }
    .chips::-webkit-scrollbar {
      display: none;
    }
    .chips .chip {
      flex: none;
    }
  }
</style>
