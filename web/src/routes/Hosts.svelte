<!--
  Hosts: where runners can go.

  The list comes from the live fleet cache, so a host that stops sending
  heartbeats shows Unreachable here without anything being pressed. Everything
  that keeps a host from taking work -- cordoned, unreachable, no capacity left,
  a backend that is not available -- is said in words on the card, because that
  is the question this page exists to answer.

  Two things on this page do not arrive over the stream, though, and that is
  what refreshing is for here: a host enrolled a moment ago, which the
  controller announces only once it has heard from the agent, and the join
  tokens, which move when somebody mints or spends one rather than when the
  fleet does.
-->
<script lang="ts">
  import PageBand from '$lib/bands/PageBand.svelte';
  import { hostsLine } from '$lib/bands/lines';
  import Tooltip from '$lib/components/Tooltip.svelte';
  import HostCapacityMap from '$lib/insights/HostCapacityMap.svelte';
  import { hostSignals } from '$lib/insights/signals';
  import MetricGrid from '$lib/components/MetricGrid.svelte';
  import { tick } from 'svelte';
  import {
    ChevronDown,
    ChevronRight,
    ChevronsDownUp,
    ChevronsUpDown,
    CircleArrowUp,
    Plus,
    Server,
  } from '@lucide/svelte';
  import {
    clearHostThrottle,
    listJoinTokens,
    listMachines,
    getProviderRenting,
    listProviders,
    pauseProvider,
    resumeProvider,
  } from '$lib/api/client';
  import { SvelteMap } from 'svelte/reactivity';
  import { events } from '$lib/api/sse';
  import type { Host, JoinToken, Machine, Provider } from '$lib/api/types';
  import { NO_VALUE, onClockTick, pluralise } from '$lib/format';
  import { href, router } from '$lib/router';
  import { checkHostPriority } from '$lib/api/client';
  import { fleet } from '$lib/state/fleet.svelte';
  import { remember, remembered } from '$lib/state/prefs.svelte';
  import { session } from '$lib/state/session.svelte';
  import { toasts } from '$lib/state/toasts.svelte';
  import { updates } from '$lib/state/updates.svelte';
  import RolloutStatus from '$lib/updates/RolloutStatus.svelte';
  import StartRolloutDialog from '$lib/updates/StartRolloutDialog.svelte';
  import { rolloutIsOpen, rolloutOffer, rolloutWords, type RolloutOffer } from '$lib/updates/words';
  import Button from '$lib/components/Button.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import LoadingBoundary from '$lib/components/LoadingBoundary.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import HostCard from '$lib/hosts/HostCard.svelte';
  import HostCapacityDialog from '$lib/hosts/HostCapacityDialog.svelte';
  import HostRunnerSizesDialog from '$lib/hosts/HostRunnerSizesDialog.svelte';
  import { cordon } from '$lib/hosts/actions';
  import {
    HEALTH_FILTERS,
    attentionDetail,
    emptyCopy,
    filterSentence,
    healthCounts,
    healthFilterFrom,
    hostsFor,
    type HealthFilter,
  } from '$lib/hosts/health-filter';
  import { slotsOf } from '$lib/hosts/slots';
  import {
    EXPANDED_KEY,
    allExpanded,
    isIdList,
    setAllExpanded,
    setExpanded,
  } from '$lib/hosts/card-fold';
  import HostDeleteDialog from '$lib/hosts/HostDeleteDialog.svelte';
  import HostPriorityDialog from '$lib/hosts/HostPriorityDialog.svelte';
  import HostRenameDialog from '$lib/hosts/HostRenameDialog.svelte';
  import HostLabelsDialog from '$lib/hosts/HostLabelsDialog.svelte';
  import HostUpdateDialog from '$lib/hosts/HostUpdateDialog.svelte';
  import JoinTokenList from '$lib/hosts/JoinTokenList.svelte';
  import MachineBand from '$lib/providers/MachineBand.svelte';

  const canOperate = $derived(session.can('operator'));
  const canAdmin = $derived(session.can('admin'));

  const hosts = $derived(
    [...fleet.hosts].sort((a, b) => (a.name ?? '').localeCompare(b.name ?? '')),
  );
  const connected = $derived(hosts.filter((h) => h.healthy === true).length);
  // The slots hosts take, which is what the machines hold of a standard size
  // where one is set, not the sum of the operators' ceilings.
  const capacity = $derived(hosts.reduce((sum, h) => sum + slotsOf(h), 0));
  const inUse = $derived(hosts.reduce((sum, h) => sum + (h.active_runners ?? 0), 0));

  /* -- the health filter --------------------------------------------------------
   * The pill on each card is the one authority on a host's OS health, and the
   * counts here ask it (health-filter.ts), so a tile, a chip and a pill cannot
   * disagree. The tiles, the machine band and the map keep every host: only the
   * cards are filtered, because a filter that changed the totals above it
   * would make them stop meaning what their labels say.
   * ---------------------------------------------------------------------- */

  // The one shared clock, so the counts here and each card's pill go stale on
  // the same tick rather than a pill saying one thing and a chip another.
  let now = $state(Date.now());
  $effect(() => onClockTick((t) => (now = t)));
  const counts = $derived(healthCounts(hosts, now));
  const filter = $derived(healthFilterFrom(router.param('health')));
  const shown = $derived(hostsFor(hosts, filter, now));

  // Replaces the history entry and neither scrolls nor moves focus: choosing a
  // chip is not going somewhere. The default is the absence of the parameter,
  // so a plain /hosts link is All and an address never carries "health=all".
  function chooseFilter(next: HealthFilter): void {
    router.setQuery({ health: next === 'all' ? null : next });
  }

  // The button that was pressed unmounts with the empty state, and focus would
  // fall to the page; the All chip is where somebody pressing it wants to be.
  async function showAll(): Promise<void> {
    chooseFilter('all');
    await tick();
    document.getElementById('hosts-health-all')?.focus();
  }

  /* -- the capacity map ---------------------------------------------------------
   * Folded by default: it is a chart for asking which machine is the busy one,
   * and putting it above the cards pushed every host's controls a screen and a
   * half down. A choice this browser has made is remembered, written only when
   * it is made and never on sight, for the reason HostCapacityMap gives for its
   * layout: a browser that had merely looked would otherwise have "chosen".
   *
   * `{#if}` rather than a native <details>: Svelte builds a closed <details>'s
   * children anyway, which would keep the map's day of samples, its timer and
   * its per-heartbeat merge running behind a fold nobody has opened.
   * ---------------------------------------------------------------------- */

  const MAP_OPEN = 'zoomies.hosts.map.open';
  let mapOpen = $state(
    remembered(MAP_OPEN, false, (value): value is boolean => typeof value === 'boolean'),
  );
  function toggleMap(): void {
    mapOpen = !mapOpen;
    remember(MAP_OPEN, mapOpen);
  }

  // The map's "Manage" scrolls to a host's card. A card the filter has hidden
  // has no heading to scroll to, and doing nothing would look like a broken
  // link, so the filter is lifted first.
  async function manage(host: Host): Promise<void> {
    if (!shown.some((h) => h.id === host.id)) {
      router.setQuery({ health: null });
      await tick();
    }
    const heading = document.getElementById(`host-${host.id}-name`);
    heading?.scrollIntoView({ block: 'center' });
    heading?.focus({ preventScroll: true });
  }

  /*
   * The cards that are open.
   *
   * A card is closed unless this browser has been asked to open it. The page
   * holds the choice rather than the card, so a filter that takes a card off the
   * screen and brings it back does not close it, and it is written when a card is
   * opened or closed and never on sight, for the same reason as the map's.
   */

  let openCards = $state(remembered(EXPANDED_KEY, [] as string[], isIdList));
  function toggleCard(host: Host, open: boolean): void {
    if (!host.id) return;
    openCards = setExpanded(
      openCards,
      host.id,
      open,
      hosts.map((h) => h.id ?? ''),
    );
    remember(EXPANDED_KEY, openCards);
  }

  /*
   * Every card at once. It acts on the cards on show and on no others: opening
   * a card nobody can see would be a change made out of sight, and somebody who
   * has narrowed the page and pressed "Collapse all" has said nothing about the
   * hosts the filter took away. The button says which way it will go, so a card
   * opened or closed by hand moves it too.
   */
  const shownIds = $derived(shown.map((h) => h.id).filter((id): id is string => Boolean(id)));
  const allShownOpen = $derived(allExpanded(openCards, shownIds));
  const toggleShownHelp = $derived.by(() => {
    const verb = allShownOpen ? 'Close' : 'Open';
    return shownIds.length === hosts.length
      ? `${verb} the details of every host.`
      : `${verb} the details of the ${shownIds.length} hosts shown. The others stay as they are.`;
  });
  function toggleShownCards(): void {
    openCards = setAllExpanded(
      openCards,
      shownIds,
      !allShownOpen,
      hosts.map((h) => h.id ?? ''),
    );
    remember(EXPANDED_KEY, openCards);
  }

  /*
   * The button is offered only while two or more cards are on show, and that
   * count can fall with nobody pressing anything: a host is deleted, or a report
   * ages out of the filter on the page's clock. Whoever was on the button would
   * be dropped on the page when it went, so focus goes to the filter that is on,
   * which is where showAll() sends it too. This runs before the page changes,
   * because afterwards the button is gone, and so is the proof that it held focus.
   */
  $effect.pre(() => {
    const chip = `hosts-health-${filter}`;
    if (shownIds.length > 1) return;
    if (!document.activeElement?.classList.contains('expand-button')) return;
    void tick().then(() => document.getElementById(chip)?.focus());
  });

  /* -- the machines behind the hosts -------------------------------------------
   * Fetched here and kept out of the fleet cache deliberately: a fourth
   * collection there would cost every signed-in tab a request on every
   * reconcile pass, and the app shell weight the budget has none of. The live
   * half is one subscription, which costs nothing when nobody is on this page.
   * ------------------------------------------------------------------------ */

  let machines = $state<Machine[]>([]);
  let providers = $state<Provider[]>([]);
  let machinesReload = $state(0);

  $effect(() => {
    void machinesReload;
    const controller = new AbortController();
    void Promise.all([
      listProviders(controller.signal),
      listMachines({ limit: 200 }, controller.signal),
    ])
      .then(([providerPage, machinePage]) => {
        providers = providerPage.items ?? [];
        machines = machinePage.items ?? [];
      })
      .catch((cause: unknown) => {
        // Not an error state. The hosts are the page; the band above them is
        // context, and a fleet with no providers configured answers 200 with
        // nothing in it anyway.
        if (cause instanceof DOMException && cause.name === 'AbortError') return;
      });
    return () => controller.abort();
  });

  $effect(() => {
    const stop = [
      events.subscribe('machine.updated', (row) => {
        const index = machines.findIndex((m) => m.id === row.id);
        machines = index === -1 ? [...machines, row] : machines.with(index, row);
      }),
      events.subscribe('machine.deleted', (payload) => {
        machines = machines.filter((m) => m.id !== payload.id);
      }),
      events.subscribe('provider.updated', (row) => {
        providers = providers.map((p) => (p.id === row.id ? row : p));
      }),
    ];
    return () => {
      for (const off of stop) off();
    };
  });

  /** The machine a host is, for the hosts Zoomies rented. */
  const machineByHost = $derived.by(() => {
    const out = new SvelteMap<string, Machine>();
    for (const machine of machines) {
      if (machine.host_id) out.set(machine.host_id, machine);
    }
    return out;
  });

  async function pauseAll(paused: boolean): Promise<void> {
    for (const provider of providers) {
      if (!provider.id || provider.paused === paused) continue;
      try {
        const saved = paused
          ? await pauseProvider(provider.id, { reason: 'Paused from the Hosts page' })
          : await resumeProvider(provider.id);
        providers = providers.map((p) => (p.id === saved.id ? saved : p));
      } catch (cause) {
        toasts.fromError(
          cause,
          paused ? `${provider.name} was not paused` : `${provider.name} was not resumed`,
        );
        return;
      }
    }
    if (paused) {
      // With renting switched off the machine loop does nothing, so drains and
      // deletes wait too. Asked now rather than kept, because the switch can
      // move while this page is open.
      const rentingOff = (await getProviderRenting().catch(() => null))?.state === 'off';
      toasts.info(
        'New machines paused',
        rentingOff
          ? 'Nothing new is bought. Drains and deletes resume once renting is switched on.'
          : 'Nothing new is bought. Drains, deletes and machines already on their way carry on.',
      );
    } else {
      toasts.success('New machines resumed', 'Machines may be bought again.');
    }
  }

  /* -- join tokens ------------------------------------------------------------
   * Admin only, and fetched here rather than kept in the fleet cache: they
   * change when somebody mints one, not when the fleet moves.
   * ---------------------------------------------------------------------- */

  let tokens = $state<JoinToken[]>([]);
  let tokensLoading = $state(false);
  let tokensError = $state<unknown>(null);
  let tokensReload = $state(0);

  $effect(() => {
    if (!canAdmin) return;
    void tokensReload;
    const controller = new AbortController();
    tokensLoading = true;
    void listJoinTokens(controller.signal)
      .then((result) => {
        // Only the tokens that could still enrol a host. Spent ones are kept by
        // the controller as the record of how each host arrived, and expired
        // ones wait for the next prune; listing either here buries the few
        // that are worth revoking.
        tokens = (result.items ?? []).filter((t) => !t.used_at && t.usable !== false);
        tokensError = null;
      })
      .catch((cause: unknown) => {
        if (cause instanceof DOMException && cause.name === 'AbortError') return;
        tokensError = cause;
      })
      .finally(() => (tokensLoading = false));
    return () => controller.abort();
  });

  /* -- updating every host that is behind -------------------------------------
   * Admin only. The status is the document Settings → Updates reads, followed
   * over the stream, because a rollout moves on the controller's passes and not
   * on anything this page does. The count on the button is the hosts whose own
   * update block says they can be asked, the same answer their cards' buttons
   * are drawn from.
   * ------------------------------------------------------------------------ */

  $effect(() => {
    if (!canAdmin) return;
    return updates.follow();
  });
  const updateStatus = $derived(canAdmin ? updates.status : null);
  const rolloutAction = $derived(rolloutOffer(updateStatus, hosts, canAdmin));
  const rollout = $derived(updateStatus?.rollout ?? null);
  // An ended rollout stays on this page only once it has watched it end: the
  // status keeps the last one for good, and a card for a rollout that finished
  // last month would sit above the hosts with nothing to do. Settings → Updates
  // always shows it.
  let watched = $state('');
  $effect(() => {
    if (rollout && rolloutIsOpen(rollout)) watched = rollout.id;
  });
  const shownRollout = $derived(
    rollout && (rolloutIsOpen(rollout) || rollout.id === watched) ? rollout : null,
  );
  const rolloutText = $derived(shownRollout ? rolloutWords(shownRollout) : null);

  let rolloutConfirm = $state(false);
  // What the button said when it was pressed, so a host joining while the
  // dialog is open cannot change the number the administrator agreed to.
  let rolloutCount = $state(0);
  let rolloutTag = $state('');
  let rolloutBusy = $state(false);

  // After a press the button that was pressed is gone, and focus would fall to
  // the page. It goes to the rollout, after the frames in which a dialog hands
  // focus back to an opener it can no longer find.
  function landOnRollout(): void {
    requestAnimationFrame(() =>
      requestAnimationFrame(() => {
        document.getElementById('hosts-rollout')?.focus();
        setTimeout(() => {
          const held = document.activeElement;
          if (!held || held === document.body || held.id === 'page-heading')
            document.getElementById('hosts-rollout')?.focus();
        }, 250);
      }),
    );
  }

  function askRollout(offer: Extract<RolloutOffer, { kind: 'offer' }>): void {
    rolloutCount = offer.count;
    rolloutTag = offer.tag;
    rolloutConfirm = true;
  }

  async function startRollout(): Promise<boolean> {
    try {
      await updates.startRollout();
      landOnRollout();
    } catch (cause) {
      toasts.fromError(cause, 'The rollout was not started');
    }
    // Closed either way: a refusal is a toast in the controller's words.
    return true;
  }

  async function moveRollout(resume: boolean): Promise<void> {
    rolloutBusy = true;
    try {
      await (resume ? updates.resumeRollout() : updates.cancelRollout());
      landOnRollout();
    } catch (cause) {
      toasts.fromError(
        cause,
        resume ? 'The rollout was not resumed' : 'The rollout was not cancelled',
      );
    } finally {
      rolloutBusy = false;
    }
  }

  /* -- actions ----------------------------------------------------------------- */

  let editing = $state<Host | null>(null);
  let editOpen = $state(false);
  let renaming = $state<Host | null>(null);
  let renameOpen = $state(false);
  let sizing = $state<Host | null>(null);
  let sizeOpen = $state(false);
  let profiling = $state<Host | null>(null);
  let profileOpen = $state(false);
  let deleting = $state<Host | null>(null);
  let deleteOpen = $state(false);
  let updating = $state<Host | null>(null);
  let updateOpen = $state(false);
  let prio = $state<Host | null>(null);
  let prioOpen = $state(false);
  /**
   * Fetch both halves of this page again: the fleet, and the tokens beside it.
   * The token list reports its own progress in place, so the button follows the
   * reconcile -- the slower and more interesting of the two.
   */
  async function refreshPage(): Promise<void> {
    tokensReload += 1;
    machinesReload += 1;
    await fleet.reconcile();
  }

  /**
   * Lift a throttle by hand. Optimistic like a cordon: the card shows the
   * host on its configured capacity at once, and comes back if the controller
   * refuses. The toast says what the lift does and does not promise, because
   * nothing pins it -- a host still under pressure is stepped down again on
   * its next heartbeat.
   */
  async function liftThrottle(host: Host): Promise<void> {
    if (!host.id) return;
    const name = host.name || host.id;
    const result = await fleet.optimistic(
      host.id,
      { throttle: undefined, throttle_reason: '', effective_capacity: slotsOf(host) },
      () => clearHostThrottle(host.id ?? ''),
      `The throttle on ${name} was not lifted`,
    );
    if (result === undefined) return;
    toasts.success(
      `Throttle lifted on ${name}`,
      'It takes its configured capacity again on the next pass, and its runners get their full CPU allocation on its next heartbeat. If the pressure is still there, that heartbeat throttles it again.',
    );
  }

  function edit(host: Host): void {
    editing = host;
    editOpen = true;
  }

  function rename(host: Host): void {
    renaming = host;
    renameOpen = true;
  }

  function size(host: Host): void {
    sizing = host;
    sizeOpen = true;
  }

  function sizes(host: Host): void {
    profiling = host;
    profileOpen = true;
  }

  function remove(host: Host): void {
    deleting = host;
    deleteOpen = true;
  }

  function update(host: Host): void {
    updating = host;
    updateOpen = true;
  }

  function priority(host: Host): void {
    prio = host;
    prioOpen = true;
  }

  /**
   * Re-read what the specs rank this host at, without opening anything. Used
   * after an upgrade the host has had: the machine re-reports its figures on
   * its next heartbeat anyway, and this settles the card and the next pass
   * now. The name remembered is what the toast says while it runs.
   */
  async function recheck(host: Host): Promise<void> {
    if (!host.id) return;
    try {
      const fresh = await checkHostPriority(host.id);
      fleet.reconcile();
      const name = fresh.name || host.id;
      toasts.success(
        `Re-checked ${name}`,
        `Its priority stands at ${fresh.priority} of 100, ${fresh.priority_source === 'overridden' ? 'still where the operator pinned it' : 'scored from its specs as they were last reported'}. The next pass places against it.`,
      );
    } catch (cause) {
      toasts.fromError(cause, 'The priority could not be re-checked');
    }
  }

  const bandLine = $derived(
    fleet.loaded ? hostsLine({ connected, total: hosts.length, inUse, capacity }) : null,
  );
