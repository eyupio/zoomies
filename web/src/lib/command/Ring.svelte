<!--
  The ring: slots in use out of the slots the fleet has, with the number of
  running jobs inside it. It is the page's signature and the first thing read
  from across a room, so it says one number large and the rest in its name.

  The two halos pulse only while something runs, and the page turns them off
  while the stream is down (`data-live="false"` on its root). A pulse on a
  page that has stopped hearing from the controller would be a picture of work
  nobody can vouch for, which is the failure a wall display must not have.
-->
<script lang="ts">
  import Tooltip from '$lib/components/Tooltip.svelte';
  import type { TipSource } from '$lib/tooltip-detail';
  import { pluralise } from '$lib/format';
  import { ringFraction } from './chart';
  import { RUNNING_HREF } from './links';

  interface Props {
    running: number;
    busy: number;
    slots: number;
    /** What the ring says on hover and focus: the slots by what is in them, and the pools. */
    detail: TipSource;
  }
  let { running, busy, slots, detail }: Props = $props();

  // pathLength="100" makes the arc a percentage, whatever the ring's size.
  const arc = $derived(ringFraction(busy, slots) * 100);
  const pulsing = $derived(busy > 0);
  const name = $derived(
    `${pluralise(running, 'job')} running, ${busy} of ${pluralise(slots, 'slot')} in use`,
  );
</script>

<Tooltip bare {detail} text="Open the running jobs.">
  <a class="ring" href={RUNNING_HREF} aria-label={name} data-testid="command-ring">
    {#if pulsing}
      <span class="halo" aria-hidden="true"></span>
      <span class="halo late" aria-hidden="true"></span>
    {/if}
    <svg aria-hidden="true">
      <circle class="track" pathLength="100" />
      <circle class="arc" pathLength="100" style:stroke-dasharray="{arc} 100" />
    </svg>
    <span class="figure" aria-hidden="true">
      <b>{running}</b>
      <small>running</small>
    </span>
  </a>
</Tooltip>

<style>
  .ring {
    position: relative;
    display: grid;
    place-items: center;
    flex: none;
    width: var(--z-command-ring);
    height: var(--z-command-ring);
    border-radius: var(--z-radius-full);
    color: var(--z-text);
    text-decoration: none;
  }
  .ring:focus-visible {
    outline: none;
    box-shadow: var(--z-focus-ring);
  }
  svg {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    /* The arc starts at twelve o'clock, where a reader expects a dial to. */
    transform: rotate(-90deg);
    overflow: visible;
  }
  circle {
    cx: 50%;
    cy: 50%;
    r: calc(50% - var(--z-command-ring-stroke) / 2);
    fill: none;
    stroke-width: var(--z-command-ring-stroke);
  }
  .track {
    stroke: var(--z-border);
  }
  .arc {
    stroke: var(--z-busy);
    stroke-linecap: round;
    transition:
      stroke-dasharray var(--z-motion-slow) var(--z-ease),
      stroke var(--z-motion-fast) var(--z-ease);
  }
  .ring:hover .track,
  .ring:focus-visible .track {
    stroke: var(--z-accent-border);
  }
  .figure {
    position: relative;
    display: grid;
    justify-items: center;
    line-height: 1;
  }
  b {
    font-size: var(--z-text-3xl);
    font-weight: var(--z-weight-semibold);
    font-variant-numeric: tabular-nums;
  }
  small {
    margin-top: var(--z-space-1);
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .halo {
    position: absolute;
    inset: 0;
    border: var(--z-border-width-thick) solid var(--z-busy-border);
    border-radius: var(--z-radius-full);
    opacity: 0;
    animation: halo calc(var(--z-motion-slow) * 10) var(--z-ease) infinite;
  }
  .halo.late {
    animation-delay: calc(var(--z-motion-slow) * 5);
  }
  @keyframes halo {
    from {
      transform: scale(1);
      opacity: 0.9;
    }
    to {
      transform: scale(1.18);
      opacity: 0;
    }
  }
  /* Read from across a room on a wall, at arm's length on a phone, where the
     verdict beside it needs the width more. */
  @media (max-width: 768px) {
    .ring {
      width: var(--z-command-ring-phone);
      height: var(--z-command-ring-phone);
    }
    b {
      font-size: var(--z-text-xl);
    }
  }
  :global([data-live='false']) .halo {
    display: none;
  }
  @media (prefers-reduced-motion: reduce) {
    .halo {
      display: none;
    }
  }
</style>
