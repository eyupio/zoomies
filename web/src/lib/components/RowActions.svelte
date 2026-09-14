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
  /** Where the arrow keys have got to. */
  let active = $state(0);

  /**
   * The one button in the tab order.
   *
   * Never a disabled one: a disabled button cannot take focus, so a tab stop
   * on it is no tab stop at all and the whole toolbar falls out of the keyboard
   * order -- which on a row whose first action is already in force is every
   * such row on the page. `-1` when nothing here can be pressed, which is the
   * truth rather than a trap.
   */
  const tabStop = $derived(
    active < actions.length && !actions[active]?.disabled
      ? active
      : actions.findIndex((action) => !action.disabled),
  );

  /** The next button along that can actually be pressed, wrapping round. */
  function step(from: number, delta: number): void {
    for (let i = 1; i <= actions.length; i++) {
      const next = (from + delta * i + actions.length * i) % actions.length;
      if (!actions[next]?.disabled) {
        active = next;
        group?.querySelector<HTMLElement>(`[data-action-index="${next}"]`)?.focus();
        return;
      }
    }
  }

  function onKeydown(event: KeyboardEvent): void {
    switch (event.key) {
      case 'ArrowRight':
        event.preventDefault();
        step(active, 1);
        break;
      case 'ArrowLeft':
        event.preventDefault();
        step(active, -1);
        break;
      case 'Home':
        // Stopped as well as prevented: the grid reads Home as "the first row".
        event.preventDefault();
        event.stopPropagation();
        step(actions.length - 1, 1);
        break;
      case 'End':
        event.preventDefault();
        event.stopPropagation();
        step(0, -1);
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
    <button
      type="button"
      data-action-index={index}
      class:danger={action.danger}
      tabindex={index === tabStop ? 0 : -1}
      disabled={action.disabled}
      aria-label="{action.label}: {subject}"
      title={action.disabled && action.reason ? action.reason : action.label}
      onfocus={() => (active = index)}
      onclick={action.onSelect}
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
  button:hover:not(:disabled) {
    background: var(--z-surface-hover);
    color: var(--z-text);
  }
  button.danger {
    color: var(--z-danger);
  }
  button.danger:hover:not(:disabled) {
    background: var(--z-danger-subtle);
  }
  button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  button:focus-visible {
    outline: var(--z-focus-width) solid var(--z-focus-colour);
    outline-offset: calc(-1 * var(--z-focus-offset));
  }
</style>
