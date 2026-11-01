<!--
  The failures nobody has dealt with, which is the first question an operator
  arrives with: what went wrong that I do not know about yet.

  GitHub answers "what failed"; it cannot answer "and have you read it". Zoomies
  can, because a failure can be marked as known here, and it does, because a
  panel that showed every recent failure for ever would stop being read: by the
  third day the same red row is wallpaper, and the failure that is actually new
  is somewhere under it. The three halves of the question live on the server in
  one filter (failed, recent, acknowledged by nobody, not replaced by a later
  attempt), so this panel, the Jobs page's "Needs attention" view and anything
  else that asks cannot disagree about what is still owed.

  Marking as known is not resolving. The failure keeps its row, its conclusion
  and its place in the failed view; what stops is this panel asking. A re-run
  that passes settles it, a re-run that fails is a new failure somebody has to
  look at again, and the toast that announces the acknowledgement carries the
  undo, so a row put away by mistake is one click from coming back.

  Where it ran is the page's own switch: a failure on somebody else's runner is
  in the repositories this fleet watches, but it is not the fleet's to fix, so
  the panel shows this fleet's own by default like the running-jobs panel
  beside it.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import { Check, CircleCheck, Zap } from '@lucide/svelte';
  import { acknowledgeJob, listJobs, reopenJobAttention } from '$lib/api/client';
  import { events } from '$lib/api/sse';
  import type { Job } from '$lib/api/types';
  import { fleetFailed } from '$lib/faults';
  import { formatNumber } from '$lib/format';
  import { readableJobText } from '$lib/jobs/name';
  import { failureSummary } from '$lib/jobs/why';
  import { fleet } from '$lib/state/fleet.svelte';
  import { prefs } from '$lib/state/prefs.svelte';
  import { session } from '$lib/state/session.svelte';
  import { toasts } from '$lib/state/toasts.svelte';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import IconButton from '$lib/components/IconButton.svelte';
  import JobDrawer from '$lib/jobs/JobDrawer.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import OverviewDisclosure from './OverviewDisclosure.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import { eliCanRepairRun, targetHref } from '$lib/repairs/workspace';

  interface Props {
    class?: string;
  }

  let { class: className = '' }: Props = $props();

  /**
   * A glance with the count, not the list: the Jobs page's view is for the whole
   * of it. Kept short on purpose, because the panel sits above the pools and
   * the Overview's own rule is that nothing needing a person pushes the fleet
   * below the fold of a 1440x900 laptop.
   */
  const SHOWN = 2;

  let jobs = $state.raw<Job[]>([]);
  let total = $state(0);
  let loading = $state(true);
  let error = $state<unknown>(null);
  let loadRevision = 0;
  /** The job whose acknowledgement is in flight, so its button cannot be pressed twice. */
  let acking = $state<string | null>(null);
  let selected = $state<Job | null>(null);
  let drawerOpen = $state(false);
  let list = $state<HTMLUListElement | null>(null);

  const others = $derived(prefs.otherRunners);
  // The Jobs page lists this fleet's own jobs unless it is told otherwise, so the
  // link to the rest has to carry the scope the count above it was made in.
  const restHref = $derived(`/jobs?attention=true${others ? '&all=true' : ''}`);
  // The same goes for the line shown when nothing is owed: it says "wherever they
  // ran" only when the switch is on, so its link to the history has to as well.
  const failedHref = $derived(`/jobs?failed=true${others ? '&all=true' : ''}`);
  // Marking a failure as known is the fleet's, not one reader's, so it takes
  // the operator role the API asks for and nobody below it.
  const canAcknowledge = $derived(session.can('operator'));

  export async function reload(): Promise<void> {
    await load(others);
  }

  async function load(all: boolean, signal?: AbortSignal): Promise<void> {
    const revision = ++loadRevision;
    try {
      const page = await listJobs(
        {
          attention: true,
          managed: all ? undefined : true,
          limit: SHOWN,
        },
        signal,
      );
      if (signal?.aborted || revision !== loadRevision) return;
      jobs = page.items ?? [];
      total = page.total ?? jobs.length;
      error = null;
    } catch (cause) {
      if (signal?.aborted || revision !== loadRevision) return;
      error = cause;
    } finally {
      if (!signal?.aborted && revision === loadRevision) loading = false;
    }
  }

  $effect(() => {
    const all = others;
    const controller = new AbortController();
    void load(all, controller.signal);
    return () => controller.abort();
  });

  // The stream says a job changed; whether that adds, settles or removes a
  // failure is the server's composite question, so the panel asks again --
  // at most once a second, because a busy fleet completes jobs in bursts and
  // every one of them is a frame.
  let pending: ReturnType<typeof setTimeout> | null = null;
  function schedule(): void {
    if (pending !== null) return;
    pending = setTimeout(() => {
      pending = null;
      void load(others);
    }, 1000);
  }

  $effect(() =>
    events.subscribe('job.updated', (job) => {
      if (selected && job.id === selected.id) selected = job;
      schedule();
    }),
  );
  $effect(() => () => {
    if (pending !== null) clearTimeout(pending);
  });

  // PR closures and check results can change on GitHub without a job event.
  $effect(() => {
    const timer = setInterval(() => void load(others), 30000);
    return () => clearInterval(timer);
  });

  // One reconciling fetch after a gap in the stream, as the panels around it
  // do: the replay buffer is finite.
  let previous: string | null = null;
  $effect(() => {
    const status = fleet.connection;
    if (previous !== null && previous !== 'live' && status === 'live') void load(others);
    previous = status;
  });

  /**
   * Mark one failure as known. The row leaves at once, because the answer is
   * certain, and comes back with the list as it really is if the request is
   * refused: a triage list that lagged a second behind the button would be
   * pressed twice.
   */
  async function acknowledge(job: Job): Promise<void> {
    if (!job.id || acking !== null) return;
    const before = jobs;
    const beforeTotal = total;
    // The press is on the row that is about to leave, and focus leaves with it.
    const held = list?.contains(document.activeElement) ?? false;
    const at = jobs.findIndex((row) => row.id === job.id);
    acking = job.id;
    ++loadRevision;
    jobs = jobs.filter((row) => row.id !== job.id);
    total = Math.max(0, beforeTotal - 1);
    if (held) void settleFocus(at);
    try {
      await acknowledgeJob(job.id);
      toasts.success(
        'Marked as known',
        'The failure stays in the failed view; a re-run that passes is what settles it.',
        { label: 'Undo', run: () => void reopen(job) },
      );
    } catch (cause) {
      jobs = before;
      total = beforeTotal;
      toasts.fromError(cause, 'Could not mark the failure as known');
    } finally {
      acking = null;
    }
  }

  /**
   * Put focus on the failure that took the place of the one that left, or on the
   * page when none is left, so the keyboard carries on down the list instead of
   * starting again from the top of the document.
   */
  async function settleFocus(at: number): Promise<void> {
    await tick();
    const rows = list ? Array.from(list.querySelectorAll<HTMLElement>('.what')) : [];
    const next = rows[Math.min(at, rows.length - 1)] ?? document.getElementById('page-heading');
    next?.focus();
  }

  async function reopen(job: Job): Promise<void> {
    if (!job.id) return;
    try {
      await reopenJobAttention(job.id);
      await load(others);
    } catch (cause) {
      toasts.fromError(cause, 'Could not put the failure back');
    }
  }

  function open(job: Job): void {
    selected = job;
    drawerOpen = true;
  }
