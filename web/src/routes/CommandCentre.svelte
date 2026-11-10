<!--
  The command centre: the whole fleet on one screen, to be watched rather than
  worked through.

  It is an immersive route, so the shell gives it no sidebar, top bar or footer
  and this bar is the page's only chrome. Under it are the hero, the deck with
  its rail, and the ticker; this owns the bar, full screen, the way out, and
  whether the page is live.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { Maximize, Minimize, X } from '@lucide/svelte';
  import { events } from '$lib/api/sse';
  import type { Job, Problem } from '$lib/api/types';
  import Button from '$lib/components/Button.svelte';
  import Logo from '$lib/components/Logo.svelte';
  import AttentionList from '$lib/command/AttentionList.svelte';
  import FinishedList from '$lib/command/FinishedList.svelte';
  import Hero from '$lib/command/Hero.svelte';
  import HostLane from '$lib/command/HostLane.svelte';
  import Ticker from '$lib/command/Ticker.svelte';
  import WaitingList from '$lib/command/WaitingList.svelte';
  import { clock } from '$lib/command/clock.svelte';
  import { fullscreen } from '$lib/command/fullscreen.svelte';
  import { commandJobs } from '$lib/command/jobs.svelte';
  import { FINISHED_WINDOW_MS, waitingSummary } from '$lib/command/jobs-merge';
  import { lanes } from '$lib/command/lanes';
  import { completedSinceHref } from '$lib/command/links';
  import { verdict, type VerdictInput } from '$lib/command/verdict';
  import { onClockTick } from '$lib/format';
  import { layers } from '$lib/keys';
  import { router } from '$lib/router';
  import JobDrawer from '$lib/jobs/JobDrawer.svelte';
  import { fleet } from '$lib/state/fleet.svelte';
  import StatusDot from '$lib/components/StatusDot.svelte';
  import { jobStatus } from '$lib/status';
  import { feed } from '$lib/state/feed.svelte';
  import { notifications } from '$lib/state/notifications.svelte';

  let now = $state(Date.now());
  onMount(() => onClockTick((at) => (now = at)));

  const live = $derived(fleet.connection === 'live');

  /*
    A wall display that has quietly stopped updating is the one failure this
    page must not have. While the stream is down the second-by-second clock
    stops, so no age or wait goes on counting from numbers nobody can vouch
    for, and the page remembers when it last heard anything. When the stream
    returns, everything the page holds is fetched again (the replay buffer is
    finite and may not cover the gap) before the clock is let go. The clock
    exists for this page alone, so it stops with it.
  */
  // The shapes the indicator borrows: busy and filled while live, pending and
  // dashed while not, so the change reads without colour.
  const LIVE = jobStatus('in_progress');
  const RECONNECTING = jobStatus('queued');

  let lastHeardAt = $state<number | null>(null);
  let wasLive = false;
  $effect(() => {
    if (live) {
      const returning = lastHeardAt !== null;
      wasLive = true;
      if (!returning) {
        clock.start();
        return;
      }
      void Promise.allSettled([fleet.reconcile(), commandJobs.retry()]).then(() => {
        if (fleet.connection === 'live') clock.start();
      });
      return;
    }
    clock.stop();
    if (wasLive) lastHeardAt = Date.now();
    wasLive = false;
  });
  onMount(() => () => clock.stop());

  // The rail's jobs live as long as the page: nothing else wants a window of
  // ten minutes, and a wall display is the page that asks for it.
  onMount(() => {
    commandJobs.start();
    return () => commandJobs.stop();
  });

  // One drawer for both lists. The open job is replaced when a frame for it
  // lands, as on the Jobs page, so a job that starts while it is open says so.
  let openJob = $state.raw<Job | null>(null);
  let drawerOpen = $state(false);
  function showJob(job: Job): void {
    openJob = job;
    drawerOpen = true;
  }
  onMount(() =>
    events.subscribe('job.updated', (job) => {
      if (openJob && job.id === openJob.id) openJob = job;
    }),
  );
  const finishedSince = $derived(completedSinceHref(new Date(clock.now - FINISHED_WINDOW_MS)));

  /*
    The deck and the hero are read from one snapshot, so the ring, the verdict
    and the lanes can never disagree about how many slots are working.
  */
  const deck = $derived(lanes(fleet.hosts, fleet.runners, commandJobs.waiting));
  const cells = $derived(deck.flatMap((lane) => lane.cells));
  const slots = $derived(cells.length);
  const busy = $derived(cells.filter((c) => c.runner?.state === 'busy').length);
  const starting = $derived(cells.filter((c) => c.kind === 'starting').length);
  const free = $derived(cells.filter((c) => c.kind === 'free').length);

  /** The name the verdict gives a problem's target: the host or pool by its name. */
  function targetName(problem: Problem | undefined): string | null {
    if (!problem?.target_id) return null;
    if (problem.target_kind === 'host') return fleet.host(problem.target_id)?.name ?? null;
    if (problem.target_kind === 'pool') return fleet.pool(problem.target_id)?.name ?? null;
    return null;
  }

  const waitingNow = $derived(waitingSummary(commandJobs.waiting, fleet.runners, free, clock.now));

  const verdictInput = $derived.by((): VerdictInput => {
    const waiting = fleet.stats?.fleet?.queued_jobs ?? fleet.stats?.queued_jobs ?? 0;
    const problems = notifications.active;
    return {
      live,
      lastHeardAt,
      // The page's own clock is frozen while the stream is down, so how long
      // ago the page last heard is counted on the ten-second one.
      now: live ? clock.now : now,
      hosts: deck.length,
      slots,
      busy,
      starting,
      waiting,
      waitingWithoutSlot: waitingNow.withoutSlot,
      longestWaitMs: waitingNow.longestWaitMs,
      waitingPool: waitingNow.pool,
      problems: problems.length,
      firstProblemTarget: targetName(problems[0]),
    };
  });
  const said = $derived(verdict(verdictInput));

  const time = $derived(
    new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' }).format(now),
  );
  const date = $derived(
    new Intl.DateTimeFormat(undefined, { weekday: 'short', day: 'numeric', month: 'short' }).format(
      now,
    ),
  );

  /**
   * Back to wherever the operator came from, or the Overview when the page was
   * opened directly. Full screen is left first: it was asked for to watch this
   * page, and the Overview filling a wall nobody asked it to is a surprise.
   */
  function exit(): void {
    void fullscreen.exit();
    router.back('/');
  }

  function onExitClick(event: MouseEvent): void {
    // A modified click is a request for a new tab or window, which the
    // browser does from the href; only a plain click is ours to handle.
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
      return;
    }
    event.preventDefault();
    exit();
  }

  /*
    Esc unwinds in one order: the browser takes the first to leave full screen
    (and never shows it to the page), the shell's layer stack takes the next
    for an open drawer or dialog and marks it handled, and only an Esc that
    nothing else wanted leaves the page. The shell's listener was installed
    before this page mounted, so it has had its turn by the time this runs.
  */
  function onKeydown(event: KeyboardEvent): void {
    if (event.key !== 'Escape' || event.defaultPrevented) return;
    if (layers.size > 0 || document.fullscreenElement) return;
    event.preventDefault();
    exit();
  }

  /*
    Full screen is asked of the document, not of this page's element. A
    browser leaves full screen when the element holding it is removed, and
    following a link from a host lane to its host page removes this page; the
    wall should stay full screen through that visit and the Back that ends it.
  */
  function toggleFullscreen(): void {
    void fullscreen.toggle(document.documentElement);
  }
