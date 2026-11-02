<!--
  This attempt, at a glance: which repair it is, who paid for the model, what
  was sent to it and which commit came of it. Repository, age and the failing
  head stay visible even when the supporting evidence is closed.

  Only what the row recorded is here. A fact the repair has not got yet (no model
  call, no commit) is left out until it exists, and one that is simply absent
  reads as the shared placeholder, so the card never says "0" for a thing nobody
  measured. It makes no claim about the repair's future beyond the standing
  promise in its last line, which is true of every repair.
-->
<script lang="ts">
  import CopyButton from '$lib/components/CopyButton.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import type { Schemas } from '$lib/api/types';
  import { formatBytes, formatDuration, NO_VALUE, shortId } from '$lib/format';
  import { providerWords, tookMs } from './attempt';
  import type { CaseView } from './case';
  import { commitUrl } from './links';

  interface Props {
    repair: Schemas['EliRepair'];
    view: CaseView;
  }

  let { repair, view }: Props = $props();

  const took = $derived(tookMs(repair));
  const calls = $derived(repair.details?.steps?.length ?? 0);
  const source = $derived(repair.details?.source_bytes ?? 0);
  const commit = $derived(repair.commit_sha);
  const commitLink = $derived(commitUrl(view.repoUrl, commit));
</script>

<div class="card">
  <h2>This attempt</h2>
  <dl class="facts">
    <dt>Repository</dt>
    <dd>{view.repo}</dd>
    {#if view.pullNumber}
      <dt>Pull request</dt>
      <dd>#{view.pullNumber}</dd>
    {/if}
    <dt>Age</dt>
    <dd><RelativeTime value={repair.created_at} /></dd>
    <dt>Last updated</dt>
    <dd><RelativeTime value={repair.updated_at} /></dd>
    <dt>Failing head</dt>
    <dd class="mono">
      {repair.head_sha.slice(0, 7)}<CopyButton
        value={repair.head_sha}
        label="Copy the failing head SHA"
      />
    </dd>
    <dt>Attempt</dt>
    <dd class="mono">
      {shortId(repair.id)}
      <CopyButton value={repair.id} label="Copy the repair id" />
    </dd>
    {#if repair.trigger === 'automatic'}
      <dt>Started by</dt>
      <dd>Automatic repairs, by the repository’s policy</dd>
    {/if}
    {#if took !== undefined}
      <dt>Took</dt>
      <dd class="tnum">{formatDuration(took)}</dd>
    {/if}
    <dt>Provider</dt>
    <dd>{providerWords(repair)}</dd>
    <dt>Model</dt>
    <dd>{repair.details?.model || NO_VALUE}</dd>
    {#if source > 0}
      <dt>Source supplied</dt>
      <dd class="tnum">{formatBytes(source)}</dd>
    {/if}
    {#if calls > 0}
      <dt>Model calls</dt>
      <dd class="tnum">{calls}</dd>
    {/if}
    <dt>Commit pushed</dt>
    {#if commit}
      <dd class="mono">
        {#if commitLink}
          <a href={commitLink} target="_blank" rel="noreferrer"
            >{commit.slice(0, 7)}<span class="sr-only"> (opens in a new tab)</span></a
          >
        {:else}
          {commit.slice(0, 7)}
        {/if}
      </dd>
    {:else}
      <dd>None</dd>
    {/if}
    {#if view.branch}
      <dt>Pushes to</dt>
      <dd class="mono">{view.branch}</dd>
    {/if}
  </dl>
  <p class="fine">
    Eli pushes one commit and never merges. A newer commit on the branch stops the repair without
    changing anything.
  </p>
</div>

<style>
  .card {
    padding: var(--z-space-5);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  h2 {
    margin: 0;
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
    line-height: var(--z-leading-base);
  }
  .facts {
    display: grid;
    grid-template-columns: minmax(0, 7.5rem) minmax(0, 1fr);
    gap: var(--z-space-2) var(--z-space-3);
    margin: var(--z-space-4) 0 0;
    font-size: var(--z-text-sm);
  }
  .facts dt {
    color: var(--z-text-muted);
  }
  .facts dd {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-1) var(--z-space-2);
    min-width: 0;
    margin: 0;
    overflow-wrap: anywhere;
  }
  .mono {
    font-family: var(--z-font-mono);
    font-size: var(--z-text-xs);
  }
  .tnum {
    font-variant-numeric: tabular-nums;
  }
  .fine {
    margin: var(--z-space-4) 0 0;
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
  }
</style>
