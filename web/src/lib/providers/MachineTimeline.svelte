<!--
  How a machine spent its life: one row per phase it actually reached, with how
  long it stayed there.

  The controller renders the phases from the timestamps the row carries, so a
  phase a provider skipped -- one whose create leaves the guest running never
  passes through "starting" -- is absent rather than shown as zero. The last
  row keeps counting while the machine is still on its way, because the whole
  reason somebody has this page open is that something is taking longer than
  they expected.
-->
<script lang="ts">
  import type { MachineTimelineEntry } from '$lib/api/types';
  import { ratio } from '$lib/format';
  import { machineStatus } from '$lib/status';
  import { phaseLabel, phaseState } from './machines';
  import Duration from '$lib/components/Duration.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import StatusDot from '$lib/components/StatusDot.svelte';

  interface Props {
    entries: readonly MachineTimelineEntry[];
    /** True while the machine is still moving, so the last row keeps counting. */
    running?: boolean;
    class?: string;
  }

  let { entries, running = false, class: className = '' }: Props = $props();

  interface Row {
    entry: MachineTimelineEntry;
    /** How long this phase lasted, or null while it is still the current one. */
    ms: number | null;
  }

  // A phase's duration is the gap to the next mark. The controller does not
  // send one, and computing it here rather than there is deliberate: the last
  // row's duration is "until now", and only the page knows when now is.
  const rows = $derived.by<Row[]>(() =>
    entries.map((entry, index) => {
      const next = entries[index + 1];
      if (!next) return { entry, ms: null };
      const from = Date.parse(entry.at ?? '');
      const to = Date.parse(next.at ?? '');
      return {
        entry,
        ms: Number.isFinite(from) && Number.isFinite(to) ? Math.max(0, to - from) : null,
      };
    }),
  );

  const total = $derived(rows.reduce((sum, row) => sum + Math.max(0, row.ms ?? 0), 0));

  function share(row: Row): number {
    if (total <= 0) return 0;
    return ratio((row.ms ?? 0) / total);
  }
</script>

{#if rows.length === 0}
  <EmptyState
    compact
    title="No phases yet"
    description="The controller stamps a machine's phases as it reaches them."
  />
{:else}
  <ol class="timeline {className}">
    {#each rows as row, index (`${row.entry.phase}-${row.entry.at}-${index}`)}
      {@const status = machineStatus(phaseState(row.entry.phase))}
      {@const last = index === rows.length - 1}
      <li style="--entry-colour: {status.colour}">
        <span class="marker" aria-hidden="true"><StatusDot {status} size="sm" /></span>
        <div class="body">
          <div class="head">
            <span class="phase">{phaseLabel(row.entry.phase)}</span>
            <span class="spent tabular">
              {#if last && running}
                <Duration from={row.entry.at} live />
                <span class="still">so far</span>
              {:else}
                <Duration ms={row.ms} />
              {/if}
            </span>
            <RelativeTime value={row.entry.at} class="when" />
          </div>
          <div class="bar" aria-hidden="true">
            <span class="fill" style="width: {(share(row) * 100).toFixed(1)}%"></span>
          </div>
        </div>
      </li>
    {/each}
  </ol>
{/if}

<style>
  .timeline {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  li {
    position: relative;
    display: flex;
    gap: var(--z-space-3);
    padding-bottom: var(--z-space-4);
  }
  li:last-child {
    padding-bottom: 0;
  }
  /* The thread between the phases. It stops at the last one. */
  li:not(:last-child)::before {
    content: '';
    position: absolute;
    left: var(--z-space-1);
    top: var(--z-space-4);
    bottom: 0;
    width: var(--z-nudge-1);
    background: var(--z-border);
  }
  .marker {
    display: flex;
    align-items: center;
    height: var(--z-leading-sm);
    flex: none;
  }
  .body {
    flex: 1;
    min-width: 0;
  }
  .head {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: var(--z-space-2) var(--z-space-3);
  }
  .phase {
    font-weight: var(--z-weight-medium);
    color: var(--entry-colour);
  }
  .spent {
    font-size: var(--z-text-sm);
    color: var(--z-text);
  }
  .still {
    color: var(--z-text-subtle);
  }
  .head :global(.when) {
    font-size: var(--z-text-xs);
    color: var(--z-text-subtle);
    margin-left: auto;
  }
  .bar {
    margin-top: var(--z-space-2);
    height: var(--z-space-1);
    border-radius: var(--z-radius-full);
    background: var(--z-surface-sunken);
    overflow: hidden;
  }
  .fill {
    display: block;
    height: 100%;
    min-width: var(--z-nudge-2);
    border-radius: var(--z-radius-full);
    background: var(--entry-colour);
  }
</style>