</script>

<svelte:window onkeydown={onKeydown} />

<div class="command" data-live={live}>
  <header class="bar">
    <span class="brand"><Logo variant="full" size={28} label="" /></span>
    <h1 id="page-heading" tabindex="-1">Command centre</h1>
    <p class="live" data-state={live ? 'live' : 'reconnecting'} role="status">
      <span class="live-dot" aria-hidden="true"
        ><StatusDot status={live ? LIVE : RECONNECTING} size="sm" tip={false} /></span
      >
      {live ? 'Live' : 'Reconnecting'}
    </p>
    <span class="spacer"></span>
    <time class="clock" datetime={new Date(now).toISOString()}>
      <span class="time">{time}</span>
      <span class="date">{date}</span>
    </time>
    {#if fullscreen.supported}
      <Button
        variant="secondary"
        icon={fullscreen.active ? Minimize : Maximize}
        pressed={fullscreen.active}
        help={fullscreen.active
          ? 'Leave full screen. Esc does the same.'
          : 'Fill the screen with this page, and keep the display awake.'}
        onclick={toggleFullscreen}
      >
        Full screen
      </Button>
    {/if}
    <a class="exit" href="/" onclick={onExitClick}>
      <X size={16} aria-hidden="true" />
      Exit
    </a>
  </header>
  <div class="stage">
    <Hero verdict={said} stats={fleet.stats} running={busy} {busy} {slots} now={clock.now} />
    <div class="board">
      {#if deck.length > 0}
        <section class="deck" aria-labelledby="command-hosts">
          <h2 id="command-hosts" class="sr-only">Hosts</h2>
          {#each deck as lane (lane.host.id)}
            <HostLane {lane} />
          {/each}
        </section>
      {/if}
      <div class="rail waiting">
        <WaitingList
          jobs={commandJobs.waiting}
          error={commandJobs.error}
          onopen={showJob}
          onretry={() => void commandJobs.retry()}
        />
      </div>
      {#if notifications.active.length > 0}
        <div class="rail attention">
          <AttentionList problems={notifications.active} />
        </div>
      {/if}
      <div class="rail finished">
        <FinishedList
          jobs={commandJobs.finished}
          count={commandJobs.finishedCount}
          moreHref={finishedSince}
          error={commandJobs.error}
          onopen={showJob}
          onretry={() => void commandJobs.retry()}
        />
      </div>
    </div>
  </div>
  <Ticker entries={feed.entries} />
</div>

<JobDrawer bind:open={drawerOpen} job={openJob} onclose={() => (openJob = null)} />

<style>
  .command {
    display: flex;
    flex-direction: column;
    min-height: 100vh;
    background: var(--z-bg);
  }
  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-3) var(--z-space-4);
    min-height: var(--z-topbar-height);
    padding: var(--z-space-2) var(--z-space-6);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  .brand {
    display: inline-flex;
    flex: none;
    color: var(--z-text);
  }
  h1 {
    margin: 0;
    font-size: var(--z-text-lg);
    line-height: var(--z-leading-lg);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  h1:focus {
    outline: none;
  }
  .live {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    margin: 0;
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
    color: var(--z-busy);
  }
  .live-dot {
    display: inline-flex;
  }
  .live[data-state='reconnecting'] {
    color: var(--z-pending);
  }
  /* The dot breathes while the stream is up: the proof the page is live. */
  .live[data-state='live'] :global(svg) {
    animation: breathe calc(var(--z-motion-slow) * 8) var(--z-ease) infinite alternate;
  }
  @keyframes breathe {
    to {
      opacity: 0.4;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .live[data-state='live'] :global(svg) {
      animation: none;
    }
  }
  .spacer {
    flex: 1;
  }
  .clock {
    display: inline-flex;
    align-items: baseline;
    gap: var(--z-space-2);
    font-variant-numeric: tabular-nums;
  }
  .time {
    font-size: var(--z-text-lg);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .date {
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
  .exit {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    height: var(--z-space-8);
    padding: 0 var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    color: var(--z-text);
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
    text-decoration: none;
    transition:
      background-color var(--z-motion-fast) var(--z-ease),
      border-color var(--z-motion-fast) var(--z-ease);
  }
  .exit:hover {
    background: var(--z-surface-hover);
    border-color: var(--z-border-strong);
  }
  .exit:focus-visible {
    outline: none;
    box-shadow: var(--z-focus-ring);
  }
  .stage {
    flex: 1;
  }
  /* The lanes on the left, the rail on the right. On a phone they stack in
     the order a glance wants: what waits and what is wrong before the hosts,
     and what finished after them. */
  .board {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 20rem);
    grid-template-areas:
      'deck waiting'
      'deck attention'
      'deck finished';
    grid-template-rows: auto auto 1fr;
    align-items: start;
    gap: var(--z-space-4) var(--z-space-6);
    padding: var(--z-space-6);
  }
  /* Two small hosts share a row; a lane with more slots than one row draws
     spans both, so the deck grows downwards as the fleet does. */
  .deck {
    grid-area: deck;
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    align-items: start;
    gap: var(--z-space-4);
  }
  .rail.waiting {
    grid-area: waiting;
  }
  .rail.attention {
    grid-area: attention;
  }
  .rail.finished {
    grid-area: finished;
  }
  /* The rail's three lists share one look; each owns only what is its own. */
  .rail :global(.rail-section) {
    display: grid;
    gap: var(--z-space-2);
    min-width: 0;
  }
  .rail :global(.rail-section > h2) {
    display: flex;
    align-items: baseline;
    gap: var(--z-space-2);
    margin: 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .rail :global(.count) {
    color: var(--z-text-muted);
    font-variant-numeric: tabular-nums;
  }
  .rail :global(.rail-section > ul) {
    display: grid;
    gap: var(--z-space-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .rail :global(.row) {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    box-sizing: border-box;
    width: 100%;
    min-width: 0;
    padding: var(--z-space-2);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface-raised);
    color: var(--z-text);
    font: inherit;
    text-align: left;
    text-decoration: none;
    cursor: pointer;
    animation: rail-arrive var(--z-motion-base) var(--z-ease);
    transition: border-color var(--z-motion-fast) var(--z-ease);
  }
  .rail :global(.row:hover),
  .rail :global(.row:focus-visible) {
    border-color: var(--z-accent);
  }
  .rail :global(.row:focus-visible),
  .rail :global(.more:focus-visible) {
    outline: none;
    box-shadow: var(--z-focus-ring);
  }
  .rail :global(.mark) {
    display: inline-flex;
    flex: none;
  }
  .rail :global(.what) {
    display: grid;
    flex: 1;
    min-width: 0;
  }
  /* Names are somebody else's text: cut, never allowed to widen the rail. */
  .rail :global(.name),
  .rail :global(.where) {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .rail :global(.name) {
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
  }
  .rail :global(.where) {
    font-family: var(--z-font-mono);
    font-size: var(--z-text-2xs);
    color: var(--z-text-muted);
  }
  .rail :global(.side) {
    display: grid;
    flex: none;
    justify-items: end;
    font-size: var(--z-text-xs);
  }
  .rail :global(.empty) {
    margin: 0;
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
  .rail :global(.more) {
    justify-self: start;
    padding: 0;
    border: 0;
    background: none;
    color: var(--z-accent);
    font: inherit;
    font-size: var(--z-text-sm);
    text-decoration: underline;
    text-underline-offset: var(--z-underline-offset);
    cursor: pointer;
  }
  @keyframes rail-arrive {
    from {
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .rail :global(.row) {
      animation: none;
    }
  }
  @media (pointer: coarse) {
    .rail :global(.row) {
      min-height: var(--z-control-touch);
    }
  }
  @media (max-width: 768px) {
    .bar {
      padding: var(--z-space-2) var(--z-space-4);
    }
    .board {
      grid-template-columns: minmax(0, 1fr);
      grid-template-areas:
        'waiting'
        'attention'
        'deck'
        'finished';
      grid-template-rows: none;
      padding: var(--z-space-4);
    }
    .deck {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
