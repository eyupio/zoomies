<!--
  The one button.

  `loading` swaps the label for a spinner without changing the button's width,
  because a row of actions that reflows while you are clicking it is how you
  click the wrong thing. Spinners appear here and nowhere else: content loads
  behind skeletons.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { LucideIcon } from '@lucide/svelte';
  import type { Side } from '../tooltip-place';
  import Tooltip from './Tooltip.svelte';

  interface Props {
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
    /** lg belongs to the one form that is the whole page: sign-in. */
    size?: 'sm' | 'md' | 'lg';
    type?: 'button' | 'submit' | 'reset';
    /**
     * The id of the form this button submits.
     *
     * A dialog pins its actions in a footer outside the scrolling body, so the
     * primary cannot be a descendant of the form it belongs to. This is how it
     * stays where it is and still means Enter.
     */
    form?: string;
    disabled?: boolean;
    loading?: boolean;
    /** Renders an anchor styled as a button. Use for navigation, not for actions. */
    href?: string;
    /**
     * Open the link in a new tab. Only meaningful with `href`, and the rule
     * for when to use it is simple: a link that leaves the product takes it,
     * and a link within the product does not. `rel` follows automatically,
     * and the accessible name gains "(opens in a new tab)", because a tab
     * appearing under somebody who cannot see it happen is disorienting.
     */
    newTab?: boolean;
    full?: boolean;
    icon?: LucideIcon;
    iconAfter?: LucideIcon;
    /**
     * Turn the leading icon. For an action whose whole effect is to bring what
     * is already on screen up to date: the label stays readable while it runs,
     * which `loading` deliberately does not do.
     */
    iconSpin?: boolean;
    /**
     * What it does, when the label is not enough: a sentence in the shared
     * tooltip, on hover and on keyboard focus, and linked as the button's
     * description. Never the only place something lives.
     */
    help?: string;
    /**
     * Why it cannot be used now, and what to do about it. With `disabled`, the
     * button is marked `aria-disabled` rather than `disabled`, so it can still be
     * hovered and focused and the reason can be reached.
     */
    reason?: string;
    /** The reason is a permission, so it is drawn with a padlock. */
    lock?: boolean;
    /** The keys that do the same thing, one cap each. */
    keys?: readonly string[];
    placement?: Side;
    ariaLabel?: string;
    ariaExpanded?: boolean;
    ariaControls?: string;
    ariaHaspopup?: 'menu' | 'dialog' | 'listbox' | 'true';
    onclick?: (event: MouseEvent) => void;
    class?: string;
    children: Snippet;
  }

  let {
    variant = 'secondary',
    size = 'md',
    type = 'button',
    form,
    disabled = false,
    loading = false,
    href,
    newTab = false,
    full = false,
    icon: Icon,
    iconAfter: IconAfter,
    iconSpin = false,
    help,
    reason,
    lock = false,
    keys,
    placement,
    ariaLabel,
    ariaExpanded,
    ariaControls,
    ariaHaspopup,
    onclick,
    class: className = '',
    children,
  }: Props = $props();

  const iconSize = $derived(size === 'sm' ? 13 : size === 'lg' ? 16 : 14);
  /**
   * Whether this must not be activated: told to be off, or busy with the last
   * activation. A button gets `disabled` for the first and refuses the click
   * for the second; a link has neither, so both come down to the same thing.
   */
  const inert = $derived(disabled || loading);
  /**
   * Off, with a reason to give. It is refused rather than disabled so that the
   * operator can still point at it: a natively disabled button takes no hover
   * and no focus, and the reason is the whole point.
   */
  const refused = $derived(disabled && Boolean(reason));
</script>

