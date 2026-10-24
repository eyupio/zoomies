<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import {
    inspectCheckout,
    retainCheckoutPreview,
    getCheckoutPreview,
    approveCheckoutPreview,
    publishCheckoutPreview,
    ApiError,
  } from '$lib/api/client';
  import type {
    CheckoutInspection,
    CheckoutPreview,
    CheckoutSelection,
    Job,
    OptimisationEnablement,
  } from '$lib/api/types';
  import { parseCheckoutSelection, checkoutSeconds } from '$lib/optimisations/input';
  import { router } from '$lib/router';
  import { session } from '$lib/state/session.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import Button from '$lib/components/Button.svelte';
  import Checkbox from '$lib/components/Checkbox.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import CheckoutPreviewCard from '$lib/optimisations/CheckoutPreviewCard.svelte';
  import CheckoutRunPicker from '$lib/optimisations/CheckoutRunPicker.svelte';
  import OptimisationGate from '$lib/optimisations/OptimisationGate.svelte';
  import PublicationGate from '$lib/optimisations/PublicationGate.svelte';

  // What was chosen: always an installation, a repository and one exact job.
  let chosen = $state<{
    installation: string;
    repo: string;
    selection: CheckoutSelection;
    label: string;
  } | null>(null);
  let repo = $state('');
  let manualInstallation = $state(''),
    manualRepo = $state(''),
    runId = $state(''),
    attempt = $state('1'),
    jobId = $state('');
  let previewId = $state(''),
    openInstallation = $state(''),
    openRepo = $state('');
  let enablement = $state<OptimisationEnablement | undefined>();
  let gateRefresh = $state(0);
  // Unknown is not off: until the state is read the page stays usable and the
  // server's own refusal, which names the setting, is the backstop.
  const previewsOff = $derived(enablement !== undefined && !enablement.enabled);
  let inspection = $state<CheckoutInspection | null>(null);
  let reviewed = $state(false);
  let preview = $state<CheckoutPreview | null>(null);
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
      const result = await inspectCheckout(
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
  function manual(event: SubmitEvent) {
    event.preventDefault();
    try {
      if (!/^ins_[a-zA-Z0-9_-]{1,124}$/.test(manualInstallation) || !validRepo(manualRepo))
        throw new Error('Supply an installation ID starting ins_ and repository OWNER/REPO.');
      const selection = parseCheckoutSelection({ runId, attempt, jobId });
      void inspect({
        installation: manualInstallation,
        repo: manualRepo,
        selection,
        label: `Run ${selection.run_id}, job ${selection.github_job_id}`,
      });
    } catch (e) {
      failed(e);
    }
  }
  async function retain() {
    if (busy || retryBlocked || !canCapture || !inspection || !reviewed || !chosen) return;
    const current = begin();
    try {
      const result = await retainCheckoutPreview(
        {
          installation_id: chosen.installation,
          repository: chosen.repo,
          // The positions the server found and the reviewer was shown, so the
          // preview is of exactly that step.
          selection: inspection.selection,
          head_sha: inspection.head_sha,
          source_sha256: inspection.source_sha256,
          history_reviewed: true,
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
    if (busy || !preview || !canCapture) return;
    const current = begin();
    try {
      const result = await approveCheckoutPreview(
        preview.id,
        {
          installation_id: preview.installation_id,
          repository: preview.repository,
          head_sha: preview.preview.head_sha,
          source_sha256: preview.preview.source_sha256,
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
    if (busy || !preview || !canCapture) return;
    const current = begin();
    try {
      const result = await publishCheckoutPreview(
        preview.id,
        {
          installation_id: preview.installation_id,
          repository: preview.repository,
          head_sha: preview.preview.head_sha,
          source_sha256: preview.preview.source_sha256,
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
      if (!/^opv_[a-zA-Z0-9_-]{1,124}$/.test(readId))
        throw new Error('Supply a preview ID starting opv_.');
      const result = await getCheckoutPreview(readId, inst, repository, controller?.signal);
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
  // A link from a job, or to a kept preview. A job link prefills the run and
  // waits for a press: reading source spends GitHub requests, so nothing is
  // read just because a page opened.
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
  title="Checkout preview"
  subtitle="See whether a workflow fetches history it does not need. A preview changes nothing on GitHub."
  ><Button href="/optimisations/cache" variant="secondary">Cache preview</Button><Button
    href="/optimisations/sharding"
    variant="secondary">Sharding preview</Button
  ><Button href="/comparisons" variant="secondary">Compare runs</Button></PageHeader
>
{#if session.visitor}
  <ErrorState
    title="Sign in to read previews"
    description="Previews contain a repository's workflow lines. Reading and creating them need an operator."
  />
{:else}
  <div class="page">
    {#if canCapture}
      <OptimisationGate bind:enablement refreshKey={gateRefresh} />
      <section class="card" aria-labelledby="choose-title">
        <h2 id="choose-title">1. Choose a run</h2>
        <p>
          Pick a recent successful run that a push started. Zoomies reads the workflow file at that
          push's commit, finds the full-history checkout in the job you choose, and shows it to you
          before anything is kept. Nothing is saved at this step and no model sees the file.
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
        <details class="more">
          <summary>Enter a run by its IDs instead</summary>
          <form onsubmit={manual}>
            <div class="fields">
              <Field label="Installation ID" required
                >{#snippet children(f)}<Input
                    {...f}
                    bind:value={manualInstallation}
                    disabled={busy}
                    required
                    placeholder="ins_..."
                  />{/snippet}</Field
              >
              <Field label="Repository" required
                >{#snippet children(f)}<Input
                    {...f}
                    bind:value={manualRepo}
                    disabled={busy}
                    required
                    placeholder="OWNER/REPO"
                  />{/snippet}</Field
              >
              <Field label="Run ID" required hint="The GitHub run ID, exactly as GitHub shows it."
                >{#snippet children(f)}<Input
                    {...f}
                    bind:value={runId}
                    disabled={busy}
                    required
                    inputmode="numeric"
                    mono
                  />{/snippet}</Field
              >
              <Field label="Run attempt" required
                >{#snippet children(f)}<Input
                    {...f}
                    bind:value={attempt}
                    disabled={busy}
                    required
                    inputmode="numeric"
                  />{/snippet}</Field
              >
              <Field label="GitHub job ID" required
                >{#snippet children(f)}<Input
                    {...f}
                    bind:value={jobId}
                    disabled={busy}
                    required
                    inputmode="numeric"
                    mono
                  />{/snippet}</Field
              >
            </div>
            <Button
              type="submit"
              variant="secondary"
              disabled={busy || retryBlocked || previewsOff}
              reason={previewsOff ? 'Turn on workflow previews above first.' : undefined}
              >Read workflow source</Button
            >
          </form>
        </details>
      </section>
    {:else}
      <p>Creating, opening and approving a preview needs an operator.</p>
    {/if}

    {#if busy}<p role="status">Working…</p>{/if}
    {#if error}<p role="alert" tabindex="-1" bind:this={alertElement}>{error}</p>{/if}
    {#if retryBlocked}<p role="status">
        Wait until {new Date(retryAt).toLocaleTimeString()} before another request. Retry manually; this
        page does not poll.
      </p>{/if}

    {#if inspection}
      <section class="card" aria-labelledby="review-title">
        <h2 id="review-title" tabindex="-1" bind:this={sourceHeading}>2. Review the source</h2>
        <p>
          Found the checkout step <strong>{inspection.step_name}</strong> in job
          <strong>{inspection.job_name}</strong>. It fetches the full history of the repository.
        </p>
        <dl class="facts">
          <div>
            <dt>File</dt>
            <dd>{inspection.path}</dd>
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
            <dt>This checkout took</dt>
            <dd>
              {checkoutSeconds(inspection.checkout_ms)} in this run (a measurement, not a saving)
            </dd>
          </div>
        </dl>
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <pre
          class="source"
          tabindex="0"
          aria-label="Workflow source, untrusted text">{inspection.content}</pre>
        <p>
          The preview would change one line: <code>fetch-depth: 0</code> to
          <code>fetch-depth: 1</code> on this checkout. Anything that reads tags, runs
          <code>git describe</code>, diffs against older commits, or is consumed by another
          repository's tooling may need the full history.
        </p>
        <Checkbox
          bind:checked={reviewed}
          label="I reviewed this file. Neither this repository's tooling nor its downstream consumers need full history."
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
      <CheckoutPreviewCard
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
        <summary>Open a kept preview</summary>
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
                  required
                  placeholder="ins_..."
                />{/snippet}</Field
            >
            <Field label="Repository" required
              >{#snippet children(f)}<Input
                  {...f}
                  bind:value={openRepo}
                  disabled={busy}
                  required
                  placeholder="OWNER/REPO"
                />{/snippet}</Field
            >
            <Field label="Preview ID" required
              >{#snippet children(f)}<Input
                  {...f}
                  bind:value={previewId}
                  disabled={busy}
                  required
                  placeholder="opv_..."
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
  .fields {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 14rem), 1fr));
    gap: var(--z-space-4);
    margin-block: var(--z-space-4);
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
  .more {
    min-width: 0;
  }
  summary {
    cursor: pointer;
    font-weight: var(--z-weight-semibold);
  }
  .mono {
    font-family: var(--z-font-mono);
  }
  .source {
    font-family: var(--z-font-mono);
    font-size: var(--z-text-sm);
    overflow: auto;
    max-height: 24rem;
    padding: var(--z-space-4);
    margin-block: var(--z-space-4);
    background: var(--z-surface-sunken);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    white-space: pre;
  }
</style>
