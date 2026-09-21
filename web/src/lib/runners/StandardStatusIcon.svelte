<!-- Original vector artwork refined with the Zoomies owner. The face is
     shared by running and seated poses; all moving parts have opaque fills. -->
<script lang="ts">
  import { standardMotion } from './standard-motion';
  let { state, seed = 'zoomies' }: { state: string; seed?: string } = $props();
  const motion = $derived(standardMotion(state, seed));
</script>

{#snippet face()}
  <g class="head">
    <path fill="var(--z-standard-grey)" d="M174 65Q201 67 210 90Q217 110 202 130L178 120Z" />
    <path
      fill="var(--z-standard-ink)"
      stroke="var(--z-standard-white)"
      stroke-width="2"
      d="M108 92Q106 45 143 43Q184 39 190 82L198 107Q207 137 177 149Q143 159 119 135Z"
    />
    <path
      fill="var(--z-standard-white)"
      d="M148 44Q165 45 167 64L167 91Q171 110 188 114L177 139L148 129Q157 110 153 88Z"
    />
    <g class="ear"
      ><path
        fill="var(--z-standard-ink)"
        stroke="var(--z-standard-white)"
        stroke-width="3"
        d="M119 65Q98 58 88 83L63 125Q50 151 64 168Q72 174 81 166L78 174Q92 169 101 153Q123 132 124 94Z"
      />
      <path
        d="M108 81Q108 119 77 151Q101 140 112 117Q119 98 108 81Z"
        fill="var(--z-standard-shade)"
      /></g
    >{#if motion.state === 'removed'}
      <path
        d="M137 92q7 5 14 0m27-1q4 4 8 0"
        fill="none"
        stroke="var(--z-standard-white)"
        stroke-width="2.5"
        stroke-linecap="round"
      />
    {:else}
      <g class="eyes"
        ><ellipse cx="144" cy="91" rx="8" ry="10" fill="var(--z-standard-white)" /><ellipse
          cx="181"
          cy="90"
          rx="4.5"
          ry="6.5"
          fill="var(--z-standard-white)"
        /><ellipse cx="147" cy="92" rx="4.5" ry="7" fill="var(--z-standard-ink)" /><ellipse
          cx="183"
          cy="91"
          rx="2.5"
          ry="4.5"
          fill="var(--z-standard-ink)"
        /><circle cx="148" cy="88" r="1.8" fill="var(--z-standard-white)" /></g
      >
    {/if}<path
      fill="var(--z-standard-white)"
      d="M135 119Q153 113 166 118Q180 102 198 108Q211 123 188 131Q178 149 158 139Z"
    />
    <path
      fill="none"
      stroke="var(--z-standard-ink)"
      stroke-width="3"
      stroke-linecap="round"
      d={motion.state === 'failed' ? 'M153 136Q169 125 183 135' : 'M151 126Q167 142 184 130'}
    />
    <path
      fill="var(--z-standard-ink)"
      d="M181 107Q190 102 201 108Q204 117 195 122Q185 123 181 115Z"
    />
    <path d="M187 108Q193 105 198 109L191 111Z" fill="var(--z-standard-highlight)" /><path
      d="M135 76Q142 72 148 76"
      fill="none"
      stroke="var(--z-standard-grey)"
      stroke-width="2.5"
      stroke-linecap="round"
    /></g
  >
{/snippet}

<svg
  class="standard-mark"
  class:running={motion.running}
  class:spinning={motion.spinning}
  class:still={motion.still}
  data-style="standard"
  data-motion={motion.state}
  viewBox="0 0 300 280"
  aria-hidden="true"
  focusable="false"
  style:--stride={`${motion.stride}s`}
  style:--phase={`${motion.phase}s`}
  style:--hind-far={`${motion.phase - motion.stride * 0.5}s`}
  style:--front-far={`${motion.phase - motion.stride * 0.25}s`}
  style:--hind-near={`${motion.phase - motion.stride * 0.125}s`}
  style:--front-near={`${motion.phase - motion.stride * 0.75}s`}
  style:--spin={`${motion.spin}s`}
  style:--spin-phase={`${motion.spinPhase}s`}
  style:--gesture={`${motion.gesture}s`}
  style:--blink={`${motion.blink}s`}
>
  {#if motion.running}
    <ellipse cx="150" cy="229" rx="85" ry="4" fill="var(--z-standard-grey)" opacity=".13" />
    {#if motion.spinning}
      <g class="swish"
        ><path
          d="M51 201A112 112 0 1 1 234 223"
          fill="none"
          stroke="var(--z-standard-grey)"
          stroke-width="3"
          stroke-linecap="round"
        /></g
      >
    {/if}
    <g class="spin">
      <g class="dog"
        ><g class="leg hind-far"
          ><path
            d="M89 154Q105 150 111 164L91 190L78 209Q89 212 82 220L64 219Q60 215 67 205L75 182Z"
            fill="var(--z-standard-grey)"
            stroke="var(--z-standard-ink)"
            stroke-width="2.5"
            stroke-linejoin="round"
          /></g
        ><g class="leg front-far"
          ><path
            d="M176 154L193 157L185 187L166 207Q177 211 170 218L152 216Q148 211 154 205L168 182Z"
            fill="var(--z-standard-grey)"
            stroke="var(--z-standard-ink)"
            stroke-width="2.5"
            stroke-linejoin="round"
          /></g
        ><g class="tail"
          ><path
            d="M85 145Q51 132 46 104Q29 117 49 146Q62 164 86 165Z"
            fill="var(--z-standard-ink)"
            stroke="var(--z-standard-white)"
            stroke-width="2"
          /></g
        ><path
          d="M75 151Q82 132 112 134L162 137Q181 129 199 144Q214 157 201 175Q182 188 159 177L119 180Q85 187 75 166Z"
          fill="var(--z-standard-ink)"
          stroke="var(--z-standard-white)"
          stroke-width="2"
        /><path
          d="M178 139Q196 137 203 149Q207 163 192 177L178 173Q187 158 178 139Z"
          fill="var(--z-standard-white)"
        /><g class="leg hind-near"
          ><path
            d="M80 158Q91 152 105 164L95 185L111 208Q122 209 119 219Q111 225 100 218L79 191Q73 185 77 176Z"
            fill="var(--z-standard-white)"
            stroke="var(--z-standard-ink)"
            stroke-width="2.5"
            stroke-linejoin="round"
          /><path
            d="M108 214L106 219"
            fill="none"
            stroke="var(--z-standard-grey)"
            stroke-width="2"
            stroke-linecap="round"
          /></g
        ><g class="leg front-near"
          ><path
            d="M189 156Q200 153 205 165L202 191L217 210Q229 212 222 221L209 222Q204 222 201 214L184 190Z"
            fill="var(--z-standard-white)"
            stroke="var(--z-standard-ink)"
            stroke-width="2.5"
            stroke-linejoin="round"
          /><path
            d="M216 216L215 221"
            fill="none"
            stroke="var(--z-standard-grey)"
            stroke-width="2"
            stroke-linecap="round"
          /></g
        ><g transform="translate(95 46) scale(.73)">{@render face()}</g></g
      >
    </g>
  {:else}
    <ellipse cx="150" cy="245" rx="73" ry="5" fill="var(--z-standard-grey)" opacity=".15" />
    <g class="dog"
      ><path
        class="tail"
        fill="var(--z-standard-ink)"
        stroke="var(--z-standard-white)"
        stroke-width="2"
        d="M207 217Q247 211 253 183Q269 218 244 234L206 235Z"
      />
      <path
        fill="var(--z-standard-ink)"
        stroke="var(--z-standard-white)"
        stroke-width="2"
        d="M118 133Q152 119 178 139Q195 157 202 185Q232 195 234 225Q236 241 213 241H134Q107 233 104 207Q103 170 118 133Z"
      />
      <path
        fill="var(--z-standard-white)"
        d="M129 138Q151 151 174 144Q165 178 144 203L127 194L119 181Z"
      />
      <path
        fill="var(--z-standard-highlight)"
        stroke="var(--z-standard-ink)"
        stroke-width="2"
        d="M170 185L182 228Q200 229 199 238Q190 246 167 239L158 203Z"
      />
      <g class="front"
        ><path
          fill="var(--z-standard-white)"
          stroke="var(--z-standard-ink)"
          stroke-width="2.5"
          d="M123 181Q133 181 143 192L144 228Q158 230 153 239Q137 246 115 240Q112 234 123 228Z"
        /><path
          d="M125 234v6M134 235v6"
          fill="none"
          stroke="var(--z-standard-grey)"
          stroke-width="2"
          stroke-linecap="round"
        /></g
      >{@render face()}</g
    >
  {/if}
</svg>

<style>
  .standard-mark {
    width: var(--z-avatar-size);
    height: var(--z-avatar-size);
    flex: none;
    overflow: hidden;
  }
  .spin,
  .dog,
  .leg,
  .tail,
  .head,
  .ear,
  .eyes,
  .front,
  .swish {
    transform-box: view-box;
  }
  .spin,
  .swish {
    transform-origin: 150px 150px;
  }
  .dog {
    transform-origin: 150px 210px;
    animation: breathe var(--gesture) var(--phase) ease-in-out infinite;
  }
  .head {
    transform-origin: 147px 137px;
  }
  .ear {
    transform-origin: 118px 76px;
  }
  .eyes {
    transform-origin: 155px 88px;
    animation: blink var(--blink) var(--phase) infinite;
  }
  .tail {
    transform-origin: 208px 217px;
    animation: patient-wag var(--gesture) var(--phase) ease-in-out infinite;
  }
  .front {
    transform-origin: 132px 183px;
  }
  .swish {
    opacity: 0;
  }
  .running .dog {
    transform-origin: 150px 170px;
    animation: run-bounce var(--stride) var(--phase) ease-in-out infinite;
  }
  .running .head {
    animation: head-bob var(--stride) var(--phase) ease-in-out infinite;
  }
  .running .ear {
    animation: ear-flap var(--stride) var(--phase) ease-in-out infinite;
  }
  .running .tail {
    transform-origin: 81px 150px;
    animation: tail-wag var(--stride) var(--phase) ease-in-out infinite;
  }
  .leg {
    animation: stride var(--stride) ease-in-out infinite;
  }
  .hind-far {
    transform-origin: 97px 162px;
    animation-delay: var(--hind-far);
  }
  .front-far {
    transform-origin: 183px 163px;
    animation-delay: var(--front-far);
  }
  .hind-near {
    transform-origin: 90px 165px;
    animation-delay: var(--hind-near);
  }
  .front-near {
    transform-origin: 195px 164px;
    animation-delay: var(--front-near);
  }
  .spinning .spin {
    animation: zoomies-spin var(--spin) var(--spin-phase) infinite;
  }
  .spinning .swish {
    animation: spin-swish var(--spin) var(--spin-phase) infinite;
  }
  [data-motion='idle'] .head {
    animation: look var(--gesture) var(--phase) ease-in-out infinite;
  }
  [data-motion='idle'] .ear,
  [data-motion='throttled'] .ear {
    animation: twitch var(--gesture) var(--phase) ease-in-out infinite;
  }
  [data-motion='throttled'] .head {
    transform: rotate(6deg);
  }
  [data-motion='throttled'] .tail {
    animation: none;
  }
  [data-motion='provisioning'] .head {
    animation: sniff var(--gesture) var(--phase) ease-in-out infinite;
  }
  [data-motion='registering'] .head {
    animation: look var(--gesture) var(--phase) ease-in-out infinite;
  }
  [data-motion='registering'] .front {
    animation: wave var(--gesture) var(--phase) ease-in-out infinite;
  }
  [data-motion='draining'] .head {
    animation: settle var(--gesture) var(--phase) ease-in-out infinite;
  }
  [data-motion='draining'] .tail {
    animation: none;
  }
  [data-motion='failed'] .head {
    transform: rotate(-8deg);
  }
  [data-motion='removed'] .head {
    transform: translateY(4px) rotate(5deg);
  }
  .standard-mark.still :global(*) {
    animation: none !important;
  }
  @keyframes breathe {
    50% {
      transform: scale(1, 1.012);
    }
  }
  @keyframes run-bounce {
    0%,
    100% {
      transform: translateY(2px) rotate(-1deg);
    }
    50% {
      transform: translateY(-5px) rotate(1deg);
    }
  }
  @keyframes stride {
    0%,
    100% {
      transform: rotate(-24deg);
    }
    50% {
      transform: rotate(25deg);
    }
  }
  @keyframes tail-wag {
    50% {
      transform: rotate(-12deg);
    }
  }
  @keyframes head-bob {
    50% {
      transform: rotate(-4deg);
    }
  }
  @keyframes ear-flap {
    50% {
      transform: rotate(12deg);
    }
  }
  @keyframes blink {
    0%,
    43%,
    47%,
    100% {
      transform: scaleY(1);
    }
    45% {
      transform: scaleY(0.08);
    }
  }
  /* A brief whole-dog spin between long running stretches, not a loading ring. */
  @keyframes zoomies-spin {
    0%,
    64% {
      transform: rotate(0deg) scale(1);
    }
    67% {
      transform: rotate(-12deg) scale(0.94);
    }
    78% {
      transform: rotate(360deg) scale(0.94);
    }
    82%,
    100% {
      transform: rotate(360deg) scale(1);
    }
  }
  @keyframes spin-swish {
    0%,
    64%,
    83%,
    100% {
      opacity: 0;
      transform: rotate(0deg);
    }
    68% {
      opacity: 0.65;
      transform: rotate(0deg);
    }
    79% {
      opacity: 0.65;
      transform: rotate(360deg);
    }
  }
  @keyframes look {
    0%,
    15%,
    66%,
    100% {
      transform: rotate(0);
    }
    28%,
    48% {
      transform: rotate(8deg);
    }
  }
  @keyframes twitch {
    0%,
    30%,
    42%,
    100% {
      transform: rotate(0);
    }
    34%,
    39% {
      transform: rotate(-6deg);
    }
  }
  @keyframes patient-wag {
    0%,
    10%,
    30%,
    50%,
    100% {
      transform: rotate(0);
    }
    20%,
    40% {
      transform: rotate(-14deg);
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
    .standard-mark :global(*) {
      animation: none !important;
    }
  }
</style>
