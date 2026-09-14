<!--
  The Host capacity map: every host's utilisation on one chart, live and
  backwards in time.

  Each host is a colour and each measurement is a stroke, so a line is read
  the way the usage chart's are: the colour says which machine, the dash says
  what about it. Everything is a share of the machine -- CPU measured, memory
  measured, load against the CPU count, runners against slots, what the
  scheduler has promised against what it may place on, disk -- so seven
  different figures sit on one 0-100% axis and can be read against each other.
  Only load may pass 100, because a load of twice the CPUs is a real thing and
  is what steps a throttle up. It gets a lane of its own above the axis, on
  its own scale up to the highest load in view: a spike to eight times the
  cores is still drawn as a spike, and the axis everything else is read on
  keeps its room rather than being squashed into the bottom tenth of the chart
  behind thirty labels.

  The controller writes one sample per host per minute; the stream keeps the
  newest point moving between them, from the same host view the cards below
  are drawn from, so the chart's right-hand edge and the card never disagree.
  A host that stops reporting draws a gap, not a flat line: a flat line is
  what a healthy, quiet machine draws too. One missing minute is not that,
  though -- it is a sampler a moment late or a tab that slept -- so the line
  is drawn across it and only two absent in a row break it.

  The three shortest windows -- the last minute, five, ten -- are drawn ten
  seconds to a point, from what the stream delivers while the page is open,
  because an operator watching a job land wants each heartbeat and not the
  last of every two. Behind them the minute samples are still the history,
  so those windows fill in as the stream arrives.

  Hosts and measurements switch on and off with a click and the choice is
  remembered, because an operator watching two machines out of forty wants
  them there tomorrow as well. A moment is chosen by a click or a tap, and
  scrubbed by dragging, so the chart reads the same under a finger as under
  a mouse; the reading sits beside the crosshair on a desktop and under the
  chart on a phone.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { listHostSamples } from '$lib/api/client';
  import type { Host, HostSample } from '$lib/api/types';
  import ChartPanel from '$lib/components/ChartPanel.svelte';
  import Segmented from '$lib/components/Segmented.svelte';
  import StatusDot from '$lib/components/StatusDot.svelte';
  import Switch from '$lib/components/Switch.svelte';
  import { fleet } from '$lib/state/fleet.svelte';
  import { storage } from '$lib/state/prefs.svelte';
  import { hostStatus } from '$lib/status';
  import {
    DEFAULT_METRICS,
    LONGEST_FINE_WINDOW,
    LONGEST_WINDOW,
    METRICS,
    WINDOWS,
    bridgeFor,
    grainOf,
    hostSeries,
    hostTone,
    lineRuns,
    liveSample,
    mergeHostSamples,
    metricText,
    metricValue,
    overflowCeiling,
    timeTicks,
    windowSlots,
    type MetricKey,
    type WindowKey,
  } from './hostSeries';
  import type { SignalPoint } from './signals';

  let { hosts, onmanage }: { hosts: Host[]; onmanage?: (host: Host) => void } = $props();

  /* -- what is remembered -------------------------------------------------- */

  const KEY = 'zoomies.hosts.map';
  function remembered<T>(name: string, fallback: T, valid: (v: unknown) => v is T): T {
    try {
      const raw = storage.get(`${KEY}.${name}`);
      if (raw === null) return fallback;
      const parsed: unknown = JSON.parse(raw);
      return valid(parsed) ? parsed : fallback;
    } catch {
      return fallback;
    }
  }
  const isWindow = (v: unknown): v is WindowKey =>
    typeof v === 'string' && WINDOWS.some((w) => w.value === v);
  const isMetrics = (v: unknown): v is MetricKey[] =>
    Array.isArray(v) && v.every((k) => METRICS.some((m) => m.key === k));
  const isIds = (v: unknown): v is string[] =>
    Array.isArray(v) && v.every((k) => typeof k === 'string');

  // The day is the default, as it is on every other trend: an hour is too
  // narrow to show a fleet that goes quiet overnight and busy at nine.
  let windowKey = $state<WindowKey>(remembered('window', '24h', isWindow));
  let live = $state(remembered('live', true, (v): v is boolean => typeof v === 'boolean'));
  let enabled = $state<MetricKey[]>(remembered('metrics', [...DEFAULT_METRICS], isMetrics));
  let hidden = $state<string[]>(remembered('hidden', [], isIds));
  $effect(() => storage.set(`${KEY}.window`, JSON.stringify(windowKey)));
  $effect(() => storage.set(`${KEY}.live`, JSON.stringify(live)));
  $effect(() => storage.set(`${KEY}.metrics`, JSON.stringify(enabled)));
  $effect(() => storage.set(`${KEY}.hidden`, JSON.stringify(hidden)));

  const chosen = $derived(
    WINDOWS.find((w) => w.value === windowKey) ?? WINDOWS.find((w) => w.value === '24h')!,
  );

  /* -- the samples ----------------------------------------------------------- */

  let history = $state.raw<HostSample[]>([]);
  let observed = $state.raw<HostSample[]>([]);
  let recent = $state.raw<HostSample[]>([]);
  let current = $state(Date.now());
  let failed = $state(false);
  let attempt = $state(0);

  /** The window's slots: its edges, how many points it has, how fine a slot is. */
  const slots = $derived(windowSlots(current, chosen.seconds, chosen.bucket));
  const bucketMs = $derived(chosen.bucket * 1000);

  $effect(() => {
    void attempt;
    const window = chosen;
    const controller = new AbortController();
    let disposed = false;
    void listHostSamples({ window: window.value }, controller.signal)
      .then((page) => {
        if (disposed) return;
        history = page.items ?? [];
        failed = false;
      })
      .catch((cause: unknown) => {
        if (disposed || (cause instanceof DOMException && cause.name === 'AbortError')) return;
        failed = true;
      });
    // The right-hand edge moves on every point: once a minute is enough for
    // a window drawn by the minute, and a window drawn finer keeps step.
    const timer = setInterval(() => (current = Date.now()), Math.min(30_000, window.bucket * 1000));
    return () => {
      disposed = true;
      controller.abort();
      clearInterval(timer);
    };
  });

  // The newest point of every line is the host as the stream last showed it.
  // Off, the chart is history alone, which is the honest picture when the
  // question is "what did yesterday look like" rather than "what is it doing".
  //
  // Two buffers, because the two kinds of window want different things kept.
  // The long one holds a point a minute for as far back as the widest window
  // reaches, which is what the windows drawn by the minute fold from. The
  // short one holds every ten-second slot for the last ten minutes, so the
  // sub-minute windows can show each heartbeat: kept at that grain for a
  // week it would be tens of thousands of samples a host, re-sorted on every
  // frame, for detail no window that wide could draw.
  $effect(() => {
    if (!live) return;
    const now = Date.now();
    const fresh = hosts.map((h) => liveSample(h, now));
    untrack(() => {
      observed = [
        ...mergeHostSamples(observed, fresh, now, LONGEST_WINDOW.seconds).values(),
      ].flat();
      recent = [
        ...mergeHostSamples(
          recent,
          fresh,
          now,
          LONGEST_FINE_WINDOW.seconds,
          grainOf(LONGEST_FINE_WINDOW.bucket),
        ).values(),
      ].flat();
      current = now;
    });
  });

  // The replay buffer is finite, so a reconnect reloads history, as every
  // other trend does.
  let wasLive = false;
  $effect(() => {
    const connected = fleet.connection === 'live';
    if (connected && !wasLive) untrack(() => (attempt += 1));
    wasLive = connected;
  });

  /* -- the lines ------------------------------------------------------------- */

  // A phone gets a taller, narrower drawing: the same 760-wide picture
  // scaled to 340px is a strip whose labels are unreadable.
  let narrow = $state(false);
  $effect(() => {
    const query = window.matchMedia('(max-width: 640px)');
    const update = () => (narrow = query.matches);
    update();
    query.addEventListener('change', update);
    return () => query.removeEventListener('change', update);
  });
  const W = $derived(narrow ? 420 : 760);
  const H = $derived(narrow ? 300 : 228);
  const LEFT = 46;
  const RIGHT = $derived(W - 12);
  const TOP = 14;
  const BOTTOM = $derived(H - 24);
  const SPAN = $derived(RIGHT - LEFT);

  const byHost = $derived(
    mergeHostSamples(
      history,
      live ? [...observed, ...recent] : [],
      current,
      chosen.seconds,
      slots.grain,
    ),
  );
  const count = $derived(slots.count);
  const visibleHosts = $derived(hosts.filter((h) => !hidden.includes(h.id ?? '')));
  const metrics = $derived(METRICS.filter((m) => enabled.includes(m.key)));

  interface Series {
    id: string;
    host: Host;
    index: number;
    metric: (typeof METRICS)[number];
    tone: string;
    points: SignalPoint[];
    runs: string[];
    last: { i: number; value: number } | null;
  }

  // The axis is 0-100% and stays so. Load is the one figure that can pass it,
  // and when it does in this window it gets a lane above the axis on its own
  // scale, rather than stretching the axis to wherever it reached and
  // squashing every other line into a strip at the bottom.
  const LANE = 40;
  const overflow = $derived.by(() => {
    if (!enabled.includes('load')) return 0;
    let peak: number | null = null;
    for (const host of visibleHosts)
      for (const s of byHost.get(host.id ?? '') ?? [])
        peak = Math.max(peak ?? 0, metricValue(s, 'load') ?? 0);
    return overflowCeiling(peak);
  });
  /** Where 100% sits: the top of the drawing, or under the lane when there is one. */
  const AXIS_TOP = $derived(overflow ? TOP + LANE : TOP);
  const x = (i: number) => LEFT + (i * SPAN) / Math.max(1, count - 1);
  const y = (value: number) => {
    if (value <= 100 || !overflow)
      return AXIS_TOP + (1 - Math.min(value, 100) / 100) * (BOTTOM - AXIS_TOP);
    return AXIS_TOP - ((Math.min(value, overflow) - 100) / (overflow - 100)) * LANE;
  };

  /** Every run of observed intervals a stroke joins, as one path each. */
  function runsOf(points: SignalPoint[]): string[] {
    return lineRuns(points, bridgeFor(chosen.bucket)).map((run) => {
      const d = run
        .map((p, n) => `${n ? 'L' : 'M'}${x(p.i).toFixed(1)},${y(p.value).toFixed(1)}`)
        .join(' ');
      // A single observed interval has no length to stroke: give it a dot's
      // worth so the line cap draws it.
      return run.length > 1 ? d : `${d} l0.01,0`;
    });
  }

  const series = $derived.by((): Series[] => {
    const out: Series[] = [];
    hosts.forEach((host, index) => {
      if (hidden.includes(host.id ?? '')) return;
      const samples = byHost.get(host.id ?? '') ?? [];
      for (const metric of metrics) {
        const points = hostSeries(samples, metric.key, current, chosen.seconds, chosen.bucket);
        let last: Series['last'] = null;
        for (let i = points.length - 1; i >= 0; i--) {
          const v = points[i]?.value;
          if (v !== null && v !== undefined) {
            last = { i, value: v };
            break;
          }
        }
        out.push({
          id: `${host.id}:${metric.key}`,
          host,
          index,
          metric,
          tone: hostTone(index),
          points,
          runs: runsOf(points),
          last,
        });
      }
    });
    return out;
  });

  const anyObserved = $derived(series.some((s) => s.last !== null));

  /* -- reading a moment ------------------------------------------------------ */

  let hover = $state<number | null>(null);
  let selected = $state<number | null>(null);
  const activeIndex = $derived(Math.min(count - 1, hover ?? selected ?? count - 1));
  /** The last slot of the active point, which is the moment its label names. */
  const activeAt = $derived(slots.end - (count - 1 - activeIndex) * bucketMs);

  /** Which point is under a pointer, from where it is across the chart. */
  function indexAt(event: PointerEvent): number {
    const rect = (event.currentTarget as SVGSVGElement).getBoundingClientRect();
    const viewX = ((event.clientX - rect.left) / rect.width) * W;
    const i = Math.round(((viewX - LEFT) / SPAN) * Math.max(1, count - 1));
    return Math.max(0, Math.min(count - 1, i));
  }

  // A mouse reads the chart by passing over it, and the reading goes when it
  // leaves. A finger cannot hover: it arrives, and the moment it leaves the
  // glass the pointer has left too, so a reading that lived only under the
  // pointer was gone before it could be read. So a touch chooses the moment,
  // the way the timeline control does, and dragging across the chart scrubs
  // it. A click does the same, because a chosen moment that stays put is
  // useful with a mouse as well: it is how two hosts get compared at one
  // instant without holding still.
  function onPress(event: PointerEvent): void {
    selected = indexAt(event);
    if (event.pointerType === 'mouse') return;
    // Capture so a drag that wanders off the chart keeps scrubbing. A drag
    // the browser takes for a vertical scroll cancels the pointer instead,
    // which is `touch-action: pan-y` doing its job.
    (event.currentTarget as SVGSVGElement).setPointerCapture(event.pointerId);
  }
  function onPointer(event: PointerEvent): void {
    if (event.pointerType === 'mouse') hover = indexAt(event);
    else if (event.buttons) selected = indexAt(event);
  }

  const fine = $derived(chosen.bucket < 60);
  const time = (at: number) =>
    new Date(at).toLocaleTimeString(undefined, {
      hour: '2-digit',
      minute: '2-digit',
      ...(fine ? { second: '2-digit' } : {}),
    });
  const stamp = (at: number) =>
    chosen.seconds > 86400
      ? new Date(at).toLocaleString(undefined, {
          weekday: 'short',
          hour: '2-digit',
          minute: '2-digit',
        })
      : time(at);

  /** The window's edges, and the round times between them for the labels. */
  const end = $derived(slots.end);
  const start = $derived(end - (count - 1) * bucketMs);
  const ticks = $derived(
    timeTicks(start, end, narrow ? 3 : 5).map((at) => ({
      at,
      x: LEFT + ((at - start) / Math.max(1, end - start)) * SPAN,
    })),
  );
  const grid = [0, 25, 50, 75, 100];

  /** What every visible host read at the active moment, for the card. */
  const reading = $derived(
    visibleHosts.map((host, n) => {
      const index = hosts.indexOf(host);
      // The sample in the active slot, for the exact figure: only where one
      // point is one slot, since a folded point is a peak of several.
      const grainMs = slots.grain * 1000;
      const sample =
        chosen.bucket === slots.grain
          ? byHost
              .get(host.id ?? '')
              ?.find(
                (s) => Math.floor(new Date(s.at ?? '').getTime() / grainMs) * grainMs === activeAt,
              )
          : undefined;
      return {
        host,
        tone: hostTone(index),
        n,
        figures: metrics.map((metric) => {
          const point = series.find((s) => s.host === host && s.metric === metric)?.points[
            activeIndex
          ];
          return {
            metric,
            percent: point?.value ?? null,
            exact: sample ? metricText(sample, metric.key) : null,
          };
        }),
      };
    }),
  );

  /* -- toggles ----------------------------------------------------------------- */

  function toggleMetric(key: MetricKey): void {
    enabled = enabled.includes(key) ? enabled.filter((k) => k !== key) : [...enabled, key];
  }
  function toggleHost(id: string): void {
    hidden = hidden.includes(id) ? hidden.filter((h) => h !== id) : [...hidden, id];
  }
  function only(id: string): void {
    const others = hosts.map((h) => h.id ?? '').filter((h) => h !== id);
    // A second "only" on the one host already alone brings everyone back.
    hidden =
      hidden.length === others.length && others.every((h) => hidden.includes(h)) ? [] : others;
  }

  /** The host as it is right now, for the legend's figures. */
  const now = $derived(new Map(hosts.map((h) => [h.id ?? '', liveSample(h, current)])));

  const description = $derived(
    `${hosts.length === 0 ? 'No hosts yet. ' : ''}${chosen.name}. Solid lines are measured by the agent; dashed ones are what the scheduler has promised or what the host holds. Each figure is a share of the machine, so they read against each other.`,
  );
