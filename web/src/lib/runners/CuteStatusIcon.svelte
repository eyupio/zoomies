<!-- Original Zoomies cocker spaniel avatar, based on the approved status previews. -->
<script lang="ts">
  let { state, seed = 'zoomies' }: { state: string; seed?: string } = $props();
  const energetic = $derived(['busy', 'zoomies', 'maximum_zoomies'].includes(state));
  // Stable per-runner variation avoids fleet-wide synchronisation and never
  // restarts on CPU sample updates. Motion stays entirely in CSS: no timers.
  const timing = $derived.by(() => {
    let hash = 2166136261;
    for (const char of seed) hash = Math.imul(hash ^ char.charCodeAt(0), 16777619) >>> 0;
    const fraction = (hash % 1000) / 1000;
    const base = state === 'maximum_zoomies' ? 4 : state === 'zoomies' ? 6 : 9;
    return {
      duration: `${base + fraction * 3}s`,
      delay: `-${fraction * 13}s`,
      blink: `${5 + fraction * 4}s`,
    };
  });
</script>

<svg
  class="zoomies-mark"
  data-style="cute"
  data-motion={state}
  class:energetic
  viewBox="0 0 240 220"
  fill="none"
  aria-hidden="true"
  focusable="false"
  style:--lap={timing.duration}
  style:--phase={timing.delay}
  style:--blink={timing.blink}
