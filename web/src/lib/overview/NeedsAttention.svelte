<!--
  What needs a person, as rows: the worst three with the fix the controller
  suggests beside each, a way to the thing each is about, and a way to put each
  one down.

  The card is shut by default and its header says what it holds (how many of
  each severity, and the worst problem by name), so the page says what is
  wrong before anything is opened and the pools stay on the first screen
  whatever the fleet is carrying; a reader who wants the fix opens it, and the
  choice is remembered like the other sections'. Everything past three is
  still the drawer's, and the card says how many are there. With nothing wrong
  there is no card at all, only the quiet line the summary has always been, so
  a working fleet is never announced with a banner.

  Each row carries the drawer's own dismiss menu (snooze for a while, snooze
  every problem of this kind, or dismiss until resolved) so a decision made
  here means exactly what it means there, and is listed there as dismissed.
-->
<script lang="ts">
  import { TriangleAlert, X } from '@lucide/svelte';
  import type { Problem } from '$lib/api/types';
  import { formatNumber } from '$lib/format';
  import { severityStatus } from '$lib/status';
  import { notifications, problemKey } from '$lib/state/notifications.svelte';
  import { dismissMenuItems } from '$lib/problems/dismiss-menu';
  import { problemTarget } from '$lib/problems/targets';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import DropdownMenu from '$lib/components/DropdownMenu.svelte';
  import RemedyText from '$lib/components/RemedyText.svelte';
  import BulkDismiss from '$lib/problems/BulkDismiss.svelte';
  import OverviewDisclosure from './OverviewDisclosure.svelte';
  import ProblemsSummary from './ProblemsSummary.svelte';
  import { attentionSummary } from './attention';
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
  const summary = $derived(attentionSummary(notifications.groups));

  const handlers = {
    ondismiss: (p: Problem) => notifications.dismiss(p),
    onsnooze: (p: Problem, ms: number) => notifications.snooze(p, ms),
    onsnoozetype: (p: Problem, ms: number) => notifications.snoozeType(p, ms),
  };

  function open(): void {
    notifications.open = true;
  }
</script>

{#if loading || ordered.length === 0}
  <ProblemsSummary {loading} {setupPending} class={className} />
{:else}
  <!-- A phone draws no band and keeps the pools on its first screen, so it
       keeps the one-line summary; the card is for the screens with room. -->
  <div class="phone"><ProblemsSummary {loading} {setupPending} class={className} /></div>
  <div class="attention wide {className}" data-severity={worst}>
    <OverviewDisclosure id="attention" title="Needs attention" icon={TriangleAlert} {summary}>
      {#snippet actions()}
        <!-- Everything on the card can be put away from its header, shut or
             open, with the drawer's own menu. A click on a control in the
             header is not a click on the header, so none of them toggles it. -->
        <span
          class="bulk"
          role="presentation"
          onclick={(event) => {
            event.preventDefault();
            event.stopPropagation();
          }}
        >
          <BulkDismiss />
        </span>
        <!-- The rest are the drawer's; the line says how many, or that there
             are none, and is the way in either way, shut or open. -->
        <button
          type="button"
          class="more"
          onclick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            open();
          }}
        >
          {more > 0 ? `and ${formatNumber(more)} more` : 'Open the drawer'}
        </button>
      {/snippet}
      <section class="card" aria-label="Needs attention">
        <ul class="rows">
          {#each shown as problem (problemKey(problem))}
            {@const target = problemTarget(problem)}
            <li class="row">
              <Badge status={severityStatus(problem.severity)} size="sm" />
              <div class="words">
                <p class="title">{problem.title}</p>
                {#if problem.fix}<p class="fix"><RemedyText text={problem.fix} /></p>{/if}
              </div>
              <span class="actions">
                {#if target}
                  <Button
                    variant="secondary"
                    size="sm"
                    href={target.href}
                    ariaLabel="{target.label}: {problem.title}">Open</Button
                  >
                {/if}
                <DropdownMenu
                  label="Dismiss or snooze: {problem.title}"
                  triggerIcon={X}
                  items={dismissMenuItems(problem, handlers)}
                  size="sm"
                />
              </span>
            </li>
          {/each}
        </ul>
      </section>
    </OverviewDisclosure>
  </div>
{/if}

<style>
  /* The worst severity on the card's edge, as the summary line drew it. */
  .attention :global(details) {
    border-left-width: var(--z-border-width-rail);
  }
  .attention[data-severity='error'] :global(details) {
    border-left-color: var(--z-danger);
  }
  .attention[data-severity='warning'] :global(details) {
    border-left-color: var(--z-pending);
  }
  /* The rows run edge to edge under the header, as they did in the panel. */
  .attention :global(.overview-disclosure > .content) {
    padding: 0;
  }
  .actions {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
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
  .phone {
    display: none;
  }
  @media (max-width: 768px) {
    .phone {
      display: block;
    }
    .wide {
      display: none;
    }
  }
  @media (max-width: 480px) {
    /* A phone has no room for the button beside the words: it goes under them. */
    .row {
      grid-template-columns: auto minmax(0, 1fr);
    }
    .row > .actions {
      grid-column: 2;
      justify-self: start;
    }
  }
</style>
