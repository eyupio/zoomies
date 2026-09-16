<!--
  A menu button. Roving tabindex, arrow keys, Home/End, type-ahead, and Escape
  through the shared layer stack.

  The list opens in the browser's top layer, as the tooltip does and for the
  same reason: a menu on a grid's last row was cut off at the frame's edge,
  because the scrolling frame clips what overflows it and no amount of
  z-index gets a positioned child out of an ancestor's clip. A popover keeps
  the list in its owner's DOM and accessibility tree -- so a click inside it
  is still a click inside this component, and the trigger still owns it --
  while the browser draws it over everything. It is placed against the
  trigger every time it opens, flipped above where there is no room below,
  kept inside the window, and let go of when the trigger scrolls out of the
  frame it lives in: a menu floating over rows it no longer belongs to is
  worse than one that closed.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import { Ellipsis, ExternalLink } from '@lucide/svelte';
  import type { LucideIcon } from '@lucide/svelte';
  import { layers } from '../keys';
  import Button from './Button.svelte';
  import IconButton from './IconButton.svelte';

  export interface MenuItem {
    id: string;
    label: string;
    icon?: LucideIcon;
    disabled?: boolean;
    /** Renders in the danger colour and is separated from the safe items. */
    danger?: boolean;
    /** Draw a rule above this item. */
    separated?: boolean;
    /**
     * One of several answers to a question -- the theme, say. Consecutive
     * items sharing a `choice` are drawn as one segmented row under that
     * word, each a radio item with `checked` on the one in force, so a
     * screen reader hears "Theme, Dark, checked" rather than three commands.
     */
    choice?: string;
    checked?: boolean;
    /** A key cap at the right: the shortcut that does the same thing. */
    hint?: string;
    /** A link rather than an action. With `newTab`, one that leaves the product. */
    href?: string;
    newTab?: boolean;
    onSelect?: () => void;
  }

  interface Props {
    items: readonly MenuItem[];
    /** The trigger's accessible name: "Runner actions". */
    label: string;
    /** Visible trigger text. Omit for an icon-only trigger. */
    triggerLabel?: string;
    triggerIcon?: LucideIcon;
    align?: 'start' | 'end';
    size?: 'sm' | 'md';
    disabled?: boolean;
    class?: string;
    /** Rendered above the items: who the menu is about. Not interactive. */
    header?: Snippet;
  }

  let {
    items,
    label,
    triggerLabel,
    triggerIcon = Ellipsis,
    align = 'end',
    size = 'md',
    disabled = false,
    class: className = '',
    header,
  }: Props = $props();

  /** The items as rows: a choice's items share one row, everything else is its own. */
  type Row =
    | { kind: 'item'; item: MenuItem; index: number }
    | { kind: 'choice'; label: string; entries: { item: MenuItem; index: number }[] };

  const rows = $derived.by<Row[]>(() => {
    const out: Row[] = [];
    items.forEach((item, index) => {
      const last = out[out.length - 1];
      if (item.choice && last?.kind === 'choice' && last.label === item.choice) {
        last.entries.push({ item, index });
      } else if (item.choice) {
        out.push({ kind: 'choice', label: item.choice, entries: [{ item, index }] });
      } else {
        out.push({ kind: 'item', item, index });
      }
    });
    return out;
  });

  const uid = $props.id();
  const id = `menu-${uid}`;

  let open = $state(false);
  let active = $state(0);
  let menu = $state<HTMLDivElement | null>(null);
  let wrap = $state<HTMLDivElement | null>(null);
  let typed = '';
  let typedTimer: ReturnType<typeof setTimeout> | null = null;

  const enabled = $derived(items.filter((i) => !i.disabled));

  function toggle(): void {
    if (open) close();
    else show();
  }

  function show(): void {
    if (disabled || items.length === 0) return;
    open = true;
    active = items.findIndex((i) => !i.disabled);
    if (active < 0) active = 0;
  }

  function close(restoreFocus = true): void {
    if (!open) return;
    open = false;
    if (restoreFocus) wrap?.querySelector('button')?.focus();
  }

  function choose(item: MenuItem): void {
    if (item.disabled) return;
    close();
    item.onSelect?.();
  }

  function step(delta: number): void {
    if (items.length === 0) return;
    let next = active;
    for (let i = 0; i < items.length; i++) {
      next = (next + delta + items.length) % items.length;
      if (!items[next]?.disabled) break;
    }
    active = next;
    focusActive();
  }

  function focusActive(): void {
    queueMicrotask(() => {
      menu?.querySelector<HTMLElement>(`[data-index="${active}"]`)?.focus();
    });
  }

  function onMenuKeydown(event: KeyboardEvent): void {
    switch (event.key) {
      // The keys this menu handles stop here: a menu opened from a grid row
      // otherwise moved the grid's focused row as well.
      case 'ArrowDown':
        event.preventDefault();
        event.stopPropagation();
        step(1);
        break;
      case 'ArrowUp':
        event.preventDefault();
        event.stopPropagation();
        step(-1);
        break;
      // Across a choice row the options sit side by side, so the keys that
      // point that way move too. Elsewhere they walk the same list.
      case 'ArrowRight':
        event.preventDefault();
        event.stopPropagation();
        step(1);
        break;
      case 'ArrowLeft':
        event.preventDefault();
        event.stopPropagation();
        step(-1);
        break;
      case 'Home':
        event.preventDefault();
        event.stopPropagation();
        active = items.findIndex((i) => !i.disabled);
        focusActive();
        break;
      case 'End':
        event.preventDefault();
        event.stopPropagation();
        for (let i = items.length - 1; i >= 0; i--) {
          if (!items[i]?.disabled) {
            active = i;
            break;
          }
        }
        focusActive();
        break;
      case 'Tab':
        close(false);
        break;
      default:
        if (event.key.length === 1 && /\S/.test(event.key)) typeahead(event.key);
    }
  }

  function typeahead(key: string): void {
    typed += key.toLowerCase();
    if (typedTimer) clearTimeout(typedTimer);
    typedTimer = setTimeout(() => (typed = ''), 600);
    const found = items.findIndex((i) => !i.disabled && i.label.toLowerCase().startsWith(typed));
    if (found >= 0) {
      active = found;
      focusActive();
    }
  }

  $effect(() => {
    if (!open) return;
    const layer = layers.push('dropdown', () => close());
    focusActive();
    const onDocument = (event: MouseEvent) => {
      const target = event.target as Node | null;
      if (target && !wrap?.contains(target)) close(false);
    };
    document.addEventListener('mousedown', onDocument);
    return () => {
      layers.remove(layer);
      document.removeEventListener('mousedown', onDocument);
    };
  });

  /** The gap between the trigger and the list, and the margin kept off the window's edges. */
  const GAP = 4;

  $effect(() => {
    if (!open || !wrap || !menu) return;
    const anchor = wrap;
    const list = menu;
    list.showPopover();

    function position(): void {
      const rect = anchor.getBoundingClientRect();
      const width = document.documentElement.clientWidth;
      const height = document.documentElement.clientHeight;
      // Out of the window, or out of a scrolling ancestor: the trigger the
      // menu belongs to is no longer where the menu is pointing.
      if (rect.bottom <= 0 || rect.top >= height || rect.right <= 0 || rect.left >= width) {
        release();
        return;
      }
      for (let parent = anchor.parentElement; parent; parent = parent.parentElement) {
        const style = getComputedStyle(parent);
        const clip = parent.getBoundingClientRect();
        const clipsX = /auto|scroll|hidden|clip/.test(style.overflowX);
        const clipsY = /auto|scroll|hidden|clip/.test(style.overflowY);
        if (
          (clipsX && (rect.right <= clip.left || rect.left >= clip.right)) ||
          (clipsY && (rect.bottom <= clip.top || rect.top >= clip.bottom))
        ) {
          release();
          return;
        }
      }
      const bounds = list.getBoundingClientRect();
      // Below by preference, above when the window has no room for it there
      // and more room the other way -- which is what a menu on the last row
      // of a full page needs.
      const below = height - rect.bottom - GAP;
      const above = rect.top - GAP;
      const flip = bounds.height > below && above > below;
      const y = flip ? rect.top - GAP - bounds.height : rect.bottom + GAP;
      const x = align === 'end' ? rect.right - bounds.width : rect.left;
      list.style.left = `${Math.max(GAP, Math.min(x, width - bounds.width - GAP))}px`;
      list.style.top = `${Math.max(GAP, Math.min(y, height - bounds.height - GAP))}px`;
    }

    // A scroll that takes the trigger away closes the menu, and focus goes
    // back to the trigger only when it was inside the list: a wheel under a
    // menu nobody was typing into should not drag the page somewhere else.
    function release(): void {
      close(!!list.contains(document.activeElement));
    }

    position();
    // Capture sees scrolls from the table's own frame, not just the window.
    document.addEventListener('scroll', position, true);
    window.addEventListener('resize', position);
    const observer = new ResizeObserver(position);
    observer.observe(anchor);
    observer.observe(list);
    return () => {
      observer.disconnect();
      document.removeEventListener('scroll', position, true);
      window.removeEventListener('resize', position);
      if (list.matches(':popover-open')) list.hidePopover();
    };
  });
