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
  import Tooltip from '$lib/components/Tooltip.svelte';
  import { formatDuration, NO_VALUE, parseGoDuration } from '$lib/format';
  import { notifications } from '$lib/state/notifications.svelte';
  import HourChart from './HourChart.svelte';
  import Ring from './Ring.svelte';
  import { finishedTodayHref, QUEUE_HREF, RUNNERS_HREF } from './links';
  import type { Verdict } from './verdict';

  interface Props {
    verdict: Verdict;
    stats: Stats | null;
    running: number;
    busy: number;
    slots: number;
    live: boolean;
    now: number;
  }
  let { verdict, stats, running, busy, slots, live, now }: Props = $props();

  interface Figure {
    label: string;
    value: string;
    note: string;
    href: string;
    help: string;
  }

  /*
    The fleet's own figures, as the Overview's band reads them: on an
    organisation that also uses hosted runners the top-level figures are mostly
    somebody else's jobs. The controller answers zero for a timing it has no
    samples for, and "0ms" claims instant work where the honest answer is that
    nothing was measured, so a figure with nothing behind it is NO_VALUE.
  */
  const figures = $derived.by((): Figure[] => {
    const own = stats?.fleet ?? stats ?? null;
    const started = (own?.running_jobs ?? 0) + (own?.completed ?? 0);
    const wait = started > 0 ? own?.median_wait_ms : undefined;
    const waitP95 = started > 0 ? own?.p95_wait_ms : undefined;
    const startup = stats?.p50_startup_ms || undefined;
    const startupP95 = stats?.p95_startup_ms || undefined;
    const completed = own?.completed ?? 0;
    const succeeded = own?.succeeded ?? 0;
    const window = stats?.window ? formatDuration(parseGoDuration(stats.window)) : null;
    return [
      {
        label: 'Median wait',
        value: formatDuration(wait),
        note: waitP95 === undefined ? NO_VALUE : `p95 ${formatDuration(waitP95)}`,
        href: QUEUE_HREF,
        help: 'Open the queue.',
      },
      {
        label: 'Runner start',
        value: formatDuration(startup),
        note: startupP95 === undefined ? NO_VALUE : `p95 ${formatDuration(startupP95)}`,
        href: RUNNERS_HREF,
        help: 'Open the runners.',
      },
      {
        label: 'Succeeded',
        value: completed > 0 ? `${Math.round((succeeded / completed) * 100)}%` : NO_VALUE,
        note:
          completed > 0
            ? `${succeeded} of ${completed}${window ? ` in ${window}` : ''}`
            : 'nothing finished',
        href: finishedTodayHref(new Date(now)),
        help: "Open today's finished jobs.",
      },
    ];
  });

  function openProblems(): void {
    notifications.open = true;
  }
</script>

<section class="hero" aria-labelledby="command-verdict">
  <Ring {running} {busy} {slots} {live} />

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

<style>
  .hero {
    display: grid;
    grid-template-columns: auto minmax(0, 1.2fr) minmax(0, 1fr) minmax(0, 1fr);
    align-items: center;
    gap: var(--z-space-6);
    padding: var(--z-space-6);
    border-bottom: var(--z-border-width) solid var(--z-border);
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
  .part :global(svg) {
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
