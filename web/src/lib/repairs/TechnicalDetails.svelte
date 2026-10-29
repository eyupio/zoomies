<!--
  What a repair did, for the person who has to work out why it did not do better:
  the model it used, how much source it was given, the stage it reached, the
  failure code the worker recorded, and every model call with its size and time.

  Supporting evidence stays closed until asked for. A call whose provider did not report usage says
  "Unknown", never zero: a zero would read as a call that cost nothing.
-->
<script lang="ts">
  import { ChevronRight } from '@lucide/svelte';
  import type { Schemas } from '$lib/api/types';
  import CopyButton from '$lib/components/CopyButton.svelte';
  import { formatBytes, formatDuration, formatNumber, NO_VALUE } from '$lib/format';
  import { investigationText, responseText } from './attempt';
  import { stageWords } from './journey';

  interface Props {
    repair: Schemas['EliRepair'];
  }

  let { repair }: Props = $props();

  const details = $derived(repair.details);
  const steps = $derived(details?.steps ?? []);
  const unknownUsage = $derived(steps.some((step) => !step.usage_reported));
  const elapsed = $derived(steps.reduce((sum, step) => sum + step.elapsed_ms, 0));
  const summary = $derived.by(() => {
    const call = steps.length
      ? `${formatNumber(steps.length)} model ${steps.length === 1 ? 'call' : 'calls'}`
      : 'No model calls';
    const source = details?.source_bytes ? `${formatBytes(details.source_bytes)} source` : '';
    const took = elapsed ? formatDuration(elapsed) : '';
    return [call, source, took].filter(Boolean).join(' · ');
  });

  // Supporting evidence is closed until somebody asks for it, even after a
  // repair stops: the outcome and the next action are what the page is for.
  let touched = $state<boolean | null>(null);
  const open = $derived(touched ?? false);
  const headingId = $props.id();
</script>

<details
  class="tech"
  {open}
  ontoggle={(event) => {
    touched = event.currentTarget.open;
  }}
