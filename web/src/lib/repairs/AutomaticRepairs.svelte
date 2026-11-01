<!--
  The repositories that repair themselves: who pays, whether they can, what the
  automation did last, and the way to stop
  it. It is an administrator's panel and appears only when something is automatic,
  so a fleet that never turned the feature on is never told about it.

  A repository that cannot run is first, with what to do about it, because the
  failure it describes is silent: Eli simply does not start. Every
  name here (repository, provider, model) was written by a person, so each one is
  text.
-->
<script lang="ts">
  import type { Schemas } from '$lib/api/types';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import { onClockTick } from '$lib/format';
  import { automaticStatus } from '$lib/status';
  import { lastWords, paidByWords, runningWords } from './automatic';

  interface Props {
    repositories: Schemas['EliAutomaticRepository'][];
    /** The last read failed, so what is shown is what was last read, or nothing yet. */
    error?: boolean;
    onstop: (repos: string[]) => void;
    /** Render only the rows, for a section that supplies its own heading. */
    bare?: boolean;
  }

  let { repositories, error = false, onstop, bare = false }: Props = $props();

  let now = $state(Date.now());
  $effect(() => onClockTick((t) => (now = t)));

  const healthOf = (repo: Schemas['EliAutomaticRepository']) =>
    repo.problem ? 'problem' : 'ready';
</script>

{#snippet rows()}
  {#if bare}
    <p class="intro">
      Eli repairs failed pull request checks on these repositories without asking. It pushes one
      commit and never merges.
    </p>
  {/if}
  {#if bare && repositories.length > 1}
    <div class="bulk">
      <Button variant="secondary" size="sm" onclick={() => onstop(repositories.map((r) => r.repo))}
        >Stop all</Button
      >
    </div>
  {/if}
  {#if error}
    <p class="stale" role="status">
      Could not read automatic repairs.{repositories.length > 0
        ? ' Showing what was last read.'
        : ' Try Refresh.'}
    </p>
  {/if}

  {#each repositories as repo (repo.repo)}
    {@const running = runningWords(repo.running)}
    <article class="row">
      <div class="who">
        <div class="name">
          <h3>{repo.repo}</h3>
          <Badge status={automaticStatus(healthOf(repo))} size="sm" />
        </div>
        {#if !(repo.problem && !repo.provider_name)}
          <p class="paid">
            {paidByWords(repo)}{repo.allow_workflows ? '. Workflow files may be edited.' : '.'}
          </p>
        {/if}
        {#if repo.problem}
          <p class="problem">
            {repo.problem}
            <a href="/repairs/setup?repo={encodeURIComponent(repo.repo)}">Fix in setup</a>
          </p>
        {/if}
      </div>

      <div class="latest">
        <span class="label">Latest automatic repair</span>
        {#if repo.last}
          <a href="/repairs/{encodeURIComponent(repo.last.id)}">{lastWords(repo, now)}</a>
        {:else}
          <span>{lastWords(repo, now)}</span>
        {/if}
        {#if running}<span class="running">{running}</span>{/if}
      </div>

      <div class="acts">
        <Button
          variant="secondary"
          ariaLabel="Stop automatic repairs on {repo.repo}"
          onclick={() => onstop([repo.repo])}>Stop automatic repairs</Button
        >
      </div>
    </article>
  {/each}
{/snippet}

{#if bare}
  {@render rows()}
{:else}
  <Panel
    title="Automatic repairs"
    description="Eli repairs failed pull request checks on these repositories without asking. It pushes one commit and never merges."
    flush
  >
    {#snippet actions()}
      {#if repositories.length > 1}
        <Button
          variant="secondary"
          size="sm"
          onclick={() => onstop(repositories.map((r) => r.repo))}>Stop all</Button
        >
      {/if}
    {/snippet}
    {@render rows()}
  </Panel>
{/if}

<style>
  .intro {
    margin: 0;
    padding: var(--z-space-3) var(--z-space-4);
    border-bottom: var(--z-border-width) solid var(--z-border);
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
  }
  .bulk {
    display: flex;
    justify-content: flex-end;
    padding: var(--z-space-2) var(--z-space-4);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  .stale {
    margin: 0;
    padding: var(--z-space-3) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
    color: var(--z-pending);
    font-size: var(--z-text-sm);
  }
  .row {
    display: grid;
    grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr) auto;
    gap: var(--z-space-4);
    align-items: start;
    padding: var(--z-space-4) var(--z-space-5);
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .row:first-child {
    border-top: 0;
  }
  .who,
  .latest {
    min-width: 0;
  }
  .name {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-1) var(--z-space-3);
  }
  h3 {
    margin: 0;
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
    line-height: var(--z-leading-base);
    overflow-wrap: anywhere;
  }
  p {
    margin: var(--z-space-1) 0 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    overflow-wrap: anywhere;
  }
  .paid {
    color: var(--z-text-muted);
  }
  .problem {
    color: var(--z-danger);
  }
  .problem a {
    color: var(--z-accent);
    white-space: nowrap;
  }
  .latest {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-1);
    font-size: var(--z-text-sm);
    overflow-wrap: anywhere;
  }
  .label {
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
  }
  .latest a {
    color: var(--z-accent);
  }
  .running {
    color: var(--z-busy);
    font-weight: var(--z-weight-medium);
  }
  .acts {
    display: flex;
    justify-content: flex-end;
  }
  @media (max-width: 900px) {
    .row {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    }
    .acts {
      grid-column: 1 / -1;
      justify-content: flex-start;
    }
  }
  @media (max-width: 640px) {
    .row {
      grid-template-columns: minmax(0, 1fr);
      padding: var(--z-space-4);
    }
  }
</style>
