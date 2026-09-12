<!--
  One fleet figure over a window, minute by minute: a line, the area under
  it, and every way of asking what a point was.

  Hovering draws a crosshair and a card with the figure at that moment and
  whatever else the caller can say about it -- the other three counts, on the
  fleet trend -- so a spike in the queue is read against the runners that
  were there to take it. The slider under the chart is the same inspection
  for a keyboard or a thumb, and the coverage strip says which minutes were
  actually observed: a gap is a gap, never a zero.
-->
<script lang="ts">
  import type { SignalPoint } from './signals';
  import { formatNumber } from '$lib/format';
  let {
    points,
    label,
    tone = 'accent',
    /** The width of a point, in minutes: what the coverage strip counts. */
    step = 1,
    /** More about a moment, for the card: the other figures at that time. */
    detail,
  }: {
    points: SignalPoint[];
    label: string;
    tone?: string;
    step?: number;
    detail?: (at: number) => Array<[string, string]>;
  } = $props();

  const LEFT = 36;
  const RIGHT = 724;
  const TOP = 22;
  const BOTTOM = 130;
  const SPAN = RIGHT - LEFT;

  let selected = $state<number | null>(null);
  let hover = $state<number | null>(null);
  const known = $derived(points.filter((p) => p.value !== null));
  const ceiling = $derived(Math.max(2, ...known.map((p) => p.value ?? 0)));
  const x = (i: number) => LEFT + (i * SPAN) / Math.max(1, points.length - 1);
  const y = (value: number) => BOTTOM - (value / ceiling) * (BOTTOM - TOP);
  /** The point being read: the hovered one, else the chosen one, else now. */
  const activeIndex = $derived(hover ?? selected ?? points.length - 1);
  const active = $derived(points[activeIndex]);

  /** Every unbroken run of observed minutes, as one line and one area each. */
  const runs = $derived.by(() => {
    const out: Array<{ line: string; area: string }> = [];
    let run: number[] = [];
    const flush = () => {
      if (run.length === 0) return;
      const line = run
        .map((i, n) => `${n ? 'L' : 'M'}${x(i).toFixed(1)},${y(points[i]!.value!).toFixed(1)}`)
        .join(' ');
      const first = run[0]!;
      const last = run[run.length - 1]!;
      out.push({
        line,
        area: `${line} L${x(last).toFixed(1)},${BOTTOM} L${x(first).toFixed(1)},${BOTTOM} Z`,
      });
      run = [];
    };
    points.forEach((p, i) => {
      if (p.value === null) flush();
      else run.push(i);
    });
    flush();
    return out;
  });

  const peak = $derived(
    known.reduce<SignalPoint | null>(
      (best, p) => (best && best.value! >= p.value! ? best : p),
      null,
    ),
  );
  const mean = $derived(
    known.length ? known.reduce((n, p) => n + (p.value ?? 0), 0) / known.length : null,
  );
  const now = $derived([...known].reverse()[0] ?? null);

  const time = (at: number) =>
    new Date(at).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
  const unit = $derived(step === 1 ? 'minutes' : `${step}-minute intervals`);

  /** Which point the pointer is over, from where it is across the chart. */
  function onPointer(event: PointerEvent): void {
    const svg = event.currentTarget as SVGSVGElement;
    const rect = svg.getBoundingClientRect();
    const share = (event.clientX - rect.left) / rect.width;
    const viewX = share * 760;
    const i = Math.round(((viewX - LEFT) / SPAN) * Math.max(1, points.length - 1));
    hover = Math.max(0, Math.min(points.length - 1, i));
  }
</script>

