<!--
  What the fleet is doing about a job that is still waiting.

  The counts are this pool's, live from the fleet cache, and they stay here:
  they are facts about the pool and the panel is where an operator reads them.

  The sentence is not. It used to be worked out here from those same counts,
  which could see how many runners a pool had and not the one thing that
  decides whether it can have another -- whether any host will take it. So a
  pool whose selector matched nothing read as a fleet that was merely busy, and
  the advice was to wait for something that would never happen. It also
  disagreed with the CLI, which reasoned its way to a different paragraph for
  the same question.

  It now comes from GET /jobs/{id}/explanation, which is computed from the last
  scheduler plan and the fleet around the job, and is the same answer
  `zoomies jobs get` prints.
-->
<script lang="ts">
  import type { Job, JobExplanation } from '$lib/api/types';
  import { getJobExplanation } from '$lib/api/client';
  import { fleet } from '$lib/state/fleet.svelte';
  import Duration from '$lib/components/Duration.svelte';

  interface Props {
    job: Job;
    class?: string;
  }

  let { job, class: className = '' }: Props = $props();

  const pool = $derived(fleet.pool(job.pool_id));
  const counts = $derived(pool?.counts);
  const warming = $derived((counts?.provisioning ?? 0) + (counts?.registering ?? 0));
  const idle = $derived(counts?.idle ?? 0);
  const busy = $derived(counts?.busy ?? 0);

  let why = $state<JobExplanation | null>(null);

  // Refetched whenever the drawer is given a different job and whenever the
  // event stream replaces this one: the answer is about the fleet around the
  // job, so it goes stale for reasons the job row does not show.
  $effect(() => {
    const id = job.id;
    void job.state;
    if (!id) return;
    const controller = new AbortController();
    void (async () => {
      try {
        why = await getJobExplanation(id, controller.signal);
      } catch {
        // A diagnostic that is down is not the drawer's news to break: the
        // counts below are still true, and they are most of the answer.
        why = null;
      }
    })();
    return () => controller.abort();
  });
</script>

<div class="waiting {className}" role="status" aria-label="What is happening to this job">
  <p class="line">
    <span class="lead" class:blocked={why?.blocked}>{why?.blocked ? 'Blocked' : 'Waiting'}</span>
    <span class="tabular"><Duration from={job.queued_at} live /></span>
    {#if pool}
      <span>in <a href="/pools/{pool.id}">{pool.name ?? pool.id}</a></span>
    {/if}
  </p>
  <p class="summary">{why?.summary ?? 'Working out what the fleet is doing about it…'}</p>
  {#if why?.detail}<p class="summary detail">{why.detail}</p>{/if}
  {#if counts}
    <dl class="counts">
      <div>
        <dt>Starting</dt>
        <dd class="tabular">{warming}</dd>
      </div>
      <div>
        <dt>Idle</dt>
        <dd class="tabular">{idle}</dd>
      </div>
      <div>
        <dt>Busy</dt>
        <dd class="tabular">{busy}</dd>
      </div>
      <div>
        <dt>Ceiling</dt>
        <dd class="tabular">{pool?.max_runners ?? '--'}</dd>
      </div>
    </dl>
  {/if}
  {#if why?.fix}
    <p class="blocked"><span class="fixLabel">Fix</span> {why.fix}</p>
  {/if}
</div>

<style>
  .waiting {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
    padding: var(--z-space-3);
    border: var(--z-border-width) solid var(--z-pending-border);
    border-radius: var(--z-radius-md);
    background: var(--z-pending-subtle);
  }
  .line {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--z-space-2);
    margin: 0;
    font-size: var(--z-text-base);
    color: var(--z-text);
  }
  .lead {
    font-weight: var(--z-weight-semibold);
  }
  .line a {
    color: var(--z-accent);
  }
  .summary,
  .blocked {
    margin: 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    color: var(--z-text);
    max-width: 70ch;
  }
  .blocked {
    color: var(--z-danger);
  }
  .detail {
    color: var(--z-text-muted);
  }
  .lead.blocked {
    color: var(--z-danger);
  }
  .fixLabel {
    font-weight: var(--z-weight-medium);
  }
  .counts {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2) var(--z-space-5);
    margin: 0;
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
    font-size: var(--z-text-sm);
    color: var(--z-text);
  }
</style>
