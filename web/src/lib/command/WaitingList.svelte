<!--
  The rail's Waiting section: the jobs this fleet has a hand in that wait for
  a runner, longest first, with how long and why.

  A row opens the job drawer in place rather than leaving the page, because a
  job has no page of its own and a wall display should stay where it is. The
  reason is the Queue page's own, from the same helper, so the two pages never
  give one job two explanations.
-->
<script lang="ts">
  import RailSection from './RailSection.svelte';
  import RailSkeleton from './RailSkeleton.svelte';
  import type { Job } from '$lib/api/types';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import Tooltip from '$lib/components/Tooltip.svelte';
  import OffStatusIcon from '$lib/components/OffStatusIcon.svelte';
  import { formatDuration, NO_VALUE } from '$lib/format';
  import { queuedActivity } from '$lib/jobs/activity-status';
  import { readableJobName } from '$lib/jobs/name';
  import { prefs } from '$lib/state/prefs.svelte';
  import { clock } from './clock.svelte';
  import { WAITING_SHOWN } from './jobs-merge';
  import { QUEUE_HREF } from './links';
  import { elapsedMs } from './time';
  import { waitingText, waitingTip } from './tips';

  interface Props {
    jobs: readonly Job[];
    /** How many wait, which past the fetch's cap is more than `jobs` holds. */
    count: number;
    /** True until the first read has answered: the rows' shapes, not an empty list. */
    loading: boolean;
    error: unknown;
    onopen: (job: Job) => void;
    onretry: () => void;
  }
  let { jobs, count, loading, error, onopen, onretry }: Props = $props();

  const shown = $derived(jobs.slice(0, WAITING_SHOWN));
  const more = $derived(Math.max(0, count - shown.length));

  function wait(job: Job): string {
    const ms = elapsedMs(job.queued_at, clock.now);
    return ms === null ? NO_VALUE : formatDuration(ms);
  }
</script>

<RailSection
  id="command-waiting"
  title="Waiting"
  count={loading || error ? undefined : count}
  busy={loading}
>
  {#if loading}
    <RailSkeleton />
  {:else if error}
    <ErrorState compact {error} title="The waiting jobs did not load." {onretry} />
  {:else if jobs.length === 0}
    <p class="empty">Nothing is waiting.</p>
  {:else}
    <ul>
      {#each shown as job (job.id)}
        {@const activity = queuedActivity(job, prefs.quirkyStatus)}
        <li>
          <Tooltip
            bare
            detail={() => waitingTip(job, activity, clock.now)}
            text={waitingText(job, activity)}
          >
            <button type="button" class="row" onclick={() => onopen(job)}>
              <span class="mark"><OffStatusIcon status={activity.status} seed={job.id} /></span>
              <span class="what">
                <span class="name">{readableJobName(job.job_name)}</span>
                <span class="where">{job.repo || NO_VALUE}</span>
              </span>
              <span class="side">
                <span class="wait">{wait(job)}</span>
                <span class="why">{activity.label}</span>
              </span>
            </button>
          </Tooltip>
        </li>
      {/each}
    </ul>
    {#if more > 0}
      <a class="more" href={QUEUE_HREF}>and {more} more</a>
    {/if}
  {/if}
</RailSection>

<style>
  .wait {
    color: var(--z-pending);
    font-variant-numeric: tabular-nums;
  }
</style>
