<!--
  Jobs: what has run, what is running, and what is waiting.

  The page exists to answer two questions. "Why is this slow?" is answered by
  sorting on queue wait or duration, which is why those two columns are sorted
  by the server rather than by the browser -- the interesting job is rarely on
  the page you are looking at. "Why has this not started?" is answered by the
  unmatched filter, which finds the queued jobs no enabled pool claims. A job
  that already ran is never counted there: its labels may name a hosted or vendor
  runner Zoomies does not own, and something ran it. Neither is a job
  whose labels all name such a runner, however long it queues -- a vendor is
  about to start it, and this fleet was never in the running.

  This is the Workflows page one step down: every job on a row of its own,
  where Workflows has the run each belongs to. The two share their filters and
  their views, so a link into either means the same on the other; what differs
  is the question. A run is what an operator arrives with from a pull request,
  and a job is what a pool claimed, a runner ran and a queue wait belongs to --
  which is why every link that names a job still lands here.

  GitHub reports every job in an installed repository, most of which this fleet
  never touches. The page therefore shows Zoomies' own work by default -- jobs a
  pool claims, jobs its runners ran, and queued jobs nothing here claims that
  something here could have -- and the "Include other runners" switch widens it
  to everything GitHub has reported.

  Status is the filter this page is opened for, so it is a row of buttons above
  the grid rather than a menu to open, and a page nobody has asked anything of
  answers "what is running?" before being asked. That default holds for a bare
  visit only: a link that already carries a filter -- the problems panel's, the
  Overview's, a colleague's -- said what it wanted, and narrowing it to running
  would answer a different question.

  The note explaining unmatched jobs belongs to the filtered view, not to the
  default one. An organisation that also rents runners elsewhere keeps queueing
  jobs this fleet has no pool for, and the ones whose labels leave any doubt are
  counted here because nothing here ran them -- so a note above the default grid
  is a standing red warning about somebody else's work, which is the one thing
  this page must not do. Discovery is the problems panel's job: `jobs.unmatched`
  waits out a grace period first, and its link opens this page with the filter
  already on.
