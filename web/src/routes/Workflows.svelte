<!--
  Workflows: what GitHub's Actions tab lists, in this fleet's terms.

  One row per workflow run -- the "#1009" beside a workflow's name on GitHub --
  with the jobs GitHub reported under it summed up, and each row opens in
  place to those jobs and how every one of them is getting on. It is the page
  an operator arrives at from a pull request: they know the run, and the
  question is what this fleet did with it. The Jobs page is the same history
  one step down, every job on a row of its own, for the questions that name a
  job -- which pool claimed it, why it waited -- and every link that names one
  still lands there.

  The filters and the status views are the Jobs page's own, read at the run's
  level: a status names the run's, and anything else keeps a run whenever any
  job of it matches. So the two pages share one address bar, and a link
  carrying `?failed=true` means the same on either.

  The page shows the runs this fleet has a hand in by default, for the reason
  the Jobs page does: GitHub reports every job in an installed repository,
  most of which ran somewhere else, and "Include other runners" widens it.
-->
<script lang="ts">
  import { getJobFacets, listWorkflowRuns } from '$lib/api/client';
  import type { Job, WorkflowRun } from '$lib/api/types';
  import { events } from '$lib/api/sse';
  import { formatDuration, formatNumber } from '$lib/format';
  import { fleet } from '$lib/state/fleet.svelte';
  import { HOSTED, jobStatus, RUNNER_LOST, UNMATCHED } from '$lib/status';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import DataGrid from '$lib/components/DataGrid.svelte';
  import type { GridColumn, GridPage, GridQuery } from '$lib/components/DataGrid.svelte';
  import Duration from '$lib/components/Duration.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import ActivityStatus from '$lib/jobs/ActivityStatus.svelte';
  import { workflowActivity } from '$lib/jobs/activity-status';
  import { endOfDay, startOfDay } from '$lib/jobs/DateRange.svelte';
  import { jobFilterState } from '$lib/jobs/filter-state.svelte';
  import GitHubLink from '$lib/jobs/GitHubLink.svelte';
  import JobDrawer from '$lib/jobs/JobDrawer.svelte';
  import JobFilters from '$lib/jobs/JobFilters.svelte';
  import JobViewFilter from '$lib/jobs/JobViewFilter.svelte';
  import LevelSwitch from '$lib/workflows/LevelSwitch.svelte';
  import RunJobs from '$lib/workflows/RunJobs.svelte';

  /* -- filter state, held in the URL and shared with the Jobs page -------- */

  const filterState = jobFilterState();
  const filters = $derived(filterState.filters);
  const view = $derived(filterState.view);
  const inHand = $derived(filterState.inHand);
  const { patch, clearFilters, setView } = filterState;

  /* -- facets and live rows -------------------------------------------------- */

  let facets = $state<{ repos?: string[]; workflows?: string[]; conclusions?: string[] }>({});
  let liveKey = $state(0);

  async function loadFacets(signal: AbortSignal): Promise<void> {
    try {
      facets = await getJobFacets(signal);
    } catch {
      // The menus fall back to what is on the page; the grid itself will
      // have reported the outage.
    }
  }

  $effect(() => {
    const controller = new AbortController();
    void loadFacets(controller.signal);
    return () => controller.abort();
  });

  // A job changing state anywhere may move its run, so the page refetches,
  // debounced by the grid. The job open in the drawer is replaced outright,
  // as on the Jobs page: the frame is its GET shape.
  $effect(() =>
    events.subscribe('job.updated', (job) => {
      liveKey += 1;
      if (selected && job.id === selected.id) selected = job;
    }),
  );

  async function refreshPage(): Promise<void> {
    liveKey += 1;
    await loadFacets(new AbortController().signal);
  }

  /** Labels worth offering in the filter: what the pools answer to. */
  const labelOptions = $derived.by(() => {
    const seen: Record<string, true> = {};
    for (const pool of fleet.pools) for (const label of pool.labels ?? []) seen[label] = true;
    return Object.keys(seen).sort((a, b) => a.localeCompare(b));
  });

  /* -- the empty state ------------------------------------------------------ */

  const emptyTitle = $derived(
    filters.unmatched
      ? 'No run is stuck'
      : filters.faulted
        ? 'This fleet has broken nothing'
        : filters.failed
          ? 'No failed runs'
          : view === 'running'
            ? 'Nothing is running right now'
            : view === 'queued'
              ? 'Nothing is queued'
              : view === 'finished'
                ? 'Nothing has finished yet'
                : filters.all
                  ? 'No workflow runs recorded yet'
                  : 'No runs have touched this fleet',
  );

  const emptyDescription = $derived(
    filters.unmatched
      ? 'No run has a job queued with labels no pool claims, which is how it should be.'
      : filters.faulted
        ? "No runner here stopped under a job or failed to start one within these filters. Any failures in this period are the workflows' own. Widen the dates to look further back."
        : filters.failed
          ? 'No run has a job GitHub reported as failed or timed out, and no runner here has stopped under one. Widen the dates to look further back.'
          : view === 'running'
            ? 'No run has a job on a runner here at this moment, which on a quiet fleet is the ordinary state. Queued shows what is waiting for one, and All shows every run this fleet has been asked for.'
            : view === 'queued'
              ? 'No run is waiting for a runner, so the fleet is keeping up with what GitHub is asking of it. Running shows what is being worked on now.'
              : view === 'finished'
                ? 'No run has ended within these filters. Running and Queued show the work still in hand, and All shows every status at once.'
                : filters.all
                  ? 'Zoomies records a run the first time GitHub tells it about one of its jobs, over a webhook delivery. If workflows are running and nothing appears here, the delivery is not arriving.'
                  : 'This view shows runs with a job a pool claims or a runner here ran. Include other runners to see everything GitHub has reported, hosted runners included.',
  );

  /* -- the grid ---------------------------------------------------------------- */

  async function fetchRuns(query: GridQuery, signal: AbortSignal): Promise<GridPage<WorkflowRun>> {
    const page = await listWorkflowRuns(
      {
        q: filters.q || undefined,
        repo: filters.repo,
        workflow: filters.workflow,
        pool_id: filters.pool_id,
        label: filters.label,
        conclusion: filters.conclusion,
        state: filters.state,
        provisioning: filters.provisioning,
        cancelling: inHand ? false : undefined,
        since: startOfDay(filters.since),
        until: endOfDay(filters.until),
        unmatched: filters.unmatched ? true : undefined,
        failed: filters.failed ? true : undefined,
        faulted: filters.faulted ? true : undefined,
        managed: filters.all ? undefined : true,
        limit: query.limit,
        offset: query.offset,
        sort: query.sort,
        order: query.order,
      },
      signal,
    );
    return { items: page.items ?? [], total: page.total };
  }

  let selected = $state<Job | null>(null);
  let drawerOpen = $state(false);

  function open(job: Job): void {
    selected = job;
    drawerOpen = true;
  }

  // A run has no ID of its own: the repository and GitHub's run ID together
  // are what a run is, since run IDs key a run only within its repository.
  function rowId(run: WorkflowRun): string {
    return `${run.repo ?? ''}#${run.github_run_id ?? 0}`;
  }

  /**
   * How the run's jobs are getting on, in one phrase: what is still to do
   * while the run is in hand, and how they ended once it is not. Read down a
   * column, "3 succeeded · 1 failed" says which run to open.
   */
  function jobsSummary(run: WorkflowRun): string {
    const jobs = run.jobs;
    if (!jobs) return '';
    const parts: string[] = [];
    if (jobs.in_progress) parts.push(`${jobs.in_progress} running`);
    if (jobs.queued) parts.push(`${jobs.queued} queued`);
    if (jobs.waiting) parts.push(`${jobs.waiting} waiting`);
    if (jobs.succeeded) parts.push(`${jobs.succeeded} succeeded`);
    if (jobs.failed) parts.push(`${jobs.failed} failed`);
    if (jobs.cancelled) parts.push(`${jobs.cancelled} cancelled`);
    if (jobs.skipped) parts.push(`${jobs.skipped} skipped`);
    return parts.join(' · ');
  }

  // The sortable ids are the columns the store understands: queued_at,
  // started_at, completed_at, repo, workflow, run_number, state, jobs,
  // duration and queue_wait.
  const columns = $derived<GridColumn<WorkflowRun>[]>([
    {
      id: 'state',
      header: 'State',
      sortable: true,
      width: '12rem',
      hideable: false,
      value: (run) => jobStatus(run.state, run.conclusion).label,
      cell: stateCell,
    },
    {
      id: 'run_number',
      header: 'Run',
      sortable: true,
      width: '7rem',
      hideable: false,
      value: (run) => (run.run_number ? `#${run.run_number}` : String(run.github_run_id ?? '')),
      cell: runCell,
    },
    { id: 'repo', header: 'Repository', sortable: true, value: (run) => run.repo ?? '' },
    { id: 'workflow', header: 'Workflow', sortable: true, value: (run) => run.workflow ?? '' },
    {
      id: 'branch',
      header: 'Branch',
      priority: 'wide',
      value: (run) => run.head_branch ?? '',
      cell: branchCell,
    },
    {
      id: 'jobs',
      header: 'Jobs',
      sortable: true,
      width: '14rem',
      value: (run) => `${formatNumber(run.jobs?.total ?? 0)}: ${jobsSummary(run)}`,
      cell: jobsCell,
    },
    {
      id: 'queue_wait',
      header: 'Queue wait',
      sortable: true,
      align: 'end',
      width: '8rem',
      value: (run) => formatDuration(run.queue_wait_ms),
      cell: queueWaitCell,
    },
    {
      id: 'duration',
      header: 'Duration',
      sortable: true,
      align: 'end',
      width: '8rem',
      value: (run) => formatDuration(run.duration_ms),
      cell: durationCell,
    },
    {
      id: 'queued_at',
      header: 'Queued',
      sortable: true,
      width: '9rem',
      value: (run) => run.queued_at ?? '',
      cell: queuedCell,
    },
  ]);
