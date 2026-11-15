<!--
  The deck's pager: which hosts are on show, how long until the next page,
  and the button that holds the page still.

  It is drawn only while the fleet is taller than the deck. The dots are the
  pages and the one being read fills as its dwell runs out, so the next move
  is expected rather than sudden; past a handful of pages the dots would be a
  row of noise, and one bar says the same thing. The count is plain text, not
  a live region: a screen reader told every ten seconds which hosts a wall is
  showing would hear nothing else.
-->
<script lang="ts">
  import { Pause } from '@lucide/svelte';
  import Button from '$lib/components/Button.svelte';
  import type { DeckTour } from './tour.svelte';

  let { tour }: { tour: DeckTour } = $props();

  const DOTS_UP_TO = 8;
  const dots = $derived(tour.stops.length <= DOTS_UP_TO ? tour.stops.map((_, i) => i) : [0]);
  const current = $derived(tour.stops.length <= DOTS_UP_TO ? tour.page : 0);
</script>

{#if tour.active}
  <div
    class="pager"
    role="group"
    aria-label="Hosts on show"
    data-testid="command-pager"
    data-held={tour.held}
    {@attach tour.engage}
  >
    <Button
      variant="ghost"
      size="sm"
      icon={Pause}
      pressed={tour.paused}
      help={tour.paused
        ? 'Let the hosts turn again.'
        : 'Hold these hosts still. They also hold while the pointer or the keyboard is on them.'}
      onclick={tour.toggle}
    >
      Pause
    </Button>
    <span class="count" data-testid="command-pager-count">{tour.label}</span>
    <span class="dots" class:bar={dots.length === 1} aria-hidden="true">
      {#each dots as dot (dot)}
        <span class="dot" class:current={dot === current}>
          {#if dot === current}<span class="fill" style:--progress={tour.progress}></span>{/if}
        </span>
      {/each}
    </span>
  </div>
{/if}

<style>
  .pager {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-3);
    padding: var(--z-space-1) var(--z-space-4) var(--z-space-1) var(--z-space-1);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-full);
    background: var(--z-surface-raised);
    box-shadow: var(--z-shadow-md);
    pointer-events: auto;
    animation: rise var(--z-motion-slow) var(--z-ease);
  }
  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(var(--z-space-2));
    }
  }
  .count {
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    color: var(--z-text);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .dots {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
  }
  .dot {
    position: relative;
    width: var(--z-space-2);
    height: var(--z-space-1);
    overflow: hidden;
    border-radius: var(--z-radius-full);
    background: var(--z-border-strong);
    transition: width var(--z-motion-slow) var(--z-ease);
  }
  .dot.current,
  .bar .dot {
    width: var(--z-space-8);
  }
  .fill {
    position: absolute;
    inset: 0;
    border-radius: inherit;
    background: var(--z-accent);
    transform: scaleX(var(--progress));
    transform-origin: left;
    /* One tick of the tour's clock, so the bar runs rather than steps. */
    transition: transform 200ms linear;
  }
  /* Held, the bar says so by going quiet rather than by stopping half full
     in the colour of something still running. */
  [data-held='true'] .fill {
    background: var(--z-text-muted);
  }
  @media (prefers-reduced-motion: reduce) {
    .pager {
      animation: none;
    }
    .fill {
      transition: none;
    }
  }
</style>
