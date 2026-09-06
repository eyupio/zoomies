<!--
  The phone's side menu: every section, named, over the page.

  The bar along the bottom edge carries four of the ten sections, which is as
  many as fit a screen as labelled targets. This is where the other six live,
  and where the masthead a phone otherwise never sees goes -- so the menu
  answers both "where else can I go" and "what am I looking at".

  It behaves like the rest of the overlays: a modal layer, Escape closes it, the
  page behind it is inert, focus is trapped and given back. It slides from the
  left because that is the edge a navigation lives on when there is room for
  one, and it closes itself the moment a section is chosen -- nobody wants to
  dismiss a menu they have already used.
-->
<script lang="ts">
  import { X } from '@lucide/svelte';
  import { layers, lockScroll, pageInert, trapFocus } from '../keys';
  import { router } from '../router';
  import { viewport } from '../state/viewport.svelte';
  import IconButton from '../components/IconButton.svelte';
  import Logo from '../components/Logo.svelte';
  import { SECTIONS, isCurrentSection } from './sections';

  interface Props {
    open?: boolean;
  }

  let { open = $bindable(false) }: Props = $props();

  let backdrop = $state<HTMLDivElement | null>(null);

  function close(): void {
    open = false;
  }

  function isCurrent(path: string): boolean {
    return isCurrentSection(path, router.pathname);
  }

  // A window that has grown past the phone breakpoint has its sidebar back, and
  // a menu floating over that is a second navigation saying the same thing.
  $effect(() => {
    if (!viewport.phone) open = false;
  });

  $effect(() => {
    if (!open) return;
    const layer = layers.push('drawer', close);
    const unlock = lockScroll();
    const uninert = pageInert(backdrop);
    return () => {
      layers.remove(layer);
      unlock();
      uninert();
    };
  });
</script>

{#if open}
  <div class="backdrop" bind:this={backdrop}>
    <!-- A div, not a focusable-but-aria-hidden button. See Dialog.svelte. -->
    <div class="scrim" aria-hidden="true" onclick={close}></div>
    <div class="panel" role="dialog" aria-modal="true" aria-label="All sections" use:trapFocus>
      <header>
        <a href="/" class="mark" onclick={close}>
          <Logo variant="mark" size={28} label="" />
          <span class="name">Zoomies</span>
        </a>
        <IconButton icon={X} label="Close the menu" size="sm" onclick={close} />
      </header>

      <!--
        Named for what it is rather than "Sections", which the bottom bar
        already answers to: two navigation landmarks sharing one name is two
        landmarks a screen-reader user cannot tell apart.
      -->
      <nav aria-label="All sections">
        <ul>
          {#each SECTIONS as item (item.path)}
            {@const current = isCurrent(item.path)}
            <li>
              <a
                href={item.path}
                aria-current={current ? 'page' : undefined}
                class:current
                onclick={close}
              >
                <item.icon size={18} aria-hidden="true" />
                <span>{item.label}</span>
              </a>
            </li>
          {/each}
        </ul>
      </nav>
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: var(--z-layer-drawer);
    display: flex;
    justify-content: flex-start;
  }
  .scrim {
    position: absolute;
    inset: 0;
    /* A veil made from the page's own ground, so it dims in light and in dark
       without a hard-coded black that only works in one of them. */
    background: color-mix(in srgb, var(--z-bg) 72%, transparent);
    backdrop-filter: blur(3px);
    cursor: default;
  }
  .panel {
    position: relative;
    display: flex;
    flex-direction: column;
    width: 84%;
    max-width: var(--z-width-drawer-sm);
    height: 100%;
    border-right: var(--z-border-width) solid var(--z-border);
    background: var(--z-surface);
    box-shadow: var(--z-shadow-lg);
    animation: slide var(--z-motion-slow) var(--z-ease);
  }
  .panel:focus {
    outline: none;
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-4);
    padding: var(--z-space-4) var(--z-space-4) var(--z-space-3);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  .mark {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-3);
    color: var(--z-text);
    text-decoration: none;
    border-radius: var(--z-radius-md);
  }
  .name {
    font-size: var(--z-text-lg);
    font-weight: var(--z-weight-semibold);
    letter-spacing: -0.02em;
  }
  nav {
    flex: 1;
    padding: var(--z-space-3);
    overflow-y: auto;
    /* Clear of the bar along the bottom edge, which the menu covers but does
       not replace, and of the home indicator below it. */
    padding-bottom: calc(var(--z-space-16) + env(safe-area-inset-bottom, 0px));
  }
  ul {
    display: flex;
    flex-direction: column;
    gap: var(--z-nudge-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  a {
    display: flex;
    align-items: center;
    gap: var(--z-space-3);
    height: var(--z-space-12);
    padding: 0 var(--z-space-3);
    border-radius: var(--z-radius-md);
    color: var(--z-text-muted);
    text-decoration: none;
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-medium);
  }
  a:hover {
    background: var(--z-surface-hover);
    color: var(--z-text);
  }
  a.current {
    background: var(--z-accent-subtle);
    color: var(--z-accent);
  }
  @keyframes slide {
    from {
      transform: translateX(calc(-1 * var(--z-space-4)));
      opacity: 0;
    }
  }
</style>
