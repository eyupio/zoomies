<script lang="ts">
  import type { Host } from '$lib/api/types';
  import ChartPanel from '$lib/components/ChartPanel.svelte';
  import { hostSignals } from './signals';
  let {
    hosts,
    compact = false,
    onmanage,
  }: { hosts: Host[]; compact?: boolean; onmanage?: (host: Host) => void } = $props();
  let expanded = $state(false);
  const ranked = $derived(
    [...hosts].sort(
      (a, b) =>
        Number(hostSignals(a).eligible) - Number(hostSignals(b).eligible) ||
        (a.name ?? '').localeCompare(b.name ?? ''),
    ),
  );
  const visible = $derived(expanded ? ranked : ranked.slice(0, compact ? 4 : 8));
  function resources(h: Host) {
    const s = hostSignals(h);
    return [
      {
        label: 'Runner slots used',
        value: s.capacity && s.used !== null ? (100 * s.used) / s.capacity : null,
        text: s.used !== null && s.capacity !== null ? `${s.used} / ${s.capacity}` : 'Not reported',
        warning: s.free === 0,
      },
      {
        label: 'CPU committed',
        value: s.cpu,
        text: s.cpu === null ? 'Not measured' : `${s.cpu.toFixed(0)}%`,
        warning: (s.cpu ?? 0) >= 90,
      },
      {
        label: 'Memory committed',
        value: s.memory,
        text: s.memory === null ? 'Not measured' : `${s.memory.toFixed(0)}%`,
        warning: (s.memory ?? 0) >= 90,
      },
      {
        label: 'Disk free',
        value: s.diskFree,
        text: s.diskFree === null ? 'Not measured' : `${s.diskFree.toFixed(0)}%`,
        warning: s.diskFree !== null && s.diskFree <= 10,
      },
    ];
  }
</script>

<ChartPanel
  title="Host capacity map"
  description="Slots and resource commitments from the latest host snapshot. CPU and memory are reserved allocations, not measured utilisation. Available slots do not guarantee a compatible runner fits."
>
  <div class="map">
    {#each visible as host (host.id)}{@const s = hostSignals(host)}
      <article class="host" class:attention={!s.eligible || s.free === 0}>
        <header>
          <a href={`/runners?host_id=${encodeURIComponent(host.id ?? '')}`}
            >{host.name ?? host.id}</a
          ><span class="state" class:ready={s.eligible && s.free !== 0}>{s.state}</span>
        </header>
        <p>
          {host.platform_label ??
            `${host.os ?? 'Unknown OS'} · ${host.arch ?? 'Unknown architecture'}`}
        </p>
        <div class="resources">
          {#each resources(host) as r (r.label)}<div class="resource">
              <span>{r.label}</span><strong>{r.text}</strong>
              <div
                class="gauge"
                class:unknown={r.value === null}
                class:warning={r.warning}
                aria-hidden="true"
              >
                <span style:width={`${Math.min(100, Math.max(0, r.value ?? 0))}%`}></span>
              </div>
            </div>{/each}
        </div>
        <footer>
          {#if onmanage}<button class="manage" onclick={() => onmanage?.(host)}>Manage host</button
            >{/if}
          <span
            >{s.eligible
              ? `${s.free ?? 'Unknown'} free runner slots`
              : 'Excluded from new placements'}</span
          ><a href={`/usage?group_by=host&entity=${encodeURIComponent(host.id ?? '')}`}
            >Usage history ↗</a
          >
        </footer>
      </article>
    {:else}<p>No hosts connected yet.</p>{/each}
  </div>
  {#if ranked.length > (compact ? 4 : 8)}<button
      class="expand"
      onclick={() => (expanded = !expanded)}
      >{expanded ? 'Show fewer hosts' : `Show all ${ranked.length} hosts`}</button
    >{/if}
</ChartPanel>

<style>
  .map {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 260px), 1fr));
    gap: var(--z-space-3);
  }
  .host {
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    padding: var(--z-space-4);
    min-width: 0;
    background: var(--z-surface);
  }
  .host.attention {
    border-color: var(--z-pending-border);
  }
  header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--z-space-3);
    flex-wrap: wrap;
  }
  header a {
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
    overflow-wrap: anywhere;
  }
  .state {
    font-size: var(--z-text-2xs);
    padding: var(--z-space-1) var(--z-space-2);
    border-radius: var(--z-radius-full);
    color: var(--z-pending);
    background: var(--z-pending-subtle);
  }
  .state.ready {
    color: var(--z-idle);
    background: var(--z-idle-subtle);
  }
  p {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    margin: var(--z-space-2) 0 var(--z-space-4);
  }
  .resources {
    display: grid;
    gap: var(--z-space-3);
  }
  .resource {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: var(--z-space-1) var(--z-space-2);
    font-size: var(--z-text-xs);
  }
  .resource > span {
    color: var(--z-text-muted);
  }
  strong {
    font-variant-numeric: tabular-nums;
    font-weight: var(--z-weight-medium);
  }
  .gauge {
    grid-column: 1/-1;
    height: var(--z-space-1);
    border-radius: var(--z-radius-full);
    background: var(--z-surface-sunken);
    overflow: hidden;
  }
  .gauge span {
    display: block;
    height: 100%;
    background: var(--z-accent);
  }
  .gauge.warning span {
    background: var(--z-pending);
  }
  .gauge.unknown {
    border: var(--z-border-width) dashed var(--z-border-strong);
  }
  footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    gap: var(--z-space-2);
    font-size: var(--z-text-xs);
    margin-top: var(--z-space-4);
    color: var(--z-text-muted);
  }
  footer a {
    color: var(--z-accent);
  }
  .expand {
    margin-top: var(--z-space-4);
    padding: var(--z-space-2) var(--z-space-3);
    color: var(--z-accent);
    background: var(--z-accent-subtle);
    border: var(--z-border-width) solid var(--z-accent-border);
    border-radius: var(--z-radius-sm);
    cursor: pointer;
  }
  .manage {
    padding: 0;
    border: 0;
    background: none;
    color: var(--z-accent);
    font: inherit;
    cursor: pointer;
    text-decoration: underline;
  }
</style>
