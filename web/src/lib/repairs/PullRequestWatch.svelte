<!--
  One watched pull request: what Eli intends to do, what it saw, what the
  original failure was, what it changed and what the checks say now.

  It reads the repair history for its repository only when somebody opens it,
  because that is the one read here that is not free: it is a page of repairs,
  and most visits never open a row. What it does not do is reach into any of
  them for raw source, model output or job logs. Those are a repair's own
  details and stay behind the permission the attempt page asks for, which is why
  every attempt here is a link out and never an inline body.

  An observation that failed is never drawn as a clean result: the activity, the
  checks and the "next" line all come from `summariseWatch`, which reports
  "unknown" rather than "passed" for a read it has no evidence for.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { ChevronRight } from '@lucide/svelte';
  import { listEliRepairs } from '$lib/api/client';
  import type { Schemas } from '$lib/api/types';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import { formatNumber, pluralise, relativeTime, shortId } from '$lib/format';
  import { watchStatus } from '$lib/status';
  import { pullAttempts } from './attempt';
  import { caseFromSnapshot } from './case';
  import { checkOutcome, failingChecks, summariseWatch, type Watch } from './watches';

  interface Props {
    watch: Watch;
    /** What the repository was set to do, for the line that says what is followed. */
    behaviour: string;
    /** A control for this row is in flight, so its buttons stay put and say so. */
    busy?: boolean;
    now?: number;
    onpause: (watch: Watch) => void;
    onresume: (watch: Watch) => void;
    onstop: (watch: Watch) => void;
  }

  let {
    watch,
    behaviour,
    busy = false,
    now = Date.now(),
    onpause,
    onresume,
    onstop,
  }: Props = $props();

  const uid = $props.id();
  let open = $state(false);
  let repairs = $state<Schemas['EliRepair'][]>([]);
  let repairsTotal = $state(0);
  let repairsLoaded = $state(false);
  let repairsFailed = $state(false);
  /** Set once the read has been asked for, so opening and closing does not repeat it. */
  let asked = false;

  const summary = $derived(summariseWatch(watch, now));
  const kind = $derived(
    summary.state !== 'enabled'
      ? summary.state === 'stopped'
        ? 'stopped'
        : 'paused'
      : summary.attention.length > 0
        ? 'attention'
        : 'watching',
  );
  const failures = $derived(failingChecks(summary.checks));
  const latest = $derived(
    [...repairs]
      .filter((repair) => repair.pull_number === watch.pull_number)
      .sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at))[0],
  );
  const view = $derived(latest ? caseFromSnapshot(latest) : null);
  const attempts = $derived(pullAttempts(repairs, watch.repo, watch.pull_number));

  /**
   * The attempts for this pull request, read once and kept. A failed read leaves
   * the section saying so rather than saying Eli changed nothing, which are two
   * different sentences and only one of them is true.
   */
  async function readAttempts(): Promise<void> {
    if (asked) return;
    asked = true;
    try {
      const page = await listEliRepairs({ repo: watch.repo, limit: 50 });
      repairs = page.items ?? [];
      repairsTotal = page.total ?? repairs.length;
      repairsFailed = false;
    } catch {
      repairsFailed = true;
    } finally {
      repairsLoaded = true;
    }
  }

  $effect(() => {
    if (open) void readAttempts();
  });
  onMount(() => {
    if (open) void readAttempts();
  });
</script>

<article
  class="pull"
  class:attention={kind === 'attention'}
  aria-label="Watched pull request {watch.repo} #{watch.pull_number}"
