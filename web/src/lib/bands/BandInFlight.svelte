<!--
  The Workflows band's picture: the work in flight by repository, a square per
  job, running solid and waiting half-shade, the repository with the most
  first. The sentence says nine jobs are in flight; this says whose.

  It asks for the runs with work in hand itself, rather than reading the grid's
  page: the grid shows whatever view and filters the operator chose, and a
  picture of "running" runs alone would say fewer waiting than the sentence
  beside it. One request, repeated when the fleet's own queued or running
  count moves and after a reconnect, so the rows follow the figures. A failed
  fetch draws nothing, as every picture on a stage does.
-->
<script lang="ts">
  import Tooltip from '$lib/components/Tooltip.svelte';
  import { listWorkflowRuns } from '$lib/api/client';
  import type { WorkflowRun } from '$lib/api/types';
  import { fleet } from '$lib/state/fleet.svelte';
  import { href } from '$lib/router';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import { inFlight, MAX_FLIGHT_SQUARES } from './in-flight';

  let runs = $state.raw<WorkflowRun[]>([]);
  let failed = $state(false);
  let loading = $state(true);
  let pending: AbortController | null = null;

  export async function reload(): Promise<void> {
    pending?.abort();
    const controller = new AbortController();
    pending = controller;
    try {
      const page = await listWorkflowRuns(
        {
          state: ['waiting', 'queued', 'in_progress'],
          managed: true,
          cancelling: false,
          limit: 100,
        },
        controller.signal,
      );
      if (controller.signal.aborted || pending !== controller) return;
      runs = page.items ?? [];
      failed = false;
    } catch {
      if (controller.signal.aborted || pending !== controller) return;
      failed = true;
    } finally {
      if (!controller.signal.aborted && pending === controller) loading = false;
    }
  }

  // The fleet's own counts are the sentence; when they move, the rows under
  // them are asked again, a moment later so a burst of frames is one request.
  const key = $derived(
    `${fleet.stats?.fleet?.running_jobs ?? fleet.stats?.running_jobs ?? 0}:${fleet.stats?.fleet?.queued_jobs ?? fleet.stats?.queued_jobs ?? 0}:${fleet.connection === 'live'}`,
  );
  $effect(() => {
    void key;
    const timer = setTimeout(() => void reload(), 400);
    return () => {
      clearTimeout(timer);
      pending?.abort();
    };
  });

  const data = $derived(inFlight(runs));
</script>

<!-- While the first answer is on its way the picture holds its room with the
     shape it will have, so the page under the band does not move when it
     arrives; a hover that was over a row stays over it. -->
{#if loading && !failed}
  <div class="pic" aria-hidden="true">
    <div class="head"><span class="title">In flight, by repository</span></div>
    <div class="placeholder">
      {#each [0, 1, 2] as i (i)}<Skeleton width="100%" height="var(--z-space-4)" />{/each}
    </div>
  </div>
{:else if !failed && data.rows.length > 0}
  <div class="pic" data-testid="band-in-flight">
    <div class="head">
      <span class="title">In flight, by repository</span>
      <span class="summary">{data.summary}</span>
    </div>
    <ul class="rows">
      {#each data.rows as row (row.repo)}
        <li class="row">
          <Tooltip bare whenTruncated text={row.repo}>
            <a
              class="name"
              href={href(`/jobs?repo=${encodeURIComponent(row.repo)}`)}
              aria-label={`${row.repo}: ${row.note}`}>{row.repo}</a
            >
          </Tooltip>
          <span class="squares" aria-hidden="true">
            {#each row.squares.slice(0, MAX_FLIGHT_SQUARES) as kind, i (i)}
              <i class="sq" data-kind={kind}></i>
            {/each}
            {#if row.squares.length > MAX_FLIGHT_SQUARES}<span class="extra"
                >+{row.squares.length - MAX_FLIGHT_SQUARES}</span
              >{/if}
          </span>
          <span class="note" aria-hidden="true">{row.note}</span>
        </li>
      {/each}
    </ul>
    <div class="foot">
      <span class="legend" aria-hidden="true">
        <span><i class="sq" data-kind="running"></i>Running</span>
        <span><i class="sq" data-kind="waiting"></i>Waiting</span>
      </span>
      {#if data.more > 0}<span class="more"
          >and {data.more} more {data.more === 1 ? 'repository' : 'repositories'}</span
        >{/if}
    </div>
  </div>
{/if}

<style>
  .pic {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
    min-width: 0;
    width: 100%;
    color: var(--z-panel-text-muted);
  }
  .placeholder {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
    min-width: 12rem;
  }
  .head,
  .foot {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: var(--z-space-3);
    flex-wrap: wrap;
  }
  .title {
    font-size: var(--z-text-2xs);
    font-weight: var(--z-weight-semibold);
    letter-spacing: var(--z-tracking-wider);
    text-transform: uppercase;
  }
  .summary,
  .more {
    font-size: var(--z-text-xs);
    color: var(--z-panel-text-subtle);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .rows {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    gap: var(--z-space-2) var(--z-space-3);
    align-items: center;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .row {
    display: contents;
  }
  .name {
    max-width: 12rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
    color: var(--z-panel-text);
    text-decoration: none;
    border-radius: var(--z-radius-sm);
  }
  .name:hover {
    text-decoration: underline;
  }
  .name:focus-visible {
    outline: var(--z-focus-width) solid var(--z-brand-runner-blue);
    outline-offset: var(--z-focus-offset);
  }
  .squares {
    display: flex;
    align-items: center;
    gap: var(--z-nudge-3);
    min-width: 0;
    flex-wrap: wrap;
  }
  .extra {
    font-size: var(--z-text-2xs);
    color: var(--z-panel-text-subtle);
  }
  .note {
    font-size: var(--z-text-2xs);
    color: var(--z-panel-text-subtle);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .sq {
    display: block;
    width: var(--z-space-4);
    height: var(--z-space-4);
    border-radius: var(--z-nudge-2);
  }
  .sq[data-kind='running'] {
    background: var(--z-brand-runner-blue);
  }
  .sq[data-kind='waiting'] {
    background: color-mix(in srgb, var(--z-brand-runner-blue) 52%, var(--z-panel-raised));
  }
  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-1) var(--z-space-3);
    font-size: var(--z-text-2xs);
    color: var(--z-panel-text-subtle);
    align-items: center;
  }
  .legend span {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
  }
  .legend .sq {
    width: var(--z-space-2);
    height: var(--z-space-2);
  }
</style>