-->
<script lang="ts">
  import PageBand from '$lib/bands/PageBand.svelte';
  import { revealHash } from '$lib/bands/jump';
  import { jobsLine, windowPhrase } from '$lib/bands/lines';
  import Tooltip from '$lib/components/Tooltip.svelte';
  import { Check } from '@lucide/svelte';
  import { readableJobText } from '$lib/jobs/name';
  import { visualStyle } from '$lib/state/ui-style.svelte';
  import { viewport } from '$lib/state/viewport.svelte';
  import JobsInsights from '$lib/insights/JobsInsights.svelte';
  import FleetHistory from '$lib/insights/FleetHistory.svelte';
  import {
    acknowledgeJob,
    analyseQueueWait,
    analyseRecurringFailures,
    getJobFacets,
    listJobs,
    reopenJobAttention,
  } from '$lib/api/client';
  import {
    PROVISIONING_STATUSES,
    type Job,
    type QueueWaitFinding,
    type RecurringFailuresFinding,
  } from '$lib/api/types';
  import AnalysisCard from '$lib/insights/AnalysisCard.svelte';
  import { router } from '$lib/router';
  import {
    analysisWindow,
    refusalOf,
    type AnalysisNarrowing,
    CAUSE_WORDS,
    FAILURE_CLASS_WORDS,
    jobWords,
    longestWaits,
    queueWaitScope,
    recurringFailuresScope,
    whyNoWindow,
    wordFor,
  } from '$lib/insights/analysis';
  import { events } from '$lib/api/sse';
  import { session } from '$lib/state/session.svelte';
  import { toasts } from '$lib/state/toasts.svelte';
  import { fleetFailed } from '$lib/faults';
  import {
    NO_VALUE,
    describeWindow,
    formatDuration,
    formatNumber,
    formatPercent,
    pluralise,
  } from '$lib/format';
  import { fleet } from '$lib/state/fleet.svelte';
  import {
    HOSTED,
    jobStatus,
    queueStatus,
    RUNNER_LOST,
    stuckUnmatched,
    UNMATCHED,
  } from '$lib/status';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import DataGrid from '$lib/components/DataGrid.svelte';
  import type { GridColumn, GridPage, GridQuery } from '$lib/components/DataGrid.svelte';
  import Duration from '$lib/components/Duration.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import RowActions from '$lib/components/RowActions.svelte';
  import type { RowAction } from '$lib/components/RowActions.svelte';
  import StateCell from '$lib/components/StateCell.svelte';
  import { endOfMoment, localMoment, startOfMoment } from '$lib/jobs/DateRange.svelte';
  import FleetPunchCard from '$lib/overview/FleetPunchCard.svelte';
  import GitHubLink from '$lib/jobs/GitHubLink.svelte';
  import JobDrawer from '$lib/jobs/JobDrawer.svelte';
  import JobFilters from '$lib/jobs/JobFilters.svelte';
  import JobLabels from '$lib/jobs/JobLabels.svelte';
  import JobViewFilter from '$lib/jobs/JobViewFilter.svelte';
  import SizeAdvice from '$lib/jobs/SizeAdvice.svelte';
  import { classCell } from '$lib/jobs/size';
  import { failureSummary } from '$lib/jobs/why';
  import UnmatchedNote from '$lib/jobs/UnmatchedNote.svelte';
  import { jobFilterState } from '$lib/jobs/filter-state.svelte';
  import LevelSwitch from '$lib/workflows/LevelSwitch.svelte';

  /* -- filter state, held in the URL ---------------------------------------
   * Shared with the Workflows page, which reads the same keys at the run's
   * level, so a link into either means the same on the other. What each key
   * means, and why the default is what it is, is with the code in
   * lib/jobs/filter-state.svelte.ts.
   * --------------------------------------------------------------------- */

  const operational = $derived(visualStyle.choice === 'mission-control');
  const compact = $derived(operational && viewport.phone);
  let contextExpanded = $state(false);
  // The rows are what the page is for, on every screen: the tiles, outcomes and activity
  // history are one fold under them.
  const layoutSections = ['records', 'context'];
  const filterState = jobFilterState();
  const filters = $derived(filterState.filters);
  const view = $derived(filterState.view);
  const inHand = $derived(filterState.inHand);
  const narrowed = $derived(filterState.narrowed);
  const { patch, clearFilters, setView } = filterState;

  /* -- facets and live rows -------------------------------------------------- */

  let facets = $state<{ repos?: string[]; workflows?: string[]; conclusions?: string[] }>({});
  let pageRows = $state<Job[]>([]);
  let liveKey = $state(0);

  async function loadFacets(signal: AbortSignal): Promise<void> {
    try {
      facets = await getJobFacets(signal);
    } catch {
      // The menus fall back to what is on the page. A failed facet request is
      // not worth a toast: the grid itself will have reported the outage.
    }
  }

  $effect(() => {
    const controller = new AbortController();
    void loadFacets(controller.signal);
    return () => controller.abort();
  });

  // A job changing state anywhere refetches the current page, debounced by the
  // grid, so in the ordinary case there is never anything to press.
  // The job open in the drawer is replaced outright: the frame is the job's
  // GET shape, so the drawer moves from "running" to "failed at step 3" the
  // moment GitHub says so, without the operator closing and reopening it.
  $effect(() =>
    events.subscribe('job.updated', (job) => {
      liveKey += 1;
      if (selected && job.id === selected.id) selected = job;
    }),
  );

  /**
   * Refreshing fetches both halves: the grid follows `liveKey`, and the facet
   * menus are re-read here so a repository that has just run its first job
   * appears in the filter rather than only in the rows.
   */
  async function refreshPage(): Promise<void> {
    liveKey += 1;
    adviceKey += 1;
    await loadFacets(new AbortController().signal);
  }

  /** Bumped by the refresh button so the size-label report reads again too. */
  let adviceKey = $state(0);

  /* Why the queued jobs waited ------------------------------------------------
   * The Queued view is where "why is this waiting?" is asked of the whole
   * queue, so the queue-wait analysis sits there and nowhere else on the page.
   * It reads the page's own dates, or the day before now when none are set,
   * and the one pool the filter names; filtered to several pools it reads the
   * fleet, since the analysis narrows to one pool or none.
   * ------------------------------------------------------------------------ */

  let wait = $state.raw<QueueWaitFinding | null>(null);
  let waitError = $state<{ message: string; refused: boolean } | null>(null);
  let waitLoading = $state(false);
  // Queued work, whether or not a view button says so: an address that keeps
  // the list's state at queued while narrowing it (with unmatched, say) is
  // still the queue somebody is asking about, and the card then says it does
  // not apply the narrowing (below). The UI itself never writes such an
  // address: the "Unmatched only" switch clears the state.
  //
  // Never for a public view's visitor: the route refuses one before it reads
  // anything, and a card that can only say "sign in to do this" is one more
  // thing a visitor learns to distrust (lib/shell/sections.ts).
  const queuedShown = $derived(
    !session.visitor &&
      (view === 'queued' || (filters.state.length === 1 && filters.state[0] === 'queued')),
  );
  const waitPool = $derived(filters.pool_id.length === 1 ? filters.pool_id[0] : undefined);
  // Primitives, so a change to a filter the analysis does not read (a
  // repository, a label) does not ask it again.
  const waitSince = $derived(filters.since);
  const waitUntil = $derived(filters.until);

  $effect(() => {
    if (!queuedShown) return;
    void adviceKey;
    const since = startOfMoment(waitSince);
    const until = endOfMoment(waitUntil);
    const period = analysisWindow(since, until, Date.now());
    const controller = new AbortController();
    // The last window's answer goes at once: left standing while the next
    // loads, it would sit under the new dates, as HostDetail's does not.
    wait = null;
    waitError = null;
    if (!period) {
      // The date picker points out a range typed backwards, and nothing points
      // out one that has not started; the card says why it asked nothing,
      // since a titled frame with nothing under it says nothing.
      waitError = { message: whyNoWindow(since, until), refused: true };
      waitLoading = false;
      return;
    }
    waitLoading = true;
    analyseQueueWait({ ...period, pool_id: waitPool }, controller.signal)
      .then((next) => {
        if (!controller.signal.aborted) wait = next;
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        // The grid below is still true; only this card says what went wrong.
        waitError = refusalOf(cause);
      })
      .finally(() => {
        if (!controller.signal.aborted) waitLoading = false;
      });
    return () => controller.abort();
  });

  // What the card covers, and which of the list's filters it leaves out:
  // the route reads one pool or none, the list beside it much more.
  const waitScope = $derived(
    queueWaitScope(
      {
        ...filters,
        // Only a provisioning status the address chose. The Queued view's own
        // default is every waiting job, and the card's period already says it
        // reads more than what waits now.
        provisioning: router
          .paramList('provisioning')
          .some((v) => (PROVISIONING_STATUSES as readonly string[]).includes(v))
          ? filters.provisioning
          : [],
      },
      (poolId) => fleet.pool(poolId)?.name ?? poolId,
    ),
  );

  // The pool the card was asked about, for Ask Eli to pass to its tool: the
  // finding for one pool never names it unless that pool waited most.
  const waitNarrowed = $derived<AnalysisNarrowing>(
    waitPool ? { pool: { id: waitPool, name: fleet.pool(waitPool)?.name } } : {},
  );

  /* Which jobs keep failing ---------------------------------------------------
   * The Failed view is where "why do these keep failing?" is asked, so the
   * recurring-failures analysis sits there. It reads the page's own dates, or
   * the day before now, and nothing else the list is narrowed by.
   * ------------------------------------------------------------------------ */

  let failing = $state.raw<RecurringFailuresFinding | null>(null);
  let failingError = $state<{ message: string; refused: boolean } | null>(null);
  let failingLoading = $state(false);
  // Never for a public view's visitor, as the queue card is not.
  const failingShown = $derived(!session.visitor && filters.failed);

  $effect(() => {
    if (!failingShown) return;
    void adviceKey;
    const since = startOfMoment(waitSince);
    const until = endOfMoment(waitUntil);
    const period = analysisWindow(since, until, Date.now());
    const controller = new AbortController();
    failing = null;
    failingError = null;
    if (!period) {
      failingError = { message: whyNoWindow(since, until), refused: true };
      failingLoading = false;
      return;
    }
    failingLoading = true;
    analyseRecurringFailures(period, controller.signal)
      .then((next) => {
        if (!controller.signal.aborted) failing = next;
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        failingError = refusalOf(cause);
      })
      .finally(() => {
        if (!controller.signal.aborted) failingLoading = false;
      });
    return () => controller.abort();
  });

  const failingScope = $derived(recurringFailuresScope({ ...filters, provisioning: [] }));

  /** Every pool's longest waits, longest first, as one list. */
  const longest = $derived(longestWaits(wait?.evidence.pools ?? []));

  /** Labels worth offering in the filter: what the pools answer to, plus what this page asked for. */
  const labelOptions = $derived.by(() => {
    const seen: Record<string, true> = {};
    for (const pool of fleet.pools) for (const label of pool.labels ?? []) seen[label] = true;
    for (const job of pageRows) for (const label of job.labels ?? []) seen[label] = true;
    return Object.keys(seen).sort((a, b) => a.localeCompare(b));
  });

  const unmatchedOnPage = $derived(pageRows.filter(stuckUnmatched).length);

  /* -- the empty state ------------------------------------------------------
   * An empty grid means something different in each of the three views, and
   * saying "no jobs recorded yet" to somebody whose jobs all ran on hosted
   * runners would be a lie the page can easily avoid telling.
   * ---------------------------------------------------------------------- */

  const emptyTitle = $derived(
    narrowed
      ? 'No jobs match these filters'
      : filters.unmatched
        ? 'No unmatched jobs'
        : filters.attention
          ? 'No failure is waiting on you'
          : filters.faulted
            ? 'This fleet has broken nothing'
            : filters.failed
              ? 'No failed jobs'
              : view === 'running'
                ? 'Nothing is running right now'
                : view === 'queued'
                  ? 'Nothing is queued'
                  : view === 'finished'
                    ? 'Nothing has finished yet'
                    : filters.all
                      ? 'No jobs recorded yet'
                      : 'No jobs have run on this fleet',
  );

  const emptyDescription = $derived(
    narrowed
      ? 'Nothing fits the search and filters above. Clear filters returns to what is running now, and All shows every status.'
      : filters.unmatched
        ? 'Nothing is queued with labels no pool claims, which is how it should be. Jobs that already ran are not counted here however their labels read.'
        : filters.attention
          ? 'Nothing failed in the last week that nobody has acknowledged and no later attempt has replaced. The Failed view keeps every failure in history, and a re-run that passes settles one.'
          : filters.faulted
            ? "No runner here stopped under a job or failed to start one within these filters. Any failures in this period are the workflows' own. Widen the dates to look further back."
            : filters.failed
              ? 'Nothing GitHub reported as failed or timed out, and no runner here has stopped under a job. Widen the dates to look further back.'
              : view === 'running'
                ? 'No runner here is working on a job at this moment, which on a quiet fleet is the ordinary state. Queued shows what is waiting for one, and All shows everything this fleet has been asked to do.'
                : view === 'queued'
                  ? 'Nothing is waiting for a runner, so the fleet is keeping up with what GitHub is asking of it. Running shows what is being worked on now, and the Queue holds anything removed from the queue rather than run.'
                  : view === 'finished'
                    ? 'Nothing has ended within these filters. Running and Queued show the work still in hand, and All shows every status at once.'
                    : filters.all
                      ? 'Zoomies records a job the first time GitHub tells it about one, over a webhook delivery. If workflows are running and nothing appears here, the delivery is not arriving.'
                      : 'This view shows jobs a pool claims or a runner here ran. Include other runners to see everything GitHub has reported, hosted runners included.',
  );

  /* -- the grid ---------------------------------------------------------------- */

  async function fetchJobs(query: GridQuery, signal: AbortSignal): Promise<GridPage<Job>> {
    const page = await listJobs(
      {
        q: filters.q || undefined,
        repo: filters.repo,
        workflow: filters.workflow,
        pool_id: filters.pool_id,
        host_id: filters.host_id,
        label: filters.label,
        conclusion: filters.conclusion,
        state: filters.state,
        provisioning: filters.provisioning,
        cancelling: inHand ? false : undefined,
        since: startOfMoment(filters.since),
        until: endOfMoment(filters.until),
        unmatched: filters.unmatched ? true : undefined,
        hosted: filters.hosted === 'false' ? false : undefined,
        failed: filters.failed ? true : undefined,
        faulted: filters.faulted ? true : undefined,
        attention: filters.attention ? true : undefined,
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

  /** The job whose triage action is in flight, so its button cannot be pressed twice. */
  let acking = $state<string | null>(null);

  function subjectOf(job: Job): string {
    return `${job.repo ?? 'this repository'} / ${readableJobText(job.job_name) || 'this job'}`;
  }

  /**
   * The triage action on a row of the attention view: the same decision the
   * Overview's failures panel and the drawer write, through the same route.
   * Checking failures off as they are read is what this view is for, and a
   * page that showed the work but made the operator open each drawer to act
   * on it would be a list that refuses to be worked through.
   */
  function rowActions(job: Job): RowAction[] {
    if (!filters.attention || !session.can('operator') || !job.id) return [];
    return [
      {
        id: 'acknowledge',
        label: 'Mark as known',
        icon: Check,
        disabled: acking === job.id,
        onSelect: () => void acknowledge(job),
      },
    ];
  }

  /**
   * Mark one failure as known, from its row. The row leaves on the next
   * refetch, which the frame triggers; the toast is the undo, exactly as in
   * the Overview's panel.
   */
  async function acknowledge(job: Job): Promise<void> {
    if (!job.id || acking !== null) return;
    acking = job.id;
    try {
      await acknowledgeJob(job.id);
      liveKey += 1;
      toasts.success(
        'Marked as known',
        'The failure stays in the failed view; a re-run that passes is what settles it.',
        { label: 'Undo', run: () => void reopen(job) },
      );
    } catch (cause) {
      toasts.fromError(cause, 'Could not mark the failure as known');
    } finally {
      acking = null;
    }
  }

  async function reopen(job: Job): Promise<void> {
    if (!job.id) return;
    try {
      await reopenJobAttention(job.id);
      liveKey += 1;
    } catch (cause) {
      toasts.fromError(cause, 'Could not put the failure back');
    }
  }

  // A named function, not an inline arrow: the grid's fetch effect reads its
  // props, and a fresh function identity on every render would make it refetch
  // in response to its own results.
  function takePage(rows: Job[]): void {
    pageRows = rows;
  }

  function rowId(job: Job): string {
    return job.id ?? '';
  }

  /**
   * The one phrase a row has room for on a job that went wrong comes from the
   * shared helper, because the Overview's failures panel shows the same
   * failure in the same words.
   */

  // The sortable ids are the column names the store understands: queued_at,
  // started_at, completed_at, repo, workflow, state, duration and queue_wait.
  const baseColumns = $derived<GridColumn<Job>[]>([
    {
      id: 'state',
      header: 'State',
      sortable: true,
      width: '9.5rem',
      hideable: false,
      value: (job) => {
        const queue = queueStatus(job);
        const state = jobStatus(job.state, job.conclusion).label;
        return queue ? `${state} (${queue.label.toLowerCase()})` : state;
      },
      cell: stateCell,
    },
    {
      id: 'failed_at',
      header: 'Failed at',
      priority: 'wide',
      value: failureSummary,
      cell: failedAtCell,
    },
    { id: 'repo', header: 'Repository', sortable: true, value: (job) => job.repo ?? '' },
    { id: 'workflow', header: 'Workflow', sortable: true, value: (job) => job.workflow ?? '' },
    {
      id: 'job_name',
      header: 'Job',
      value: (job) => readableJobText(job.job_name),
      cell: jobNameCell,
    },
    {
      id: 'labels',
      header: 'Labels',
      priority: 'wide',
      value: (job) => (job.labels ?? []).join(' '),
      cell: labelsCell,
    },
    {
      id: 'pool',
      header: 'Pool',
      priority: 'wide',
      value: (job) => job.pool_name ?? '',
      cell: poolCell,
    },
    {
      // Which class of host the job was put in, and where it ran when that
      // was another. Off by default: until size routing is on there is nothing
      // in it, and a column of dashes is not a reason to move the ones people
      // read.
      id: 'class',
      header: 'Size class',
      help: 'Which class of host the job was put in, and where it ran when that was another.',
      priority: 'wide',
      hiddenByDefault: true,
      value: (job) => classCell(job) || NO_VALUE,
    },
    {
      id: 'runner',
      header: 'Runner',
      priority: 'wide',
      value: (job) => job.runner_name ?? '',
      cell: runnerCell,
    },
    {
      id: 'controller_version',
      header: 'Controller version',
      priority: 'wide',
      hiddenByDefault: true,
      value: (job) => job.controller_version ?? NO_VALUE,
    },
    {
      id: 'queue_wait',
      header: 'Queue wait',
      sortable: true,
      align: 'end',
      width: '8rem',
      value: (job) => formatDuration(job.queue_wait_ms),
      cell: queueWaitCell,
    },
    {
      id: 'duration',
      header: 'Duration',
      sortable: true,
      align: 'end',
      width: '8rem',
      value: (job) => formatDuration(job.duration_ms),
      cell: durationCell,
    },
    {
      id: 'queued_at',
      header: 'Queued',
      sortable: true,
      width: '9rem',
      value: (job) => job.queued_at ?? '',
      cell: queuedCell,
    },
    { id: 'link', header: 'Run', priority: 'wide', width: '5.5rem', align: 'end', cell: linkCell },
  ]);
  const preferred = ['job_name', 'repo', 'state', 'queue_wait', 'duration'];
  const shownColumns = $derived(
    operational
      ? [...baseColumns].sort(
          (a, b) =>
            (preferred.includes(a.id) ? preferred.indexOf(a.id) : preferred.length) -
            (preferred.includes(b.id) ? preferred.indexOf(b.id) : preferred.length),
        )
      : baseColumns,
  );
  /*
   * The triage action is a column rather than a row action on the whole grid,
   * and only in the attention view: every other view is history, where "mark
   * as known" is a decision about a failure somebody has gone looking for, not
   * about the row in front of them. It is last so the columns an operator
   * reads keep the places they have everywhere else.
   */
  const triageColumn: GridColumn<Job> = {
    id: 'triage',
    header: 'Triage',
    width: '4.5rem',
    align: 'end',
    cell: triageCell,
  };
  const columns = $derived(filters.attention ? [...shownColumns, triageColumn] : shownColumns);

  // The same scope the page's own figures are in: this fleet's jobs, or every job GitHub reports.
  const bandStats = $derived(filters.all ? fleet.stats : (fleet.stats?.fleet ?? fleet.stats));
  const bandLine = $derived(
    bandStats
      ? jobsLine({
          running: bandStats.running_jobs ?? 0,
          queued: bandStats.queued_jobs ?? 0,
          succeeded: bandStats.succeeded ?? 0,
          failed: bandStats.failed ?? 0,
          windowText: windowPhrase(describeWindow(fleet.stats?.window)),
        })
      : null,
  );
</script>

{#snippet jobNameCell(job: Job)}
  {#if operational}
    <button
      class="job-inspect"
      type="button"
      aria-label="Inspect {readableJobText(job.job_name) || 'job'}"
      onclick={(event) => {
        event.stopPropagation();
        open(job);
      }}>{readableJobText(job.job_name)}</button
    >
  {:else}{readableJobText(job.job_name)}{/if}
{/snippet}

{#snippet stateCell(job: Job)}
  <span class="state">
    <StateCell status={jobStatus(job.state, job.conclusion)} />
    <!-- GitHub still calls a stood-down job queued, because Zoomies cannot
         unqueue one. Without this the operator's own decision was invisible
         here, and a job they had removed from the queue sat in the list
         looking like work the fleet was about to pick up. -->
    {#if queueStatus(job)}
      {@const queue = queueStatus(job)}
      <Badge status={queue!} size="sm" help={queue!.hint} />
    {/if}
    {#if fleetFailed(job)}
      <Badge status={RUNNER_LOST} size="sm" help={job.fault_fix || RUNNER_LOST.hint} />
    {/if}
    {#if stuckUnmatched(job)}
      <Badge status={UNMATCHED} size="sm" help={UNMATCHED.hint} />
    {:else if job.hosted && job.matched === false}
      <Badge status={HOSTED} size="sm" help={HOSTED.hint} />
    {/if}
  </span>
{/snippet}

{#snippet failedAtCell(job: Job)}
  {#if fleetFailed(job)}
    <Tooltip bare text={job.runner_fault || job.fault_fix || ''}>
      <span class="failed-at danger">{failureSummary(job)}</span>
    </Tooltip>
  {:else if job.failed_step}
    <Tooltip bare text="Step {job.failed_step.number}: {job.failed_step.name}">
      <span class="failed-at">{job.failed_step.name}</span>
    </Tooltip>
  {:else}
    <span class="none">{NO_VALUE}</span>
  {/if}
{/snippet}

{#snippet labelsCell(job: Job)}
  <JobLabels labels={job.labels} />
{/snippet}

{#snippet poolCell(job: Job)}
  {#if job.pool_id}
    <a href="/pools/{job.pool_id}" onclick={(event) => event.stopPropagation()}>
      {job.pool_name || job.pool_id}
    </a>
  {:else}
    <span class="none">Unclaimed</span>
  {/if}
{/snippet}

{#snippet runnerCell(job: Job)}
  {#if job.runner_id}
    <a href="/runners/{job.runner_id}" onclick={(event) => event.stopPropagation()}>
      {job.runner_name || job.runner_id}
    </a>
  {:else}
    <span class="none">{NO_VALUE}</span>
  {/if}
{/snippet}

{#snippet queueWaitCell(job: Job)}
  {#if job.state === 'queued' && !job.started_at}
    <span class="waiting"><Duration from={job.queued_at} live /> so far</span>
  {:else}
    {formatDuration(job.queue_wait_ms)}
  {/if}
{/snippet}

{#snippet durationCell(job: Job)}
  {#if job.state === 'in_progress' && job.started_at}
    <span class="waiting"><Duration from={job.started_at} live /> so far</span>
  {:else if job.duration_ms}
    {formatDuration(job.duration_ms)}
  {:else}
    <span class="none">{NO_VALUE}</span>
  {/if}
{/snippet}

{#snippet queuedCell(job: Job)}
  <RelativeTime value={job.queued_at} />
{/snippet}

{#snippet linkCell(job: Job)}
  <GitHubLink
    href={job.html_url}
    runNumber={job.run_number}
    label="Open {readableJobText(job.job_name) || 'this job'} on GitHub, in a new tab"
    onclick={(event) => event.stopPropagation()}
  />
{/snippet}

{#snippet triageCell(job: Job)}
  <RowActions actions={rowActions(job)} subject={subjectOf(job)} />
{/snippet}

<PageHeader
  title="Jobs"
  subtitle={filters.all
    ? 'Every workflow job GitHub has told Zoomies about, whatever ran it.'
    : 'The workflow jobs this fleet claims, runs, or is waiting to run.'}
  onrefresh={refreshPage}
  ><Button href="/comparisons" variant="secondary">Compare runs</Button></PageHeader
>
<PageBand page="jobs" line={bandLine}>
  <!-- Today, by the hour: the punch card's bottom row alone. An hour pressed
       narrows the list to it, across every status, since a past hour's jobs
       have mostly finished. -->
  {#snippet aside()}
    {#if fleet.loaded}<FleetPunchCard
        days={1}
        pickHint="Pick an hour to narrow the list to it."
        onpick={(at) => {
          setView('all');
          patch({
            since: localMoment(at),
            until: localMoment(new Date(at.getTime() + 59 * 60_000)),
          });
        }}
      />{/if}
  {/snippet}
</PageBand>

{#each layoutSections as section (section)}
  {#if section === 'context'}
    <details
      class="fleet-context"
      open={contextExpanded}
      ontoggle={(event) => (contextExpanded = event.currentTarget.open)}
    >
      <summary>Fleet context and activity</summary>
      {@render fleetContext()}
    </details>
  {:else}
    <div class="content">
      <div class="toolbar">
        <JobViewFilter value={view} onchange={setView} />
        <LevelSwitch level="jobs" />
      </div>

      <JobFilters
        {compact}
        statusChips={view === ''}
        value={filters}
        {facets}
        pools={fleet.pools}
        hosts={fleet.hosts}
        {labelOptions}
        onchange={patch}
        onclear={clearFilters}
      />

      <!-- Before the list, where its question is asked; on a Mission Control
           phone after it, since records come before supplementary analytics
           there (docs/ui-guidelines.md). -->
      {#if !compact}{@render queuedCard()}{@render failingCard()}{/if}

      {#if filters.unmatched && unmatchedOnPage > 0}
        <UnmatchedNote count={unmatchedOnPage} />
      {/if}

      <div id="jobs-list" class="jump-target" tabindex="-1" use:revealHash>
        <DataGrid
          gridId="jobs"
          label="Jobs"
          {columns}
          fetcher={fetchJobs}
          {rowId}
          {filters}
          defaultSort="queued_at"
          defaultOrder="desc"
          noun="jobs"
          {liveKey}
          onopen={open}
          onrows={takePage}
          {emptyTitle}
          {emptyDescription}
        >
          {#snippet emptyAction()}
            {#if narrowed}
              <Button variant="secondary" onclick={clearFilters}>Clear filters</Button>
            {:else if view !== '' && view !== 'all'}
              <!--
          A narrowed status view that came back empty is the one case where the
          way out is not another filter but the same page without this one, so
          it is offered as a button rather than left to be found in the row.
        -->
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
      </div>

      {#if compact}{@render queuedCard()}{@render failingCard()}{/if}

      <p class="footnote">
        Sort by queue wait to find work that waited, or by duration to find work that took its time.
        Both are sorted by the server, so the slowest job in the fleet is one click away rather than
        one page away.
      </p>
    </div>
  {/if}
{/each}

{#snippet queuedCard()}
  {#if queuedShown}
    <AnalysisCard
      title="Why jobs waited"
      finding={wait}
      error={waitError}
      loading={waitLoading}
      scope={waitScope.scope}
      narrowedTo={waitNarrowed}
      note={waitScope.unapplied}
    >
      {#if wait}
        {@const ev = wait.evidence}
        <dl class="facts">
          <div>
            <dt>Jobs</dt>
            <dd class="figure">
              {pluralise(ev.jobs, 'job')} queued, {formatNumber(ev.waited)} waited, {formatDuration(
                ev.wait_seconds * 1000,
              )} of waiting
            </dd>
          </div>
          {#if !waitPool && ev.pools_cut > 0}
            <div>
              <dt>Pools</dt>
              <dd>
                {pluralise(ev.pools_cut, 'pool')} left out below and counted in the totals; filter to
                one pool to read it whole.
              </dd>
            </div>
          {/if}
        </dl>
        {#if ev.fleet.length}
          <p class="rows-label" id="wait-causes">Where the wait went</p>
          <ul class="rows" aria-labelledby="wait-causes">
            {#each ev.fleet as c (c.cause)}
              <li>
                <span>{wordFor(CAUSE_WORDS, c.cause)}</span>
                <span class="figure muted"
                  >{formatPercent(c.share)}, {formatDuration(c.seconds * 1000)}</span
                >
              </li>
            {/each}
          </ul>
        {/if}
        {#if ev.pools.length > 1}
          <p class="rows-label" id="wait-pools">By pool</p>
          <ul class="rows" aria-labelledby="wait-pools">
            {#each ev.pools as p (p.pool_id)}
              <li>
                {#if p.pool_id && !p.deleted}
                  <a href="/pools/{p.pool_id}">{p.pool_name || p.pool_id}</a>
                {:else}
                  <span>{p.pool_name || (p.pool_id ? p.pool_id : 'No pool claimed them')}</span>
                {/if}
                {#if p.deleted}<span class="muted">deleted since</span>{/if}
                <span class="figure muted"
                  >{pluralise(p.jobs, 'job')}, {formatDuration(p.wait_seconds * 1000)} waiting{p.dominant
                    ? `, most of it ${wordFor(CAUSE_WORDS, p.dominant).toLowerCase()}`
                    : ''}</span
                >
              </li>
            {/each}
          </ul>
        {/if}
        {#if longest.length}
          <p class="rows-label" id="wait-longest">Longest waits</p>
          <ul class="rows" aria-labelledby="wait-longest">
            {#each longest as j (j.job_id)}
              <li>
                <span class="quoted">{jobWords(j) || j.job_id}</span>
                <span class="figure muted"
                  >{formatDuration(j.wait_seconds * 1000)} in {j.pool}, {wordFor(
                    CAUSE_WORDS,
                    j.cause,
                  ).toLowerCase()}</span
                >
              </li>
            {/each}
          </ul>
        {/if}
        {#if ev.no_runner_record > 0}
          <p class="evidence-note">
            {pluralise(ev.no_runner_record, 'job')} waited past becoming eligible with no record of its
            runner left, so that wait could not be split into startup and GitHub's hand-off.
          </p>
        {/if}
        {#if ev.runner_kept > 0}
          <p class="evidence-note">
            {pluralise(ev.runner_kept, 'job')} waited on a runner its pool keeps between jobs, so that
            wait could not be split either.
          </p>
        {/if}
        {#if ev.jobs_truncated || ev.runners_truncated}
          <p class="evidence-note">
            A read stopped at its limit, so the newest records were read; a shorter period reads all
            of them.
          </p>
        {/if}
      {/if}
    </AnalysisCard>
  {/if}
{/snippet}

{#snippet failingCard()}
  {#if failingShown}
    <AnalysisCard
      title="Which jobs keep failing"
      finding={failing}
      error={failingError}
      loading={failingLoading}
      scope={failingScope.scope}
      narrowedTo={{}}
      note={failingScope.unapplied}
    >
      {#if failing}
        {@const ev = failing.evidence}
        <dl class="facts">
          <div>
            <dt>Jobs</dt>
            <dd class="figure">
              {formatNumber(ev.jobs)} finished with a pass or a failure, {formatNumber(ev.failures)} failed
            </dd>
          </div>
        </dl>
        {#if ev.groups.length}
          <p class="rows-label" id="failing-jobs">Jobs that failed three times or more</p>
          <ul class="rows" aria-labelledby="failing-jobs">
            {#each ev.groups as g (g.last_job_id)}
              <li>
                <span class="quoted"
                  >{jobWords({ repo: g.repository, workflow: g.workflow, job_name: g.job }) ||
                    g.last_job_id}</span
                >
                <span class="figure muted"
                  >{g.failures} of {g.runs} runs, {pluralise(
                    g.branches,
                    'branch',
                    'branches',
                  )}{g.failed_step ? `, usually at ${readableJobText(g.failed_step)}` : ''}; {wordFor(
                    FAILURE_CLASS_WORDS,
                    g.class,
                  ).toLowerCase()}{g.still_failing ? ', still failing' : ''}</span
                >
              </li>
            {/each}
          </ul>
        {/if}
        {#if ev.groups_cut > 0}
          <p class="evidence-note">
            {pluralise(ev.groups_cut, 'job')} left out above; a shorter period reads fewer.
          </p>
        {/if}
        {#if ev.truncated}
          <p class="evidence-note">
            The read stopped at its limit, so the newest jobs were read; a shorter period reads all
            of them.
          </p>
        {/if}
      {/if}
    </AnalysisCard>
  {/if}
{/snippet}

{#snippet fleetContext()}
  <JobsInsights others={filters.all} />
  <details class="activity-history">
    <summary>Explore fleet activity over the last 24 hours</summary><FleetHistory
      others={filters.all}
    />
  </details>
  <SizeAdvice refresh={adviceKey} />
{/snippet}
<JobDrawer bind:open={drawerOpen} job={selected} onclose={() => (selected = null)} />

<style>
  .jump-target {
    scroll-margin-top: calc(var(--z-topbar-height) + var(--z-space-4));
  }
  .job-inspect {
    text-align: left;
    color: var(--z-accent);
    cursor: pointer;
    overflow-wrap: anywhere;
  }

  @media (pointer: coarse) {
    .job-inspect {
      min-width: var(--z-control-touch);
      min-height: var(--z-control-touch);
    }
    .fleet-context > summary {
      min-height: var(--z-control-touch);
    }
  }

  .fleet-context {
    display: block;
    margin-block: var(--z-space-4);
  }
  .fleet-context > summary {
    padding: var(--z-space-3);
    color: var(--z-accent);
    cursor: pointer;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  .fleet-context[open] > summary {
    margin-bottom: var(--z-space-3);
  }

  .activity-history {
    margin: var(--z-space-4) 0 var(--z-space-5);
  }
  .activity-history summary {
    padding: var(--z-space-3);
    color: var(--z-accent);
    font-size: var(--z-text-sm);
    cursor: pointer;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  .activity-history[open] summary {
    margin-bottom: var(--z-space-3);
  }
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
  .state {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    white-space: nowrap;
  }
  a {
    color: var(--z-accent);
  }
  .none {
    color: var(--z-text-subtle);
  }
  .waiting {
    color: var(--z-pending);
    white-space: nowrap;
  }
  .failed-at {
    color: var(--z-text);
    overflow-wrap: anywhere;
  }
  .failed-at.danger {
    color: var(--z-danger);
    font-weight: var(--z-weight-medium);
  }
  .footnote {
    margin: 0;
    color: var(--z-text-subtle);
    font-size: var(--z-text-xs);
  }
</style>
