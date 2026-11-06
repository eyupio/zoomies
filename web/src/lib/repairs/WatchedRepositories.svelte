<!--
  The repositories Eli is watching, one row each, with the pull requests it is
  following underneath.

  A row answers the question a person actually has after they have chosen
  repositories: what did I set this to do, is it running, how many pull requests
  is it on, when did it last see something, and is anything waiting for me. The
  row carries the pause for the repository; the pull request rows carry the pause
  and stop for one, because "stop admitting new work here" and "stop this pull
  request" are different promises and are made in different places.

  Nothing here says all clear unless both reads answered. `watchingState` decides
  which of the four shapes the section has, and a failed read keeps its own
  sentence rather than becoming an empty list.
-->
<script lang="ts">
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import { pluralise, relativeTime } from '$lib/format';
  import { watchStatus } from '$lib/status';
  import PullRequestWatch from './PullRequestWatch.svelte';
  import {
    behaviourLabel,
    capabilityWords,
    decisionFor,
    pauseRepositoryCopy,
    repositoryNames,
    summariseRepository,
    watchingOrder,
    watchesFor,
    type Behaviour,
    type RepositoryWatch,
    type Watch,
    type WatchingState,
  } from './watches';

  interface Props {
    decisions: RepositoryWatch[];
    watches: Watch[];
    state: WatchingState;
    /** Ids of controls whose request is in flight, so their buttons stay put and say so. */
    busy?: string[];
    now?: number;
    onpausewatch: (watch: Watch) => void;
    onresumewatch: (watch: Watch) => void;
    onstopwatch: (watch: Watch) => void;
    onpauserepo: (decision: RepositoryWatch) => void;
    onresumerepo: (decision: RepositoryWatch) => void;
    onstopall: () => void;
    /** A sentence about the last read, when it failed. */
    failure?: string;
  }

  let {
    decisions,
    watches,
    state,
    busy = [],
    now = Date.now(),
    onpausewatch,
    onresumewatch,
    onstopwatch,
    onpauserepo,
    onresumerepo,
    onstopall,
    failure = '',
  }: Props = $props();

  const groups = $derived(
    repositoryNames(decisions, watches).map((repo) => {
      const decision = decisionFor(decisions, repo) ?? null;
      return {
        repo,
        decision,
        summary: summariseRepository(decision, watchesFor(watches, repo), now),
        pulls: watchingOrder(watchesFor(watches, repo), now),
      };
    }),
  );
  /** What needs a person comes first, as it does everywhere else on this page. */
  const ordered = $derived(
    [...groups].sort(
      (a, b) =>
        Number(b.summary.attention > 0) - Number(a.summary.attention > 0) ||
        a.repo.localeCompare(b.repo),
    ),
  );
  const fleetWatching = $derived(watches.filter((watch) => watch.enabled).length);
  const isBusy = (id: string): boolean => busy.includes(id);
  const behaviourOfWatch = (watch: Watch): Behaviour =>
    watch.checks && watch.conflicts
      ? 'babysit'
      : watch.checks
        ? 'checks'
        : watch.conflicts
          ? 'conflicts'
          : 'babysit';
</script>