</script>

<ChartPanel title="Host capacity map" {description}>
  {#snippet actions()}
    <div class="controls">
      <Switch bind:checked={live} label="Live" />
      <Segmented
        label="Window"
        value={windowKey}
        options={WINDOWS}
        onchange={(v) => (windowKey = v as WindowKey)}
      />
    </div>
  {/snippet}

  <div class="map">
    <div class="metrics" role="group" aria-label="Measurements shown">
      {#each METRICS as metric (metric.key)}
        <button
          type="button"
          class="metric"
          aria-pressed={enabled.includes(metric.key)}
          title={metric.hint}
          onclick={() => toggleMetric(metric.key)}
        >
          <svg class="stroke" viewBox="0 0 28 8" aria-hidden="true">
            <line
              x1="1"
              y1="4"
              x2="27"
              y2="4"
              stroke-dasharray={metric.dash}
              stroke-linecap="round"
            />
          </svg>
          {metric.label}
        </button>
      {/each}
    </div>

    <div class="plot">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        role="img"
        aria-label={`${series.length} lines across ${visibleHosts.length} of ${hosts.length} hosts, ${chosen.name.toLowerCase()}. Inspect the timeline below for exact values.`}
        onpointerdown={onPress}
        onpointermove={onPointer}
        onpointerleave={() => (hover = null)}
      >
        {#if overflow}
          <!-- The lane for load past the cores, on its own scale up to the
               peak in view. Its floor is the axis's 100%, which is why the
               line there is drawn firmer than the grid. -->
          <rect class="lane" x={LEFT} y={TOP} width={SPAN} height={LANE} />
          <line class="grid" x1={LEFT} x2={RIGHT} y1={TOP} y2={TOP} />
          <text class="axis" x={LEFT - 8} y={TOP + 4} text-anchor="end">{overflow}%</text>
          <text class="lane-label" x={RIGHT - 6} y={TOP + 12} text-anchor="end">
            load past the cores
          </text>
        {/if}
        <!-- The pressure band: what a throttle steps a host down for. -->
        <rect class="pressure" x={LEFT} y={y(100)} width={SPAN} height={y(90) - y(100)} />
        <text class="band-label" x={LEFT + 6} y={y(95) + 4}>pressure</text>
        {#each grid as value (value)}
          <line
            class="grid"
            class:edge={value === 100 && overflow > 0}
            x1={LEFT}
            x2={RIGHT}
            y1={y(value)}
            y2={y(value)}
          />
          <text class="axis" x={LEFT - 8} y={y(value) + 4} text-anchor="end">{value}%</text>
        {/each}
        {#each series as s (s.id)}
          {#each s.runs as d, n (n)}
            <path
              {d}
              class="line"
              style:stroke={s.tone}
              stroke-dasharray={s.metric.dash}
              vector-effect="non-scaling-stroke"
            />
          {/each}
          {#if s.last && live && s.last.i === count - 1}
            <circle class="pulse" cx={x(s.last.i)} cy={y(s.last.value)} r="4" style:fill={s.tone} />
          {/if}
        {/each}
        <line class="cursor" x1={x(activeIndex)} x2={x(activeIndex)} y1={TOP} y2={BOTTOM} />
        {#each series as s (s.id)}
          {@const v = s.points[activeIndex]?.value}
          {#if v !== null && v !== undefined}
            <circle class="marker" cx={x(activeIndex)} cy={y(v)} r="4" style:fill={s.tone} />
          {/if}
        {/each}
        {#each ticks as tick (tick.at)}
          <line class="tick" x1={tick.x} x2={tick.x} y1={BOTTOM} y2={BOTTOM + 4} />
          <text class="axis" x={tick.x} y={H - 6} text-anchor="middle">{stamp(tick.at)}</text>
        {/each}
      </svg>

      {#if !anyObserved}
        <p class="empty">
          {#if hosts.length === 0}
            No hosts connected yet.
          {:else if metrics.length === 0}
            Choose a measurement above to draw it.
          {:else if visibleHosts.length === 0}
            Every host is switched off. Choose one below.
          {:else}
            No samples in this window yet. The controller writes one a minute.
          {/if}
        </p>
      {/if}

      {#if (hover !== null || selected !== null) && reading.length > 0 && metrics.length > 0}
        <div
          class="card"
          class:right={activeIndex > count / 2}
          style:left="{(100 * x(activeIndex)) / W}%"
          aria-hidden="true"
        >
          <p class="when">
            {stamp(activeAt)}{#if chosen.bucket > 60}
              · {chosen.bucket / 60}-minute peak{/if}
          </p>
          {#each reading as r (r.host.id)}
            <div class="row">
              <span class="swatch" style:background={r.tone}></span>
              <span class="name">{r.host.name ?? r.host.id}</span>
              {#each r.figures as f (f.metric.key)}
                <span class="figure" title={f.metric.label}>
                  <svg class="stroke" viewBox="0 0 20 8" aria-hidden="true">
                    <line
                      x1="1"
                      y1="4"
                      x2="19"
                      y2="4"
                      stroke-dasharray={f.metric.dash}
                      stroke-linecap="round"
                      style:stroke={r.tone}
                    />
                  </svg>
                  <span class="short">{f.metric.short}</span>
                  {#if f.percent === null}
                    <span class="gap">–</span>
                  {:else}
                    <strong>{f.percent.toFixed(0)}%</strong>
                    {#if f.exact && f.metric.key !== 'cpu'}<small>{f.exact}</small>{/if}
                  {/if}
                </span>
              {/each}
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <div class="scrub">
      <label>
        <span>Inspect a moment</span>
        <input
          type="range"
          min="0"
          max={Math.max(0, count - 1)}
          value={selected ?? count - 1}
          oninput={(e) => (selected = Number(e.currentTarget.value))}
          aria-valuetext={`${stamp(activeAt)}: ${reading
            .map(
              (r) =>
                `${r.host.name ?? r.host.id} ${r.figures
                  .map(
                    (f) =>
                      `${f.metric.label} ${f.percent === null ? 'not measured' : `${f.percent.toFixed(0)}%`}`,
                  )
                  .join(', ')}`,
            )
            .join('; ')}`}
        />
      </label>
      <output>{stamp(activeAt)}</output>
      {#if selected !== null}
        <!-- A chosen moment stays chosen until it is let go of, so there has
             to be a way to let go: on a phone, where a tap chose it, there is
             no pointer to move away. -->
        <button type="button" class="text" onclick={() => (selected = null)}>Back to now</button>
      {/if}
    </div>

    <div class="hosts" role="group" aria-label="Hosts shown">
      {#each hosts as host, index (host.id)}
        {@const id = host.id ?? ''}
        {@const shown = !hidden.includes(id)}
        {@const sample = now.get(id)}
        <div class="host" class:off={!shown}>
          <button
            type="button"
            class="toggle"
            aria-pressed={shown}
            onclick={() => toggleHost(id)}
            title={shown ? `Hide ${host.name ?? id}` : `Show ${host.name ?? id}`}
          >
            <span class="swatch" style:background={hostTone(index)}></span>
            <span class="name">{host.name ?? id}</span>
            <StatusDot status={hostStatus(host)} size="sm" />
          </button>
          <span class="figures">
            {#each metrics as metric (metric.key)}
              {@const v = sample ? metricValue(sample, metric.key) : null}
              <span
                class="figure"
                title={`${metric.label}: ${sample ? metricText(sample, metric.key) : 'not measured'}`}
              >
                <svg class="stroke" viewBox="0 0 20 8" aria-hidden="true">
                  <line
                    x1="1"
                    y1="4"
                    x2="19"
                    y2="4"
                    stroke-dasharray={metric.dash}
                    stroke-linecap="round"
                    style:stroke={hostTone(index)}
                  />
                </svg>
                <span class="short" aria-label={metric.label}>{metric.short}</span>
                {#if v === null}<span class="gap">–</span>{:else}<strong>{v.toFixed(0)}%</strong
                  >{/if}
              </span>
            {/each}
          </span>
          <span class="host-actions">
            <button type="button" class="text" onclick={() => only(id)}>Only</button>
            {#if onmanage}<button type="button" class="text" onclick={() => onmanage?.(host)}
                >Manage</button
              >{/if}
            <a href={`/usage?group_by=host&entity=${encodeURIComponent(id)}`}>History ↗</a>
          </span>
        </div>
      {/each}
    </div>

    {#if failed}
      <p class="retry">
        Earlier samples could not be loaded. Live observations are still shown.
        <button type="button" class="text" onclick={() => (attempt += 1)}>Retry history</button>
      </p>
    {/if}
  </div>
</ChartPanel>

<style>
  .controls {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-3);
  }
  .map {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
    min-width: 0;
  }

  /* -- measurements: a row of chips, each carrying its stroke ---------------- */
  .metrics {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
  .metric {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    padding: var(--z-space-1) var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-full);
    background: var(--z-surface);
    color: var(--z-text-muted);
    font: inherit;
    font-size: var(--z-text-xs);
    cursor: pointer;
    transition:
      background var(--z-motion-fast) var(--z-ease),
      color var(--z-motion-fast) var(--z-ease),
      border-color var(--z-motion-fast) var(--z-ease);
  }
  .metric:hover {
    border-color: var(--z-border-strong);
    color: var(--z-text);
  }
  .metric[aria-pressed='true'] {
    background: var(--z-accent-subtle);
    border-color: var(--z-accent-border);
    color: var(--z-text);
  }
  .metric[aria-pressed='true'] .stroke line {
    stroke: var(--z-accent);
  }
  .stroke {
    width: 28px;
    height: 8px;
    flex: none;
  }
  .stroke line {
    stroke: currentColor;
    stroke-width: 2;
  }

  /* -- the chart --------------------------------------------------------------- */
  .plot {
    position: relative;
  }
  svg {
    display: block;
    width: 100%;
    height: auto;
    touch-action: pan-y;
  }
  .pressure {
    fill: var(--z-pending-subtle);
  }
  .band-label,
  .lane-label {
    font-size: 10px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }
  .band-label {
    fill: var(--z-pending);
  }
  .grid {
    stroke: var(--z-border);
    stroke-dasharray: 2 4;
  }
  .grid.edge {
    stroke: var(--z-border-strong);
    stroke-dasharray: none;
  }
  .lane {
    fill: var(--z-surface-sunken);
  }
  .lane-label {
    fill: var(--z-text-subtle);
  }
  .axis {
    fill: var(--z-text-muted);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
  }
  .line {
    fill: none;
    stroke-width: 2;
    stroke-linejoin: round;
    stroke-linecap: round;
  }
  .cursor {
    stroke: var(--z-text-subtle);
    stroke-dasharray: 3 3;
  }
  .marker {
    stroke: var(--z-surface);
    stroke-width: 2;
  }
  /* The newest point of a live line breathes, so "live" is something seen
     rather than a switch that says so. Under reduced motion it holds still. */
  .pulse {
    transform-box: fill-box;
    transform-origin: center;
    animation: breathe 2.4s var(--z-ease) infinite;
  }
  @keyframes breathe {
    0% {
      opacity: 0.9;
      transform: scale(1);
    }
    50% {
      opacity: 0.25;
      transform: scale(2.2);
    }
    100% {
      opacity: 0.9;
      transform: scale(1);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .pulse {
      animation: none;
    }
  }
  .empty {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    pointer-events: none;
  }

  /* -- the card beside the crosshair --------------------------------------- */
  .card {
    position: absolute;
    top: 0;
    transform: translateX(var(--z-space-2));
    min-width: 12rem;
    max-width: 22rem;
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
    padding-bottom: var(--z-space-1);
    border-bottom: var(--z-border-width) solid var(--z-border);
    margin-bottom: var(--z-space-1);
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-1) var(--z-space-2);
    padding: var(--z-nudge-2) 0;
  }
  .row .name {
    font-weight: var(--z-weight-medium);
    margin-right: var(--z-space-1);
  }
  .swatch {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: var(--z-radius-sm);
    flex: none;
  }
  .figure {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .figure .stroke {
    width: 20px;
  }
  .figure strong {
    font-weight: var(--z-weight-semibold);
  }
  .figure small {
    color: var(--z-text-muted);
  }
  .gap {
    color: var(--z-text-subtle);
  }

  /* -- inspecting by keyboard ------------------------------------------------ */
  .scrub {
    display: flex;
    align-items: center;
    gap: var(--z-space-4);
    flex-wrap: wrap;
  }
  .scrub label {
    display: flex;
    align-items: center;
    gap: var(--z-space-3);
    flex: 1;
    font-size: var(--z-text-xs);
    min-width: 0;
  }
  .scrub label span {
    white-space: nowrap;
    color: var(--z-text-muted);
  }
  .scrub input {
    width: 100%;
    min-width: 70px;
    accent-color: var(--z-accent);
    height: var(--z-space-6);
  }
  output {
    font-size: var(--z-text-xs);
    font-variant-numeric: tabular-nums;
  }

  /* -- the hosts: one row each, the legend and the switchboard --------------- */
  .hosts {
    display: grid;
    gap: var(--z-space-1);
    border-top: var(--z-border-width) solid var(--z-border);
    padding-top: var(--z-space-3);
  }
  .host {
    display: grid;
    grid-template-columns: minmax(10rem, 1fr) auto auto;
    align-items: center;
    gap: var(--z-space-2) var(--z-space-4);
    padding: var(--z-space-1) var(--z-space-2);
    border-radius: var(--z-radius-sm);
    font-size: var(--z-text-xs);
    transition: opacity var(--z-motion-fast) var(--z-ease);
  }
  .host:hover {
    background: var(--z-surface-sunken);
  }
  .host.off .figures,
  .host.off .name {
    opacity: 0.5;
  }
  .host.off .swatch {
    background: transparent !important;
    box-shadow: inset 0 0 0 var(--z-border-width) var(--z-border-strong);
  }
  .toggle {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    min-width: 0;
    padding: var(--z-space-1) var(--z-space-2);
    margin-left: calc(-1 * var(--z-space-2));
    border: 0;
    border-radius: var(--z-radius-sm);
    background: none;
    color: var(--z-text);
    font: inherit;
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    text-align: left;
    cursor: pointer;
  }
  .toggle .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .figures {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-1) var(--z-space-3);
    justify-content: flex-end;
  }
  .host-actions {
    display: flex;
    gap: var(--z-space-3);
    white-space: nowrap;
  }
  .host-actions a {
    color: var(--z-accent);
  }
  .text {
    padding: 0;
    border: 0;
    background: none;
    color: var(--z-accent);
    font: inherit;
    font-size: var(--z-text-xs);
    cursor: pointer;
  }
  .text:hover {
    text-decoration: underline;
  }
  .short {
    color: var(--z-text-muted);
  }
  .tick {
    stroke: var(--z-border-strong);
  }
  .retry {
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  @media (max-width: 640px) {
    .host {
      grid-template-columns: 1fr;
    }
    .figures {
      justify-content: flex-start;
    }
    /* On a phone the reading sits under the chart rather than beside the
       crosshair: a card at the finger is a card under the finger, and one
       twenty-two rems wide beside a point near the edge is off the screen. */
    .card,
    .card.right {
      position: static;
      transform: none;
      max-width: none;
      margin-top: var(--z-space-2);
    }
  }
  /* A finger is wider than a pointer. Every target on the map -- the chips,
     the hosts' switches, Only and Manage, the timeline's thumb -- rises to
     the touch size where the pointer is coarse, and nowhere else: the desktop
     rows stay dense, since a mouse is fine with a row of text. */
  @media (pointer: coarse) {
    .metric,
    .toggle,
    .text,
    .host-actions a {
      min-height: var(--z-control-touch);
    }
    .text,
    .host-actions a {
      display: inline-flex;
      align-items: center;
      padding: 0 var(--z-space-2);
    }
    .scrub input {
      height: var(--z-control-touch);
    }
    .host {
      padding-block: 0;
    }
  }
</style>