{#snippet body()}
  <span class="content" class:hidden={loading}>
    {#if Icon}
      <span class="lead" class:spinning={iconSpin}><Icon size={iconSize} aria-hidden="true" /></span
      >
    {/if}
    <span class="label">{@render children()}</span>
    {#if IconAfter}<IconAfter size={iconSize} aria-hidden="true" />{/if}
  </span>
  {#if loading}
    <span class="spinner" aria-hidden="true"></span>
    <span class="sr-only">Working</span>
  {/if}
{/snippet}

{#snippet control()}
  {#if href}
    <!--
    A link that is off has no `href`.

    `pointer-events: none` stops the mouse and nothing else: the anchor kept
    its href, so it stayed in the tab order and Enter still followed it, and it
    was still there to be copied or opened in a new tab from the context menu.
    An <a> without an href is not focusable and not activatable, which is the
    same refusal for everyone; `aria-disabled` and `aria-busy` are what say
    why, and the styling is unchanged because `.btn` does not depend on href.
  -->
    <a
      href={inert ? undefined : href}
      class="btn {variant} {size} {className}"
      class:full
      class:refused
      target={newTab && !inert ? '_blank' : undefined}
      rel={newTab && !inert ? 'noopener noreferrer' : undefined}
      role={refused ? 'link' : undefined}
      tabindex={refused ? 0 : undefined}
      aria-label={ariaLabel}
      aria-disabled={inert ? 'true' : undefined}
      aria-busy={loading || iconSpin ? 'true' : undefined}
    >
      {@render body()}
      {#if newTab}<span class="sr-only"> (opens in a new tab)</span>{/if}
    </a>
  {:else}
    <!--
    Loading is `aria-disabled`, not `disabled`.

    Disabling the element the operator has just activated makes the browser
    blur it, so focus falls to <body>. Inside a Dialog that also disarms the
    focus trap, which listens on the panel: a Tab pressed from <body> is never
    intercepted, and the operator walks out of an open modal into the page
    behind it. aria-disabled keeps the button focused and announced, the
    pointer is stopped in CSS, and the click handler refuses while busy.
  -->
    <button
      {type}
      {form}
      class="btn {variant} {size} {className}"
      class:full
      class:refused
      disabled={disabled && !refused}
      aria-disabled={loading || refused ? 'true' : undefined}
      aria-label={ariaLabel}
      aria-busy={loading || iconSpin ? 'true' : undefined}
      aria-expanded={ariaExpanded}
      aria-controls={ariaControls}
      aria-haspopup={ariaHaspopup}
      onclick={(event) => {
        if (loading || refused) {
          event.preventDefault();
          return;
        }
        onclick?.(event);
      }}
    >
      {@render body()}
    </button>
  {/if}
{/snippet}

<!--
  Always wrapped, with nothing to say unless `help`, `reason` or `keys` is given:
  wrapping only when there is help would rebuild the button, and drop its focus,
  the moment help appeared. The wrapper takes no box of its own.
-->
<Tooltip bare text={help ?? ''} {reason} {lock} {keys} {placement}>
  {@render control()}
</Tooltip>

<style>
  .btn {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--z-space-2);
    border: var(--z-border-width) solid transparent;
    border-radius: var(--z-radius-full);
    font-family: inherit;
    font-weight: var(--z-weight-medium);
    line-height: 1;
    text-decoration: none;
    white-space: nowrap;
    cursor: pointer;
    transition:
      background-color var(--z-motion-fast) var(--z-ease),
      border-color var(--z-motion-fast) var(--z-ease),
      color var(--z-motion-fast) var(--z-ease);
  }
  /* A button that is busy still holds focus, so it must not also be clickable
     -- otherwise a second press fires the same request again. */
  .btn[aria-disabled='true']:not(.refused) {
    pointer-events: none;
  }
  .btn.full {
    width: 100%;
  }
  .md {
    height: var(--z-space-8);
    padding: 0 var(--z-space-3);
    font-size: var(--z-text-sm);
  }
  .sm {
    height: var(--z-space-6);
    padding: 0 var(--z-space-2);
    font-size: var(--z-text-xs);
  }
  .lg {
    height: var(--z-space-10);
    padding: 0 var(--z-space-4);
    font-size: var(--z-text-base);
  }
  @media (pointer: coarse) {
    .lg {
      height: var(--z-control-touch);
    }
  }
  .btn:disabled,
  .btn[aria-disabled='true'] {
    cursor: not-allowed;
    opacity: 0.55;
  }

  /* The brand's own blue under Zoomies Black, 7.3:1 in both themes, where the
     darkened accent existed to carry white text (docs/brand.md). It names the
     brand constants and not --z-text, so a band that remaps the text tokens
     to the panel's cannot turn the label white on blue. */
  .primary {
    background: var(--z-brand-runner-blue);
    color: var(--z-brand-black);
  }
  .primary:hover:not(:disabled):not([aria-disabled='true']) {
    filter: brightness(1.12);
  }
  /* Held down. Without it a click on a slow action gives no feedback at all
     until the request comes back. */
  .primary:active:not(:disabled):not([aria-disabled='true']) {
    filter: brightness(0.94);
  }
  .secondary {
    background: var(--z-surface);
    color: var(--z-text);
    border-color: var(--z-border-strong);
  }
  .secondary:hover:not(:disabled):not([aria-disabled='true']) {
    background: var(--z-surface-hover);
  }
  .ghost {
    background: transparent;
    color: var(--z-text-muted);
  }
  .ghost:hover:not(:disabled):not([aria-disabled='true']) {
    background: var(--z-surface-hover);
    color: var(--z-text);
  }
  .danger {
    background: var(--z-danger);
    color: var(--z-danger-contrast);
  }
  .danger:hover:not(:disabled):not([aria-disabled='true']) {
    background: var(--z-danger-hover);
  }

  .content {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
  }
  .content.hidden {
    visibility: hidden;
  }
  .label {
    display: inline-block;
  }
  .lead {
    display: inline-flex;
  }
  .lead.spinning {
    animation: spin calc(var(--z-motion-slow) * 2) linear infinite;
  }

  .spinner {
    position: absolute;
    inset: 0;
    margin: auto;
    width: var(--z-control-icon);
    height: var(--z-control-icon);
    border: var(--z-border-width-thick) solid currentColor;
    border-top-color: transparent;
    border-radius: var(--z-radius-full);
    animation: spin calc(var(--z-motion-slow) * 2) linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    /* The ring still reads as "working"; the label is replaced and `aria-busy`
       says so. Nobody has to watch something spin to learn that, and the same
       goes for a turning icon. */
    .spinner,
    .lead.spinning {
      animation: none;
    }
  }
</style>
