<!--
  Fleet activity: what the fleet has been doing lately, every figure at once.

  It used to draw one of them at a time, chosen from a dropdown, which made
  the one question the panel exists to answer -- was anything waiting, and
  was there anything free to take it -- two separate looks at two separate
  charts. Now the queue, the running jobs, the idle, busy and live runners
  are chips, and each one on the chart is a line: the colour is the status
  colour the console uses for that state everywhere else, and the stroke
  says whether the figure counts jobs or runners. The two busy figures share
  a colour on purpose, since a running job and the runner running it should
  lie on top of one another.

  Where jobs queued with nothing idle to take them, the chart is shaded. It
  is this panel's version of the capacity map's pressure band: a count of
  jobs has no 85% to draw a line at, but "something was waiting and nothing
  was free" needs no threshold to be worth seeing.

  The rows beneath are the legend, the switchboard and the reading at once:
  each figure's meter shows what it was at the moment under the crosshair,
  or now, so the exact numbers for a moment are never only in a card that
  follows the pointer. The line above the chart names the peak in view and
  how much of the window the fleet spent short of runners.

  The controller writes one sample a minute and the stream keeps the newest
  point moving between fetches; a longer window folds those minutes into
  intervals that carry their peak, because a queue that hit twelve for two
  minutes is what an operator looking at a day is looking for. Each figure
  carries its own peak through a fold, so a folded point is five readings
  from the same interval rather than one minute's snapshot. A minute nobody
  sampled is a gap and never a zero: a controller that was down and an empty
  queue are opposite news. History reloads on a reconnect, for the same
  reason everything else does: the replay buffer is finite.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { listSamples } from '$lib/api/client';
  import { events } from '$lib/api/sse';
  import type { FleetSample, Stats } from '$lib/api/types';
  import { fleet } from '$lib/state/fleet.svelte';
  import { formatNumber } from '$lib/format';
  import ChartPanel from '$lib/components/ChartPanel.svelte';
  import Segmented from '$lib/components/Segmented.svelte';
  import { remember, remembered } from '$lib/state/prefs.svelte';
  import TrendPlot from './TrendPlot.svelte';
  import FigureChips from './FigureChips.svelte';
  import FigureRows from './FigureRows.svelte';
  import ReadingCard from './ReadingCard.svelte';
  import { countAxis, seriesPeak } from './plot';
  import {
    DEFAULT_SIGNALS,
    SIGNALS,
    foldMinutes,
    leadSignal,
    mergeSamples,
    signalLines,
    starvedRuns,
    type SignalKey,
  } from './signals';

  let { others = false }: { others?: boolean } = $props();

  /**
   * The windows on offer, and how many minutes each point of the chart
   * folds. Sixty points read; fourteen hundred set as a smear, so a day is
   * ninety-six quarter-hours.
   */
  const WINDOWS = [
    { value: '1h', label: '1h', name: 'The last hour, minute by minute', minutes: 60, step: 1 },
    {
      value: '6h',
      label: '6h',
      name: 'The last 6 hours, in 5-minute peaks',
      minutes: 6 * 60,
      step: 5,
    },
    {
      value: '24h',
      label: '24h',
      name: 'The last 24 hours, in 15-minute peaks',
      minutes: 24 * 60,
      step: 15,
    },
  ] as const;
  type WindowKey = (typeof WINDOWS)[number]['value'];

  /* -- what is remembered -------------------------------------------------- */

  const KEY = 'zoomies.fleet.trend';
  const isWindow = (v: unknown): v is WindowKey =>
    typeof v === 'string' && WINDOWS.some((w) => w.value === v);
  const isSignals = (v: unknown): v is SignalKey[] =>
    Array.isArray(v) && v.every((k) => SIGNALS.some((s) => s.key === k));

  // The day is the default window everywhere a range is chosen: an hour is
  // too narrow to show a fleet that goes quiet overnight and busy at nine.
  let windowKey = $state<WindowKey>(remembered(`${KEY}.window`, '24h', isWindow));
  let enabled = $state<SignalKey[]>(remembered(`${KEY}.signals`, [...DEFAULT_SIGNALS], isSignals));
  $effect(() => remember(`${KEY}.window`, windowKey));
  $effect(() => remember(`${KEY}.signals`, enabled));

  const chosen = $derived(WINDOWS.find((w) => w.value === windowKey) ?? WINDOWS[2]);

  /* -- the samples ----------------------------------------------------------- */

  let samples = $state.raw<FleetSample[]>([]);
  let current = $state(Date.now());
  let failed = $state(false);
  let loading = $state(false);
  let attempt = $state(0);

  function liveRunners(r: Stats['runners']): number | undefined {
    return r
      ? (r.provisioning ?? 0) +
          (r.registering ?? 0) +
          (r.busy ?? 0) +
          (r.idle ?? 0) +
          (r.draining ?? 0)
      : undefined;
  }
  function record(stats: Stats) {
    const now = Date.now();
    const r = stats.runners;
    samples = mergeSamples(
      samples,
      [
        {
          at: new Date(now).toISOString(),
          queued_jobs: stats.queued_jobs,
          running_jobs: stats.running_jobs,
          fleet_queued_jobs: stats.fleet?.queued_jobs,
          fleet_running_jobs: stats.fleet?.running_jobs,
          idle_runners: r?.idle,
          busy_runners: r?.busy,
          total_runners: liveRunners(r),
        },
      ],
      now,
      chosen.minutes,
    );
    current = now;
  }
  $effect(() => {
    void attempt;
    const window = chosen;
    const controller = new AbortController();
    let disposed = false;
    untrack(() => {
      loading = true;
      if (fleet.stats) record(fleet.stats);
    });
    void listSamples({ window: window.value }, controller.signal)
      .then((page) => {
        if (disposed) return;
        samples = mergeSamples(page.items ?? [], samples, Date.now(), window.minutes);
        failed = false;
      })
      .catch((cause: unknown) => {
        if (disposed || (cause instanceof DOMException && cause.name === 'AbortError')) return;
        failed = true;
      })
      .finally(() => {
        if (!disposed) loading = false;
      });
    const unsubscribe = events.subscribe('stats', record);
    const timer = setInterval(() => {
      current = Date.now();
    }, 60000);
    return () => {
      disposed = true;
      controller.abort();
      unsubscribe();
      clearInterval(timer);
    };
  });
  let wasLive = false;
  $effect(() => {
    const live = fleet.connection === 'live';
    if (live && !wasLive) {
      untrack(() => (attempt += 1));
    }
    wasLive = live;
  });

  /* -- the lines ------------------------------------------------------------- */

  const signals = $derived(SIGNALS.filter((s) => enabled.includes(s.key)));
  const lead = $derived(leadSignal(enabled));
  const lines = $derived(
    signalLines(samples, enabled, others, current, chosen.minutes, chosen.step),
  );
  const count = $derived(
    Math.max(1, lines[0]?.points.length ?? Math.ceil(chosen.minutes / chosen.step)),
  );
  const peak = $derived(seriesPeak(lead ? lines.filter((l) => l.series === lead) : []));
  const axis = $derived(
    countAxis(
      lines.reduce(
        (top, line) =>
          line.points.reduce((n, p) => (p.value !== null && p.value > n ? p.value : n), top),
        0,
      ),
    ),
  );

  // The shading is drawn from the queue and the idle runners whether or not
  // they are chips on the chart: it says the fleet ran short, and that is
  // true of the window regardless of which figures the operator is looking
  // at. Reading it off the chosen lines would make it blink out the moment
  // somebody switched a figure off.
  // It is judged a minute at a time and folded afterwards, which is why these
  // are drawn at the minute whatever the window is: a fifteen-minute point
  // carries each figure's peak, and the most idle runners there were at any
  // moment in a quarter of an hour says nothing about whether anything was
  // free when the queue was deep.
  const pressure = $derived(
    signalLines(samples, ['queue', 'idle'], others, current, chosen.minutes, 1),
  );
  const starved = $derived(
    starvedRuns(
      pressure.find((l) => l.series.key === 'queue') ?? null,
      pressure.find((l) => l.series.key === 'idle') ?? null,
      chosen.step,
    ),
  );
  const starvedCount = $derived(starved.reduce((n, run) => n + (run.to - run.from + 1), 0));

  /* -- reading a moment ------------------------------------------------------ */

  let hover = $state<number | null>(null);
  let selected = $state<number | null>(null);
  const reading = $derived(hover !== null || selected !== null);
  const activeIndex = $derived(Math.min(count - 1, hover ?? selected ?? count - 1));
  // The timeline is the drawn points, and with no figure chosen it is still
  // the window's shape: an empty chart keeps its axis and its scrubber.
  const points = $derived(lines[0]?.points ?? foldMinutes(pressure[0]?.points ?? [], chosen.step));
  const activeAt = $derived(points[activeIndex]?.at ?? current);
  const start = $derived(points[0]?.at ?? current);
  const end = $derived(points[points.length - 1]?.at ?? current);

  const time = (at: number) =>
    new Date(at).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
  const stamp = (at: number) =>
    chosen.minutes > 60 * 12
      ? new Date(at).toLocaleString(undefined, {
          weekday: 'short',
          hour: '2-digit',
          minute: '2-digit',
        })
      : time(at);
  const unit = $derived(chosen.step === 1 ? 'minutes' : `${chosen.step}-minute intervals`);
  const moment = $derived(reading ? `at ${stamp(activeAt)}` : 'now');

  /** What each chosen figure read at the active moment, or now. */
  const readings = $derived(
    lines.map((line) => ({
      series: line.series,
      value: reading ? (line.points[activeIndex]?.value ?? null) : (line.last?.value ?? null),
    })),
  );
  /** How much of the window was actually observed, along the lead line. */
  const observed = $derived((lines[0]?.points ?? []).filter((p) => p.value !== null).length);

  /* -- emphasis ------------------------------------------------------------------ */

  // Two ways to single a figure out, and either will do: its chip or its row,
  // and the line the pointer is nearest. A chip that is not on the chart
  // singles out nothing -- dimming every line to show that a figure is absent
  // reads as a fault, not an answer.
  let chip = $state<SignalKey | null>(null);
  let near = $state<string | null>(null);
  const emphasis = $derived.by(() => {
    const key = chip ?? near;
    return SIGNALS.find((s) => s.key === key && enabled.includes(s.key))?.key ?? null;
  });

  function toggle(key: SignalKey): void {
    enabled = enabled.includes(key) ? enabled.filter((k) => k !== key) : [...enabled, key];
  }

  const valuetext = $derived(
    `${stamp(activeAt)}: ${
      readings.length
        ? readings
            .map((r) => `${r.series.label} ${r.value === null ? 'not sampled' : r.value}`)
            .join(', ')
        : 'no figures chosen'
    }`,
  );
  const description = $derived(
    `${others ? 'All GitHub-reported jobs' : 'Zoomies jobs'}; runner counts always belong to this fleet. ${chosen.name}. Independent of table filters.`,
  );
