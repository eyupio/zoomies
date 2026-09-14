<script module lang="ts">
  import type { LucideIcon } from '@lucide/svelte';

  export interface RowAction {
    id: string;
    /** The verb, and the start of the button's accessible name: "Pause". */
    label: string;
    icon: LucideIcon;
    /** Renders in the danger colour. */
    danger?: boolean;
    disabled?: boolean;
    /**
     * Why it cannot be done, for the operator who wonders. A refusal that does
     * not say what to change is a refusal that gets reported as a bug.
     */
    reason?: string;
    onSelect: () => void;
  }
</script>

<!--
  A row's actions, as buttons rather than a menu.

  The actions an operator repeats all day -- pause this, run this now -- are
  worth a press each rather than a press to open a menu and a second to choose
  from it. The cost of putting them on the row is the keyboard: a menu is one
  tab stop per row, and four loose buttons would be four, which on a full page
  is a hundred stops between the grid and whatever follows it.

  So this is the ARIA toolbar pattern, which exists for exactly that trade: the
  group is one tab stop, and the arrow keys move along it. Left and Right are
  free inside a cell -- the grid's own keys are Up, Down, Home, End, Enter and
  Space, and its handler ignores anything whose target is not the row itself.

  The name of each button carries what it acts on, because "Pause" repeated
  twenty-five times down a page is twenty-five buttons a screen reader cannot
  tell apart, and because a test that reaches for the one in the selection bar
  should not find a row instead.

  An action already in force is refused rather than removed: `aria-disabled`,
  not the attribute. A natively disabled button cannot be focused, which cost
  the keyboard twice over -- the reason it gives for refusing was reachable
  only with a pointer, and confirming an action that made its own button
  unavailable dropped focus to the top of the document, because the button the
  dialog meant to give focus back to could no longer take it.
-->
<script lang="ts">
  interface Props {
    actions: readonly RowAction[];
    /** What these actions act on: "acme/api · build". It names the toolbar and every button. */
    subject: string;
    class?: string;
  }

  let { actions, subject, class: className = '' }: Props = $props();

  let group = $state<HTMLDivElement | null>(null);
  /** Where the arrow keys have got to, and so the one button in the tab order. */
  let active = $state(0);
  const tabStop = $derived(active < actions.length ? active : 0);

  /** Move along the row, wrapping round. */
  function focusAt(index: number): void {
    if (actions.length === 0) return;
    const next = ((index % actions.length) + actions.length) % actions.length;
    active = next;
    group?.querySelector<HTMLElement>(`[data-action-index="${next}"]`)?.focus();
  }

  function onKeydown(event: KeyboardEvent): void {
    switch (event.key) {
      case 'ArrowRight':
        event.preventDefault();
        focusAt(active + 1);
        break;
      case 'ArrowLeft':
        event.preventDefault();
        focusAt(active - 1);
        break;
      // Prevented, or the page jumps to its top and bottom under a toolbar
      // that has its own ends to go to.
      case 'Home':
        event.preventDefault();
        focusAt(0);
        break;
      case 'End':
        event.preventDefault();
        focusAt(actions.length - 1);
        break;
      default:
        break;
    }
  }
</script>

<!--
  The row itself opens the drawer, so the group swallows the click before it
  gets there. One handler for all of them: a control that forgot its own would
  navigate away instead of acting.

  The group carries `tabindex="-1"`: a toolbar's focus lives on its buttons and
  never on the group, and -1 keeps it out of the tab order while still
  satisfying the rule that an element with an interactive role is focusable.
-->
<div
  bind:this={group}
  class="row-actions {className}"
  role="toolbar"
  aria-label="Actions for {subject}"
  aria-orientation="horizontal"
  tabindex={-1}
  onkeydown={onKeydown}
  onclick={(event) => event.stopPropagation()}
>
  {#each actions as action, index (action.id)}
    {@const Icon = action.icon}
    {@const refused = action.disabled ? (action.reason ?? '') : ''}
    <button
      type="button"
      data-action-index={index}
      class:danger={action.danger}
      tabindex={index === tabStop ? 0 : -1}
      aria-disabled={action.disabled ? 'true' : undefined}
      aria-label={refused
        ? `${action.label}: ${subject}. ${refused}`
        : `${action.label}: ${subject}`}
      title={refused || action.label}
      onfocus={() => (active = index)}
      onclick={() => {
        if (!action.disabled) action.onSelect();
      }}
    >
      <Icon size={14} aria-hidden="true" />
    </button>
  {/each}
</div>

<style>
  .row-actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: var(--z-space-1);
  }
  button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--z-space-6);
    height: var(--z-space-6);
    border: var(--z-border-width) solid transparent;
    border-radius: var(--z-radius-md);
    background: transparent;
    color: var(--z-text-muted);
    cursor: pointer;
    transition:
      background-color var(--z-motion-fast) var(--z-ease),
      color var(--z-motion-fast) var(--z-ease);
  }
  button:not([aria-disabled]):hover {
    background: var(--z-surface-hover);
    color: var(--z-text);
  }
  button.danger {
    color: var(--z-danger);
  }
  button.danger:not([aria-disabled]):hover {
    background: var(--z-danger-subtle);
  }
  button[aria-disabled='true'] {
    opacity: 0.5;
    cursor: not-allowed;
  }
  button:focus-visible {
    outline: var(--z-focus-width) solid var(--z-focus-colour);
    outline-offset: calc(-1 * var(--z-focus-offset));
  }
</style>
