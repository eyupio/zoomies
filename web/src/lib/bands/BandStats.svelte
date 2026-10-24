<!--
  The figures a band carries, in one row.

  Two presentations of the same list. Inside a band they are neutral white on
  Zoomies Black with no status colour, because the stage is the same in both themes.
  On their own, when a band is switched off or a phone is hiding it, they are an
  ordinary panel on the page's surface, so an operator who does not want the dog
  still has the numbers. The component never decides which: PageBand does.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import Duration from '$lib/components/Duration.svelte';
  import Tooltip from '$lib/components/Tooltip.svelte';
  import { NO_VALUE } from '$lib/format';
  import { jumpToSection } from './jump';
  import type { BandMore, BandStat } from './stats';

  interface Props {
    stats: readonly BandStat[];
    more?: BandMore;
    /** `band` is on the stage; `panel` stands alone on the page. */
    variant?: 'band' | 'panel';
    /** What the strip is called to a screen reader. */
    label?: string;
  }
  let { stats, more, variant = 'panel', label = 'Key figures' }: Props = $props();

  /**
   * Pressing a figure may also narrow the list below it. The jump waits for
   * that change to land, because the filters above the list can add or remove
   * a row and move the section under the pointer.
   */
  async function reveal(stat: BandStat): Promise<void> {
    stat.onselect?.();
    if (!stat.section) return;
    await tick();
    jumpToSection(stat.section);
  }

  function follow(event: MouseEvent, stat: BandStat): void {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey)
      return;
    event.preventDefault();
    void reveal(stat);
  }
</script>

{#snippet figure(stat: BandStat)}
  <span class="label">{stat.label}</span>
  <strong class="value">
    {#if stat.since}<Duration from={stat.since} live />{:else}{stat.value ?? NO_VALUE}{/if}
  </strong>
{/snippet}

{#snippet item(stat: BandStat)}
  {#if stat.onselect}
    <button
      type="button"
      class="stat interactive"
      class:active={stat.active}
      data-tone={stat.tone}
      aria-pressed={stat.active}
      onclick={() => void reveal(stat)}
    >
      {@render figure(stat)}
    </button>
  {:else if stat.section}
    <a
      class="stat interactive"
      class:active={stat.active}
      data-tone={stat.tone}
      href="#{stat.section}"
      onclick={(event) => follow(event, stat)}
    >
      {@render figure(stat)}
    </a>
  {:else if stat.href}
    <a class="stat interactive" data-tone={stat.tone} href={stat.href}>
      {@render figure(stat)}
    </a>
  {:else}
    <div class="stat" data-tone={stat.tone}>{@render figure(stat)}</div>
  {/if}
{/snippet}

<ul class="strip {variant}" aria-label={label}>
  {#each stats as stat (stat.label)}
    <li>
      <!-- The shared bubble, not the browser's title: it shows for the keyboard too. -->
      {#if stat.hint}
        <Tooltip bare text={stat.hint}>{@render item(stat)}</Tooltip>
      {:else}
        {@render item(stat)}
      {/if}
    </li>
  {/each}
  {#if more}
    <li class="more"><a href={more.href}>{more.label}<span aria-hidden="true"> ↗</span></a></li>
  {/if}
</ul>

<style>
  .strip {
    display: flex;
    flex-wrap: wrap;
    align-items: stretch;
    gap: var(--z-space-2) var(--z-space-3);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  li {
    display: flex;
    min-width: 0;
  }
  .stat {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-1);
    min-width: 0;
    padding: var(--z-space-2) var(--z-space-3);
    border: var(--z-border-width) solid transparent;
    border-radius: var(--z-radius-md);
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    text-decoration: none;
  }
  button.stat {
    cursor: pointer;
  }
  .label {
    font-size: var(--z-text-2xs);
    font-weight: var(--z-weight-semibold);
    letter-spacing: var(--z-tracking-wider);
    text-transform: uppercase;
  }
  .value {
    font-size: var(--z-text-lg);
    line-height: var(--z-leading-lg);
    font-weight: var(--z-weight-semibold);
    font-variant-numeric: tabular-nums;
  }
  .more {
    align-items: flex-end;
    margin-inline-start: auto;
    padding-block: var(--z-space-2);
  }
  .more a {
    font-size: var(--z-text-sm);
    text-decoration: none;
    border-radius: var(--z-radius-sm);
  }
  .more a:hover {
    text-decoration: underline;
  }
  .interactive {
    transition:
      background-color var(--z-motion-fast) var(--z-ease),
      border-color var(--z-motion-fast) var(--z-ease);
  }

  /* On the stage: neutral white and the brand's blue, as the headline is. */
  .band .stat {
    border-color: var(--z-panel-border);
    background: var(--z-panel-raised);
    color: var(--z-panel-text);
  }
  .band .label {
    color: var(--z-panel-text-muted);
  }
  .band .interactive:hover,
  .band .interactive.active {
    border-color: var(--z-brand-runner-blue);
  }
  .band .interactive:focus-visible,
  .band .more a:focus-visible {
    outline: var(--z-focus-width) solid var(--z-brand-runner-blue);
    outline-offset: var(--z-focus-offset);
  }
  .band .more a {
    color: var(--z-brand-runner-blue);
  }

  /* Alone on the page: the surface and the accent, like any other panel. */
  .panel {
    margin-bottom: var(--z-space-4);
  }
  .panel .stat {
    border-color: var(--z-border);
    background: var(--z-surface);
    color: var(--z-text);
  }
  .panel .label {
    color: var(--z-text-muted);
  }
  .panel .stat[data-tone='warning'] {
    border-color: var(--z-pending);
    background: var(--z-pending-subtle);
  }
  .panel .interactive:hover,
  .panel .interactive.active {
    border-color: var(--z-accent);
    background: var(--z-accent-subtle);
  }
  .panel .interactive:focus-visible,
  .panel .more a:focus-visible {
    outline: var(--z-focus-width) solid var(--z-accent);
    outline-offset: var(--z-focus-offset);
  }
  .panel .more a {
    color: var(--z-accent);
  }

  @media (max-width: 768px) {
    .strip {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    /* In the grid the cell is the width; a tile that stayed as wide as its own
       word left the columns looking ragged, with a different gap after each. */
    .strip .stat {
      flex: 1;
    }
    .more {
      grid-column: 1 / -1;
      margin-inline-start: 0;
    }
  }
  @media (pointer: coarse) {
    .interactive,
    .more a {
      min-height: var(--z-control-touch);
    }
  }
</style>
