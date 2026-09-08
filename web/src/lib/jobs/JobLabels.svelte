<!--
  The labels a job asked for, or a pool answers to.

  In a table cell only the first few fit, so the rest are counted and the full
  set is in the title -- an operator scanning a `runs-on` typo needs to see the
  labels themselves, not a truncated string.
-->
<script lang="ts">
  import { joinWords } from '$lib/format';

  interface Props {
    labels?: readonly string[];
    /** How many to render before counting the rest. 0 shows them all. */
    max?: number;
    /**
     * Let the chips wrap onto more lines. Off in a grid, where three long
     * labels wrapped one per line made every row in the table that tall; on
     * wherever the full set is being shown deliberately.
     */
    wrap?: boolean;
    class?: string;
  }

  let { labels = [], max = 3, wrap = false, class: className = '' }: Props = $props();

  const shown = $derived(max > 0 ? labels.slice(0, max) : [...labels]);
  const rest = $derived(Math.max(0, labels.length - shown.length));
  const all = $derived(labels.length > 0 ? joinWords([...labels]) : '');
</script>

{#if labels.length === 0}
  <span class="none">No labels</span>
{:else}
  <span class="labels {className}" class:wrap title={all}>
    {#each shown as label (label)}
      <span class="label mono">{label}</span>
    {/each}
    {#if rest > 0}
      <span class="more tabular">+{rest}</span>
      <span class="sr-only">, and {joinWords(labels.slice(shown.length).map(String))}</span>
    {/if}
  </span>
{/if}

<style>
  .labels {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-1);
    min-width: 0;
  }
  .label {
    padding: 0 var(--z-space-1);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface-sunken);
    color: var(--z-text-muted);
    font-size: var(--z-text-2xs);
    line-height: var(--z-leading-2xs);
    white-space: nowrap;
  }
  /* One line unless asked otherwise, and the counter is never the thing that
     gets pushed out: it is the only sign that there is more to see. */
  .labels:not(.wrap) {
    flex-wrap: nowrap;
    overflow: hidden;
  }
  .labels:not(.wrap) .label {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .labels:not(.wrap) .more {
    flex: none;
  }
  .more {
    color: var(--z-text-subtle);
    font-size: var(--z-text-2xs);
  }
  .none {
    color: var(--z-text-subtle);
  }
</style>
