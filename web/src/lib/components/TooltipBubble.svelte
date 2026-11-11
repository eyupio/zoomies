<!--
  The bubble: the one surface every hover and focus help in the product is drawn
  on.

  `Tooltip` is the component to reach for. This is what it, and the activity
  matrix (which has a grid of triggers and so one bubble that moves between
  them), both draw through, so that the surface, the caret, the motion and the
  placement rules exist once.

  It is in the browser's top layer, as `Tooltip` has always been, so a table's
  scrolling frame and a stacking context cannot clip it. It is let go of when
  its trigger scrolls out of the frame it lives in, or out of the window: a
  bubble floating over rows it no longer belongs to is worse than none.

  Three kinds of content share it:

  * plain help: an optional heading, a sentence, a reason (why the control
    cannot be used now, and what to do about it) and the keys that do the same
    thing;
  * a structured card, given as `detail`: a title and a few label and value
    rows, drawn as a heading and a `<dl>` with every value as text;
  * a rich card, given as `children`, with whatever layout it likes;
  * the matrix's data card, which is a rich card that is not interactive.

  The caret points at the trigger's centre even when the bubble has been held
  inside the window. An invisible halo around the bubble bridges the gap to the
  trigger, so the pointer can travel onto it without it closing.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import { Info, Lock } from '@lucide/svelte';
  import { resolveDetail, type TipDetail, type TipSource } from '../tooltip-detail';
  import { NARROW_QUERY, narrowPlacement, place, type Box, type Side } from '../tooltip-place';

  interface Props {
    /** The element the bubble points at. For a layout-neutral wrapper, its children are measured. */
    anchor: HTMLElement;
    /** The side of the trigger it prefers. It flips when there is no room. */
    placement?: Side;
    /** An ancestor whose left and right edges stand in for the trigger's, for a side placement. */
    edge?: HTMLElement | null;
    /** `wide` suits a card with figures and rows; `card` is the matrix's data card. */
    size?: 'default' | 'wide' | 'card';
    /** Plain help. Ignored when `children` is given. */
    title?: string;
    text?: string;
    reason?: string;
    /** The reason is a permission, so its icon is a padlock rather than an "i". */
    lock?: boolean;
    keys?: readonly string[];
    /** A title and label and value rows. Ignored when `children` is given; drawn instead of plain help. */
    detail?: TipSource;
    /**
     * Whether the pointer can land on the bubble. A card over a grid of squares
     * must not, or it would catch the pointer on its way to the next one.
     */
    interactive?: boolean;
    /** The bubble is no longer where its trigger is, and has nothing left to point at. */
    onclose?: () => void;
    onenter?: () => void;
    onleave?: () => void;
    children?: Snippet;
  }

  let {
    anchor,
    placement = 'top',
    edge = null,
    size = 'default',
    title,
    text,
    reason,
    lock = false,
    keys,
    detail,
    interactive = true,
    onclose,
    onenter,
    onleave,
    children,
  }: Props = $props();

  /*
    Worked out here, and only here: the bubble exists only while it is open,
    so a card given as a function costs nothing until then, and while it is
    open it follows whatever it reads (a clock, a frame) as it changes.
  */
  const card = $derived(detail ? resolveDetail(detail) : undefined);

  let bubble = $state<HTMLSpanElement | null>(null);
  const narrow = window.matchMedia(NARROW_QUERY);
  /** Where the bubble ended up once there was room to measure; until then, where it wants to be. */
  let placed = $state<Side | null>(null);
  /**
   * The side it sits on. Before the first measurement that is the preference,
   * not nothing, so the entrance already slides from the right side; a flip
   * corrects it a moment later.
   */
  const side = $derived(placed ?? narrowPlacement(placement, narrow.matches));

  const CLIPS = /auto|scroll|hidden|clip/;

  /**
   * The trigger's box. A wrapper that is layout-neutral has no box of its own,
   * so it is the union of what it holds, leaving out the description and the
   * bubble themselves.
   */
  function boxOf(el: HTMLElement): Box {
    if (getComputedStyle(el).display !== 'contents') {
      const r = el.getBoundingClientRect();
      return { left: r.left, top: r.top, right: r.right, bottom: r.bottom };
    }
    let box: Box | null = null;
    for (const child of el.children) {
      if (child.hasAttribute('data-tip-skip')) continue;
      const r = child.getBoundingClientRect();
      if (r.width === 0 && r.height === 0) continue;
      box = box
        ? {
            left: Math.min(box.left, r.left),
            top: Math.min(box.top, r.top),
            right: Math.max(box.right, r.right),
            bottom: Math.max(box.bottom, r.bottom),
          }
        : { left: r.left, top: r.top, right: r.right, bottom: r.bottom };
    }
    if (box) return box;
    const r = el.getBoundingClientRect();
    return { left: r.left, top: r.top, right: r.right, bottom: r.bottom };
  }

  $effect(() => {
    if (!bubble) return;
    const tip = bubble;
    const target = anchor;
    const rail = edge;
    const preferred = placement;

    // Reading these makes an open bubble reposition when its copy changes.
    void title;
    void text;
    void reason;
    void keys;
    void card;

    // The browser's top layer escapes overflow clipping and stacking contexts,
    // while keeping the bubble in its owner's DOM and accessibility tree.
    tip.showPopover();

    function position(): void {
      const box = boxOf(target);
      const width = document.documentElement.clientWidth;
      const height = document.documentElement.clientHeight;
      if (box.bottom <= 0 || box.top >= height || box.right <= 0 || box.left >= width) {
        onclose?.();
        return;
      }
      // A focused trigger can scroll out of a table without leaving the
      // window. Its top-layer bubble must not float over unrelated rows.
      for (let parent = target.parentElement; parent; parent = parent.parentElement) {
        const style = getComputedStyle(parent);
        if (style.display !== 'contents') {
          const clip = parent.getBoundingClientRect();
          const clipsX = CLIPS.test(style.overflowX);
          const clipsY = CLIPS.test(style.overflowY);
          if (
            (clipsX && (box.right <= clip.left || box.left >= clip.right)) ||
            (clipsY && (box.bottom <= clip.top || box.top >= clip.bottom))
          ) {
            onclose?.();
            return;
          }
        }
        // A trigger inside an open popover is in the top layer with it:
        // nothing above the popover in the DOM clips what it shows, whatever
        // overflow is set there. Walking on closes the bubble of every menu
        // item whose menu lies across a clipping ancestor, which is every
        // menu in a table cell.
        if (parent.hasAttribute('popover')) break;
      }
      const bounds = tip.getBoundingClientRect();
      const railBox = rail ? boxOf(rail) : undefined;
      const spot = place({
        anchor: box,
        edge: railBox,
        size: { width: bounds.width, height: bounds.height },
        viewport: { width, height },
        preferred: narrowPlacement(preferred, narrow.matches),
      });
      placed = spot.side;
      tip.style.left = `${spot.left}px`;
      tip.style.top = `${spot.top}px`;
      tip.style.setProperty('--arrow', `${spot.arrow}px`);
    }

    position();
    // Capture sees scrolls from the table's own viewport, not just the window.
    document.addEventListener('scroll', position, true);
    window.addEventListener('resize', position);
    const observer = new ResizeObserver(position);
    observer.observe(target);
    observer.observe(tip);
    return () => {
      observer.disconnect();
      document.removeEventListener('scroll', position, true);
      window.removeEventListener('resize', position);
      if (tip.matches(':popover-open')) tip.hidePopover();
    };
  });
