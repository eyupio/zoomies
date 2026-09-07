<!--
  What the fleet did, and roughly what it cost, over a bounded range of days.

  This is the one page whose numbers somebody puts in front of a finance team,
  so two things matter more than they do elsewhere. The first is that every
  figure says what it counts: "jobs" alone is ambiguous between queued, started
  and finished, and the three differ at the edges of any interval, so all three
  are columns. The second is that an estimate never reads as a fact -- cost
  comes from rates an administrator typed, Zoomies knows no cloud prices, and
  the page says so above the table rather than in a footnote.

  The range and the grouping live in the address bar, so a report can be sent to
  somebody else as a link rather than as a description of which boxes to tick.
-->
<script lang="ts">
  import { Download, Receipt } from '@lucide/svelte';
  import { getUsage, usageCsvUrl } from '$lib/api/client';
  import type { Usage, UsageGrouping } from '$lib/api/types';
  import { router } from '$lib/router';
  import { fleet } from '$lib/state/fleet.svelte';
  import { formatNumber, formatPercent } from '$lib/format';
  import Button from '$lib/components/Button.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import LoadingBoundary from '$lib/components/LoadingBoundary.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Select from '$lib/components/Select.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import DateRange, { endOfDay, startOfDay } from '$lib/jobs/DateRange.svelte';

  /** The groupings the API offers, in the order an operator narrows through them. */
  const GROUPINGS: { value: UsageGrouping; label: string }[] = [
    { value: 'pool', label: 'Pool' },
    { value: 'installation', label: 'Installation' },
    { value: 'repository', label: 'Repository' },
    { value: 'workflow', label: 'Workflow' },
  ];

  /** What the column of keys is called, once a grouping is chosen. */
  const KEY_HEADER: Record<UsageGrouping, string> = {
    pool: 'Pool',
    installation: 'Installation',
    repository: 'Repository',
    workflow: 'Workflow',
  };

  /** A calendar date, in the operator's own time zone rather than UTC's. */
  function day(at: Date): string {
    const local = new Date(at.getTime() - at.getTimezoneOffset() * 60_000);
    return local.toISOString().slice(0, 10);
  }

  const DEFAULT_DAYS = 30;
  const today = day(new Date());
  const monthAgo = day(new Date(Date.now() - DEFAULT_DAYS * 86_400_000));

  const since = $derived(router.param('since') || monthAgo);
  const until = $derived(router.param('until') || today);
  const grouping = $derived<UsageGrouping>(
    (GROUPINGS.find((g) => g.value === router.param('group_by'))?.value ?? 'pool') as UsageGrouping,
  );

  /**
   * The query both the table and the CSV link are built from, so the file an
   * operator downloads is the report they are looking at.
   */
  const query = $derived({
    from: startOfDay(since) ?? '',
    to: endOfDay(until) ?? '',
    group_by: grouping,
  });

  let report = $state.raw<Usage | null>(null);
  let loading = $state(true);
  let error = $state<unknown>(null);

  async function load(q: typeof query, signal?: AbortSignal): Promise<void> {
    loading = true;
    try {
      report = await getUsage(q, signal);
      error = null;
    } catch (cause) {
      if (signal?.aborted) return;
      error = cause;
    } finally {
      if (!signal?.aborted) loading = false;
    }
  }

  $effect(() => {
    const q = query;
    const controller = new AbortController();
    void load(q, controller.signal);
    return () => controller.abort();
  });

  const rows = $derived(report?.items ?? []);
  /**
   * Whether runner time can be attributed at this grouping at all. A runner
   * idles on behalf of a pool and never on behalf of a repository, so those
   * columns are absent rather than zero -- a zero would be a claim.
   */
  const attributable = $derived(report?.allocation_attributable !== false);
  const backwards = $derived(since > until);

  function setRange(next: { since: string; until: string }): void {
    router.setQuery({ since: next.since || null, until: next.until || null });
  }

  /** Hours, to two places: the unit an invoice is written in. */
  function hours(seconds: number | null): string {
    if (seconds === null) return 'Not attributable';
    return (seconds / 3600).toFixed(2);
  }

  /** A mean wait in the units a person reads it in. */
  function wait(seconds: number | null): string {
    if (seconds === null) return 'Nothing started';
    if (seconds < 90) return `${seconds.toFixed(1)}s`;
    return `${(seconds / 60).toFixed(1)} min`;
  }

  /** An estimate, marked as one by the note above the table, not by a symbol. */
  function cost(value: number | undefined): string {
    if (value === undefined) return 'No rate set';
    return value.toFixed(2);
  }

  /**
   * How much of a runner's paid-for life was spent executing a job. The number
   * that answers "are we paying for idle runners?", which is the question this
   * page is usually opened to settle.
   */
  function utilisation(executed: number, allocated: number | null): string {
    if (allocated === null || allocated <= 0) return '--';
    return formatPercent(executed / allocated);
  }

  /**
   * The row's key in the words the rest of the product uses.
   *
   * The API groups by identifier, which is right for a stable report and wrong
   * for a person: a table of `pool_demoarm` is not something anybody takes to a
   * finance meeting. Repositories and workflows are already names.
   *
   * Three cases have to look different, and used to look the same. A live pool
   * is its name. A pool the fleet has no record of -- deleted since the range
   * began -- keeps its id, because its hours were still spent, but it is said
   * to be gone rather than shown as a bare identifier that reads like a name.
   * And a row with no pool at all is not a pool: it is the work this fleet had
   * no hand in, which on a one-pool fleet otherwise looks like a second pool.
   */
  function label(key: string): string {
    if (!key) return 'Unattributed';
    if (grouping !== 'pool') return key;
    return fleet.pool(key)?.name ?? key;
  }

  /**
   * What this row is, when the grouping is by pool.
   *
   * `unknown` while the fleet cache is still loading: every pool would
   * otherwise be reported as deleted for the moment it takes to arrive, which
   * is worse than saying nothing.
   */
  function kind(key: string): 'live' | 'gone' | 'unattributed' | 'unknown' {
    if (grouping !== 'pool') return 'live';
    if (!key) return 'unattributed';
    if (fleet.pool(key)) return 'live';
    return fleet.loaded ? 'gone' : 'unknown';
  }
