<script lang="ts">
  import { SvelteMap } from 'svelte/reactivity';
  import type { UsageRow, UsageGrouping } from '$lib/api/types';
  import MetricGrid from '$lib/components/MetricGrid.svelte';
  import ChartPanel from '$lib/components/ChartPanel.svelte';
  import Select from '$lib/components/Select.svelte';
  import { formatNumber } from '$lib/format';
  let {
    rows,
    grouping,
    label,
    onselect,
  }: {
    rows: UsageRow[];
    grouping: UsageGrouping;
    label: (key: string) => string;
    onselect: (key: string) => void;
  } = $props();
  type Bucket = NonNullable<UsageRow['history']>[number];
  let metric = $state('outcomes');
  let selected = $state<number | null>(null);
  const total = $derived(
    rows.reduce(
      (a, r) => ({
        jobs: a.jobs + r.jobs,
        completed: a.completed + r.jobs_completed,
        succeeded: a.succeeded + (r.succeeded ?? 0),
        failed: a.failed + (r.failed ?? 0),
        cancelled: a.cancelled + (r.cancelled ?? 0),
        unknown: a.unknown + (r.unknown ?? 0),
        seconds: a.seconds + r.job_execution_seconds,
        allocated: a.allocated + (r.allocated_runner_seconds ?? 0),
        starts: a.starts + r.jobs_started,
        wait: a.wait + (r.average_queue_wait_seconds ?? 0) * r.jobs_started,
      }),
      {
        jobs: 0,
        completed: 0,
        succeeded: 0,
        failed: 0,
        cancelled: 0,
        unknown: 0,
        seconds: 0,
        allocated: 0,
        starts: 0,
        wait: 0,
      },
    ),
  );
  const buckets = $derived.by(() => {
    const map = new SvelteMap<string, Bucket>();
    for (const row of rows)
      for (const b of row.history ?? []) {
        const a = map.get(b.from);
        if (!a) map.set(b.from, { ...b });
        else {
          a.queued += b.queued;
          a.started += b.started;
          a.succeeded += b.succeeded;
          a.failed += b.failed;
          a.cancelled += b.cancelled;
          a.unknown += b.unknown;
          a.execution_seconds += b.execution_seconds;
          a.allocated_seconds += b.allocated_seconds;
          a.capacity_samples += b.capacity_samples;
          a.capacity_reached += b.capacity_reached;
        }
      }
    return [...map.values()].sort((a, b) => a.from.localeCompare(b.from));
  });
  const metrics = $derived([
    {
      label: 'Jobs queued',
      value: formatNumber(total.jobs),
      detail: `${formatNumber(total.starts)} started in this window`,
    },
    {
      label: 'Success rate',
      value: total.completed ? `${((100 * total.succeeded) / total.completed).toFixed(1)}%` : '—',
      detail: `${formatNumber(total.succeeded)} of ${formatNumber(total.completed)} completed`,
      tone: 'success' as const,
    },
    {
      label: 'Failed jobs',
      value: formatNumber(total.failed),
      detail: `${formatNumber(total.cancelled)} cancelled / skipped · ${formatNumber(total.unknown)} unknown`,
      tone: total.failed ? ('danger' as const) : ('neutral' as const),
    },
    {
      label: 'Execution hours',
      value: (total.seconds / 3600).toFixed(2),
      detail: 'Running time inside this window',
    },
    {
      label: 'Average queue wait',
      value: total.starts ? `${(total.wait / total.starts).toFixed(1)}s` : '—',
      detail: 'Weighted across jobs that started',
    },
    {
      label: 'Runner busy share',
      value: total.allocated ? `${((100 * total.seconds) / total.allocated).toFixed(1)}%` : '—',
      detail:
        grouping === 'repository' || grouping === 'workflow'
          ? 'Allocation not attributable'
          : `${(total.allocated / 3600).toFixed(2)} allocated runner-hours`,
    },
  ]);
  const series = $derived(
    metric === 'execution'
      ? [
          {
            key: 'execution_seconds' as const,
            name: 'Executing (hours)',
            tone: 'var(--z-busy)',
            divisor: 3600,
          },
          {
            key: 'allocated_seconds' as const,
            name: 'Allocated (hours)',
            tone: 'var(--z-accent)',
            divisor: 3600,
          },
        ]
      : [
          { key: 'queued' as const, name: 'Queued', tone: 'var(--z-accent)', divisor: 1 },
          { key: 'succeeded' as const, name: 'Succeeded', tone: 'var(--z-idle)', divisor: 1 },
          { key: 'failed' as const, name: 'Failed', tone: 'var(--z-danger)', divisor: 1 },
          {
            key: 'cancelled' as const,
            name: 'Cancelled / skipped',
            tone: 'var(--z-neutral)',
            divisor: 1,
          },
          { key: 'unknown' as const, name: 'Unknown', tone: 'var(--z-pending)', divisor: 1 },
        ],
  );
  const visibleSeries = $derived(
    series.filter(
      (s) =>
        s.key !== 'allocated_seconds' || (grouping !== 'repository' && grouping !== 'workflow'),
    ),
  );
  const ceiling = $derived(
    Math.ceil(
      Math.max(2, ...buckets.flatMap((b) => visibleSeries.map((s) => b[s.key] / s.divisor))) / 2,
    ) * 2,
  );
  $effect(() => {
    void buckets;
    selected = null;
  });
  const active = $derived(buckets[selected ?? buckets.length - 1]);
  const ranked = $derived(
    [...rows].sort((a, b) => b.job_execution_seconds - a.job_execution_seconds).slice(0, 10),
  );
  const maxHours = $derived(Math.max(1, ...ranked.map((r) => r.job_execution_seconds)));
  const lanes = $derived([
    { key: 'queued' as const, name: 'Queued', tone: 'var(--z-accent)' },
    { key: 'succeeded' as const, name: 'Succeeded', tone: 'var(--z-idle)' },
    { key: 'failed' as const, name: 'Failed', tone: 'var(--z-danger)' },
    ...(grouping === 'pool'
      ? [{ key: 'capacity_reached' as const, name: 'Capacity reached', tone: 'var(--z-pending)' }]
      : []),
  ]);
  function date(value: string): string {
    return new Date(value).toLocaleString(undefined, {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  }
  function points(key: keyof Bucket, divisor: number): string {
    return buckets
      .map(
        (b, i) =>
          `${48 + (i * 720) / Math.max(1, buckets.length - 1)},${192 - (Number(b[key]) / divisor / ceiling) * 164}`,
      )
      .join(' ');
  }
  function describe(b: Bucket): string {
    return `${date(b.from)}: ${b.queued} queued, ${b.succeeded} succeeded, ${b.failed} failed, ${b.cancelled} cancelled or skipped, ${b.unknown} unknown; ${b.capacity_samples ? `${b.capacity_reached} capacity-blocked pool-minutes out of ${b.capacity_samples} observed` : 'no capacity observations'}`;
  }
</script>

<MetricGrid items={metrics} />
<div class="insights">
  <ChartPanel
    title="Usage over time"
    description="Follow demand, job outcomes and runner time across the selected window. Select a dot below to inspect its interval."
  >
    {#snippet actions()}<Select
        ariaLabel="Chart metric"
        value={metric}
        size="sm"
        options={[
          { value: 'outcomes', label: 'Job activity' },
          { value: 'execution', label: 'Runner time' },
        ]}
        onchange={(v) => (metric = v)}
      />{/snippet}
    {#if buckets.length}
      <div class="legend">
        {#each visibleSeries as s (s.key)}<span><i style:background={s.tone}></i>{s.name}</span
          >{/each}
      </div>
      <svg
        viewBox="0 0 800 225"
        role="img"
        aria-label={`${metric === 'execution' ? 'Runner hours' : 'Job activity'} trend across ${buckets.length} intervals`}
      >
        {#each [0, 0.5, 1] as tick (tick)}<line
            x1="48"
            x2="768"
            y1={192 - tick * 164}
            y2={192 - tick * 164}
            class="gridline"
          /><text x="40" y={196 - tick * 164} text-anchor="end"
            >{(ceiling * tick).toFixed(metric === 'execution' ? 1 : 0)}</text
          >{/each}
        {#each visibleSeries as s (s.key)}<polyline
            points={points(s.key, s.divisor)}
            fill="none"
            stroke={s.tone}
            stroke-width="2.5"
            vector-effect="non-scaling-stroke"
          />{/each}
        {#if selected !== null}<line
            x1={48 + (selected * 720) / Math.max(1, buckets.length - 1)}
            x2={48 + (selected * 720) / Math.max(1, buckets.length - 1)}
            y1="20"
            y2="192"
            class="cursor"
          />{/if}
        <text x="48" y="218">{date(buckets[0]?.from ?? '')}</text><text
          x="768"
          y="218"
          text-anchor="end">{date(buckets[buckets.length - 1]?.from ?? '')}</text
        >
      </svg>
      <div class="inspect" aria-live="polite">
        {#if active}<strong>{date(active.from)}</strong><span
            >{visibleSeries
              .map(
                (s) =>
                  `${s.name}: ${(active[s.key] / s.divisor).toLocaleString(undefined, { maximumFractionDigits: 2 })}`,
              )
              .join(' · ')}</span
          >{/if}
      </div>
    {:else}<p class="muted">No retained history in this window.</p>{/if}
  </ChartPanel>
  <ChartPanel
    title="Execution by group"
    description="The ten largest consumers of runner execution time. Select a group to focus the report."
  >
    <div class="ranking">
      {#each ranked as row (row.key)}<button
          class="rank"
          disabled={!row.key}
          onclick={() => onselect(row.key)}
          ><span class="rank-heading"
            ><span>{label(row.key)}</span><strong
              >{(row.job_execution_seconds / 3600).toFixed(2)} h</strong
            ></span
          ><span class="track"
            ><span style:width={`${(100 * row.job_execution_seconds) / maxHours}%`}></span></span
          ><small
            >{formatNumber(row.jobs_completed)} completed · {formatNumber(row.failed ?? 0)} failed</small
          ></button
        >{/each}
    </div>
  </ChartPanel>
</div>
<ChartPanel
  title="Activity matrix"
  description="Each dot represents one interval: hourly for windows up to 48 hours, otherwise 24 hours from the range start. Darker dots mean more activity. Tap or focus a dot for exact values."
>
  <div class="matrix-scroll">
    <div
      class="matrix"
      style:grid-template-columns={`max-content repeat(${buckets.length}, minmax(16px,1fr))`}
    >
      {#each lanes as lane (lane.key)}
        {@const maximum = Math.max(1, ...buckets.map((b) => b[lane.key]))}
        <span class="lane">{lane.name}</span>
        {#each buckets as b, i (b.from)}
          {@const unknown = lane.key === 'capacity_reached' && b.capacity_samples === 0}
          <button
            class="dot"
            class:unknown
            class:chosen={selected === i}
            style:--dot-color={lane.tone}
            style:--dot-opacity={b[lane.key] ? 0.3 + (0.7 * b[lane.key]) / maximum : 0}
            aria-label={`${lane.name}, ${describe(b)}`}
            title={describe(b)}
            onfocus={() => (selected = i)}
            onclick={() => (selected = i)}><span></span></button
          >
        {/each}
      {/each}
    </div>
  </div>
  <p class="muted">
    {#if grouping === 'pool'}Capacity reached counts observed pool-minutes with a host-capacity
      placement block, not incidents or exact duration. Outlined dots have no capacity observations.
      History starts after this feature is installed.{:else}Capacity observations belong to pools.
      Choose Pool to inspect recorded placement bottlenecks.{/if} All history reflects retained records;
    pruning can remove older activity.
  </p>
  {#if active}<p class="selection" aria-live="polite">{describe(active)}</p>{/if}
</ChartPanel>

<style>
  .insights {
    display: grid;
    grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
    gap: var(--z-space-4);
    margin-bottom: var(--z-space-4);
  }
  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-3);
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
  }
  .legend span {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
  }
  i {
    width: var(--z-space-2);
    height: var(--z-space-2);
    border-radius: var(--z-radius-full);
  }
  svg {
    width: 100%;
    height: auto;
    display: block;
    margin-top: var(--z-space-4);
    overflow: visible;
  }
  text {
    fill: var(--z-text-muted);
    font-size: 12px;
  }
  .gridline {
    stroke: var(--z-border);
    stroke-dasharray: 3 4;
  }
  .cursor {
    stroke: var(--z-text-muted);
    stroke-dasharray: 3 3;
  }
  .inspect {
    min-height: var(--z-space-12);
    display: flex;
    flex-direction: column;
    gap: var(--z-space-1);
    padding: var(--z-space-3);
    background: var(--z-surface-sunken);
    border-radius: var(--z-radius-sm);
    font-size: var(--z-text-xs);
    font-variant-numeric: tabular-nums;
  }
  .inspect span {
    color: var(--z-text-muted);
  }
  .ranking {
    display: grid;
    gap: var(--z-space-3);
    max-height: 340px;
    overflow-y: auto;
  }
  .rank {
    padding: var(--z-space-2);
    text-align: left;
    background: none;
    border: 0;
    border-radius: var(--z-radius-sm);
    color: var(--z-text);
    cursor: pointer;
  }
  .rank:hover {
    background: var(--z-surface-hover);
  }
  .rank-heading {
    display: flex;
    justify-content: space-between;
    gap: var(--z-space-3);
    font-size: var(--z-text-xs);
  }
  .rank-heading > span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  strong {
    font-weight: var(--z-weight-semibold);
  }
  .rank-heading strong {
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .track {
    display: block;
    height: var(--z-space-2);
    margin: var(--z-space-2) 0;
    background: var(--z-surface-sunken);
    border-radius: var(--z-radius-full);
    overflow: hidden;
  }
  .track > span {
    display: block;
    height: 100%;
    background: var(--z-accent);
    border-radius: inherit;
  }
  small,
  .muted {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .muted {
    margin: var(--z-space-4) 0 0;
  }
  .matrix-scroll {
    overflow-x: auto;
  }
  .matrix {
    display: grid;
    align-items: center;
    gap: var(--z-space-2);
    min-width: 100%;
    width: max-content;
  }
  .lane {
    position: sticky;
    left: 0;
    background: var(--z-surface);
    z-index: 1;
    font-size: var(--z-text-xs);
    padding-right: var(--z-space-3);
  }
  .dot {
    width: 20px;
    height: 24px;
    padding: var(--z-space-1);
    background: none;
    border: 0;
    cursor: pointer;
    border-radius: var(--z-radius-sm);
  }
  .dot span {
    display: block;
    width: 12px;
    height: 12px;
    border-radius: var(--z-radius-full);
    background: color-mix(
      in srgb,
      var(--dot-color) calc(var(--dot-opacity) * 100%),
      var(--z-surface-sunken)
    );
    border: var(--z-border-width) solid var(--z-border);
  }
  .dot.unknown span {
    background: none;
    border-style: dashed;
  }
  .chosen {
    outline: var(--z-border-width) solid var(--z-accent);
  }
  button:focus-visible {
    outline: 2px solid var(--z-accent);
    outline-offset: 2px;
  }
  .selection {
    margin: var(--z-space-3) 0 0;
    font-size: var(--z-text-xs);
  }
  @media (max-width: 1024px) {
    .insights {
      grid-template-columns: 1fr;
    }
    .ranking {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      max-height: none;
    }
  }
  @media (max-width: 768px) {
    .ranking {
      grid-template-columns: 1fr;
    }
  }
</style>
