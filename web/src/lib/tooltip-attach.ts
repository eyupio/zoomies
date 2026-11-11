/**
 * The shared tooltip for an element that is built by hand rather than by a
 * component, such as the grips and edges `tableLayout` adds to a table heading.
 *
 * It is the same controller and the same bubble as `Tooltip`, so these follow the
 * same timing, close on the same Escape and never open beside another one. What
 * it cannot do is wrap its element, so the sentence is linked to it with
 * `aria-describedby`, and the bubble is mounted when it opens and taken away
 * when it closes. It returns what undoes all of that.
 */
import { mount, unmount } from 'svelte';
import { focusWasPlaced } from './keys';
import TooltipBubble from './components/TooltipBubble.svelte';
import { tooltips, type Bubble, type Wait } from './tooltip';
import { detailSentence, type TipDetail } from './tooltip-detail';
import { descriptionHost } from './tooltip-description';
import { whenPointerMoves } from './tooltip-intent';
import type { Side } from './tooltip-place';

export interface AttachedTip {
  /** The sentence. With `detail`, only what assistive technology gets; empty, the card is read instead. */
  text: string;
  /** A title and label and value rows, drawn in place of the sentence. */
  detail?: TipDetail;
  placement?: Side;
  wait?: Wait;
}

let counter = 0;

export function attachTip(el: HTMLElement, tip: AttachedTip): () => void {
  let shown: ReturnType<typeof mount> | null = null;

  const handle: Bubble = {
    show() {
      if (shown) return;
      shown = mount(TooltipBubble, {
        target: document.body,
        props: {
          anchor: el,
          placement: tip.placement ?? 'top',
          text: tip.detail ? undefined : tip.text,
          detail: tip.detail,
          size: tip.detail ? 'wide' : 'default',
          onclose: () => tooltips.dismiss(handle),
          onenter: () => tooltips.bubbleEnter(handle),
          onleave: () => tooltips.bubbleLeave(handle),
        },
      });
    },
    hide() {
      if (!shown) return;
      void unmount(shown);
      shown = null;
    },
  };

  const description = document.createElement('span');
  description.id = `tip-attached-${++counter}`;
  description.textContent = tip.detail ? tip.text || detailSentence(tip.detail) : tip.text;
  descriptionHost().append(description);
  const described = (el.getAttribute('aria-describedby') ?? '').split(/\s+/).filter(Boolean);
  el.setAttribute('aria-describedby', [...described, description.id].join(' '));

  let pointerReached = false;
  const stopIntent = whenPointerMoves(el, (event) => {
    // A finger has no hover; the sentence is still the description.
    if (event.pointerType === 'touch' || pointerReached) return;
    pointerReached = true;
    tooltips.hover(handle, tip.wait);
  });
  const onLeave = () => {
    pointerReached = false;
    tooltips.unhover(handle);
  };
  // A press inside the wait means the help is not wanted, and must not arrive late.
  const onPress = () => {
    pointerReached = true;
    tooltips.dismiss(handle);
  };
  const onFocus = () => {
    if (!focusWasPlaced() && el.matches(':focus-visible')) tooltips.focus(handle);
  };
  const onBlur = () => tooltips.blur(handle);
  el.addEventListener('pointerleave', onLeave);
  el.addEventListener('pointerdown', onPress);
  el.addEventListener('focus', onFocus);
  el.addEventListener('blur', onBlur);

  return () => {
    stopIntent();
    el.removeEventListener('pointerleave', onLeave);
    el.removeEventListener('pointerdown', onPress);
    el.removeEventListener('focus', onFocus);
    el.removeEventListener('blur', onBlur);
    tooltips.release(handle);
    handle.hide();
    description.remove();
    const rest = (el.getAttribute('aria-describedby') ?? '')
      .split(/\s+/)
      .filter((id) => id && id !== description.id);
    if (rest.length > 0) el.setAttribute('aria-describedby', rest.join(' '));
    else el.removeAttribute('aria-describedby');
  };
}
