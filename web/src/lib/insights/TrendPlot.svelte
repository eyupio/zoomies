<!--
  A trend's drawing: a line per figure over a window, drawn at the width the
  panel has, one unit to a pixel. Everything that is not a line -- which
  figures are on the chart, which moment is being read, what the reading was
  -- lives in the panel above it; the plot draws what it is given and reports
  where the pointer is. The fleet activity panel and the usage chart are both
  this, which is why nothing in here knows what a figure counts.

  Colour and stroke are the panel's to assign, and between them they say
  which figure a line is: the fleet trend gives a colour to each state and a
  stroke to jobs against runners, the usage chart a colour to each outcome
  and a dash to the nearer of two hues. Pointing at a line singles it out and
  steps the rest back, and a chip does the same, because with five lines on
  one chart a legend cannot say which is which.

  The newest value is written at the end of every line, pushed apart with a
  leader where two lines end together, so the chart reads from its
  right-hand edge alone. A figure nobody sampled is a gap and never a zero:
  a controller that was down and a queue that was empty are opposite news.

  Behind the lines are the bands: the stretches of the window where the thing
  the panel is watching for was true -- work waiting with nothing free, a
  pool that had nowhere to put a runner. A condition the lines are read
  against rather than a line of its own.

  The lines are revealed left to right when the window changes, and that is
  the only motion the plot has beyond the newest point's pulse: a live chart
  that redrew with a flourish every time a sample landed would be
  unwatchable.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import { formatNumber } from '$lib/format';
  import {
    nearestSeries,
    seriesRuns,
    spreadLabels,
    timeTicks,
    trendFrame,
    trendIndexAtX,
    trendX,
    trendY,
    type SeriesLine,
  } from './plot';

  interface Props {
    lines: SeriesLine[];
    /** How many points every line has. */
    count: number;
    /** The top of the axis, and the figures the grid is labelled with. */
    ceiling: number;
    grid: number[];
    /** The window's edges, for the labels along the bottom. */
    start: number;
    end: number;
    /** The stretches the panel wants shaded behind the lines. */
    bands: Array<{ from: number; to: number }>;
    activeIndex: number;
    /** A moment is being read, by hover, by touch or from the timeline. */
    reading: boolean;
    /** A figure singled out from a chip, a legend row or the line itself. */
    emphasis: string | null;
    /** The figure the wash is drawn under. Only where one is on the chart. */
    lead: string | null;
    /**
     * Which point is the present, so that the reading there can breathe. A
     * line that ends anywhere else has stopped reporting, and a window that
     * ended in the past has no present in it at all: a dot pulsing at the end
     * of a closed report, or of a sampler that died an hour ago, claims one.
     */
    present?: number | null;
    /** The lines are revealed afresh whenever this changes. */
    revealKey: string;
    stamp: (at: number) => string;
    /** How a figure is written: counts are whole, hours are not. */
    format?: (value: number) => string;
    label: string;
    loading?: boolean;
    /** Something to say instead of lines: no figures chosen, no samples. */
    message?: string;
    onhover: (i: number | null) => void;
    onpress: (i: number) => void;
    onnear: (key: string | null) => void;
    /** The reading, rendered beside the crosshair. */
    card?: Snippet;
  }

  let {
    lines,
    count,
    ceiling,
    grid,
    start,
    end,
    bands,
    activeIndex,
    reading,
    emphasis,
    lead,
    present = null,
    revealKey,
    stamp,
    format = formatNumber,
    label,
    loading = false,
    message,
    onhover,
    onpress,
    onnear,
    card,
  }: Props = $props();

  const uid = $props.id();

  /* -- the frame ------------------------------------------------------------- */

  let width = $state(0);
  const digits = $derived(format(ceiling).length);
  const frame = $derived(trendFrame(width || 760, { digits }));
  const x = $derived(trendX(frame, count));
  const y = $derived(trendY(frame, ceiling));
  /** Half a point's width: what a shaded interval is widened by at each end. */
  const halfPoint = $derived(frame.SPAN / Math.max(1, (count - 1) * 2));
  // A label that wants to sit centred on the first tick hangs off the left of
  // the drawing and is cut in half by it -- the window's first point is at the
  // gutter, not inset from it. The two at the edges are anchored to the edge
  // instead, which is where the eye looks for them anyway.
  const ticks = $derived(
    timeTicks(start, end, frame.narrow ? 3 : 5).map((at) => {
      const x = frame.LEFT + ((at - start) / Math.max(1, end - start)) * frame.SPAN;
      const half = 3.2 * stamp(at).length;
      const anchor = x - half < 2 ? 'start' : x + half > frame.W - 2 ? 'end' : 'middle';
      return { at, x, anchor, labelX: anchor === 'start' ? 2 : anchor === 'end' ? frame.W - 2 : x };
    }),
  );

  /* -- the lines ------------------------------------------------------------- */

  function pathOf(run: { i: number; value: number }[]): string {
    const d = run
      .map((p, n) => `${n ? 'L' : 'M'}${x(p.i).toFixed(1)},${y(p.value).toFixed(1)}`)
      .join(' ');
    // A single observed interval has no length to stroke: give it a dot's
    // worth so the line cap draws it.
    return run.length > 1 ? d : `${d} l0.01,0`;
  }

  interface Drawn {
    line: SeriesLine;
    lit: boolean;
    paths: string[];
    areas: string[];
  }
  const drawn = $derived.by((): Drawn[] => {
    const out = lines.map((line) => {
      const runs = seriesRuns(line.points);
      const lit = emphasis === null || line.series.key === emphasis;
      const paths = runs.map(pathOf);
      // The wash belongs to the figure the chart leads with, and only while
      // it is alone: under five overlapping lines it is a stain, not a
      // reading.
      const areas =
        lines.length === 1 && line.series.key === lead
          ? runs.map(
              (run, n) =>
                `${paths[n]} L${x(run[run.length - 1]!.i).toFixed(1)},${frame.BOTTOM} L${x(run[0]!.i).toFixed(1)},${frame.BOTTOM} Z`,
            )
          : [];
      return { line, lit, paths, areas };
    });
    // The singled-out line is drawn last, so it sits over the rest.
    return emphasis === null ? out : [...out.filter((d) => !d.lit), ...out.filter((d) => d.lit)];
  });

  /** The newest value at the end of every line that reaches the edge. */
  const ends = $derived.by(() => {
    const eligible = lines.filter((l) => l.last !== null && l.last.i >= count - 2);
    const wanted = eligible.map((l) => y(l.last!.value));
    const placed = spreadLabels(wanted, 13, frame.TOP + 5, frame.BOTTOM - 2);
    return eligible.map((line, n) => ({
      line,
      x0: x(line.last!.i),
      y0: wanted[n]!,
      y: placed[n]!,
      text: format(line.last!.value),
    }));
  });

  /* -- the pointer ----------------------------------------------------------- */

  let svg = $state<SVGSVGElement | null>(null);
  function at(event: PointerEvent): { i: number; viewY: number } {
    const rect = (svg ?? (event.currentTarget as SVGSVGElement)).getBoundingClientRect();
    const scale = frame.W / Math.max(1, rect.width);
    return {
      i: trendIndexAtX(frame, count, (event.clientX - rect.left) * scale),
      viewY: (event.clientY - rect.top) * scale,
    };
  }
  // A mouse reads the chart by passing over it, and the reading goes when it
  // leaves. A finger cannot hover: it arrives, and the moment it leaves the
  // glass the pointer has left too, so a reading that lived only under the
  // pointer was gone before it could be read. So a touch chooses the moment
  // and dragging scrubs it; a click does the same, because a chosen moment
  // that stays put is how the queue and the idle runners get read at one
  // instant while somebody talks about it.
  function onPress(event: PointerEvent): void {
    onpress(at(event).i);
    if (event.pointerType === 'mouse') return;
    // Capture so a drag that wanders off the chart keeps scrubbing. A drag
    // the browser takes for a vertical scroll cancels the pointer instead,
    // which is `touch-action: pan-y` doing its job.
    (event.currentTarget as SVGSVGElement).setPointerCapture(event.pointerId);
  }
  function onMove(event: PointerEvent): void {
    const { i, viewY } = at(event);
    if (event.pointerType === 'mouse') {
      onhover(i);
      onnear(nearestSeries(lines, i, viewY, y, 10));
    } else if (event.buttons) onpress(i);
  }
  function onLeave(): void {
    onhover(null);
    onnear(null);
  }
