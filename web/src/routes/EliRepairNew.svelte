<!--
  The repair page: one failure, what is in the way of repairing it, and the one
  button that starts a repair.

  It is addressed by what it is about (a failed run, a pull request, or an offer)
  so a link to it keeps working, and it reads the run's failed jobs and readiness
  together on arrival. Reading never calls a model and never spends an attempt:
  the only thing here that does either is Fix with Eli, and the page never presses
  it for anybody. After a start it moves to the page of the repair it made.

  A repair already running for this failure is the process itself, so arriving
  on a failure that has one goes to it instead of offering to queue a second,
  which the controller would refuse anyway. It is found by the run the failure was
  reached by, and, once readiness has said which pull request and commit that is,
  by those: a repair queued for a pasted pull request records no run.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import {
    ApiError,
    actEliOpportunity,
    getEliAutomaticRepairs,
    getEliReadiness,
    listEliOpportunities,
    listEliRepairs,
    listJobs,
    requestEliRepair,
  } from '$lib/api/client';
  import type { Job, Schemas } from '$lib/api/types';
  import Button from '$lib/components/Button.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import SectionNav from '$lib/components/SectionNav.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import { router } from '$lib/router';
  import { session } from '$lib/state/session.svelte';
  import { toasts } from '$lib/state/toasts.svelte';
  import { automaticNote, findAutomaticAttempt } from '$lib/repairs/automatic';
  import { caseFromLive } from '$lib/repairs/case';
  import { blockingRow, checkRows } from '$lib/repairs/checks';
  import { runningRepair } from '$lib/repairs/journey';
  import { changedBlocker, parseTarget, stageOf, targetHref } from '$lib/repairs/workspace';
  import AutomaticNote from '$lib/repairs/AutomaticNote.svelte';
  import CaseCard from '$lib/repairs/CaseCard.svelte';
  import DecisionCard from '$lib/repairs/DecisionCard.svelte';
  import FailedJobs from '$lib/repairs/FailedJobs.svelte';
  import NoticeCard from '$lib/repairs/NoticeCard.svelte';
  import PullRequestBar from '$lib/repairs/PullRequestBar.svelte';
  import ReadinessChecklist from '$lib/repairs/ReadinessChecklist.svelte';
  import RepairSetup from '$lib/repairs/RepairSetup.svelte';
  import RepairTargetDialog from '$lib/repairs/RepairTargetDialog.svelte';

  const target = $derived(parseTarget(router.query));
  /** What the page is about once an offer has said so: an offer names its own failure. */
  let about = $state<{ repo: string; pull: number; run: number } | null>(null);
  let jobs = $state<Job[]>([]);
  let readiness = $state<Schemas['EliReadiness'] | null>(null);
  /** Why readiness could not be read, when it could not. */
  let readFailed = $state('');
  let offerGone = $state(false);
  /** The first read has finished, whatever it found. */
  let loaded = $state(false);
  let busy = $state(false);
  let starting = $state(false);
  let startError = $state('');
  let instruction = $state('');
  let dialogOpen = $state(false);
  let setupOpen = $state(false);
  let token = 0;
  /**
   * What Eli did on its own for this repository, read only for an administrator and
   * only when the repository repairs itself: automatic repairs have no owner, so
   * nobody else can read them, and a repository that does not repair itself has none.
   */
  let automaticAttempts = $state<Schemas['EliRepair'][]>([]);
  let automaticRow = $state<Schemas['EliAutomaticRepository'] | undefined>(undefined);

  const hasTarget = $derived(Boolean(target.offer || (target.repo && (target.pull || target.run))));
  const view = $derived(
    caseFromLive({
      repo: about?.repo ?? target.repo,
      jobs,
      readiness,
      openedJobId: target.job,
      run: about?.run || target.run || undefined,
    }),
  );
  const rows = $derived(readiness ? checkRows(readiness, view.repoUrl) : []);
  const stage = $derived(stageOf(readiness, Boolean(readFailed)));
  const blocking = $derived(blockingRow(rows));
  const changed = $derived(readiness ? changedBlocker(readiness) : undefined);
  const setupNeeded = $derived(blocking?.action?.kind === 'setup');
  const note = $derived(
    readiness && (stage === 'ready' || stage === 'blocked')
      ? automaticNote({
          mode: readiness.policy_mode,
          admin: session.can('admin'),
          attempt: findAutomaticAttempt(automaticAttempts, {
            repo: readiness.repo,
            pull: readiness.pull_number,
            head: readiness.head_sha,
          }),
          repository: automaticRow,
        })
      : null,
  );
  // The log is read for somebody whose GitHub account is known to write to the
  // repository, which is what the access check found out.
  const canReadLogs = $derived(
    readiness !== null &&
      (rows.find((row) => row.code === 'access')?.status === 'ok' ||
        (rows.length === 0 && readiness.ready)),
  );
  const blockedChecks = $derived(rows.filter((row) => row.status === 'blocked').length);
  const navItems = $derived([
    { id: 'next', label: 'Next step' },
    ...(loaded && (setupNeeded || setupOpen)
      ? [{ id: 'repair-setup', label: 'Setup', secondary: true }]
      : []),
    { id: 'failed', label: 'What failed', count: view.jobs.length },
    ...(blockedChecks > 0 ? [{ id: 'checks', label: 'Checks', count: blockedChecks }] : []),
  ]);

  const spoken = $derived.by(() => {
    if (!loaded) return 'Checking repair readiness.';
    if (offerGone) return 'This offer is no longer available.';
    switch (stage) {
      case 'checking':
        return 'Checking repair readiness.';
      case 'error':
        return readFailed;
      case 'changed':
        return changed?.message ?? 'This failure has changed.';
      case 'ready':
        return 'Ready. Review the repair, then press Fix with Eli.';
      default:
        return blocking ? `${blocking.label}: ${blocking.message}` : 'Eli cannot act on this yet.';
    }
  });

  /**
   * The repository's repairs that are under way, read once when the page is
   * entered, so a failure that already has one is followed rather than offered
   * again. Only running ones are asked for: a long history could push the one in
   * hand off the page. It is a courtesy: without it readiness still answers.
   */
  async function runningRepairs(repo: string): Promise<Schemas['EliRepair'][]> {
    try {
      return (await listEliRepairs({ repo, group: 'active', limit: 20 })).items;
    } catch {
      return [];
    }
  }

  function follow(repair: Schemas['EliRepair']): void {
    router.navigate(`/repairs/${encodeURIComponent(repair.id)}`, { replace: true });
  }

  async function load(arrival = false): Promise<void> {
    const mine = ++token;
    busy = true;
    readFailed = '';
    try {
      let { repo, pull, run } = target;
      if (target.offer) {
        const offers = await listEliOpportunities();
        if (mine !== token) return;
        const offer = offers.items.find((item) => item.id === target.offer);
        offerGone = !offer;
        if (!offer) {
          about = null;
          jobs = [];
          readiness = null;
          return;
        }
        repo = offer.repo;
        pull = offer.pull_number;
        run = offer.run_id;
      }
      if (!repo || (pull < 1 && run < 1)) {
        about = null;
        return;
      }
      about = { repo, pull, run };
      const rows = arrival ? await runningRepairs(repo) : [];
      if (mine !== token) return;
      const active = runningRepair(rows, { repo, pull, run });
      if (active) {
        follow(active);
        return;
      }
      const [found, answered] = await Promise.allSettled([
        run > 0 ? listJobs({ repo: [repo], run_id: [run], failed: true }) : Promise.resolve(null),
        getEliReadiness(pull > 0 ? { repo, pull_number: pull } : { repo, run_id: run }),
      ]);
      if (mine !== token) return;
      // The jobs are what makes the failure legible, not what makes the page
      // work: a run Zoomies has pruned, or a read that failed, leaves the case
      // with less in it and says so.
      jobs = found.status === 'fulfilled' && found.value ? (found.value.items ?? []) : [];
      if (answered.status === 'fulfilled') {
        readiness = answered.value;
        // A run is only a run until readiness has said which pull request and
        // commit it is, and those are what a repair queued for a pasted pull
        // request is known by: it records no run, so the look by run above cannot
        // have found it.
        const queued = answered.value.ready
          ? runningRepair(rows, {
              repo: answered.value.repo,
              pull: answered.value.pull_number,
              head: answered.value.head_sha,
            })
          : undefined;
        if (queued) {
          follow(queued);
          return;
        }
        automaticAttempts = [];
        automaticRow = undefined;
        // A failure that has been fixed, closed or replaced shows no note, so reads for
        // one would be wasted.
        if (
          answered.value.policy_mode === 'automatic' &&
          session.can('admin') &&
          !changedBlocker(answered.value)
        ) {
          const [made, overview] = await Promise.allSettled([
            listEliRepairs({ repo: answered.value.repo, trigger: 'automatic', limit: 100 }),
            getEliAutomaticRepairs(),
          ]);
          if (mine !== token) return;
          // Both are courtesies: without them the note says what it can without them.
          automaticAttempts = made.status === 'fulfilled' ? made.value.items : [];
          automaticRow =
            overview.status === 'fulfilled'
              ? overview.value.repositories.find(
                  (row) => row.repo.toLowerCase() === answered.value.repo.toLowerCase(),
                )
              : undefined;
        }
      } else {
        readiness = null;
        readFailed =
          answered.reason instanceof ApiError
            ? answered.reason.message
            : 'Could not check repair readiness. Check that the controller is running, then try again.';
      }
    } finally {
      if (mine === token) {
        busy = false;
        loaded = true;
      }
    }
  }

  async function start(): Promise<void> {
    if (!readiness?.ready || starting) return;
    starting = true;
    startError = '';
    try {
      let row: Schemas['EliRepair'] | null = null;
      if (target.offer) {
        row = (await actEliOpportunity(target.offer, { action: 'accept' })).repair ?? null;
      } else {
        row = await requestEliRepair({
          repo: readiness.repo,
          pull_number: readiness.pull_number,
          head_sha: readiness.head_sha,
          instruction,
          ...(about?.run ? { run_id: about.run } : {}),
        });
      }
      toasts.success('Repair queued');
      router.navigate(row?.id ? `/repairs/${encodeURIComponent(row.id)}` : '/repairs');
    } catch (cause) {
      if (cause instanceof ApiError && cause.isConflict) {
        // The target moved after it was reviewed. Read it again so the page says
        // what became of it, instead of leaving a stale offer on screen.
        starting = false;
        await load();
        if (!changed) startError = cause.message;
      } else {
        startError =
          cause instanceof ApiError
            ? cause.message
            : 'Could not start the repair. Check readiness again.';
      }
    } finally {
      starting = false;
    }
  }

  /**
   * A failure replaced by a newer commit is asked about again as the pull request
   * it belongs to, in place of the stale run, so the answer can change.
   */
  function checkCurrent(): void {
    if (!readiness?.pull_number) return;
    router.navigate(targetHref({ repo: readiness.repo, pull: readiness.pull_number }), {
      replace: true,
    });
  }

  function openSetup(): void {
    setupOpen = true;
    queueMicrotask(() =>
      document.getElementById('repair-setup')?.scrollIntoView({ block: 'start' }),
    );
  }

  // Read when the page opens and whenever the address names something else. The
  // job the person came from is only a mark on the page, so changing it reads
  // nothing again.
  let read = '';
  $effect(() => {
    const key = `${target.repo}|${target.pull}|${target.run}|${target.offer}`;
    if (key === read) return;
    read = key;
    untrack(() => {
      jobs = [];
      readiness = null;
      automaticAttempts = [];
      automaticRow = undefined;
      readFailed = '';
      startError = '';
      instruction = '';
      setupOpen = false;
      offerGone = false;
      loaded = false;
      if (!hasTarget) {
        about = null;
        loaded = true;
        return;
      }
      void load(true);
    });
  });

  let workspace = $state<HTMLElement | null>(null);
  let decision = $state<HTMLElement | null>(null);
  // Each answer replaces what the page says, and the button that was pressed can go
  // with it: Check again leaves when the answer is that nothing is in the way.
  // Keyboard focus went with it, to the top of the document, and whoever was working
  // by keyboard had to find the card again. When focus was in the workspace and the
  // element that held it is gone, it follows to the heading of the card that says
  // what to do next. An answer that arrives while focus is elsewhere, or leaves the
  // element where it was, touches nothing.
  let heldFocus = false;
  $effect.pre(() => {
    void readiness;
    heldFocus = workspace?.contains(document.activeElement) ?? false;
  });
  $effect(() => {
    void readiness;
    if (!heldFocus || !workspace || workspace.contains(document.activeElement)) return;
    decision?.querySelector<HTMLElement>('h2')?.focus();
  });

  $effect(() => {
    if (!loaded || !hasTarget || offerGone || !view.title) return;
    const title = view.title.length > 60 ? `${view.title.slice(0, 59)}…` : view.title;
    router.setTitle(`Repair: ${title}`);
  });
