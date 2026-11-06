<!--
  The hub: what needs the person, what is running, and what happened.

  The section navigation is the one summary: it counts what needs the person,
  what is running and what is in the history, and the sections below keep their
  latest sentence when closed. A setup line appears only when something is
  missing, so a person who can repair goes straight to the work. A section that
  holds work opens; a quiet section says so in one line.

  It reads on a ten-second timer, as the history does, and reads the offers at
  most once a minute: they rarely change, and the check behind them asks GitHub
  about the person's access. The Refresh button is the one time somebody is asking
  right now, so it reads them at once.
-->
<script lang="ts">
  import { CircleCheck, GitPullRequest, Settings2 } from '@lucide/svelte';
  import { onMount, tick } from 'svelte';
  import {
    ApiError,
    actEliOpportunity,
    getEliAutomaticRepairs,
    getEliRepairSettings,
    listEliOpportunities,
    listEliRepairs,
    listEliWatchRepositories,
    listEliWatches,
    listJobs,
  } from '$lib/api/client';
  import type { Schemas } from '$lib/api/types';
  import Button from '$lib/components/Button.svelte';
  import ConfirmDialog from '$lib/components/ConfirmDialog.svelte';
  import Disclosure from '$lib/components/Disclosure.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import SectionNav from '$lib/components/SectionNav.svelte';
  import { formatNumber, pluralise, relativeTime } from '$lib/format';
  import { router } from '$lib/router';
  import { session } from '$lib/state/session.svelte';
  import { toasts } from '$lib/state/toasts.svelte';
  import type { StopChoice } from '$lib/repairs/automatic';
  import type { CaseView } from '$lib/repairs/case';
  import type { RepairHistorySummary } from '$lib/repairs/history';
  import { RUNNING_LIMIT, offerCases, setupSummary } from '$lib/repairs/hub';
  import { stopAutomatic } from '$lib/repairs/stop';
  import {
    pauseRepository,
    pauseWatch,
    resumeRepository,
    resumeWatch,
    stopAllWatches,
    stopWatch,
  } from '$lib/repairs/watchActions';
  import {
    pauseWatchCopy,
    stopAllCopy,
    stopWatchCopy,
    summariseWatch,
    watchingState,
    type RepositoryWatch,
    type Watch,
    type WatchControlCopy,
  } from '$lib/repairs/watches';
  import { parseTarget, targetHref } from '$lib/repairs/workspace';
  import ActiveRepairs from '$lib/repairs/ActiveRepairs.svelte';
  import AutomaticRepairs from '$lib/repairs/AutomaticRepairs.svelte';
  import OfferList from '$lib/repairs/OfferList.svelte';
  import PullRequestBar from '$lib/repairs/PullRequestBar.svelte';
  import RepairHistory from '$lib/repairs/RepairHistory.svelte';
  import RepairRow from '$lib/repairs/RepairRow.svelte';
  import RepairTargetDialog from '$lib/repairs/RepairTargetDialog.svelte';
  import SetupBar from '$lib/repairs/SetupBar.svelte';
  import StopAutomaticDialog from '$lib/repairs/StopAutomaticDialog.svelte';
  import WatchedRepositories from '$lib/repairs/WatchedRepositories.svelte';

  /** How often the hub reads again, and how long the offers are left between reads. */
  const POLL_MS = 10_000;
  const OFFERS_EVERY_MS = 60_000;

  // A failure used to be repaired on this page, and every bookmark, document and
  // link in the problems drawer still says so. The repair has a page of its own
  // now: the same address, one segment longer, replacing this entry so Back does
  // not land on a page that only forwards.
  const legacy = parseTarget(router.query);
  const forwarding = Boolean(legacy.repo && (legacy.pull || legacy.run));

  let settings = $state<Schemas['EliRepairSettings'] | null>(null);
  let offers = $state<Schemas['EliOpportunity'][]>([]);
  let cases = $state<Record<string, CaseView>>({});
  let offersLoaded = $state(false);
  let running = $state<Schemas['EliRepair'][]>([]);
  let runningTotal = $state(0);
  let attention = $state<Schemas['EliRepair'][]>([]);
  let attentionTotal = $state(0);
  let attentionLoaded = $state(false);
  let attentionFailed = $state(false);
  let error = $state('');
  /** The repositories that repair themselves, for an administrator; empty for everyone else. */
  let automatic = $state<Schemas['EliAutomaticRepository'][]>([]);
  /** The last read of them failed, so `automatic` is what was read before, if anything was. */
  let automaticFailed = $state(false);
  let stopping = $state<string[]>([]);
  let stopOpen = $state(false);
  let stopBusy = $state(false);
  let stopError = $state('');
  let dismissing = $state(false);
  let dialogOpen = $state(false);
  /** The watches and the repository decisions behind the Watching section, for an administrator. */
  let decisions = $state<RepositoryWatch[]>([]);
  let watches = $state<Watch[]>([]);
  let watchLoaded = $state(false);
  let watchFailed = $state(false);
  let watchError = $state('');
  let watchBusy = $state<string[]>([]);
  /** The control waiting on a confirmation, with the promise it made. */
  let confirming = $state<{
    copy: WatchControlCopy;
    run: () => Promise<string>;
  } | null>(null);
  let confirmBusy = $state(false);
  let confirmError = $state('');
  let confirmOpen = $state(false);

  let history = $state<RepairHistory | null>(null);
  /** What the closed History section says about the list it last read. */
  let historySummary = $state<RepairHistorySummary>({
    loaded: false,
    error: '',
    total: 0,
    filtered: false,
  });
  let offersReadAt = 0;
  /** The offers the cases were last read for, so an unchanged set is not read again. */
  let casesFor = '';

  // Offers are for a signed-in person, and with sign-in off there is nobody to offer to.
  const offering = $derived(!session.authDisabled);
  // What automatic repairs have done is an administrator's to read: they have no owner.
  const administrator = $derived(session.can('admin'));
  // The panel shows when something is automatic, and also when the settings say so and the
  // read has not yet worked, because a failure to look is the one thing it must not hide.
  const showAutomatic = $derived(
    automatic.length > 0 ||
      (automaticFailed && Boolean(settings?.policies.some((p) => p.enabled && p.automatic))),
  );
  const automaticProblem = $derived(automatic.some((repo) => Boolean(repo.problem)));
  const automaticWords = $derived.by(() => {
    if (automaticFailed) return 'Could not read automatic repairs';
    if (automaticProblem) {
      const problems = automatic.filter((repo) => repo.problem).length;
      return `${problems} of ${automatic.length} ${
        automatic.length === 1 ? 'repository' : 'repositories'
      } cannot repair`;
    }
    return pluralise(automatic.length, 'repository', 'repositories');
  });
  const historyWords = $derived.by(() => {
    if (!historySummary.loaded) return historySummary.error ? 'Could not read' : 'Loading history';
    if (historySummary.error) return 'Could not read the history';
    if (historySummary.total === 0)
      return historySummary.filtered ? 'No matching repairs' : 'No repairs';
    const latest = historySummary.latest ? ` · latest ${relativeTime(historySummary.latest)}` : '';
    return `${formatNumber(historySummary.total)} ${
      historySummary.total === 1 ? 'repair' : 'repairs'
    }${historySummary.filtered ? ' matching' : ''}${latest}`;
  });
  const setup = $derived(
    setupSummary({
      settings,
      userId: session.identity?.id,
      admin: administrator,
      authDisabled: session.authDisabled,
    }),
  );
  const watching = $derived(
    watchingState({
      loaded: watchLoaded,
      failed: watchFailed,
      repositories: decisions.length,
      watches: watches.length,
    }),
  );
  const watchCount = $derived(watches.filter((watch) => watch.enabled).length);
  /** Something a person has to look at, so the section opens and the nav says so. */
  const watchNeedsPerson = $derived(
    watches.some((watch) => watch.enabled && summariseWatch(watch).attention.length > 0),
  );
  // A section appears when there is something in it, and also when the read
  // failed: an administrator who has watches and cannot read them must not be
  // shown a hub that looks as if nothing is being followed.
  const showWatching = $derived(watching === 'working' || watching === 'failed');
  const watchingWords = $derived.by(() => {
    if (watching === 'failed') return 'Could not read what Eli is watching';
    if (watching === 'loading') return 'Reading what Eli is watching';
    if (watching === 'quiet') return 'Nothing being watched';
    return `${pluralise(watchCount, 'pull request')} watched in ${pluralise(
      new Set(watches.map((watch) => watch.repo.toLowerCase())).size,
      'repository',
      'repositories',
    )}`;
  });

  // Setup is a task, not a status: the bar appears only when something is
  // actually in the way. Once a person can repair, the header's Repair settings
  // link is the way back to the choices.
  const showSetup = $derived(setup.state === 'needed' || setup.state === 'off');
  const navItems = $derived([
    { id: 'needs', label: 'Needs you', count: offers.length + attentionTotal },
    { id: 'running', label: 'In progress', count: running.length },
    { id: 'history', label: 'History', secondary: true },
    ...(administrator && showWatching
      ? [{ id: 'watching', label: 'Watching', count: watchCount, secondary: true }]
      : []),
    ...(administrator && showAutomatic
      ? [{ id: 'automatic', label: 'Automatic', count: automatic.length, secondary: true }]
      : []),
  ]);

  const failure = (cause: unknown): string =>
    cause instanceof ApiError ? cause.message : 'Could not read PR repairs.';

  /** The failed jobs of every offer's run, read in one request and sorted out by offer. */
  async function readCases(list: Schemas['EliOpportunity'][]): Promise<void> {
    const key = list.map((offer) => `${offer.id}:${offer.run_id}`).join(',');
    if (key === casesFor) return;
    casesFor = key;
    const withRun = list.filter((offer) => offer.run_id > 0);
    if (withRun.length === 0) {
      cases = offerCases(list, []);
      return;
    }
    try {
      const found = await listJobs({
        repo: [...new Set(withRun.map((offer) => offer.repo))],
        run_id: [...new Set(withRun.map((offer) => offer.run_id))],
        failed: true,
        limit: 200,
      });
      cases = offerCases(list, found.items ?? []);
    } catch {
      // The jobs make an offer legible, not usable: without them the card says less.
      casesFor = '';
      cases = offerCases(list, []);
    }
  }

  async function refresh(): Promise<void> {
    const readOffers = offering && Date.now() - offersReadAt >= OFFERS_EVERY_MS;
    const [read, offered, going, automaticRead, needs, decisionsRead, watchesRead] =
      await Promise.allSettled([
        getEliRepairSettings(),
        readOffers ? listEliOpportunities() : Promise.resolve(null),
        listEliRepairs({ group: 'active', limit: RUNNING_LIMIT }),
        administrator ? getEliAutomaticRepairs() : Promise.resolve(null),
        listEliRepairs({ group: 'needs_action', limit: RUNNING_LIMIT }),
        administrator ? listEliWatchRepositories() : Promise.resolve(null),
        administrator ? listEliWatches({ limit: 100 }) : Promise.resolve(null),
      ]);
    // One read failing leaves the others standing, and the first failure is the one said.
    // The automatic repairs say their own, in their panel, so one failure is not said twice.
    const failed = [read, offered, going, needs].find((result) => result.status === 'rejected');
    error = failed ? failure(failed.reason) : '';
    if (read.status === 'fulfilled') settings = read.value;
    if (automaticRead.status === 'fulfilled') {
      automaticFailed = false;
      if (automaticRead.value) automatic = automaticRead.value.repositories;
    } else {
      automaticFailed = true;
    }
    // The two watch reads are one answer: a failure in either means the section
    // cannot say what is being followed, so neither half is trusted.
    if (decisionsRead.status === 'fulfilled' && watchesRead.status === 'fulfilled') {
      decisions = decisionsRead.value?.items ?? [];
      watches = watchesRead.value?.items ?? [];
      watchFailed = false;
      watchError = '';
    } else if (administrator) {
      watchFailed = true;
      const reason =
        decisionsRead.status === 'rejected'
          ? decisionsRead.reason
          : watchesRead.status === 'rejected'
            ? watchesRead.reason
            : undefined;
      watchError = reason ? failure(reason) : '';
    }
    watchLoaded = true;

    if (going.status === 'fulfilled') {
      running = going.value.items;
      runningTotal = going.value.total;
    }
    attentionFailed = needs.status === 'rejected';
    if (needs.status === 'fulfilled') {
      attention = needs.value.items;
      attentionTotal = needs.value.total;
      attentionLoaded = true;
    }
    if (offered.status === 'fulfilled' && offered.value) {
      offersReadAt = Date.now();
      offers = offered.value.items;
      offersLoaded = true;
      await readCases(offers);
    }
  }

  /** What the page header's Refresh means here: read everything again, offers included. */
  async function reload(): Promise<void> {
    offersReadAt = 0;
    await Promise.all([refresh(), history?.reload()]);
  }

  function reviewAll(): void {
    const section = document.getElementById('history');
    if (section instanceof HTMLDetailsElement) section.open = true;
    history?.reviewNeedsAction();
    section?.scrollIntoView({ block: 'start' });
  }

  async function dismiss(id: string): Promise<void> {
    if (dismissing) return;
    dismissing = true;
    try {
      await actEliOpportunity(id, { action: 'dismiss' });
      toasts.success('Offer dismissed for this commit');
      offersReadAt = 0;
      await refresh();
    } catch (cause) {
      toasts.error(
        'Could not dismiss that offer',
        cause instanceof ApiError ? cause.message : 'Please try again.',
      );
    } finally {
      dismissing = false;
    }
  }

  function askToStop(repos: string[]): void {
    stopping = repos;
    stopError = '';
    stopOpen = true;
  }

  /**
   * Pausing, resuming and stopping a watch. The button that was pressed is
   * disabled while its request is out, so a second press cannot make a second
   * request that arrives after the first and undoes it.
   */
  async function changeWatch(
    id: string,
    action: () => Promise<string>,
    then?: () => void,
  ): Promise<void> {
    if (watchBusy.includes(id)) return;
    watchBusy = [...watchBusy, id];
    try {
      const error = await action();
      if (!error) {
        then?.();
        await refresh();
      }
    } finally {
      watchBusy = watchBusy.filter((busy) => busy !== id);
    }
  }

  function confirmControl(copy: WatchControlCopy, run: () => Promise<string>): void {
    confirming = { copy, run };
    confirmError = '';
    confirmOpen = true;
  }

  /** The confirmation closes on success and stays, with its reason, on a failure. */
  async function runConfirmed(): Promise<boolean> {
    if (!confirming || confirmBusy) return false;
    confirmBusy = true;
    const error = await confirming.run();
    confirmBusy = false;
    if (error) {
      confirmError = error;
      return false;
    }
    confirmError = '';
    await refresh();
    await tick();
    if (!document.activeElement || document.activeElement === document.body) {
      document.getElementById('page-heading')?.focus();
    }
    return true;
  }

  /**
   * Stops them, and says what became of each. When nothing could be stopped the
   * dialog stays, with the reason, so the same press is another try.
   */
  async function stop(then: StopChoice): Promise<void> {
    if (stopBusy) return;
    stopBusy = true;
    stopError = '';
    const result = await stopAutomatic(stopping, then);
    stopBusy = false;
    if (!result.done) {
      stopError = result.error;
      return;
    }
    stopOpen = false;
    await refresh();
    // The button that opened the dialog went with its row, and focus with it.
    await tick();
    if (!document.activeElement || document.activeElement === document.body) {
      document.getElementById('page-heading')?.focus();
    }
  }

  onMount(() => {
    if (forwarding) {
      router.navigate(targetHref(legacy), { replace: true });
      return;
    }
    void refresh();
    const timer = setInterval(() => {
      if (!document.hidden) void refresh();
    }, POLL_MS);
    return () => clearInterval(timer);
  });