</script>

{#snippet name()}
  {#if title}<strong class="title">{title}</strong>{/if}
{/snippet}

{#snippet caps()}
  {#if keys && keys.length > 0}
    <span class="keys"
      >{#each keys as key, i (i)}<kbd>{key}</kbd>{/each}</span
    >
  {/if}
{/snippet}

{#snippet head()}
  {#if title || (keys && keys.length > 0)}
    <span class="head">{@render name()}{@render caps()}</span>
  {/if}
{/snippet}

{#snippet sentence()}
  {#if text}<span class="sentence" class:muted={Boolean(title)}>{text}</span>{/if}
{/snippet}

<!--
  The card is a span, as the bubble and its scroller are: the bubble is drawn
  inside its trigger's wrapper, which can stand in a sentence, a link or a
  button, where only phrasing content belongs, so the bubble's own element
  cannot take a div. The heading and the list inside are kept because they are
  what the card is; the bubble is built through the DOM and never re-parsed,
  so nothing moves them, and it is hidden from assistive technology, whose copy
  is the trigger's description.
-->
{#snippet cardOf(tip: TipDetail)}
  <span class="card"
    ><h3 class="card-title">{tip.title}</h3>
    {#if tip.rows.length > 0}<dl class="card-rows">
        {#each tip.rows as row, i (i)}<div>
            <dt>{row.label}</dt>
            <dd>{row.value}</dd>
          </div>{/each}
      </dl>{/if}</span
  >
{/snippet}

{#snippet why()}
  {#if reason}
    <span class="reason">
      {#if lock}<Lock size={12} aria-hidden="true" />{:else}<Info
          size={12}
          aria-hidden="true"
        />{/if}
      <span>{reason}</span>
    </span>
  {/if}
{/snippet}

<!--
  The parts are written with nothing between them: what the bubble contains as
  text is exactly its words, with no space of the template's at either end.
-->
<span
  bind:this={bubble}
  class="bubble {size}"
  class:passive={!interactive}
  data-side={side}
  data-tip-skip
  popover="manual"
  role="presentation"
  aria-hidden="true"
  onpointerenter={() => onenter?.()}
  onpointerleave={() => onleave?.()}
  ><span class="scroll"
    >{#if children}{@render children()}{:else if card}{@render cardOf(card)}{:else}<span
        class="plain">{@render head()}{@render sentence()}{@render why()}</span
      >{/if}</span
  ></span
>

<style>
  .bubble {
    --width: 280px;
    --shift: 4px;
    /* The caret is a square rotated a quarter turn; half of it sits outside. */
    --caret: var(--z-space-2);
    position: fixed;
    inset: auto;
    margin: 0;
    z-index: var(--z-layer-dropdown);
    /* Not clipped: the caret and the hover halo live outside the box. The
       scrolling a tall card needs is on the inner element. */
    overflow: visible;
    max-width: min(var(--width), calc(100vw - var(--z-space-4)));
    padding: var(--z-space-1) var(--z-space-2);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface-raised);
    color: var(--z-text);
    box-shadow: var(--z-shadow-md);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    /*
      The bubble is a child of what it describes, so it inherits that thing's
      text: a table heading's capitals and tracking, a numeric cell's right
      alignment, an actions column's weight. It says its own piece in its own
      voice wherever it was opened from.
    */
    font-weight: var(--z-weight-normal);
    font-style: normal;
    font-variant-numeric: normal;
    letter-spacing: normal;
    text-align: start;
    text-decoration: none;
    text-transform: none;
    cursor: default;
    overflow-wrap: anywhere;
    white-space: normal;
    width: max-content;
    /* The pointer is allowed onto the bubble (see the halo below). */
    pointer-events: auto;
  }
  .bubble.wide {
    --width: 360px;
  }
  .bubble.card {
    --width: 300px;
  }
  .bubble.passive {
    pointer-events: none;
  }

  .scroll {
    display: block;
    max-height: calc(100dvh - var(--z-space-4) - var(--z-space-2) - 2 * var(--z-border-width));
    overflow-y: auto;
  }

  /*
    The entrance: it fades in and slides the last few pixels from the trigger's
    side. With reduced motion the motion tokens are 1ms, so it is not a
    movement anyone sees.
  */
  .bubble:popover-open {
    opacity: 1;
    transform: none;
    transition:
      opacity var(--z-motion-fast) var(--z-ease),
      transform var(--z-motion-fast) var(--z-ease);
  }
  @starting-style {
    .bubble:popover-open {
      opacity: 0;
      transform: translate(var(--from-x, 0), var(--from-y, 0));
    }
  }
  .bubble[data-side='top'] {
    --from-y: var(--shift);
  }
  .bubble[data-side='bottom'] {
    --from-y: calc(-1 * var(--shift));
  }
  .bubble[data-side='left'] {
    --from-x: var(--shift);
  }
  .bubble[data-side='right'] {
    --from-x: calc(-1 * var(--shift));
  }

  /*
    The hover bridge. It is part of the bubble for hit-testing and invisible,
    reaching out past the gap to the trigger, so a pointer on its way from the
    one to the other is never over nothing.
  */
  .bubble::after {
    content: '';
    position: absolute;
    inset: -10px;
    z-index: -1;
  }
  .bubble.passive::after {
    display: none;
  }

  /*
    The caret: an 8px square, rotated, in the bubble's own surface and border.
    Only the two borders that face away from the bubble are drawn, and it is
    centred on the bubble's border so the line does not run through it. --arrow
    is set from the trigger's centre, after the bubble has been held in the
    window, so it keeps pointing at the trigger.
  */
  .bubble::before {
    content: '';
    position: absolute;
    box-sizing: border-box;
    width: var(--caret);
    height: var(--caret);
    background: var(--z-surface-raised);
    border: 0 solid var(--z-border);
    pointer-events: none;
  }
  .bubble[data-side='top']::before {
    bottom: calc(-0.5 * var(--caret) - var(--z-border-width) / 2);
    left: calc(var(--arrow, 50%) - var(--z-border-width));
    border-width: 0 var(--z-border-width) var(--z-border-width) 0;
    transform: translateX(-50%) rotate(45deg);
  }
  .bubble[data-side='bottom']::before {
    top: calc(-0.5 * var(--caret) - var(--z-border-width) / 2);
    left: calc(var(--arrow, 50%) - var(--z-border-width));
    border-width: var(--z-border-width) 0 0 var(--z-border-width);
    transform: translateX(-50%) rotate(45deg);
  }
  .bubble[data-side='left']::before {
    right: calc(-0.5 * var(--caret) - var(--z-border-width) / 2);
    top: calc(var(--arrow, 50%) - var(--z-border-width));
    border-width: var(--z-border-width) var(--z-border-width) 0 0;
    transform: translateY(-50%) rotate(45deg);
  }
  .bubble[data-side='right']::before {
    left: calc(-0.5 * var(--caret) - var(--z-border-width) / 2);
    top: calc(var(--arrow, 50%) - var(--z-border-width));
    border-width: 0 0 var(--z-border-width) var(--z-border-width);
    transform: translateY(-50%) rotate(45deg);
  }

  /*
    The structured card.
    A heading and a two-column list: the labels muted and as wide as the
    longest, the values beside them and broken anywhere, because a value is
    often somebody else's text with no spaces in it. */
  .card {
    display: grid;
    gap: var(--z-space-1);
    padding: var(--z-space-1) 0;
  }
  .card-title {
    margin: 0;
    font-size: inherit;
    line-height: inherit;
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .card-rows {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: var(--z-nudge-2) var(--z-space-3);
    margin: 0;
  }
  .card-rows div {
    display: contents;
  }
  .card-rows dt {
    color: var(--z-text-muted);
  }
  .card-rows dd {
    margin: 0;
    font-variant-numeric: tabular-nums;
  }

  /* -- plain help ---------------------------------------------------------- */
  .plain {
    display: flex;
    flex-direction: column;
    gap: var(--z-nudge-2);
    padding: var(--z-space-1) 0;
  }
  .head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--z-space-3);
  }
  .title {
    font-weight: var(--z-weight-semibold);
  }
  .sentence.muted {
    color: var(--z-text-muted);
  }
  .keys {
    display: inline-flex;
    gap: var(--z-space-1);
    margin-left: auto;
  }
  kbd {
    padding: 0 var(--z-space-1);
    border: var(--z-border-width) solid var(--z-border);
    border-bottom-width: var(--z-border-width-thick);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface-sunken);
    font-family: var(--z-font-mono);
    font-size: var(--z-text-2xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text);
  }
  .reason {
    display: flex;
    align-items: flex-start;
    gap: var(--z-space-1);
    margin-top: var(--z-space-1);
    padding-top: var(--z-space-1);
    border-top: var(--z-border-width) solid var(--z-border);
    color: var(--z-text-muted);
  }
  .reason :global(svg) {
    flex: none;
    /* Centred on the first line of the sentence. */
    margin-top: var(--z-nudge-2);
  }
</style>