>
  <summary>
    <ChevronRight size={16} class="chev" aria-hidden="true" />
    <span class="titles">
      <h2 id={headingId}>Technical details</h2>
      <span class="sub">{summary}</span>
    </span>
  </summary>

  <div class="body">
    <dl class="facts">
      <dt>Model</dt>
      <dd>{details?.model || NO_VALUE}</dd>
      <dt>Source supplied</dt>
      <dd>{details?.source_bytes ? formatBytes(details.source_bytes) : NO_VALUE}</dd>
      <dt>Stage reached</dt>
      <dd>{stageWords(details?.stage)}</dd>
      {#if details?.failure_code}
        <dt>Failure code</dt>
        <dd><code>{details.failure_code}</code></dd>
      {/if}
    </dl>

    {#if steps.length}
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div class="scroll" role="region" aria-label="Model calls" tabindex="0">
        <table>
          <thead>
            <tr>
              <th scope="col">Model call</th>
              <th scope="col" class="num">Input tokens</th>
              <th scope="col" class="num">Output tokens</th>
              <th scope="col" class="num">Request</th>
              <th scope="col" class="num">Time</th>
            </tr>
          </thead>
          <tbody>
            {#each steps as step, index (index)}
              <tr>
                <th scope="row">{index + 1}</th>
                <td class="num"
                  >{step.usage_reported ? formatNumber(step.input_tokens) : 'Unknown'}</td
                >
                <td class="num"
                  >{step.usage_reported ? formatNumber(step.output_tokens) : 'Unknown'}</td
                >
                <td class="num">{formatBytes(step.request_bytes)}</td>
                <td class="num">{formatDuration(step.elapsed_ms)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      {#if unknownUsage}
        <p class="note">Unknown means the provider did not report usage for that call.</p>
      {/if}
      <p class="note">
        Responses have recognised credentials and email addresses removed. They may include
        repository source; review them before sharing.
      </p>
      {#each steps as step, index (index)}
        <details class="response">
          <summary>Model response {index + 1}</summary>
          <div class="response-body">
            {#if step.error}<p class="note">Call error: {step.error}</p>{/if}
            {#if step.validation_error}
              <p class="note">Validation error: {step.validation_error}</p>
            {/if}
            {#if step.response_truncated}
              <p class="note">This response was shortened to the 96 KiB retention limit.</p>
            {/if}
            {#if step.response_recorded && step.response}
              <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
              <pre
                tabindex="0"
                aria-label="Response text for model call {index + 1}">{step.response}</pre>
              <CopyButton
                value={step.response}
                label="Copy model response {index + 1}"
                showLabel
                size="md"
              />
            {:else}
              <p class="note">{responseText(step)}</p>
            {/if}
          </div>
        </details>
      {/each}
    {:else}
      <p class="note">No model call was recorded for this repair.</p>
    {/if}

    <div class="copy">
      <CopyButton
        value={investigationText(repair)}
        label="Copy investigation details"
        showLabel
        size="md"
      />
    </div>
  </div>
</details>

<style>
  .tech {
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  summary {
    display: flex;
    align-items: center;
    gap: var(--z-space-3);
    padding: var(--z-space-4) var(--z-space-5);
    cursor: pointer;
    list-style: none;
  }
  summary::-webkit-details-marker {
    display: none;
  }
  summary :global(.chev) {
    flex: none;
    color: var(--z-text-subtle);
    transition: transform var(--z-motion-fast) var(--z-ease);
  }
  .tech[open] > summary :global(.chev) {
    transform: rotate(90deg);
  }
  .titles {
    min-width: 0;
  }
  h2 {
    margin: 0;
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
    line-height: var(--z-leading-base);
  }
  .sub {
    display: block;
    margin-top: var(--z-space-1);
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
  }
  .body {
    display: grid;
    gap: var(--z-space-4);
    padding: 0 var(--z-space-5) var(--z-space-5);
  }
  .facts {
    display: grid;
    grid-template-columns: var(--z-facts-columns);
    gap: var(--z-space-2) var(--z-space-4);
    margin: 0;
    font-size: var(--z-text-sm);
  }
  .facts dt {
    color: var(--z-text-muted);
  }
  .facts dd {
    min-width: 0;
    margin: 0;
    overflow-wrap: anywhere;
  }
  code {
    font-family: var(--z-font-mono);
    font-size: var(--z-text-xs);
  }
  /* The table keeps its width and scrolls inside this box, so a phone never
     scrolls sideways as a page. The box takes focus so a keyboard can scroll it. */
  .scroll {
    overflow-x: auto;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--z-text-sm);
  }
  th,
  td {
    padding: var(--z-space-2) var(--z-space-3);
    border-top: var(--z-border-width) solid var(--z-border);
    text-align: left;
    white-space: nowrap;
  }
  thead th {
    border-top: 0;
    background: var(--z-surface-sunken);
    color: var(--z-text-muted);
    font-weight: var(--z-weight-medium);
  }
  tbody th {
    font-weight: var(--z-weight-medium);
  }
  .num {
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
  .note {
    margin: 0;
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
  }
  .copy {
    justify-self: start;
  }
  .response {
    min-width: 0;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
  }
  .response summary {
    padding: var(--z-space-3);
    font-size: var(--z-text-sm);
  }
  .response-body {
    display: grid;
    justify-items: start;
    gap: var(--z-space-3);
    min-width: 0;
    padding: 0 var(--z-space-3) var(--z-space-3);
  }
  pre {
    box-sizing: border-box;
    width: 100%;
    max-height: 24rem;
    overflow: auto;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    margin: 0;
    padding: var(--z-space-3);
    background: var(--z-surface-sunken);
    color: var(--z-text);
    font-family: var(--z-font-mono);
    font-size: var(--z-text-xs);
  }
  @media (max-width: 640px) {
    summary {
      padding: var(--z-space-4);
    }
    .body {
      padding: 0 var(--z-space-4) var(--z-space-4);
    }
    .facts {
      grid-template-columns: minmax(0, 1fr);
      gap: var(--z-space-1);
    }
    .facts dd {
      margin-bottom: var(--z-space-2);
    }
  }
</style>
