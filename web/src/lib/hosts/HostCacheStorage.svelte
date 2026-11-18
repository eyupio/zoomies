<script lang="ts">
  import type { Host } from '$lib/api/types';
  import { formatBytes } from '$lib/format';
  import Tooltip from '$lib/components/Tooltip.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import { cacheStorageSummary } from './cache-storage';

  let { host }: { host: Host } = $props();
  const summary = $derived(cacheStorageSummary(host));
  // A finger has no hover: a tap opens the same bubble, as it does for the
  // memory pills, so the breakdown is not something only a mouse can read.
  let open = $state(false);
  const help = $derived(
    summary
      ? `Packages ${formatBytes(summary.packages)}, tools ${formatBytes(summary.tools)}, Actions service ${formatBytes(summary.service)}. Current cache namespaces only.`
      : '',
  );
</script>

{#if summary}
  <p class="cache-storage" class:over={summary.over} data-testid="host-cache-storage">
    <Tooltip text={help} bind:open>
      <button
        type="button"
        class="help-term"
        aria-expanded={open}
        onclick={(event) => {
          event.currentTarget.focus();
          open = true;
        }}
        >Cache: {summary.measuredAt
          ? formatBytes(summary.bytes)
          : 'size unknown'}{summary.partial && summary.measuredAt ? ' (partial)' : ''}</button
      >
    </Tooltip>
    {#if summary.over}<span>Over pool limit</span>{/if}
    {#if summary.measuredAt}<RelativeTime value={summary.measuredAt} prefix="measured" />{/if}
    {#if summary.reasons}<span>{summary.reasons}</span>{/if}
  </p>
{/if}

<style>
  .cache-storage {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--z-space-2);
    margin: 0;
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
  .cache-storage.over {
    color: var(--z-pending);
  }
  .help-term {
    color: inherit;
    font: inherit;
    background: none;
    border: 0;
    padding: 0;
    border-bottom: var(--z-border-width) dotted var(--z-border-strong);
    cursor: help;
  }
</style>