</script>

{#snippet card()}
  <ReadingCard
    when={stamp(activeAt)}
    note={chosen.step > 1 ? `${chosen.step}-minute peaks` : undefined}
    {readings}
    {emphasis}
  />
{/snippet}

<ChartPanel title="Fleet activity" {description}>
  {#snippet actions()}
    <div class="controls">
      <Segmented
        label="Window"
        value={windowKey}
        options={WINDOWS}
        onchange={(v) => (windowKey = v as WindowKey)}
      />
    </div>
  {/snippet}

  <div class="trend">
    <div class="toolbar">
      <FigureChips
        label="Figures shown"
        figures={SIGNALS}
        {enabled}
        group={(figure) => (figure.jobs ? 'jobs' : 'runners')}
        ontoggle={toggle}
        onchip={(key) => (chip = key)}
      />
      {#if observed > 0}
        <p class="headline" aria-live="off">
          {#if peak && lead}
            <span
              >Peak in view: <strong>{formatNumber(peak.value)}</strong>
              {lead.label.toLowerCase()}, at {stamp(peak.line.points[peak.i]?.at ?? activeAt)}</span
            >
          {/if}
          <span>
            <strong class:warn={starvedCount > 0}>{starvedCount}</strong>
            of {count}
            {unit} with jobs queued and nothing idle
          </span>
        </p>
      {/if}
    </div>

    <TrendPlot
      {lines}
      {count}
      ceiling={axis.ceiling}
      grid={axis.values}
      {start}
      {end}
      bands={starved}
      {activeIndex}
      {reading}
      {emphasis}
      lead={lead?.key ?? null}
      present={count - 1}
      revealKey={`${windowKey}:${others}`}
      {stamp}
      {loading}
      message={signals.length === 0
        ? 'Choose a figure above to draw it.'
        : observed === 0
          ? 'No samples recorded in this window yet.'
          : undefined}
      label={`Fleet activity: ${signals.map((s) => s.label.toLowerCase()).join(', ') || 'no figures chosen'}; ${observed} observed ${unit}. Inspect the timeline below for exact values.`}
      onhover={(i) => (hover = i)}
      onpress={(i) => (selected = i)}
      onnear={(key) => (near = key)}
      card={readings.length ? card : undefined}
    />

    <div class="scrub">
      <label>
        <span>Inspect a moment</span>
        <input
          type="range"
          min="0"
          max={Math.max(0, count - 1)}
          value={selected ?? count - 1}
          oninput={(e) => (selected = Number(e.currentTarget.value))}
          aria-valuetext={valuetext}
        />
      </label>
      <output
        >{stamp(activeAt)}{#if !reading}<span class="now"> · now</span>{/if}</output
      >
      {#if selected !== null}
        <!-- A chosen moment stays chosen until it is let go of, so there has
             to be a way to let go: on a phone, where a tap chose it, there is
             no pointer to move away. -->
        <button type="button" class="text" onclick={() => (selected = null)}>Back to now</button>
      {/if}
    </div>

    <FigureRows
      label="Figures read at this moment"
      {readings}
      ceiling={axis.ceiling}
      {emphasis}
      {moment}
      empty="No figures chosen. Switch one on above to draw it."
      ontoggle={toggle}
      onchip={(key) => (chip = key)}
    />

    <div class="coverage" aria-label={`${observed} of ${count} ${unit} observed`}>
      {#each points as point, i (point.at)}<span
          class:observed={point.value !== null}
          class:starved={starved.some((run) => i >= run.from && i <= run.to)}
          title={`${stamp(point.at)}: ${point.value === null ? 'not sampled' : formatNumber(point.value)}`}
        ></span>{/each}
    </div>
    <p class="note">{observed} / {count} {unit} observed · gaps mean no sample</p>

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
    gap: var(--z-space-2);
  }
  .trend {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    min-width: 0;
  }

  /* -- the chips and the headline -------------------------------------------- */
  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-2) var(--z-space-4);
  }
  .headline {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-1) var(--z-space-4);
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .headline strong {
    color: var(--z-text);
    font-weight: var(--z-weight-semibold);
    font-variant-numeric: tabular-nums;
  }
  .headline strong.warn {
    color: var(--z-pending);
  }

  /* -- the timeline ------------------------------------------------------------ */
  .scrub {
    display: flex;
    align-items: center;
    gap: var(--z-space-3);
    flex-wrap: wrap;
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
    accent-color: var(--z-accent);
    height: var(--z-space-6);
  }
  output {
    font-size: var(--z-text-xs);
    font-variant-numeric: tabular-nums;
  }
  output .now {
    color: var(--z-text-muted);
  }

  /* -- the coverage strip ------------------------------------------------------- */
  .coverage {
    display: flex;
    gap: var(--z-border-width);
    height: var(--z-space-2);
  }
  .coverage span {
    flex: 1;
    background: var(--z-surface-sunken);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
  }
  .coverage .observed {
    background: var(--z-accent);
    border-color: var(--z-accent);
  }
  .coverage .starved {
    background: var(--z-pending);
    border-color: var(--z-pending);
  }
  .note,
  .retry {
    margin: 0;
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
  }

  button.text {
    padding: 0;
    border: 0;
    background: none;
    color: var(--z-accent);
    font-size: var(--z-text-xs);
    text-decoration: underline;
    cursor: pointer;
  }
</style>
