<!--
  A multi-step form, for a flow that is genuinely a sequence: the AI Context
  setup, the provider form and the migration. The step list is a real list with
  `aria-current`, so where you are is never only a colour.

  A pool is not one. Its settings have no order an operator has to follow, so
  the pool editor is one page of sections that can each be reached in a tap; see
  "The pool editor" in docs/ui-guidelines.md.

  `jumpable` makes each stage in the list but the current one a button that goes
  straight to that stage, for a form whose answers can be given in any order and
  are all checked again at the end. Such a form leaves its last button pressable
  whatever the other stages hold, and has `onfinish` send the operator to the
  first one that is wrong (the provider form does, and says where on its review
  step): a button that is off for a reason on a stage they are not standing on
  explains nothing. Off, the list is only a picture of where you are, and the
  flow stays a sequence.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import { Check } from '@lucide/svelte';
  import Button from './Button.svelte';

  export interface WizardStep {
    id: string;
    title: string;
    description?: string;
  }

  interface Props {
    steps: readonly WizardStep[];
    current?: number;
    /** False while the current step is invalid; the Next button waits. */
    canAdvance?: boolean;
    busy?: boolean;
    /** Whether a stage in the list can be clicked to go straight to it. */
    jumpable?: boolean;
    nextLabel?: string;
    backLabel?: string;
    finishLabel?: string;
    cancelLabel?: string;
    onnext?: () => void | Promise<void>;
    onback?: () => void;
    onfinish?: () => void | Promise<void>;
    oncancel?: () => void;
    class?: string;
    children: Snippet<[WizardStep, number]>;
  }

  let {
    steps,
    current = $bindable(0),
    canAdvance = true,
    busy = false,
    jumpable = false,
    nextLabel = 'Next',
    backLabel = 'Back',
    finishLabel = 'Create',
    cancelLabel = 'Cancel',
    onnext,
    onback,
    onfinish,
    oncancel,
    class: className = '',
    children,
  }: Props = $props();

  const step = $derived(steps[current] ?? steps[0]);
  const isLast = $derived(current >= steps.length - 1);

  async function next(): Promise<void> {
    if (!canAdvance || busy) return;
    if (isLast) {
      await onfinish?.();
      return;
    }
    await onnext?.();
    if (current < steps.length - 1) current += 1;
  }

  function back(): void {
    if (current === 0) {
      oncancel?.();
      return;
    }
    onback?.();
    current -= 1;
  }

  /**
   * Go to a stage by its place in the list. Leaving a stage runs the same hook
   * Next or Back would, so whatever that stage marks as seen is marked here too.
   * Deliberately not gated on `canAdvance`: the stage being left may be invalid,
   * and the operator is allowed to look elsewhere. Whatever is wrong is caught
   * by `onfinish`.
   */
  async function jump(index: number): Promise<void> {
    if (busy || index === current || index < 0 || index >= steps.length) return;
    if (index > current) await onnext?.();
    else onback?.();
    current = index;
  }
</script>

