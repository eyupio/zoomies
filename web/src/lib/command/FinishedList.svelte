<!--
  The rail's Just finished section: the jobs that ended in the last ten
  minutes, newest first, with how each ended and how long it took.

  How it ended is said by shape as well as colour: a tick for a success, a
  triangle for a failure, the neutral square for a job cancelled or skipped,
  each with the word as its accessible name. A failed job's name is in the
  danger colour too, because a wall is read from further away than a mark is
  drawn.
-->
<script lang="ts">
  import { CircleCheck, Square, TriangleAlert } from '@lucide/svelte';
  import type { Job } from '$lib/api/types';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import { formatDuration, NO_VALUE } from '$lib/format';
  import { readableJobText } from '$lib/jobs/name';
  import { FINISHED_SHOWN, finishedOutcome } from './jobs-merge';

  interface Props {
    jobs: readonly Job[];
    count: number;
    /** Where "and N more" leads: the Jobs page over the same window. */
    moreHref: string;
    error: unknown;
    onopen: (job: Job) => void;
    onretry: () => void;
  }
  let { jobs, count, moreHref, error, onopen, onretry }: Props = $props();

  const shown = $derived(jobs.slice(0, FINISHED_SHOWN));
  const more = $derived(count - shown.length);
</script>

<section class="rail-section" aria-labelledby="command-finished">
  <h2 id="command-finished">
    Just finished <span class="count">{count}</span>
  </h2>
  {#if error}
    <ErrorState compact {error} title="The finished jobs did not load." {onretry} />
  {:else if jobs.length === 0}
    <p class="empty">Nothing has finished in the last ten minutes.</p>
  {:else}
    <ul>
      {#each shown as job (job.id)}
        {@const outcome = finishedOutcome(job)}
        <li>
          <button type="button" class="row" data-outcome={outcome.kind} onclick={() => onopen(job)}>
            <span class="mark outcome {outcome.kind}" role="img" aria-label={outcome.label}>
              {#if outcome.kind === 'succeeded'}
                <CircleCheck size={16} aria-hidden="true" />
              {:else if outcome.kind === 'failed'}
                <TriangleAlert size={16} aria-hidden="true" />
              {:else}
                <Square size={14} aria-hidden="true" />
              {/if}
            </span>
            <span class="what">
              <span class="name">{readableJobText(job.job_name) || 'Unnamed job'}</span>
              <span class="where">{job.repo || NO_VALUE}</span>
            </span>
            <span class="side">
              <span class="took"
                >{typeof job.duration_ms === 'number'
                  ? formatDuration(job.duration_ms)
                  : NO_VALUE}</span
              >
            </span>
          </button>
        </li>
      {/each}
    </ul>
    {#if more > 0}
      <a class="more" href={moreHref}>and {more} more</a>
    {/if}
  {/if}
</section>

<style>
  .outcome.succeeded {
    color: var(--z-idle);
  }
  .outcome.failed,
  [data-outcome='failed'] .name {
    color: var(--z-danger);
  }
  .outcome.neutral {
    color: var(--z-neutral);
  }
  .took {
    color: var(--z-text-muted);
    font-variant-numeric: tabular-nums;
  }
</style>
