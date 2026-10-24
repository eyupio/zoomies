<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import {
    ApiError,
    inspectShardingPreview,
    retainShardingPreview,
    getShardingPreview,
  } from '$lib/api/client';
  import type {
    Job,
    OptimisationEnablement,
    ShardingInspection,
    ShardingPreview,
  } from '$lib/api/types';
  import { checkoutSeconds } from '$lib/optimisations/input';
  import { session } from '$lib/state/session.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Button from '$lib/components/Button.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import Checkbox from '$lib/components/Checkbox.svelte';
  import CheckoutRunPicker from '$lib/optimisations/CheckoutRunPicker.svelte';
  import OptimisationGate from '$lib/optimisations/OptimisationGate.svelte';
  import ShardingPreviewCard from '$lib/optimisations/ShardingPreviewCard.svelte';
  import { router } from '$lib/router';

  let chosen = $state<{
    installation: string;
    repo: string;
    run_id: string;
    attempt: number;
    github_job_id: string;
    label: string;
  } | null>(null);
  let repo = $state('');
  let enablement = $state<OptimisationEnablement | undefined>();
  let gateRefresh = $state(0);
  const previewsOff = $derived(enablement !== undefined && !enablement.enabled);
  let inspection = $state<ShardingInspection | null>(null);
  let reviewed = $state(false);
  let preview = $state<ShardingPreview | null>(null);
  let previewId = $state(''),
    openInstallation = $state(''),
    openRepo = $state('');
  let loadedKey = '';
  let busy = $state(false),
    error = $state(''),
    retryAt = $state(0),
    retryBlocked = $state(false);
  let operation = 0;
  let alertElement = $state<HTMLParagraphElement | null>(null);
  let reviewHeading = $state<HTMLHeadingElement | null>(null);
  let controller: AbortController | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const canCapture = $derived(session.can('operator') && !session.visitor);
  $effect(() => {
    if (error) alertElement?.focus();
  });

  function failed(e: unknown) {
    error =
      e instanceof ApiError && e.isNotFound
        ? 'This preview is unavailable. The job or its evidence may be outside this scope.'
        : e instanceof Error
          ? e.message
          : 'The request could not be completed.';
    if (e instanceof ApiError && e.status === 403) gateRefresh++;
    if (e instanceof ApiError && e.status === 429) {
      retryAt = e.retryAt ?? Date.now() + 60000;
      retryBlocked = true;
      clearTimeout(timer);
      timer = setTimeout(
        () => {
          retryBlocked = false;
        },
        Math.max(0, retryAt - Date.now()),
      );
    }
  }
  function begin(): number {
    const current = ++operation;
    controller?.abort();
    controller = new AbortController();
    busy = true;
    error = '';
    return current;
  }
  function pick(job: Job) {
    chosen = {
      installation: job.installation_id ?? '',
      repo: job.repo ?? '',
      run_id: String(job.github_run_id),
      attempt: job.run_attempt ?? 1,
      github_job_id: String(job.github_job_id),
      label: `${job.workflow || 'Workflow'}, job ${job.job_name}`,
    };
    inspection = null;
    preview = null;
    reviewed = false;
  }
  async function inspect() {
    if (busy || retryBlocked || !canCapture || !chosen) return;
    const current = begin();
    inspection = null;
    try {
      const result = await inspectShardingPreview(
        {
          installation_id: chosen.installation,
          repository: chosen.repo,
          selection: {
            run_id: chosen.run_id,
            attempt: chosen.attempt,
            github_job_id: chosen.github_job_id,
          },
        },
        controller?.signal,
      );
      if (current !== operation) return;
      inspection = result;
      reviewed = false;
      preview = null;
      queueMicrotask(() => reviewHeading?.focus());
    } catch (e) {
      if (current === operation) failed(e);
    } finally {
      if (current === operation) busy = false;
    }
  }
  async function retain() {
    if (busy || retryBlocked || !canCapture || !inspection || !reviewed) return;
    const current = begin();
    try {
      const result = await retainShardingPreview(
        {
          installation_id: inspection.installation_id,
          repository: inspection.repository,
          selection: inspection.selection,
          head_sha: inspection.head_sha,
          source_sha256: inspection.source_sha256,
          shards: inspection.shards,
        },
        controller?.signal,
      );
      if (current !== operation) return;
      preview = result;
      previewId = result.id;
      loadedKey = `${result.id}|${result.installation_id}|${result.repository}`;
      router.setQuery({
        id: result.id,
        installation_id: result.installation_id,
        repo: result.repository,
      });
      inspection = null;
      reviewed = false;
    } catch (e) {
      if (current === operation) failed(e);
    } finally {
      if (current === operation) busy = false;
    }
  }
  async function read(readId = previewId, inst = openInstallation, repository = openRepo) {
    const current = begin();
    preview = null;
    inspection = null;
    try {
      openInstallation = inst;
      openRepo = repository;
      previewId = readId;
      if (
        !/^ins_[a-zA-Z0-9_-]{1,124}$/.test(inst) ||
        !/^[^\s/?#]+\/[^\s/?#]+$/.test(repository) ||
        repository.length > 1024
      )
        throw new Error('Supply an installation ID starting ins_ and repository OWNER/REPO.');
      if (!/^ops_[a-zA-Z0-9_-]{1,124}$/.test(readId))
        throw new Error('Supply a sharding preview ID starting ops_.');
      const result = await getShardingPreview(readId, inst, repository, controller?.signal);
      if (current !== operation) return;
      preview = result;
      loadedKey = `${readId}|${inst}|${repository}`;
      router.setQuery({ id: readId, installation_id: inst, repo: repository });
    } catch (e) {
      if (current === operation) failed(e);
    } finally {
      if (current === operation) busy = false;
    }
  }
  $effect(() => {
    const readId = router.param('id'),
      inst = router.param('installation_id'),
      repository = router.param('repo');
    if (session.visitor) return;
    untrack(() => {
      if (readId && inst && repository && `${readId}|${inst}|${repository}` !== loadedKey)
        void read(readId, inst, repository);
    });
  });
  function cleanup() {
    operation++;
    controller?.abort();
    clearTimeout(timer);
  }
  onDestroy(cleanup);
</script>

<PageHeader
  title="Sharding preview"
  subtitle="Split a Playwright test job into shards that together run the whole suite. A preview changes nothing on GitHub."
  ><Button href="/optimisations/readiness" variant="secondary">Readiness preview</Button><Button
    href="/optimisations/cache"
    variant="secondary">Cache preview</Button
  ><Button href="/optimisations/checkout" variant="secondary">Checkout preview</Button></PageHeader
>
{#if session.visitor}
  <ErrorState
    title="Sign in to preview a sharding change"
    description="Previewing a workflow's test job needs an operator."
  />
{:else}
  <div class="page">
    {#if canCapture}
      <OptimisationGate bind:enablement refreshKey={gateRefresh} />
      <section class="card" aria-labelledby="choose-sharding-title">
        <h2 id="choose-sharding-title">1. Choose a run of the test job</h2>
        <p>
          Pick a recent successful run that a push started. Zoomies reads the workflow at that
          commit and finds its one Playwright test step on an Ubuntu runner. The number of shards
          comes from the fleet's own history of that job and from how many runners the pool could
          start at once; you do not choose it, and Zoomies never guesses it from the size of the
          repository.
        </p>
        <CheckoutRunPicker
          bind:repo
          disabled={busy || retryBlocked}
          actionsDisabled={previewsOff}
          onpick={pick}
        />
        {#if chosen}
          <div class="ready">
            <p>Ready to read <strong>{chosen.label}</strong> in {chosen.repo}.</p>
            <Button
              variant="primary"
              disabled={busy || retryBlocked || previewsOff}
              reason={previewsOff ? 'Turn on workflow previews above first.' : undefined}
              onclick={inspect}>Read this workflow</Button
            >
          </div>
        {/if}
      </section>
    {:else}
      <p>Previewing a sharding change needs an operator.</p>
    {/if}

    {#if busy}<p role="status">Working…</p>{/if}
    {#if error}<p role="alert" tabindex="-1" bind:this={alertElement}>{error}</p>{/if}
    {#if retryBlocked}<p role="status">
        Wait until {new Date(retryAt).toLocaleTimeString()} before another request. Retry manually; this
        page does not poll.
      </p>{/if}

    {#if inspection}
      <section class="card" aria-labelledby="review-sharding-title">
        <h2 id="review-sharding-title" tabindex="-1" bind:this={reviewHeading}>
          2. Review the shards
        </h2>
        <p>
          Found the test step <strong>{inspection.step_name}</strong> in job
          <strong>{inspection.job_name}</strong>. It would run as
          <strong>{inspection.shards} shards</strong> that together run the whole suite, and a job with
          the original name stays behind to fail unless every shard passed, so a required check that names
          the job still resolves.
        </p>
        <dl class="facts">
          <div>
            <dt>File</dt>
            <dd>{inspection.workflow_path}, line {inspection.line}</dd>
          </div>
          <div>
            <dt>Commit</dt>
            <dd class="mono">{inspection.head_sha}</dd>
          </div>
          <div>
            <dt>History</dt>
            <dd>
              the test step took a median {checkoutSeconds(inspection.test_ms)} after {checkoutSeconds(
                inspection.setup_ms,
              )} of setup, over {inspection.runs} runs
            </dd>
          </div>
          <div>
            <dt>Runner time per run</dt>
            <dd>
              about {checkoutSeconds(inspection.runner_ms_before)} now, about {checkoutSeconds(
                inspection.runner_ms_after,
              )} sharded, plus one short aggregator job. It rises because every shard repeats the setup.
            </dd>
          </div>
          <div>
            <dt>Runners needed at once</dt>
            <dd>
              {inspection.shards} of the {inspection.headroom} the pool could start now (an upper bound
              at one moment, not a reservation)
            </dd>
          </div>
          <div>
            <dt>Wall clock</dt>
            <dd>
              estimated about {checkoutSeconds(inspection.wall_ms_after)} instead of {checkoutSeconds(
                inspection.wall_ms_before,
              )} if the shards start together (an estimate from medians, not a saving)
            </dd>
          </div>
        </dl>
        <div
          class="diff"
          role="group"
          aria-label="The job block this preview changes, untrusted text"
        >
          <pre class="removed"><span aria-hidden="true">- </span><span class="sr"
              >Removed: </span>{inspection.before.replace(/\n$/, '')}</pre>
          <pre class="added"><span aria-hidden="true">+ </span><span class="sr"
              >Added: </span>{inspection.after.replace(/\n$/, '')}</pre>
        </div>
        <p>
          Playwright splits the suite itself, so no test is chosen or dropped by Zoomies. Passing CI
          alone cannot establish a saving, and more runner time is certain.
        </p>
        <Checkbox
          bind:checked={reviewed}
          label="I reviewed this job block and the extra runner time; no saving has been measured."
          disabled={busy}
        />
        <Button
          onclick={retain}
          loading={busy && !!inspection && reviewed}
          disabled={busy || retryBlocked || !reviewed}>Keep a preview for 14 days</Button
        >
        <p>
          Keeping retains the exact source, history and shard count privately for fourteen days. It
          approves and publishes nothing and does not open a pull request.
        </p>
      </section>
    {/if}

    {#if preview}<ShardingPreviewCard {preview} />{/if}

    {#if canCapture}
      <details class="more card">
        <summary>Open a kept sharding preview</summary>
        <form
          onsubmit={(e) => {
            e.preventDefault();
            if (!busy && !retryBlocked) void read();
          }}
        >
          <div class="fields">
            <Field label="Installation ID" required
              >{#snippet children(f)}<Input
                  {...f}
                  bind:value={openInstallation}
                  disabled={busy}
                  placeholder="ins_..."
                />{/snippet}</Field
            >
            <Field label="Repository" required
              >{#snippet children(f)}<Input
                  {...f}
                  bind:value={openRepo}
                  disabled={busy}
                  placeholder="OWNER/REPO"
                />{/snippet}</Field
            >
            <Field label="Preview ID" required
              >{#snippet children(f)}<Input
                  {...f}
                  bind:value={previewId}
                  disabled={busy}
                  placeholder="ops_..."
                />{/snippet}</Field
            >
          </div>
          <Button type="submit" variant="secondary" disabled={busy || retryBlocked}
            >Open preview</Button
          >
        </form>
      </details>
    {/if}
  </div>
{/if}

<style>
  .page {
    display: grid;
    gap: var(--z-space-6);
  }
  .card {
    padding: var(--z-space-6);
    background: var(--z-surface);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-lg);
    min-width: 0;
  }
  h2 {
    font-size: var(--z-text-lg);
    font-weight: var(--z-weight-semibold);
    margin-block: var(--z-space-4);
  }
  p {
    margin-block: var(--z-space-3);
  }
  .ready,
  .fields {
    display: grid;
    gap: var(--z-space-3);
  }
  .ready {
    margin-block-start: var(--z-space-4);
    padding-block-start: var(--z-space-4);
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .facts {
    display: grid;
    gap: var(--z-space-2);
  }
  .facts div {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
  dt {
    font-weight: var(--z-weight-semibold);
  }
  dd {
    overflow-wrap: anywhere;
    min-width: 0;
  }
  .mono {
    font-family: var(--z-font-mono);
  }
  .diff {
    margin-block: var(--z-space-4);
    overflow: auto;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
  }
  pre {
    font-family: var(--z-font-mono);
    font-size: var(--z-text-sm);
    padding: var(--z-space-2) var(--z-space-4);
    white-space: pre;
    margin: 0;
  }
  .added {
    font-weight: var(--z-weight-semibold);
    background: var(--z-surface-sunken);
  }
  .more {
    padding: var(--z-space-4);
  }
  summary {
    cursor: pointer;
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