{#if state === 'failed'}
  <p class="note problem" role="status">
    {failure || 'Could not read what Eli is watching.'} Nothing here says whether a pull request is being
    followed, because this page could not find out.
  </p>
{:else if state === 'loading'}
  <p class="note">Reading what Eli is watching.</p>
{:else if state === 'quiet'}
  <p class="note">
    Nothing is being watched. Choose repositories in <a href="/repairs/setup">repair settings</a> to have
    Eli follow their pull requests.
  </p>
{:else}
  {#if fleetWatching > 1}
    <div class="bulk">
      <Button variant="secondary" size="sm" disabled={busy.includes('all')} onclick={onstopall}
        >Stop all</Button
      >
    </div>
  {/if}
  {#if failure}
    <p class="note problem" role="status">{failure} Showing what was last read.</p>
  {/if}

  {#each ordered as group (group.repo)}
    {@const repoState =
      group.summary.state !== 'enabled'
        ? 'paused'
        : group.summary.attention > 0
          ? 'attention'
          : 'watching'}
    <article class="repo" aria-label="Watched repository {group.repo}">
      <header class="head">
        <h3>{group.repo}</h3>
        <Badge status={watchStatus(repoState)} size="sm" />
      </header>
      <p class="behaviour">
        {group.summary.behaviour
          ? behaviourLabel(group.summary.behaviour)
          : 'Behaviour not recorded'}
        {#if group.decision}
          · {capabilityWords(group.decision.checks, group.decision.conflicts)}
        {/if}
      </p>
      <dl class="facts">
        <dt>Watched pull requests</dt>
        <dd>
          {pluralise(
            group.summary.watching,
            'pull request',
          )}{#if group.summary.paused + group.summary.stopped > 0}, and {pluralise(
              group.summary.paused + group.summary.stopped,
              'paused or stopped',
            )}{/if}
        </dd>
        <dt>Last successful observation</dt>
        <dd>
          {group.summary.observedAt ? relativeTime(group.summary.observedAt, now) : 'None yet'}
        </dd>
        <dt>Now</dt>
        <dd>{group.summary.activity}</dd>
        <dt>Needs a person</dt>
        <dd>
          {group.summary.attention > 0
            ? pluralise(group.summary.attention, 'pull request')
            : group.summary.observationFailed
              ? 'An observation failed, so nothing is known'
              : 'Nothing'}
        </dd>
      </dl>

      {#if group.decision}
        <div class="acts">
          {#if group.decision.enabled}
            <Button
              variant="ghost"
              size="sm"
              disabled={isBusy(group.decision.id)}
              onclick={() => onpauserepo(group.decision!)}>Pause new pull requests</Button
            >
          {:else}
            <Button
              variant="ghost"
              size="sm"
              disabled={isBusy(group.decision.id)}
              onclick={() => onresumerepo(group.decision!)}>Resume new pull requests</Button
            >
          {/if}
          <span class="hint">{pauseRepositoryCopy(group.decision, group.pulls).inFlight}</span>
        </div>
      {:else}
        <p class="hint">
          Only single pull requests were admitted here. Choose this repository in
          <a href="/repairs/setup?repo={encodeURIComponent(group.repo)}">repair settings</a> to follow
          its new ones too.
        </p>
      {/if}

      {#if group.pulls.length}
        <div class="pulls">
          {#each group.pulls as watch (watch.id)}
            <PullRequestWatch
              {watch}
              {now}
              behaviour={behaviourLabel(behaviourOfWatch(watch))}
              busy={isBusy(watch.id)}
              onpause={onpausewatch}
              onresume={onresumewatch}
              onstop={onstopwatch}
            />
          {/each}
        </div>
      {:else}
        <p class="note">
          No pull request is being followed here. Resuming the repository picks up the ones it has
          open now.
        </p>
      {/if}
    </article>
  {/each}
{/if}

<style>
  .note {
    margin: 0;
    padding: var(--z-space-3) var(--z-space-4);
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
  }
  .note.problem {
    color: var(--z-danger);
  }
  .bulk {
    display: flex;
    justify-content: flex-end;
    padding: var(--z-space-2) var(--z-space-4);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  .repo {
    display: grid;
    gap: var(--z-space-3);
    padding: var(--z-space-4);
    border-top: var(--z-border-width) solid var(--z-border);
    min-width: 0;
  }
  .repo:first-of-type {
    border-top: 0;
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-2);
    min-width: 0;
  }
  h3 {
    margin: 0;
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
    overflow-wrap: anywhere;
  }
  .behaviour {
    margin: 0;
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
  }
  .facts {
    /* Each term sits above its value, and the four pairs run across rather than
       down, so a repository's row is one line of facts on a desktop. Direct
       dt/dd children keep the definition list valid. */
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    grid-template-rows: auto auto;
    grid-auto-flow: column;
    gap: var(--z-space-1) var(--z-space-4);
    margin: 0;
  }
  .facts dt {
    color: var(--z-text-subtle);
    font-size: var(--z-text-xs);
  }
  .facts dd {
    margin: 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    overflow-wrap: anywhere;
  }
  .acts {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-2) var(--z-space-3);
  }
  .hint {
    margin: 0;
    color: var(--z-text-subtle);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-sm);
    overflow-wrap: anywhere;
  }
  .pulls {
    display: grid;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    overflow: hidden;
  }
  @media (max-width: 900px) {
    .facts {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      grid-template-rows: repeat(4, auto);
    }
  }
  @media (max-width: 640px) {
    .repo {
      padding: var(--z-space-3);
    }
    .facts {
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: none;
      grid-auto-flow: row;
    }
  }
</style>
