<!--
  The command centre's hero: the ring, the verdict, three figures and the last
  hour. It answers "what is happening?" in the order a glance reads it: a
  number, a sentence, then the timings behind the sentence.

  The verdict is worked out by the route from the same snapshot as the rest of
  the page; this only draws it. Every figure is a link to the records it is
  made from, and the shared tooltip names where, because "Runner start" does
  not say that it opens the Runners page.
-->
<script lang="ts">
  import { TriangleAlert, Clock } from '@lucide/svelte';
  import type { Stats } from '$lib/api/types';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import Tooltip from '$lib/components/Tooltip.svelte';
  import { notifications } from '$lib/state/notifications.svelte';
  import HourChart from './HourChart.svelte';
  import Ring from './Ring.svelte';
  import { heroFigures } from './figures';
  import type { Verdict } from './verdict';

  interface Props {
    verdict: Verdict;
    stats: Stats | null;
    running: number;
    busy: number;
    slots: number;
    now: number;
    /**
     * The fleet's first read has not answered. The verdict is not drawn until
     * it has: "No hosts yet." about a fleet nobody has looked at is a claim.
     */
    loading?: boolean;
  }
  let { verdict, stats, running, busy, slots, now, loading = false }: Props = $props();

  const figures = $derived(heroFigures(stats, now));

  function openProblems(): void {
    notifications.open = true;
  }
</script>

{#if loading}
  <section class="hero" aria-labelledby="command-verdict" aria-busy="true">
    <span class="ring-skeleton"><Skeleton width="100%" height="100%" radius="full" /></span>
    <div class="verdict">
      <h2 id="command-verdict" class="sr-only">Loading the fleet.</h2>
      <Skeleton width="60%" height="var(--z-text-2xl)" />
      <Skeleton lines={2} height="var(--z-text-base)" />
    </div>
    <ul class="figures" aria-hidden="true">
      {#each [0, 1, 2] as figure (figure)}
        <li class="figure"><Skeleton lines={3} height="var(--z-text-xs)" /></li>
      {/each}
    </ul>
    <div class="chart chart-skeleton">
      <Skeleton width="50%" height="var(--z-text-xs)" />
      <Skeleton height="var(--z-space-16)" radius="md" />
    </div>
  </section>
{:else}
  <section class="hero" aria-labelledby="command-verdict">
    <Ring {running} {busy} {slots} />

    <div class="verdict" data-verdict={verdict.kind}>
      <h2 id="command-verdict">{verdict.headline}</h2>
      <p class="sentence" aria-live="polite">
        {#each verdict.parts as part, i (i)}
          {#if part.href}
            <a class="part {part.tone ?? ''}" href={part.href}>
              {#if part.tone === 'pending'}<Clock size={14} aria-hidden="true" />{/if}
              {#if part.tone === 'danger'}<TriangleAlert size={14} aria-hidden="true" />{/if}
              {part.text}
            </a>
          {:else if part.opens === 'problems'}
            <button type="button" class="part {part.tone ?? ''}" onclick={openProblems}>
              {#if part.tone === 'pending'}<Clock size={14} aria-hidden="true" />{/if}
              {#if part.tone === 'danger'}<TriangleAlert size={14} aria-hidden="true" />{/if}
              {part.text}
            </button>
          {:else}{part.text}{/if}
        {/each}
      </p>
    </div>

    <ul class="figures">
      {#each figures as figure (figure.label)}
        <li>
          <Tooltip bare text={figure.help}>
            <a class="figure" href={figure.href}>
              <span class="label">{figure.label}</span>
              <b class="value">{figure.value}</b>
              <span class="note">{figure.note}</span>
            </a>
          </Tooltip>
        </li>
      {/each}
    </ul>

    <div class="chart">
      <HourChart {slots} />
    </div>
  </section>
{/if}

<style>
  .hero {
    display: grid;
    grid-template-columns: auto minmax(0, 1.2fr) minmax(0, 1fr) minmax(0, 1fr);
    align-items: center;
    gap: var(--z-space-6);
    padding: var(--z-space-6);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  /* The ring's own box, so the verdict beside it does not move when it lands. */
  .ring-skeleton {
    display: block;
    width: var(--z-command-ring);
    height: var(--z-command-ring);
  }
  .chart-skeleton {
    display: grid;
    gap: var(--z-space-2);
  }
  .verdict {
    display: grid;
    gap: var(--z-space-2);
    min-width: 0;
  }
  h2 {
    margin: 0;
    font-size: var(--z-text-2xl);
    line-height: var(--z-leading-2xl);
    font-weight: var(--z-weight-semibold);
    letter-spacing: var(--z-tracking-tight);
    color: var(--z-text);
  }
  [data-verdict='not-live'] h2,
  [data-verdict='queue'] h2 {
    color: var(--z-pending);
  }
  [data-verdict='attention'] h2 {
    color: var(--z-danger);
  }
  .sentence {
    margin: 0;
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
    color: var(--z-text-muted);
  }
  .part {
    display: inline;
    padding: 0;
    border: 0;
    background: none;
    font: inherit;
    font-weight: var(--z-weight-semibold);
    color: var(--z-accent);
    text-decoration: underline;
    text-underline-offset: var(--z-underline-offset);
    cursor: pointer;
  }
  /* The tone's shape sits in the line, before the words it marks. */
  .part :global(svg) {
    display: inline-block;
    margin-right: var(--z-space-1);
    vertical-align: -0.125em;
  }
  .part.pending {
    color: var(--z-pending);
  }
  .part.danger {
    color: var(--z-danger);
  }
  .figures {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--z-space-3);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .figure {
    display: grid;
    gap: var(--z-space-1);
    min-width: 0;
    padding: var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-lg);
    background: var(--z-surface);
    color: var(--z-text);
    text-decoration: none;
    transition: border-color var(--z-motion-fast) var(--z-ease);
  }
  .figure:hover,
  .figure:focus-visible {
    border-color: var(--z-accent);
  }
  .figure:focus-visible {
    outline: none;
    box-shadow: var(--z-focus-ring);
  }
  .label,
  .note {
    overflow: hidden;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* The label names the stats window, and a narrow tile wraps it rather than
     cutting off the period the figure is about. */
  .label {
    white-space: normal;
  }
  .value {
    font-size: var(--z-text-xl);
    font-weight: var(--z-weight-semibold);
    font-variant-numeric: tabular-nums;
  }
  .chart {
    min-width: 0;
  }
  @media (max-width: 1180px) {
    .hero {
      grid-template-columns: auto minmax(0, 1fr);
    }
    .figures,
    .chart {
      grid-column: 1 / -1;
    }
  }
  @media (max-width: 768px) {
    /* A phrase in the sentence is still a whole fingertip tall to press. */
    .part {
      display: inline-flex;
      align-items: center;
    }
    .ring-skeleton {
      width: var(--z-command-ring-phone);
      height: var(--z-command-ring-phone);
    }
    .hero {
      gap: var(--z-space-4);
      padding: var(--z-space-4);
    }
    .figure {
      padding: var(--z-space-2);
    }
    .value {
      font-size: var(--z-text-lg);
    }
  }
</style>
