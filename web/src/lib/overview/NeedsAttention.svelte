<!--
  What needs a person, as rows: the worst three with the fix the controller
  suggests beside each, and a way to the thing each is about.

  The page used to say only the count here and keep the list in the drawer,
  because the whole list pushed the fleet below the fold. Three rows do not,
  and three rows answer the question an operator opens the page with before
  they have clicked anything; everything past three is still the drawer's, and
  the card says how many are there. With nothing wrong there is no card at
  all, only the quiet line the summary has always been, so a working fleet is
  never announced with a banner.
-->
<script lang="ts">
  import { TriangleAlert } from '@lucide/svelte';
  import type { Problem } from '$lib/api/types';
  import { formatNumber } from '$lib/format';
  import { severityStatus } from '$lib/status';
  import { notifications } from '$lib/state/notifications.svelte';
  import { problemTarget } from '$lib/problems/targets';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import RemedyText from '$lib/components/RemedyText.svelte';
  import ProblemsSummary from './ProblemsSummary.svelte';
  import { ATTENTION_SHOWN, cap } from './panels';

  interface Props {
    loading?: boolean;
    /** The first-run checklist is on screen: the summary stands down with it. */
    setupPending?: boolean;
    class?: string;
  }
  let { loading = false, setupPending = false, class: className = '' }: Props = $props();

  // The groups are already worst first, so flattening them is the order the
  // drawer shows and the one the cap cuts.
  const ordered = $derived(notifications.groups.flatMap((group) => group.items));
  const { shown, more } = $derived(cap(ordered, ATTENTION_SHOWN));
  const worst = $derived(notifications.worst ?? 'info');

  function key(problem: Problem): string {
    return problem.code + (problem.target_id ?? '') + problem.title;
  }

  function open(): void {
    notifications.open = true;
  }
</script>

{#if loading || ordered.length === 0}
  <ProblemsSummary {loading} {setupPending} class={className} />
{:else}
  <div class="attention {className}" data-severity={worst}>
    <Panel
      title="Needs attention"
      icon={TriangleAlert}
      description="Worst first, with the fix the controller suggests."
      flush
    >
      {#snippet actions()}
        <button type="button" class="more" onclick={open}>
          {more > 0 ? `and ${formatNumber(more)} more` : 'Open the drawer'}
        </button>
      {/snippet}
      <ul class="rows">
        {#each shown as problem (key(problem))}
          {@const target = problemTarget(problem)}
          <li class="row">
            <Badge status={severityStatus(problem.severity)} size="sm" />
            <div class="words">
              <p class="title">{problem.title}</p>
              {#if problem.fix}<p class="fix"><RemedyText text={problem.fix} /></p>{/if}
            </div>
            {#if target}
              <Button
                variant="secondary"
                size="sm"
                href={target.href}
                ariaLabel="{target.label}: {problem.title}">Open</Button
              >
            {/if}
          </li>
        {/each}
      </ul>
    </Panel>
  </div>
{/if}

<style>
  /* The worst severity on the card's edge, as the summary line drew it. */
  .attention :global(.panel) {
    border-left-width: var(--z-border-width-rail);
  }
  .attention[data-severity='error'] :global(.panel) {
    border-left-color: var(--z-danger);
  }
  .attention[data-severity='warning'] :global(.panel) {
    border-left-color: var(--z-pending);
  }
  .more {
    padding: 0 var(--z-space-1);
    border: 0;
    background: none;
    color: var(--z-accent);
    font: inherit;
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
    cursor: pointer;
  }
  .more:hover {
    text-decoration: underline;
  }
  .rows {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: start;
    gap: var(--z-space-3);
    /* Three rows and the band still leave the pools on the first screen of a
       1440x900 laptop, which is the rule the summary line was cut down for. */
    padding: var(--z-space-2) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  .row:last-child {
    border-bottom: 0;
  }
  .words {
    min-width: 0;
  }
  .title {
    margin: 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
    overflow-wrap: anywhere;
  }
  .fix {
    margin: var(--z-space-1) 0 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    color: var(--z-text-muted);
    overflow-wrap: anywhere;
  }
  @media (max-width: 480px) {
    /* A phone has no room for the button beside the words: it goes under them. */
    .row {
      grid-template-columns: auto minmax(0, 1fr);
    }
    .row > :global(:last-child:not(.words)) {
      grid-column: 2;
      justify-self: start;
    }
  }
</style>
