<!--
  The Kennel Club band's picture: one square per repository, worst first. A
  repository with an error open is punched through, one with warnings only is
  solid, a clean one faint, and one not yet looked at dashed. The named ones
  (the overview lists up to ten with something open) carry their findings in
  their title; the rest are their state alone, since that is all the overview
  says of them.

  From the overview the page already holds, so it costs no request. The whole
  picture is a way into the repositories page, worst first.
-->
<script lang="ts">
  import type { KennelOverview } from '$lib/api/types';
  import { href } from '$lib/router';
  import { repoSquares } from './repo-squares';

  interface Props {
    overview: KennelOverview | null;
    /** Where the squares lead: the repositories page, worst first. */
    to: string;
  }
  let { overview, to }: Props = $props();

  const data = $derived(repoSquares(overview));
</script>

{#if data.squares.length > 0}
  <div class="pic" data-testid="band-repositories">
    <div class="head">
      <span class="title">Repositories, worst first</span>
      <span class="summary">{data.summary}</span>
    </div>
    <a
      class="squares"
      href={href(to)}
      aria-label={`${data.summary}. Open the repositories, worst first.`}
    >
      {#each data.squares as square, i (i)}
        <i class="sq" data-kind={square.kind} title={square.note}></i>
      {/each}
      {#if data.more > 0}<span class="extra">+{data.more}</span>{/if}
    </a>
    <div class="legend" aria-hidden="true">
      <span><i class="sq" data-kind="error"></i>Error open</span>
      <span><i class="sq" data-kind="warning"></i>Warnings</span>
      <span><i class="sq" data-kind="clean"></i>Clean</span>
      <span><i class="sq" data-kind="pending"></i>Not yet evaluated</span>
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
  .head {
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
  .summary {
    font-size: var(--z-text-xs);
    color: var(--z-panel-text-subtle);
    font-variant-numeric: tabular-nums;
  }
  .squares {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-1);
    padding: var(--z-space-1);
    margin: calc(-1 * var(--z-space-1));
    border-radius: var(--z-radius-md);
    text-decoration: none;
  }
  .squares:hover {
    background: var(--z-panel-raised);
  }
  .squares:focus-visible {
    outline: var(--z-focus-width) solid var(--z-brand-runner-blue);
    outline-offset: var(--z-focus-offset);
  }
  .extra {
    font-size: var(--z-text-2xs);
    color: var(--z-panel-text-subtle);
  }
  .sq {
    display: block;
    box-sizing: border-box;
    width: var(--z-space-5);
    height: var(--z-space-5);
    border-radius: var(--z-nudge-2);
  }
  .sq[data-kind='error'] {
    background: radial-gradient(
      circle,
      var(--z-panel-bg) 0 var(--z-nudge-3),
      var(--z-brand-runner-blue) calc(var(--z-nudge-3) + var(--z-nudge-1))
    );
  }
  .sq[data-kind='warning'] {
    background: var(--z-brand-runner-blue);
  }
  .sq[data-kind='clean'] {
    background: color-mix(in srgb, var(--z-brand-runner-blue) 30%, var(--z-panel-raised));
  }
  .sq[data-kind='pending'] {
    background: transparent;
    border: var(--z-border-width) dashed var(--z-panel-text-subtle);
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
