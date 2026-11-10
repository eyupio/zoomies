<!--
  The rail's Waiting section: the jobs this fleet has a hand in that wait for
  a runner, longest first, with how long and why.

  A row opens the job drawer in place rather than leaving the page, because a
  job has no page of its own and a wall display should stay where it is. The
  reason is the Queue page's own, from the same helper, so the two pages never
  give one job two explanations.
-->
<script lang="ts">
  import type { Job } from '$lib/api/types';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import OffStatusIcon from '$lib/components/OffStatusIcon.svelte';
  import { formatDuration, NO_VALUE } from '$lib/format';
  import { queuedActivity } from '$lib/jobs/activity-status';
  import { readableJobText } from '$lib/jobs/name';
  import { prefs } from '$lib/state/prefs.svelte';
  import { clock } from './clock.svelte';
  import { WAITING_SHOWN } from './jobs-merge';
  import { QUEUE_HREF } from './links';
  import { elapsedMs } from './time';

  interface Props {
    jobs: readonly Job[];
    error: unknown;
    onopen: (job: Job) => void;
    onretry: () => void;
  }
  let { jobs, error, onopen, onretry }: Props = $props();

  const shown = $derived(jobs.slice(0, WAITING_SHOWN));
  const more = $derived(jobs.length - shown.length);

  function wait(job: Job): string {
    const ms = elapsedMs(job.queued_at, clock.now);
    return ms === null ? NO_VALUE : formatDuration(ms);
  }
</script>

<section class="rail-section" aria-labelledby="command-waiting">
  <h2 id="command-waiting">
    Waiting <span class="count">{jobs.length}</span>
  </h2>
  {#if error}
    <ErrorState compact {error} title="The waiting jobs did not load." {onretry} />
  {:else if jobs.length === 0}
    <p class="empty">Nothing is waiting.</p>
  {:else}
    <ul>
      {#each shown as job (job.id)}
        {@const activity = queuedActivity(job, prefs.quirkyStatus)}
        <li>
          <button type="button" class="row" onclick={() => onopen(job)}>
            <span class="mark"><OffStatusIcon status={activity.status} seed={job.id} /></span>
            <span class="what">
              <span class="name">{readableJobText(job.job_name) || 'Unnamed job'}</span>
              <span class="where">{job.repo || NO_VALUE}</span>
            </span>
            <span class="side">
              <span class="wait">{wait(job)}</span>
              <span class="why">{activity.label}</span>
            </span>
          </button>
        </li>
      {/each}
    </ul>
    {#if more > 0}
      <a class="more" href={QUEUE_HREF}>and {more} more</a>
    {/if}
  {/if}
</section>

<style>
  .wait {
    color: var(--z-pending);
    font-variant-numeric: tabular-nums;
  }
</style>
