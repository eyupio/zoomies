<!--
  The one place draft publication is explained and turned on, beside the
  approved preview that needs it. It says what the write does, lets an
  administrator consent with one press and tells everybody else whom to ask.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { ApiError, getOptimisationEnablement, setOptimisationPublish } from '$lib/api/client';
  import type { OptimisationEnablement } from '$lib/api/types';
  import Button from '$lib/components/Button.svelte';
  import CopyButton from '$lib/components/CopyButton.svelte';

  let {
    enablement = $bindable<OptimisationEnablement | undefined>(undefined),
    refreshKey = 0,
  }: { enablement?: OptimisationEnablement; refreshKey?: number } = $props();
  let busy = $state(false),
    error = $state('');

  async function load() {
    try {
      enablement = await getOptimisationEnablement();
      error = '';
    } catch (cause) {
      error =
        cause instanceof ApiError ? cause.message : 'Could not read whether publishing is on.';
    }
  }
  async function turnOn() {
    if (busy) return;
    busy = true;
    error = '';
    try {
      enablement = await setOptimisationPublish(true);
    } catch (cause) {
      error =
        cause instanceof ApiError
          ? cause.message
          : 'Could not turn draft publishing on. Try again.';
    } finally {
      busy = false;
    }
  }
  onMount(() => {
    if (!enablement) void load();
  });
  $effect(() => {
    if (refreshKey > 0) void load();
  });
  const request =
    'Please turn on optimisation.source_reads, optimisation.workflow_edits and optimisation.publish (Settings, Optimisation) so I can open a draft pull request for an approved checkout preview in Zoomies.';
</script>

{#if enablement && !enablement.publish_enabled}
  <section class="gate" aria-labelledby="publish-gate-title">
    <h2 id="publish-gate-title">Turn on draft publishing</h2>
    <p>
      This lets an operator open one draft pull request for an approved preview. Zoomies commits the
      one reviewed line on a new branch and opens the draft against the branch the run came from. It
      never merges, never force-pushes and runs no job, and no measurement shows the change saves
      time. It also needs the two preview grants.
    </p>
    {#if enablement.can_publish}
      <Button onclick={turnOn} loading={busy} disabled={busy}>Turn on draft publishing</Button>
    {:else}
      <p>{enablement.publish_reason}</p>
      <CopyButton value={request} label="Copy a request for an administrator" showLabel />
    {/if}
    {#if error}<p role="alert">{error}</p>{/if}
  </section>
{:else if error}
  <p role="alert">{error}</p>
{/if}

<style>
  .gate {
    padding: var(--z-space-6);
    background: var(--z-surface);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-lg);
    min-width: 0;
  }
  h2 {
    font-size: var(--z-text-lg);
    font-weight: var(--z-weight-semibold);
    margin-block: var(--z-space-4);
  }
  p {
    margin-block: var(--z-space-3);
  }
</style>
