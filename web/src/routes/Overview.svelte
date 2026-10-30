<!-- Current work keeps the same place in both styles; supporting detail stays live when closed. -->
<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { ChevronDown } from '@lucide/svelte';
  import RepairOffers from '$lib/repairs/RepairOffers.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Select from '$lib/components/Select.svelte';
  import HostCapacityMap from '$lib/insights/HostCapacityMap.svelte';
  import { navigate } from '$lib/router';
  import { fleet } from '$lib/state/fleet.svelte';
  import { notifications } from '$lib/state/notifications.svelte';
  import { formatDuration, pluralise } from '$lib/format';
  import { prefs } from '$lib/state/prefs.svelte';
  import ActiveJobs from '$lib/overview/ActiveJobs.svelte';
  import FirstRun from '$lib/overview/FirstRun.svelte';
  import FleetStatus from '$lib/overview/FleetStatus.svelte';
  import FleetActivity from '$lib/overview/FleetActivity.svelte';
  import PoolUtilisation from '$lib/overview/PoolUtilisation.svelte';
  import ProblemsSummary from '$lib/overview/ProblemsSummary.svelte';
  import RecentFailures from '$lib/overview/RecentFailures.svelte';
  import EventsFeed from '$lib/overview/EventsFeed.svelte';
  import OverviewDisclosure from '$lib/overview/OverviewDisclosure.svelte';
  import StartupTimings from '$lib/overview/StartupTimings.svelte';

  let setupPending = $state(false);
  let failures = $state<RecentFailures | null>(null);
  let activity = $state<FleetActivity | null>(null);
  let historySummary = $state('Loading history');
  let active = $state('now');
  let moreOpen = $state(false);
  let pageNav = $state<HTMLElement>();
  let jumped: { id: string; y: number } | null = null;
  const loading = $derived(!fleet.loaded);
  // On a desktop the hero says what the problems banner says, with the same way in, so
  // the banner stands down there. The one thing only it carries is the count of problems
  // somebody has put away, so it stays when that is all it has to say.
  const heroSaysIt = $derived(
    fleet.loaded &&
      prefs.bandShown('overview') &&
      !setupPending &&
      (notifications.active.length > 0 || notifications.dismissed.length === 0),
  );
  const failed = $derived(!fleet.loaded && fleet.error !== null);
  const others = $derived(prefs.otherRunners);
  const capacity = $derived(fleet.stats?.hosts);
  const capacitySummary = $derived(
    !fleet.loaded
      ? 'Loading capacity'
      : !capacity
        ? 'Capacity unavailable'
        : `${pluralise(capacity.healthy ?? 0, 'connected host')} · ${capacity.used ?? 0} of ${capacity.capacity ?? 0} slots used`,
  );
  const timingSummary = $derived(
    !fleet.loaded
      ? 'Loading timings'
      : `p50 startup ${formatDuration(fleet.stats?.p50_startup_ms || undefined)} · registration ${formatDuration(fleet.stats?.p50_registration_ms || undefined)}`,
  );
  const links = [
    { id: 'now', label: 'Now' },
    { id: 'pools', label: 'Pools' },
    { id: 'jobs', label: 'Jobs' },
    { id: 'history', label: 'History' },
    { id: 'diagnostics', label: 'Diagnostics' },
  ];

  async function reveal(id: string, focus = true): Promise<void> {
    moreOpen = false;
    const target = document.getElementById(id);
    if (!target) return;
    if (target instanceof HTMLDetailsElement) target.open = true;
    for (let parent = target.parentElement; parent; parent = parent.parentElement) {
      if (parent instanceof HTMLDetailsElement) parent.open = true;
    }
    await tick();
    target.scrollIntoView({ block: 'start' });
    jumped = { id: id === 'capacity' || id === 'startup' ? 'diagnostics' : id, y: window.scrollY };
    if (focus) {
      const heading =
        target instanceof HTMLDetailsElement ? target.querySelector('summary') : target;
      heading?.focus({ preventScroll: true });
    }
    active = id === 'capacity' || id === 'startup' ? 'diagnostics' : id;
  }

  function jump(event: MouseEvent, id: string): void {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey)
      return;
    event.preventDefault();
    if (location.hash !== `#${id}`) history.pushState(null, '', `#${id}`);
    void reveal(id);
  }

  onMount(() => {
    let frame = 0;
    function update(): void {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        if (!pageNav) return;
        // A destination near the foot cannot reach the sticky bar. Keep its
        // jump indication until the operator actually scrolls elsewhere.
        if (jumped && Math.abs(window.scrollY - jumped.y) < 1) {
          active = jumped.id;
          return;
        }
        jumped = null;
        const edge =
          pageNav.getBoundingClientRect().bottom +
          parseFloat(getComputedStyle(pageNav).marginBottom) +
          1;
        let current = 'now';
        for (const { id } of [...links.slice(0, 3), { id: 'events' }, ...links.slice(3)]) {
          if (id === 'events' && window.matchMedia('(min-width: 1181px)').matches) continue;
          const node = document.getElementById(id);
          if (node && node.getBoundingClientRect().top <= edge) current = id;
        }
        if (
          window.scrollY > 0 &&
          window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 1
        )
          current = 'diagnostics';
        active = current;
      });
    }
    const fragment = () => void reveal(location.hash.slice(1), false);
    function escape(event: KeyboardEvent): void {
      if (event.key === 'Escape' && moreOpen) {
        moreOpen = false;
        pageNav?.querySelector('button')?.focus();
      }
    }
    window.addEventListener('keydown', escape);
    window.addEventListener('scroll', update, { passive: true });
    window.addEventListener('resize', update);
    window.addEventListener('hashchange', fragment);
    update();
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener('keydown', escape);
      window.removeEventListener('scroll', update);
      window.removeEventListener('resize', update);
      window.removeEventListener('hashchange', fragment);
    };
  });
  // Native fragment scrolling happens before the asynchronous route is ready.
  $effect(() => {
    if (fleet.loaded && !setupPending && location.hash)
      void tick().then(() => reveal(location.hash.slice(1), false));
  });