</script>

<PageHeader
  title="Hosts"
  subtitle="The machines that run runners, and how much room each one has left."
  onrefresh={refreshPage}
>
  {#snippet meta()}
    <!--
      The figures this line used to carry (how many hosts, how many connected,
      how many slots in use) are the band's sentence and the tiles under it: the
      page said the same three numbers three times within 400px. What is left is
      the two things only a line of text can say: why an update cannot be
      offered, and how many hosts arrive over a private network, which no other
      figure on the page counts and which an operator watching a tunnel needs.
    -->
    {#if fleet.loaded && hosts.some((h) => h.connection === 'tailcat')}
      <p class="summary">
        {pluralise(hosts.filter((h) => h.connection === 'tailcat').length, 'host')} via Tailcat
      </p>
    {/if}
    {#if rolloutAction.kind === 'off'}
      <!-- The reason is text beside the action, as on a host's card: a phone has no hover. -->
      <p class="summary">{rolloutAction.sentence}</p>
    {/if}
  {/snippet}
  <Button variant="secondary" href="/usage?group_by=host">Usage history</Button>
  {#if rolloutAction.kind === 'offer'}
    {@const offer = rolloutAction}
    <Button variant="secondary" icon={CircleArrowUp} onclick={() => askRollout(offer)}
      >Update {pluralise(offer.count, 'host')}</Button
    >
  {:else if rolloutAction.kind === 'off'}
    <Button variant="secondary" icon={CircleArrowUp} disabled>Update hosts</Button>
  {/if}
  {#if canAdmin}
    <Button variant="primary" icon={Plus} href="/hosts/new">Add a host</Button>
  {/if}
</PageHeader>
<PageBand page="hosts" line={bandLine} />

<div class="content">
  {#if canAdmin}
    <!-- Always in the page for an administrator, because a region that appears
         with its words already in it is not reliably read out. -->
    <p class="sr-only" role="status" aria-live="polite">{rolloutText?.live ?? ''}</p>
  {/if}
  {#if canAdmin && shownRollout}
    <!-- Focusable by script only: after a press the button that was pressed is
         gone, and focus comes here rather than falling to the page. -->
    <section class="panel" id="hosts-rollout" aria-labelledby="hosts-rollout-heading" tabindex="-1">
      <header>
        <div>
          <h2 id="hosts-rollout-heading">Host rollout</h2>
          <p>Hosts behind the controller's release, updated one at a time.</p>
        </div>
      </header>
      <div class="rollout-body">
        <RolloutStatus
          rollout={shownRollout}
          sentence={shownRollout.state === 'running' ? (updateStatus?.reason ?? '') : ''}
        >
          {#snippet actions()}
            {#if rolloutText?.canResume}
              <Button
                size="sm"
                variant="primary"
                disabled={rolloutBusy}
                onclick={() => void moveRollout(true)}>Resume the rollout</Button
              >
            {/if}
            {#if rolloutText?.open}
              <Button
                size="sm"
                variant="secondary"
                disabled={rolloutBusy}
                onclick={() => void moveRollout(false)}>Cancel the rollout</Button
              >
            {/if}
          {/snippet}
        </RolloutStatus>
      </div>
    </section>
  {/if}
  <!--
    A failed reconcile once the hosts are on screen is not an error state: the
    cache still holds every host and the stream keeps updating them. Only a
    first load that never landed gets one, as the Overview does.
  -->
  <LoadingBoundary
    loading={fleet.loading && !fleet.loaded}
    error={fleet.loaded ? null : fleet.error}
    empty={fleet.loaded && hosts.length === 0}
    onretry={() => void fleet.reconcile()}
  >
    {#snippet skeleton()}
      <div class="grid">
        {#each [0, 1, 2] as card (card)}
          <div class="card-skeleton">
            <Skeleton width="45%" height="1.25rem" />
            <Skeleton lines={3} />
            <Skeleton height="var(--z-space-2)" radius="full" />
          </div>
        {/each}
      </div>
    {/snippet}

    {#snippet emptyState()}
      <EmptyState
        icon={Server}
        title="No hosts yet"
        description="A host is a machine running the Zoomies agent, and it is where runners are created. The controller can run one itself, or you can enrol another."
      >
        {#if canAdmin}
          <Button variant="primary" icon={Plus} href="/hosts/new">Add a host</Button>
        {:else}
          <p class="need-admin">An administrator can enrol one.</p>
        {/if}
      </EmptyState>
    {/snippet}

    <MetricGrid
      strip
      items={[
        {
          label: 'Available host slots',
          value: hosts
            .filter((h) => hostSignals(h).eligible)
            .every((h) => hostSignals(h).free !== null)
            ? String(
                hosts
                  .filter((h) => hostSignals(h).eligible)
                  .reduce((n, h) => n + (hostSignals(h).free ?? 0), 0),
              )
            : NO_VALUE,
          detail: 'Connected, uncordoned, compatible hosts; runner fit still applies',
        },
        {
          label: 'Hosts connected',
          value: `${connected} / ${hosts.length}`,
          detail: 'Sending heartbeats',
        },
        {
          label: 'Need attention',
          value: String(counts.attention),
          detail: attentionDetail(counts),
          href: href('/hosts', { health: 'attention' }),
          tone: counts.attention > 0 ? 'warning' : 'neutral',
        },
        {
          label: 'Runner slots in use',
          value: `${inUse} / ${capacity}`,
          detail: 'Configured slots across all hosts',
        },
        {
          label: 'Cordoned hosts',
          value: String(hosts.filter((h) => h.cordoned).length),
          detail: 'Excluded from new placements',
        },
        {
          label: 'Hosts at slot capacity',
          value: String(
            hosts.filter((h) => slotsOf(h) > 0 && (h.active_runners ?? 0) >= slotsOf(h)).length,
          ),
          detail: 'Resource limits can restrict placement sooner',
        },
      ]}
    />
    <div class="capacity-map">
      <!--
        A disclosure button, not a section of its own: the open map already
        brings its own heading and region, and a second region of the same name
        would make every way of finding it ambiguous.
      -->
      <div class="map-toggle">
        <Button
          variant="secondary"
          class="map-button"
          icon={mapOpen ? ChevronDown : ChevronRight}
          ariaExpanded={mapOpen}
          ariaControls={mapOpen ? 'host-capacity-map' : undefined}
          onclick={toggleMap}>Host capacity map</Button
        >
        {#if !mapOpen}
          <span class="map-hint">CPU, memory and runner slots for every host, over time</span>
        {/if}
      </div>
      {#if mapOpen}
        <div class="map-body" id="host-capacity-map">
          <HostCapacityMap {hosts} page="hosts" onmanage={(host) => void manage(host)} />
        </div>
      {/if}
    </div>
    <h2 class="controls-heading">Host controls and configuration</h2>
    <div class="toolbar">
      <div class="filters" role="group" aria-label="Show hosts by OS health">
        {#each HEALTH_FILTERS as option (option.value)}
          <Tooltip bare text={option.title}>
            <button
              type="button"
              class="filter"
              id="hosts-health-{option.value}"
              aria-pressed={filter === option.value}
              onclick={() => chooseFilter(option.value)}
              >{option.label} <span class="count">{counts[option.value]}</span></button
            >
          </Tooltip>
        {/each}
      </div>
      <!-- Outside the group, which is a choice of what to show and not a control
           over the cards. One button for both ways, so a keyboard user who
           presses it keeps their place and the name says what the next press
           does. Not offered for a page of one card: that card's own Details is
           the whole of it, and a second button for the same press is noise. -->
      {#if shownIds.length > 1}
        <div class="expand">
          <!-- Which cards "all" means, in words on the page as well as in the
               tooltip: a phone has no hover, and "Collapse all" that leaves cards
               open out of sight would be a surprise. Said only once a filter has
               taken some away. -->
          {#if shownIds.length < hosts.length}
            <span class="expand-hint">Acts on the {pluralise(shownIds.length, 'host')} shown</span>
          {/if}
          <Button
            variant="ghost"
            size="sm"
            class="expand-button"
            icon={allShownOpen ? ChevronsDownUp : ChevronsUpDown}
            help={toggleShownHelp}
            onclick={toggleShownCards}>{allShownOpen ? 'Collapse all' : 'Expand all'}</Button
          >
        </div>
      {/if}
    </div>
    <!--
      Always in the page, so that a screen reader hears the sentence appear in a
      region it already knows. It is written for every filter but All, a count
      of zero included: a filter that says nothing when it matches nothing is
      indistinguishable from one that did not work.
    -->
    <div role="status">
      {#if filter !== 'all'}
        <p class="filter-note">{filterSentence(filter, shown.length, hosts.length)}</p>
      {/if}
    </div>
    {#if filter !== 'all' && shown.length === 0}
      {@const copy = emptyCopy(filter, counts)}
      <EmptyState compact icon={Server} title={copy.title} description={copy.description}>
        <Button variant="secondary" onclick={() => void showAll()}>Show all hosts</Button>
      </EmptyState>
    {:else}
      <div class="grid">
        {#each shown as host (host.id)}
          <HostCard
            {host}
            machine={host.id ? machineByHost.get(host.id) : null}
            {canOperate}
            {canAdmin}
            oncordon={(target, next) => void cordon(target, next)}
            onthrottle={(target) => void liftThrottle(target)}
            oncapacity={size}
            onsizes={sizes}
            onpriority={priority}
            onrecheck={(target) => void recheck(target)}
            onedit={edit}
            onrename={rename}
            ondelete={remove}
            onupdate={update}
            expanded={host.id ? openCards.includes(host.id) : false}
            ontoggle={toggleCard}
          />
        {/each}
      </div>
    {/if}
  </LoadingBoundary>

  <!--
    Machines are provider inventory, and a machine becomes a host only when its
    agent joins. With the band above the cards the page's own subject started
    1,060px down a 900px window, so the inventory follows the hosts it turns
    into; the Providers page carries the same strip as its main subject.
  -->
  {#if providers.length > 0}
    <MachineBand
      class="machine-band"
      {machines}
      {providers}
      {canOperate}
      onpause={(paused) => void pauseAll(paused)}
    />
  {/if}

  {#if canAdmin}
    <section class="panel" aria-labelledby="join-tokens-heading">
      <!--
        No button of its own. "Add a host" is already the page's primary
        action, in the header, and two of the same button on one screen is a
        question about which one is the real one rather than a convenience.
      -->
      <header>
        <div>
          <h2 id="join-tokens-heading">Join tokens</h2>
          <p>
            Each one enrols a single host, then is spent. Adding a host mints one; revoke any that
            were minted and never used.
          </p>
        </div>
      </header>
      <div class="panel-body">
        <LoadingBoundary
          loading={tokensLoading && tokens.length === 0}
          error={tokensError}
          onretry={() => (tokensReload += 1)}
        >
          {#snippet skeleton()}
            <Skeleton lines={3} />
          {/snippet}
          <JoinTokenList {tokens} onrevoked={() => (tokensReload += 1)} />
        </LoadingBoundary>
      </div>
    </section>
  {/if}
</div>

<HostCapacityDialog bind:open={sizeOpen} host={sizing} onclose={() => (sizing = null)} />
<HostRunnerSizesDialog
  bind:open={profileOpen}
  host={profiling}
  onclose={() => (profiling = null)}
/>

<HostPriorityDialog bind:open={prioOpen} host={prio} onclose={() => (prio = null)} />
<HostRenameDialog bind:open={renameOpen} host={renaming} onclose={() => (renaming = null)} />
<HostLabelsDialog bind:open={editOpen} host={editing} onclose={() => (editing = null)} />
<HostDeleteDialog bind:open={deleteOpen} host={deleting} onclose={() => (deleting = null)} />
<HostUpdateDialog bind:open={updateOpen} host={updating} onclose={() => (updating = null)} />
<StartRolloutDialog
  bind:open={rolloutConfirm}
  tag={rolloutTag}
  count={rolloutCount}
  onconfirm={startRollout}
/>

<style>
  .capacity-map {
    margin-bottom: var(--z-space-5);
  }
  .map-toggle {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-2) var(--z-space-3);
  }
  .map-hint {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .map-body {
    margin-top: var(--z-space-3);
  }
  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-2) var(--z-space-3);
    margin-bottom: var(--z-space-3);
  }
  .filters {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
  /* The hint and the button are one unit, at the end of the row whether it shares
     the row with the chips or has wrapped onto a line of its own, and they wrap
     apart from each other only when a narrow window leaves no room for both. */
  .expand {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: var(--z-space-1) var(--z-space-3);
    margin-inline-start: auto;
  }
  .expand-hint {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    text-align: end;
  }
  .filter {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    min-height: var(--z-space-6);
    padding: var(--z-space-1) var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-full);
    background: var(--z-surface);
    color: var(--z-text-muted);
    font: inherit;
    font-size: var(--z-text-xs);
    cursor: pointer;
    transition:
      background var(--z-motion-fast) var(--z-ease),
      color var(--z-motion-fast) var(--z-ease),
      border-color var(--z-motion-fast) var(--z-ease);
  }
  .filter:hover {
    border-color: var(--z-border-strong);
    color: var(--z-text);
  }
  .filter[aria-pressed='true'] {
    /* The thick border is the contract's mark for the one that is selected
       (docs/ui-guidelines.md), and it survives forced-colours mode where a
       background does not. The padding gives back what the border takes, so
       pressing a chip moves nothing. */
    padding: calc(var(--z-space-1) - var(--z-border-width))
      calc(var(--z-space-3) - var(--z-border-width));
    border-width: var(--z-border-width-thick);
    border-color: var(--z-accent);
    background: var(--z-accent-subtle);
    color: var(--z-text);
    font-weight: var(--z-weight-medium);
  }
  .count {
    font-variant-numeric: tabular-nums;
  }
  .filter-note {
    margin: 0 0 var(--z-space-3);
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .content :global(.machine-band) {
    margin-bottom: var(--z-space-5);
  }
  .controls-heading {
    font-size: var(--z-text-sm);
    margin: 0 0 var(--z-space-3);
    font-weight: var(--z-weight-semibold);
  }
  .content {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-6);
  }
  .summary {
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  /* Cards in a row share a height, and the sections of a card are rows of this
     grid (HostCard says how), so the slot bars, the three bars and the Details
     buttons line up across a row instead of sitting wherever a card's own content
     put them. The row gap is zero because it would also open up the rows inside
     every card; each card carries the space beneath it instead, and the last
     row's is taken back. */
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
    column-gap: var(--z-space-4);
    row-gap: 0;
    margin-bottom: calc(-1 * var(--z-space-4));
  }
  .card-skeleton {
    margin-bottom: var(--z-space-4);
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    padding: var(--z-space-5);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  .panel {
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  .panel header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--z-space-4);
    padding: var(--z-space-4) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  h2 {
    margin: 0;
    font-size: var(--z-text-lg);
    line-height: var(--z-leading-lg);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .panel header p {
    margin: var(--z-space-1) 0 0;
    max-width: 70ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
  .panel-body {
    padding: var(--z-space-3) 0;
  }
  .rollout-body {
    padding: var(--z-space-4) var(--z-space-5);
  }
  .need-admin {
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-subtle);
  }
  @media (pointer: coarse) {
    .filter {
      min-height: var(--z-control-touch);
    }
    /* A Button is 32px high at every width, and this one is opened with a
       thumb: it takes the touch height the chips beside it take. */
    .map-toggle :global(.map-button),
    .toolbar :global(.expand-button) {
      height: var(--z-control-touch);
    }
  }
  @media (max-width: 768px) {
    .grid {
      grid-template-columns: 1fr;
    }
  }
</style>
