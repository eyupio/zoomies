<!--
  One plot of the capacity map: a set of hosts' lines over the window, drawn
  at the width the panel has, one unit to a pixel. The map draws one of these
  for every host on the chart, or one for each, and everything that is not a
  line -- which host is on the chart, which moment is being read, what the
  reading was -- lives in the map; the plot draws what it is given and reports
  where the pointer is.

  What makes the lines readable when there are twenty of them is emphasis: a
  host or a measurement the operator has singled out is drawn on top, and the
  rest step back. Pointing at a line singles out its host without going to
  the legend, since on a busy chart the legend cannot say which line is
  which. The lead measurement's newest value is written at the end of every
  line, so the chart can be read from its right-hand edge alone, and where
  two lines end together the labels are pushed apart with a leader back to
  the line. The band from 85% is the share of the machine at which the
  scheduler starts placing one runner at a time: pressure the operator can
  see coming rather than a threshold they have to know.

  The lines are revealed left to right when the window changes, and that is
  the only motion the plot has beyond the newest point's pulse: a live chart
  that redrew with a flourish every ten seconds would be unwatchable.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import {
    PRESSURE,
    bridgeFor,
    indexAtX,
    lineRuns,
    nearestHost,
    plotFrame,
    scaleX,
    scaleY,
    spreadLabels,
    timeTicks,
    type HostLine,
    type MetricKey,
  } from './hostSeries';

  interface Props {
    lines: HostLine[];
    /** How many points every line has. */
    count: number;
    /** The seconds a point folds, which is what decides the bridge across a missed one. */
    bucket: number;
    /** The lane's ceiling for load past the cores, or 0 without a lane; shared by every plot. */
    overflow: number;
    /** The window's edges, for the labels along the bottom. */
    start: number;
    end: number;
    activeIndex: number;
    /** A moment is being read, by hover, by touch or from the timeline. */
    reading: boolean;
    emphasis: { host: string | null; metric: MetricKey | null };
    /** The measurement labelled at the ends of the lines and washed under a lone host. */
    lead: MetricKey | null;
    live: boolean;
    /** A shorter drawing with fewer labels, for the per-host stack. */
    compact?: boolean;
    /** Draw the times along the bottom. Only the last of a stack does. */
    axis?: boolean;
    /** Write the lead value at the end of every line. */
    labels?: boolean;
    /** Wash the area under the lead lines. Only where one host is drawn. */
    wash?: boolean;
    /** The lines are revealed afresh whenever this changes. */
    revealKey: string;
    stamp: (at: number) => string;
    label: string;
    loading?: boolean;
    /** Something to say instead of lines: no hosts, no samples. */
    message?: string;
    onhover: (i: number | null) => void;
    onpress: (i: number) => void;
    onnear?: (host: string | null) => void;
    /** The reading, rendered beside the crosshair. */
    card?: Snippet;
  }

  let {
    lines,
    count,
    bucket,
    overflow,
    start,
    end,
    activeIndex,
    reading,
    emphasis,
    lead,
    live,
    compact = false,
    axis = true,
    labels = true,
    wash = false,
    revealKey,
    stamp,
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
  const frame = $derived(plotFrame(width || 760, { compact, lane: overflow > 0, axis }));
  const x = $derived(scaleX(frame, count));
  const y = $derived(scaleY(frame, overflow));
  const grid = $derived(compact ? [0, 50, 100] : [0, 25, 50, 75, 100]);
  const ticks = $derived(
    timeTicks(start, end, frame.narrow ? 3 : 5).map((at) => ({
      at,
      x: frame.LEFT + ((at - start) / Math.max(1, end - start)) * frame.SPAN,
    })),
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
    line: HostLine;
    lit: boolean;
    paths: string[];
    areas: string[];
  }
  const singled = $derived(emphasis.host !== null || emphasis.metric !== null);
  const drawn = $derived.by((): Drawn[] => {
    const bridge = bridgeFor(bucket);
    const out = lines.map((line) => {
      const runs = lineRuns(line.points, bridge);
      const lit =
        !singled ||
        ((emphasis.host === null || line.host.id === emphasis.host) &&
          (emphasis.metric === null || line.metric.key === emphasis.metric));
      const paths = runs.map(pathOf);
      const areas =
        wash && line.metric.key === lead
          ? runs.map(
              (run, n) =>
                `${paths[n]} L${x(run[run.length - 1]!.i).toFixed(1)},${frame.BOTTOM} L${x(run[0]!.i).toFixed(1)},${frame.BOTTOM} Z`,
            )
          : [];
      return { line, lit, paths, areas };
    });
    // The singled-out lines are drawn last, so they sit over the rest.
    return singled ? [...out.filter((d) => !d.lit), ...out.filter((d) => d.lit)] : out;
  });

  /** The lead value at the end of every line that reaches the edge. */
  const ends = $derived.by(() => {
    if (!labels || lead === null) return [];
    const eligible = lines.filter(
      (l) => l.metric.key === lead && l.last !== null && l.last.i >= count - 2,
    );
    const wanted = eligible.map((l) => y(l.last!.value));
    const placed = spreadLabels(wanted, 14, frame.TOP + 6, frame.BOTTOM - 2);
    return eligible.map((line, n) => ({
      line,
      x0: x(line.last!.i),
      y0: wanted[n]!,
      y: placed[n]!,
      text: `${line.last!.value.toFixed(0)}%`,
    }));
  });

  /* -- the pointer ----------------------------------------------------------- */

  let svg = $state<SVGSVGElement | null>(null);
  function at(event: PointerEvent): { i: number; viewY: number } {
    const rect = (svg ?? (event.currentTarget as SVGSVGElement)).getBoundingClientRect();
    const scale = frame.W / Math.max(1, rect.width);
    return {
      i: indexAtX(frame, count, (event.clientX - rect.left) * scale),
      viewY: (event.clientY - rect.top) * scale,
    };
  }
  // A mouse reads the chart by passing over it, and the reading goes when it
  // leaves. A finger cannot hover: it arrives, and the moment it leaves the
  // glass the pointer has left too, so a reading that lived only under the
  // pointer was gone before it could be read. So a touch chooses the moment
  // and dragging scrubs it; a click does the same, because a chosen moment
  // that stays put is how two hosts get compared at one instant.
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
      onnear?.(nearestHost(lines, i, viewY, y, 10));
    } else if (event.buttons) onpress(i);
  }
  function onLeave(): void {
    onhover(null);
    onnear?.(null);
  }