</script>

<div class="overview">
  <PageHeader
    title="Overview"
    subtitle="What the fleet is doing, and what needs you."
    onrefresh={() =>
      Promise.all([fleet.reconcile(), activity?.reload(), failures?.reload()]).then(
        () => undefined,
      )}
  >
    <!--
      The connection used to be said here as well as in the top bar's dot,
      which is 100px above it and says the same thing in the vocabulary every
      other page uses. One status, said once; the stale banner below is what
      raises its voice when it matters.
    -->
    <div class="scope">
      <Select
        ariaLabel="Overview scope"
        value={others ? 'all' : 'fleet'}
        options={[
          { value: 'fleet', label: 'This fleet' },
          { value: 'all', label: 'Include other runners' },
        ]}
        onchange={(value) => (prefs.otherRunners = value === 'all')}
      />
    </div>
  </PageHeader>
  {#if failed}
    <ErrorState
      error={fleet.error}
      title="The fleet could not be loaded"
      onretry={() => void fleet.reconcile()}
    />
  {:else}
    <nav class="page-nav" aria-label="Overview sections" bind:this={pageNav}>
      {#each links as link (link.id)}
        {#if link.id !== 'history' || !setupPending}
          <a
            href="#{link.id}"
            class:secondary={link.id === 'history' || link.id === 'diagnostics'}
            aria-current={active === link.id ? 'location' : undefined}
            onclick={(event) => jump(event, link.id)}>{link.label}</a
          >
        {/if}
      {/each}
      <div class="more">
        <button
          type="button"
          aria-label="More overview sections"
          aria-current={['events', 'history', 'diagnostics'].includes(active)
            ? 'location'
            : undefined}
          aria-expanded={moreOpen}
          aria-controls="overview-more"
          class:current={['events', 'history', 'diagnostics'].includes(active)}
          onclick={() => (moreOpen = !moreOpen)}
          onkeydown={(event) => {
            if (event.key === 'Escape') moreOpen = false;
          }}>More <ChevronDown size={14} aria-hidden="true" /></button
        >
        {#if moreOpen}
          <div
            id="overview-more"
            class="more-links"
            role="group"
            aria-label="More overview sections"
          >
            {#each [{ id: 'events', label: 'Events' }, ...links.slice(3)] as link (link.id)}
              {#if link.id !== 'history' || !setupPending}<a
                  href="#{link.id}"
                  aria-current={active === link.id ? 'location' : undefined}
                  onclick={(event) => jump(event, link.id)}>{link.label}</a
                >{/if}
            {/each}
          </div>
        {/if}
      </div>
    </nav>
    <FleetStatus {setupPending} />
    <div class="stack">
      <section id="now" tabindex="-1" aria-label="Fleet now" class="now">
        <FirstRun onpending={(pending) => (setupPending = pending)} />
        {#if fleet.loaded && (fleet.connection !== 'live' || fleet.error)}<p
            class="stale"
            role="status"
          >
            {fleet.error ? 'The last refresh failed.' : 'Live updates are interrupted.'} Showing the last
            known figures; they may be out of date.
          </p>{/if}
        <div class="attention">
          <div class="problems" class:in-hero={heroSaysIt}>
            <ProblemsSummary {loading} {setupPending} />
          </div>
          {#if !setupPending}<RecentFailures bind:this={failures} />{/if}<RepairOffers />
        </div>
      </section>
      <div class="work">
        <div class="primary">
          <div id="pools" class="destination" tabindex="-1"><PoolUtilisation {loading} /></div>
          <div id="jobs" class="destination" tabindex="-1"><ActiveJobs /></div>
        </div>
        <div id="events" class="destination" tabindex="-1"><EventsFeed {loading} /></div>
      </div>
      {#if !setupPending}<OverviewDisclosure id="history" title="History" summary={historySummary}
          ><FleetActivity
            bind:this={activity}
            onsummary={(value) => (historySummary = value)}
          /></OverviewDisclosure
        >{/if}
      <section
        id="diagnostics"
        class="diagnostics destination"
        tabindex="-1"
        aria-label="Diagnostics"
      >
        <OverviewDisclosure id="capacity" title="Host capacity" summary={capacitySummary}
          >{#if fleet.loaded}<HostCapacityMap
              hosts={fleet.hosts}
              page="overview"
              onmanage={() => navigate('/hosts')}
            />{/if}</OverviewDisclosure
        >
        <OverviewDisclosure
          id="startup"
          title="Startup and registration"
          summary={timingSummary}
          class="performance"><StartupTimings {loading} /></OverviewDisclosure
        >
      </section>
    </div>
  {/if}
</div>

<style>
  .overview :global(.page-header) {
    margin-bottom: var(--z-space-3);
  }
  /*
    On a phone the subtitle is what stands between the heading and the first
    pool. The spacing around the rule and the subtitle tightens, so the pools
    still begin on the first screen above the bottom navigation.
  */
  @media (max-width: 768px) {
    .overview :global(.page-header) {
      gap: var(--z-space-1);
      margin-bottom: var(--z-nudge-2);
    }
    .overview :global(.page-header .titles) {
      display: grid;
      grid-template-columns: auto 1fr;
      align-items: baseline;
      column-gap: var(--z-space-3);
    }
    .overview :global(.page-header .titles h1) {
      grid-column: 1;
      grid-row: 1;
    }
    .overview :global(.page-header .titles .rule) {
      grid-column: 1 / -1;
      grid-row: 2;
      margin-top: var(--z-nudge-2);
    }
    .overview :global(.page-header .titles .subtitle) {
      grid-column: 1 / -1;
      grid-row: 3;
      margin-top: var(--z-nudge-2);
    }
    .overview :global(.page-header .titles .meta) {
      grid-column: 2;
      grid-row: 1;
      margin-top: 0;
    }
    .overview :global(.page-nav) {
      margin-bottom: var(--z-nudge-2);
    }
    .overview .now {
      gap: var(--z-space-2);
    }
  }
  .scope {
    max-width: 100%;
  }
  .page-nav {
    display: flex;
    align-items: center;
    position: sticky;
    top: var(--z-topbar-height);
    z-index: calc(var(--z-layer-sticky) - 1);
    min-height: var(--z-control-touch);
    margin-bottom: var(--z-space-3);
    border-bottom: var(--z-border-width) solid var(--z-border);
    background: var(--z-bg);
  }
  .page-nav a,
  .page-nav button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--z-space-1);
    min-height: var(--z-control-touch);
    padding: var(--z-space-2) var(--z-space-3);
    border: 0;
    border-bottom: var(--z-nudge-2) solid transparent;
    background: none;
    color: var(--z-text-muted);
    font: inherit;
    font-size: var(--z-text-sm);
    text-decoration: none;
    cursor: pointer;
  }
  .page-nav a:hover,
  .page-nav button:hover {
    color: var(--z-accent);
    background: var(--z-surface-hover);
  }
  .page-nav a[aria-current],
  .page-nav button.current {
    color: var(--z-accent);
    border-bottom-color: var(--z-accent);
  }
  .more {
    display: none;
    position: relative;
  }
  .more-links {
    position: absolute;
    top: 100%;
    right: 0;
    display: flex;
    flex-direction: column;
    min-width: 10rem;
    padding: var(--z-space-1);
    border: var(--z-border-width) solid var(--z-border-strong);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
    box-shadow: var(--z-shadow-md);
  }
  .more-links a {
    justify-content: flex-start;
  }
  .stack,
  .now,
  .primary,
  .diagnostics,
  .attention {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    min-width: 0;
  }
  .attention:empty {
    display: none;
  }
  @media (min-width: 769px) {
    .problems.in-hero {
      display: none;
    }
  }
  .work {
    display: grid;
    grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
    /* The events column is as tall as Pools and Active jobs beside it, so the two
       columns end on one line instead of the shorter one trailing off. */
    align-items: stretch;
    gap: var(--z-space-4);
  }
  .destination,
  .now {
    min-width: 0;
    scroll-margin-top: calc(var(--z-topbar-height) + var(--z-control-touch) + var(--z-space-3));
  }
  .stale {
    margin: 0;
    padding: var(--z-space-2) var(--z-space-3);
    border-left: var(--z-border-width-rail) solid var(--z-pending);
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
  }
  @media (pointer: coarse) {
    .scope :global(select) {
      min-height: var(--z-control-touch);
    }
  }
  /* The jump bar is for the screens that scroll to reach what is below: on a wide
     screen Now, Pools and Jobs are on one screen and History and Diagnostics are
     the next thing under them, so the bar is a row of tabs that go nowhere. */
  @media (min-width: 1181px) {
    .page-nav {
      display: none;
    }
  }
  @media (min-width: 1181px) {
    #events {
      display: flex;
      flex-direction: column;
    }
    #events > :global(.panel) {
      flex: 1;
    }
  }
  @media (max-width: 1180px) {
    .work {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  @media (max-width: 768px) {
    .page-nav .secondary {
      display: none;
    }
    .more {
      display: block;
    }
    .overview :global(.page-header .row) {
      gap: var(--z-space-2);
    }
    .overview :global(.page-header .actions) {
      width: auto;
    }
  }
</style>