</script>

<div class="plot" bind:clientWidth={width} aria-busy={loading}>
  <svg
    bind:this={svg}
    width={frame.W}
    height={frame.H}
    viewBox={`0 0 ${frame.W} ${frame.H}`}
    role="img"
    aria-label={label}
    onpointerdown={onPress}
    onpointermove={onMove}
    onpointerleave={onLeave}
  >
    <defs>
      {#each drawn as d (d.line.series.key)}
        {#if d.areas.length}
          <linearGradient id="{uid}-{d.line.series.key}" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0" style:stop-color={d.line.series.tone} stop-opacity="0.22" />
            <stop offset="1" style:stop-color={d.line.series.tone} stop-opacity="0.02" />
          </linearGradient>
        {/if}
      {/each}
    </defs>

    <!-- The band: a stretch where the fleet was short of something. It is
         drawn behind everything, in the colour the console uses for pending,
         because it is a condition the lines are read against and not a line
         of its own. -->
    {#each bands as run (run.from)}
      <rect
        class="band"
        x={x(run.from) - halfPoint}
        y={frame.TOP}
        width={x(run.to) - x(run.from) + halfPoint * 2}
        height={frame.BOTTOM - frame.TOP}
      />
    {/each}

    {#each grid as value (value)}
      <line
        class="grid"
        class:base={value === 0}
        x1={frame.LEFT}
        x2={frame.RIGHT}
        y1={y(value)}
        y2={y(value)}
      />
      <text class="axis" x={frame.LEFT - 8} y={y(value) + 4} text-anchor="end">{format(value)}</text
      >
    {/each}

    {#key revealKey}
      <g class="lines">
        {#each drawn as d (d.line.series.key)}
          <g class="series" class:dim={!d.lit}>
            {#each d.areas as area, n (n)}
              <path d={area} class="area" fill="url(#{uid}-{d.line.series.key})" />
            {/each}
            {#each d.paths as path, n (n)}
              <path
                d={path}
                class="line"
                style:stroke={d.line.series.tone}
                stroke-dasharray={d.line.series.dash}
              />
            {/each}
          </g>
        {/each}
      </g>
    {/key}

    {#each drawn as d (d.line.series.key)}
      {#if d.lit && d.line.last && d.line.last.i === present}
        <circle
          class="pulse"
          cx={x(d.line.last.i)}
          cy={y(d.line.last.value)}
          r="3.5"
          style:stroke={d.line.series.tone}
        />
        <circle
          class="now"
          cx={x(d.line.last.i)}
          cy={y(d.line.last.value)}
          r="3.5"
          style:fill={d.line.series.tone}
        />
      {/if}
    {/each}

    {#each ends as e (e.line.series.key)}
      {#if Math.abs(e.y - e.y0) > 2}
        <line
          class="leader"
          x1={e.x0}
          y1={e.y0}
          x2={frame.RIGHT + 5}
          y2={e.y}
          style:stroke={e.line.series.tone}
        />
      {/if}
      <text
        class="end"
        class:dim={emphasis !== null && e.line.series.key !== emphasis}
        x={frame.RIGHT + 8}
        y={e.y + 4}>{e.text}</text
      >
    {/each}

    {#if reading}
      <line
        class="cursor"
        x1={x(activeIndex)}
        x2={x(activeIndex)}
        y1={frame.TOP}
        y2={frame.BOTTOM}
      />
      {#each drawn as d (d.line.series.key)}
        {@const v = d.line.points[activeIndex]?.value}
        {#if d.lit && v !== null && v !== undefined}
          <circle
            class="marker"
            cx={x(activeIndex)}
            cy={y(v)}
            r="4"
            style:fill={d.line.series.tone}
          />
        {/if}
      {/each}
    {/if}

    {#each ticks as tick (tick.at)}
      <line class="tick" x1={tick.x} x2={tick.x} y1={frame.BOTTOM} y2={frame.BOTTOM + 4} />
      <text class="axis" x={tick.labelX} y={frame.H - 7} text-anchor={tick.anchor}
        >{stamp(tick.at)}</text
      >
    {/each}
  </svg>

  {#if message}
    <p class="message">{message}</p>
  {/if}

  {#if card && reading}
    <div class="card" class:right={activeIndex > count / 2} style:left="{x(activeIndex)}px">
      {@render card()}
    </div>
  {/if}
</div>

<style>
  .plot {
    position: relative;
    min-width: 0;
  }
  svg {
    display: block;
    max-width: 100%;
    height: auto;
    touch-action: pan-y;
    transition: opacity var(--z-motion-base) var(--z-ease);
  }
  /* A refetch holds the last picture at reduced strength rather than
     clearing it: no skeleton, no jump. */
  .plot[aria-busy='true'] svg {
    opacity: 0.55;
  }

  /* -- the chrome: recessive, solid hairlines --------------------------------- */
  .band {
    fill: var(--z-pending-subtle);
  }
  .grid {
    stroke: var(--z-border);
  }
  .grid.base {
    stroke: var(--z-border-strong);
  }
  .tick {
    stroke: var(--z-border-strong);
  }
  .axis {
    fill: var(--z-text-subtle);
    font-size: var(--z-text-2xs);
    font-variant-numeric: tabular-nums;
  }

  /* -- the lines ---------------------------------------------------------------- */
  .line {
    fill: none;
    stroke-width: 2;
    stroke-linejoin: round;
    stroke-linecap: round;
    transition: opacity var(--z-motion-fast) var(--z-ease);
  }
  .area {
    transition: opacity var(--z-motion-fast) var(--z-ease);
  }
  .series.dim .line,
  .series.dim .area {
    opacity: 0.16;
  }
  .lines {
    animation: reveal calc(var(--z-motion-slow) * 2) var(--z-ease) both;
  }
  @keyframes reveal {
    from {
      clip-path: inset(0 100% 0 0);
    }
    to {
      clip-path: inset(0 0 0 0);
    }
  }
  .end {
    fill: var(--z-text);
    font-size: var(--z-text-2xs);
    font-weight: var(--z-weight-medium);
    font-variant-numeric: tabular-nums;
    transition: opacity var(--z-motion-fast) var(--z-ease);
  }
  .end.dim {
    opacity: 0.3;
  }
  .leader {
    stroke-width: 1;
    opacity: 0.6;
  }

  /* -- the moment ------------------------------------------------------------- */
  .cursor {
    stroke: var(--z-text-subtle);
  }
  .marker {
    stroke: var(--z-surface);
    stroke-width: 2;
  }
  .now {
    stroke: var(--z-surface);
    stroke-width: 1.5;
  }
  /* The newest point breathes, so "live" is something seen rather than a
     switch that says so. Under reduced motion it holds still. */
  .pulse {
    fill: none;
    stroke-width: 1.5;
    transform-box: fill-box;
    transform-origin: center;
    animation: breathe 2.4s var(--z-ease) infinite;
  }
  @keyframes breathe {
    from {
      opacity: 0.7;
      transform: scale(1);
    }
    to {
      opacity: 0;
      transform: scale(3.4);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .pulse,
    .lines {
      animation: none;
    }
  }

  .message {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    pointer-events: none;
  }

  /* -- the card beside the crosshair ------------------------------------------ */
  .card {
    position: absolute;
    top: var(--z-space-2);
    transform: translateX(var(--z-space-3));
    pointer-events: none;
    z-index: var(--z-layer-sticky);
  }
  .card.right {
    transform: translateX(calc(-100% - var(--z-space-3)));
  }
  /* On a phone the reading sits under the chart rather than beside the
     crosshair: a card at the finger is a card under the finger, and one
     twenty rems wide beside a point near the edge is off the screen. */
  @media (max-width: 768px) {
    .card,
    .card.right {
      position: static;
      transform: none;
      margin-top: var(--z-space-2);
    }
  }
</style>