>
  <header class="head">
    <h3>
      <span class="number">#{watch.pull_number}</span>
      <span class="title">{view?.title ?? 'Pull request'}</span>
    </h3>
    <Badge status={watchStatus(kind)} size="sm" />
    <p class="meta">{behaviour} · {summary.activity}</p>
  </header>

  <dl class="facts">
    <dt>Intended action</dt>
    <dd>{summary.intended}</dd>
    <dt>Next</dt>
    <dd>{summary.next}</dd>
  </dl>

  {#if summary.attention.length}
    <ul class="attention" aria-label="Needs a person">
      {#each summary.attention as line (line)}
        <li>{line}</li>
      {/each}
    </ul>
  {/if}

  <details class="more" bind:open>
    <summary>
      <ChevronRight size={16} class="chev" aria-hidden="true" />
      <span>Evidence, what failed and what changed</span>
    </summary>
    <div class="body">
      {#if repairsFailed}
        <p class="note problem" role="status">
          Could not read Eli's attempts for {watch.repo}, so what it has already changed is not
          known here.
        </p>
      {:else if !repairsLoaded}
        <p class="note">Reading Eli's attempts for this repository.</p>
      {:else if !latest}
        <p class="note">
          Eli has not attempted a repair on this pull request. Watching starts from the next
          observation.
        </p>
      {/if}

      <section aria-labelledby="{uid}-evidence">
        <h4 id="{uid}-evidence">Evidence last observed</h4>
        {#if watch.checks}
          {#if summary.checks}
            <p class="note">
              {formatNumber(summary.checks.total)} check
              {summary.checks.total === 1 ? 'result' : 'results'} on
              <code>{shortId(watch.head_sha, 8)}</code>, {summary.checks.state || 'no state'}.
              {#if summary.checks.truncated}Only the first {summary.checks.checks.length} are kept.{/if}
            </p>
            {#if summary.checks.checks.length}
              <ul class="checks">
                {#each summary.checks.checks as check (check.name + check.app)}
                  <li class:failed={!['success', 'neutral', 'skipped'].includes(check.conclusion)}>
                    <span class="name">{check.name}</span>
                    <span class="outcome">{checkOutcome(check)}</span>
                    {#if check.app}<span class="app">{check.app}</span>{/if}
                  </li>
                {/each}
              </ul>
              {#if failures.length && failures.length < summary.checks.checks.length}
                <p class="note">
                  {pluralise(failures.length, 'check')} did not pass, which is what Eli acts on.
                </p>
              {/if}
            {/if}
          {:else}
            <p class="note">
              No check reading has been recorded, so nothing is known about the checks on this
              commit.
            </p>
          {/if}
        {:else}
          <p class="note">This pull request is watched for merge conflicts, not checks.</p>
        {/if}
        {#if watch.conflicts}
          {#if summary.conflict}
            <p class="note">
              {summary.conflict.mergeable ? 'No conflict' : 'A conflict'} with
              <code>{summary.conflict.baseRef || watch.base_ref || 'the base branch'}</code>.
              {#if summary.conflict.mergeState}GitHub reports
                <code>{summary.conflict.mergeState}</code>.{/if}
            </p>
          {:else}
            <p class="note">
              Mergeability has not been read, so whether this branch conflicts is not known.
            </p>
          {/if}
        {/if}
      </section>

      {#if latest && view}
        <section aria-labelledby="{uid}-failed">
          <h4 id="{uid}-failed">What failed first</h4>
          {#if view.jobs.length}
            <ul class="jobs">
              {#each view.jobs as job (job.key)}
                <li>
                  <span class="name">{job.name}</span>
                  {#if job.step}<span class="outcome">stopped at {job.step}</span>{/if}
                </li>
              {/each}
            </ul>
          {:else}
            <p class="note">The attempt kept no failed job: it was made before cases were kept.</p>
          {/if}
        </section>

        <section aria-labelledby="{uid}-changed">
          <h4 id="{uid}-changed">What Eli changed</h4>
          {#if latest.commit_sha}
            <p class="note">
              One commit, <code>{shortId(latest.commit_sha, 8)}</code>, published
              {relativeTime(latest.updated_at, now)}.
            </p>
            {#if latest.details?.changed_files?.length}
              <ul class="files">
                {#each latest.details.changed_files as file (file)}
                  <li><code>{file}</code></li>
                {/each}
              </ul>
            {/if}
            {#if latest.details?.summary}
              <p class="summary">{latest.details.summary}</p>
            {/if}
          {:else}
            <p class="note">Eli published no commit for this pull request. {latest.message}</p>
          {/if}
          <p class="note">
            <a href="/repairs/{encodeURIComponent(latest.id)}">Open this attempt</a> for its steps, tokens
            and the retained model response.
          </p>
        </section>
      {/if}

      {#if attempts.length}
        <section aria-labelledby="{uid}-attempts">
          <h4 id="{uid}-attempts">Attempts on this pull request</h4>
          <ol class="attempts">
            {#each attempts as attempt (attempt.id)}
              <li class:current={attempt.id === latest?.id}>
                <a
                  href="/repairs/{encodeURIComponent(attempt.id)}"
                  aria-label="Open the attempt from {relativeTime(
                    attempt.at,
                    now,
                  )}, {attempt.stateLabel.toLowerCase()}">{attempt.stateLabel}</a
                >
                <span class="when">{relativeTime(attempt.at, now)}</span>
                <span class="began">{attempt.began}</span>
                {#if attempt.commitSha}<code>{shortId(attempt.commitSha, 8)}</code>{/if}
                {#if attempt.id === latest?.id}<span class="current-tag">Current</span>{/if}
              </li>
            {/each}
          </ol>
          {#if repairsTotal > repairs.length}
            <p class="note">
              The newest {formatNumber(repairs.length)} repairs in this repository are listed, of
              {formatNumber(repairsTotal)}. An older attempt may not be here.
            </p>
          {/if}
        </section>
      {/if}
    </div>
  </details>

  <div class="acts">
    <Button
      variant="secondary"
      size="sm"
      href="/repairs/new?repo={encodeURIComponent(watch.repo)}&pull={watch.pull_number}"
      ariaLabel="Fix pull request {watch.pull_number} once, using your own provider"
      >Fix this PR once</Button
    >
    {#if summary.state === 'enabled'}
      <Button
        variant="ghost"
        size="sm"
        disabled={busy}
        ariaLabel="Pause watching pull request {watch.pull_number}"
        onclick={() => onpause(watch)}>Pause</Button
      >
    {:else if summary.state === 'paused'}
      <Button
        variant="ghost"
        size="sm"
        disabled={busy}
        ariaLabel="Resume watching pull request {watch.pull_number}"
        onclick={() => onresume(watch)}>Resume</Button
      >
    {/if}
    {#if summary.state !== 'stopped'}
      <Button
        variant="ghost"
        size="sm"
        disabled={busy}
        ariaLabel="Stop watching pull request {watch.pull_number}"
        onclick={() => onstop(watch)}>Stop</Button
      >
    {/if}
  </div>
</article>

<style>
  .pull {
    display: grid;
    gap: var(--z-space-2);
    padding: var(--z-space-3) var(--z-space-4);
    border-top: var(--z-border-width) solid var(--z-border);
    background: var(--z-surface);
  }
  .pull:first-child {
    border-top: 0;
  }
  /* The rail is the device the disclosures use for a section in trouble: one
     left edge, and the colour is only ever a second carrier of the word beside
     it. */
  .pull.attention {
    border-left: var(--z-border-width-rail) solid var(--z-danger);
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-1) var(--z-space-2);
    min-width: 0;
  }
  h3 {
    display: flex;
    align-items: baseline;
    gap: var(--z-space-2);
    margin: 0;
    min-width: 0;
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-semibold);
  }
  .number {
    flex: none;
    color: var(--z-text-muted);
    font-variant-numeric: tabular-nums;
  }
  .title {
    overflow-wrap: anywhere;
  }
  .meta {
    flex-basis: 100%;
    margin: 0;
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
    overflow-wrap: anywhere;
  }
  .facts {
    /* Term above value, two pairs across, as the repository rows are. Direct
       dt/dd children keep the definition list valid for a screen reader. */
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
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
  .attention {
    display: grid;
    gap: var(--z-space-1);
    margin: 0;
    padding: 0;
    list-style: none;
    color: var(--z-danger);
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
  }
  .more {
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
  }
  .more > summary {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    min-height: var(--z-control-touch);
    padding: var(--z-space-2) var(--z-space-3);
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
    cursor: pointer;
    list-style: none;
  }
  .more > summary::-webkit-details-marker {
    display: none;
  }
  .more > summary:hover {
    background: var(--z-surface-hover);
  }
  .more > summary:focus-visible {
    outline: 2px solid var(--z-focus-colour);
    outline-offset: -2px;
  }
  .more :global(.chev) {
    flex: none;
    color: var(--z-text-subtle);
    transition: transform var(--z-motion-fast) var(--z-ease);
  }
  .more[open] :global(.chev) {
    transform: rotate(90deg);
  }
  .body {
    display: grid;
    gap: var(--z-space-3);
    padding: var(--z-space-3);
    border-top: var(--z-border-width) solid var(--z-border);
  }

  h4 {
    margin: 0 0 var(--z-space-1);
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
    font-weight: var(--z-weight-semibold);
  }
  .note {
    margin: 0;
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-sm);
    overflow-wrap: anywhere;
  }
  .note.problem {
    color: var(--z-danger);
  }
  .summary {
    margin: var(--z-space-2) 0 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    overflow-wrap: anywhere;
  }
  .checks,
  .jobs,
  .files {
    display: grid;
    gap: var(--z-space-1);
    margin: var(--z-space-2) 0 0;
    padding: 0;
    list-style: none;
    font-size: var(--z-text-xs);
  }
  .attempts {
    display: grid;
    gap: var(--z-space-1);
    margin: var(--z-space-2) 0 0;
    padding: 0;
    list-style: none;
    font-size: var(--z-text-xs);
  }
  .attempts li {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--z-space-2);
  }
  .attempts a {
    color: var(--z-text);
  }
  .attempts .when,
  .attempts .began {
    color: var(--z-text-muted);
  }
  .attempts .current-tag {
    color: var(--z-text-subtle);
    font-weight: var(--z-weight-semibold);
  }
  .checks li,
  .jobs li {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
    align-items: baseline;
  }
  .checks .outcome,
  .jobs .outcome {
    color: var(--z-text-muted);
  }
  .checks li.failed .outcome {
    color: var(--z-danger);
  }
  .app,
  .files code {
    color: var(--z-text-subtle);
  }
  .name {
    overflow-wrap: anywhere;
  }
  .acts {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
  @media (max-width: 640px) {
    .pull {
      padding: var(--z-space-3);
    }
    .facts {
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: none;
      grid-auto-flow: row;
    }
  }
</style>