</script>

{#if !error && !loading && jobs.length === 0}
  <!-- Nothing is owed, so nothing needs a card: a line, like the problems summary. -->
  <section class={['clear', className]} aria-label="Recent failures">
    <CircleCheck size={16} aria-hidden="true" />
    <p>
      No failure is waiting on you.
      <a href={failedHref}>Failed view</a>
      keeps the history.
    </p>
  </section>
{:else}
  <div class="failure-attention">
    <OverviewDisclosure
      id="failures"
      title={loading
        ? 'Checking failures'
        : error
          ? 'Recent failures unavailable'
          : `${formatNumber(total)} failures need review`}
    >
      {#snippet actions()}<a class="review-failures" href={restHref}>Review failures</a>{/snippet}
      <Panel
        title="Recent failures"
        icon={Zap}
        description={others
          ? 'Unresolved failures from the last week, wherever they ran. Fixed, superseded and closed PR failures leave this list.'
          : 'Unresolved failures from the last week. Fixed, superseded and closed PR failures leave this list.'}
        class={className}
        flush
      >
        {#snippet actions()}
          {#if !loading && total > 0}
            <Badge tone="danger" label="{formatNumber(total)} to look at" dot={false} />
          {/if}
        {/snippet}

        {#if error}
          <div class="pad">
            <ErrorState
              {error}
              compact
              title="The recent failures could not be loaded"
              onretry={() => void load(others)}
            />
          </div>
        {:else if loading}
          <p class="sr-only">Loading recent failures.</p>
          <ul class="rows" aria-hidden="true">
            {#each [0, 1, 2] as row (row)}
              <li class="row">
                <div class="what"><Skeleton width="60%" height="var(--z-text-base)" /></div>
                <div class="elapsed"><Skeleton width="3rem" height="var(--z-text-xs)" /></div>
              </li>
            {/each}
          </ul>
        {:else}
          <ul class="rows" bind:this={list}>
            {#each jobs as job (job.id)}
              <li class="row">
                <button
                  class="what"
                  type="button"
                  aria-label="Inspect {readableJobText(job.job_name) || 'this failure'}"
                  onclick={() => open(job)}
                >
                  <span class="title">
                    <span class="repo mono">{job.repo ?? 'Unknown repository'}</span>
                    <span class="workflow">{job.workflow ?? 'Unknown workflow'}</span>
                  </span>
                  <span class="detail">
                    {#if readableJobText(job.job_name)}<span class="job-name"
                        >{readableJobText(job.job_name)}</span
                      >{/if}
                    {#if failureSummary(job)}
                      <span class="reason" class:danger={fleetFailed(job)}
                        >{failureSummary(job)}</span
                      >
                    {/if}
                  </span>
                  <span class="inspect">View failure details</span>
                </button>
                <p class="elapsed">
                  <span class="sr-only">Failed </span>
                  <RelativeTime value={job.completed_at ?? job.queued_at} />
                </p>
                <div class="act">
                  {#if session.can('operator') && !session.authDisabled && job.repo && job.github_run_id && eliCanRepairRun(job)}
                    <Button
                      size="sm"
                      href={targetHref({ repo: job.repo, run: job.github_run_id, job: job.id })}
                      >Fix with Eli</Button
                    >
                  {/if}
                  {#if canAcknowledge}
                    <IconButton
                      icon={Check}
                      label="Mark this failure as known"
                      help="Takes it off the failures needing attention without changing the failure."
                      size="sm"
                      disabled={acking === job.id}
                      onclick={() => void acknowledge(job)}
                    />
                  {/if}
                </div>
              </li>
            {/each}
          </ul>
          {#if total > jobs.length}
            <p class="more">
              <a href={restHref}>and {formatNumber(total - jobs.length)} more on the jobs page</a>
            </p>
          {/if}
        {/if}
      </Panel>
    </OverviewDisclosure>
  </div>
{/if}

<JobDrawer bind:open={drawerOpen} job={selected} onclose={() => (selected = null)} />

<style>
  .failure-attention :global(details) {
    border-left: var(--z-border-width-rail) solid var(--z-danger);
  }
  .failure-attention :global(.context) {
    width: auto;
  }
  .failure-attention :global(summary) {
    padding-block: 0;
    gap: var(--z-space-2);
  }
  .review-failures {
    display: inline-flex;
    align-items: center;
    min-height: var(--z-control-touch);
    color: var(--z-accent);
    font-size: var(--z-text-sm);
  }

  .pad {
    padding: var(--z-space-4) var(--z-space-5);
  }
  /* Nothing owed is one row of a card, in the grammar the panels around it share. */
  .clear {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    padding: var(--z-space-3) var(--z-space-5);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-xl);
    background: var(--z-surface);
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
  .clear p {
    margin: 0;
  }
  .clear a {
    color: var(--z-accent);
  }
  .rows {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(5rem, auto) auto;
    align-items: center;
    gap: var(--z-space-4);
    padding: var(--z-space-3) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  .row:last-child {
    border-bottom: 0;
  }
  .what {
    min-width: 0;
    padding: 0;
    text-align: left;
    cursor: pointer;
    border-radius: var(--z-radius-sm);
  }
  .inspect {
    display: block;
    margin-top: var(--z-space-1);
    color: var(--z-accent);
    font-size: var(--z-text-xs);
  }
  .what:hover .inspect {
    text-decoration: underline;
  }
  .title {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--z-space-1) var(--z-space-2);
    padding: 0;
    color: var(--z-text);
    text-align: left;
    cursor: pointer;
  }
  .what:hover .workflow,
  .what:hover .repo {
    color: var(--z-accent);
    text-decoration: underline;
  }
  .repo {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    overflow-wrap: anywhere;
  }
  .workflow {
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-medium);
    overflow-wrap: anywhere;
  }
  .detail {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--z-space-1) var(--z-space-2);
    margin: var(--z-space-1) 0 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-subtle);
  }
  .job-name {
    overflow-wrap: anywhere;
  }
  .reason {
    overflow-wrap: anywhere;
  }
  .reason.danger {
    color: var(--z-danger);
    font-weight: var(--z-weight-medium);
  }
  .elapsed {
    margin: 0;
    justify-self: end;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    white-space: nowrap;
  }
  .act {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    justify-self: end;
  }
  .more {
    margin: 0;
    padding: var(--z-space-3) var(--z-space-5);
    border-top: var(--z-border-width) solid var(--z-border);
    font-size: var(--z-text-xs);
  }
  .more a {
    color: var(--z-accent);
  }
  @media (max-width: 768px) {
    .row {
      grid-template-columns: minmax(0, 1fr) auto;
      gap: var(--z-space-1) var(--z-space-3);
    }
    .what {
      grid-column: 1 / -1;
    }
    .elapsed {
      justify-self: start;
    }
  }
</style>