</script>

{#if !forwarding}
  <PageHeader
    title="Eli repairs"
    subtitle="Review failed PR checks, follow each repair, and see what Eli is doing on its own."
    onrefresh={reload}
  >
    <Button icon={GitPullRequest} onclick={() => (dialogOpen = true)}>Fix a failed check</Button>
    {#if !showSetup}
      <!--
        Not while the setup bar is on screen: the bar carries the reason, the
        switch and its own way into the same page, and a second button for one
        destination is a question about which one is real rather than a
        convenience.
      -->
      <Button variant="ghost" icon={Settings2} href="/repairs/setup">Repair settings</Button>
    {/if}
  </PageHeader>

  <SectionNav items={navItems} label="On this page" />

  {#if error}<p role="alert" class="error">{error}</p>{/if}

  <div class="stack">
    {#if showSetup}
      <SetupBar
        {settings}
        admin={session.can('admin')}
        userId={session.identity?.id}
        authDisabled={session.authDisabled}
      />
    {/if}

    <PullRequestBar />

    {#if (offering && offers.length) || attention.length}
      <Disclosure
        id="needs"
        title="Needs you"
        summary="{pluralise(offers.length + attentionTotal, 'item')} needs review"
        tone="danger"
        open
        flush
      >
        <OfferList {offers} {cases} busy={dismissing} ondismiss={(id) => void dismiss(id)} bare />
        {#each attention as repair (repair.id)}
          <RepairRow
            {repair}
            selecting={false}
            selected={false}
            waiting={attentionFailed}
            onselect={() => {}}
          />
        {/each}
        {#if attentionTotal > attention.length}
          <Button onclick={reviewAll}>Review all {attentionTotal} repairs needing attention</Button>
        {/if}
      </Disclosure>
    {:else if offering && offersLoaded && attentionLoaded && !attentionFailed && !error}
      <section id="needs" class="clear" aria-label="Needs you">
        <CircleCheck size={16} aria-hidden="true" />
        <p>
          <strong>No repair offers or stopped repairs need review.</strong>
          <a href="/repairs/new">Fix a failed check</a> to check another pull request.
        </p>
      </section>
    {/if}

    {#if running.length}
      <Disclosure
        id="running"
        title="In progress"
        summary="{pluralise(runningTotal, 'repair')} running"
        tone="busy"
        open
        flush
      >
        <ActiveRepairs items={running} total={runningTotal} bare />
      </Disclosure>
    {/if}

    <Disclosure id="history" title="History" summary={historyWords} flush>
      <RepairHistory bind:this={history} bare onsummary={(summary) => (historySummary = summary)} />
    </Disclosure>

    {#if administrator && showWatching}
      <Disclosure
        id="watching"
        title="Watching"
        summary={watchingWords}
        tone={watching === 'failed' ? 'danger' : watchNeedsPerson ? 'pending' : ''}
        open={watching === 'failed' || watchNeedsPerson}
        flush
      >
        <WatchedRepositories
          {decisions}
          {watches}
          state={watching}
          busy={watchBusy}
          failure={watchError}
          onpausewatch={(watch) => confirmControl(pauseWatchCopy(watch), () => pauseWatch(watch))}
          onresumewatch={(watch) => void changeWatch(watch.id, () => resumeWatch(watch))}
          onstopwatch={(watch) => confirmControl(stopWatchCopy(watch), () => stopWatch(watch))}
          onpauserepo={(decision) => void changeWatch(decision.id, () => pauseRepository(decision))}
          onresumerepo={(decision) =>
            void changeWatch(decision.id, () => resumeRepository(decision))}
          onstopall={() => confirmControl(stopAllCopy(watches), () => stopAllWatches(watches))}
        />
      </Disclosure>
    {/if}

    {#if administrator && showAutomatic}
      <Disclosure
        id="automatic"
        title="Automatic repairs"
        summary={automaticWords}
        tone={automaticProblem ? 'pending' : ''}
        open={automaticProblem || automaticFailed}
        flush
      >
        <AutomaticRepairs
          repositories={automatic}
          error={automaticFailed}
          onstop={askToStop}
          bare
        />
      </Disclosure>
    {/if}
  </div>

  <RepairTargetDialog bind:open={dialogOpen} />
  <StopAutomaticDialog
    bind:open={stopOpen}
    repos={stopping}
    busy={stopBusy}
    error={stopError}
    onconfirm={(then) => void stop(then)}
  />
  <ConfirmDialog
    bind:open={confirmOpen}
    title={confirming?.copy.title ?? 'Confirm'}
    description={confirming?.copy.description ?? ''}
    consequences={confirming ? [confirming.copy.inFlight] : []}
    confirmLabel={confirming?.copy.confirm ?? 'Confirm'}
    cancelLabel="Cancel"
    tone={confirming?.copy.confirm.startsWith('Stop') ? 'danger' : 'default'}
    busy={confirmBusy}
    onconfirm={runConfirmed}
    oncancel={() => {
      if (confirmBusy) return;
      confirming = null;
      confirmError = '';
    }}
  >
    {#if confirmError}<p class="error" role="alert">{confirmError}</p>{/if}
  </ConfirmDialog>
{/if}

<style>
  .stack {
    display: grid;
    gap: var(--z-space-3);
  }
  .clear {
    display: flex;
    align-items: flex-start;
    gap: var(--z-space-2);
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
  }
  .clear :global(svg) {
    flex: none;
    margin-top: var(--z-nudge-2);
    color: var(--z-idle);
  }
  .clear p {
    margin: 0;
    line-height: var(--z-leading-sm);
  }
  .clear strong {
    color: var(--z-text);
    font-weight: var(--z-weight-semibold);
  }
  .error {
    margin: 0 0 var(--z-space-4);
    color: var(--z-danger);
    font-size: var(--z-text-sm);
  }
</style>
