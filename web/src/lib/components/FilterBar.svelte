<!--
  The controls that narrow a grid, and a chip for each filter currently applied.
  Every chip is individually removable, and there is always a way to clear the
  lot -- filters that cannot be seen are filters that get blamed on the server.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import { X } from '@lucide/svelte';
  import { tableFocus } from '../state/table-focus.svelte';

  export interface FilterChip {
    id: string;
    /** The field: "State". */
    label: string;
    /** The value: "busy". */
    value: string;
    onremove: () => void;
    /**
     * A filter some other control already says (a pressed chip in the card's
     * head), so on the whole page a chip here would say it twice. A table's
     * focus view hides that control, and there this is the only sign of it.
     */
    focusOnly?: boolean;
  }

  interface Props {
    chips?: readonly FilterChip[];
    onclear?: () => void;
    /** The search field and the filter menus. */
    children?: Snippet;
    class?: string;
  }

  let { chips = [], onclear, children, class: className = '' }: Props = $props();

  const shown = $derived(tableFocus.active ? chips : chips.filter((chip) => !chip.focusOnly));
  // Clear all takes off what this bar holds; a focus-only chip alone is some
  // other control's filter, and its own cross is how it comes off.
  const clearable = $derived(shown.some((chip) => !chip.focusOnly));
</script>

<!-- Kept in a table's focus view (`data-focus-keep`): it says which filters
     narrow the rows and is how they come off, so hiding it with the rest of the
     page would leave a narrowed list with no sign of why. Only that row is kept:
     the controls that choose a filter are `data-focus-head` and go with the
     card's head, since choosing is done on the whole page. -->
<div class="filter-bar {className}" data-focus-keep>
  {#if children}
    <div class="controls" data-focus-head>{@render children()}</div>
  {/if}
  {#if shown.length > 0}
    <div class="chips" role="group" aria-label="Filters in effect">
      {#each shown as chip (chip.id)}
        <span class="chip">
          <span class="chip-label">{chip.label}</span>
          <span class="chip-value">{chip.value}</span>
          <button
            type="button"
            aria-label="Remove the {chip.label} filter {chip.value}"
            onclick={chip.onremove}
          >
            <X size={11} aria-hidden="true" />
          </button>
        </span>
      {/each}
      {#if onclear && clearable}
        <button type="button" class="clear" onclick={onclear}>Clear all</button>
      {/if}
    </div>
  {/if}
</div>

<style>
  .filter-bar {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
  }
  .controls {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
  .chips {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
    height: var(--z-space-5);
    padding: 0 var(--z-space-1) 0 var(--z-space-2);
    border: var(--z-border-width) solid var(--z-accent-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-accent-subtle);
    font-size: var(--z-text-xs);
  }
  .chip-label {
    color: var(--z-text-muted);
  }
  .chip-value {
    color: var(--z-text);
    font-weight: var(--z-weight-medium);
  }
  .chip button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--z-space-4);
    height: var(--z-space-4);
    border: 0;
    border-radius: var(--z-radius-sm);
    background: transparent;
    color: var(--z-text-muted);
    cursor: pointer;
  }
  .chip button:hover {
    background: var(--z-surface);
    color: var(--z-text);
  }
  .clear {
    border: 0;
    background: transparent;
    color: var(--z-accent);
    font-family: inherit;
    font-size: var(--z-text-xs);
    text-decoration: underline;
    cursor: pointer;
  }
</style>