</script>

{#snippet stateCell(run: WorkflowRun)}
  <span class="state">
    <ActivityStatus
      activity={workflowActivity(run)}
      seed={`${run.repo}/${run.github_run_id}/${run.run_attempt ?? 1}`}
      pack
    />
    {#if run.jobs?.faulted}
      <Badge status={RUNNER_LOST} size="sm" title={RUNNER_LOST.hint} />
    {/if}
    {#if run.jobs?.unmatched}
      <Badge status={UNMATCHED} size="sm" title={UNMATCHED.hint} />
    {:else if run.hosted && !run.managed}
      <Badge status={HOSTED} size="sm" title={HOSTED.hint} />
    {/if}
  </span>
{/snippet}

{#snippet runCell(run: WorkflowRun)}
  <span class="run">
    <GitHubLink
      href={run.html_url}
      runNumber={run.run_number || undefined}
      label="Open run {run.run_number || run.github_run_id} of {run.workflow ||
        'this workflow'} on GitHub, in a new tab"
      onclick={(event) => event.stopPropagation()}
    />
    {#if (run.run_attempt ?? 1) > 1}
      <span class="attempt" title="Re-run: this is attempt {run.run_attempt}">
        attempt {run.run_attempt}
      </span>
    {/if}
  </span>
{/snippet}

{#snippet branchCell(run: WorkflowRun)}
  {#if run.head_branch}
    <span class="mono">{run.head_branch}</span>
  {:else}
    <span class="none">--</span>
  {/if}
{/snippet}

{#snippet jobsCell(run: WorkflowRun)}
  <span class="jobs">
    <strong>{formatNumber(run.jobs?.total ?? 0)}</strong>
    <span class="summary">{jobsSummary(run)}</span>
  </span>
{/snippet}

{#snippet queueWaitCell(run: WorkflowRun)}
  {#if run.state === 'queued' && !run.started_at}
    <span class="waiting"><Duration from={run.queued_at} live /> so far</span>
  {:else}
    {formatDuration(run.queue_wait_ms)}
  {/if}
{/snippet}

{#snippet durationCell(run: WorkflowRun)}
  {#if run.state === 'in_progress' && run.started_at}
    <span class="waiting"><Duration from={run.started_at} live /> so far</span>
  {:else if run.duration_ms}
    {formatDuration(run.duration_ms)}
  {:else}
    <span class="none">--</span>
  {/if}
{/snippet}

{#snippet queuedCell(run: WorkflowRun)}
  <RelativeTime value={run.queued_at} />
{/snippet}

{#snippet runJobs(run: WorkflowRun)}
  <RunJobs {run} onopen={open} />
{/snippet}

<PageHeader
  title="Workflows"
  subtitle={filters.all
    ? 'Every workflow run GitHub has told this controller about, whatever ran it, and the jobs inside each.'
    : 'The workflow runs this fleet claims, runs or is waiting to run, and the jobs inside each.'}
  onrefresh={refreshPage}
/>

<div class="content">
  <div class="toolbar">
    <JobViewFilter value={view} onchange={setView} />
    <LevelSwitch level="runs" />
  </div>

  <JobFilters
    statusChips={view === ''}
    value={filters}
    {facets}
    pools={fleet.pools}
    {labelOptions}
    onchange={patch}
    onclear={clearFilters}
  />

  <DataGrid
    gridId="workflows"
    label="Workflow runs"
    {columns}
    fetcher={fetchRuns}
    {rowId}
    {filters}
    defaultSort="queued_at"
    defaultOrder="desc"
    noun="runs"
    {liveKey}
    expanded={runJobs}
    expandHeader="Jobs"
    {emptyTitle}
    {emptyDescription}
  >
    {#snippet emptyAction()}
      {#if view !== '' && view !== 'all'}
        <Button variant="secondary" onclick={() => setView('all')}>Show every status</Button>
      {:else if !filters.unmatched}
        {#if filters.all}
          <Button variant="secondary" href="/installations">Check webhook delivery</Button>
        {:else}
          <Button variant="secondary" onclick={() => patch({ all: true })}>
            Include other runners
          </Button>
        {/if}
      {/if}
    {/snippet}
  </DataGrid>

  <p class="footnote">
    Open a run to see its jobs and how each is getting on, or switch to Every job for the same
    history one job to a row. Sort by queue wait to find work that waited, or by duration to find
    work that took its time.
  </p>
</div>

<JobDrawer bind:open={drawerOpen} job={selected} onclose={() => (selected = null)} />

<style>
  .content {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
  }
  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-3);
  }
  .state,
  .run {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    white-space: nowrap;
  }
  .state {
    flex-wrap: wrap;
    min-width: 0;
    max-width: 100%;
    white-space: normal;
  }
  .attempt {
    color: var(--z-text-subtle);
    font-size: var(--z-text-xs);
  }
  .jobs {
    display: inline-flex;
    align-items: baseline;
    gap: var(--z-space-2);
    min-width: 0;
  }
  .summary {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
  }
  .none {
    color: var(--z-text-subtle);
  }
  .waiting {
    color: var(--z-pending);
    white-space: nowrap;
  }
  .footnote {
    margin: 0;
    color: var(--z-text-subtle);
    font-size: var(--z-text-xs);
  }
</style>
