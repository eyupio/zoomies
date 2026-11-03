<!--
  A block of facts about one thing: who, as what, until when; which build and
  which channel. A definition list on the dark stage, as the terminal is, so
  the facts read as the product's own record beside the words that explain
  them, and not as a box the words happen to sit in.

  Two columns on a desktop and one on a phone, where a value may be an
  unbroken string (a build hash, an address) that has to wrap inside the
  panel rather than widen the page. A value that is technical is drawn in the
  mono face, and one that is a state carries the status pill after it, so a
  state is never carried by colour alone.
-->
<script lang="ts">
  import type { StatusMeta } from '../status';
  import Badge from './Badge.svelte';

  export interface Fact {
    term: string;
    value: string;
    /** The value is an identifier, a version or an address: the mono face. */
    mono?: boolean;
    /** The value is a state: its pill, after the words. */
    status?: StatusMeta;
  }

  interface Props {
    /** The accessible name of the list: what these are facts about. */
    label: string;
    items: readonly Fact[];
    class?: string;
  }
  let { label, items, class: className = '' }: Props = $props();
</script>

<dl class="facts-panel {className}" aria-label={label}>
  {#each items as item (item.term)}
    <div>
      <dt>{item.term}</dt>
      <dd>
        <span class:mono={item.mono}>{item.value}</span>
        {#if item.status}<Badge status={item.status} size="sm" />{/if}
      </dd>
    </div>
  {/each}
</dl>

<style>
  .facts-panel {
    --z-text: var(--z-panel-text);
    --z-text-muted: var(--z-panel-text-muted);
    --z-text-subtle: var(--z-panel-text-subtle);
    --z-border: var(--z-panel-border);
    --z-surface: var(--z-panel-bg);
    --z-surface-raised: var(--z-panel-raised);
    --z-surface-sunken: var(--z-panel-raised);
    --z-accent: var(--z-brand-runner-blue);
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--z-space-3) var(--z-space-6);
    margin: 0;
    padding: var(--z-space-4) var(--z-space-5);
    border: var(--z-border-width) solid var(--z-panel-border);
    border-radius: var(--z-radius-lg);
    background: var(--z-panel-bg);
    color: var(--z-panel-text);
  }
  .facts-panel > div {
    display: grid;
    gap: var(--z-space-1);
    min-width: 0;
  }
  dt {
    color: var(--z-panel-text-subtle);
    font-size: var(--z-text-2xs);
    letter-spacing: var(--z-tracking-wider);
    text-transform: uppercase;
  }
  dd {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-2);
    margin: 0;
    min-width: 0;
    color: var(--z-panel-text);
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    overflow-wrap: anywhere;
  }
  dd > span {
    min-width: 0;
  }
  .mono {
    font-family: var(--z-font-mono);
  }
  @media (max-width: 480px) {
    .facts-panel {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
