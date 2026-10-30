<!--
  The Overview's answer: is anything wrong, said first and in a sentence.

  The page under it is the evidence (the counts, the pools, the jobs), and an
  operator who opens it fifty times a day is asking one question of it. This is
  the public pages' stage, as the band the status page uses: the dog, an eyebrow,
  a large headline with its accent, the short blue rule and one sentence. It
  carries neutral white and the brand's blue and no status colour, because the
  panel is Zoomies Black in both themes and a colour chosen for one of them would
  be wrong on the other; the problem is named in counts, and the line under the
  hero is the way in.

  A phone stacks everything and the pools must still be on its first screen, which
  even a slim band costs, so the hero is for the screens that have room for both.
  The stage itself is the shared PageBand, so this page's size, switch and dismiss
  behave as every other page's do; only its sentence, its four figures and its
  Review button are its own.
-->
<script lang="ts">
  import Button from '$lib/components/Button.svelte';
  import PageBand from '$lib/bands/PageBand.svelte';
  import { describeWindow } from '$lib/format';
  import { fleet } from '$lib/state/fleet.svelte';
  import { notifications } from '$lib/state/notifications.svelte';
  import { prefs } from '$lib/state/prefs.svelte';
  import { overviewMetrics, runningJobs } from './metric-stats';
  import { statusLine } from './status-line';

  interface Props {
    /** The first-run checklist is on screen: nothing is configured yet. */
    setupPending?: boolean;
  }
  let { setupPending = false }: Props = $props();

  const line = $derived(
    statusLine({
      problems: notifications.active,
      // The same figure the band shows, from the same scope: this fleet's busy
      // runners, unless the toggle says to count every job GitHub reports.
      running: runningJobs(fleet.stats, prefs.otherRunners),
      slotsUsed: fleet.stats?.hosts?.used ?? 0,
      slots: fleet.stats?.hosts?.capacity ?? 0,
      setupPending,
    }),
  );
  // The four figures the page is judged on, carried by its band as every other
  // page carries the figures that answer it. Each is a way into its evidence.
  const figures = $derived(
    fleet.loaded && fleet.stats
      ? overviewMetrics({ stats: fleet.stats, others: prefs.otherRunners, now: Date.now() })
      : [],
  );
  const windowText = $derived(describeWindow(fleet.stats?.window));
  const coverage = $derived(
    !windowText || windowText === '1 hour'
      ? 'Waits and outcomes cover the last hour.'
      : `Waits and outcomes cover the last ${windowText}.`,
  );
</script>

<PageBand
  page="overview"
  testid="fleet-status"
  eyebrow={setupPending ? 'Welcome to Zoomies' : 'Fleet status'}
  line={fleet.loaded ? line : null}
  stats={figures}
>
  {#snippet actions()}
    <!-- The hero is the page's problems line on a desktop (Overview.svelte puts the
         old banner away there), so the way in is here. -->
    {#if line.needsYou}
      <Button variant="primary" onclick={() => (notifications.open = true)}>Review</Button>
    {/if}
  {/snippet}
</PageBand>

{#if fleet.loaded}
  <p class="coverage">
    {#if prefs.otherRunners && fleet.stats?.hosts}
      <span>Runners and hosts belong to this fleet.</span>
    {/if}
    <span>{coverage}</span>
  </p>
{/if}

<style>
  /*
    The window the waits and the outcomes under the band cover, said once rather
    than in each figure. The lane keeps its last words clear of the assistant's
    button, which sits over the window's right edge.
  */
  .coverage {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--z-space-1) var(--z-space-4);
    margin: calc(-1 * var(--z-space-2)) 0 var(--z-space-4);
    padding-inline-end: var(--z-eli-lane);
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    text-align: end;
    overflow-wrap: anywhere;
  }
  @media (max-width: 768px) {
    /* On a phone the lane wrapped this line onto a second row and pushed the
       first pool below the fold, to keep clear of a button that floats over
       whatever is scrolled under it anyway. */
    .coverage {
      justify-content: flex-start;
      padding-inline-end: 0;
      text-align: start;
    }
  }
</style>
