<!--
  A failed job's error lines, read from GitHub when somebody asks.

  Nothing is stored. The controller reads the log, takes out anything that looks
  like a credential, and answers; this shows it and says so. A log can hold
  anything a workflow printed, so it is only read for a person whose GitHub
  account can write to the repository, and until that is true the box says why
  instead of asking.

  Every line is a text node. A job's output is written by whoever can run a
  workflow, and may hold markup, so nothing in it is ever interpreted.
-->
<script lang="ts">
  import { Lock, RefreshCw, Terminal } from '@lucide/svelte';
  import { onDestroy, untrack } from 'svelte';
  import { ApiError, getEliErrorOutput } from '$lib/api/client';
  import Button from '$lib/components/Button.svelte';
  import CopyButton from '$lib/components/CopyButton.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import { pluralise } from '$lib/format';
  import { cleanLines } from './error-output';

  interface Props {
    repo: string;
    /** GitHub's id for the job, which is what the log is asked for by. */
    jobId: number;
    /** Whether the person may read it. False draws the lock and asks for nothing. */
    enabled: boolean;
    /** Why not, when it is not enabled. */
    reason?: string;
    /** Told when the lines arrive, with whether there were any. */
    onloaded?: (hasLines: boolean) => void;
  }

  let { repo, jobId, enabled, reason = '', onloaded }: Props = $props();

  type Phase = 'idle' | 'loading' | 'loaded' | 'refused' | 'failed';
  let phase = $state<Phase>('idle');
  let lines = $state<string[]>([]);
  let note = $state('');
  let readAt = $state<string | null>(null);
  let message = $state('');
  let inFlight: AbortController | null = null;

  async function load(): Promise<void> {
    inFlight?.abort();
    const mine = new AbortController();
    inFlight = mine;
    phase = 'loading';
    try {
      const answer = await getEliErrorOutput({ repo, job_id: jobId }, mine.signal);
      if (mine.signal.aborted) return;
      lines = cleanLines(answer.lines);
      note = answer.note ?? '';
      readAt = new Date().toISOString();
      phase = 'loaded';
      onloaded?.(lines.length > 0);
    } catch (cause) {
      if (mine.signal.aborted) return;
      if (cause instanceof ApiError && cause.status === 409) {
        // The controller checked the person's access and said no: that is the
        // lock again, in its words, not a failure to try again.
        message = cause.message;
        phase = 'refused';
      } else {
        message = cause instanceof ApiError ? cause.message : 'Could not read the error output.';
        phase = 'failed';
      }
    }
  }

  // Read when first shown and whenever the job or the person's access changes.
  // Polling rebuilds the case's job objects even when their identity is unchanged.
  // Keep that invalidation from replacing a log the person is reading.
  let reading = '';
  $effect(() => {
    const key = JSON.stringify([repo, jobId]);
    if (!enabled) {
      reading = '';
      inFlight?.abort();
      phase = 'idle';
      return;
    }
    if (reading === key) return;
    reading = key;
    untrack(() => void load());
  });

  onDestroy(() => inFlight?.abort());

  const copyText = $derived(lines.join('\n'));
</script>

{#if !enabled || phase === 'refused'}
  <div class="locked">
    <Lock size={16} aria-hidden="true" />
    <div>
      <strong>Error output needs write access.</strong>
      {#if phase === 'refused'}
        {message}
      {:else}
        {reason ||
          'Job logs can hold anything a workflow prints, so Zoomies reads them only for someone whose GitHub account can write to this repository.'}
      {/if}
    </div>
  </div>
{:else if phase === 'failed'}
  <div class="failed" role="alert">
    <p>{message}</p>
    <Button size="sm" onclick={() => void load()}>Try again</Button>
  </div>
{:else if phase === 'loaded'}
  <div class="term">
    <div class="head">
      <span class="title"
        ><Terminal size={16} aria-hidden="true" />Error output, {pluralise(
          lines.length,
          'line',
        )}</span
      >
      <span class="acts">
        {#if lines.length}<CopyButton
            value={copyText}
            label="Copy the error output"
            showLabel
          />{/if}
        <Button variant="ghost" size="sm" icon={RefreshCw} onclick={() => void load()}
          >Fetch again</Button
        >
      </span>
    </div>
    {#if lines.length}
      <pre>{#each lines as line, index (index)}<span class="line">{line}</span>{/each}</pre>
    {:else}
      <p class="empty">{note || 'The job printed no error lines.'}</p>
    {/if}
    <div class="foot" role="status">
      Read from GitHub <RelativeTime value={readAt} plain />. Credentials are redacted, and nothing
      here is stored.
    </div>
  </div>
{:else}
  <div class="term" role="status" aria-busy="true">
    <div class="head">
      <span class="title"><Terminal size={16} aria-hidden="true" />Reading the log from GitHub</span
      >
    </div>
    <div class="wait"><Skeleton lines={3} height="0.9rem" /></div>
  </div>
{/if}

<style>
  .locked,
  .failed {
    display: flex;
    align-items: flex-start;
    gap: var(--z-space-3);
    padding: var(--z-space-3);
    border: var(--z-border-width) dashed var(--z-border-strong);
    border-radius: var(--z-radius-md);
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
  }
  .locked :global(svg) {
    flex: none;
    margin-top: var(--z-nudge-1);
  }
  .locked strong {
    color: var(--z-text);
    font-weight: var(--z-weight-medium);
  }
  .failed {
    flex-direction: column;
    align-items: flex-start;
    border-style: solid;
    border-color: var(--z-danger-border);
    background: var(--z-danger-subtle);
    color: var(--z-text);
  }
  .failed p {
    margin: 0;
  }
  .term {
    min-width: 0;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface-sunken);
    overflow: hidden;
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-2) var(--z-space-3);
    padding: var(--z-space-2) var(--z-space-3);
    border-bottom: var(--z-border-width) solid var(--z-border);
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
  }
  .title,
  .acts {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-2);
  }
  pre {
    margin: 0;
    padding: var(--z-space-3);
    font-family: var(--z-font-mono);
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-base);
    white-space: pre-wrap;
    /* A line of a log has no break points of its own: an unbroken path or token
       must wrap here or the page grows a sideways scrollbar on a phone. */
    overflow-wrap: anywhere;
  }
  .line {
    display: block;
  }
  .empty {
    margin: 0;
    padding: var(--z-space-3);
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
  }
  .wait {
    padding: var(--z-space-3);
  }
  .foot {
    padding: var(--z-space-2) var(--z-space-3);
    border-top: var(--z-border-width) solid var(--z-border);
    color: var(--z-text-subtle);
    font-size: var(--z-text-xs);
  }
</style>
