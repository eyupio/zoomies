<!--
  One repair, followed from the moment it was queued to the sentence that says
  how it ended.

  While Eli is working the page asks again every few seconds, and stops the moment
  the repair reaches a state it will not leave. It does not ask while its tab is
  hidden (a forgotten background tab would otherwise read the repair every four
  seconds for as long as it was open) and asks at once when the tab comes back.
  Leaving the page ends all of it.

  What the repair was about comes from the case it kept when it began, because
  Zoomies' own records of the jobs are pruned and a pull request's title changes.
  Nothing here calls GitHub or a model: the error output of a failed job is the
  one thing read on demand, when the person opens that job.
-->
<script lang="ts">
  import { ExternalLink } from '@lucide/svelte';
  import { tick, untrack } from 'svelte';
  import { ApiError, getEliAutomaticRepairs, getEliRepair } from '$lib/api/client';
  import type { Schemas } from '$lib/api/types';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import Disclosure from '$lib/components/Disclosure.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import SectionNav from '$lib/components/SectionNav.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import { pluralise, shortId } from '$lib/format';
  import { router } from '$lib/router';
  import { session } from '$lib/state/session.svelte';
  import { repairStatus } from '$lib/status';
  import { requestedBy } from '$lib/repairs/attempt';
  import type { StopChoice } from '$lib/repairs/automatic';
  import { caseFromSnapshot } from '$lib/repairs/case';
  import { isActive } from '$lib/repairs/history';
  import { REPAIR_TRACK, trackIndex } from '$lib/repairs/journey';
  import { safeHttpUrl } from '$lib/repairs/links';
  import { stopAutomatic } from '$lib/repairs/stop';
  import AttemptFacts from '$lib/repairs/AttemptFacts.svelte';
  import AttemptTimeline from '$lib/repairs/AttemptTimeline.svelte';
  import AutomaticCard from '$lib/repairs/AutomaticCard.svelte';
  import CaseCard from '$lib/repairs/CaseCard.svelte';
  import FailedJobs from '$lib/repairs/FailedJobs.svelte';
  import NextSteps from '$lib/repairs/NextSteps.svelte';
  import NoticeCard from '$lib/repairs/NoticeCard.svelte';
  import OutcomeBanner from '$lib/repairs/OutcomeBanner.svelte';
  import ProgressTrack from '$lib/repairs/ProgressTrack.svelte';
  import RepairStatusBadge from '$lib/repairs/RepairStatusBadge.svelte';
  import StopAutomaticDialog from '$lib/repairs/StopAutomaticDialog.svelte';
  import TechnicalDetails from '$lib/repairs/TechnicalDetails.svelte';

  /** How often a repair that is still going is read again. */
  const POLL_MS = 4000;

  const id = $derived(router.params.id ?? '');

  let repair = $state<Schemas['EliRepair'] | null>(null);
  let phase = $state<'loading' | 'ready' | 'missing' | 'failed'>('loading');
  let failure = $state<unknown>(null);
  /** A read after the first one failed, so what is on screen may be out of date. */
  let refreshFailed = $state(false);
  let now = $state(Date.now());
  let token = 0;
  let inFlight: AbortController | null = null;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const active = $derived(repair ? isActive(repair.state) : false);
  const view = $derived(repair ? caseFromSnapshot(repair) : null);
  const pullLink = $derived(safeHttpUrl(view?.pullUrl));
  const runLink = $derived(safeHttpUrl(view?.runUrl));
  const navItems = $derived([
    { id: 'summary', label: 'Summary' },
    { id: 'what-failed', label: 'What failed', count: view?.jobs.length ?? 0 },
    { id: 'details', label: 'Details' },
  ]);
  const caseSummary = $derived.by(() => {
    if (!view) return '';
    const pull = view.pullNumber ? `${view.repo} #${view.pullNumber}` : view.repo;
    return `${pull} · ${pluralise(view.jobs.length, 'failed job')}`;
  });

  // An automatic repair has no owner, so only an administrator can open one, and only
  // an administrator can read the policy that started it.
  const automatic = $derived(repair?.trigger === 'automatic' && session.can('admin'));
  let overview = $state<Schemas['EliAutomaticRepository'][] | null>(null);
  let overviewFailed = $state(false);
  let overviewFor = '';
  let card = $state<AutomaticCard | null>(null);
  let stopOpen = $state(false);
  let stopBusy = $state(false);
  let stopError = $state('');
  const repository = $derived(
    repair
      ? overview?.find((row) => row.repo.toLowerCase() === repair!.repo.toLowerCase())
      : undefined,
  );

  const spoken = $derived.by(() => {
    if (phase === 'missing') return 'This repair is not in your history.';
    if (phase === 'failed') return 'Could not load this repair.';
    if (!repair) return 'Loading the repair.';
    if (active) return `${REPAIR_TRACK[trackIndex(repair)]!.label}.`;
    return `Repair finished. ${repairStatus(repair.state).label}. ${repair.message}`;
  });

  function schedule(): void {
    clearTimeout(timer);
    timer = undefined;
    if (!repair || !isActive(repair.state)) return;
    timer = setTimeout(() => {
      // A hidden tab does not read; the next visible moment does.
      if (document.visibilityState === 'visible') void load();
      else schedule();
    }, POLL_MS);
  }

  async function load(): Promise<void> {
    const wanted = id;
    if (!wanted) return;
    const mine = ++token;
    inFlight?.abort();
    const controller = new AbortController();
    inFlight = controller;
    try {
      const row = await getEliRepair(wanted, controller.signal);
      if (mine !== token) return;
      repair = row;
      phase = 'ready';
      refreshFailed = false;
    } catch (cause) {
      if (mine !== token || controller.signal.aborted) return;
      if (repair) {
        refreshFailed = true;
      } else if (cause instanceof ApiError && cause.isNotFound) {
        phase = 'missing';
      } else {
        phase = 'failed';
        failure = cause;
      }
    } finally {
      if (mine === token) schedule();
    }
  }

  async function readOverview(): Promise<void> {
    const mine = overviewFor;
    try {
      const answer = await getEliAutomaticRepairs();
      if (mine !== overviewFor) return;
      overview = answer.repositories;
      overviewFailed = false;
    } catch {
      if (mine === overviewFor) overviewFailed = true;
    }
  }

  async function stopHere(then: StopChoice): Promise<void> {
    if (!repair || stopBusy) return;
    stopBusy = true;
    stopError = '';
    const result = await stopAutomatic([repair.repo], then);
    stopBusy = false;
    if (!result.done) {
      stopError = result.error;
      return;
    }
    stopOpen = false;
    await readOverview();
    await tick();
    // The button that was pressed left the card with the state it belonged to.
    if (!document.activeElement || document.activeElement === document.body) card?.focus();
  }

  function stop(): void {
    token++;
    inFlight?.abort();
    clearTimeout(timer);
    timer = undefined;
  }

  // Read when the page opens and whenever the address names another repair.
  let reading = '';
  $effect(() => {
    const next = id;
    if (next === reading) return;
    reading = next;
    untrack(() => {
      stop();
      repair = null;
      phase = 'loading';
      failure = null;
      refreshFailed = false;
      if (next) void load();
    });
  });

  // Leaving the page ends the polling and any read in flight.
  $effect(() => stop);

  // The policy is read once for each automatic repair the page shows, and not at all
  // for any other.
  $effect(() => {
    const wanted = automatic && repair ? repair.id : '';
    if (wanted === overviewFor) return;
    overviewFor = wanted;
    untrack(() => {
      overview = null;
      overviewFailed = false;
      if (wanted) void readOverview();
    });
  });

  $effect(() => {
    const onVisible = () => {
      if (document.visibilityState === 'visible' && repair && isActive(repair.state)) void load();
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => document.removeEventListener('visibilitychange', onVisible);
  });

  // The elapsed time runs on while the repair does.
  $effect(() => {
    if (!active) return;
    now = Date.now();
    const tick = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(tick);
  });

  $effect(() => {
    if (!view?.title) return;
    const title = view.title.length > 60 ? `${view.title.slice(0, 59)}…` : view.title;
    router.setTitle(`Repair: ${title}`);
  });

  const crumbs = $derived.by(() => {
    const trail: { label: string; href?: string }[] = [{ label: 'Eli repairs', href: '/repairs' }];
    if (repair && view) {
      trail.push({ label: view.pullNumber ? `${view.repo} #${view.pullNumber}` : view.repo });
      trail.push({ label: `Attempt ${shortId(repair.id)}` });
    } else {
      trail.push({ label: 'Repair' });
    }
    return trail;
  });
</script>

<PageHeader
  title={view?.title ?? 'Repair'}
  breadcrumb={crumbs}
  onrefresh={phase === 'missing' ? undefined : () => load()}
>
  {#snippet meta()}
    {#if repair}
      <RepairStatusBadge state={repair.state} />
      <Badge tone="neutral" label={requestedBy(repair)} dot={false} />
      <span class="started"><RelativeTime value={repair.created_at} prefix="Started" /></span>
    {/if}
  {/snippet}
  {#if pullLink}
    <Button variant="secondary" href={pullLink} newTab iconAfter={ExternalLink}>Open PR</Button>
  {/if}
  {#if runLink}
    <Button variant="secondary" href={runLink} newTab iconAfter={ExternalLink}>Open run</Button>
  {/if}
</PageHeader>

<output class="sr-only" aria-live="polite">{spoken}</output>

{#if phase === 'missing'}
  <NoticeCard heading="This repair is not in your history">
    It may have been made by someone else, or the address may be wrong. Repairs you can open are
    listed on the Eli repairs page.
    {#snippet action()}
      <Button href="/repairs">Back to Eli repairs</Button>
    {/snippet}
  </NoticeCard>
{:else if phase === 'failed'}
  <ErrorState error={failure} title="Could not load this repair" onretry={() => void load()} />
{:else if !repair || !view}
  <div class="loading">
    <Skeleton height="9rem" radius="md" />
    <Skeleton height="11rem" radius="md" />
  </div>
{:else}
  <SectionNav items={navItems} label="On this page" />
  <div class="cols">
    <div class="main">
      <div class="summary" id="summary" tabindex="-1">
        {#if refreshFailed}
          <p class="stale" role="status">
            Could not refresh just now. This is the last answer Zoomies had, and it will try again
            shortly.
          </p>
        {/if}
        {#if active}
          <ProgressTrack {repair} {now} />
        {:else}
          <OutcomeBanner {repair} />
          <NextSteps {repair} {view} />
        {/if}
      </div>
      <Disclosure id="what-failed" title="What Eli was asked to fix" summary={caseSummary}>
        <div class="case">
          <CaseCard
            {view}
            failedLabel={active ? 'What Eli is fixing' : 'What Eli was asked to fix'}
            pullNote="Pull request details were not kept"
          />
        </div>
        <FailedJobs
          jobs={view.jobs}
          repo={view.repo}
          canReadLogs
          description="As they were when the repair began."
          none="This repair did not keep a record of the jobs it was asked to fix, so their names and steps are not shown."
          bare
        />
      </Disclosure>
      <div class="details" id="details" tabindex="-1">
        <TechnicalDetails {repair} />
      </div>
    </div>
    <aside class="side" aria-label="This attempt">
      <AttemptFacts {repair} {view} />
      {#if automatic}
        <AutomaticCard
          bind:this={card}
          repo={repair.repo}
          {repository}
          loaded={overview !== null}
          failed={overviewFailed}
          unpushed={repair.state === 'queued' || repair.state === 'working'}
          onstop={() => {
            stopError = '';
            stopOpen = true;
          }}
          onretry={() => {
            overviewFailed = false;
            void readOverview();
          }}
        />
      {/if}
      {#if !active}
        <details class="timeline-fold">
          <summary>Timeline</summary>
          <AttemptTimeline {repair} bare />
        </details>
      {/if}
    </aside>
  </div>
  <StopAutomaticDialog
    bind:open={stopOpen}
    repos={[repair.repo]}
    busy={stopBusy}
    error={stopError}
    onconfirm={(then) => void stopHere(then)}
  />
{/if}

<style>
  .started {
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
  }
  .loading {
    display: grid;
    gap: var(--z-space-4);
  }
  .cols {
    display: grid;
    gap: var(--z-space-5);
    grid-template-columns: minmax(0, 1fr);
  }
  .main,
  .side {
    display: grid;
    gap: var(--z-space-5);
    align-content: start;
    min-width: 0;
  }
  .summary,
  .details,
  .case {
    display: grid;
    gap: var(--z-space-5);
    min-width: 0;
    scroll-margin-top: calc(var(--z-topbar-height) + var(--z-control-touch) + var(--z-space-3));
  }
  .summary:focus,
  .details:focus {
    outline: none;
  }
  .timeline-fold > summary {
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
    cursor: pointer;
  }
  .stale {
    margin: 0;
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-pending-border);
    border-radius: var(--z-radius-md);
    background: var(--z-pending-subtle);
    font-size: var(--z-text-sm);
  }
  @media (min-width: 1180px) {
    .cols {
      grid-template-columns: minmax(0, 1fr) 22rem;
      align-items: start;
    }
    .side {
      position: sticky;
      top: var(--z-space-6);
    }
  }
</style>
