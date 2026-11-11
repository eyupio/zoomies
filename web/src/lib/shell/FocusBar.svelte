<!--
  The bar a table's focus view draws in place of the shell.

  Focus shows one table and nothing else, so the sidebar, the top bar and the
  footer go, and this is what is left of them: the page's name, whether the
  page is live, Refresh, Full screen where the browser offers it, and the way
  back. It is the command centre's bar, cut down to what a list needs.
-->
<script lang="ts">
  import { Eye, Maximize, Minimize, X } from '@lucide/svelte';
  import Button from '../components/Button.svelte';
  import RefreshButton from '../components/RefreshButton.svelte';
  import StatusDot from '../components/StatusDot.svelte';
  import { fullscreen } from '../command/fullscreen.svelte';
  import { isTypingTarget, layers } from '../keys';
  import { router } from '../router';
  import { fleet } from '../state/fleet.svelte';
  import { refresh } from '../state/refresh.svelte';
  import { session } from '../state/session.svelte';
  import { jobStatus } from '../status';
  import { focusAddress } from '../table-focus';

  const live = $derived(fleet.connection === 'live');
  // The shapes the command centre's indicator borrows: busy and filled while
  // live, pending and dashed while not, so the change reads without colour.
  const LIVE = jobStatus('in_progress');
  const RECONNECTING = jobStatus('queued');

  /** The same address without the focus parameter: filters, sort and paging kept. */
  const exitHref = $derived(focusAddress(router.pathname, router.search, null));

  function exit(): void {
    void fullscreen.exit();
    router.navigate(exitHref);
  }

  function onExitClick(event: MouseEvent): void {
    // A modified click asks for a new tab or window, which the browser does
    // from the href; only a plain click is ours to handle.
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
      return;
    }
    event.preventDefault();
    exit();
  }

  /*
    Esc unwinds in one order, as on the command centre: the browser takes the
    first to leave full screen (and never shows it to the page), the shell's
    layer stack takes the next for an open menu, drawer or dialog and marks it
    handled, and only an Esc nothing else wanted leaves focus. The shell's
    listener was installed before this bar mounted, so it has had its turn.
  */
  function onKeydown(event: KeyboardEvent): void {
    if (event.key !== 'Escape' || event.defaultPrevented) return;
    if (layers.size > 0 || document.fullscreenElement) return;
    // Esc in the card's search box is the field's: it clears or leaves the
    // text, and an operator halfway through a search must not lose the view.
    if (isTypingTarget(event.target)) return;
    event.preventDefault();
    exit();
  }

  /*
    Asked of the document rather than the table: a browser leaves full screen
    when the element holding it is removed, and opening a runner from a row
    removes the list. The wall stays full screen through that visit and the
    Back that ends it.
  */
  function toggleFullscreen(): void {
    void fullscreen.toggle(document.documentElement);
  }
</script>

<svelte:window onkeydown={onKeydown} />

<header class="focus-bar" data-testid="focus-bar">
  <h1 id="focus-heading" tabindex="-1">{router.title}</h1>
  <p class="live" data-state={live ? 'live' : 'reconnecting'} role="status">
    <span class="live-dot" aria-hidden="true"
      ><StatusDot status={live ? LIVE : RECONNECTING} size="sm" tip={false} /></span
    >
    {live ? 'Live' : 'Reconnecting'}
  </p>
  <!-- The shell's visitor notice is chrome this view does without, but what it
       says is not: a visitor is told the view is public and only reads. -->
  {#if session.visitor}
    <p class="visitor" role="note">
      <Eye size={14} aria-hidden="true" />
      <span>Public, read-only view</span>
    </p>
  {/if}
  <span class="spacer"></span>
  <div class="controls">
    <!-- The page's own refresh, which its hidden header registered: the
         button and `R` still run the one handler. -->
    {#if refresh.available}<RefreshButton class="focus-bar-control" />{/if}
    {#if fullscreen.supported}
      <Button
        variant="secondary"
        icon={fullscreen.active ? Minimize : Maximize}
        pressed={fullscreen.active}
        help={fullscreen.active
          ? 'Leave full screen. Esc does the same.'
          : 'Fill the screen with this table, and keep the display awake.'}
        class="focus-bar-control"
        onclick={toggleFullscreen}
      >
        Full screen
      </Button>
    {/if}
    <Button
      variant="secondary"
      icon={X}
      href={exitHref}
      help="Back to the whole page, with the same filters."
      keys={['Esc']}
      class="focus-bar-control"
      onclick={onExitClick}
    >
      Exit focus
    </Button>
  </div>
</header>

<style>
  .focus-bar {
    display: flex;
    flex: none;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-2) var(--z-space-4);
    min-height: var(--z-topbar-height);
    padding: var(--z-space-2) var(--z-space-4);
    border-bottom: var(--z-border-width) solid var(--z-border);
    background: var(--z-surface);
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
  .visitor {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    margin: 0;
    padding: var(--z-space-1) var(--z-space-3);
    border: var(--z-border-width) solid var(--z-accent-border);
    border-radius: var(--z-radius-md);
    background: var(--z-accent-subtle);
    color: var(--z-accent);
    font-size: var(--z-text-xs);
  }
  .spacer {
    flex: 1;
  }
  .controls {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-2);
  }
  /* A finger's target on a phone or a tablet, as everywhere else in the product. */
  @media (max-width: 768px), (pointer: coarse) {
    .controls :global(.focus-bar-control) {
      min-height: var(--z-control-touch);
    }
  }
  @media (max-width: 768px) {
    .focus-bar {
      padding: var(--z-space-2) var(--z-space-3);
    }
    /* The controls take a line of their own under the name, rather than
       pushing it off the edge. */
    .spacer {
      display: none;
    }
    .controls {
      width: 100%;
    }
  }
</style>
