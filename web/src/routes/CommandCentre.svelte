<!--
  The command centre: the whole fleet on one screen, to be watched rather than
  worked through.

  It is an immersive route, so the shell gives it no sidebar, top bar or footer
  and this bar is the page's only chrome. The bands under it arrive with the
  tasks that build them; this owns the bar, full screen and the way out.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { Maximize, Minimize, X } from '@lucide/svelte';
  import type { Problem } from '$lib/api/types';
  import Button from '$lib/components/Button.svelte';
  import Logo from '$lib/components/Logo.svelte';
  import Hero from '$lib/command/Hero.svelte';
  import HostLane from '$lib/command/HostLane.svelte';
  import { clock } from '$lib/command/clock.svelte';
  import { fullscreen } from '$lib/command/fullscreen.svelte';
  import { lanes } from '$lib/command/lanes';
  import { verdict, type VerdictInput } from '$lib/command/verdict';
  import { onClockTick } from '$lib/format';
  import { layers } from '$lib/keys';
  import { router } from '$lib/router';
  import { fleet } from '$lib/state/fleet.svelte';
  import { notifications } from '$lib/state/notifications.svelte';

  let now = $state(Date.now());
  onMount(() => onClockTick((at) => (now = at)));

  // One second-by-second clock for every running age on the page, and none
  // once the page has gone: a wall display is the only reader that needs it.
  onMount(() => {
    clock.start();
    return () => clock.stop();
  });

  const live = $derived(fleet.connection === 'live');

  /*
    The deck and the hero are read from one snapshot, so the ring, the verdict
    and the lanes can never disagree about how many slots are working.
    Task 7 brings the queued-jobs list; until then the lanes are given none,
    and no lane says that jobs wait for it.
  */
  const deck = $derived(lanes(fleet.hosts, fleet.runners, []));
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

  const verdictInput = $derived.by((): VerdictInput => {
    const waiting = fleet.stats?.fleet?.queued_jobs ?? fleet.stats?.queued_jobs ?? 0;
    const problems = notifications.active;
    return {
      live,
      // Task 8 records when the stream last delivered anything.
      lastHeardAt: null,
      now: clock.now,
      hosts: deck.length,
      slots,
      busy,
      starting,
      waiting,
      // Task 7 replaces these three with the queued-jobs list. From the stats
      // alone the best that can be said is that jobs wait and no slot on any
      // host is free; how long and for which pool needs the jobs themselves.
      waitingWithoutSlot: free === 0 ? waiting : 0,
      longestWaitMs: 0,
      waitingPool: null,
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

<div class="command">
  <header class="bar">
    <span class="brand"><Logo variant="full" size={28} label="" /></span>
    <h1 id="page-heading" tabindex="-1">Command centre</h1>
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
    <Hero verdict={said} stats={fleet.stats} running={busy} {busy} {slots} {live} now={clock.now} />
    {#if deck.length > 0}
      <section class="deck" aria-labelledby="command-hosts">
        <h2 id="command-hosts" class="sr-only">Hosts</h2>
        {#each deck as lane (lane.host.id)}
          <HostLane {lane} />
        {/each}
      </section>
    {/if}
  </div>
</div>

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
  /* Two small hosts share a row; a lane with more slots than one row draws
     spans both, so the deck grows downwards as the fleet does. */
  .deck {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    align-items: start;
    gap: var(--z-space-4);
    padding: var(--z-space-6);
  }
  @media (max-width: 768px) {
    .bar {
      padding: var(--z-space-2) var(--z-space-4);
    }
    .deck {
      grid-template-columns: minmax(0, 1fr);
      padding: var(--z-space-4);
    }
  }
</style>
