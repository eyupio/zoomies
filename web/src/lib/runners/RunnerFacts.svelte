<!--
  The facts about a runner that do not change while you watch: where it came
  from, what it is made of, and when each part of its life happened.

  Identifiers are monospaced and copyable, because the next thing an operator
  does with a container ID is paste it into a shell.
-->
<script lang="ts">
  import type { RunnerDetail } from '$lib/api/types';
  import { formatNumber, shortId } from '$lib/format';
  import CopyButton from '$lib/components/CopyButton.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';

  interface Props {
    runner: RunnerDetail;
    class?: string;
  }

  let { runner, class: className = '' }: Props = $props();

  const labels = $derived(runner.labels ?? []);
</script>

<dl class="facts {className}">
  <div class="row">
    <dt>Pool</dt>
    <dd>
      {#if runner.pool_id}
        <a href="/pools/{runner.pool_id}">{runner.pool_name ?? runner.pool_id}</a>
      {:else}
        <span class="none">No pool</span>
      {/if}
    </dd>
  </div>

  <div class="row">
    <dt>Host</dt>
    <dd>
      {#if runner.host_name || runner.host_id}
        <a href="/hosts">{runner.host_name ?? runner.host_id}</a>
      {:else}
        <span class="none">Not placed yet</span>
      {/if}
    </dd>
  </div>

  <!--
    A runner that is not progressing is usually a host that has gone quiet, and
    until now answering that meant leaving this page for the Hosts page and
    finding the right row. The heartbeat is the first thing to look at, so it
    is here, beside the host it belongs to.
  -->
  {#if runner.host}
    <div class="row">
      <dt>Host last seen</dt>
      <dd>
        <RelativeTime value={runner.host.last_heartbeat} />
        {#if runner.host.healthy === false}
          <span class="warn">not heartbeating</span>
        {/if}
      </dd>
    </div>
  {/if}

  <div class="row">
    <dt>Requested image</dt>
    <dd class="mono break">{runner.image || '--'}</dd>
  </div>

  <div class="row">
    <dt>Resolved digest</dt>
    <dd class="mono break">{runner.image_digest || 'Not reported'}</dd>
  </div>

  <div class="row">
    <dt>Runner version</dt>
    <dd class="mono">{runner.runner_version || 'Unknown'}</dd>
  </div>

  <div class="row">
    <dt>Lifetime</dt>
    <dd>
      {runner.ephemeral === false ? 'Reused between jobs' : 'One job, then it exits'}
    </dd>
  </div>

  <div class="row">
    <dt>Labels</dt>
    <dd>
      {#if labels.length > 0}
        <ul class="labels">
          {#each labels as label (label)}
            <li class="mono">{label}</li>
          {/each}
        </ul>
      {:else}
        <span class="none">None</span>
      {/if}
    </dd>
  </div>

  {#if runner.container_id}
    <div class="row">
      <dt>Container</dt>
      <dd class="with-copy">
        <span class="mono" title={runner.container_id}>{shortId(runner.container_id, 6)}</span>
        <CopyButton value={runner.container_id} label="Copy the container ID" size="sm" />
      </dd>
    </div>
  {/if}

  {#if runner.github_runner_id}
    <div class="row">
      <dt>GitHub runner</dt>
      <dd class="tabular">{formatNumber(runner.github_runner_id)}</dd>
    </div>
  {/if}

  {#if runner.id}
    <div class="row">
      <dt>Runner ID</dt>
      <dd class="with-copy">
        <span class="mono" title={runner.id}>{shortId(runner.id)}</span>
        <CopyButton value={runner.id} label="Copy the runner ID" size="sm" />
      </dd>
    </div>
  {/if}

  <div class="row">
    <dt>Created</dt>
    <dd><RelativeTime value={runner.created_at} /></dd>
  </div>

  {#if runner.create_task_issued_at}
    <div class="row">
      <dt>Create issued to host</dt>
      <dd><RelativeTime value={runner.create_task_issued_at} /></dd>
    </div>
  {/if}

  {#if runner.container_started_at}
    <div class="row">
      <dt>Container started</dt>
      <dd><RelativeTime value={runner.container_started_at} /></dd>
    </div>
  {/if}

  {#if runner.registered_at}
    <div class="row">
      <dt>Registered with GitHub</dt>
      <dd><RelativeTime value={runner.registered_at} /></dd>
    </div>
  {/if}

  <!--
    Not "Registered": that is registered_at above. This is when the workload
    came up, and labelling it as the registration made a runner whose container
    started and never reached GitHub look like one that had.
  -->
  {#if runner.started_at}
    <div class="row">
      <dt>Workload up</dt>
      <dd><RelativeTime value={runner.started_at} /></dd>
    </div>
  {/if}

  {#if runner.last_idle_at}
    <div class="row">
      <dt>Last idle</dt>
      <dd><RelativeTime value={runner.last_idle_at} /></dd>
    </div>
  {/if}

  {#if runner.finished_at}
    <div class="row">
      <dt>Finished</dt>
      <dd><RelativeTime value={runner.finished_at} /></dd>
    </div>
  {/if}

  <!--
    Cleaned up is a different fact from finished, and the gap between them is
    the interesting part: finished is when the runner stopped working, cleaned
    up is when nothing of it was left. A terminal runner with no cleaned-up
    time is still awaiting one or both positive confirmations.
  -->
  {#if runner.host_removed_at}
    <div class="row">
      <dt>Removed from host</dt>
      <dd><RelativeTime value={runner.host_removed_at} /></dd>
    </div>
  {/if}
  {#if runner.registration_deleted_at}
    <div class="row">
      <dt>Removed from GitHub</dt>
      <dd><RelativeTime value={runner.registration_deleted_at} /></dd>
    </div>
  {/if}
  {#if runner.cleaned_up_at}
    <div class="row">
      <dt>Cleaned up</dt>
      <dd><RelativeTime value={runner.cleaned_up_at} /></dd>
    </div>
  {:else if runner.finished_at}
    <div class="row">
      <dt>Cleanup</dt>
      <dd>Awaiting confirmation</dd>
    </div>
  {/if}
  {#if runner.cleanup_estimated_at}
    <div class="row">
      <dt>Earlier cleanup estimate</dt>
      <dd><RelativeTime value={runner.cleanup_estimated_at} /></dd>
    </div>
  {/if}
</dl>

<style>
  .facts {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-3);
    margin: 0;
  }
  .row {
    display: grid;
    grid-template-columns: 8rem 1fr;
    align-items: baseline;
    gap: var(--z-space-3);
  }
  dt {
    font-size: var(--z-text-2xs);
    font-weight: var(--z-weight-medium);
    text-transform: uppercase;
    letter-spacing: var(--z-tracking-wide);
    color: var(--z-text-subtle);
  }
  dd {
    margin: 0;
    min-width: 0;
    font-size: var(--z-text-sm);
    color: var(--z-text);
  }
  .break {
    overflow-wrap: anywhere;
  }
  .none {
    color: var(--z-text-subtle);
  }
  .warn {
    margin-left: var(--z-space-2);
    color: var(--z-danger);
  }
  .with-copy {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
  }
  a {
    color: var(--z-accent);
    text-decoration: none;
  }
  a:hover {
    text-decoration: underline;
  }
  .labels {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .labels li {
    padding: 0 var(--z-space-2);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface-sunken);
    font-size: var(--z-text-2xs);
    line-height: var(--z-leading-2xs);
    color: var(--z-text-muted);
  }
  @media (max-width: 768px) {
    .row {
      grid-template-columns: 1fr;
      gap: var(--z-space-1);
    }
  }
</style>
