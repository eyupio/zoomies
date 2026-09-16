<!--
  A figure's swatch: its line exactly as the chart draws it, dash and all. A
  dot would carry the colour and nothing else, and the stroke is the half of
  the answer that survives a reader who cannot tell the two hues apart.
-->
<script lang="ts">
  import type { Series } from './plot';

  let { series, off = false }: { series: Series; off?: boolean } = $props();
</script>

<svg class="stroke" class:off viewBox="0 0 24 8" aria-hidden="true">
  <line
    x1="1"
    y1="4"
    x2="23"
    y2="4"
    stroke-dasharray={series.dash}
    stroke-linecap="round"
    style:stroke={series.tone}
  />
</svg>

<style>
  .stroke {
    width: 24px;
    height: 8px;
    flex: none;
  }
  line {
    stroke-width: 2;
  }
  /* A figure that is off says so with its stroke as well as its weight: the
     colour alone is the thing a reader who cannot see it would lose. */
  .off {
    opacity: 0.4;
  }
</style>