</script>

<div class="plot" class:compact bind:clientWidth={width} aria-busy={loading}>
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
      {#each drawn as d (d.line.id)}
        {#if d.areas.length}
          <linearGradient id="{uid}-{d.line.index}" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0" style:stop-color={d.line.tone} stop-opacity="0.24" />
            <stop offset="1" style:stop-color={d.line.tone} stop-opacity="0.02" />
          </linearGradient>
        {/if}
      {/each}
    </defs>

    {#if overflow}
      <!-- The lane for load past the cores, on its own scale up to the peak
           in view. Its floor is the axis's 100%, which is why the line there
           is drawn firmer than the grid. -->
      <rect class="lane" x={frame.LEFT} y={frame.TOP} width={frame.SPAN} height={frame.LANE} />
      <line class="grid" x1={frame.LEFT} x2={frame.RIGHT} y1={frame.TOP} y2={frame.TOP} />
      <text class="axis" x={frame.LEFT - 8} y={frame.TOP + 4} text-anchor="end">{overflow}%</text>
      {#if !compact}
        <text class="lane-label" x={frame.RIGHT - 4} y={frame.TOP + 13} text-anchor="end">
          load past the cores
        </text>
      {/if}
    {/if}

    <!-- The pressure band: from the share at which the scheduler slows
         placement on a host. -->
    <rect
      class="pressure"
      x={frame.LEFT}
      y={y(100)}
      width={frame.SPAN}
      height={y(PRESSURE) - y(100)}
    />
    <line class="threshold" x1={frame.LEFT} x2={frame.RIGHT} y1={y(PRESSURE)} y2={y(PRESSURE)} />
    {#if !compact}
      <text class="band-label" x={frame.LEFT + 6} y={y(PRESSURE) - 4}>pressure</text>
    {/if}

    {#each grid as value (value)}
      <line
        class="grid"
        class:base={value === 0}
        class:edge={value === 100 && overflow > 0}
        x1={frame.LEFT}
        x2={frame.RIGHT}
        y1={y(value)}
        y2={y(value)}
      />
      {#if !compact || value !== 50}
        <text class="axis" x={frame.LEFT - 8} y={y(value) + 4} text-anchor="end">{value}%</text>
      {/if}
    {/each}

    {#key revealKey}
      <g class="lines">
        {#each drawn as d (d.line.id)}
          <g class="series" class:dim={!d.lit}>
            {#each d.areas as area, n (n)}
              <path d={area} class="area" fill="url(#{uid}-{d.line.index})" />
            {/each}
            {#each d.paths as path, n (n)}
              <path
                d={path}
                class="line"
                style:stroke={d.line.tone}
                stroke-dasharray={d.line.metric.dash}
              />
            {/each}
          </g>
        {/each}
      </g>
    {/key}

    {#each drawn as d (d.line.id)}
      {#if d.lit && live && d.line.last && d.line.last.i === count - 1}
        <circle
          class="pulse"
          cx={x(d.line.last.i)}
          cy={y(d.line.last.value)}
          r="3.5"
          style:stroke={d.line.tone}
        />
        <circle
          class="now"
          cx={x(d.line.last.i)}
          cy={y(d.line.last.value)}
          r="3.5"
          style:fill={d.line.tone}
        />
      {/if}
    {/each}

    {#each ends as e (e.line.id)}
      {#if Math.abs(e.y - e.y0) > 2}
        <line
          class="leader"
          x1={e.x0}
          y1={e.y0}
          x2={frame.RIGHT + 5}
          y2={e.y}
          style:stroke={e.line.tone}
        />
      {/if}
      <text
        class="end"
        class:dim={singled && !drawn.find((d) => d.line === e.line)?.lit}
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
      {#each drawn as d (d.line.id)}
        {@const v = d.line.points[activeIndex]?.value}
        {#if d.lit && v !== null && v !== undefined}
          <circle class="marker" cx={x(activeIndex)} cy={y(v)} r="4" style:fill={d.line.tone} />
        {/if}
      {/each}
    {/if}

    {#if axis}
      {#each ticks as tick (tick.at)}
        <line class="tick" x1={tick.x} x2={tick.x} y1={frame.BOTTOM} y2={frame.BOTTOM + 4} />
        <text class="axis" x={tick.x} y={frame.H - 7} text-anchor="middle">{stamp(tick.at)}</text>
      {/each}
    {/if}
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
  .grid {
    stroke: var(--z-border);
  }
  .grid.base,
  .grid.edge {
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
  .lane {
    fill: var(--z-surface-sunken);
  }
  .pressure {
    fill: var(--z-pending-subtle);
  }
  .threshold {
    stroke: var(--z-pending-border);
  }
  .band-label,
  .lane-label {
    font-size: var(--z-text-2xs);
    font-weight: var(--z-weight-medium);
    letter-spacing: var(--z-tracking-wide);
    text-transform: uppercase;
  }
  .band-label {
    fill: var(--z-pending);
  }
  .lane-label {
    fill: var(--z-text-subtle);
  }

  /* -- the lines ---------------------------------------------------------------- */
  .line {
    fill: none;
    stroke-width: 2;
    stroke-linejoin: round;
    stroke-linecap: round;
    transition:
      opacity var(--z-motion-fast) var(--z-ease),
      stroke-width var(--z-motion-fast) var(--z-ease);
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
  /* The newest point of a live line breathes, so "live" is something seen
     rather than a switch that says so. Under reduced motion it holds still. */
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
