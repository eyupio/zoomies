<!--
  The offers waiting for a person: failed pull requests Eli can look at, each with
  what it is about (the pull request, where it is, when it failed, which jobs
  stopped at which step) so the decision can be made from here.

  Review fix opens the repair page for the offer, which reads and starts
  nothing and spends no attempt. Dismiss hides the offer for this commit. Every
  name here was written by whoever opened the pull request or edited a workflow,
  so each one is text.
-->
<script lang="ts">
  import { BookOpen, GitBranch } from '@lucide/svelte';
  import type { Schemas } from '$lib/api/types';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import StatusDot from '$lib/components/StatusDot.svelte';
  import { pluralise } from '$lib/format';
  import { jobStatus } from '$lib/status';
  import type { CaseView } from './case';
  import { targetHref } from './workspace';

  interface Props {
    offers: Schemas['EliOpportunity'][];
    /** What is known about each offer's failure, by the offer's id. */
    cases: Record<string, CaseView>;
    /** An offer is being dismissed, so the buttons wait. */
    busy: boolean;
    ondismiss: (id: string) => void;
    /** Render only the rows, for a section that supplies its own heading. */
    bare?: boolean;
  }

  let { offers, cases, busy, ondismiss, bare = false }: Props = $props();

  const failed = jobStatus('completed', 'failure');

  const where = (offer: Schemas['EliOpportunity']): string => `${offer.repo} #${offer.pull_number}`;
</script>

{#snippet rows()}
  {#each offers as offer (offer.id)}
    {@const view = cases[offer.id]}
    <article class="offer">
      <div class="what">
        <h3>
          {offer.title || view?.title || `Pull request #${offer.pull_number}`}
          <span class="num">#{offer.pull_number}</span>
        </h3>
        <div class="meta">
          <span><BookOpen size={12} aria-hidden="true" />{offer.repo}</span>
          {#if view?.branch}
            <span><GitBranch size={12} aria-hidden="true" />{view.branch}</span>
          {/if}
          <span>
            {#if view?.failedAt}
              <RelativeTime value={view.failedAt} prefix="Failed" />
            {:else}
              <RelativeTime value={offer.created_at} prefix="Offered" />
            {/if}
          </span>
        </div>
        {#if view?.jobs.length}
          <ul class="failing" aria-label="Failed jobs">
            {#each view.jobs as job (job.key)}
              <li class="chip">
                <StatusDot status={failed} size="sm" tip={false} />
                <span class="jname">{job.name || 'A job'}</span>
                {#if job.step}
                  <span class="muted">
                    {#if job.stepNumber}step {job.stepNumber}, {job.step}{:else}{job.step}{/if}
                  </span>
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
      </div>
      <div class="acts">
        <Button
          variant="primary"
          ariaLabel="Review fix for {where(offer)}"
          href={targetHref({
            offer: offer.id,
            repo: offer.repo,
            pull: offer.pull_number,
            run: offer.run_id,
          })}>Review fix</Button
        >
        <Button
          variant="ghost"
          disabled={busy}
          ariaLabel="Dismiss the offer for {where(offer)}"
          onclick={() => ondismiss(offer.id)}>Dismiss</Button
        >
      </div>
    </article>
  {/each}
{/snippet}

{#if bare}
  {@render rows()}
{:else}
  <Panel
    title="Needs you"
    description="Failed pull requests Eli can look at. Reviewing one never calls a model or uses a try."
    flush
  >
    {#snippet actions()}
      <Badge tone="pending" label={pluralise(offers.length, 'offer')} />
    {/snippet}
    {@render rows()}
  </Panel>
{/if}

<style>
  .offer {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: var(--z-space-4);
    align-items: center;
    padding: var(--z-space-4) var(--z-space-5);
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .offer:first-child {
    border-top: 0;
  }
  .what {
    min-width: 0;
  }
  h3 {
    margin: 0;
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
    line-height: var(--z-leading-base);
    overflow-wrap: anywhere;
  }
  .num {
    color: var(--z-text-muted);
    font-weight: var(--z-weight-normal);
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-1) var(--z-space-4);
    margin-top: var(--z-space-1);
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
  }
  .meta span {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .meta :global(svg) {
    flex: none;
    color: var(--z-text-subtle);
  }
  .failing {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
    margin: var(--z-space-3) 0 0;
    padding: 0;
    list-style: none;
  }
  /* A job and its step share a chip, and the step drops under the name when the column
     is narrow, because the chip wraps rather than squeezing the name. A name breaks
     inside a word only when the word cannot fit on a line of its own. */
  .chip {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0 var(--z-space-2);
    max-width: 100%;
    min-height: var(--z-space-6);
    padding: 0 var(--z-space-2);
    border: var(--z-border-width) solid var(--z-danger-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-danger-subtle);
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
    overflow-wrap: anywhere;
  }
  .muted {
    color: var(--z-text-muted);
    font-weight: var(--z-weight-normal);
  }
  .acts {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
    justify-content: flex-end;
  }
  @media (max-width: 640px) {
    .offer {
      grid-template-columns: minmax(0, 1fr);
      padding: var(--z-space-4);
    }
    .acts {
      justify-content: flex-start;
    }
  }
</style>
