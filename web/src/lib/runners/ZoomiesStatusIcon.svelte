<!-- Original Zoomies paw-and-swish artwork. No icon font, raster or external asset. -->
<script lang="ts">
  let { state, seed = 'zoomies' }: { state: string; seed?: string } = $props();
  // Stable per-runner variation: no timers or jitter on live sample updates.
  const timing = $derived.by(() => {
    let hash = 2166136261;
    for (const char of seed) hash = Math.imul(hash ^ char.charCodeAt(0), 16777619) >>> 0;
    const fraction = (hash % 1000) / 1000;
    const base = state === 'maximum_zoomies' ? 6 : state === 'zoomies' ? 8 : 11;
    return { duration: `${base + fraction * 4}s`, delay: `-${fraction * 13}s` };
  });
</script>

<svg
  class="zoomies-mark"
  data-motion={state}
  viewBox="0 0 40 40"
  fill="none"
  aria-hidden="true"
  style:--lap={timing.duration}
  style:--phase={timing.delay}
>
  <g class="trail" stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
    <path d="M4 26c-2 7 14 11 26 3" />
    <path class="speed" d="M2 18h6M3 22h4" />
  </g>
  <g class="paw" fill="currentColor">
    <ellipse cx="13" cy="15" rx="3.1" ry="4.2" transform="rotate(-28 13 15)" />
    <ellipse cx="20" cy="11.6" rx="3.1" ry="4.2" transform="rotate(-8 20 11.6)" />
    <ellipse cx="27" cy="13.4" rx="3" ry="4.1" transform="rotate(22 27 13.4)" />
    <ellipse cx="31" cy="20.3" rx="2.6" ry="3.7" transform="rotate(36 31 20.3)" />
    <path
      d="M13.2 25.7c-.5-3 2.4-4.3 4.7-7 1.4-1.6 3.2-1.8 4.8-.4 2.2 2 2.5 4.4 4.1 6.2 2.9 3.5.1 6.5-3.4 5.4-2.4-.8-4.1-1.3-6.4-.6-2.5.8-3.5-1-3.8-3.6Z"
    />
    <g class="face" fill="var(--z-surface)">
      <ellipse cx="18" cy="23.5" rx=".9" ry="1.1" />
      <ellipse cx="23" cy="24" rx=".9" ry="1.1" />
      <path
        d="M19 26q1.5 1.6 3 .2"
        fill="none"
        stroke="var(--z-surface)"
        stroke-width="1.1"
        stroke-linecap="round"
      />
    </g>
  </g>
  {#if state === 'maximum_zoomies'}
    <path
      class="spark"
      d="m34 3 1.2 3.8L39 8l-3.8 1.2L34 13l-1.2-3.8L29 8l3.8-1.2Z"
      fill="currentColor"
    />
  {:else if state === 'zoomies'}
    <g class="spark" stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
      <path d="m33 8 1-5m2 7 3-3" />
    </g>
  {:else if state === 'throttled' || state === 'draining'}
    <path
      class="leash"
      d="M5 6v5c0 4 4 5 7 5"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
    />
    <circle cx="5" cy="4" r="2.5" stroke="currentColor" stroke-width="1.6" />
  {:else if state === 'registering' || state === 'provisioning'}
    <g class="sniff" fill="currentColor"
      ><circle cx="33" cy="7" r="1.5" /><circle cx="37" cy="13" r="1" /></g
    >
  {:else if state === 'failed'}
    <path d="m30 3 7 7m0-7-7 7" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" />
  {:else if state === 'removed'}
    <path d="M30 6h7" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" />
  {/if}
</svg>

<style>
  .zoomies-mark {
    width: 1.8rem;
    height: 1.8rem;
    flex: none;
    overflow: visible;
  }
  .paw {
    transform-box: fill-box;
    transform-origin: center;
  }
  .speed {
    opacity: 0;
  }
  .paw,
  .speed,
  .spark,
  .sniff {
    animation-duration: var(--lap);
    animation-delay: var(--phase);
    animation-timing-function: ease-in-out;
    animation-iteration-count: infinite;
  }
  [data-motion='busy'] .paw,
  [data-motion='zoomies'] .paw {
    animation-name: scamper;
  }
  [data-motion='maximum_zoomies'] .paw {
    animation-name: leap;
  }
  [data-motion='zoomies'] .speed,
  [data-motion='maximum_zoomies'] .speed {
    animation-name: breeze;
  }
  .spark {
    transform-box: fill-box;
    transform-origin: center;
    animation-name: glint;
  }
  .sniff {
    animation-name: breeze;
  }
  [data-motion='registering'] .paw,
  [data-motion='provisioning'] .paw {
    animation-name: sniff;
  }
  [data-motion='throttled'] .paw {
    animation-name: settle;
  }
  [data-motion='removed'] .paw {
    opacity: 0.55;
  }
  /* Two little bounds, then a long pause. Nothing flashes or moves the label. */
  @keyframes scamper {
    0%,
    6%,
    14%,
    22%,
    100% {
      transform: translateY(0) rotate(0);
    }
    10% {
      transform: translateY(-3px) rotate(-9deg);
    }
    18% {
      transform: translateY(-2px) rotate(6deg);
    }
  }
  @keyframes leap {
    0%,
    5%,
    13%,
    21%,
    100% {
      transform: translate(0) rotate(0);
    }
    9% {
      transform: translate(1px, -4px) rotate(-13deg);
    }
    17% {
      transform: translate(2px, -3px) rotate(9deg);
    }
  }
  @keyframes breeze {
    0%,
    5%,
    23%,
    100% {
      opacity: 0.35;
    }
    9%,
    17% {
      opacity: 1;
    }
  }
  @keyframes glint {
    0%,
    5%,
    23%,
    100% {
      transform: scale(0.85);
    }
    12% {
      transform: scale(1.1) rotate(12deg);
    }
  }
  @keyframes sniff {
    0%,
    8%,
    28%,
    100% {
      transform: rotate(0);
    }
    14% {
      transform: rotate(-9deg);
    }
    22% {
      transform: rotate(6deg);
    }
  }
  @keyframes settle {
    0%,
    10%,
    30%,
    100% {
      transform: translateY(0);
    }
    20% {
      transform: translateY(2px) rotate(-5deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .paw,
    .speed,
    .spark,
    .sniff {
      animation: none !important;
    }
    [data-motion='zoomies'] .speed,
    [data-motion='maximum_zoomies'] .speed {
      opacity: 1;
    }
  }
</style>
