<!--
  The runner state machine, drawn: provisioning, registering, idle and busy,
  then draining, with how many runners are at each step right now.

  The store enforces this order -- a runner cannot skip from provisioning to
  busy -- so the flow is the fleet's own shape and not a diagram somebody
  drew. Each step is the shape and colour every other page gives the state,
  carries its count, and links to the runners in it; the runners that left
  the flow sideways, by failing, are named beside it rather than in it.
-->
<script lang="ts">
  import type { Stats } from '$lib/api/types';
  import { formatNumber, pluralise } from '$lib/format';
  import { prefs } from '$lib/state/prefs.svelte';
  import { runnerStatus } from '$lib/status';
  import StatusDot from '$lib/components/StatusDot.svelte';
  import Tooltip from '$lib/components/Tooltip.svelte';

  let { runners }: { runners: NonNullable<Stats['runners']> } = $props();

  /**
   * The states a runner passes through, in the order the store allows. Failed
   * and removed are exits from the flow rather than steps of it.
   */
  const STEPS = ['provisioning', 'registering', 'idle', 'busy', 'draining'] as const;
  const steps = $derived(
    STEPS.map((state) => ({
      state,
      status: runnerStatus(state, prefs.quirkyStatus),
      count: runners[state] ?? 0,
    })),
  );
  const live = $derived(steps.reduce((n, s) => n + s.count, 0));
  const failed = $derived(runners.failed ?? 0);
</script>

<div class="flow">
  <ol class="steps" aria-label="Runner lifecycle, in order">
    {#each steps as step, i (step.state)}
      <li class="step" data-tone={step.status.tone} class:empty={step.count === 0}>
        <Tooltip
          text="{step.status.label}: {pluralise(step.count, 'runner')}. {step.status.hint ?? ''}"
        >
          {#snippet content()}
            <strong>{step.status.label}</strong>
            <span class="hint">{step.status.hint}</span>
          {/snippet}
          <a href="/runners?state={step.state}" aria-label="{step.status.label} runners">
            <span class="head"><StatusDot status={step.status} size="sm" />{step.status.label}</span
            >
            <span class="count">{formatNumber(step.count)}</span>
          </a>
        </Tooltip>
        <!-- Idle and busy trade places as jobs come and go; every other step
             is one way. The arrow is kept in the layout past the last step,
             and where a row ends, so that every card is the same width
             however many columns the flow wraps to. -->
        <span class="arrow" aria-hidden="true" class:spent={i === steps.length - 1}
          >{step.state === 'idle' ? '⇄' : '→'}</span
        >
      </li>
    {/each}
  </ol>
  <p class="aside">
    {pluralise(live, 'live runner')} in the flow.
    {#if failed > 0}
      <a class="failed" href="/runners?state=failed"
        >{pluralise(failed, 'runner')} left it by failing</a
      >, and {failed === 1 ? 'is' : 'are'} kept for the record.
    {:else}
      None has left it by failing.
    {/if}
    A runner that finishes draining is removed, and removed runners are not counted.
  </p>
</div>

<style>
  /* A grid rather than a wrapping row: wrapped flex items are as wide as
     their own label, so "Provisioning" and "Registering" ended up different
     sizes and the rows under them lined up with nothing. Equal columns keep
     the steps in step whatever the width. */
  .steps {
    display: grid;
    grid-template-columns: repeat(5, minmax(0, 1fr));
    align-items: stretch;
    gap: var(--z-space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .step {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: stretch;
    gap: var(--z-space-2);
    min-width: 0;
  }
  /* The tooltip wraps each card, so it is the grid cell that has to fill --
     otherwise a card sits at its label's width again. */
  .step :global(.tip-wrap) {
    display: flex;
    min-width: 0;
  }
  .step a {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--z-space-1);
    height: 100%;
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-left: var(--z-border-width-rail) solid var(--tone);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
    color: var(--z-text);
    text-decoration: none;
    transition:
      border-color var(--z-motion-fast) var(--z-ease),
      background-color var(--z-motion-fast) var(--z-ease);
  }
  .step a:hover {
    border-color: var(--z-border-strong);
    border-left-color: var(--tone);
    background: var(--z-surface-hover);
  }
  .step.empty a {
    background: var(--z-surface-sunken);
  }
  .step.empty .count {
    color: var(--z-text-subtle);
  }
  [data-tone='busy'] {
    --tone: var(--z-busy);
  }
  [data-tone='idle'] {
    --tone: var(--z-idle);
  }
  [data-tone='pending'] {
    --tone: var(--z-pending);
  }
  [data-tone='draining'] {
    --tone: var(--z-draining);
  }
  [data-tone='danger'] {
    --tone: var(--z-danger);
  }
  [data-tone='neutral'] {
    --tone: var(--z-neutral);
  }
  .head {
    display: inline-flex;
    min-width: 0;
    align-items: center;
    gap: var(--z-space-2);
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .count {
    font-size: var(--z-text-xl);
    line-height: var(--z-leading-xl);
    font-weight: var(--z-weight-semibold);
    font-variant-numeric: tabular-nums;
    transition: color var(--z-motion-base) var(--z-ease);
  }
  .arrow {
    align-self: center;
    width: var(--z-space-4);
    text-align: center;
    color: var(--z-text-subtle);
    font-size: var(--z-text-base);
  }
  /* The arrow after the last step never points anywhere; it holds its column
     so the last card matches the others. */
  .arrow.spent {
    visibility: hidden;
  }
  .hint {
    display: block;
    color: var(--z-text-muted);
  }
  .aside {
    margin: var(--z-space-3) 0 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .failed {
    color: var(--z-danger);
  }
  @media (max-width: 1024px) {
    .steps {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
    .step:nth-child(3n) .arrow {
      visibility: hidden;
    }
  }
  @media (max-width: 768px) {
    .steps {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .step:nth-child(3n) .arrow {
      visibility: visible;
    }
    .step:nth-child(2n) .arrow {
      visibility: hidden;
    }
    /* Two columns on a 360px phone leave "Provisioning" barely enough room,
       so the card gives back what the gaps and the arrow can spare. */
    .step {
      gap: var(--z-space-1);
    }
    .step a {
      padding: var(--z-space-3);
    }
    .arrow {
      width: var(--z-space-3);
    }
  }
</style>
