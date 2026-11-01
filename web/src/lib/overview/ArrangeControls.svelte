<!--
  The controls a card carries while the Overview is being arranged: a grip to
  drag it by, a button each to move it a place up or down, and one to hide it.

  They are drawn by the page across the top of the card rather than inside its
  head, because seven panels own their heads and the controls are the page's
  business, not theirs. Every control names the panel, so a screen reader
  hears "Move Pools down" and not a row of identical arrows; the one at an end
  is refused with a reason rather than removed, so a keyboard user is never
  left on a button that vanished.
-->
<script lang="ts">
  import { ChevronDown, ChevronUp, EyeOff, GripVertical } from '@lucide/svelte';
  import IconButton from '$lib/components/IconButton.svelte';

  interface Props {
    title: string;
    first: boolean;
    last: boolean;
    onmove: (delta: -1 | 1) => void;
    onhide: () => void;
    ondrag: (event: PointerEvent) => void;
  }
  let { title, first, last, onmove, onhide, ondrag }: Props = $props();
</script>

<div class="arrange" role="toolbar" aria-label="Arrange {title}">
  <button type="button" class="grip" aria-label="Move {title}" onpointerdown={ondrag}>
    <GripVertical size={16} aria-hidden="true" />
  </button>
  <span class="title">{title}</span>
  <IconButton
    icon={ChevronUp}
    label="Move {title} up"
    size="sm"
    disabled={first}
    reason={first ? 'It is already first.' : undefined}
    onclick={() => onmove(-1)}
  />
  <IconButton
    icon={ChevronDown}
    label="Move {title} down"
    size="sm"
    disabled={last}
    reason={last ? 'It is already last.' : undefined}
    onclick={() => onmove(1)}
  />
  <IconButton icon={EyeOff} label="Hide {title}" size="sm" onclick={onhide} />
</div>

<style>
  .arrange {
    display: flex;
    align-items: center;
    gap: var(--z-space-1);
    width: max-content;
    max-width: 100%;
    margin-bottom: var(--z-space-2);
    padding: var(--z-space-1) var(--z-space-2) var(--z-space-1) var(--z-space-1);
    border: var(--z-border-width) solid var(--z-border-strong);
    border-radius: var(--z-radius-full);
    background: var(--z-surface-raised);
  }
  .grip {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--z-space-8);
    height: var(--z-space-8);
    border: 0;
    border-radius: var(--z-radius-full);
    background: none;
    color: var(--z-text-muted);
    cursor: grab;
    /* The browser's own scroll or zoom on a finger would take the drag away. */
    touch-action: none;
  }
  .grip:hover {
    background: var(--z-surface-hover);
    color: var(--z-text);
  }
  .grip:focus-visible {
    outline: var(--z-focus-width) solid var(--z-focus-colour);
    outline-offset: var(--z-focus-offset);
  }
  .title {
    padding-inline: var(--z-space-1) var(--z-space-2);
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
    color: var(--z-text);
    overflow-wrap: anywhere;
  }
</style>