>
  <circle cx="120" cy="114" r="86" fill="var(--z-surface-sunken)" />
  <ellipse cx="120" cy="192" rx="55" ry="7" fill="var(--z-border)" />
  {#if energetic}
    <g
      class="speed"
      fill="none"
      stroke="var(--z-avatar-accent)"
      stroke-width="3"
      stroke-linecap="round"><path d="M23 104h20M17 122h16M30 141h16M194 98h14M199 117h21" /></g
    >
  {/if}
  <g class="dog">
    <path class="tail fur" d="M149 161Q188 133 193 151Q194 168 163 177Z" />
    <path class="fur" d="M83 155Q82 134 110 132L133 133Q158 140 157 173L145 183H92Z" />
    <path class="white" d="M104 138Q119 146 139 139L137 169Q118 180 102 166Z" />
    <g class="face">
      <path
        class="ear-l fur"
        d="M84 66Q61 59 51 81Q45 96 45 119Q38 128 49 132Q45 143 56 143Q61 155 71 145Q84 149 88 135L93 83Z"
      />
      <path
        class="ear-r fur"
        d="M153 66Q176 61 185 82Q189 97 190 120Q198 132 186 135Q189 146 177 146Q170 156 161 144Q150 148 147 131L145 81Z"
      />
      <path class="shine" d="M65 88Q55 109 59 125M174 88Q182 106 177 127" />
      <path
        class="fur"
        d="M76 89Q76 59 100 53L98 45L114 50L124 42L128 51Q161 51 165 84L163 117Q158 140 121 146Q87 144 77 120Z"
      />
      <path
        fill="var(--z-avatar-cream)"
        d="M114 55Q124 52 129 56L127 77Q124 92 131 104L140 120L99 124Q100 104 111 90Q119 74 114 55Z"
      />
      <path class="shine" d="M91 76Q96 65 104 64M140 65Q151 68 154 78" />
      {#if state === 'removed'}
        <path class="line" d="M88 103q10 8 20 0m28 0q10 8 20 0" />
      {:else}
        <g class="eyes"
          ><ellipse cx="98" cy="102" rx="12" ry="15" fill="var(--z-avatar-cream)" /><ellipse
            cx="146"
            cy="101"
            rx="12"
            ry="15"
            fill="var(--z-avatar-cream)"
          /><ellipse cx="102" cy="103" rx="7" ry="10" fill="var(--z-avatar-ink)" /><ellipse
            cx="149"
            cy="102"
            rx="7"
            ry="10"
            fill="var(--z-avatar-ink)"
          /><circle cx="104" cy="99" r="2.7" fill="var(--z-avatar-white)" /><circle
            cx="151"
            cy="98"
            r="2.7"
            fill="var(--z-avatar-white)"
          /></g
        >
      {/if}
      {#if energetic}
        <path
          d="M101 127Q122 117 143 125Q141 148 123 150Q105 146 101 127Z"
          fill="var(--z-avatar-ink)"
        />
        <path
          class="tongue"
          d="M116 135Q124 132 131 136L130 145Q124 154 117 146Z"
          fill="var(--z-avatar-tongue)"
        /><path d="M124 138v7" stroke="var(--z-avatar-tongue-line)" stroke-width="1.4" />
      {:else}
        <path class="line" d={state === 'failed' ? 'M109 139q13-10 26 0' : 'M107 131q16 12 31-1'} />
      {/if}
      <path
        class="white"
        d="M121 115Q108 110 98 119Q88 132 105 136Q115 139 122 131Q131 139 143 132Q153 123 140 117Q133 112 121 115Z"
      />
      <path
        d="M113 114Q122 109 133 114Q133 122 123 125Q114 123 113 114Z"
        fill="var(--z-avatar-ink)"
      /><path
        d="M119 114h7"
        stroke="var(--z-avatar-shine)"
        stroke-width="2"
        stroke-linecap="round"
      />
      <g class="freckles" fill="var(--z-avatar-freckle)"
        ><circle cx="102" cy="126" r="1.3" /><circle cx="108" cy="129" r="1.1" /><circle
          cx="138"
          cy="125"
          r="1.3"
        /></g
      >
      <path
        d="M92 145Q121 156 148 142L149 150Q120 166 92 153Z"
        fill="currentColor"
        stroke="var(--z-avatar-ink)"
        stroke-width="2"
      />
      <circle
        cx="123"
        cy="158"
        r="6"
        fill="var(--z-avatar-tag)"
        stroke="var(--z-avatar-ink)"
        stroke-width="1.5"
      /><path
        d="M120 156h5l-5 4h5"
        fill="none"
        stroke="var(--z-avatar-tag-ink)"
        stroke-width="1.3"
      />
    </g>
    <g class="paw-l"
      ><path class="white" d="M83 162Q96 155 105 167L105 181Q87 192 76 180Q72 171 83 162Z" /><path
        class="line"
        d="M84 179v5M92 179v6"
      /></g
    >
    <g class="paw-r"
      ><path class="white" d="M136 164Q147 155 159 165Q174 178 160 186L139 184Z" /><path
        class="line"
        d="M149 179v6M158 178v6"
      /></g
    >
  </g>
</svg>

<style>
  .zoomies-mark {
    width: var(--z-avatar-size);
    height: var(--z-avatar-size);
    flex: none;
    overflow: hidden;
  }
  .fur {
    fill: var(--z-avatar-fur);
    stroke: var(--z-avatar-ink);
    stroke-width: 2.4;
    stroke-linejoin: round;
  }
  .white {
    fill: var(--z-avatar-cream);
    stroke: var(--z-avatar-ink);
    stroke-width: 2.4;
    stroke-linejoin: round;
  }
  .shine {
    fill: none;
    stroke: var(--z-avatar-shine);
    stroke-width: 3;
    stroke-linecap: round;
  }
  .line {
    fill: none;
    stroke: var(--z-avatar-ink);
    stroke-width: 2.4;
    stroke-linecap: round;
  }
  .dog,
  .face,
  .ear-l,
  .ear-r,
  .paw-l,
  .paw-r,
  .tail,
  .eyes,
  .speed,
  .tongue {
    transform-box: view-box;
    animation-duration: var(--lap);
    animation-delay: var(--phase);
    animation-timing-function: ease-in-out;
    animation-iteration-count: infinite;
  }
  .dog {
    transform-origin: 120px 180px;
    animation-name: breathe;
  }
  .face {
    transform-origin: 120px 123px;
  }
  .ear-l {
    transform-origin: 84px 73px;
  }
  .ear-r {
    transform-origin: 151px 72px;
  }
  .paw-l {
    transform-origin: 89px 169px;
  }
  .paw-r {
    transform-origin: 148px 169px;
  }
  .tail {
    transform-origin: 151px 165px;
    animation-name: tail-wag;
  }
  .eyes {
    transform-origin: 120px 102px;
    animation-name: blink;
    animation-duration: var(--blink);
  }
  .tongue {
    transform-origin: 124px 133px;
    animation-name: pant;
  }
  .energetic .dog {
    animation-name: bound;
  }
  .energetic .ear-l {
    animation-name: flap-left;
  }
  .energetic .ear-r {
    animation-name: flap-right;
  }
  .energetic .paw-l {
    animation-name: step-left;
  }
  .energetic .paw-r {
    animation-name: step-right;
  }
  .speed {
    animation-name: breeze;
  }
  [data-motion='maximum_zoomies'] {
    --hop: -10px;
  }
  [data-motion='zoomies'] {
    --hop: -7px;
  }
  [data-motion='busy'] {
    --hop: -4px;
  }
  [data-motion='throttled'] .face,
  [data-motion='idle'] .face {
    animation-name: patient-tilt;
  }
  [data-motion='throttled'] .ear-r {
    animation-name: ear-twitch;
  }
  [data-motion='provisioning'] .face {
    animation-name: sniff;
  }
  [data-motion='registering'] .face {
    animation-name: patient-tilt;
  }
  [data-motion='registering'] .paw-r {
    animation-name: wave;
  }
  [data-motion='draining'] .face {
    animation-name: settle;
  }
  [data-motion='draining'] .tail {
    animation-name: none;
  }
  [data-motion='failed'] .dog,
  [data-motion='failed'] .tail,
  [data-motion='removed'] .dog,
  [data-motion='removed'] .tail,
  [data-motion='unknown'] .dog,
  [data-motion='unknown'] .tail {
    animation-name: none;
  }
  [data-motion='failed'] .face {
    transform: rotate(-8deg);
  }
  [data-motion='removed'] .face {
    transform: translateY(4px) rotate(5deg);
  }
  /* Two clear bounds or gestures, followed by a rest. SVG transforms stay
     inside the reserved avatar box and cannot nudge the label or table. */
  @keyframes bound {
    0%,
    4%,
    12%,
    20%,
    100% {
      transform: translateY(0);
    }
    8%,
    16% {
      transform: translateY(var(--hop)) scale(0.98, 1.02);
    }
  }
  @keyframes flap-left {
    0%,
    4%,
    12%,
    20%,
    100% {
      transform: rotate(0);
    }
    8%,
    16% {
      transform: rotate(-17deg);
    }
  }
  @keyframes flap-right {
    0%,
    4%,
    12%,
    20%,
    100% {
      transform: rotate(0);
    }
    8%,
    16% {
      transform: rotate(17deg);
    }
  }
  @keyframes step-left {
    0%,
    4%,
    12%,
    20%,
    100% {
      transform: translateY(0);
    }
    8%,
    16% {
      transform: translateY(-13px) rotate(-8deg);
    }
  }
  @keyframes step-right {
    0%,
    8%,
    16%,
    24%,
    100% {
      transform: translateY(0);
    }
    12%,
    20% {
      transform: translateY(-13px) rotate(8deg);
    }
  }
  @keyframes tail-wag {
    0%,
    4%,
    12%,
    20%,
    28%,
    100% {
      transform: rotate(0);
    }
    8%,
    24% {
      transform: rotate(-12deg);
    }
    16% {
      transform: rotate(17deg);
    }
  }
  @keyframes blink {
    0%,
    39%,
    43%,
    100% {
      transform: scaleY(1);
    }
    41% {
      transform: scaleY(0.08);
    }
  }
  @keyframes pant {
    0%,
    4%,
    12%,
    20%,
    100% {
      transform: scaleY(0.9);
    }
    8%,
    16% {
      transform: scaleY(1.12);
    }
  }
  @keyframes breeze {
    0%,
    4%,
    24%,
    100% {
      opacity: 0.15;
      transform: translateX(0);
    }
    8%,
    16% {
      opacity: 0.8;
      transform: translateX(-6px);
    }
  }
  @keyframes breathe {
    0%,
    100% {
      transform: scale(1);
    }
    50% {
      transform: scale(1.01, 1.02);
    }
  }
  @keyframes patient-tilt {
    0%,
    12%,
    65%,
    100% {
      transform: rotate(0);
    }
    24%,
    46% {
      transform: rotate(9deg);
    }
  }
  @keyframes ear-twitch {
    0%,
    65%,
    76%,
    100% {
      transform: rotate(0);
    }
    68%,
    73% {
      transform: rotate(-9deg);
    }
    70% {
      transform: rotate(3deg);
    }
  }
  @keyframes sniff {
    0%,
    8%,
    30%,
    100% {
      transform: translateY(0) rotate(0);
    }
    14%,
    23% {
      transform: translateY(-3px) rotate(-8deg);
    }
    18% {
      transform: translateY(-1px) rotate(-5deg);
    }
  }
  @keyframes wave {
    0%,
    10%,
    35%,
    100% {
      transform: translateY(0);
    }
    17%,
    29% {
      transform: translateY(-14px) rotate(12deg);
    }
    23% {
      transform: translateY(-14px) rotate(-6deg);
    }
  }
  @keyframes settle {
    0%,
    10%,
    50%,
    100% {
      transform: translateY(0);
    }
    25%,
    35% {
      transform: translateY(3px) rotate(4deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .dog,
    .face,
    .ear-l,
    .ear-r,
    .paw-l,
    .paw-r,
    .tail,
    .eyes,
    .speed,
    .tongue {
      animation: none !important;
    }
    .speed {
      opacity: 0.55;
    }
  }
</style>
