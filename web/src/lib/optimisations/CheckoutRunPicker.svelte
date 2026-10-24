<!--
  Choose the run to read from a list of recent successful ones, so nobody types
  GitHub IDs. Only a successful run started by a push can be previewed, because
  the workflow file is read at that push's commit; the list says so and offers
  nothing else.
-->
<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { ApiError, getJobFacets, listJobs } from '$lib/api/client';
  import type { Job } from '$lib/api/types';
  import { NO_VALUE } from '$lib/format';
  import Button from '$lib/components/Button.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import Select from '$lib/components/Select.svelte';

  let {
    repo = $bindable(''),
    disabled = false,
    actionsDisabled = false,
    onpick,
  }: {
    repo?: string;
    disabled?: boolean;
    /** Looking at runs stays allowed while reviewing one is not. */
    actionsDisabled?: boolean;
    onpick: (job: Job) => void;
  } = $props();

  let repos = $state<string[]>([]);
  let jobs = $state<Job[]>([]);
  let loading = $state(false),
    error = $state('');
  let controller: AbortController | undefined;

  const pickable = (job: Job) =>
    job.run_event === 'push' &&
    !!job.installation_id &&
    !!job.github_run_id &&
    !!job.github_job_id &&
    (job.run_attempt ?? 1) >= 1;

  async function loadRepos() {
    try {
      repos = (await getJobFacets()).repos ?? [];
    } catch {
      repos = [];
    }
  }
  async function loadJobs() {
    controller?.abort();
    jobs = [];
    error = '';
    if (!repo) return;
    controller = new AbortController();
    loading = true;
    try {
      const page = await listJobs(
        { repo: [repo], state: ['completed'], conclusion: ['success'], limit: 50 },
        controller.signal,
      );
      jobs = (page.items ?? []).filter(pickable);
    } catch (cause) {
      if (cause instanceof DOMException && cause.name === 'AbortError') return;
      error = cause instanceof ApiError ? cause.message : 'Could not list recent runs.';
    } finally {
      loading = false;
    }
  }
  onMount(() => void loadRepos());
  $effect(() => {
    void repo;
    void loadJobs();
  });
  onDestroy(() => controller?.abort());
</script>

<div class="picker">
  <Select
    bind:value={repo}
    options={repos.map((r) => ({ value: r, label: r }))}
    placeholder="Choose a repository"
    ariaLabel="Repository"
    {disabled}
  />
  {#if loading}<p role="status">Finding recent successful runs…</p>{/if}
  {#if error}<p role="alert">{error}</p>{/if}
  {#if repo && !loading && !error && jobs.length === 0}
    <EmptyState
      title="No recent runs to preview"
      description="Only a successful job from a run started by a push can be previewed, because the workflow is read at that push's commit. Nothing like that has run in {repo} lately."
    />
  {:else if jobs.length > 0}
    <table>
      <caption class="sr">Recent successful jobs started by a push in {repo}</caption>
      <thead>
        <tr
          ><th scope="col">Workflow</th><th scope="col">Job</th><th scope="col">Branch</th><th
            scope="col">Finished</th
          ><th scope="col"><span class="sr">Action</span></th></tr
        >
      </thead>
      <tbody>
        {#each jobs as job (job.id)}
          <tr>
            <td>{job.workflow || NO_VALUE}</td>
            <td>{job.job_name}</td>
            <td>{job.head_branch || NO_VALUE}</td>
            <td
              >{#if job.completed_at}<RelativeTime value={job.completed_at} />{/if}</td
            >
            <td
              ><Button
                variant="secondary"
                disabled={disabled || actionsDisabled}
                reason={actionsDisabled && !disabled
                  ? 'Turn on workflow previews above first.'
                  : undefined}
                onclick={() => onpick(job)}>Review</Button
              ></td
            >
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>

<style>
  .picker {
    display: grid;
    gap: var(--z-space-4);
    min-width: 0;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    display: block;
    overflow-x: auto;
    /* The visually hidden header cell is absolutely positioned; without a
       positioned scroller it escapes the scroll area and widens the page. */
    position: relative;
  }
  th,
  td {
    text-align: start;
    padding: var(--z-space-2) var(--z-space-3);
    border-bottom: var(--z-border-width) solid var(--z-border);
    white-space: nowrap;
  }
  th {
    font-weight: var(--z-weight-semibold);
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
</style>
