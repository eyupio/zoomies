<!--
  What the fleet has been doing lately, one figure at a time: the queue, the
  running jobs, the idle runners or the live ones, over the last hour, six
  hours or day.

  The controller writes one sample a minute and the stream keeps the series
  moving between fetches; a longer window folds those minutes into intervals
  that carry their peak, because a queue that hit twelve for two minutes is
  what an operator looking at a day is looking for. History reloads on a
  reconnect, for the same reason everything else does: the replay buffer is
  finite.
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
  import Select from '$lib/components/Select.svelte';
  import SignalTrend from './SignalTrend.svelte';
  import { foldMinutes, minuteSeries, mergeSamples } from './signals';
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

  let samples = $state.raw<FleetSample[]>([]);
  let current = $state(Date.now());
  let metric = $state('queue');
  let windowKey = $state<WindowKey>('1h');
  let failed = $state(false);
  let attempt = $state(0);
  const chosen = $derived(WINDOWS.find((w) => w.value === windowKey) ?? WINDOWS[0]);
  const options = [
    { value: 'queue', label: 'Queued jobs' },
    { value: 'running', label: 'Running jobs' },
    { value: 'idle', label: 'Idle runners' },
    { value: 'live', label: 'Live runners' },
  ];
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
      if (fleet.stats) record(fleet.stats);
    });
    void listSamples({ window: window.value }, controller.signal)
      .then((page) => {
        if (disposed) return;
        samples = mergeSamples(page.items ?? [], samples, Date.now(), window.minutes);
        failed = false;
      })
      .catch(() => {
        if (!disposed) failed = true;
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
  function value(s: FleetSample): number | null {
    return (
      (metric === 'queue'
        ? others
          ? s.queued_jobs
          : s.fleet_queued_jobs
        : metric === 'running'
          ? others
            ? s.running_jobs
            : s.fleet_running_jobs
          : metric === 'idle'
            ? s.idle_runners
            : s.total_runners) ?? null
    );
  }
  const minutes = $derived(
    minuteSeries(
      samples.map((s) => ({ at: new Date(s.at ?? '').getTime(), value: value(s) })),
      current,
      chosen.minutes,
    ),
  );
  const points = $derived(foldMinutes(minutes, chosen.step));
  const byMinute = $derived(
    new Map(samples.map((s) => [Math.floor(new Date(s.at ?? '').getTime() / 60_000), s])),
  );

  /**
   * The other figures at a moment, for the card. A folded point carries its
   * peak, so the minute shown is the one the peak came from; the rest of the
   * fleet at that minute is what explains it.
   */
  function detail(at: number): Array<[string, string]> {
    const start = Math.floor(at / 60_000);
    let best: FleetSample | undefined;
    for (let m = start; m < start + chosen.step; m++) {
      const s = byMinute.get(m);
      if (s && (!best || (value(s) ?? -1) > (value(best) ?? -1))) best = s;
    }
    if (!best) return [];
    const num = (n: number | undefined) => (n === undefined ? '--' : formatNumber(n));
    return [
      ['Queued jobs', num(others ? best.queued_jobs : best.fleet_queued_jobs)],
      ['Running jobs', num(others ? best.running_jobs : best.fleet_running_jobs)],
      ['Idle runners', num(best.idle_runners)],
      ['Busy runners', num(best.busy_runners)],
      ['Live runners', num(best.total_runners)],
    ];
  }
</script>

<ChartPanel
  title="Fleet activity"
  description={`${others ? 'All GitHub-reported jobs' : 'Zoomies jobs'}; runner counts always belong to this fleet. ${chosen.name}. Independent of table filters.`}
>
  {#snippet actions()}
    <div class="controls">
      <Select
        ariaLabel="Fleet trend metric"
        value={metric}
        {options}
        size="sm"
        onchange={(v) => (metric = v)}
      />
      <Segmented
        label="Window"
        value={windowKey}
        options={WINDOWS}
        onchange={(v) => (windowKey = v as WindowKey)}
      />
    </div>
  {/snippet}
  <SignalTrend
    {points}
    step={chosen.step}
    {detail}
    label={options.find((o) => o.value === metric)?.label ?? 'Activity'}
    tone={metric === 'queue' ? 'pending' : metric === 'idle' ? 'idle' : 'busy'}
  />
  {#if failed}<p class="retry">
      Earlier samples could not be loaded. Live observations are still shown. <button
        onclick={() => (attempt += 1)}>Retry history</button
      >
    </p>{/if}
</ChartPanel>

<style>
  /* Side by side: a select is as wide as its container, and on its own in a
     wrapping header it would take the whole line and push the windows under
     it. */
  .controls {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-2);
  }
  .retry {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  button {
    color: var(--z-accent);
    background: none;
    border: 0;
    text-decoration: underline;
    cursor: pointer;
  }
</style>
