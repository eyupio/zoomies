<!--
  Within-page navigation for a long operational page.

  It says where a person can go, how many things are there, and where they are
  now. A link opens a closed destination before moving focus, so a summary is a
  door rather than a label. On a phone the last destinations move behind More,
  as the Overview's page navigation does, so a tab is never cut in half at the
  edge of the screen.
-->
<script lang="ts">
  import { ChevronDown } from '@lucide/svelte';
  import { untrack } from 'svelte';

  export interface SectionNavItem {
    id: string;
    label: string;
    /** A number worth seeing before the section is opened. */
    count?: number | string;
    /** Shown only behind More on a phone. */
    secondary?: boolean;
  }

  interface Props {
    items: readonly SectionNavItem[];
    label: string;
    /** Open a closed destination before the browser scrolls to it. */
    onjump?: (id: string) => void;
  }

  let { items, label, onjump }: Props = $props();

  let nav = $state<HTMLElement | null>(null);
  let active = $state(untrack(() => items[0]?.id ?? ''));
  let moreOpen = $state(false);

  const moreActive = $derived(items.some((item) => item.secondary && item.id === active));

  function target(id: string): HTMLElement | null {
    return document.getElementById(id);
  }

  function reveal(id: string, focus = true): void {
    moreOpen = false;
    const destination = target(id);
    if (!destination) return;
    for (let node: HTMLElement | null = destination; node; node = node.parentElement) {
      if (node instanceof HTMLDetailsElement) node.open = true;
    }
    destination.scrollIntoView({ block: 'start' });
    if (focus) {
      const focusable =
        destination instanceof HTMLDetailsElement
          ? destination.querySelector<HTMLElement>('summary')
          : destination;
      focusable?.focus({ preventScroll: true });
    }
    active = id;
    if (location.hash !== `#${id}`) history.replaceState(null, '', `#${id}`);
  }

  function jump(event: MouseEvent, id: string): void {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey)
      return;
    event.preventDefault();
    onjump?.(id);
    reveal(id);
  }

  $effect(() => {
    // Read the items here so a section that appears after data arrives joins
    // the navigation without a remount.
    const currentItems = items;
    let frame = 0;
    function update(): void {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        if (!nav) return;
        const edge = nav.getBoundingClientRect().bottom + 1;
        let current = currentItems[0]?.id ?? '';
        for (const item of currentItems) {
          const node = target(item.id);
          if (node && node.getBoundingClientRect().top <= edge) current = item.id;
        }
        if (
          window.scrollY > 0 &&
          window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 1
        ) {
          current = currentItems.at(-1)?.id ?? current;
        }
        active = current;
      });
    }
    const fragment = () => {
      const id = location.hash.slice(1);
      if (id) reveal(id, false);
    };
    function escape(event: KeyboardEvent): void {
      if (event.key === 'Escape' && moreOpen) {
        moreOpen = false;
        nav?.querySelector<HTMLElement>('.more > summary')?.focus();
      }
    }
    window.addEventListener('scroll', update, { passive: true });
    window.addEventListener('resize', update);
    window.addEventListener('hashchange', fragment);
    window.addEventListener('keydown', escape);
    update();
    if (location.hash) fragment();
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener('scroll', update);
      window.removeEventListener('resize', update);
      window.removeEventListener('hashchange', fragment);
      window.removeEventListener('keydown', escape);
    };
  });
</script>

<nav class="page-nav" aria-label={label} bind:this={nav}>
  {#each items as item (item.id)}
    <a
      class:secondary={item.secondary}
      href="#{item.id}"
      aria-current={active === item.id ? 'location' : undefined}
      onclick={(event) => jump(event, item.id)}
      >{item.label}{#if item.count !== undefined && item.count !== ''}<span class="count"
          >{item.count}</span
        >{/if}</a
    >
  {/each}
  {#if items.some((item) => item.secondary)}
    <details class="more" bind:open={moreOpen}>
      <summary aria-current={moreActive ? 'location' : undefined}
        >More <ChevronDown size={14} aria-hidden="true" /></summary
      >
      <div class="more-links" role="group" aria-label="More sections">
        {#each items as item (item.id)}
          {#if item.secondary}
            <a
              href="#{item.id}"
              aria-current={active === item.id ? 'location' : undefined}
              onclick={(event) => jump(event, item.id)}
              >{item.label}{#if item.count !== undefined && item.count !== ''}<span class="count"
                  >{item.count}</span
                >{/if}</a
            >
          {/if}
        {/each}
      </div>
    </details>
  {/if}
</nav>

<style>
  .page-nav {
    display: flex;
    align-items: center;
    position: sticky;
    top: var(--z-topbar-height);
    z-index: calc(var(--z-layer-sticky) - 1);
    min-height: var(--z-control-touch);
    margin: 0 0 var(--z-space-4);
    border-bottom: var(--z-border-width) solid var(--z-border);
    background: var(--z-bg);
    overflow-x: auto;
    scrollbar-width: none;
  }
  .page-nav::-webkit-scrollbar {
    display: none;
  }
  .page-nav a,
  .page-nav summary {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--z-space-1);
    min-height: var(--z-control-touch);
    padding: var(--z-space-2) var(--z-space-3);
    border: 0;
    border-bottom: var(--z-border-width-thick) solid transparent;
    background: none;
    color: var(--z-text-muted);
    font: inherit;
    font-size: var(--z-text-sm);
    text-decoration: none;
    white-space: nowrap;
    cursor: pointer;
  }
  .page-nav a:hover,
  .page-nav summary:hover {
    color: var(--z-accent);
    background: var(--z-surface-hover);
  }
  .page-nav a[aria-current],
  .page-nav summary[aria-current] {
    color: var(--z-accent);
    border-bottom-color: var(--z-accent);
    font-weight: var(--z-weight-semibold);
  }
  .count {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 1.25rem;
    height: 1.25rem;
    padding: 0 var(--z-space-1);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface);
    color: var(--z-text-muted);
    font-size: var(--z-text-2xs);
    font-variant-numeric: tabular-nums;
  }
  .page-nav a[aria-current] .count,
  .more-links a[aria-current] .count {
    border-color: var(--z-accent-border);
    background: var(--z-accent-subtle);
    color: var(--z-accent);
  }
  .more {
    display: none;
    position: relative;
    margin-left: auto;
  }
  .more summary {
    list-style: none;
  }
  .more summary::-webkit-details-marker {
    display: none;
  }
  .more-links {
    position: absolute;
    top: 100%;
    right: 0;
    max-width: calc(100vw - var(--z-space-6));
    z-index: var(--z-layer-dropdown);
    display: flex;
    flex-direction: column;
    min-width: 11rem;
    padding: var(--z-space-1);
    border: var(--z-border-width) solid var(--z-border-strong);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
    box-shadow: var(--z-shadow-md);
  }
  .more-links a {
    justify-content: flex-start;
    min-height: var(--z-control-touch);
  }
  @media (max-width: 768px) {
    .page-nav {
      margin-right: calc(var(--z-space-3) * -1);
      margin-left: calc(var(--z-space-3) * -1);
      padding-right: var(--z-space-3);
      padding-left: var(--z-space-3);
      overflow: visible;
    }
    .more {
      display: block;
    }
    .page-nav > a.secondary {
      display: none;
    }
    .page-nav > a,
    .page-nav summary {
      flex: none;
      padding-right: var(--z-space-2);
      padding-left: var(--z-space-2);
      font-size: var(--z-text-xs);
    }
  }
</style>