<div class="trend" style:--signal={`var(--z-${tone})`}>
  {#if known.length}
    <div class="summary">
      <span
        ><strong>{now ? formatNumber(now.value) : '--'}</strong> now{#if now}, at {time(
            now.at,
          )}{/if}</span
      >
      {#if peak}
        <span><strong>{formatNumber(peak.value)}</strong> at most, at {time(peak.at)}</span>
      {/if}
      {#if mean !== null}
        <span
          ><strong>{mean.toLocaleString(undefined, { maximumFractionDigits: 1 })}</strong> on average</span
        >
      {/if}
    </div>
    <div class="plot">
      <svg
        viewBox="0 0 760 154"
        role="img"
        aria-label={`${label}; ${known.length} observed ${unit}. Inspect the timeline below for exact values.`}
        onpointermove={onPointer}
        onpointerleave={() => (hover = null)}
      >
        <line x1={LEFT} x2={RIGHT} y1={TOP} y2={TOP} /><line
          x1={LEFT}
          x2={RIGHT}
          y1={BOTTOM}
          y2={BOTTOM}
        />
        <text x={LEFT - 8} y={TOP + 4} text-anchor="end">{ceiling}</text><text
          x={LEFT - 8}
          y={BOTTOM + 4}
          text-anchor="end">0</text
        >
        {#each runs as run, i (i)}
          <path d={run.area} class="area" />
          <path d={run.line} class="line" vector-effect="non-scaling-stroke" />
        {/each}
        {#if active && active.value !== null}
          <line
            class="cursor"
            x1={x(activeIndex)}
            x2={x(activeIndex)}
            y1={TOP}
            y2={BOTTOM}
            vector-effect="non-scaling-stroke"
          />
          <circle cx={x(activeIndex)} cy={y(active.value)} r="4" class="marker" />
        {:else if active}
          <line
            class="cursor"
            x1={x(activeIndex)}
            x2={x(activeIndex)}
            y1={TOP}
            y2={BOTTOM}
            vector-effect="non-scaling-stroke"
          />
        {/if}
        {#if points[0]}<text x={LEFT} y="150">{time(points[0].at)}</text
          >{/if}{#if points[points.length - 1]}<text x={RIGHT} y="150" text-anchor="end"
            >{time(points[points.length - 1]!.at)}</text
          >{/if}
      </svg>
      {#if active && (hover !== null || selected !== null)}
        <div
          class="card"
          class:right={activeIndex > points.length / 2}
          style:left="{(100 * (x(activeIndex) - 0)) / 760}%"
          aria-hidden="true"
        >
          <p class="when">{time(active.at)}</p>
          <p class="what">
            {#if active.value === null}
              Not sampled
            {:else}
              <strong>{formatNumber(active.value)}</strong>
              {label.toLowerCase()}
            {/if}
          </p>
          {#if detail && active.value !== null}
            <dl>
              {#each detail(active.at) as [name, value] (name)}
                <div>
                  <dt>{name}</dt>
                  <dd>{value}</dd>
                </div>
              {/each}
            </dl>
          {/if}
        </div>
      {/if}
    </div>
  {:else}<p class="empty">No samples recorded in this window yet.</p>{/if}
  <div class="scrub">
    <label
      ><span>Inspect {label.toLowerCase()}</span><input
        type="range"
        min="0"
        max={Math.max(0, points.length - 1)}
        value={selected ?? points.length - 1}
        oninput={(e) => (selected = Number(e.currentTarget.value))}
        aria-valuetext={active
          ? `${time(active.at)}: ${active.value === null ? 'not sampled' : active.value}`
          : 'No samples'}
      /></label
    ><output
      >{active
        ? `${time(active.at)} · ${active.value === null ? 'Not sampled' : active.value}`
        : 'Not sampled'}</output
    >
  </div>
  <div class="coverage" aria-label={`${known.length} of ${points.length} ${unit} observed`}>
    {#each points as point (point.at)}<span
        class:observed={point.value !== null}
        title={`${time(point.at)}: ${point.value ?? 'not sampled'}`}
      ></span>{/each}
  </div>
  <p class="note">{known.length} / {points.length} {unit} observed · gaps mean no sample</p>
</div>

<style>
  .summary {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-1) var(--z-space-4);
    margin-bottom: var(--z-space-2);
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .summary strong {
    color: var(--z-text);
    font-weight: var(--z-weight-semibold);
    font-variant-numeric: tabular-nums;
  }
  .plot {
    position: relative;
  }
  svg {
    display: block;
    width: 100%;
    height: auto;
    touch-action: pan-y;
  }
  line {
    stroke: var(--z-border);
    stroke-dasharray: 3 4;
  }
  text {
    fill: var(--z-text-muted);
    font-size: 12px;
  }
  .area {
    fill: var(--signal);
    opacity: 0.12;
  }
  .line {
    fill: none;
    stroke: var(--signal);
    stroke-width: 2.5;
    stroke-linejoin: round;
    stroke-linecap: round;
  }
  .cursor {
    stroke: var(--z-text-subtle);
    stroke-dasharray: 3 3;
  }
  .marker {
    fill: var(--signal);
    stroke: var(--z-surface);
    stroke-width: 2;
  }
  /* The card sits beside the crosshair, on whichever side has the room. */
  .card {
    position: absolute;
    top: 0;
    transform: translateX(var(--z-space-2));
    max-width: 16rem;
    padding: var(--z-space-2) var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface-raised);
    box-shadow: var(--z-shadow-md);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    pointer-events: none;
    z-index: var(--z-layer-sticky);
  }
  .card.right {
    transform: translateX(calc(-100% - var(--z-space-2)));
  }
  .card p {
    margin: 0;
  }
  .when {
    color: var(--z-text-subtle);
  }
  .what strong {
    font-weight: var(--z-weight-semibold);
    font-variant-numeric: tabular-nums;
  }
  .card dl {
    display: grid;
    gap: var(--z-nudge-2) var(--z-space-3);
    margin: var(--z-space-1) 0 0;
    padding-top: var(--z-space-1);
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .card dl div {
    display: flex;
    justify-content: space-between;
    gap: var(--z-space-3);
  }
  .card dt {
    color: var(--z-text-muted);
  }
  .card dd {
    margin: 0;
    font-variant-numeric: tabular-nums;
  }
  .scrub {
    display: flex;
    align-items: center;
    gap: var(--z-space-4);
    flex-wrap: wrap;
    margin-top: var(--z-space-3);
  }
  label {
    display: flex;
    align-items: center;
    gap: var(--z-space-3);
    flex: 1;
    font-size: var(--z-text-xs);
    min-width: 0;
  }
  label span {
    white-space: nowrap;
  }
  input {
    width: 100%;
    min-width: 70px;
    accent-color: var(--signal);
    height: var(--z-space-6);
  }
  output {
    font-size: var(--z-text-xs);
    font-variant-numeric: tabular-nums;
  }
  .coverage {
    display: flex;
    gap: var(--z-border-width);
    margin-top: var(--z-space-3);
    height: var(--z-space-2);
  }
  .coverage span {
    flex: 1;
    background: var(--z-surface-sunken);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
  }
  .coverage .observed {
    background: var(--signal);
    border-color: var(--signal);
  }
  .note,
  .empty {
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
  }
  .empty {
    padding: var(--z-space-5) 0;
  }
</style>
