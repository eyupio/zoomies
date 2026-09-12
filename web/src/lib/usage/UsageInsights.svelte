<script lang="ts">
  import { getUsage } from '$lib/api/client';
  import type { UsageRow, UsageGrouping } from '$lib/api/types';
  import MetricGrid from '$lib/components/MetricGrid.svelte';
  import ChartPanel from '$lib/components/ChartPanel.svelte';
  import Select from '$lib/components/Select.svelte';
  import ActivityMatrix, { type ActivityRange } from '$lib/insights/ActivityMatrix.svelte';
  import {
    addDays,
    fillWindow,
    hasCapacity,
    intervalWidth,
    localDate,
    mergeHistories,
    MODES,
    startOfLocalDay,
    type ActivityBucket,
    type ActivityMode,
    type Interval,
  } from '$lib/insights/activity';
  import { formatNumber, toMillis } from '$lib/format';
  let {
    rows,
    grouping,
    range,
    entity = '',
    fetchedAt = 0,
    label,
    onselect,
  }: {
    rows: UsageRow[];
    grouping: UsageGrouping;
    /** The report's own bounds, as the request carried them. */
    range: { from: string; to: string };
    /** The group the report is focused on, or empty for all of them. */
    entity?: string;
    /** When the report landed, so the matrix knows its hours are stale. */
    fetchedAt?: number;
    label: (key: string) => string;
    onselect: (key: string) => void;
  } = $props();
  type Bucket = ActivityBucket;
  let metric = $state('outcomes');
  let selected = $state<number | null>(null);
  let mode = $state<ActivityMode>('outcomes');
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
  /**
   * The API cuts hourly buckets for a window of two days or less and daily
   * ones beyond, anchored at the start the request named; the same rule here
   * is what lets the matrix know whether a square is an hour or a day.
   */
  const interval = $derived<Interval>(
    (toMillis(range.to) ?? 0) - (toMillis(range.from) ?? 0) <= 48 * intervalWidth('hour')
      ? 'hour'
      : 'day',
  );
  const first = $derived(startOfLocalDay(new Date(toMillis(range.from) ?? Date.now())));
  const buckets = $derived.by(() => {
    const from = new Date(toMillis(range.from) ?? 0);
    const span = (toMillis(range.to) ?? 0) - from.getTime();
    const count = Math.max(1, Math.ceil(span / intervalWidth(interval)));
    return fillWindow(mergeHistories(rows), from, count, interval);
  });
  const modes = $derived(MODES.filter((m) => m.value !== 'capacity' || hasCapacity(buckets)));

  /** A day's hours for the detail, in the report's own grouping and focus. */
  async function hourly(day: Date): Promise<Bucket[]> {
    const to = addDays(day, 1);
    const report = await getUsage({
      from: day.toISOString(),
      to: to.toISOString(),
      group_by: grouping,
      key: entity || undefined,
    });
    const count = Math.round((to.getTime() - day.getTime()) / intervalWidth('hour'));
    return fillWindow(mergeHistories(report.items ?? []), day, count, 'hour');
  }

  /**
   * The Jobs page cut to the square's day and, where the grouping is one the
   * Jobs page can filter by, to the focused group. An hour cannot be asked
   * for there, so an hour's link is its day's.
   */
  function links(at: ActivityRange) {
    const day = localDate(at.from);
    const params = [`since=${day}`, `until=${day}`];
    const filter = { pool: 'pool_id', repository: 'repo', workflow: 'workflow' }[
      grouping as string
    ];
    if (entity && filter) params.push(`${filter}=${encodeURIComponent(entity)}`);
    const out = [{ href: `/jobs?${params.join('&')}`, label: 'Jobs that day' }];
    if (at.bucket.failed > 0) {
      out.push({ href: `/jobs?failed=true&${params.join('&')}`, label: 'Failed jobs' });
    }
    return out;
  }
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
      // A share only while the runner records are all there: runners are
      // kept for less time than jobs, so a range that reaches past runner
      // retention has its jobs' hours and only some of its runners', and the
      // ratio is then a number that means nothing rather than a percentage.
      value:
        total.allocated && total.allocated >= total.seconds
          ? `${((100 * total.seconds) / total.allocated).toFixed(1)}%`
          : '—',
      detail:
        grouping === 'repository' || grouping === 'workflow'
          ? 'Allocation not attributable'
          : total.allocated && total.allocated < total.seconds
            ? 'Runner records for part of this range are no longer retained'
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
</script>

<MetricGrid items={metrics} />
<div class="insights">
  <ChartPanel
    title="Usage over time"
    description="Follow demand, job outcomes and runner time across the selected window. Select a square in the matrix below to inspect its interval here."
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
  description={interval === 'day'
    ? 'Each square is one day of the range, laid out as a calendar. Greener as more jobs finish, red when any fail; hover a square for its figures and select it for its hours and its jobs.'
    : 'Each square is one hour of the range. Greener as more jobs finish, red when any fail; hover a square for its figures and select it for its jobs.'}
>
  {#snippet actions()}<Select
      ariaLabel="Colour the matrix by"
      value={mode}
      size="sm"
      options={modes}
      onchange={(v) => (mode = v as ActivityMode)}
    />{/snippet}
  <ActivityMatrix
    {buckets}
    {interval}
    {first}
    {mode}
    weeks={buckets.length}
    bind:selected
    {fetchedAt}
    hourly={interval === 'day' ? hourly : undefined}
    {links}
    subject={entity ? label(entity) : `every ${grouping}`}
  />
  <p class="muted">
    {#if grouping === 'pool'}Capacity reached counts observed pool-minutes with a host-capacity
      placement block, not incidents or exact duration. Dashed squares have no capacity
      observations. History starts after this feature is installed.{:else}Capacity observations
      belong to pools. Choose Pool to inspect recorded placement bottlenecks.{/if} All history reflects
    retained records; pruning can remove older activity.
  </p>
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