</script>

<div class="menu-wrap {className}" bind:this={wrap}>
  {#if triggerLabel}
    <Button
      {size}
      onclick={toggle}
      {disabled}
      ariaLabel={label}
      ariaExpanded={open}
      ariaControls="{id}-list"
      ariaHaspopup="menu"
      iconAfter={triggerIcon}
      class="trigger"
    >
      <span class="trigger-label">{triggerLabel}</span>
    </Button>
  {:else}
    <IconButton
      icon={triggerIcon}
      {label}
      {size}
      {disabled}
      expanded={open}
      controls="{id}-list"
      haspopup="menu"
      onclick={toggle}
    />
  {/if}

  {#if open}
    <div
      bind:this={menu}
      id="{id}-list"
      class="menu"
      popover="manual"
      role="menu"
      aria-label={label}
      tabindex="-1"
      onkeydown={onMenuKeydown}
    >
      {#if header}
        <div class="head">{@render header()}</div>
        <span class="rule" role="separator"></span>
      {/if}
      {#each rows as row (row.kind === 'item' ? row.item.id : `choice-${row.label}`)}
        {#if row.kind === 'choice'}
          {#if row.entries[0]?.item.separated}<span class="rule" role="separator"></span>{/if}
          <div class="choice" role="group" aria-label={row.label}>
            <span class="choice-label" aria-hidden="true">{row.label}</span>
            <span class="choice-options">
              {#each row.entries as entry (entry.item.id)}
                <button
                  type="button"
                  role="menuitemradio"
                  aria-checked={entry.item.checked === true}
                  class="option"
                  class:on={entry.item.checked}
                  data-index={entry.index}
                  tabindex={entry.index === active ? 0 : -1}
                  disabled={entry.item.disabled}
                  onclick={() => choose(entry.item)}
                >
                  {#if entry.item.icon}
                    {@const OptionIcon = entry.item.icon}
                    <OptionIcon size={13} aria-hidden="true" />
                  {/if}
                  <span>{entry.item.label}</span>
                </button>
              {/each}
            </span>
          </div>
        {:else}
          {@const item = row.item}
          {#if item.separated}<span class="rule" role="separator"></span>{/if}
          {#if item.href}
            <a
              role="menuitem"
              class="item"
              href={item.href}
              target={item.newTab ? '_blank' : undefined}
              rel={item.newTab ? 'noopener noreferrer' : undefined}
              data-index={row.index}
              tabindex={row.index === active ? 0 : -1}
              onclick={() => choose(item)}
            >
              {#if item.icon}
                {@const ItemIcon = item.icon}
                <ItemIcon size={14} aria-hidden="true" />
              {/if}
              <span>{item.label}</span>
              {#if item.newTab}
                <ExternalLink size={12} aria-hidden="true" class="trail" />
                <span class="sr-only">Opens in a new tab</span>
              {/if}
            </a>
          {:else}
            <button
              type="button"
              role="menuitem"
              class="item"
              class:danger={item.danger}
              data-index={row.index}
              tabindex={row.index === active ? 0 : -1}
              disabled={item.disabled}
              onclick={() => choose(item)}
            >
              {#if item.icon}
                {@const ItemIcon = item.icon}
                <ItemIcon size={14} aria-hidden="true" />
              {/if}
              <span>{item.label}</span>
              {#if item.hint}<kbd class="hint" aria-hidden="true">{item.hint}</kbd>{/if}
            </button>
          {/if}
        {/if}
      {/each}
      {#if enabled.length === 0}
        <p class="none">Nothing available here</p>
      {/if}
    </div>
  {/if}
</div>

<style>
  .menu-wrap {
    position: relative;
    display: inline-flex;
  }
  /* Placed by the effect above, in the top layer, so `inset: auto` and no
     margin: the popover's own centring would otherwise fight the coordinates
     it is given. */
  .menu {
    position: fixed;
    inset: auto;
    margin: 0;
    z-index: var(--z-layer-dropdown);
    min-width: 190px;
    max-height: calc(100dvh - var(--z-space-4));
    overflow-y: auto;
    padding: var(--z-space-1);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface-raised);
    box-shadow: var(--z-shadow-md);
    animation: pop var(--z-motion-base) var(--z-ease);
  }
  .menu:focus {
    outline: none;
  }
  .item {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    width: 100%;
    padding: var(--z-space-2) var(--z-space-2);
    border: 0;
    border-radius: var(--z-radius-sm);
    background: transparent;
    color: var(--z-text);
    font-family: inherit;
    font-size: var(--z-text-sm);
    text-align: left;
    cursor: pointer;
  }
  .item:hover:not(:disabled),
  .item:focus-visible {
    background: var(--z-surface-hover);
  }
  .item:disabled {
    color: var(--z-text-subtle);
    cursor: not-allowed;
  }
  .item.danger {
    color: var(--z-danger);
  }
  .item.danger:hover:not(:disabled) {
    background: var(--z-danger-subtle);
  }
  a.item {
    text-decoration: none;
  }
  .item .hint {
    margin-left: auto;
    padding: var(--z-nudge-1) var(--z-space-1);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface-sunken);
    font-family: var(--z-font-mono);
    font-size: var(--z-text-2xs);
    line-height: 1;
    color: var(--z-text-subtle);
  }
  .item :global(.trail) {
    margin-left: auto;
    color: var(--z-text-subtle);
  }
  .head {
    padding: var(--z-space-2) var(--z-space-2) var(--z-space-1);
  }
  /*
    A choice is one row: the question at the left, the answers as a segmented
    control at the right, the same shape the Appearance page draws for it.
  */
  .choice {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-3);
    padding: var(--z-space-1) var(--z-space-2);
  }
  .choice-label {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .choice-options {
    display: inline-flex;
    flex: none;
    border: var(--z-border-width) solid var(--z-border-strong);
    border-radius: var(--z-radius-md);
    overflow: hidden;
  }
  .option {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
    height: var(--z-space-6);
    padding: 0 var(--z-space-2);
    border: 0;
    border-left: var(--z-border-width) solid var(--z-border-strong);
    background: var(--z-surface);
    color: var(--z-text-muted);
    font-family: inherit;
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    white-space: nowrap;
    cursor: pointer;
  }
  .option:first-child {
    border-left: 0;
  }
  .option:hover:not(:disabled) {
    background: var(--z-surface-hover);
    color: var(--z-text);
  }
  .option.on {
    background: var(--z-accent);
    color: var(--z-accent-contrast);
  }
  .option:focus-visible {
    /* Inside a clipped, rounded group the outline would be cut off, so the
       ring is drawn inward, as the tokens provide for. */
    outline: none;
    box-shadow: inset var(--z-focus-ring);
  }
  .rule {
    display: block;
    height: var(--z-border-width);
    margin: var(--z-space-1) 0;
    background: var(--z-border);
  }
  .none {
    margin: 0;
    padding: var(--z-space-2);
    font-size: var(--z-text-xs);
    color: var(--z-text-subtle);
  }
  @keyframes pop {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
  }
</style>
