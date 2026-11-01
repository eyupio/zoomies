<!--
  A square button whose only content is an icon, so it always carries a label
  for anyone who cannot see the icon.
-->
<script lang="ts">
  import type { LucideIcon } from '@lucide/svelte';
  import type { Side } from '../tooltip-place';
  import Tooltip from './Tooltip.svelte';

  interface Props {
    icon: LucideIcon;
    /** Required. Becomes the accessible name and the heading of the tooltip. */
    label: string;
    /**
     * A sentence under the heading, for what the label does not say. It is
     * linked as the button's description, so it is never only in the tooltip.
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
    /** A selector for an ancestor the bubble clears when it goes beside the button. */
    edge?: string;
    variant?: 'ghost' | 'secondary' | 'danger';
    size?: 'sm' | 'md';
    disabled?: boolean;
    loading?: boolean;
    href?: string;
    /** An `href` that leaves the product: opens in a new tab, and says so. */
    newTab?: boolean;
    /** For toggle buttons: renders `aria-pressed`. */
    pressed?: boolean;
    expanded?: boolean;
    controls?: string;
    haspopup?: 'menu' | 'dialog' | 'listbox' | 'true';
    onclick?: (event: MouseEvent) => void;
    class?: string;
  }

  let {
    icon: Icon,
    label,
    help,
    reason,
    lock = false,
    keys,
    placement,
    edge,
    variant = 'ghost',
    size = 'md',
    disabled = false,
    loading = false,
    href,
    newTab = false,
    pressed,
    expanded,
    controls,
    haspopup,
    onclick,
    class: className = '',
  }: Props = $props();

  const iconSize = $derived(size === 'sm' ? 14 : 16);
  /** Off, with a reason to give: refused rather than disabled, so it can still be pointed at. */
  const refused = $derived(disabled && Boolean(reason));
</script>

{#snippet control()}
  {#if href}
    <a
      {href}
      class="icon-btn {variant} {size} {className}"
      class:refused
      aria-label={newTab ? `${label} (opens in a new tab)` : label}
      target={newTab ? '_blank' : undefined}
      rel={newTab ? 'noopener noreferrer' : undefined}
    >
      <Icon size={iconSize} aria-hidden="true" />
    </a>
  {:else}
    <!--
    Loading is `aria-disabled`, not `disabled`, for the reason Button gives:
    disabling the element the operator has just activated blurs it, so focus
    falls to <body> -- and when the request is refused and the editor stays
    open, the next Tab starts from the top of the page.
  -->
    <button
      type="button"
      class="icon-btn {variant} {size} {className}"
      class:refused
      aria-label={label}
      aria-pressed={pressed}
      aria-expanded={expanded}
      aria-controls={controls}
      aria-haspopup={haspopup}
      aria-busy={loading ? 'true' : undefined}
      aria-disabled={loading || refused ? 'true' : undefined}
      disabled={disabled && !refused}
      onclick={(event) => {
        if (loading || refused) {
          event.preventDefault();
          return;
        }
        onclick?.(event);
      }}
    >
      <Icon size={iconSize} aria-hidden="true" />
    </button>
  {/if}
{/snippet}

<!-- The label is the heading of the bubble: an icon alone shows no name. -->
<Tooltip bare title={label} text={help ?? ''} {reason} {lock} {keys} {placement} {edge}>
  {@render control()}
</Tooltip>

<style>
  .icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: var(--z-border-width) solid transparent;
    border-radius: var(--z-radius-full);
    background: transparent;
    color: var(--z-text-muted);
    cursor: pointer;
    transition:
      background-color var(--z-motion-fast) var(--z-ease),
      color var(--z-motion-fast) var(--z-ease);
  }
  .md {
    width: var(--z-space-8);
    height: var(--z-space-8);
  }
  .sm {
    width: var(--z-space-6);
    height: var(--z-space-6);
  }
  .ghost:hover:not(:disabled):not([aria-disabled='true']) {
    background: var(--z-surface-hover);
    color: var(--z-text);
  }
  .secondary {
    background: var(--z-surface);
    border-color: var(--z-border-strong);
    color: var(--z-text);
  }
  .secondary:hover:not(:disabled):not([aria-disabled='true']) {
    background: var(--z-surface-hover);
  }
  .danger {
    color: var(--z-danger);
  }
  .danger:hover:not(:disabled):not([aria-disabled='true']) {
    background: var(--z-danger-subtle);
  }
  /* A button that is busy still holds focus, so it must not also be clickable
     -- otherwise a second press fires the same request again. */
  .icon-btn[aria-disabled='true']:not(.refused) {
    pointer-events: none;
  }
  .icon-btn:disabled,
  .icon-btn[aria-disabled='true'] {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .icon-btn[aria-pressed='true'] {
    background: var(--z-accent-subtle);
    color: var(--z-accent);
  }
</style>
