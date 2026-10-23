<!--
  The hub: what needs the person, what is running, and what happened.

  One line of setup and two attention rails say whether anything is owed. The
  sections below keep their counts and their latest sentence when closed, so the
  History and an administrator's Automatic repairs do not compete with live work
  for the first screen. A section that holds work opens; a quiet section says so
  in one line.

  It reads on a ten-second timer, as the history does, and reads the offers at
  most once a minute: they rarely change, and the check behind them asks GitHub
  about the person's access. The Refresh button is the one time somebody is asking
  right now, so it reads them at once.
-->
<script lang="ts">
  import {
    ArrowRight,
    CircleCheck,
    GitPullRequest,
    Play,
    Settings2,
    TriangleAlert,
  } from '@lucide/svelte';
  import { onMount, tick } from 'svelte';
  import {
    ApiError,
    actEliOpportunity,
    getEliAutomaticRepairs,
    getEliRepairSettings,
    listEliOpportunities,
    listEliRepairs,
    listJobs,
  } from '$lib/api/client';
  import type { Schemas } from '$lib/api/types';
  import PageBand from '$lib/bands/PageBand.svelte';
  import { repairsLine } from '$lib/bands/lines';
  import Button from '$lib/components/Button.svelte';
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
  import { RUNNING_LIMIT, offerCases } from '$lib/repairs/hub';
  import { stopAutomatic } from '$lib/repairs/stop';
  import { parseTarget, targetHref } from '$lib/repairs/workspace';
  import ActiveRepairs from '$lib/repairs/ActiveRepairs.svelte';
  import AutomaticRepairs from '$lib/repairs/AutomaticRepairs.svelte';
  import OfferList from '$lib/repairs/OfferList.svelte';
  import RepairHistory from '$lib/repairs/RepairHistory.svelte';
  import RepairTargetDialog from '$lib/repairs/RepairTargetDialog.svelte';
  import SetupBar from '$lib/repairs/SetupBar.svelte';
  import StopAutomaticDialog from '$lib/repairs/StopAutomaticDialog.svelte';

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
  /** The first read has finished, even if part of it failed. */
  let readOnce = $state(false);
  let running = $state<Schemas['EliRepair'][]>([]);
  let runningTotal = $state(0);
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
  const automaticUsed = $derived(automatic.reduce((sum, repo) => sum + Math.max(0, repo.used), 0));
  const automaticLimit = $derived(
    automatic.reduce((sum, repo) => sum + Math.max(0, repo.daily_limit), 0),
  );
  const automaticWords = $derived.by(() => {
    if (automaticFailed) return 'Could not read automatic repairs';
    if (automaticProblem) {
      const problems = automatic.filter((repo) => repo.problem).length;
      return `${problems} of ${automatic.length} ${
        automatic.length === 1 ? 'repository' : 'repositories'
      } cannot repair`;
    }
    return `${pluralise(automatic.length, 'repository', 'repositories')} · ${automaticUsed} of ${automaticLimit} attempts used`;
  });
  const historyOpen = $derived(
    historySummary.loaded &&
      !historySummary.error &&
      offers.length === 0 &&
      running.length === 0 &&
      settings !== null,
  );
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
  const bandLine = $derived(
    readOnce && !error ? repairsLine({ offers: offers.length, running: runningTotal }) : null,
  );
  const navItems = $derived([
    { id: 'now', label: 'Now' },
    { id: 'needs', label: 'Needs you', count: offers.length },
    { id: 'running', label: 'In progress', count: running.length },
    ...(administrator && showAutomatic
      ? [{ id: 'automatic', label: 'Automatic', count: automatic.length, secondary: true }]
      : []),
    { id: 'history', label: 'History', secondary: true },
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
    const [read, offered, going, automaticRead] = await Promise.allSettled([
      getEliRepairSettings(),
      readOffers ? listEliOpportunities() : Promise.resolve(null),
      listEliRepairs({ group: 'active', limit: RUNNING_LIMIT }),
      administrator ? getEliAutomaticRepairs() : Promise.resolve(null),
    ]);
    // One read failing leaves the others standing, and the first failure is the one said.
    // The automatic repairs say their own, in their panel, so one failure is not said twice.
    const failed = [read, offered, going].find((result) => result.status === 'rejected');
    error = failed ? failure(failed.reason) : '';
    if (read.status === 'fulfilled') settings = read.value;
    if (automaticRead.status === 'fulfilled') {
      automaticFailed = false;
      if (automaticRead.value) automatic = automaticRead.value.repositories;
    } else {
      automaticFailed = true;
    }
    if (going.status === 'fulfilled') {
      running = going.value.items;
      runningTotal = going.value.total;
    }
    if (offered.status === 'fulfilled' && offered.value) {
      offersReadAt = Date.now();
      offers = offered.value.items;
      offersLoaded = true;
      await readCases(offers);
    }
    readOnce = true;
  }

  /** What the page header's Refresh means here: read everything again, offers included. */
  async function reload(): Promise<void> {
    offersReadAt = 0;
    await Promise.all([refresh(), history?.reload()]);
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

  /** Open a closed destination before moving to it, and leave focus on its heading. */
  function jumpTo(id: string): void {
    const target = document.getElementById(id);
    if (!target) return;
    for (let node: HTMLElement | null = target; node; node = node.parentElement) {
      if (node instanceof HTMLDetailsElement) node.open = true;
    }
    target.scrollIntoView({ block: 'start' });
    const focusable =
      target instanceof HTMLDetailsElement ? target.querySelector<HTMLElement>('summary') : target;
    focusable?.focus({ preventScroll: true });
    if (location.hash !== `#${id}`) window.history.replaceState(null, '', `#${id}`);
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
    <Button icon={GitPullRequest} onclick={() => (dialogOpen = true)}>Repair a PR</Button>
    <Button variant="ghost" icon={Settings2} href="/repairs/setup">Setup</Button>
  </PageHeader>

  <PageBand page="repairs" line={bandLine} />

  <SectionNav items={navItems} label="On this page" />

  {#if error}<p role="alert" class="error">{error}</p>{/if}

  <div class="stack">
    <section id="now" class="now" aria-label="Now">
      {#if offers.length || running.length}
        <div class="attention">
          {#if offers.length}
            <a
              class="rail danger"
              href="#needs"
              onclick={(event) => {
                event.preventDefault();
                jumpTo('needs');
              }}
            >
              <span class="rail-icon"><TriangleAlert size={16} aria-hidden="true" /></span>
              <span class="rail-body">
                <strong>{pluralise(offers.length, 'offer needs', 'offers need')} review</strong>
                <span>Failed PR checks on Ask me first repositories.</span>
              </span>
              <span class="rail-go">Review offers <ArrowRight size={14} aria-hidden="true" /></span>
            </a>
          {/if}
          {#if running.length}
            <a
              class="rail busy"
              href="#running"
              onclick={(event) => {
                event.preventDefault();
                jumpTo('running');
              }}
            >
              <span class="rail-icon"><Play size={16} aria-hidden="true" /></span>
              <span class="rail-body">
                <strong>{pluralise(runningTotal, 'repair is', 'repairs are')} running now</strong>
                <span>Eli is reading the failed checks or preparing a fix.</span>
              </span>
              <span class="rail-go">Follow <ArrowRight size={14} aria-hidden="true" /></span>
            </a>
          {/if}
        </div>
      {/if}
      <SetupBar
        {settings}
        admin={session.can('admin')}
        userId={session.identity?.id}
        authDisabled={session.authDisabled}
      />
    </section>

    {#if offering && offers.length}
      <Disclosure
        id="needs"
        title="Needs you"
        summary="{pluralise(offers.length, 'offer')} · reviewing never calls a model"
        tone="danger"
        open
        flush
      >
        <OfferList {offers} {cases} busy={dismissing} ondismiss={(id) => void dismiss(id)} bare />
      </Disclosure>
    {:else if offering && offersLoaded}
      <section id="needs" class="clear" aria-label="Needs you">
        <CircleCheck size={16} aria-hidden="true" />
        <p>
          <strong>Nothing needs you right now.</strong> Offers appear when a pull request's checks
          fail on a repository set to Ask me first.
          <a href="/repairs/new">Repair a pull request</a> to start one yourself.
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

    <Disclosure id="history" title="History" summary={historyWords} open={historyOpen} flush>
      <RepairHistory bind:this={history} bare onsummary={(summary) => (historySummary = summary)} />
    </Disclosure>
  </div>

  <RepairTargetDialog bind:open={dialogOpen} />
  <StopAutomaticDialog
    bind:open={stopOpen}
    repos={stopping}
    busy={stopBusy}
    error={stopError}
    onconfirm={(then) => void stop(then)}
  />
{/if}

<style>
  .stack,
  .now {
    display: grid;
    gap: var(--z-space-3);
  }
  .now {
    scroll-margin-top: calc(var(--z-topbar-height) + var(--z-control-touch) + var(--z-space-3));
  }
  .attention {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--z-space-3);
  }
  .rail {
    display: flex;
    align-items: center;
    gap: var(--z-space-3);
    min-height: 4.25rem;
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-left: var(--z-border-width-rail) solid var(--z-pending);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
    color: var(--z-text);
    text-decoration: none;
    box-shadow: var(--z-shadow-sm);
  }
  .rail.danger {
    border-left-color: var(--z-danger);
  }
  .rail.busy {
    border-left-color: var(--z-busy);
  }
  .rail:hover {
    background: var(--z-surface-hover);
  }
  .rail-icon {
    display: grid;
    place-items: center;
    width: var(--z-space-8);
    height: var(--z-space-8);
    border-radius: var(--z-radius-sm);
    background: var(--z-pending-subtle);
    color: var(--z-pending);
    flex: none;
  }
  .rail.danger .rail-icon {
    background: var(--z-danger-subtle);
    color: var(--z-danger);
  }
  .rail.busy .rail-icon {
    background: var(--z-busy-subtle);
    color: var(--z-busy);
  }
  .rail-body {
    min-width: 0;
  }
  .rail-body strong {
    display: block;
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
    line-height: var(--z-leading-base);
    overflow-wrap: anywhere;
  }
  .rail-body span {
    display: block;
    margin-top: var(--z-nudge-1);
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    overflow-wrap: anywhere;
  }
  .rail-go {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
    margin-left: auto;
    color: var(--z-accent);
    font-size: var(--z-text-xs);
    white-space: nowrap;
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
  @media (max-width: 768px) {
    .attention {
      grid-template-columns: minmax(0, 1fr);
    }
    .rail {
      min-height: var(--z-control-touch);
    }
    .rail-go {
      display: none;
    }
  }
</style>
