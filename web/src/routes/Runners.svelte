<!--
  Runners: what exists right now.

  Live operational context precedes the grid. Every filter lives in the query string, so a narrowed
  view -- "busy runners on host-2" -- is a URL somebody can paste into a chat
  window and land on exactly what you were looking at.

  It stays live without anything being pressed: the fleet cache applies runner.*
  events as they arrive, the grid refetches off its version counter, and the
  state cell reads the cached runner first so a badge flips the moment the
  event lands rather than when the next page of rows does. Refreshing by hand
  reconciles that cache, which bumps the same counter, so one gesture brings
  both the cache and the page of rows up to date.
-->
<script lang="ts">
  import Tooltip from '$lib/components/Tooltip.svelte';
  import { readableJobText } from '$lib/jobs/name';
  import RunnerInsights from '$lib/insights/RunnerInsights.svelte';
  import PageBand from '$lib/bands/PageBand.svelte';
  import { demandMore, demandStats } from '$lib/bands/demand-stats';
  import { revealHash } from '$lib/bands/jump';
  import { runnersLine } from '$lib/bands/runners-line';
  import { provisioningDemand } from '$lib/insights/provisioning.svelte';
  import { finite, poolSignals, queueTone } from '$lib/insights/signals';
  import FleetHistory from '$lib/insights/FleetHistory.svelte';
  import { CircleSlash, Search, Server, Trash2 } from '@lucide/svelte';
  import { analyseStartupFailures, listRunners } from '$lib/api/client';
  import {
    RUNNER_STATES,
    type Runner,
    type RunnerState,
    type StartupFailuresFinding,
  } from '$lib/api/types';
  import {
    NO_VALUE,
    formatAbsolute,
    formatBytes,
    formatNumber,
    formatPercent,
    pluralise,
  } from '$lib/format';
  import { faultLabel } from '$lib/faults';
  import AnalysisCard from '$lib/insights/AnalysisCard.svelte';
  import {
    analysisWindow,
    refusalOf,
    type AnalysisNarrowing,
    EXPLANATION_WORDS,
    startupFailuresScope,
    STAGE_WORDS,
    wordFor,
  } from '$lib/insights/analysis';
  import { registerSearch } from '$lib/keys';
  import { navigate, router } from '$lib/router';
  import { QUEUE_STATUS_LABELS, runnerStatus } from '$lib/status';
  import { fleet } from '$lib/state/fleet.svelte';
  import { prefs } from '$lib/state/prefs.svelte';
  import { session } from '$lib/state/session.svelte';
  import Button from '$lib/components/Button.svelte';
  import CopyButton from '$lib/components/CopyButton.svelte';
  import DataGrid from '$lib/components/DataGrid.svelte';
  import type {
    BulkAction,
    GridColumn,
    GridPage,
    GridQuery,
  } from '$lib/components/DataGrid.svelte';
  import DropdownMenu from '$lib/components/DropdownMenu.svelte';
  import type { MenuItem } from '$lib/components/DropdownMenu.svelte';
  import Duration from '$lib/components/Duration.svelte';
  import GitHubLink from '$lib/jobs/GitHubLink.svelte';
  import FilterBar from '$lib/components/FilterBar.svelte';
  import type { FilterChip } from '$lib/components/FilterBar.svelte';
  import Input from '$lib/components/Input.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Select from '$lib/components/Select.svelte';
  import Switch from '$lib/components/Switch.svelte';
  import RunnerConfirm from '$lib/runners/RunnerConfirm.svelte';
  import RunnerMemoryBadges from '$lib/runners/RunnerMemoryBadges.svelte';
  import RunnerStatus from '$lib/runners/RunnerStatus.svelte';
  import { runnerDisplayStatus } from '$lib/runners/runner-status';
  import FilterChips from '$lib/components/FilterChips.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import { runnerStateChips, runnersEyebrow } from '$lib/runners/state-chips';

  const canOperate = $derived(session.can('operator'));

  /* -- filters, kept in the URL under the API's own names ------------------- */

  const search = $derived(router.param('q'));
  const poolId = $derived(router.param('pool_id'));
  const hostId = $derived(router.param('host_id'));
  const includeRemoved = $derived(router.param('include_removed') === '1');
  const states = $derived(
    router
      .paramList('state')
      .filter((value): value is RunnerState =>
        (RUNNER_STATES as readonly string[]).includes(value),
      ),
  );

  const filters = $derived({ search, poolId, hostId, includeRemoved, states });
  // The provisioning figures the band carries. They follow the pool filter, as the strip
  // they replace did, and are said so by the context line under the band.
  const demand = provisioningDemand(() => poolId);
  const queuedNow = $derived(
    poolId
      ? poolSignals(fleet.pools, fleet.stats).find((p) => p.pool.id === poolId)?.queued
      : finite(fleet.stats?.fleet?.queued_jobs),
  );
  // The demand above says what is waiting; this says what is being worked on,
  // so a quiet queue beside a busy fleet is not read as an idle one.
  const runningNow = $derived(
    poolId
      ? poolSignals(fleet.pools, fleet.stats).find((p) => p.pool.id === poolId)?.busy
      : finite(fleet.stats?.runners?.busy),
  );
  const bandFigures = $derived(
    demandStats({
      queued: queuedNow ?? null,
      running: runningNow ?? null,
      queuedWarning: queueTone(queuedNow, session.meta?.queue_warning_threshold) === 'warning',
      counts: demand.counts,
      oldest: demand.oldest,
      poolId,
      labels: QUEUE_STATUS_LABELS,
      // The list this figure counts is on the page, so pressing it narrows the
      // rows to busy runners and takes the reader to them.
      onPageRunning: {
        section: 'runners-list',
        active: states.length === 1 && states[0] === 'busy',
        onselect: () => router.setQuery({ state: ['busy'], offset: null }),
      },
    }),
  );

  /* What keeps failing to start ------------------------------------------------
   * Narrowing the list to failed runners is asking "why do these keep
   * failing?", so that is when the startup-failures analysis is shown. It
   * reads the day before now (failed runners are kept for retention.runners,
   * and the list has no dates of its own) and the pool and host the list is
   * narrowed to.
   * ------------------------------------------------------------------------ */

  // Not for a public view's visitor, whom the route refuses before it reads
  // anything (lib/shell/sections.ts says why a refused thing is not offered).
  const failedShown = $derived(!session.visitor && states.includes('failed'));
  // The pool and host the card was asked about, for Ask Eli to pass to its
  // tool: a finding narrowed to one of them never names it.
  const startupNarrowed = $derived<AnalysisNarrowing>({
    ...(poolId ? { pool: { id: poolId, name: fleet.pool(poolId)?.name } } : {}),
    ...(hostId ? { host: { id: hostId, name: fleet.host(hostId)?.name } } : {}),
  });
  // What the card covers, and the list's search it leaves out.
  const startupScope = $derived(
    startupFailuresScope(
      { pool_id: poolId, host_id: hostId, q: search },
      { pool: (id) => fleet.pool(id)?.name ?? id, host: (id) => fleet.host(id)?.name ?? id },
    ),
  );
  let startup = $state.raw<StartupFailuresFinding | null>(null);
  let startupError = $state<{ message: string; refused: boolean } | null>(null);
  let startupLoading = $state(false);
  let startupKey = $state(0);

  $effect(() => {
    if (!failedShown) return;
    const pool = poolId || undefined;
    const host = hostId || undefined;
    void startupKey;
    const controller = new AbortController();
    // The last pool's answer goes at once: left standing while the next loads,
    // it would sit under a list that has already moved on, as the Jobs card's
    // and the host page's do not.
    startup = null;
    startupError = null;
    const period = analysisWindow(undefined, undefined, Date.now());
    if (!period) return;
    startupLoading = true;
    analyseStartupFailures({ ...period, pool_id: pool, host_id: host }, controller.signal)
      .then((next) => {
        if (!controller.signal.aborted) startup = next;
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        // The list of failed runners is still the page's; only this card failed.
        startupError = refusalOf(cause);
      })
      .finally(() => {
        if (!controller.signal.aborted) startupLoading = false;
      });
    return () => controller.abort();
  });

  // The chips' counts are the fleet's, so they are shown only while the rows
  // under them are the fleet's too.
  const counted = $derived(poolId === '' && hostId === '');
  const filtered = $derived(
    search !== '' || poolId !== '' || hostId !== '' || includeRemoved || states.length > 0,
  );

  let searchField = $state<HTMLInputElement | null>(null);
  $effect(() => registerSearch(searchField));

  const poolOptions = $derived([
    { value: '', label: 'Any pool' },
    ...fleet.pools.map((pool) => ({ value: pool.id ?? '', label: pool.name ?? 'unnamed' })),
  ]);

  const hostOptions = $derived([
    { value: '', label: 'Any host' },
    ...fleet.hosts.map((host) => ({ value: host.id ?? '', label: host.name ?? 'unnamed' })),
  ]);

  function nameOf(options: { value: string; label: string }[], id: string): string {
    return options.find((option) => option.value === id)?.label ?? id;
  }

  const chips = $derived.by(() => {
    const active: FilterChip[] = [];
    if (search) {
      active.push({
        id: 'q',
        label: 'Matching',
        value: search,
        onremove: () => router.setQuery({ q: null }),
      });
    }
    if (poolId) {
      active.push({
        id: 'pool',
        label: 'Pool',
        value: nameOf(poolOptions, poolId),
        onremove: () => router.setQuery({ pool_id: null }),
      });
    }
    if (hostId) {
      active.push({
        id: 'host',
        label: 'Host',
        value: nameOf(hostOptions, hostId),
        onremove: () => router.setQuery({ host_id: null }),
      });
    }
    for (const state of states) {
      active.push({
        id: `state-${state}`,
        label: 'State',
        value: runnerStatus(state, prefs.quirkyStatus).label,
        onremove: () => router.setQuery({ state: states.filter((s) => s !== state) }),
      });
    }
    if (includeRemoved) {
      active.push({
        id: 'include_removed',
        label: 'Removed',
        value: 'Shown',
        onremove: () => router.setQuery({ include_removed: null }),
      });
    }
    return active;
  });

  function clearFilters(): void {
    router.setQuery({
      q: null,
      pool_id: null,
      host_id: null,
      state: null,
      include_removed: null,
      offset: null,
    });
  }

  /* -- rows ------------------------------------------------------------------ */

  /** The page currently on screen, so a bulk action knows what it is acting on. */
  let onScreen: Runner[] = [];

  async function fetchRunners(query: GridQuery, signal: AbortSignal): Promise<GridPage<Runner>> {
    const response = await listRunners(
      {
        limit: query.limit,
        offset: query.offset,
        sort: query.sort || undefined,
        order: query.order,
        q: search || undefined,
        pool_id: poolId ? [poolId] : undefined,
        host_id: hostId ? [hostId] : undefined,
        state: states.length > 0 ? states : undefined,
        include_removed: includeRemoved || undefined,
      },
      signal,
    );
    return { items: response.items ?? [], total: response.total ?? 0 };
  }

  /**
   * Warm the fleet cache with the page on screen, so the command palette can
   * find a runner the operator is looking at.
   *
   * Only when it would actually change something: ingesting a row that differs
   * bumps the fleet's shape, and the fleet's shape is this grid's `liveKey`,
   * so warming the cache on every page unconditionally would have the grid
   * refetching itself for ever. Identity is what the palette needs, so
   * identity is what is compared -- CPU and memory move constantly and are
   * not worth a round trip.
   */
  function takeRows(rows: Runner[]): void {
    onScreen = rows;
    const changed = rows.some((row) => {
      const cached = fleet.runner(row.id);
      return (
        !cached ||
        cached.name !== row.name ||
        cached.state !== row.state ||
        cached.pool_id !== row.pool_id ||
        cached.host_id !== row.host_id
      );
    });
    if (changed) fleet.ingestRunners(rows);
  }

  function resolve(ids: readonly string[]): Runner[] {
    return ids.map((id) => onScreen.find((row) => row.id === id) ?? fleet.runner(id) ?? { id });
  }

  /* -- drain and delete ------------------------------------------------------- */

  let confirmOpen = $state(false);
  let confirmAction = $state<'drain' | 'delete'>('drain');
  let confirmTargets = $state<Runner[]>([]);

  /** Settles the bulk action that opened the confirmation, once it closes. */
  let confirmSettled: ((done: boolean) => void) | null = null;

  /**
   * Open the confirmation. Resolves when it closes: true once the controller
   * accepted the request, so the grid clears its selection; false on cancel,
   * so the rows stay ticked for a second thought.
   */
  function ask(action: 'drain' | 'delete', targets: Runner[]): Promise<boolean> {
    if (targets.length === 0) return Promise.resolve(false);
    confirmAction = action;
    confirmTargets = targets;
    confirmOpen = true;
    return new Promise((resolve) => {
      confirmSettled?.(false);
      confirmSettled = resolve;
    });
  }

  function settleConfirm(done: boolean): void {
    if (done) confirmTargets = [];
    confirmSettled?.(done);
    confirmSettled = null;
  }

  const bulkActions = $derived<BulkAction[]>(
    canOperate
      ? [
          {
            id: 'drain',
            label: 'Drain',
            icon: CircleSlash,
            run: (ids) => ask('drain', resolve(ids)),
          },
          {
            id: 'delete',
            label: 'Delete',
            icon: Trash2,
            danger: true,
            run: (ids) => ask('delete', resolve(ids)),
          },
        ]
      : [],
  );

  function actionsFor(runner: Runner): MenuItem[] {
    const terminal = runner.state === 'removed' || runner.state === 'failed';
    return [
      {
        id: 'drain',
        label: 'Drain',
        icon: CircleSlash,
        disabled: terminal || runner.state === 'draining',
        reason: terminal
          ? 'This runner has already finished, so there is nothing to drain.'
          : 'This runner is already draining. It leaves when its job ends.',
        onSelect: () => ask('drain', [runner]),
      },
      {
        id: 'delete',
        label: 'Delete',
        icon: Trash2,
        danger: true,
        separated: true,
        onSelect: () => ask('delete', [runner]),
      },
    ];
  }

  /* -- columns ---------------------------------------------------------------- */

  const columns = $derived.by(() => {
    const list: GridColumn<Runner>[] = [
      {
        id: 'state',
        header: 'Status',
        sortable: true,
        hideable: false,
        width: '11.5rem',
        fixed: true,
        wrap: true,
        value: (row) => runnerDisplayStatus(fleet.runner(row.id) ?? row, prefs.quirkyStatus).label,
        cell: stateCell,
      },
      {
        id: 'name',
        header: 'Name',
        sortable: true,
        hideable: false,
        width: '18rem',
        value: (row) => row.name ?? '',
        cell: nameCell,
      },
      {
        // Pool and host names are hyphenated, and without a width they wrap
        // one segment per line and triple the row height.
        id: 'pool',
        header: 'Pool',
        sortable: true,
        width: '11rem',
        value: (row) => row.pool_name ?? '',
        cell: poolCell,
      },
      {
        id: 'host',
        header: 'Host',
        priority: 'wide',
        sortable: true,
        width: '10rem',
        value: (row) => row.host_name ?? '',
      },
      {
        id: 'job',
        header: 'Current job',
        value: (row) => readableJobText(row.current_job?.job_name),
        cell: jobCell,
      },
      {
        id: 'created_at',
        header: 'Age',
        priority: 'wide',
        hiddenByDefault: true,
        sortable: true,
        align: 'end',
        width: '7rem',
        value: (row) => row.created_at ?? '',
        cell: ageCell,
      },
      {
        id: 'jobs',
        header: 'Jobs handled',
        priority: 'wide',
        sortable: true,
        align: 'end',
        width: '7rem',
        value: (row) => formatNumber(row.jobs_handled ?? 0),
      },
      {
        id: 'cpu',
        header: 'CPU',
        priority: 'wide',
        hiddenByDefault: true,
        align: 'end',
        width: '6rem',
        value: (row) =>
          row.cpu_percent === undefined ? NO_VALUE : formatPercent((row.cpu_percent ?? 0) / 100, 0),
      },
      {
        id: 'memory',
        header: 'Memory',
        priority: 'wide',
        align: 'end',
        // Room for the figure and two small badges. The grid reads a width as a
        // share, so this is a little more of the frame, not a fixed measure.
        width: '11rem',
        // The cell holds interactive badges with cards of their own, so the grid's
        // native title -- which would open over them -- is turned off by the wrap
        // flag, and the cell keeps itself on one line.
        wrap: true,
        value: (row) => (row.memory_bytes === undefined ? NO_VALUE : formatBytes(row.memory_bytes)),
        cell: memoryCell,
      },
    ];
    if (canOperate) {
      list.push({
        id: 'actions',
        header: 'Actions',
        fixed: true,
        // The menu is anchored in this cell, so the cell must let it out.
        overflows: true,
        hideable: false,
        align: 'end',
        width: '5rem',
        value: () => '',
        cell: actionsCell,
      });
    }
    return list;
  });

  function stopRowClick(event: Event): void {
    event.stopPropagation();
  }
