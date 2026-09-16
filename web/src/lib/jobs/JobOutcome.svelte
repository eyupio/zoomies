<!--
  Why a job went wrong, said first and said plainly.

  A failed job has two possible authors, and the drawer's first duty is to name
  the right one. When the fleet's own runner stopped under the job, that is
  said before anything else, because GitHub records it as an ordinary failure
  and the workflow's owner will otherwise go looking for a bug that is not
  there. Otherwise the step that failed is named, so the operator knows which
  log to open before they leave for GitHub.

  Naming the author is only half of it. The category says which of the fleet's
  problems this is, and the fix -- which the server sends, so the UI, the CLI
  and the problems drawer cannot offer three different remedies -- says what to
  change. A panel that said "the runner stopped" and left somebody to work out
  whether that meant a memory limit or an expired registry credential was a
  panel that told them they had a problem and nothing more.
-->
<script lang="ts">
  import { ExternalLink, RotateCcw, TriangleAlert } from '@lucide/svelte';
  import type { Job } from '$lib/api/types';
  import { faultDetail, faultLabel, fleetFailed } from '$lib/faults';
  import { formatDuration, toMillis } from '$lib/format';
  import { jobStatus } from '$lib/status';

  interface Props {
    job: Job;
    /** Set when the viewer may ask GitHub to run the run's failed jobs again. */
    onRerun?: (() => void) | null;
    rerunning?: boolean;
    class?: string;
  }

  let { job, onRerun = null, rerunning = false, class: className = '' }: Props = $props();

  const status = $derived(jobStatus(job.state, job.conclusion));
  const step = $derived(job.failed_step ?? null);
  const ours = $derived(fleetFailed(job));
  const kindLabel = $derived(faultLabel(job.fault_kind));
  const kindDetail = $derived(faultDetail(job.fault_kind));

  /** How long the failing step ran, when both of its stamps are known. */
  const stepTook = $derived.by(() => {
    if (!step) return null;
    const from = toMillis(step.started_at);
    const to = toMillis(step.completed_at);
    return from === null || to === null ? null : to - from;
  });

  const heading = $derived.by(() => {
    // The category leads when there is one: "Out of memory" is a heading
    // somebody can act on, and "The runner stopped under this job" is the same
    // news with the useful half removed.
    if (ours && kindLabel) return kindLabel;
    if (job.runner_fault) return 'The runner stopped under this job';
    if (step) return `${status.label} at step ${step.number ?? '?'}, ${step.name ?? 'unnamed'}`;
    if (job.state === 'completed')
      return `${status.label} after ${formatDuration(job.duration_ms)}`;
    return status.label;
  });
</script>

<div class="note {className}" role="note" aria-label="Why this job went wrong">
  <TriangleAlert size={16} aria-hidden="true" class="icon" />
  <div class="body">
    <p class="heading">{heading}</p>
    {#if ours}
      {#if kindDetail}
        <p class="detail">{kindDetail}</p>
      {/if}
      {#if job.runner_fault}
        <p class="detail muted">{job.runner_fault}.</p>
      {/if}
      <p class="detail">
        GitHub records this as an ordinary failure; the workflow did nothing wrong.
      </p>
      {#if job.fault_fix}
        <p class="detail fix"><span class="fix-label">Fix</span> {job.fault_fix}</p>
      {/if}
      <div class="actions">
        {#if onRerun}
          <!--
            aria-disabled, not disabled: this sits inside the job drawer's
            focus trap. A natively disabled button is blurred by the browser,
            so focus would fall to <body> and the trap's next Tab would never
            intercept it -- walking the operator straight out of an open
            drawer. aria-disabled keeps it focused and announced, and the
            click handler refuses while busy.
          -->
          <button
            class="action rerun"
            type="button"
            aria-disabled={rerunning ? 'true' : undefined}
            aria-busy={rerunning ? 'true' : undefined}
            onclick={() => {
              if (!rerunning) onRerun?.();
            }}
          >
            <RotateCcw size={13} aria-hidden="true" />
            {rerunning ? 'Asking GitHub…' : 'Run it again'}
          </button>
        {/if}
        {#if job.runner_id}
          <a class="action" href="/runners/{job.runner_id}">Open the runner</a>
        {/if}
      </div>
      {#if onRerun}
        <p class="detail muted">
          GitHub has no job-level re-run, so this runs every failed job in the run again.
        </p>
      {/if}
    {:else if step}
      <p class="detail">
        {#if stepTook !== null}
          The step ran for {formatDuration(stepTook)} before it {step.conclusion === 'timed_out'
            ? 'timed out'
            : step.conclusion === 'cancelled'
              ? 'was cancelled'
              : 'failed'}.
        {/if}
        Every step after it was skipped. Its output is on GitHub.
      </p>
      {#if job.html_url}
        <a class="action" href={job.html_url} target="_blank" rel="noopener noreferrer">
          Open the failed step's log
          <ExternalLink size={13} aria-hidden="true" />
          <span class="sr-only">(opens in a new tab)</span>
        </a>
      {/if}
    {:else}
      <p class="detail">
        GitHub reported the job {status.label.toLowerCase()} without naming a step. The run on GitHub
        has the detail.
      </p>
    {/if}
  </div>
</div>

<style>
  .note {
    display: flex;
    align-items: flex-start;
    gap: var(--z-space-3);
    padding: var(--z-space-3);
    border: var(--z-border-width) solid var(--z-danger-border);
    border-radius: var(--z-radius-md);
    background: var(--z-danger-subtle);
  }
  .note :global(.icon) {
    flex: none;
    color: var(--z-danger);
  }
  .body {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--z-space-2);
    min-width: 0;
  }
  .heading {
    margin: 0;
    font-size: var(--z-text-base);
    line-height: var(--z-leading-base);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
    overflow-wrap: anywhere;
  }
  .detail {
    margin: 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    color: var(--z-text);
    max-width: 70ch;
    overflow-wrap: anywhere;
  }
  .muted {
    color: var(--z-text-muted);
  }
  .fix-label {
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .fix {
    color: var(--z-text);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-3);
  }
  .rerun {
    border: var(--z-border-width) solid var(--z-danger-border);
    border-radius: var(--z-radius-sm);
    padding: var(--z-space-1) var(--z-space-2);
    background: transparent;
    cursor: pointer;
    font: inherit;
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
  }
  .rerun:hover:not([aria-disabled='true']) {
    background: var(--z-surface);
  }
  /* A button that is busy still holds focus, so it must not also be
     clickable -- otherwise a second press fires the same request again. */
  .rerun[aria-disabled='true'] {
    pointer-events: none;
    cursor: progress;
    color: var(--z-text-muted);
  }
  .action {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
    color: var(--z-accent);
    text-decoration: none;
  }
  .action:hover {
    text-decoration: underline;
  }
</style>
