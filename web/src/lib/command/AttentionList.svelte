<!--
  The rail's Needs attention section: the open problems, most severe first,
  each with its title, a line of detail and the page that fixes it.

  The route leaves this out altogether when nothing is open. An empty "Needs
  attention" heading on a wall reads as something wrong with nothing said
  about it, which is worse than no heading at all.
-->
<script lang="ts">
  import type { Problem } from '$lib/api/types';
  import Tooltip from '$lib/components/Tooltip.svelte';
  import { fleet } from '$lib/state/fleet.svelte';
  import { problemKey } from '$lib/problems/identity';
  import { notifications } from '$lib/state/notifications.svelte';
  import { severityStatus } from '$lib/status';
  import { PROBLEMS_SHOWN } from './jobs-merge';
  import RailSection from './RailSection.svelte';
  import { problemHref } from './links';
  import { problemTip } from './tips';

  interface Props {
    problems: readonly Problem[];
  }
  let { problems }: Props = $props();

  const shown = $derived(problems.slice(0, PROBLEMS_SHOWN));
  const more = $derived(problems.length - shown.length);

  /** The name of what a problem is about, where the page holds it. */
  function targetName(problem: Problem): string | undefined {
    if (problem.target_kind === 'host') return fleet.host(problem.target_id)?.name;
    if (problem.target_kind === 'pool') return fleet.pool(problem.target_id)?.name;
    return undefined;
  }

  function openDrawer(): void {
    notifications.open = true;
  }
</script>

<RailSection id="command-attention" title="Needs attention" count={problems.length}>
  <ul>
    {#each shown as problem (problemKey(problem))}
      {@const href = problemHref(problem)}
      <li>
        {#snippet body()}
          {@const status = severityStatus(problem.severity)}
          <span class="mark" style:color={status.colour} role="img" aria-label={status.label}>
            <status.icon size={16} aria-hidden="true" />
          </span>
          <span class="what">
            <span class="name">{problem.title}</span>
            {#if problem.detail}<span class="detail">{problem.detail}</span>{/if}
          </span>
        {/snippet}
        <Tooltip bare detail={problemTip(problem, targetName(problem))}>
          {#if href}
            <a class="row" {href}>{@render body()}</a>
          {:else}
            <button type="button" class="row" onclick={openDrawer}>{@render body()}</button>
          {/if}
        </Tooltip>
      </li>
    {/each}
  </ul>
  {#if more > 0}
    <button type="button" class="more" onclick={openDrawer}>and {more} more</button>
  {/if}
</RailSection>

<style>
  .detail {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
</style>
