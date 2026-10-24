<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import {
    inspectCachePreview,
    retainCachePreview,
    approveCachePreview,
    publishCachePreview,
    getCachePreview,
    ApiError,
  } from '$lib/api/client';
  import type {
    CacheInspection,
    CachePreview,
    CacheSelection,
    Job,
    OptimisationEnablement,
  } from '$lib/api/types';
  import { parseCheckoutSelection, checkoutSeconds } from '$lib/optimisations/input';
  import { router } from '$lib/router';
  import { session } from '$lib/state/session.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Button from '$lib/components/Button.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import Checkbox from '$lib/components/Checkbox.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import CachePreviewCard from '$lib/optimisations/CachePreviewCard.svelte';
  import CheckoutRunPicker from '$lib/optimisations/CheckoutRunPicker.svelte';
  import OptimisationGate from '$lib/optimisations/OptimisationGate.svelte';
  import PublicationGate from '$lib/optimisations/PublicationGate.svelte';

  let chosen = $state<{
    installation: string;
    repo: string;
    selection: CacheSelection;
    label: string;
  } | null>(null);
  let repo = $state('');
  let previewId = $state(''),
    openInstallation = $state(''),
    openRepo = $state('');
  let enablement = $state<OptimisationEnablement | undefined>();
  let gateRefresh = $state(0);
  const previewsOff = $derived(enablement !== undefined && !enablement.enabled);
  let inspection = $state<CacheInspection | null>(null);
  let reviewed = $state(false);
  let preview = $state<CachePreview | null>(null);
  let busy = $state(false),
    error = $state(''),
    retryAt = $state(0),
    retryBlocked = $state(false);
  let operation = 0,
    loadedKey = '';
  let alertElement = $state<HTMLParagraphElement | null>(null);
  let sourceHeading = $state<HTMLHeadingElement | null>(null);
  let controller: AbortController | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const canCapture = $derived(session.can('operator') && !session.visitor);
  $effect(() => {
    if (error) alertElement?.focus();
  });

  function failed(e: unknown) {
    error =
      e instanceof ApiError && e.isNotFound
        ? 'This preview is unavailable. It may be expired, evicted or outside this scope. Nothing was recreated.'
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
  const validRepo = (value: string) =>
    /^[^\s/\\?#]+\/[^\s/\\?#]+$/.test(value) && value.length <= 1024;

  async function inspect(target: NonNullable<typeof chosen>) {
    if (busy || retryBlocked || !canCapture) return;
    const current = begin();
    chosen = target;
    inspection = null;
    reviewed = false;
    preview = null;
    try {
      const result = await inspectCachePreview(
        {
          installation_id: target.installation,
          repository: target.repo,
          selection: target.selection,
        },
        controller?.signal,
      );
      if (current !== operation) return;
      inspection = result;
      queueMicrotask(() => sourceHeading?.focus());
    } catch (e) {
      if (current === operation) failed(e);
    } finally {
      if (current === operation) busy = false;
    }
  }
  function pick(job: Job) {
    void inspect({
      installation: job.installation_id ?? '',
      repo: job.repo ?? '',
      selection: {
        run_id: String(job.github_run_id),
        attempt: job.run_attempt ?? 1,
        github_job_id: String(job.github_job_id),
      },
      label: `${job.workflow || 'Workflow'}, job ${job.job_name}`,
    });
  }
  async function retain() {
    if (busy || retryBlocked || !canCapture || !inspection || !reviewed || !chosen) return;
    const current = begin();
    try {
      const result = await retainCachePreview(
        {
          installation_id: chosen.installation,
          repository: chosen.repo,
          selection: inspection.selection,
          head_sha: inspection.head_sha,
          source_sha256: inspection.source_sha256,
          lockfile_sha256: inspection.lockfile_sha256,
        },
        controller?.signal,
      );
      if (current !== operation) return;
      preview = result;
      previewId = result.id;
      loadedKey = `${result.id}|${chosen.installation}|${chosen.repo}`;
      router.setQuery({ id: result.id, installation_id: chosen.installation, repo: chosen.repo });
      inspection = null;
      reviewed = false;
    } catch (e) {
      if (current === operation) failed(e);
    } finally {
      if (current === operation) busy = false;
    }
  }
  async function approve() {
    if (busy || retryBlocked || !canCapture || !preview) return;
    const current = begin();
    try {
      const result = await approveCachePreview(
        preview.id,
        {
          installation_id: preview.installation_id,
          repository: preview.repository,
          head_sha: preview.head_sha,
          source_sha256: preview.source_sha256,
        },
        controller?.signal,
      );
      if (current !== operation) return;
      preview = result;
    } catch (e) {
      if (current === operation) failed(e);
    } finally {
      if (current === operation) busy = false;
    }
  }
  async function publish() {
    if (busy || retryBlocked || !canCapture || !preview) return;
    const current = begin();
    try {
      const result = await publishCachePreview(
        preview.id,
        {
          installation_id: preview.installation_id,
          repository: preview.repository,
          head_sha: preview.head_sha,
          source_sha256: preview.source_sha256,
        },
        controller?.signal,
      );
      if (current !== operation) return;
      preview = result;
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
      if (!/^ins_[a-zA-Z0-9_-]{1,124}$/.test(inst) || !validRepo(repository))
        throw new Error('Supply an installation ID starting ins_ and repository OWNER/REPO.');
      if (!/^opc_[a-zA-Z0-9_-]{1,124}$/.test(readId))
        throw new Error('Supply a cache preview ID starting opc_.');
      const result = await getCachePreview(readId, inst, repository, controller?.signal);
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
      repository = router.param('repo'),
      run = router.param('run_id'),
      job = router.param('job_id'),
      tries = router.param('attempt');
    if (session.visitor) return;
    untrack(() => {
      if (readId && inst && repository) {
        if (`${readId}|${inst}|${repository}` !== loadedKey) void read(readId, inst, repository);
      } else if (run && job && inst && repository && !chosen) {
        try {
          const selection = parseCheckoutSelection({
            runId: run,
            attempt: tries || '1',
            jobId: job,
          });
          chosen = {
            installation: inst,
            repo: repository,
            selection,
            label: `Run ${selection.run_id}, job ${selection.github_job_id}`,
          };
        } catch (e) {
          failed(e);
        }
      }
    });
  });
  onDestroy(() => {
    operation++;
    controller?.abort();
    clearTimeout(timer);
  });
</script>

<PageHeader
  title="Cache preview"
  subtitle="See whether a Node build can cache its dependencies consistently. A preview changes nothing on GitHub."
  ><Button href="/optimisations/checkout" variant="secondary">Checkout preview</Button><Button
    href="/optimisations/readiness"
    variant="secondary">Readiness preview</Button
  ><Button href="/comparisons" variant="secondary">Compare runs</Button></PageHeader
>
{#if session.visitor}
  <ErrorState
    title="Sign in to read cache previews"
    description="Previewing a workflow's setup cache needs an operator."
  />
{:else}
  <div class="page">
    {#if canCapture}
      <OptimisationGate bind:enablement refreshKey={gateRefresh} />
      <section class="card" aria-labelledby="choose-cache-title">
        <h2 id="choose-cache-title">1. Choose a run</h2>
        <p>
          Pick a recent successful run that a push started. Zoomies reads the workflow and the root
          lockfile at that commit, finds the pinned setup-node step, and shows the one cache line it
          would add. Nothing is saved at this step and no model sees the file.
        </p>
        {#if chosen && !inspection && !busy}
          <p>
            Ready to read <strong>{chosen.label}</strong> in {chosen.repo}.
            <Button
              variant="primary"
              disabled={previewsOff || retryBlocked}
              reason={previewsOff ? 'Turn on workflow previews above first.' : undefined}
              onclick={() => chosen && void inspect(chosen)}>Read this workflow</Button
            >
          </p>
        {/if}
        <CheckoutRunPicker
          bind:repo
          disabled={busy || retryBlocked}
          actionsDisabled={previewsOff}
          onpick={pick}
        />
      </section>
    {:else}
      <p>Previewing a cache change needs an operator.</p>
    {/if}

    {#if busy}<p role="status">Working…</p>{/if}
    {#if error}<p role="alert" tabindex="-1" bind:this={alertElement}>{error}</p>{/if}
    {#if retryBlocked}<p role="status">
        Wait until {new Date(retryAt).toLocaleTimeString()} before another request. Retry manually; this
        page does not poll.
      </p>{/if}

    {#if inspection}
      <section class="card" aria-labelledby="review-cache-title">
        <h2 id="review-cache-title" tabindex="-1" bind:this={sourceHeading}>
          2. Review the cache change
        </h2>
        <p>
          Found the setup step <strong>{inspection.step_name}</strong> in job
          <strong>{inspection.job_name}</strong>, using
          <strong>{inspection.package_manager}</strong>
          and the root <code>{inspection.lockfile_path}</code>.
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
            <dt>Source digest</dt>
            <dd class="mono">{inspection.source_sha256}</dd>
          </div>
          <div>
            <dt>Lockfile digest</dt>
            <dd class="mono">{inspection.lockfile_sha256}</dd>
          </div>
          <div>
            <dt>Setup step averaged</dt>
            <dd>
              {checkoutSeconds(inspection.setup_ms)} over {inspection.runs} runs (descriptive, not a saving)
            </dd>
          </div>
        </dl>
        <div
          class="diff"
          role="group"
          aria-label="The setup block this preview changes, untrusted text"
        >
          <pre class="removed"><span aria-hidden="true">- </span><span class="sr"
              >Removed: </span>{inspection.before.replace(/\n$/, '')}</pre>
          <pre class="added"><span aria-hidden="true">+ </span><span class="sr"
              >Added: </span>{inspection.after.replace(/\n$/, '')}</pre>
        </div>
        <p>
          The cache is the setup action's own package cache, shared with whoever can write this
          repository's cache. The timing is one descriptive figure, and passing CI alone cannot
          establish a saving.
        </p>
        <Checkbox
          bind:checked={reviewed}
          label="I reviewed this setup block and the repository's tooling and downstream consumers can use the package cache."
          disabled={busy}
        />
        <Button
          onclick={retain}
          loading={busy && !!inspection && reviewed}
          disabled={busy || retryBlocked || !reviewed}>Keep a preview for 14 days</Button
        >
      </section>
    {/if}

    {#if preview}
      {#if canCapture && preview.approval && !preview.publication}
        <PublicationGate bind:enablement refreshKey={gateRefresh} />
      {/if}
      <CachePreviewCard
        {preview}
        canApprove={canCapture}
        approving={busy}
        onapprove={() => void approve()}
        canPublish={canCapture && enablement?.publish_enabled === true}
        publishing={busy}
        onpublish={() => void publish()}
      />
    {/if}

    {#if canCapture}
      <details class="more card">
        <summary>Open a kept cache preview</summary>
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
                  placeholder="opc_..."
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
  .fields {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 14rem), 1fr));
    gap: var(--z-space-3);
  }
  .more {
    display: grid;
    gap: var(--z-space-6);
    padding: var(--z-space-6);
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
</style>
