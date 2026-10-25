<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import {
    ApiError,
    inspectKennelFix,
    retainKennelFix,
    approveKennelFix,
    publishKennelFix,
    getKennelFix,
  } from '$lib/api/client';
  import type {
    KennelFixInspection,
    KennelFixPreview,
    OptimisationEnablement,
  } from '$lib/api/types';
  import { session } from '$lib/state/session.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Button from '$lib/components/Button.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import Checkbox from '$lib/components/Checkbox.svelte';
  import OptimisationGate from '$lib/optimisations/OptimisationGate.svelte';
  import PublicationGate from '$lib/optimisations/PublicationGate.svelte';
  import KennelShell from '$lib/kennel/KennelShell.svelte';
  import KennelFixCard from '$lib/kennel/KennelFixCard.svelte';
  import { appendedLines, timeoutMinutes, timeoutMinutesHint } from '$lib/kennel/words';
  import { router } from '$lib/router';

  // The finding this fix is for, read from the address: the blob names the file,
  // the job is its position in the file, and the repository is Kennel Club's.
  const repositoryId = $derived(router.param('repository') ?? '');
  const blob = $derived(router.param('blob') ?? '');
  const job = $derived(router.param('job') ?? '');
  const jobIndex = $derived(/^[0-9]{1,4}$/.test(job) ? Number(job) : -1);
  const named = $derived(
    /^kcr_[a-zA-Z0-9_-]{1,124}$/.test(repositoryId) && /^[a-f0-9]{40}$/.test(blob) && jobIndex >= 0,
  );

  let minutes = $state('');
  let enablement = $state<OptimisationEnablement | undefined>();
  let gateRefresh = $state(0);
  const previewsOff = $derived(enablement !== undefined && !enablement.enabled);
  let inspection = $state<KennelFixInspection | null>(null);
  let reviewed = $state(false);
  let preview = $state<KennelFixPreview | null>(null);
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
  const chosenMinutes = $derived(timeoutMinutes(minutes));
  const hint = $derived(timeoutMinutesHint(minutes));
  const diff = $derived(inspection ? appendedLines(inspection.before, inspection.after) : null);
  $effect(() => {
    if (error) alertElement?.focus();
  });

  function failed(e: unknown) {
    error =
      e instanceof ApiError && e.isNotFound
        ? 'This fix is unavailable. The repository or the finding may be outside this scope.'
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
  async function inspect() {
    if (busy || retryBlocked || !canCapture || !named || chosenMinutes === null) return;
    const current = begin();
    inspection = null;
    try {
      const result = await inspectKennelFix(
        {
          kennel_repository_id: repositoryId,
          blob_sha: blob,
          job_index: jobIndex,
          minutes: chosenMinutes,
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
      const result = await retainKennelFix(
        {
          kennel_repository_id: inspection.kennel_repository_id,
          blob_sha: inspection.blob_sha,
          job_index: inspection.job_index,
          minutes: inspection.minutes,
          head_sha: inspection.head_sha,
          source_sha256: inspection.source_sha256,
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
        repository: null,
        blob: null,
        job: null,
      });
      inspection = null;
      reviewed = false;
    } catch (e) {
      if (current === operation) failed(e);
    } finally {
      if (current === operation) busy = false;
    }
  }
  async function decide(action: typeof approveKennelFix | typeof publishKennelFix) {
    if (busy || retryBlocked || !canCapture || !preview) return;
    const current = begin();
    try {
      const result = await action(
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
      if (
        !/^ins_[a-zA-Z0-9_-]{1,124}$/.test(inst) ||
        !/^[^\s/?#]+\/[^\s/?#]+$/.test(repository) ||
        repository.length > 1024
      )
        throw new Error('Supply an installation ID starting ins_ and repository OWNER/REPO.');
      if (!/^kfp_[a-zA-Z0-9_-]{1,124}$/.test(readId))
        throw new Error('Supply a fix ID starting kfp_.');
      const result = await getKennelFix(readId, inst, repository, controller?.signal);
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

<KennelShell current="repositories">
  <PageHeader
    title="Timeout fix"
    subtitle="Give a job that has no timeout one, with the minutes you choose. A preview changes nothing on GitHub."
  >
    {#if named}
      <Button href="/kennel/repositories/{encodeURIComponent(repositoryId)}" variant="secondary"
        >Back to the repository</Button
      >
    {/if}
  </PageHeader>
  {#if session.visitor}
    <ErrorState
      title="Sign in to preview a fix"
      description="Previewing a change to a workflow needs an operator."
    />
  {:else}
    <div class="page">
      {#if canCapture}
        <OptimisationGate bind:enablement refreshKey={gateRefresh} />
        {#if named}
          <section class="card" aria-labelledby="choose-timeout-title">
            <h2 id="choose-timeout-title">1. Choose the timeout</h2>
            <p>
              Zoomies reads the workflow file this finding names, at the head of the default branch,
              and finds the job. The number of minutes is yours: nothing in the file says how long
              the job should take, and too small a value fails a healthy run.
            </p>
            <div class="ready">
              <Field
                label="Minutes the job may run"
                required
                hint={hint || 'A whole number of minutes from 1 to 1440.'}
              >
                {#snippet children(f)}
                  <Input
                    {...f}
                    bind:value={minutes}
                    disabled={busy}
                    inputmode="numeric"
                    placeholder="30"
                  />
                {/snippet}
              </Field>
              <Button
                variant="primary"
                disabled={busy || retryBlocked || previewsOff || chosenMinutes === null}
                reason={previewsOff
                  ? 'Turn on workflow previews above first.'
                  : chosenMinutes === null
                    ? hint
                    : undefined}
                onclick={inspect}>Read this workflow</Button
              >
            </div>
          </section>
        {:else if !preview}
          <section class="card">
            <h2>Open a fix from a finding</h2>
            <p>
              A timeout fix starts from a <code>ci.no_timeout</code> finding on a repository's page,
              where each job without a timeout has a <strong>Preview a fix</strong> button. Or open a
              fix you kept, below.
            </p>
          </section>
        {/if}
      {:else}
        <p>Previewing a fix needs an operator.</p>
      {/if}

      {#if busy}<p role="status">Working…</p>{/if}
      {#if error}<p role="alert" tabindex="-1" bind:this={alertElement}>{error}</p>{/if}
      {#if retryBlocked}<p role="status">
          Wait until {new Date(retryAt).toLocaleTimeString()} before another request. Retry manually;
          this page does not poll.
        </p>{/if}

      {#if inspection}
        <section class="card" aria-labelledby="review-timeout-title">
          <h2 id="review-timeout-title" tabindex="-1" bind:this={reviewHeading}>
            2. Review the change
          </h2>
          <p>
            Job <strong>{inspection.job_name}</strong> has no <code>timeout-minutes</code>. The fix
            adds
            <strong>{inspection.minutes} minutes</strong>.
          </p>
          <dl class="facts">
            <div>
              <dt>File</dt>
              <dd>{inspection.workflow_path}, line {inspection.line}</dd>
            </div>
            <div>
              <dt>Read at</dt>
              <dd>
                <span class="mono">{inspection.head_sha}</span> on
                <span class="mono">{inspection.branch}</span>
              </dd>
            </div>
          </dl>
          <div class="diff" role="group" aria-label="The change to this job, untrusted text">
            {#if diff}
              {#each diff.context as line, i (i)}
                <pre class="context"><span aria-hidden="true"></span><span class="sr"
                    >Unchanged: </span>{line}</pre>
              {/each}
              {#each diff.added as line, i (i)}
                <pre class="added"><span aria-hidden="true">+ </span><span class="sr"
                    >Added: </span>{line}</pre>
              {/each}
            {:else}
              <pre class="removed"><span aria-hidden="true">- </span><span class="sr"
                  >Removed: </span>{inspection.before.replace(/\n$/, '')}</pre>
              <pre class="added"><span aria-hidden="true">+ </span><span class="sr"
                  >Added: </span>{inspection.after.replace(/\n$/, '')}</pre>
            {/if}
          </div>
          <p>
            The minutes are yours, not inferred. A job that needs longer will be stopped at this
            limit. The finding closes when Kennel Club next reads this file on the default branch
            and the job has a timeout, not when a pull request is opened.
          </p>
          <Checkbox
            bind:checked={reviewed}
            label="I reviewed this change and {inspection.minutes} minutes is enough for a healthy run of this job."
            disabled={busy}
          />
          <Button
            onclick={retain}
            loading={busy && !!inspection && reviewed}
            disabled={busy || retryBlocked || !reviewed}>Keep a fix for 14 days</Button
          >
          <p>
            Keeping retains the exact file and change privately for fourteen days. It approves and
            publishes nothing and does not open a pull request.
          </p>
        </section>
      {/if}

      {#if preview}
        {#if canCapture && preview.approval && !preview.publication}
          <PublicationGate bind:enablement refreshKey={gateRefresh} />
        {/if}
        <KennelFixCard
          {preview}
          canApprove={canCapture}
          approving={busy}
          onapprove={() => void decide(approveKennelFix)}
          canPublish={canCapture && enablement?.publish_enabled === true}
          publishing={busy}
          onpublish={() => void decide(publishKennelFix)}
        />
      {/if}

      {#if canCapture}
        <details class="more card">
          <summary>Open a kept fix</summary>
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
              <Field label="Fix ID" required
                >{#snippet children(f)}<Input
                    {...f}
                    bind:value={previewId}
                    disabled={busy}
                    placeholder="kfp_..."
                  />{/snippet}</Field
              >
            </div>
            <Button type="submit" variant="secondary" disabled={busy || retryBlocked}
              >Open fix</Button
            >
          </form>
        </details>
      {/if}
    </div>
  {/if}
</KennelShell>

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
    padding: var(--z-space-1) var(--z-space-4);
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
