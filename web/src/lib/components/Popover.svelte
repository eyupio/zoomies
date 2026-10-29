<!--
  A panel that belongs to a control: opened from it, placed against it, and
  closed when it scrolls away from it.

  It borrows the menu's mechanics because they are the ones that survived a
  grid's last row: the panel is a popover, so the browser draws it in the top
  layer while it stays in its owner's DOM and accessibility tree, and it is
  placed on every open with the same flip and clamping a menu gets. Unlike a
  menu it is a place to read and fill in rather than a list of commands, so it
  is a non-modal dialog: Tab moves through its controls and on out of it, which
  closes it, as a popover that has stopped being looked at should.

  The footer is outside the scrolling body, so a checklist that needs the
  height of the window can never push the button that applies it off screen.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import { layers, placeFocus } from '../keys';

  interface Props {
    open: boolean;
    /** The control the panel points at. Its position is measured on every open. */
    anchor: HTMLElement | null;
    /** The panel's accessible name. */
    label: string;
    align?: 'start' | 'end';
    onclose: () => void;
    children: Snippet;
    /** Actions that stay put while the body scrolls. */
    footer?: Snippet;
    class?: string;
  }

  let {
    open,
    anchor,
    label,
    align = 'end',
    onclose,
    children,
    footer,
    class: className = '',
  }: Props = $props();

  const uid = $props.id();
  const id = `popover-${uid}`;
  let panel = $state<HTMLDivElement | null>(null);

  /** The gap between the control and the panel, and the margin kept off the window's edges. */
  const GAP = 8;

  function close(restoreFocus: boolean): void {
    if (!open) return;
    if (restoreFocus && anchor && document.contains(anchor)) anchor.focus();
    onclose();
  }
  $effect(() => {
    if (!open) return;
    const layer = layers.push('dropdown', () => close(true));
    // The panel is where the keyboard goes, not the control that opened it: the
    // first Tab has to reach the first answer, not the band behind the panel.
    queueMicrotask(() => {
      if (panel) placeFocus(panel);
    });
    const onDocument = (event: MouseEvent) => {
      const target = event.target as Node | null;
      if (!target) return;
      if (panel?.contains(target) || anchor?.contains(target)) return;
      close(false);
    };
    document.addEventListener('mousedown', onDocument);
    return () => {
      layers.remove(layer);
      document.removeEventListener('mousedown', onDocument);
    };
  });

  $effect(() => {
    if (!open || !anchor || !panel) return;
    const trigger = anchor;
    const box = panel;
    box.showPopover();

    function position(): void {
      const rect = trigger.getBoundingClientRect();
      const width = document.documentElement.clientWidth;
      const height = document.documentElement.clientHeight;
      // Out of the window, or out of a scrolling ancestor: the control the
      // panel belongs to is no longer where the panel is pointing.
      if (rect.bottom <= 0 || rect.top >= height || rect.right <= 0 || rect.left >= width) {
        close(false);
        return;
      }
      for (let parent = trigger.parentElement; parent; parent = parent.parentElement) {
        const style = getComputedStyle(parent);
        const clip = parent.getBoundingClientRect();
        const clipsX = /auto|scroll|hidden|clip/.test(style.overflowX);
        const clipsY = /auto|scroll|hidden|clip/.test(style.overflowY);
        if (
          (clipsX && (rect.right <= clip.left || rect.left >= clip.right)) ||
          (clipsY && (rect.bottom <= clip.top || rect.top >= clip.bottom))
        ) {
          close(false);
          return;
        }
      }
      const bounds = box.getBoundingClientRect();
      // Below by preference, above when the window has no room for it there and
      // more room the other way, which is what a control on the first screen of
      // a long page needs.
      const below = height - rect.bottom - GAP;
      const above = rect.top - GAP;
      const flip = bounds.height > below && above > below;
      const y = flip ? rect.top - GAP - bounds.height : rect.bottom + GAP;
      const x = align === 'end' ? rect.right - bounds.width : rect.left;
      box.style.left = `${Math.max(GAP, Math.min(x, width - bounds.width - GAP))}px`;
      box.style.top = `${Math.max(GAP, Math.min(y, height - bounds.height - GAP))}px`;
    }

    position();
    document.addEventListener('scroll', position, true);
    window.addEventListener('resize', position);
    const observer = new ResizeObserver(position);
    observer.observe(trigger);
    observer.observe(box);
    return () => {
      observer.disconnect();
      document.removeEventListener('scroll', position, true);
      window.removeEventListener('resize', position);
      if (box.matches(':popover-open')) box.hidePopover();
    };
  });
</script>

{#if open}
  <div
    bind:this={panel}
    {id}
    class="popover {className}"
    popover="manual"
    role="dialog"
    aria-modal="false"
    aria-label={label}
    tabindex="-1"
    onfocusout={(event) => {
      const next = event.relatedTarget as Node | null;
      // Focus leaving the panel is the panel being left; focus returning to the
      // control it belongs to is handled by the control's own click.
      if (!next || !panel?.contains(next)) close(false);
    }}
  >
    <div class="body">
      {@render children()}
    </div>
    {#if footer}
      <div class="foot">{@render footer()}</div>
    {/if}
  </div>
{/if}

<style>
  /* Placed by the effect above, in the top layer, so `inset: auto` and no
     margin: the popover's own centring would otherwise fight the coordinates
     it is given. */
  .popover {
    position: fixed;
    inset: auto;
    margin: 0;
    display: flex;
    flex-direction: column;
    width: min(30rem, calc(100vw - var(--z-space-8)));
    max-height: min(38rem, calc(100dvh - var(--z-space-8)));
    overflow: hidden;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-lg);
    background: var(--z-surface-raised);
    color: var(--z-text);
    box-shadow: var(--z-shadow-md);
    animation: arrive var(--z-motion-base) var(--z-ease);
  }
  .popover:focus {
    outline: none;
  }
  .body {
    min-height: 0;
    overflow-y: auto;
    padding: var(--z-space-4);
  }
  .foot {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: var(--z-space-2);
    flex: none;
    padding: var(--z-space-3) var(--z-space-4);
    border-top: var(--z-border-width) solid var(--z-border);
    background: var(--z-surface);
  }
  @keyframes arrive {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .popover {
      animation: none;
    }
  }
</style>
