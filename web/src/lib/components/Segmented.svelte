<!--
  One choice among a few, as one control: the chosen option is filled, the
  rest are the surface, and a hairline joins them so the row reads as a
  choice being made rather than several things to do. The quick ranges on
  the activity matrix and the windows on the fleet trend are both this.

  Buttons with aria-pressed rather than radios: the choice takes effect at
  once, there is nothing to submit, and a screen reader says "pressed" for
  the one in force, which is the whole state.
-->
<script lang="ts">
  export interface SegmentedOption<T extends string = string> {
    value: T;
    /** What the button says: short, since there are several in a row. */
    label: string;
    /** The full name, for the tooltip and assistive technology. */
    name?: string;
  }

  interface Props<T extends string> {
    options: readonly SegmentedOption<T>[];
    value: T;
    /** What the choice is, for the group's accessible name. */
    label: string;
    onchange: (value: T) => void;
    class?: string;
  }

  let { options, value, label, onchange, class: className = '' }: Props<string> = $props();
</script>

<div class="segmented {className}" role="group" aria-label={label}>
  {#each options as option (option.value)}
    <button
      type="button"
      aria-pressed={value === option.value}
      aria-label={option.name}
      title={option.name}
      onclick={() => onchange(option.value)}>{option.label}</button
    >
  {/each}
</div>

<style>
  .segmented {
    display: inline-flex;
    /* Never squeezed: the group clips its own corners, so a flex row that
       shrank it would cut the last options off rather than wrap them. */
    flex: none;
    border: var(--z-border-width) solid var(--z-border-strong);
    border-radius: var(--z-radius-md);
    overflow: hidden;
  }
  button {
    /* The height of the other small controls, so a row of them lines up. */
    height: var(--z-space-6);
    padding: 0 var(--z-space-3);
    border: 0;
    border-left: var(--z-border-width) solid var(--z-border-strong);
    background: var(--z-surface);
    color: var(--z-text-muted);
    font: inherit;
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    cursor: pointer;
    transition:
      background-color var(--z-motion-fast) var(--z-ease),
      color var(--z-motion-fast) var(--z-ease);
  }
  button:first-child {
    border-left: 0;
  }
  button:hover {
    background: var(--z-surface-hover);
    color: var(--z-text);
  }
  button[aria-pressed='true'] {
    background: var(--z-accent);
    color: var(--z-accent-contrast);
  }
  button:focus-visible {
    /* Inside a clipped, rounded group the outline would be cut off, so the
       ring is drawn inward, as the tokens provide for. */
    outline: none;
    box-shadow: inset var(--z-focus-ring);
  }
</style>
