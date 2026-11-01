<!--
  Where the policy behind an automatic repair stands now, beside the repair, for
  an administrator: still automatic (who pays, and the way to stop it), or no longer. The repair was started by the policy as it
  was, and the person reading it is usually here to decide whether to let it
  happen again.

  It reads once, and says when it could not, so the rest of the page never waits
  on it.
-->
<script lang="ts">
  import type { Schemas } from '$lib/api/types';
  import Button from '$lib/components/Button.svelte';
  import { paidByWords } from './automatic';

  interface Props {
    repo: string;
    /** The repository's row of the overview, or nothing when it is not automatic now. */
    repository: Schemas['EliAutomaticRepository'] | undefined;
    /** The overview has been read, so a missing row means the repository is not automatic. */
    loaded: boolean;
    failed: boolean;
    /** The repair beside this card has not pushed yet, so stopping also stops it. */
    unpushed: boolean;
    onstop: () => void;
    onretry: () => void;
  }

  let { repo, repository, loaded, failed, unpushed, onstop, onretry }: Props = $props();

  const headingId = $props.id();
  let heading = $state<HTMLHeadingElement | null>(null);
  const setup = $derived(`/repairs/setup?repo=${encodeURIComponent(repo)}`);

  /** The page moves focus here when the button that was pressed has left the card. */
  export function focus(): void {
    heading?.focus();
  }
</script>

<section class="card" aria-labelledby={headingId}>
  <h2 id={headingId} tabindex="-1" bind:this={heading}>Automatic repairs on {repo}</h2>
  {#if failed}
    <p class="muted" role="status">
      Could not read the repository’s automatic repair status.
      <Button variant="ghost" size="sm" onclick={onretry}>Try again</Button>
    </p>
  {:else if !loaded}
    <p class="muted">Checking the repository’s policy.</p>
  {:else if repository}
    <p>
      <strong>Automatic repairs are on.</strong>
      {paidByWords(repository)}.
    </p>
    {#if repository.problem}<p class="problem">{repository.problem}</p>{/if}
    {#if unpushed}
      <p class="muted">Stopping now also stops this repair if it has not pushed yet.</p>
    {/if}
    <div class="acts">
      <Button variant="secondary" onclick={onstop}>Stop automatic repairs</Button>
      <a href={setup}>Policy in setup</a>
    </div>
  {:else}
    <p>This repository is no longer set to repair automatically. It was when this repair began.</p>
    <div class="acts"><a href={setup}>Policy in setup</a></div>
  {/if}
</section>

<style>
  .card {
    padding: var(--z-space-5);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  h2 {
    margin: 0;
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
    line-height: var(--z-leading-base);
    overflow-wrap: anywhere;
  }
  /* The provider, the model and the sentence built from them were named by people. */
  p {
    margin: var(--z-space-3) 0 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    overflow-wrap: anywhere;
  }
  .muted {
    color: var(--z-text-muted);
  }
  .problem {
    color: var(--z-danger);
  }
  strong {
    font-weight: var(--z-weight-semibold);
  }
  .acts {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-3) var(--z-space-4);
    margin-top: var(--z-space-4);
    font-size: var(--z-text-sm);
  }
  .acts a {
    color: var(--z-accent);
  }
</style>
