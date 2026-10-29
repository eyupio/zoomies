<!--
  Pick a failure to repair. It lists the recent failed checks Zoomies knows about,
  so the common path is a press, not a copied URL; the paste box stays as the
  fallback for a pull request that has not failed here or is not in the list.
  Choosing never starts a repair and spends nothing: it opens the review page.
-->
<script lang="ts">
  import { onDestroy } from 'svelte';
  import { ApiError, listJobs } from '$lib/api/client';
  import type { Job } from '$lib/api/types';
  import Button from '$lib/components/Button.svelte';
  import Dialog from '$lib/components/Dialog.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import { router } from '$lib/router';
  import { eliCanRepairRun, targetHref } from './workspace';
  import { parseRepairTarget } from './target';

  let { open = $bindable(false) }: { open: boolean } = $props();

  let value = $state('');
  let error = $state('');
  let failures = $state<Job[]>([]);
  let loading = $state(false);
  let readError = $state('');
  let token = 0;

  const FORM = 'repair-target-form';

  /** One row per run: several failed jobs of the same run are one repair. */
  function recent(items: Job[]): Job[] {
    const seen: string[] = [];
    const out: Job[] = [];
    for (const job of items) {
      if (!job.repo || !job.github_run_id || !eliCanRepairRun(job)) continue;
      const key = `${job.repo}:${job.github_run_id}`;
      if (seen.includes(key)) continue;
      seen.push(key);
      out.push(job);
    }
    return out;
  }

  async function load(): Promise<void> {
    const mine = ++token;
    loading = true;
    readError = '';
    try {
      const page = await listJobs({ failed: true, limit: 20 });
      if (mine !== token) return;
      failures = recent(page.items ?? []);
    } catch (cause) {
      if (mine !== token) return;
      readError =
        cause instanceof ApiError
          ? cause.message
          : 'Could not read recent failures. Paste a pull request link instead.';
    } finally {
      if (mine === token) loading = false;
    }
  }

  function choose(job: Job): void {
    if (!job.repo || !job.github_run_id) return;
    open = false;
    router.navigate(
      targetHref({ repo: job.repo, run: job.github_run_id, job: job.id ?? undefined }),
    );
  }

  function submit(event: SubmitEvent): void {
    event.preventDefault();
    const target = parseRepairTarget(value);
    if (!target) {
      error = 'Paste a GitHub pull request URL, such as https://github.com/owner/repo/pull/123.';
      return;
    }
    error = '';
    value = '';
    open = false;
    router.navigate(targetHref({ repo: target.repo, pull: target.pull_number }));
  }

  // A dialog opened again reads the failures again, and starts clean.
  $effect(() => {
    if (!open) {
      error = '';
      return;
    }
    void load();
  });

  onDestroy(() => {
    token++;
  });
</script>

<Dialog
  bind:open
  title="Fix a failed check"
  description="Choose a recent failed check, or paste a pull request link. Eli checks it before anything is spent."
>
  <div class="failures">
    {#if loading}
      <p class="muted" role="status">Reading recent failures.</p>
    {:else if readError}
      <p class="muted" role="alert">{readError}</p>
    {:else if failures.length === 0}
      <p class="muted">No recent failed pull request checks. Paste a pull request link below.</p>
    {:else}
      <ul aria-label="Recent failures">
        {#each failures as job (job.id ?? `${job.repo}:${job.github_run_id}`)}
          <li>
            <button type="button" class="failure" onclick={() => choose(job)}>
              <span class="title">{job.job_name || job.workflow || 'A failed check'}</span>
              <span class="meta">
                <span class="repo">{job.repo}</span>
                {#if job.workflow}<span>{job.workflow}</span>{/if}
                <span
                  ><RelativeTime value={job.completed_at ?? job.queued_at} prefix="Failed" /></span
                >
              </span>
            </button>
          </li>
        {/each}
      </ul>
      {#if failures.length === 20}
        <p class="more">The newest 20 are shown.</p>
      {/if}
    {/if}
  </div>

  <details class="paste">
    <summary>Paste a pull request link</summary>
    <form id={FORM} onsubmit={submit}>
      <Field
        label="Pull request"
        hint="For example https://github.com/owner/repo/pull/123."
        {error}
      >
        {#snippet children({ id, describedBy, invalid })}
          <Input
            {id}
            {describedBy}
            {invalid}
            bind:value
            placeholder="https://github.com/owner/repo/pull/123"
            autocomplete="off"
            spellcheck={false}
          />
        {/snippet}
      </Field>
      <div class="paste-actions">
        <Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
        <Button variant="primary" type="submit" form={FORM}>Check this pull request</Button>
      </div>
    </form>
  </details>
</Dialog>

<style>
  .failures {
    min-height: 3rem;
  }
  .muted {
    margin: 0;
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
  }
  ul {
    display: grid;
    margin: 0;
    padding: 0;
    list-style: none;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    overflow: hidden;
  }
  .failure {
    display: grid;
    gap: var(--z-space-1);
    width: 100%;
    padding: var(--z-space-3) var(--z-space-4);
    border: 0;
    border-top: var(--z-border-width) solid var(--z-border);
    background: var(--z-surface);
    color: var(--z-text);
    text-align: left;
    cursor: pointer;
  }
  li:first-child .failure {
    border-top: 0;
  }
  .failure:hover,
  .failure:focus-visible {
    background: var(--z-surface-hover);
  }
  .title {
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-semibold);
    overflow-wrap: anywhere;
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-1) var(--z-space-3);
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
  }
  .repo {
    font-family: var(--z-font-mono);
  }
  .more {
    margin: var(--z-space-2) 0 0;
    color: var(--z-text-subtle);
    font-size: var(--z-text-xs);
  }
  .paste {
    margin-top: var(--z-space-4);
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .paste > summary {
    padding: var(--z-space-3) 0;
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
    cursor: pointer;
  }
  .paste form {
    padding-top: var(--z-space-2);
  }
  .paste-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
    margin-top: var(--z-space-3);
  }
</style>
