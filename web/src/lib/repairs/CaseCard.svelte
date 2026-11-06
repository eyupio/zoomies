<!--
  What a repair is about, on one card with a red rail: when it failed, the title,
  where the pull request, the branch, the commit and the run are on GitHub, where
  it ran, and which jobs failed at which step.

  The title is the pull request's own once it is known and otherwise says what
  ran, and a dashed chip says the pull request has not been looked up rather than
  leaving a gap. Everything is a text node, and an address is only ever a link if
  it is a web one.
-->
<script lang="ts">
  import {
    ExternalLink,
    GitBranch,
    GitCommitHorizontal,
    GitPullRequest,
    Workflow,
    BookOpen,
  } from '@lucide/svelte';
  import type { Snippet } from 'svelte';
  import CopyButton from '$lib/components/CopyButton.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import StatusDot from '$lib/components/StatusDot.svelte';
  import { pluralise } from '$lib/format';
  import { jobStatus } from '$lib/status';
  import type { CaseView } from './case';
  import { safeHttpUrl } from './links';

  interface Props {
    view: CaseView;
    /** The line above the title. The attempt page says what Eli was asked instead. */
    failedLabel?: string;
    /** What the dashed chip says when no pull request is known. A kept case says it was not kept. */
    pullNote?: string;
    /** More rows for the facts, as `<dt>` and `<dd>` pairs. */
    children?: Snippet;
  }

  let {
    view,
    failedLabel = 'Failed check',
    pullNote = 'Pull request not looked up yet',
    children,
  }: Props = $props();

  const headingId = $props.id();
  const failed = jobStatus('completed', 'failure');

  const repoUrl = $derived(safeHttpUrl(view.repoUrl));
  const pullUrl = $derived(safeHttpUrl(view.pullUrl));
  const commitUrl = $derived(safeHttpUrl(view.commitUrl));
  const runUrl = $derived(safeHttpUrl(view.runUrl));
  const shortSha = $derived(view.sha ? view.sha.slice(0, 7) : '');
</script>