</script>

<PageHeader
  title="Repair a failed check"
  subtitle="Eli reads the failed checks, pushes one commit to the branch, then watches CI. You review it like any other commit."
  breadcrumb={[{ label: 'Eli repairs', href: '/repairs' }, { label: 'New repair' }]}
  onrefresh={() => load()}
>
  <Button variant="secondary" onclick={() => (dialogOpen = true)}
    >Choose another pull request</Button
  >
</PageHeader>

<output class="sr-only" aria-live="polite">{spoken}</output>

{#if offerGone}
  <NoticeCard heading="This offer is no longer available">
    It may have been dismissed, accepted already, or replaced because the pull request has a newer
    commit. Offers that still need you are on the Eli repairs page.
    {#snippet action()}
      <Button href="/repairs">Back to Eli repairs</Button>
    {/snippet}
  </NoticeCard>
{:else if !hasTarget}
  <NoticeCard heading="Which pull request should Eli repair?">
    Paste the address of a pull request, or open a failed run from the Overview and choose Fix with
    Eli.
    {#snippet action()}
      <PullRequestBar bare />
    {/snippet}
  </NoticeCard>
{:else}
  <SectionNav items={navItems} label="On this page" />
  <div class="workspace" bind:this={workspace}>
    <div class="case">
      {#if loaded}
        <CaseCard {view} />
      {:else}
        <Skeleton height="11rem" radius="md" />
      {/if}
    </div>
    <aside class="decision" id="next" aria-label="Next step" tabindex="-1" bind:this={decision}>
      <DecisionCard
        {stage}
        {readiness}
        {rows}
        busy={busy || starting}
        bind:instruction
        allowInstruction={!target.offer}
        onstart={() => void start()}
        onrecheck={() => void load()}
        onsetup={openSetup}
        onchoose={() => (dialogOpen = true)}
        oncurrent={readiness?.pull_number ? checkCurrent : undefined}
        runUrl={view.runUrl}
        error={readFailed || startError}
      />
      {#if loaded && note}<AutomaticNote {note} />{/if}
    </aside>
    {#if loaded}
      {#if readiness && (setupNeeded || setupOpen) && !changed}
        <div class="setup" id="repair-setup" tabindex="-1">
          {#key readiness.repo}
            <RepairSetup repo={readiness.repo} oncomplete={() => load()} />
          {/key}
        </div>
      {/if}
      <div class="failed" id="failed" tabindex="-1">
        <FailedJobs
          jobs={view.jobs}
          repo={view.repo}
          description={view.jobsFromGitHub
            ? 'Read from GitHub just now, for a run this fleet did not run, so it has no record of its own to blame or place a runner on.'
            : 'From GitHub’s report, as Zoomies recorded it.'}
          {canReadLogs}
          logsBlockedReason="Job logs can hold anything a workflow prints, so Zoomies reads them only for someone whose GitHub account can write to this repository. Fix what is blocking the checks and the error lines appear here."
        />
      </div>
      {#if rows.length && blockedChecks > 0}
        <div class="checks" id="checks" tabindex="-1">
          <ReadinessChecklist
            {rows}
            {busy}
            onrecheck={() => void load()}
            onsetup={openSetup}
            onchoose={() => (dialogOpen = true)}
          />
        </div>
      {/if}
    {/if}
  </div>
{/if}

<RepairTargetDialog bind:open={dialogOpen} />

<style>
  .workspace {
    display: grid;
    gap: var(--z-space-5);
    min-width: 0;
  }
  @media (min-width: 1180px) {
    .workspace {
      grid-template-columns: minmax(0, 1fr) 24rem;
      align-items: start;
    }
    .case {
      grid-column: 1;
      grid-row: 1;
    }
    .decision {
      grid-column: 2;
      grid-row: 1;
      position: sticky;
      top: var(--z-space-6);
    }
    .setup,
    .failed,
    .checks {
      grid-column: 1 / -1;
    }
  }
  .case,
  .decision,
  .failed,
  .checks,
  .setup {
    min-width: 0;
    scroll-margin-top: calc(var(--z-topbar-height) + var(--z-control-touch) + var(--z-space-3));
  }
  .decision:focus,
  .failed:focus,
  .checks:focus,
  .setup:focus {
    outline: none;
  }
</style>
