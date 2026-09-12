<script lang="ts">
  import { listProvisioning } from '$lib/api/client';
  import { events } from '$lib/api/sse';
  import { fleet } from '$lib/state/fleet.svelte';
  import Duration from '$lib/components/Duration.svelte';
  import { untrack } from 'svelte';
  let { poolId = '' }: { poolId?: string } = $props();
  let counts = $state<Record<string, number> | null>(null);
  let failed = $state(false);
  let oldest = $state<string | null>(null);
  let retry = $state(0);
  $effect(() => {
    const id = poolId;
    void retry;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    let disposed = false;
    let fetching = false;
    let dirty = false;
    counts = null;
    failed = false;
    oldest = null;
    async function load() {
      if (fetching) {
        dirty = true;
        return;
      }
      fetching = true;
      try {
        const result = await listProvisioning(
          { limit: 1, sort: 'queued_at', order: 'asc', pool_id: id ? [id] : undefined },
          controller.signal,
        );
        if (!disposed) {
          counts = result.counts ?? null;
          oldest = result.items?.[0]?.queued_at ?? null;
          failed = false;
        }
      } catch {
        if (!disposed) failed = true;
      } finally {
        fetching = false;
        if (dirty && !disposed) {
          dirty = false;
          schedule();
        }
      }
    }
    function schedule() {
      if (timer) return;
      timer = setTimeout(() => {
        timer = undefined;
        void load();
      }, 1500);
    }
    void load();
    const unsubscribe = events.subscribe('job.updated', schedule);
    const offReset = events.subscribe('resync', schedule);
    return () => {
      disposed = true;
      controller.abort();
      clearTimeout(timer);
      unsubscribe();
      offReset();
    };
  });
  let previous = '';
  $effect(() => {
    const next = fleet.connection;
    if (next === 'live' && previous && previous !== 'live') untrack(() => (retry += 1));
    previous = next;
  });
</script>

<div class="pulse" aria-label="Provisioning demand context">
  <span>Provisioning demand</span>
  {#if counts}{#each [{ key: 'ready', label: 'Ready' }, { key: 'expedited', label: 'Run now' }, { key: 'paused', label: 'Paused' }, { key: 'deleted', label: 'Removed' }] as item (item.key)}<a
        class={item.key}
        href={`/queue?provisioning=${item.key}${poolId ? `&pool_id=${encodeURIComponent(poolId)}` : ''}`}
        ><strong>{counts[item.key] ?? 0}</strong> {item.label}</a
      >{/each}{:else}<span>{failed ? 'Counts unavailable' : 'Loading counts…'}</span>{/if}
  {#if oldest}<span>Oldest queued demand: <Duration from={oldest} live /></span>{/if}
  {#if failed}<span>Last received counts</span><button onclick={() => (retry += 1)}
      >Refresh counts</button
    >{/if}
  <a class="manage" href={`/queue${poolId ? `?pool_id=${encodeURIComponent(poolId)}` : ''}`}
    >Manage queue ↗</a
  >
</div>

<style>
  .pulse {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-2);
    padding: var(--z-space-3) var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
    margin-bottom: var(--z-space-4);
    font-size: var(--z-text-xs);
  }
  .pulse > span {
    color: var(--z-text-muted);
    margin-right: var(--z-space-2);
  }
  a {
    padding: var(--z-space-2);
    border-radius: var(--z-radius-sm);
    color: var(--z-text);
    text-decoration: none;
    background: var(--z-surface-sunken);
  }
  a:hover {
    text-decoration: underline;
  }
  strong {
    font-variant-numeric: tabular-nums;
  }
  .expedited {
    color: var(--z-accent);
    background: var(--z-accent-subtle);
  }
  .paused {
    color: var(--z-pending);
    background: var(--z-pending-subtle);
  }
  .deleted {
    color: var(--z-text-muted);
  }
  .manage {
    margin-left: auto;
    color: var(--z-accent);
    background: none;
  }
  button {
    color: var(--z-accent);
    background: none;
    border: 0;
    cursor: pointer;
  }
</style>
