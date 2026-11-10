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
  import Button from '$lib/components/Button.svelte';
  import Logo from '$lib/components/Logo.svelte';
  import { fullscreen } from '$lib/command/fullscreen.svelte';
  import { onClockTick } from '$lib/format';
  import { layers } from '$lib/keys';
  import { router } from '$lib/router';

  let now = $state(Date.now());
  onMount(() => onClockTick((at) => (now = at)));

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
  <div class="stage"></div>
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
  @media (max-width: 768px) {
    .bar {
      padding: var(--z-space-2) var(--z-space-4);
    }
  }
</style>