{#snippet stage(s: WizardStep, i: number)}
  <span class="marker" aria-hidden="true">
    {#if i < current}<Check size={12} />{:else}{i + 1}{/if}
  </span>
  <span class="step-text">
    <span class="step-title">{s.title}</span>
    {#if s.description}<span class="step-description">{s.description}</span>{/if}
  </span>
{/snippet}

<div class="wizard {className}">
  <ol class="steps">
    {#each steps as s, i (s.id)}
      <!--
        `aria-current` is on the item, because a button's children are
        presentational and one set on a span inside it is not announced. The stage
        you are on is the one stage that goes nowhere, so it is never a button: a
        control that did nothing would be a tab stop, and a dead one.
      -->
      <li
        class:done={i < current}
        class:active={i === current}
        aria-current={i === current ? 'step' : undefined}
      >
        {#if jumpable && i !== current}
          <button type="button" class="step-link" disabled={busy} onclick={() => jump(i)}>
            {@render stage(s, i)}
          </button>
        {:else}
          <div class="step-link">
            {@render stage(s, i)}
          </div>
        {/if}
      </li>
    {/each}
  </ol>

  <div class="panel">
    {#if step}
      <h2>{step.title}</h2>
      {#if step.description}<p class="lede">{step.description}</p>{/if}
      <div class="content">{@render children(step, current)}</div>
    {/if}
  </div>

  <footer>
    <Button variant="ghost" onclick={back} disabled={busy}>
      {current === 0 ? cancelLabel : backLabel}
    </Button>
    <p class="progress" aria-live="polite">Step {current + 1} of {steps.length}</p>
    <Button variant="primary" onclick={next} disabled={!canAdvance} loading={busy}>
      {isLast ? finishLabel : nextLabel}
    </Button>
  </footer>
</div>

<style>
  .wizard {
    display: grid;
    grid-template-columns: 220px minmax(0, 1fr);
    grid-template-areas: 'steps panel' 'steps footer';
    gap: var(--z-space-6);
    align-items: start;
  }
  .steps {
    grid-area: steps;
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .step-link {
    display: flex;
    align-items: flex-start;
    gap: var(--z-space-3);
    width: 100%;
    margin: 0;
    padding: 0;
    border: 0;
    border-radius: var(--z-radius-sm);
    background: none;
    color: inherit;
    font: inherit;
    text-align: start;
  }
  button.step-link {
    cursor: pointer;
  }
  button.step-link:hover .step-title {
    color: var(--z-text);
  }
  /*
    A shadow is not drawn in forced-colours mode (Windows High Contrast), which
    would leave the stage a keyboard has reached with no indicator at all. The
    outline is transparent so nothing changes anywhere else, and that mode paints
    it in a system colour.
  */
  button.step-link:focus-visible {
    outline: var(--z-focus-width) solid transparent;
    box-shadow: var(--z-focus-ring);
  }
  button.step-link:disabled {
    cursor: default;
  }
  .marker {
    flex: none;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--z-space-5);
    height: var(--z-space-5);
    border: var(--z-border-width) solid var(--z-border-strong);
    border-radius: var(--z-radius-full);
    background: var(--z-surface);
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .active .marker {
    border-color: var(--z-accent);
    background: var(--z-accent);
    color: var(--z-accent-contrast);
  }
  .done .marker {
    border-color: var(--z-idle-border);
    background: var(--z-idle-subtle);
    color: var(--z-idle);
  }
  .step-text {
    display: flex;
    flex-direction: column;
    gap: var(--z-nudge-2);
    padding-top: var(--z-nudge-2);
  }
  .step-title {
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
    color: var(--z-text-muted);
  }
  .active .step-title {
    color: var(--z-text);
  }
  .step-description {
    font-size: var(--z-text-xs);
    color: var(--z-text-subtle);
  }
  .panel {
    grid-area: panel;
    padding: var(--z-space-6);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  h2 {
    margin: 0;
    font-size: var(--z-text-lg);
    font-weight: var(--z-weight-semibold);
  }
  .lede {
    margin: var(--z-space-1) 0 0;
    max-width: 68ch;
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
    color: var(--z-text-muted);
  }
  .content {
    margin-top: var(--z-space-5);
  }
  footer {
    grid-area: footer;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-4);
    /*
      Next sits at the end of this row, which is the window's bottom-right when
      the wizard is taller than the screen. The assistant's button is fixed
      there, so the row stops short of its lane rather than putting the one
      control a wizard exists for underneath a control that covers it.
    */
    padding-inline-end: var(--z-eli-lane);
  }
  .progress {
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-subtle);
  }
  @media (max-width: 1180px) {
    .wizard {
      grid-template-columns: minmax(0, 1fr);
      grid-template-areas: 'steps' 'panel' 'footer';
    }
    .steps {
      flex-direction: row;
      flex-wrap: wrap;
      gap: var(--z-space-4);
    }
    .step-description {
      display: none;
    }
  }
  /*
    On a phone the controls that move the wizard on stay on screen, above the
    navigation bar that is fixed to the bottom edge there (the same clearance the
    shell's footer reserves). Without this "Next" was however many screens of
    explainer below the choice it belongs to, and the first step of the pool
    wizard is four paragraphs long.
  */
  @media (max-width: 768px) {
    footer {
      position: sticky;
      bottom: calc(var(--z-space-16) + var(--z-safe-bottom));
      z-index: var(--z-layer-sticky);
      /* The lane is repeated here rather than inherited from the rule above,
         because this shorthand resets every side and the button is in the same
         corner on a phone. */
      padding: var(--z-space-3) var(--z-eli-lane) var(--z-space-3) 0;
      border-top: var(--z-border-width) solid var(--z-border);
      background: var(--z-bg);
    }
  }
</style>