{#snippet pool(ran: NonNullable<CaseView['ran']>)}
  {#if ran.poolId}<a href="/pools/{encodeURIComponent(ran.poolId)}">{ran.pool}</a
    >{:else}{ran.pool}{/if}
{/snippet}

{#snippet runner(ran: NonNullable<CaseView['ran']>)}
  {#if ran.runnerId}<a class="mono" href="/runners/{encodeURIComponent(ran.runnerId)}"
      >{ran.runner}</a
    >{:else}<span class="mono">{ran.runner}</span>{/if}
{/snippet}

<section class="case" aria-labelledby={headingId}>
  <div class="over">
    <span>{failedLabel}</span>
    {#if view.failedAt}<span class="when"
        ><RelativeTime value={view.failedAt} prefix="Failed" /></span
      >{/if}
  </div>
  <h2 id={headingId}>{view.title}</h2>
  {#if view.hasPull && (view.author || view.base)}
    <p class="byline">
      {#if view.author && view.base}
        Opened by @{view.author}, merging into <code>{view.base}</code>
      {:else if view.author}
        Opened by @{view.author}
      {:else}
        Merging into <code>{view.base}</code>
      {/if}
    </p>
  {/if}

  <div class="chips">
    {#if view.pullNumber}
      {#if pullUrl}
        <a class="chip link" href={pullUrl} target="_blank" rel="noreferrer">
          <GitPullRequest size={14} aria-hidden="true" />{view.repo} #{view.pullNumber}
          <ExternalLink size={12} aria-hidden="true" /><span class="sr-only">
            (opens in a new tab)</span
          >
        </a>
      {:else}
        <span class="chip"
          ><GitPullRequest size={14} aria-hidden="true" />{view.repo} #{view.pullNumber}</span
        >
      {/if}
    {:else if repoUrl}
      <a class="chip link" href={repoUrl} target="_blank" rel="noreferrer">
        <BookOpen size={14} aria-hidden="true" />{view.repo}
        <ExternalLink size={12} aria-hidden="true" /><span class="sr-only">
          (opens in a new tab)</span
        >
      </a>
    {:else}
      <span class="chip"><BookOpen size={14} aria-hidden="true" />{view.repo}</span>
    {/if}
    {#if view.branch}
      <span class="chip"><GitBranch size={14} aria-hidden="true" />{view.branch}</span>
    {/if}
    {#if shortSha}
      <span class="chip mono">
        <GitCommitHorizontal size={14} aria-hidden="true" />
        {#if commitUrl}
          <a href={commitUrl} target="_blank" rel="noreferrer"
            >{shortSha}<span class="sr-only"> (opens in a new tab)</span></a
          >
        {:else}
          {shortSha}
        {/if}
        <CopyButton value={view.sha ?? ''} label="Copy the full commit SHA" />
      </span>
    {/if}
    {#if view.runLabel}
      {#if runUrl}
        <a class="chip link" href={runUrl} target="_blank" rel="noreferrer">
          <Workflow size={14} aria-hidden="true" />{view.runLabel}
          <ExternalLink size={12} aria-hidden="true" /><span class="sr-only">
            (opens in a new tab)</span
          >
        </a>
      {:else}
        <span class="chip"><Workflow size={14} aria-hidden="true" />{view.runLabel}</span>
      {/if}
    {/if}
    {#if !view.hasPull}
      <span class="chip pending"><GitPullRequest size={14} aria-hidden="true" />{pullNote}</span>
    {/if}
  </div>

  {#if view.ran || view.jobs.length || children}
    <dl class="facts">
      {#if view.ran && (view.ran.pool || view.ran.runner)}
        {@const ran = view.ran}
        <dt>Where it ran</dt>
        <dd>
          {#if ran.pool && ran.runner}
            Pool {@render pool(ran)}, runner {@render runner(ran)}
          {:else if ran.pool}
            Pool {@render pool(ran)}
          {:else}
            Runner {@render runner(ran)}
          {/if}
        </dd>
      {/if}
      {#if view.jobs.length}
        <dt>Failed jobs</dt>
        <dd>
          <ul class="failing">
            {#each view.jobs as job (job.key)}
              <li class="chip bad">
                <StatusDot status={failed} size="sm" tip={false} />
                <span class="jname">{job.name || 'A job'}</span>
                {#if job.stepNumber}<span class="muted">step {job.stepNumber}</span>{/if}
              </li>
            {/each}
          </ul>
          <span class="sr-only">{pluralise(view.jobs.length, 'failed job')}</span>
        </dd>
      {/if}
      {@render children?.()}
    </dl>
  {/if}
</section>

<style>
  .case {
    min-width: 0;
    padding: var(--z-space-5);
    border: var(--z-border-width) solid var(--z-border);
    border-left: var(--z-border-width-rail) solid var(--z-danger);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  .over {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    gap: var(--z-space-1) var(--z-space-3);
    color: var(--z-text-subtle);
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-medium);
  }
  .when {
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-normal);
    letter-spacing: 0;
    text-transform: none;
  }
  h2 {
    margin: var(--z-space-2) 0 0;
    font-size: var(--z-text-lg);
    font-weight: var(--z-weight-semibold);
    line-height: var(--z-leading-lg);
    letter-spacing: var(--z-tracking-tight);
    overflow-wrap: anywhere;
  }
  .byline {
    margin: var(--z-space-1) 0 0;
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
    overflow-wrap: anywhere;
  }
  .byline code,
  .mono {
    font-family: var(--z-font-mono);
    font-size: var(--z-text-xs);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
    margin-top: var(--z-space-4);
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    max-width: 100%;
    min-height: var(--z-space-6);
    padding: 0 var(--z-space-2);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-surface-sunken);
    color: var(--z-text);
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
    /* A branch or a repository name can be long and has no spaces to break at. */
    overflow-wrap: anywhere;
  }
  .chip :global(svg) {
    flex: none;
    color: var(--z-text-subtle);
  }
  .chip a {
    color: inherit;
  }
  .chip.link {
    color: var(--z-accent);
  }
  .chip.pending {
    border-style: dashed;
    color: var(--z-text-muted);
    font-weight: var(--z-weight-normal);
  }
  .facts {
    display: grid;
    grid-template-columns: minmax(0, 10rem) minmax(0, 1fr);
    gap: var(--z-space-3) var(--z-space-4);
    margin: var(--z-space-4) 0 0;
    padding-top: var(--z-space-4);
    border-top: var(--z-border-width) solid var(--z-border);
    font-size: var(--z-text-sm);
  }
  .facts :global(dt) {
    color: var(--z-text-muted);
  }
  .facts :global(dd) {
    min-width: 0;
    margin: 0;
    overflow-wrap: anywhere;
  }
  .failing {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .chip.bad {
    border-color: var(--z-danger-border);
    background: var(--z-danger-subtle);
  }
  .jname {
    min-width: 0;
  }
  .muted {
    color: var(--z-text-muted);
    font-weight: var(--z-weight-normal);
  }
  @media (max-width: 640px) {
    .case {
      padding: var(--z-space-4);
    }
    .facts {
      grid-template-columns: minmax(0, 1fr);
      gap: var(--z-space-1);
    }
    .facts :global(dd) {
      margin-bottom: var(--z-space-2);
    }
  }
</style>
