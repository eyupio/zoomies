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
  import { Eye, Maximize, Minimize, X } from '@lucide/svelte';
  import { events } from '$lib/api/sse';
  import type { Job, Problem } from '$lib/api/types';
  import Button from '$lib/components/Button.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import Logo from '$lib/components/Logo.svelte';
  import RefreshButton from '$lib/components/RefreshButton.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
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
  import { session } from '$lib/state/session.svelte';

  let now = $state(Date.now());
  onMount(() => onClockTick((at) => (now = at)));

  const live = $derived(fleet.connection === 'live');

  /*
    A wall display that has quietly stopped updating is the one failure this
    page must not have. While the stream is down the second-by-second clock
    stops, so no age or wait goes on counting from numbers nobody can vouch
    for, and the page remembers when it last heard anything: the stream's
    last frame or heartbeat, which can be well before the drop was noticed,
    and never earlier than the moment the page itself went live. When the
    stream returns, everything the page holds is fetched again (the replay
    buffer is finite and may not cover the gap) before the clock is let go.
    The clock exists for this page alone, so it stops with it.
  */
  // The shapes the indicator borrows: busy and filled while live, pending and
  // dashed while not, so the change reads without colour.
  const LIVE = jobStatus('in_progress');
  const RECONNECTING = jobStatus('queued');

  let lastHeardAt = $state<number | null>(null);
  let wasLive = false;
  let liveSince = 0;
  // Set when the page goes, so a refetch that answers after it cannot start
  // a clock nothing will ever stop.
  let disposed = false;
  $effect(() => {
    if (live) {
      const returning = lastHeardAt !== null;
      if (!wasLive) liveSince = Date.now();
      wasLive = true;
      if (!returning) {
        clock.start();
        return;
      }
      void Promise.allSettled([fleet.reconcile(), commandJobs.retry()]).then(() => {
        if (!disposed && fleet.connection === 'live') clock.start();
      });
      return;
    }
    clock.stop();
    if (wasLive) lastHeardAt = Math.max(events.heardAt ?? 0, liveSince);
    wasLive = false;
  });
  onMount(() => () => {
    disposed = true;
    clock.stop();
  });

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
  // The free cell stands for every free slot on its host, so slots are
  // counted from the lanes rather than from the cells drawn.
  const slots = $derived(deck.reduce((sum, lane) => sum + lane.slots, 0));
  const busy = $derived(cells.filter((c) => c.runner?.state === 'busy').length);
  const starting = $derived(cells.filter((c) => c.kind === 'starting').length);
  const free = $derived(deck.reduce((sum, lane) => sum + lane.free, 0));

  /** The name the verdict gives a problem's target: the host or pool by its name. */
  function targetName(problem: Problem | undefined): string | null {
    if (!problem?.target_id) return null;
    if (problem.target_kind === 'host') return fleet.host(problem.target_id)?.name ?? null;
    if (problem.target_kind === 'pool') return fleet.pool(problem.target_id)?.name ?? null;
    return null;
  }

  const waitingNow = $derived(waitingSummary(commandJobs.waiting, fleet.runners, free, clock.now));

  const verdictInput = $derived.by((): VerdictInput => {
    // The same count the Waiting heading shows once the list has answered, so
    // the two never disagree; the stats figure stands in only until then.
    const listed = commandJobs.loaded && !commandJobs.error;
    const waiting = listed
      ? commandJobs.waitingCount
      : (fleet.stats?.fleet?.queued_jobs ?? fleet.stats?.queued_jobs ?? 0);
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

  /*
    The assistant's button is fixed in the window's bottom-right corner while
    the page is not full screen, and it cannot be pressed through. The shell's
    page padding is what keeps every other page clear of it; this page is its
    own chrome, so it keeps the corner itself, on the same condition the shell
    draws the button on.
  */
  const assistantShown = $derived(session.can('viewer') && !fullscreen.active);

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

  /*
    The stream keeps this page current, but a wall display is the page most
    likely to have sat through a closed lid or a controller restart, so it
    owes the same way to settle that as every other page: everything it reads,
    read again. The lists and the chart report their own failures in place,
    so one that fails does not hold the others up. When every one of them
    failed, the refresh did not happen at all, and the page says so the way
    every page does, by throwing to the one refresh: a toast, and a control
    that does not claim it refreshed.
  */
  let hero = $state<ReturnType<typeof Hero>>();
  async function refreshPage(): Promise<void> {
    const [, , chart] = await Promise.allSettled([
      fleet.reconcile(),
      commandJobs.retry(),
      hero?.reloadChart() ?? Promise.resolve(true),
    ]);
    const chartFailed = chart.status === 'rejected' || !chart.value;
    // reconcile() and retry() record a failure rather than throw it.
    if (fleet.error && commandJobs.error && chartFailed) throw fleet.error;
  }
</script>

<svelte:window onkeydown={onKeydown} />

<div class="command" data-live={live} data-assistant={assistantShown}>
  <header class="bar">
    <span class="brand"><Logo variant="full" size={28} label="" /></span>
    <h1 id="page-heading" tabindex="-1">Command centre</h1>
    <p class="live" data-state={live ? 'live' : 'reconnecting'} role="status">
      <span class="live-dot" aria-hidden="true"
        ><StatusDot status={live ? LIVE : RECONNECTING} size="sm" tip={false} /></span
      >
      {live ? 'Live' : 'Reconnecting'}
    </p>
    <!-- The shell's visitor notice is chrome this page does without, but what
         it says is not: a visitor is told the view is public and only reads,
         and is given the one thing they can do about it. -->
    {#if session.visitor}
      <p class="visitor" role="note">
        <Eye size={14} aria-hidden="true" />
        <span>Public, read-only view</span>
        <a class="signin" href="/login">Sign in</a>
      </p>
    {/if}
    <span class="spacer"></span>
    <time class="clock" datetime={new Date(now).toISOString()}>
      <span class="time">{time}</span>
      <span class="date">{date}</span>
    </time>
    <!-- First of the bar's controls, as PageHeader puts it on every other page. -->
    <RefreshButton onrefresh={refreshPage} />
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
    <Button variant="secondary" icon={X} href="/" onclick={onExitClick}>Exit</Button>
  </header>
  <div class="stage">
    {#if fleet.loaded || !fleet.error}
      <Hero
        bind:this={hero}
        verdict={said}
        stats={fleet.stats}
        running={busy}
        {busy}
        {slots}
        lanes={deck}
        now={clock.now}
        loading={!fleet.loaded}
      />
    {/if}
    <!-- In the order a phone stacks them, which is also the order a screen
         reader and Tab meet them: what waits and what is wrong before the
         hosts, and what finished after them. At desktop width the deck is laid
         on the left, beside the rail rather than in its grid, so that each
         scrolls by itself; the frame is what the deck is placed against. -->
    <div class="frame">
      <div class="board">
        <div class="rail waiting">
          <WaitingList
            jobs={commandJobs.waiting}
            count={commandJobs.waitingCount}
            loading={!commandJobs.loaded}
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
        {#if !fleet.loaded}
          <section
            class="deck"
            aria-labelledby="command-hosts"
            aria-busy={!fleet.error}
            data-testid="command-deck"
          >
            <h2 id="command-hosts" class="sr-only">Hosts</h2>
            {#if fleet.error}
              <div class="deck-error">
                <ErrorState
                  compact
                  error={fleet.error}
                  title="The hosts did not load."
                  onretry={() => void fleet.reconcile()}
                />
              </div>
            {:else}
              {#each [0, 1, 2, 3] as lane (lane)}
                <div class="lane-skeleton" aria-hidden="true">
                  <Skeleton width="40%" height="var(--z-text-sm)" />
                  <div class="cells-skeleton">
                    <Skeleton height="var(--z-command-tile-height)" radius="md" />
                    <Skeleton height="var(--z-command-tile-height)" radius="md" />
                  </div>
                </div>
              {/each}
            {/if}
          </section>
        {:else if deck.length > 0}
          <section class="deck" aria-labelledby="command-hosts" data-testid="command-deck">
            <h2 id="command-hosts" class="sr-only">Hosts</h2>
            {#each deck as lane (lane.host.id)}
              <HostLane {lane} />
            {/each}
          </section>
        {/if}
        <div class="rail finished">
          <FinishedList
            jobs={commandJobs.finished}
            count={commandJobs.finishedCount}
            moreHref={finishedSince}
            loading={!commandJobs.loaded}
            error={commandJobs.error}
            onopen={showJob}
            onretry={() => void commandJobs.retry()}
          />
        </div>
      </div>
    </div>
  </div>
  <Ticker entries={feed.entries} clearAssistant={assistantShown} />
</div>

<JobDrawer bind:open={drawerOpen} job={openJob} onclose={() => (openJob = null)} />

<style>
  /*
    At desktop width the page is the window and never more: the bar and the
    hero keep their own height at the top, the ticker is pinned at the foot,
    and the deck and the rail share what is left, each scrolling inside
    itself. A page that scrolled as a document would carry the hero away and
    the ticker off the bottom, and a wall display is not looked at with a
    scroll wheel.
  */
  .command {
    display: flex;
    flex-direction: column;
    height: 100dvh;
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
  /* Information about the view, not about the fleet: the accent, as the
     shell's own visitor notice uses it. */
  .visitor {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    margin: 0;
    padding: var(--z-space-1) var(--z-space-3);
    border: var(--z-border-width) solid var(--z-accent-border);
    border-radius: var(--z-radius-md);
    background: var(--z-accent-subtle);
    color: var(--z-text);
    font-size: var(--z-text-sm);
  }
  .visitor :global(svg) {
    flex: none;
    color: var(--z-accent);
  }
  .signin {
    display: inline-flex;
    align-items: center;
    color: var(--z-accent);
    font-weight: var(--z-weight-medium);
    text-decoration: underline;
    text-underline-offset: var(--z-underline-offset);
  }
  .signin:hover {
    color: var(--z-accent-hover);
  }
  .signin:focus-visible {
    outline: none;
    border-radius: var(--z-radius-sm);
    box-shadow: var(--z-focus-ring);
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
  /* No `min-height: 0` here: the stage may not shrink below the hero and the
     frame's least height, so a window too short for them pushes the ticker
     down, after the frame, and the document scrolls. A stage allowed to
     shrink further left the frame spilling out of it over the ticker, and the
     deck, placed from the frame, painted its lanes on top. */
  .stage {
    display: flex;
    flex: 1;
    flex-direction: column;
  }
  /* What the deck is placed against. It never shrinks to nothing: on a
     window too short for the bar, the hero and the ticker, the page gives up
     fitting and scrolls rather than drawing a deck no lane can be seen in.
     Its size is the flex layout's, never its content's (the rail's whole
     length would otherwise be its least height), so 12rem is its floor. The
     gutter is the board's padding, which the deck's inset repeats. */
  .frame {
    --command-gutter: var(--z-space-6);
    position: relative;
    flex: 1;
    min-height: 12rem;
    contain: size;
  }
  /* The rail on the right, scrolling in the board, which is the rail's
     scroller and nothing else's: the deck is placed from the frame, outside
     it, so scrolling one never moves the other. */
  .board {
    box-sizing: border-box;
    display: grid;
    grid-template-columns: minmax(0, 1fr) var(--z-command-rail);
    align-content: start;
    gap: var(--z-space-4) var(--z-space-6);
    height: 100%;
    padding: var(--command-gutter);
    overflow-x: hidden;
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  /* A wall display is looked at, and a scrollbar on it is chrome that reads as
     a fault. The deck and the rail still scroll by wheel, touch and keyboard
     when a fleet is larger than the screen; only the bar is not drawn. */
  .board,
  .deck {
    scrollbar-width: none;
  }
  .board::-webkit-scrollbar,
  .deck::-webkit-scrollbar {
    display: none;
  }
  /* The assistant's button sits over the foot of the rail, and a wall display
     is looked at rather than scrolled, so a row merely scrollable out from
     under it is not enough: the rail's scroller stops short of it. The
     button stands its gap from the window's foot, which the ticker under the
     frame fills, and its own height above that; the scroller stops that
     height and a little air above the frame's foot. The deck is placed from
     the frame, so it keeps its full height. This holds while the ticker is
     at least about 32px tall (the button's gap less that air); it is 41px
     with entries and 37px empty. */
  [data-assistant='true'] .board {
    height: calc(100% - var(--z-eli-launcher) - var(--z-space-2));
  }
  .rail {
    grid-column: 2;
    min-width: 0;
  }
  /* One host to a row, each the deck's full width, so the lanes share edges
     and their tiles share columns: the eye runs down the page instead of
     hunting for where a lane ended. A fleet with many hosts grows the deck
     downwards, inside its own scroll. Two lanes abreast left a blank block
     beside any lane that wrapped. The inset leaves room for a focus ring the
     scroll would otherwise cut. */
  .deck {
    position: absolute;
    inset: calc(var(--command-gutter) - var(--z-space-1))
      calc(var(--command-gutter) * 2 + var(--z-command-rail) - var(--z-space-1))
      calc(var(--command-gutter) - var(--z-space-1)) calc(var(--command-gutter) - var(--z-space-1));
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    align-items: start;
    align-content: start;
    gap: var(--z-space-4);
    padding: var(--z-space-1);
    overflow-x: hidden;
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  /* The loading shape of a lane: its header and a row of tiles at the tiles'
     own height, so the deck does not jump when the hosts arrive. */
  .lane-skeleton {
    display: grid;
    gap: var(--z-space-2);
    padding: var(--z-space-3);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-lg);
    background: var(--z-surface);
  }
  .cells-skeleton {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--z-space-2);
  }
  .deck-error {
    grid-column: 1 / -1;
  }
  @media (max-width: 768px) {
    .bar {
      padding: var(--z-space-2) var(--z-space-4);
    }
    /* A phone is pressed with a finger, whatever pointer it reports: every
       link and button on the page is a whole fingertip tall. */
    .command :global(a[href]),
    .command :global(button) {
      min-height: var(--z-control-touch);
    }
    /* Stacked, the page is a document again and scrolls as one. */
    .command {
      height: auto;
      min-height: 100vh;
    }
    .stage,
    .frame {
      display: block;
      position: static;
      min-height: 0;
      contain: none;
    }
    .board {
      grid-template-columns: minmax(0, 1fr);
      height: auto;
      padding: var(--z-space-4);
      overflow: visible;
    }
    /* A document scrolls under the button, as every page does; its end is
       kept clear of it, so the last row can be scrolled out from under it. */
    [data-assistant='true'] .board {
      height: auto;
      padding-bottom: var(--z-eli-clearance);
    }
    .rail {
      grid-column: auto;
    }
    .deck {
      position: static;
      grid-template-columns: minmax(0, 1fr);
      padding: 0;
      overflow: visible;
    }
  }
</style>
