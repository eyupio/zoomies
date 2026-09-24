<!--
  How well the fleet served one installation over a month: the counts and the
  exact queue, start and cleanup timings of docs/metrics.md's per-installation
  report.

  It has its own window rather than the page's date range because the route is
  relative to now and the question is "the last month", not "these dates"; and
  it names no repository, so it is safe to show to whoever may read usage.

  A figure the record no longer reaches is said to be unavailable in words,
  above the figures, rather than shown as a zero: a month whose first week was
  pruned looks exactly like a quiet week otherwise.
-->
<script lang="ts">
  import { getInstallationReport } from '$lib/api/client';
  import type { InstallationReport, ReportPercentiles } from '$lib/api/types';
  import { formatDuration, formatNumber } from '$lib/format';
  import { INSTALLATION_REPORT_URL } from '$lib/links';
  import LoadingBoundary from '$lib/components/LoadingBoundary.svelte';
  import type { ComponentProps } from 'svelte';
  import MetricGrid from '$lib/components/MetricGrid.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import Select from '$lib/components/Select.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';

  type Metric = ComponentProps<typeof MetricGrid>['items'][number];

  let { installation }: { installation: string } = $props();

  const WINDOWS = [
    { value: '168h', label: 'Last 7 days' },
    { value: '720h', label: 'Last 30 days' },
    { value: '2160h', label: 'Last 90 days' },
  ];
  let span = $state('720h');

  let report = $state.raw<InstallationReport | null>(null);
  let loading = $state(true);
  let error = $state<unknown>(null);

  $effect(() => {
    const controller = new AbortController();
    const id = installation;
    const w = span;
    loading = true;
    error = null;
    getInstallationReport(id, { window: w }, controller.signal)
      .then((next) => {
        if (!controller.signal.aborted) report = next;
      })
      .catch((cause) => {
        if (!controller.signal.aborted) error = cause;
      })
      .finally(() => {
        if (!controller.signal.aborted) loading = false;
      });
    return () => controller.abort();
  });

  const counts = $derived.by((): Metric[] => {
    const c = report?.counts;
    if (!c) return [];
    return [
      {
        label: 'Observed',
        value: formatNumber(c.observed),
        detail: 'Jobs first seen in the window',
      },
      {
        label: 'Eligible',
        value: formatNumber(c.eligible),
        detail: 'A pool claimed them and no review held them',
      },
      {
        label: 'Created for',
        value: formatNumber(c.created_for),
        detail: 'Ran on a runner started after they became eligible',
      },
      {
        label: 'Ran here',
        value: formatNumber(c.ran_here),
        detail: 'Ran on any runner this fleet created',
      },
      {
        label: 'Ran elsewhere',
        value: formatNumber(c.ran_elsewhere),
        detail: 'GitHub gave them to a runner this fleet did not create',
      },
      {
        label: 'Fleet fault',
        value: formatNumber(c.fleet_fault),
        detail: 'The runner, not the workflow, is why they went wrong',
        tone: c.fleet_fault > 0 ? 'danger' : 'neutral',
      },
      {
        label: 'Cleanup pending',
        value: formatNumber(c.cleanup_pending),
        detail: 'Finished runners the host or GitHub has not confirmed gone',
        tone: c.cleanup_pending > 0 ? 'warning' : 'neutral',
      },
      {
        label: 'Cleanup converged',
        value: formatNumber(c.cleanup_converged),
        detail: 'Both confirmations seen',
      },
    ];
  });

  const timings = $derived.by((): { label: string; p: ReportPercentiles }[] => {
    const t = report?.timings;
    if (!t) return [];
    return [
      { label: 'Eligible to first create task', p: t.scheduling },
      { label: 'Create to registered', p: t.registration },
      { label: 'Cleanup convergence', p: t.cleanup },
    ];
  });

  function seconds(value: number | null): string {
    return value === null ? 'No samples' : formatDuration(value * 1000);
  }
</script>

<Panel
  title="Installation report"
  description="How the fleet served this installation: its jobs, the runners it started for them, and their cleanup."
  class="installation-report"
>
  {#snippet actions()}
    <Select
      value={span}
      size="sm"
      ariaLabel="Report window"
      options={WINDOWS}
      onchange={(v) => (span = v)}
    />
    <a href={INSTALLATION_REPORT_URL} target="_blank" rel="noopener noreferrer"
      >How each figure is defined</a
    >
  {/snippet}

  <LoadingBoundary {loading} {error} empty={false}>
    {#snippet skeleton()}
      <Skeleton lines={4} />
    {/snippet}

    {#if report}
      {#each report.unavailable as line (line)}
        <p class="gap" data-testid="report-unavailable">{line}</p>
      {/each}
      <MetricGrid items={counts} />
      <!-- svelte-ignore a11y_no_redundant_roles -->
      <table role="table">
        <caption class="sr-only">Timings for this installation, exact percentiles</caption>
        <thead>
          <tr>
            <th scope="col">Interval</th>
            <th scope="col" class="end">p50</th>
            <th scope="col" class="end">p95</th>
            <th scope="col" class="end">Samples</th>
          </tr>
        </thead>
        <tbody>
          {#each timings as row (row.label)}
            <tr>
              <th scope="row">{row.label}</th>
              <td class="end tabular">{seconds(row.p.p50_seconds)}</td>
              <td class="end tabular">{seconds(row.p.p95_seconds)}</td>
              <td class="end tabular">{formatNumber(row.p.samples)}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </LoadingBoundary>
</Panel>

<style>
  .gap {
    max-width: 70ch;
    margin: 0 0 var(--z-space-3);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text);
  }
  table {
    width: 100%;
    table-layout: fixed;
    border-collapse: collapse;
    font-size: var(--z-text-sm);
  }
  th,
  td {
    padding: var(--z-space-2) var(--z-space-3);
    text-align: left;
    border-bottom: var(--z-border-width) solid var(--z-border);
    overflow-wrap: anywhere;
  }
  thead th {
    font-size: var(--z-text-2xs);
    font-weight: var(--z-weight-medium);
    text-transform: uppercase;
    letter-spacing: var(--z-tracking-wide);
    color: var(--z-text-muted);
  }
  tbody th {
    font-weight: var(--z-weight-medium);
  }
  tbody tr:last-child th,
  tbody tr:last-child td {
    border-bottom: 0;
  }
  .end {
    text-align: right;
  }
  .tabular {
    font-variant-numeric: tabular-nums;
  }
</style>