</script>

{#snippet stateCell(runner: Runner)}
  <!-- One cache snapshot supplies both lifecycle and CPU allocation. -->
  <RunnerStatus runner={fleet.runner(runner.id) ?? runner} />
{/snippet}

{#snippet memoryCell(runner: Runner)}
  <!-- The badges read the cache, so a loan or a swap allowance shows the moment
       its event lands; the figure is the row's, like the CPU beside it. -->
  <span class="memory-cell">
    <span class="used">
      {runner.memory_bytes === undefined ? NO_VALUE : formatBytes(runner.memory_bytes)}
    </span>
    <RunnerMemoryBadges runner={fleet.runner(runner.id) ?? runner} wrap />
  </span>
{/snippet}

{#snippet nameCell(runner: Runner)}
  <div class="name-cell">
    <Tooltip bare whenTruncated text={runner.name ?? ''}>
      <a class="name mono" href="/runners/{runner.id}">
        {runner.name ?? 'unnamed'}
      </a>
    </Tooltip>
    <span class="copy" role="presentation" onclick={stopRowClick}>
      <CopyButton value={runner.name ?? ''} label="Copy the runner name" size="sm" />
    </span>
  </div>
{/snippet}

{#snippet poolCell(runner: Runner)}
  {#if runner.pool_id}
    <Tooltip bare whenTruncated text={runner.pool_name ?? runner.pool_id}>
      <a class="link" href="/pools/{runner.pool_id}" onclick={stopRowClick}>
        {runner.pool_name ?? runner.pool_id}
      </a>
    </Tooltip>
  {:else}
    <span class="quiet">{NO_VALUE}</span>
  {/if}
{/snippet}

{#snippet jobCell(runner: Runner)}
  {#if runner.current_job}
    <span class="job">
      <span class="job-name">{readableJobText(runner.current_job.job_name) || 'Unnamed job'}</span>
      <span class="job-repo">
        <span class="job-repo-name mono">{runner.current_job.repo ?? ''}</span>
        <GitHubLink
          href={runner.current_job.html_url}
          runNumber={runner.current_job.run_number}
          label="Open {readableJobText(runner.current_job.job_name) ||
            'this job'} on GitHub, in a new tab"
          onclick={stopRowClick}
        />
      </span>
    </span>
  {:else}
    <span class="quiet">{NO_VALUE}</span>
  {/if}
{/snippet}

{#snippet ageCell(runner: Runner)}
  <Duration
    from={runner.created_at}
    to={runner.finished_at ?? undefined}
    live={!runner.finished_at}
  />
{/snippet}

{#snippet actionsCell(runner: Runner)}
  <div class="row-actions" role="presentation" onclick={stopRowClick}>
    <DropdownMenu
      items={actionsFor(runner)}
      label="Actions for {runner.name ?? 'this runner'}"
      size="sm"
    />
  </div>
{/snippet}

<PageHeader
  title="Runners"
  subtitle="Every runner the controller knows about right now, and what it is doing."
  onrefresh={() => {
    startupKey += 1;
    return fleet.reconcile();
  }}
/>

<PageBand
  page="runners"
  stats={bandFigures}
  more={demandMore(poolId)}
  line={fleet.stats?.runners
    ? runnersLine({
        provisioning: fleet.stats.runners.provisioning ?? 0,
        registering: fleet.stats.runners.registering ?? 0,
        idle: fleet.stats.runners.idle ?? 0,
        busy: fleet.stats.runners.busy ?? 0,
        draining: fleet.stats.runners.draining ?? 0,
      })
    : null}
/>
<!-- The filters in effect, each removable; it draws nothing when there are none. -->
<FilterBar {chips} onclear={clearFilters} class="in-effect" />

{#if failedShown}
  <div class="startup">
    <AnalysisCard
      title="What keeps failing to start"
      finding={startup}
      error={startupError}
      loading={startupLoading}
      scope={startupScope.scope}
      note={startupScope.unapplied}
      narrowedTo={startupNarrowed}
    >
      {#if startup}
        {@const ev = startup.evidence}
        <dl class="facts">
          <div>
            <dt>Starts</dt>
            <dd class="figure">
              {formatNumber(ev.starts)} with an outcome, {formatNumber(ev.failures)} failed, {formatNumber(
                ev.unknown,
              )} with no outcome on record
            </dd>
          </div>
        </dl>
        {#if ev.groups.length}
          <p class="rows-label" id="startup-groups">Where they fail</p>
          <ul class="rows" aria-labelledby="startup-groups">
            {#each ev.groups as g (g.scope + g.id + g.pool_id)}
              <li>
                <span>
                  {g.scope === 'host' ? 'Host' : 'Pool'}
                  <a href="/{g.scope === 'host' ? 'hosts' : 'pools'}/{g.id}">{g.name || g.id}</a>
                  {#if g.pool_id}
                    <!-- A host's group that counts one pool's starts alone. Bare, its
                         figures read as the host's own across every pool, which is the
                         one thing the route says they are not. -->
                    <span class="muted">
                      (starts of pool <a href="/pools/{g.pool_id}"
                        >{fleet.pool(g.pool_id)?.name ?? g.pool_id}</a
                      > alone)
                    </span>
                  {/if}
                </span>
                <span class="figure muted">
                  {g.failures} of {pluralise(g.starts, 'start')} failed, failing since {formatAbsolute(
                    g.failing_since,
                  )}{g.recovered_at
                    ? `, recovered ${formatAbsolute(g.recovered_at)}`
                    : ', still failing'}
                </span>
                <span class="muted">
                  Mostly {wordFor(STAGE_WORDS, g.stage)}{g.fault_kind
                    ? `, ${faultLabel(g.fault_kind).toLowerCase()}`
                    : ''}{g.explanation
                    ? `; ${g.explained} put down to ${wordFor(EXPLANATION_WORDS, g.explanation)}`
                    : ''}
                </span>
                {#if g.last_message}
                  <!-- A host wrote this; it is shown as the characters it is. -->
                  <span class="quoted">{g.last_message}</span>
                {/if}
                {#if g.last_runner_id}
                  <a href="/runners/{g.last_runner_id}">Open its last failed runner</a>
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
        {#if ev.groups_cut > 0}
          <p class="evidence-note">
            {pluralise(ev.groups_cut, 'more host or pool', 'more hosts and pools')} left out; filter to
            one to read it.
          </p>
        {/if}
        {#if ev.truncated}
          <p class="evidence-note">
            The runner read stopped at its limit, so the newest were read.
          </p>
        {/if}
      {/if}
    </AnalysisCard>
  </div>
{/if}

<!--
  The list as one card: the count over the title, the search and the menus
  opposite it, the states as chips in a row of their own, and the grid bare
  inside, so the page shows one frame. The jump target stays on the wrapper,
  so the band's figure still lands on the list.
-->
<div id="runners-list" class="jump-target" tabindex="-1" use:revealHash>
  <Panel
    title="All runners"
    icon={Server}
    eyebrow={runnersEyebrow(fleet.stats?.runners)}
    flush
    class="list-card"
  >
    {#snippet actions()}
      <div class="search">
        <Input
          bind:element={searchField}
          value={search}
          type="search"
          icon={Search}
          size="sm"
          placeholder="Search by name, ID or container"
          ariaLabel="Search runners"
          oninput={(event) =>
            router.setQuery({ q: (event.currentTarget as HTMLInputElement).value || null })}
        />
      </div>
      <div class="picker">
        <Select
          value={poolId}
          size="sm"
          ariaLabel="Filter by pool"
          options={poolOptions}
          onchange={(value) => router.setQuery({ pool_id: value || null })}
        />
      </div>
      <div class="picker">
        <Select
          value={hostId}
          size="sm"
          ariaLabel="Filter by host"
          options={hostOptions}
          onchange={(value) => router.setQuery({ host_id: value || null })}
        />
      </div>
      <Switch
        checked={includeRemoved}
        size="sm"
        label="Include removed"
        labelToggles={false}
        description="Kept for a while as history."
        onchange={(on) => router.setQuery({ include_removed: on ? '1' : null })}
      />
    {/snippet}
    {#snippet toolbar()}
      <FilterChips
        label="Filter by runner state"
        total={counted ? fleet.stats?.runners?.total : undefined}
        options={runnerStateChips(fleet.stats?.runners, prefs.quirkyStatus, counted)}
        selected={states}
        onchange={(next) => router.setQuery({ state: next.length > 0 ? next : null })}
      />
    {/snippet}
    <DataGrid
      gridId="runners"
      label="Runners"
      bare
      {columns}
      {filters}
      {bulkActions}
      fetcher={fetchRunners}
      rowId={(row) => row.id ?? ''}
      defaultSort="created_at"
      defaultOrder="desc"
      selectable={canOperate}
      liveKey={fleet.shape}
      noun="runners"
      onopen={(row) => navigate(`/runners/${row.id ?? ''}`)}
      onrows={takeRows}
      emptyTitle={filtered ? 'No runners match these filters' : 'No runners right now'}
      emptyDescription={filtered
        ? 'Nothing in the fleet answers all of these at once. Removing one of them will widen the search.'
        : 'That is normal when nothing is queued, runners are created on demand.'}
    >
      {#snippet emptyAction()}
        {#if filtered}
          <Button variant="secondary" onclick={clearFilters}>Clear filters</Button>
        {:else}
          <Button variant="secondary" href="/pools">Look at the pools</Button>
        {/if}
      {/snippet}
    </DataGrid>
  </Panel>
</div>

<!--
  Everything that is context rather than the list, folded under it: the
  scope line, the lifecycle, and the last day's activity. The band carries
  the figures, so what was a strip and a disclosure above the grid, costing
  the first screen a row of runners, is one disclosure below it, as the
  Overview folds its History.
-->
<details class="fleet-context fold">
  <summary>Fleet context</summary>
  <RunnerInsights {poolId} pulse={false} folded />
  <details class="activity-history fold">
    <summary>Explore fleet activity over the last 24 hours</summary><FleetHistory />
  </details>
</details>

<RunnerConfirm
  bind:open={confirmOpen}
  action={confirmAction}
  targets={confirmTargets}
  ondone={() => settleConfirm(true)}
  oncancel={() => settleConfirm(false)}
/>

<style>
  .jump-target {
    scroll-margin-top: calc(var(--z-topbar-height) + var(--z-space-4));
  }
  .startup {
    margin: 0 0 var(--z-space-4);
  }
  .fold {
    margin: var(--z-space-4) 0 var(--z-space-5);
  }
  .fold summary {
    padding: var(--z-space-3);
    color: var(--z-accent);
    font-size: var(--z-text-sm);
    cursor: pointer;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  .fold[open] summary {
    margin-bottom: var(--z-space-3);
  }
  .fleet-context .activity-history {
    margin-bottom: 0;
  }
  :global(.in-effect:has(.chip)) {
    margin-bottom: var(--z-space-3);
  }
  .search {
    width: 18rem;
    max-width: 100%;
  }
  .picker {
    width: 11rem;
  }
  .name-cell {
    display: flex;
    align-items: center;
    gap: var(--z-space-1);
    min-width: 0;
  }
  .name {
    color: var(--z-accent);
    font-size: var(--z-text-xs);
    text-decoration: none;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .name:hover {
    text-decoration: underline;
  }
  .copy {
    display: inline-flex;
  }
  /*
    One line, always. Pool and host names are hyphenated, and an inline link
    with no rule about wrapping breaks at every hyphen: the shipped screenshot
    has `zoomies-demo-linux-arm64` set as three lines in a cell, which makes
    every row in the grid that tall. `display: block` is what lets the ellipsis
    happen at all -- overflow on an inline box does not clip -- and the cell
    carries the full value in a title, so the part that was cut is still
    reachable without the copy button.
  */
  .link {
    display: block;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--z-accent);
    text-decoration: none;
  }
  .link:hover {
    text-decoration: underline;
  }
  .quiet {
    color: var(--z-text-subtle);
  }
  .job {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .job-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .job-repo {
    display: flex;
    align-items: center;
    gap: var(--z-space-1);
    min-width: 0;
    font-size: var(--z-text-2xs);
    color: var(--z-text-subtle);
  }
  .job-repo-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .row-actions {
    display: flex;
    justify-content: flex-end;
  }
  /* The figure stays flush right, so the column still lines up down the page, and
     the badges sit under it where there is room for them: a row that has some is a
     line taller, as a row with a job in it already is, and a row with none is as
     it always was. */
  .memory-cell {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: var(--z-space-1);
    min-width: 0;
  }
  .used {
    font-variant-numeric: tabular-nums;
  }
</style>
