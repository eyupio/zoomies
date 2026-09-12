<script lang="ts">
  import { fleet } from '$lib/state/fleet.svelte';
  import MetricGrid from '$lib/components/MetricGrid.svelte';
  import ChartPanel from '$lib/components/ChartPanel.svelte';
  import StateBreakdown from './StateBreakdown.svelte';
  import { formatNumber, formatDuration, describeWindow } from '$lib/format';
  let { others = false }: { others?: boolean } = $props();
  const stats = $derived(others ? fleet.stats : fleet.stats?.fleet);
  const suffix = $derived(others ? '&all=true' : '');
  const window = $derived(describeWindow(fleet.stats?.window) ?? 'reported window');
  const num = (n: number | undefined) => (n === undefined ? '—' : formatNumber(n));
  const segments = $derived([
    { label: 'Succeeded', value: stats?.succeeded ?? 0, tone: 'idle' as const },
    {
      label: 'Failed',
      value: stats?.failed ?? 0,
      tone: 'danger' as const,
      href: `/jobs?failed=true${suffix}`,
    },
    {
      label: 'Cancelled / skipped',
      value: stats?.cancelled ?? 0,
      tone: 'neutral' as const,
      href: `/jobs?conclusion=cancelled&conclusion=skipped${suffix}`,
    },
    { label: 'Unknown', value: stats?.unknown ?? 0, tone: 'pending' as const },
  ]);
</script>

{#if stats}
  <section aria-label="Job operational context" class="context">
    <p class="scope">
      {others ? 'All reported jobs' : 'Zoomies jobs'} · fleet-wide context; table filters apply below
    </p>
    <MetricGrid
      items={[
        {
          label: 'Queued now',
          value: num(stats.queued_jobs),
          detail: 'Waiting to start',
          tone: (stats.queued_jobs ?? 0) > 0 ? 'warning' : 'neutral',
          href: `/jobs?state=queued${suffix}`,
        },
        {
          label: 'Running now',
          value: num(stats.running_jobs),
          detail: 'Jobs in progress',
          tone: 'busy',
          href: `/jobs?state=in_progress${suffix}`,
        },
        { label: 'Completed', value: num(stats.completed), detail: `Last ${window}` },
        {
          label: 'Success rate',
          value: stats.completed
            ? `${((100 * (stats.succeeded ?? 0)) / stats.completed).toFixed(1)}%`
            : '—',
          detail: 'Successes / all completions',
          tone: 'success',
        },
        {
          label: 'Failed jobs',
          value: num(stats.failed),
          detail: `Last ${window}; includes runner faults`,
          tone: (stats.failed ?? 0) > 0 ? 'danger' : 'neutral',
          href: `/jobs?failed=true${suffix}`,
        },
        {
          label: 'P95 queue wait',
          value: stats.p95_wait_ms === undefined ? '—' : formatDuration(stats.p95_wait_ms),
          detail: `Last ${window}; observed starts`,
        },
      ]}
    />
    <ChartPanel
      title="Job outcomes"
      description={`Completions in the last ${window}. Unknown outcomes remain separate. Links open the corresponding job view across its full retained history.`}
      ><StateBreakdown {segments} label="Completed job outcomes" /></ChartPanel
    >
  </section>
{/if}

<style>
  .context {
    margin-bottom: var(--z-space-5);
  }
  .scope {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    margin: var(--z-space-3) 0;
  }
</style>
