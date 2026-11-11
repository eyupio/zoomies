<!--
  Help that appears on hover *and* on focus, and whose text is also present for
  assistive technology at all times. Nothing may live only in a tooltip.

  Every hover and focus help in the product is this component, drawn on the one
  shell (TooltipBubble) and answering to the one set of timing rules
  (lib/tooltip.ts). It replaces the browser's native `title` as hover help, which
  waits a second, cannot be styled, never shows for the keyboard and is read
  inconsistently.

  Four kinds of content:

    <Tooltip title="Refresh" text="Refreshed 4 minutes ago. Fetch this page again." keys={['r']}>
      plain help: a heading (when the control does not already show its own name),
      a sentence, an optional reason and the keys that do the same thing;

    <Tooltip detail={{ title: 'Queue wait', rows: [{ label: 'Median', value: '12s' }] }}>
      a structured card: a title and a few label and value rows, drawn as a
      heading and a `<dl>` with every value as text. With no `text` the card is
      also the description, as one run of sentences;

    <Tooltip text={summary}>{#snippet content()}...{/snippet} ...
      a rich card, with a layout of its own. `text` stays the accessible copy, so a
      card with three lines is still one sentence to a screen reader;

    <Tooltip text="Drain" reason="Only operators can drain runners. Ask an admin for the operator role.">
      a control that cannot be used now, and why. Mark it `aria-disabled`, not
      `disabled`: a disabled button cannot be hovered or focused, and the reason
      is the point.

  The description is linked to the first control inside with `aria-describedby`,
  unless it would only repeat that control's accessible name.
-->
<script module lang="ts">
  /**
   * What an outer tooltip tells the ones inside it. A badge draws its own
   * tooltip, and a badge inside a card or a cell that has something to say
   * already would otherwise race it for the pointer, and win: the inner one
   * opens last. The outer one speaks for everything it wraps.
   */
  interface Outer {
    /** It has a sentence of its own for what it wraps, which a description should not repeat. */
    owns(): boolean;
    /** It would open if the pointer were on it now. */
    engaged(): boolean;
  }
  const OUTER = Symbol('tooltip-outer');
</script>

<script lang="ts">
  import { getContext, setContext, type Snippet } from 'svelte';
  import { RICH_CARD_WAIT, tooltips, type Bubble, type Wait } from '../tooltip';
  import { focusWasPlaced } from '../keys';
  import { detailSentence, type TipDetail } from '../tooltip-detail';
  import { hosted } from '../tooltip-description';
  import { whenPointerMoves } from '../tooltip-intent';
  import type { Side } from '../tooltip-place';
  import TooltipBubble from './TooltipBubble.svelte';

  interface Props {
    /**
     * The sentence: what assistive technology gets, and what the bubble says
     * under its heading. For a rich card, the whole card as one sentence.
     */
    text?: string;
    /** A heading in the bubble, for a control that does not show its own name. */
    title?: string;
    /** Why the control cannot be used now, and what to do about it. */
    reason?: string;
    /** The reason is a permission, so it is drawn with a padlock. */
    lock?: boolean;
    /** The keys that do the same thing, one cap each: `['g', 'p']`. */
    keys?: readonly string[];
    /** Optional control for touch triggers that must reopen while still focused. */
    open?: boolean;
    descriptionId?: string;
    /**
     * Richer markup for the bubble itself -- a heading, a figure, a line of
     * context -- for sighted readers. `text` stays the accessible copy, so a
     * card with three lines is still one sentence to a screen reader.
     */
    content?: Snippet;
    /**
     * A title and a few label and value rows, for help that is figures about
     * one thing. Every value is put on the page as text. It takes the wide
     * bubble, and without `text` it is also what assistive technology gets.
     */
    detail?: TipDetail;
    placement?: Side;
    /** A card with figures and rows in it wants more room than a sentence does. */
    wide?: boolean;
    /** `none` shows it the moment the pointer arrives, for things people scrub across. */
    wait?: Wait;
    /**
     * Only when what it wraps is cut off by its own edge. The whole value of a
     * truncated name or cell is the one thing a bubble can add; on a cell that
     * fits it would only say again what is in front of the reader. The text is
     * already in the page in full, so it is not given a description either.
     */
    whenTruncated?: boolean;
    /**
     * Supplementary help for something that is not a control, a badge or a time
     * inside a sentence: the description is linked to it, and is never read in
     * the middle of the text around it. Without this, help on something with no
     * control is read where it stands, because it may be the content.
     */
    quiet?: boolean;
    /**
     * Whether the sentence is also given to assistive technology, as the
     * trigger's description. Turn it off only where the same words are already
     * in the page for it, as a chart's summary label is for each of its dots.
     */
    describe?: boolean;
    /**
     * Take no box of your own (`display: contents`), so wrapping a control does
     * not change how its parent lays it out. The bubble points at what is inside.
     */
    bare?: boolean;
    /**
     * A selector for an ancestor whose left and right edges the bubble anchors
     * to when it goes beside the trigger. The sidebar's entries are inset in
     * their rail; the bubble should clear the rail, not the entry.
     */
    edge?: string;
    class?: string;
    children: Snippet;
  }

  let {
    text = '',
    title,
    reason,
    lock = false,
    keys,
    content,
    detail,
    placement = 'top',
    wide = false,
    wait,
    whenTruncated = false,
    quiet = false,
    describe = true,
    bare = false,
    edge,
    class: className = '',
    children,
    open = $bindable(false),
    descriptionId,
  }: Props = $props();

  const outer = getContext<Outer | undefined>(OUTER);
  /** Whether an outer tooltip is speaking for this one right now. */
  const covered = () => outer?.engaged() ?? false;

  const componentId = $props.id();
  const ownId = $derived(descriptionId ?? `${componentId}-description`);

  let wrap = $state<HTMLSpanElement | null>(null);
  let pointerReached = false;
  /** Whether the description is linked to something inside, once it has been looked for. */
  let linked = $state(false);
  /** Whether the sentence would only repeat that control's name. */
  let repeated = $state(false);

  /**
   * Nothing to say. A control wraps itself in a tooltip whether or not it has
   * help today, because wrapping only when it has would rebuild the control --
   * and drop its focus, the moment help appeared. With nothing to say this is
   * a wrapper that does nothing.
   */
  const empty = $derived(
    !content && !detail && !title && !text && !reason && !(keys && keys.length > 0),
  );

  /** What assistive technology is given: the sentence, then why it is unavailable. */
  const description = $derived(
    // A truncated value is in the page in full; the bubble only shows the rest
    // of it to the eye, and a description would attach it to whatever control
    // happens to be inside. One inside a tooltip that describes it already adds
    // nothing to what that says.
    whenTruncated || !describe || outer?.owns()
      ? ''
      : content
        ? text
        : detail
          ? text || detailSentence(detail)
          : [text, reason].filter(Boolean).join(' '),
  );

  setContext<Outer>(OUTER, {
    owns: () => !empty && !whenTruncated && describe,
    engaged: () => !empty && !covered() && (!whenTruncated || (wrap ? cutShort(wrap) : false)),
  });

  const handle: Bubble = {
    show: () => (open = true),
    hide: () => (open = false),
  };

  // A bubble opened by its owner (a tap on a touch screen, through the bound
  // `open`) still has to be the only one, and the controller still has to know.
  $effect(() => {
    if (open) {
      if (tooltips.current !== handle) tooltips.adopt(handle);
    } else if (tooltips.current === handle) {
      tooltips.forget(handle);
    }
  });
  $effect(() => () => tooltips.release(handle));

  const FOCUSABLE =
    'a[href], button, input, select, textarea, summary, [tabindex]:not([tabindex="-1"])';

  /** The first thing inside a keyboard can reach, ignoring the bubble itself. */
  function findControl(root: HTMLElement): HTMLElement | null {
    for (const el of root.querySelectorAll<HTMLElement>(FOCUSABLE)) {
      if (!el.closest('[data-tip-skip]')) return el;
    }
    return null;
  }

  function squash(s: string): string {
    return s.replace(/\s+/g, ' ').trim().toLowerCase();
  }

  /** The words the wrapper shows on its own, leaving out the description and the bubble. */
  function visibleText(root: HTMLElement): string {
    let out = '';
    for (const node of root.childNodes) {
      if (node instanceof HTMLElement && node.hasAttribute('data-tip-skip')) continue;
      out += ` ${node.textContent ?? ''}`;
    }
    return out;
  }

  /**
   * Whether the sentence adds nothing to what assistive technology is already
   * given: the control's name (as a row action's, which carries its reason in
   * its label), any label inside it (a bar that reads itself out), or, with no
   * control, the words on show (a truncated identifier, where the bubble exists
   * to give the rest of it).
   */
  function repeatsName(el: HTMLElement | null, root: HTMLElement, sentence: string): boolean {
    if (!sentence) return true;
    const said = squash(sentence);
    const names = [
      el ? (el.getAttribute('aria-label') ?? el.textContent ?? '') : visibleText(root),
    ];
    for (const labelled of root.querySelectorAll('[aria-label]')) {
      if (!labelled.closest('[data-tip-skip]'))
        names.push(labelled.getAttribute('aria-label') ?? '');
    }
    return names.some((name) => {
      const already = squash(name);
      return already === said || already.includes(said);
    });
  }

  /** The first element inside that is part of what is wrapped, not the bubble or a description. */
  function firstPart(root: HTMLElement): HTMLElement | null {
    for (const el of root.children) {
      if (el instanceof HTMLElement && !el.hasAttribute('data-tip-skip')) return el;
    }
    return null;
  }

  // Link the description to the control, unless the caller already did (the
  // rich cards write `aria-describedby` themselves) or it would only repeat the
  // control's name. Something with no control is linked to only when `quiet`.
  // Redone whenever the copy changes.
  $effect(() => {
    const root = wrap;
    const sentence = description;
    const id = ownId;
    if (!root || empty) {
      linked = false;
      repeated = false;
      return;
    }
    const el = findControl(root) ?? (quiet ? firstPart(root) : null);
    linked = el !== null;
    repeated = repeatsName(el, root, sentence);
    if (!el) return;
    const ids = (el.getAttribute('aria-describedby') ?? '').split(/\s+/).filter(Boolean);
    if (ids.includes(id) || repeated) return;
    el.setAttribute('aria-describedby', [...ids, id].join(' '));
    return () => {
      const now = (el.getAttribute('aria-describedby') ?? '').split(/\s+/).filter(Boolean);
      const rest = now.filter((x) => x !== id);
      if (rest.length > 0) el.setAttribute('aria-describedby', rest.join(' '));
      else el.removeAttribute('aria-describedby');
    };
  });

  const effectiveWait = $derived(wait ?? (content || detail ? RICH_CARD_WAIT : 'default'));
  const rail = $derived(wrap && edge ? (wrap.closest<HTMLElement>(edge) ?? null) : null);

  /** Whether anything inside is cut off by its own edge: an ellipsis, or a clipped line. */
  function cutShort(root: HTMLElement): boolean {
    for (const el of root.children) {
      if (el.hasAttribute('data-tip-skip')) continue;
      if (el.scrollWidth > el.clientWidth || el.scrollHeight > el.clientHeight) return true;
    }
    // A table cell clips what is in it, and the element in it is only inline.
    const holder = root.parentElement;
    if (holder && getComputedStyle(holder).overflowX !== 'visible') {
      return holder.scrollWidth > holder.clientWidth;
    }
    return false;
  }

  $effect(() => {
    const target = wrap;
    if (!target) return;
    return whenPointerMoves(target, (event) => {
      if (event.pointerType === 'touch' || pointerReached || empty || covered()) return;
      if (whenTruncated && !cutShort(target)) return;
      pointerReached = true;
      tooltips.hover(handle, effectiveWait);
    });
  });

  function focusVisible(event: FocusEvent): boolean {
    const target = event.target;
    return target instanceof Element && target.matches(':focus-visible');
  }
</script>

{#snippet descriptionNode()}
  <!--
    Referenced by the control's aria-describedby, and kept at the end of the page
    where it is not in anybody's text. With nothing to link it to (a badge that
    is not `quiet`, say) it stays where a reader going down the page will find
    it, because there it may be the content.
  -->
  {#if description && (!repeated || descriptionId)}
    {#if linked}
      <span id={ownId} data-tip-skip use:hosted>{description}</span>
    {:else}
      <span class="sr-only" id={ownId} data-tip-skip>{description}</span>
    {/if}
  {/if}
{/snippet}

{#snippet bubbleNode()}
  {#if open && wrap && !empty}
    <TooltipBubble
      anchor={wrap}
      {placement}
      edge={rail}
      size={wide || detail ? 'wide' : 'default'}
      title={detail ? undefined : title}
      text={content || detail ? undefined : text}
      detail={content ? undefined : detail}
      {reason}
      {lock}
      {keys}
      onclose={() => tooltips.dismiss(handle)}
      onenter={() => tooltips.bubbleEnter(handle)}
      onleave={() => tooltips.bubbleLeave(handle)}
      children={content}
    />
  {/if}
{/snippet}

<!--
  The children, the description and the bubble are written with nothing between
  them on purpose. This wrapper is often layout-neutral, and a whitespace text
  node in a box that is not there is a real space in whatever sentence the
  control is part of.
-->
<span
  class="tip-wrap {className}"
  class:bare
  bind:this={wrap}
  role="presentation"
  onpointerleave={() => {
    pointerReached = false;
    tooltips.unhover(handle);
  }}
  onpointerdown={() => {
    pointerReached = true;
    tooltips.dismiss(handle);
  }}
  onfocusin={(event) => {
    // Only the keyboard's focus: a click focuses a button too, and help that
    // opens on every click would be in the way of the thing just clicked.
    if (empty || covered() || focusWasPlaced() || !focusVisible(event)) return;
    if (whenTruncated && wrap && !cutShort(wrap)) return;
    tooltips.focus(handle);
  }}
  onfocusout={() => tooltips.blur(handle)}
>
  {@render children()}{@render descriptionNode()}{@render bubbleNode()}
</span>

<style>
  .tip-wrap {
    position: relative;
    display: inline-flex;
    align-items: center;
  }
  /*
    Layout-neutral: the wrapper is there for the events and the description, and
    its parent lays out what is inside it as though it were not. Its pointer and
    focus events still arrive; only its box is gone.
  */
  .tip-wrap.bare {
    display: contents;
  }
</style>
