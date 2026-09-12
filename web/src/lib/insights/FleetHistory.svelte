<script lang="ts">
  import { untrack } from 'svelte';
  import { listSamples } from '$lib/api/client';
  import { events } from '$lib/api/sse';
  import type { FleetSample, Stats } from '$lib/api/types';
  import { fleet } from '$lib/state/fleet.svelte';
  import ChartPanel from '$lib/components/ChartPanel.svelte';
  import Select from '$lib/components/Select.svelte';
  import SignalTrend from './SignalTrend.svelte';
  import { minuteSeries, mergeSamples } from './signals';
  let { others = false }: { others?: boolean } = $props();
  let samples = $state.raw<FleetSample[]>([]);
  let current = $state(Date.now());
  let metric = $state('queue');
  let failed = $state(false);
  let attempt = $state(0);
  const options = [
    { value: 'queue', label: 'Queued jobs' },
    { value: 'running', label: 'Running jobs' },
    { value: 'idle', label: 'Idle runners' },
    { value: 'live', label: 'Live runners' },
  ];
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
          total_runners: r
            ? (r.provisioning ?? 0) +
              (r.registering ?? 0) +
              (r.busy ?? 0) +
              (r.idle ?? 0) +
              (r.draining ?? 0)
            : undefined,
        },
      ],
      now,
    );
    current = now;
  }
  $effect(() => {
    void attempt;
    const controller = new AbortController();
    let disposed = false;
    untrack(() => {
      if (fleet.stats) record(fleet.stats);
    });
    void listSamples({ window: '1h' }, controller.signal)
      .then((page) => {
        if (disposed) return;
        samples = mergeSamples(page.items ?? [], samples, Date.now());
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
  const points = $derived(
    minuteSeries(
      samples.map((s) => ({
        at: new Date(s.at ?? '').getTime(),
        value:
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
                : s.total_runners) ?? null,
      })),
      current,
    ),
  );
</script>

<ChartPanel
  title="Fleet activity · last hour"
  description={`${others ? 'All GitHub-reported jobs' : 'Zoomies jobs'}; runner counts always belong to this fleet. Independent of table filters.`}
>
  {#snippet actions()}<Select
      ariaLabel="Fleet trend metric"
      value={metric}
      {options}
      size="sm"
      onchange={(v) => (metric = v)}
    />{/snippet}
  <SignalTrend
    {points}
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
