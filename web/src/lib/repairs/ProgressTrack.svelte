<!--
  Where a repair that is still going has got to. The answer comes first: the
  current step, how long it has been, and the promise that nobody has to watch.
  The full five-step track is one press away for somebody who wants it, because
  the controller cannot report anything more precise than the stage it is on.
-->
<script lang="ts">
  import { Check } from '@lucide/svelte';
  import type { Schemas } from '$lib/api/types';
  import Panel from '$lib/components/Panel.svelte';
  import StatusDot from '$lib/components/StatusDot.svelte';
  import { formatDuration } from '$lib/format';
  import { repairStatus } from '$lib/status';
  import { guidanceFor } from './guidance';
  import { trackSteps } from './journey';

  interface Props {
    repair: Schemas['EliRepair'];
    /** The time the page believes it is, in milliseconds, so the elapsed time can run on. */
    now: number;
  }

  let { repair, now }: Props = $props();

  const steps = $derived(trackSteps(repair));
  const current = $derived(steps.find((step) => step.state === 'current')?.label ?? 'Working');
  const elapsed = $derived(formatDuration(Math.max(0, now - Date.parse(repair.created_at))));
  const guidance = $derived(guidanceFor(repair));
  const busy = repairStatus('working');
  const words = { done: 'Done', current: 'In progress', todo: 'Not started' } as const;
</script>

<Panel title="Progress" description="Updates every few seconds while Eli works.">
  {#snippet actions()}
    <span class="elapsed">Elapsed {elapsed}</span>
  {/snippet}

  <p class="now">
    <StatusDot status={busy} size="sm" tip={false} />
    <strong>{current}</strong>
    <span>{guidance.steps[0] ?? 'You can leave this page. The repair carries on without it.'}</span>
  </p>

  <details class="fold">
    <summary>Show the steps</summary>
    <ol class="track" aria-label="What Eli is doing">
      {#each steps as step (step.id)}
        <li class="step {step.state}" aria-current={step.state === 'current' ? 'step' : undefined}>
          <span class="node" aria-hidden="true">
            {#if step.state === 'done'}
              <Check size={12} />
            {:else if step.state === 'current'}
              <StatusDot status={busy} size="sm" tip={false} />
            {/if}
          </span>
          <span class="label">{step.label}</span>
          <span class="when">{words[step.state]}</span>
        </li>
      {/each}
    </ol>
  </details>
</Panel>

<style>
  .elapsed {
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .now {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-2) var(--z-space-3);
    margin: 0;
    padding: var(--z-space-4) var(--z-space-5);
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
  }
  .now strong {
    font-weight: var(--z-weight-semibold);
  }
  .now span {
    color: var(--z-text-muted);
  }
  .fold {
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .fold > summary {
    padding: var(--z-space-3) var(--z-space-5);
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
    cursor: pointer;
  }
  .track {
    display: grid;
    grid-template-columns: repeat(5, minmax(0, 1fr));
    margin: 0;
    padding: var(--z-space-2) var(--z-space-5) var(--z-space-5);
    list-style: none;
  }
  .step {
    position: relative;
    min-width: 0;
    padding: var(--z-space-8) var(--z-space-3) 0 0;
  }
  .step::before {
    content: '';
    position: absolute;
    top: 0.625rem;
    left: 1.625rem;
    right: var(--z-space-1);
    height: var(--z-border-width-thick);
    background: var(--z-border);
  }
  .step:last-child::before {
    display: none;
  }
  .step.done::before {
    background: var(--z-idle);
  }
  .node {
    position: absolute;
    top: 0;
    left: 0;
    display: grid;
    place-items: center;
    width: 1.375rem;
    height: 1.375rem;
    border: var(--z-border-width-thick) solid var(--z-border);
    border-radius: var(--z-radius-full);
    background: var(--z-surface);
    color: var(--z-text-subtle);
  }
  .done .node {
    border-color: var(--z-idle);
    background: var(--z-idle-subtle);
    color: var(--z-idle);
  }
  .current .node {
    border-color: var(--z-busy);
    color: var(--z-busy);
  }
  .label {
    display: block;
    font-weight: var(--z-weight-semibold);
    overflow-wrap: anywhere;
  }
  .todo .label {
    color: var(--z-text-muted);
    font-weight: var(--z-weight-medium);
  }
  .when {
    display: block;
    margin-top: var(--z-space-1);
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
  }
  @media (max-width: 640px) {
    .track {
      grid-template-columns: minmax(0, 1fr);
    }
    .step {
      padding: 0 0 var(--z-space-5) 2.25rem;
    }
    .step:last-child {
      padding-bottom: 0;
    }
    .step::before {
      top: 1.5rem;
      right: auto;
      bottom: 0;
      left: 0.625rem;
      width: var(--z-border-width-thick);
      height: auto;
    }
  }
</style>