</script>

<PageHeader
  title="Usage"
  subtitle="This fleet's runner capacity and job activity over a range of days. Jobs GitHub ran on its own hosted runners are not counted: they used no runner here."
>
  <Button
    variant="secondary"
    size="sm"
    icon={Download}
    href={usageCsvUrl(query)}
    disabled={backwards}
  >
    Export CSV
  </Button>
</PageHeader>

<div class="controls">
  <DateRange {since} {until} label="Usage between" onchange={setRange} />
  <label class="grouping">
    <span>Group by</span>
    <Select
      value={grouping}
      size="sm"
      ariaLabel="Group by"
      options={GROUPINGS}
      onchange={(value) => router.setQuery({ group_by: value === 'pool' ? null : value })}
    />
  </label>
</div>

<p class="note">
  Counts are additive, so adjacent reports sum: a job is counted in the interval it was queued,
  started or completed in. Costs are estimates from rates an administrator assigned to a pool.
  Zoomies does not know what your machines cost.
</p>

{#if !attributable}
  <p class="note attribution">
    A runner idles on behalf of a pool, never on behalf of a {grouping}, so runner-hours and cost
    cannot be attributed at this grouping. Group by pool or installation to see them.
  </p>
{/if}

<LoadingBoundary
  {loading}
  {error}
  empty={rows.length === 0}
  onretry={() => void load(query)}
  class="report"
>
  {#snippet skeleton()}
    <div class="frame">
      <Skeleton lines={6} />
    </div>
  {/snippet}

  {#snippet emptyState()}
    <EmptyState
      icon={Receipt}
      title="Nothing ran in this range"
      description="No job was queued, started or finished between these dates. Widen the range, or change what the report is grouped by."
    />
  {/snippet}

  <div class="frame">
    <table>
      <caption class="sr-only">
        Usage by {grouping} between {since} and {until}
      </caption>
      <thead>
        <tr>
          <th scope="col" class="key-header">{KEY_HEADER[grouping]}</th>
          <th scope="col" class="end">Queued</th>
          <th scope="col" class="end">Started</th>
          <th scope="col" class="end">Completed</th>
          <th scope="col" class="end">Peak at once</th>
          <th scope="col" class="end">Average queue wait</th>
          <th scope="col" class="end">Executing</th>
          {#if attributable}
            <th scope="col" class="end">Runner-hours</th>
            <th scope="col" class="end">Busy share</th>
            <th scope="col" class="end">Estimated cost</th>
          {/if}
        </tr>
      </thead>
      <tbody>
        {#each rows as row (row.key)}
          {@const rowKind = kind(row.key)}
          <tr>
            <th scope="row" class="key" title={row.key || undefined}>
              <span class="name" class:muted={rowKind !== 'live'}>{label(row.key)}</span>
              {#if rowKind === 'gone'}
                <span class="tag">deleted</span>
              {:else if rowKind === 'unattributed'}
                <span class="tag">no pool</span>
              {/if}
            </th>
            <td class="end tabular" data-label="Queued">{formatNumber(row.jobs)}</td>
            <td class="end tabular" data-label="Started">{formatNumber(row.jobs_started)}</td>
            <td class="end tabular" data-label="Completed">{formatNumber(row.jobs_completed)}</td>
            <td class="end tabular" data-label="Peak at once"
              >{formatNumber(row.peak_concurrency)}</td
            >
            <td class="end tabular" data-label="Average queue wait"
              >{wait(row.average_queue_wait_seconds)}</td
            >
            <td class="end tabular" data-label="Executing">{hours(row.job_execution_seconds)}</td>
            {#if attributable}
              <td class="end tabular" data-label="Runner-hours"
                >{hours(row.allocated_runner_seconds)}</td
              >
              <td class="end tabular" data-label="Busy share"
                >{utilisation(row.job_execution_seconds, row.allocated_runner_seconds)}</td
              >
              <td class="end tabular" data-label="Estimated cost">{cost(row.estimated_cost)}</td>
            {/if}
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
</LoadingBoundary>

<style>
  .controls {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-3) var(--z-space-5);
    margin: var(--z-space-4) 0;
  }
  .grouping {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
  }
  .grouping span {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    white-space: nowrap;
  }
  .note {
    max-width: 70ch;
    margin: 0 0 var(--z-space-3);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .attribution {
    color: var(--z-text);
  }
  /*
    Not `.table`: Tailwind's utility of that name sets `display: table`, which
    made this frame size itself to the columns inside it instead of scrolling
    them. On a phone the whole document then grew to the table's width, the
    fixed navigation bar grew with it, and taps on the bar landed on the footer
    underneath.
  */
  .frame {
    overflow-x: auto;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--z-text-sm);
  }
  th,
  td {
    padding: var(--z-space-3) var(--z-space-4);
    text-align: left;
    white-space: nowrap;
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  tbody tr:last-child th,
  tbody tr:last-child td {
    border-bottom: 0;
  }
  thead th {
    font-size: var(--z-text-2xs);
    font-weight: var(--z-weight-medium);
    text-transform: uppercase;
    letter-spacing: var(--z-tracking-wide);
    color: var(--z-text-muted);
    background: var(--z-surface-sunken);
  }
  .end {
    text-align: right;
  }
  .key {
    font-weight: var(--z-weight-medium);
    color: var(--z-text);
    max-width: 24rem;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .name {
    /* The tag sits beside the name rather than under it, so a narrow column
       truncates the name and never the word that says what the row is. */
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .name.muted {
    color: var(--z-text-muted);
    font-weight: var(--z-weight-regular);
  }
  .tag {
    margin-left: var(--z-space-2);
    padding: 0 var(--z-space-2);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    font-size: var(--z-text-2xs);
    font-weight: var(--z-weight-medium);
    text-transform: uppercase;
    letter-spacing: var(--z-tracking-wide);
    color: var(--z-text-muted);
    white-space: nowrap;
  }

  /*
    On a phone this is ten columns in 412 pixels, and no amount of scrolling
    makes that a table: the heading is cut off mid-word, every pool is
    "zoomies-demo-lin..." and indistinguishable from the next, and the figure
    you scrolled to belongs to a row you can no longer name. Pinning the first
    column fixed only the last of those.

    So on a phone each row becomes a card, which is what the Hosts page does
    with the same problem, and each figure carries its own heading. Nothing is
    dropped and nothing is truncated; the report simply reads down instead of
    across.
  */
  @media (max-width: 768px) {
    .frame {
      border: 0;
      background: none;
      overflow-x: visible;
    }
    table {
      display: block;
    }
    /* The column headings move onto the cells, so the row of them is noise. */
    thead {
      display: none;
    }
    tbody {
      display: flex;
      flex-direction: column;
      gap: var(--z-space-3);
    }
    /* Already blockified by the flex tbody above, so it takes padding and a
       border without needing a display of its own. */
    tbody tr {
      padding: var(--z-space-3) var(--z-space-4);
      border: var(--z-border-width) solid var(--z-border);
      border-radius: var(--z-radius-md);
      background: var(--z-surface);
    }
    tbody th.key {
      display: block;
      max-width: none;
      padding: 0 0 var(--z-space-2);
      border-bottom: var(--z-border-width) solid var(--z-border);
      /* The name is the whole point of the row, so it wraps rather than
         truncating -- there is a whole card's width for it now. */
      overflow: visible;
      white-space: normal;
      overflow-wrap: anywhere;
    }
    tbody td {
      display: flex;
      align-items: baseline;
      justify-content: space-between;
      gap: var(--z-space-4);
      padding: var(--z-space-2) 0;
      border: 0;
      text-align: right;
      white-space: normal;
    }
    tbody td::before {
      content: attr(data-label);
      flex: none;
      font-size: var(--z-text-2xs);
      font-weight: var(--z-weight-medium);
      text-transform: uppercase;
      letter-spacing: var(--z-tracking-wide);
      color: var(--z-text-muted);
      text-align: left;
    }
    .name {
      overflow: visible;
      white-space: normal;
    }
  }
  .tabular {
    font-variant-numeric: tabular-nums;
  }
</style>
