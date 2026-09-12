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
      status: runnerStatus(state),
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
        {#if i < steps.length - 1}
          <!-- Idle and busy trade places as jobs come and go; every other
               step is one way. -->
          <span class="arrow" aria-hidden="true">{step.state === 'idle' ? '⇄' : '→'}</span>
        {/if}
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
  .steps {
    display: flex;
    flex-wrap: wrap;
    align-items: stretch;
    gap: var(--z-space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .step {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    min-width: 0;
  }
  .step a {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-1);
    min-width: 7rem;
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
    align-items: center;
    gap: var(--z-space-2);
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    white-space: nowrap;
  }
  .count {
    font-size: var(--z-text-xl);
    line-height: var(--z-leading-xl);
    font-weight: var(--z-weight-semibold);
    font-variant-numeric: tabular-nums;
    transition: color var(--z-motion-base) var(--z-ease);
  }
  .arrow {
    color: var(--z-text-subtle);
    font-size: var(--z-text-base);
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
</style>
