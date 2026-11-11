<!--
  The last hour on the command centre: running jobs and queue depth, one point
  a minute, on one scale so the two can be read against each other and against
  the slots the fleet has.

  Running is the busy colour and a solid line, queued the pending colour and a
  dashed one, so the two are told apart without colour. The samples are the
  controller's own minute samples; each stats frame becomes the newest point,
  replacing one from the same minute, so the line's end is what the ring says.
  A reconnect reloads, because the stream's replay buffer is finite and a gap
  is otherwise drawn as a straight line nobody measured.
-->
<script lang="ts">
  import { listSamples } from '$lib/api/client';
  import type { FleetSample, Stats } from '$lib/api/types';
  import { trendPoints, withNow, type TrendPoint } from '$lib/bands/trend';
  import Button from '$lib/components/Button.svelte';
  import Tooltip from '$lib/components/Tooltip.svelte';
  import { fleet } from '$lib/state/fleet.svelte';
  import { areaPath, linePath } from './chart';
  import { HISTORY_HREF } from './links';
  import { hourTip } from './tips';

  interface Props {
    /** Slots the fleet has, so a full fleet's line reaches the top. */
    slots: number;
  }
  let { slots }: Props = $props();

  const MINUTES = 60;
  // The drawing's own units. The SVG stretches to its box, and the strokes are
  // kept to their width with non-scaling-stroke.
  const WIDTH = 600;
  const HEIGHT = 120;

  let samples = $state.raw<FleetSample[]>([]);
  let latest = $state.raw<TrendPoint | null>(null);
  let failed = $state(false);
  let pending: AbortController | null = null;

  function pointOf(stats: Stats): TrendPoint {
    const own = stats.fleet ?? stats;
    return { at: Date.now(), queued: own.queued_jobs ?? 0, running: own.running_jobs ?? 0 };
  }

  /**
   * Fetch the hour again. The page's refresh calls it as well as a reconnect,
   * and is told whether the samples arrived, since a refresh in which nothing
   * answered has to say so. A read overtaken by a newer one has not failed.
   */
  export async function reload(): Promise<boolean> {
    pending?.abort();
    const controller = new AbortController();
    pending = controller;
    try {
      const page = await listSamples({ window: '1h' }, controller.signal);
      if (pending !== controller) return true;
      samples = page.items ?? [];
      failed = false;
      return true;
    } catch {
      if (controller.signal.aborted || pending !== controller) return true;
      failed = true;
      return false;
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

  // Every stats frame lands in the fleet cache, so reading it here is the one
  // path; a subscription of its own as well would take each frame twice.
  $effect(() => {
    if (fleet.stats) latest = pointOf(fleet.stats);
  });

  const points = $derived.by(() => {
    const at = latest?.at ?? Date.now();
    const base = trendPoints(samples, at, MINUTES);
    return latest ? withNow(base, latest, MINUTES) : base;
  });
  const running = $derived(points.map((p) => p.running));
  const queued = $derived(points.map((p) => p.queued));
  const max = $derived(Math.max(1, slots, ...running, ...queued));
  const now = $derived(points[points.length - 1] ?? null);
  const label = $derived(
    now
      ? `The last hour: ${now.running} running and ${now.queued} waiting now. Open the history.`
      : 'The last hour: no samples yet. Open the history.',
  );
</script>

<div class="hour">
  <Tooltip bare detail={hourTip(points, "The Overview's history")}>
    <a class="chart" href={HISTORY_HREF} data-testid="command-hour-chart">
      <span class="head" aria-hidden="true">
        <span class="title">Last hour</span>
        <span class="key running">Running <b>{now?.running ?? 0}</b></span>
        <span class="key queued">Waiting <b>{now?.queued ?? 0}</b></span>
      </span>
      <svg viewBox="0 0 {WIDTH} {HEIGHT}" preserveAspectRatio="none" role="img" aria-label={label}>
        <line class="grid" x1="0" x2={WIDTH} y1={HEIGHT / 2} y2={HEIGHT / 2} />
        <path class="area" d={areaPath(running, WIDTH, HEIGHT, max)} />
        <path class="line running" d={linePath(running, WIDTH, HEIGHT, max)} />
        <path class="line queued" d={linePath(queued, WIDTH, HEIGHT, max)} />
      </svg>
    </a>
  </Tooltip>
  {#if failed}
    <p class="failed" role="status">
      The last hour's samples did not load.
      <Button variant="secondary" size="sm" onclick={() => void reload()}>Try again</Button>
    </p>
  {/if}
</div>

<style>
  .hour {
    display: grid;
    gap: var(--z-space-2);
    min-width: 0;
  }
  .chart {
    display: grid;
    gap: var(--z-space-2);
    min-width: 0;
    padding: var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-lg);
    background: var(--z-surface);
    color: var(--z-text-muted);
    text-decoration: none;
    transition: border-color var(--z-motion-fast) var(--z-ease);
  }
  .chart:hover,
  .chart:focus-visible {
    border-color: var(--z-accent);
  }
  .chart:focus-visible {
    outline: none;
    box-shadow: var(--z-focus-ring);
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--z-space-1) var(--z-space-4);
    font-size: var(--z-text-xs);
  }
  .title {
    flex: 1;
    font-weight: var(--z-weight-medium);
    color: var(--z-text);
  }
  .key {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
  }
  /* The key's swatch is the line itself, solid or dashed, as it is drawn. */
  .key::before {
    content: '';
    width: var(--z-space-4);
    border-top: var(--z-border-width-thick) solid var(--z-busy);
  }
  .key.queued::before {
    border-top-style: dashed;
    border-top-color: var(--z-pending);
  }
  .key b {
    color: var(--z-text);
    font-weight: var(--z-weight-semibold);
    font-variant-numeric: tabular-nums;
  }
  svg {
    display: block;
    width: 100%;
    height: var(--z-space-16);
  }
  .grid {
    stroke: var(--z-border);
    stroke-width: 1;
    vector-effect: non-scaling-stroke;
  }
  .area {
    fill: var(--z-busy-subtle);
  }
  .line {
    fill: none;
    stroke-width: 2;
    stroke-linejoin: round;
    stroke-linecap: round;
    vector-effect: non-scaling-stroke;
  }
  .line.running {
    stroke: var(--z-busy);
  }
  .line.queued {
    stroke: var(--z-pending);
    stroke-dasharray: 6 4;
  }
  .failed {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-2);
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
</style>
