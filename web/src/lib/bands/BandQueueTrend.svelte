<!--
  The Queue band's picture: the last hour as two sparklines, what was waiting
  and what was running, a sample a minute. The figures beside it say how many
  are waiting now; this says which way that number is going, which is the one
  thing a figure cannot carry.

  The samples are the ones the controller writes once a minute for the fleet
  trend panel; the newest point is the band's own figures, replaced as each
  stats frame arrives, so the line's end and the sentence never disagree. A
  minute nobody sampled is a gap, never a zero. A reconnect reloads, for the
  reason everything else does: the replay buffer is finite. When the fetch
  fails the picture draws nothing, as the punch card does: the band is the
  page's answer and an error here would shout about a picture.
-->
<script lang="ts">
  import { listSamples } from '$lib/api/client';
  import { events } from '$lib/api/sse';
  import type { FleetSample, Stats } from '$lib/api/types';
  import { fleet } from '$lib/state/fleet.svelte';
  import { onClockTick } from '$lib/format';
  import { sparkPath, trendPoints, trendWords, withNow, type TrendPoint } from './trend';

  interface Props {
    /** Slots the fleet has, so running is read against something. */
    slots?: number;
  }
  let { slots }: Props = $props();

  const MINUTES = 60;
  const WIDTH = 240;
  const HEIGHT = 48;

  let samples = $state.raw<FleetSample[]>([]);
  let live = $state.raw<TrendPoint | null>(null);
  let now = $state(Date.now());
  let failed = $state(false);
  let loading = $state(true);
  let pending: AbortController | null = null;

  function pointOf(stats: Stats, at: number): TrendPoint {
    const own = stats.fleet ?? stats;
    return { at, queued: own.queued_jobs ?? 0, running: own.running_jobs ?? 0 };
  }

  export async function reload(): Promise<void> {
    pending?.abort();
    const controller = new AbortController();
    pending = controller;
    try {
      const page = await listSamples({ window: '1h' }, controller.signal);
      if (controller.signal.aborted || pending !== controller) return;
      samples = page.items ?? [];
      failed = false;
    } catch {
      if (controller.signal.aborted || pending !== controller) return;
      failed = true;
    } finally {
      if (!controller.signal.aborted && pending === controller) loading = false;
    }
  }

  $effect(() => {
    void reload();
    return () => pending?.abort();
  });

  let previous: string | null = null;
  $effect(() => {
    const status = fleet.connection;
    if (previous !== null && previous !== 'live' && status === 'live') void reload();
    previous = status;
  });

  // The newest point is now, from the same frames the figures are drawn from.
  $effect(() => {
    if (fleet.stats) live = pointOf(fleet.stats, Date.now());
  });
  $effect(() => events.subscribe('stats', (stats: Stats) => (live = pointOf(stats, Date.now()))));
  $effect(() => onClockTick((at) => (now = at)));

  const points = $derived.by(() => {
    const base = trendPoints(samples, now, MINUTES);
    return live ? withNow(base, live, MINUTES) : base;
  });
  const from = $derived(now - MINUTES * 60_000);
  const queuedMax = $derived(Math.max(1, ...points.map((p) => p.queued)));
  const runningMax = $derived(Math.max(1, slots ?? 0, ...points.map((p) => p.running)));
  const queued = $derived(
    sparkPath(
      points.map((p) => ({ at: p.at, value: p.queued })),
      { width: WIDTH, height: HEIGHT, from, to: now, max: queuedMax },
    ),
  );
  const running = $derived(
    sparkPath(
      points.map((p) => ({ at: p.at, value: p.running })),
      { width: WIDTH, height: HEIGHT, from, to: now, max: runningMax },
    ),
  );
  const last = $derived(points[points.length - 1] ?? null);
  const reading = $derived(
    loading
      ? 'Loading the last hour'
      : `${trendWords(
          points.map((p) => p.queued),
          'Waiting',
        )}; ${trendWords(
          points.map((p) => p.running),
          'running',
        )}.`,
  );
</script>

{#if !failed}
  <div class="trend" data-testid="band-queue-trend" aria-busy={loading}>
    <div class="head">
      <span class="title">The last hour</span>
      <span class="summary">a sample a minute</span>
    </div>
    <div class="pair">
      <figure class="spark">
        <figcaption>
          <span>Waiting</span>
          <b>{last?.queued ?? 0}</b>
        </figcaption>
        <svg
          viewBox="0 0 {WIDTH} {HEIGHT}"
          role="img"
          aria-label={`Waiting over the last hour, now ${last?.queued ?? 0}`}
        >
          <line x1="2" x2={WIDTH - 2} y1={HEIGHT / 2} y2={HEIGHT / 2} class="grid" />
          <path d={queued.area} class="area" />
          <path d={queued.line} class="line" />
        </svg>
      </figure>
      <figure class="spark">
        <figcaption>
          <span>Running</span>
          <b
            >{last?.running ?? 0}{#if slots}<small> of {slots} slots</small>{/if}</b
          >
        </figcaption>
        <svg
          viewBox="0 0 {WIDTH} {HEIGHT}"
          role="img"
          aria-label={`Running over the last hour, now ${last?.running ?? 0}`}
        >
          <line x1="2" x2={WIDTH - 2} y1={HEIGHT / 2} y2={HEIGHT / 2} class="grid" />
          <path d={running.area} class="area" />
          <path d={running.line} class="line" />
        </svg>
      </figure>
    </div>
    <p class="reading">{reading}</p>
  </div>
{/if}

<style>
  .trend {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
    min-width: 0;
    width: 100%;
    color: var(--z-panel-text-muted);
  }
  .head {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: var(--z-space-3);
    flex-wrap: wrap;
  }
  .title {
    font-size: var(--z-text-2xs);
    font-weight: var(--z-weight-semibold);
    letter-spacing: var(--z-tracking-wider);
    text-transform: uppercase;
  }
  .summary,
  .reading {
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-panel-text-subtle);
    font-variant-numeric: tabular-nums;
  }
  .pair {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--z-space-3);
    min-width: 0;
  }
  .spark {
    margin: 0;
    padding: var(--z-space-2) var(--z-space-3);
    border: var(--z-border-width) solid var(--z-panel-border);
    border-radius: var(--z-radius-md);
    background: var(--z-panel-raised);
    min-width: 0;
  }
  figcaption {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: var(--z-space-2);
    font-size: var(--z-text-2xs);
    font-weight: var(--z-weight-semibold);
    letter-spacing: var(--z-tracking-wider);
    text-transform: uppercase;
  }
  figcaption b {
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
    letter-spacing: normal;
    text-transform: none;
    color: var(--z-panel-text);
    font-variant-numeric: tabular-nums;
  }
  figcaption small {
    font-size: var(--z-text-2xs);
    font-weight: var(--z-weight-medium);
    color: var(--z-panel-text-muted);
  }
  svg {
    display: block;
    width: 100%;
    height: auto;
    margin-top: var(--z-space-1);
  }
  .grid {
    stroke: var(--z-panel-border);
    stroke-width: 1;
  }
  .area {
    fill: var(--z-brand-runner-blue);
    fill-opacity: 0.18;
  }
  .line {
    fill: none;
    stroke: var(--z-brand-runner-blue);
    stroke-width: 1.6;
    stroke-linejoin: round;
    stroke-linecap: round;
  }
</style>
