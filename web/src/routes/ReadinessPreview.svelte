<script lang="ts">
  import { onDestroy } from 'svelte';
  import { ApiError, inspectReadinessPreview } from '$lib/api/client';
  import type { Job, OptimisationEnablement, ReadinessInspection } from '$lib/api/types';
  import { checkoutSeconds } from '$lib/optimisations/input';
  import { session } from '$lib/state/session.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Button from '$lib/components/Button.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import CheckoutRunPicker from '$lib/optimisations/CheckoutRunPicker.svelte';
  import OptimisationGate from '$lib/optimisations/OptimisationGate.svelte';

  let chosen = $state<{
    installation: string;
    repo: string;
    run_id: string;
    attempt: number;
    github_job_id: string;
    label: string;
  } | null>(null);
  let repo = $state('');
  let condition = $state('');
  let enablement = $state<OptimisationEnablement | undefined>();
  let gateRefresh = $state(0);
  const previewsOff = $derived(enablement !== undefined && !enablement.enabled);
  let inspection = $state<ReadinessInspection | null>(null);
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
  const readyCondition = $derived(
    condition.trim().length > 0 && condition.length <= 1024 && !/[\r\n\0]/.test(condition),
  );
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
  }
  async function inspect() {
    if (busy || retryBlocked || !canCapture || !chosen || !readyCondition) return;
    const current = begin();
    inspection = null;
    try {
      const result = await inspectReadinessPreview(
        {
          installation_id: chosen.installation,
          repository: chosen.repo,
          selection: {
            run_id: chosen.run_id,
            attempt: chosen.attempt,
            github_job_id: chosen.github_job_id,
          },
          condition: condition.trim(),
        },
        controller?.signal,
      );
      if (current !== operation) return;
      inspection = result;
      queueMicrotask(() => reviewHeading?.focus());
    } catch (e) {
      if (current === operation) failed(e);
    } finally {
      if (current === operation) busy = false;
    }
  }
  function cleanup() {
    operation++;
    controller?.abort();
    clearTimeout(timer);
  }
  onDestroy(cleanup);
</script>

<PageHeader
  title="Readiness preview"
  subtitle="Replace a fixed sleep with a bounded wait for a command you supply. A preview changes nothing on GitHub."
  ><Button href="/optimisations/cache" variant="secondary">Cache preview</Button><Button
    href="/optimisations/checkout"
    variant="secondary">Checkout preview</Button
  ></PageHeader
>
{#if session.visitor}
  <ErrorState
    title="Sign in to preview a readiness change"
    description="Previewing a workflow's wait needs an operator."
  />
{:else}
  <div class="page">
    {#if canCapture}
      <OptimisationGate bind:enablement refreshKey={gateRefresh} />
      <section class="card" aria-labelledby="choose-readiness-title">
        <h2 id="choose-readiness-title">1. Choose a run and supply the check</h2>
        <p>
          Pick a recent successful run that a push started. Zoomies reads the workflow at that
          commit and finds its one literal <code>sleep</code> step on an Ubuntu runner. The readiness
          command is yours: Zoomies never guesses a URL, a port or a command from the file.
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
            <Field
              label="Readiness command"
              required
              hint="One line. It is run repeatedly until it succeeds, then the step continues."
            >
              {#snippet children(f)}
                <Input
                  {...f}
                  bind:value={condition}
                  disabled={busy}
                  placeholder="curl -fsS http://127.0.0.1:8080/health"
                />
              {/snippet}
            </Field>
            <Button
              variant="primary"
              disabled={busy || retryBlocked || previewsOff || !readyCondition}
              reason={previewsOff
                ? 'Turn on workflow previews above first.'
                : !readyCondition
                  ? 'Supply one readiness command first.'
                  : undefined}
              onclick={inspect}>Read this workflow</Button
            >
          </div>
        {/if}
      </section>
    {:else}
      <p>Previewing a readiness change needs an operator.</p>
    {/if}

    {#if busy}<p role="status">Working…</p>{/if}
    {#if error}<p role="alert" tabindex="-1" bind:this={alertElement}>{error}</p>{/if}
    {#if retryBlocked}<p role="status">
        Wait until {new Date(retryAt).toLocaleTimeString()} before another request. Retry manually; this
        page does not poll.
      </p>{/if}

    {#if inspection}
      <section class="card" aria-labelledby="review-readiness-title">
        <h2 id="review-readiness-title" tabindex="-1" bind:this={reviewHeading}>
          2. Review the bounded wait
        </h2>
        <p>
          Found the wait step <strong>{inspection.step_name}</strong> in job
          <strong>{inspection.job_name}</strong>. The old fixed wait was
          <strong>{inspection.wait_seconds}s</strong>; the new poll is bounded to the same time and
          fails if the command never succeeds.
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
            <dt>Readiness command</dt>
            <dd class="mono">{inspection.condition}</dd>
          </div>
          <div>
            <dt>Wait step averaged</dt>
            <dd>
              {checkoutSeconds(inspection.wait_ms)} over {inspection.runs} runs (descriptive, not a saving)
            </dd>
          </div>
        </dl>
        <div
          class="diff"
          role="group"
          aria-label="The run block this preview changes, untrusted text"
        >
          <pre class="removed"><span aria-hidden="true">- </span><span class="sr"
              >Removed: </span>{inspection.before.replace(/\n$/, '')}</pre>
          <pre class="added"><span aria-hidden="true">+ </span><span class="sr"
              >Added: </span>{inspection.after.replace(/\n$/, '')}</pre>
        </div>
        <p>
          The condition is operator-supplied, not inferred. The old wait's duration is the bound, so
          a service that never becomes ready fails where the old wait stopped waiting. Passing CI
          alone cannot establish a saving.
        </p>
        <p>
          Nothing was retained, approved or published. This page is an inspection only; it does not
          open a pull request.
        </p>
      </section>
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
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
</style>
