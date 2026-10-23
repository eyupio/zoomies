<script lang="ts">
  import { onDestroy, untrack } from 'svelte';
  import { captureComparison, getComparison, ApiError } from '$lib/api/client';
  import type { RetainedComparison, ComparisonCaptureRequest } from '$lib/api/types';
  import { parseAttempts, validateSelection } from '$lib/comparisons/input';
  import { router } from '$lib/router';
  import { session } from '$lib/state/session.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import Textarea from '$lib/components/Textarea.svelte';
  import Select from '$lib/components/Select.svelte';
  import Button from '$lib/components/Button.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import ComparisonReport from '$lib/comparisons/ComparisonReport.svelte';

  let installation = $state(''),
    repo = $state(''),
    id = $state('');
  let kind = $state<ComparisonCaptureRequest['kind']>('runner');
  let hypothesis = $state(''),
    delta = $state(''),
    revision = $state('');
  let since = $state(new Date(Date.now() - 86400000).toISOString()),
    until = $state(new Date().toISOString());
  let baseline = $state(''),
    candidate = $state('');
  let report = $state<RetainedComparison | null>(null);
  let busy = $state(false),
    error = $state(''),
    retryAt = $state(0),
    retryBlocked = $state(false);
  let operation = 0,
    loadedKey = '';
  let alertElement = $state<HTMLParagraphElement | null>(null);
  $effect(() => {
    if (error) alertElement?.focus();
  });
  let controller: AbortController | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const canCapture = $derived(session.can('operator') && !session.visitor);
  function scope() {
    if (
      !/^ins_[a-zA-Z0-9_-]{1,124}$/.test(installation) ||
      !/^[^\s/\\?#]+\/[^\s/\\?#]+$/.test(repo) ||
      repo.length > 1024
    )
      throw new Error('Supply an installation ID starting ins_ and repository OWNER/REPO.');
  }
  function failed(e: unknown) {
    error =
      e instanceof ApiError && e.isNotFound
        ? 'This report is unavailable. It may be expired, evicted, deleted or outside this scope. No replacement was captured.'
        : e instanceof Error
          ? e.message
          : 'The comparison could not be read.';
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
  async function read(readId = id, inst = installation, repository = repo) {
    const current = ++operation;
    controller?.abort();
    controller = new AbortController();
    busy = true;
    error = '';
    report = null;
    try {
      installation = inst;
      repo = repository;
      id = readId;
      scope();
      if (!/^cmp_[a-zA-Z0-9_-]{1,124}$/.test(id))
        throw new Error('Supply a comparison ID starting cmp_.');
      const result = await getComparison(id, installation, repo, controller.signal);
      if (current !== operation) return;
      report = result;
      loadedKey = `${id}|${installation}|${repo}`;
      router.setQuery({ id, installation_id: installation, repo });
    } catch (e) {
      if (current === operation) failed(e);
    } finally {
      if (current === operation) busy = false;
    }
  }
  async function capture(event: SubmitEvent) {
    event.preventDefault();
    if (busy || retryBlocked || !canCapture) return;
    const current = ++operation;
    controller?.abort();
    busy = true;
    error = '';
    report = null;
    try {
      scope();
      const a = parseAttempts(baseline),
        b = parseAttempts(candidate);
      validateSelection(a, b);
      if (
        !hypothesis.trim() ||
        !delta.trim() ||
        new TextEncoder().encode(hypothesis).length > 4096 ||
        new TextEncoder().encode(delta).length > 4096
      )
        throw new Error('Supply a hypothesis and intended change within 4096 bytes each.');
      if (kind === 'workflow' && !revision.trim())
        throw new Error('Supply the retained exact workflow revision-delta reference.');
      if (
        !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(since) ||
        !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(until) ||
        !Number.isFinite(Date.parse(since)) ||
        !Number.isFinite(Date.parse(until)) ||
        Date.parse(until) <= Date.parse(since)
      )
        throw new Error('Supply an increasing window with valid timestamps.');
      const result = await captureComparison({
        installation_id: installation,
        repository: repo,
        kind,
        hypothesis,
        intended_delta: delta,
        revision_delta_id: kind === 'workflow' ? revision : '',
        since: new Date(since).toISOString(),
        until: new Date(until).toISOString(),
        baseline: a,
        candidate: b,
      });
      if (current !== operation) return;
      report = result;
      id = result.id;
      loadedKey = `${id}|${installation}|${repo}`;
      router.setQuery({ id, installation_id: installation, repo });
    } catch (e) {
      if (current === operation) failed(e);
    } finally {
      if (current === operation) busy = false;
    }
  }
  let prefilledKey = '';
  $effect(() => {
    const readId = router.param('id'),
      inst = router.param('installation_id'),
      repository = router.param('repo'),
      kindParam = router.param('kind'),
      revisionParam = router.param('revision');
    if (session.visitor) return;
    untrack(() => {
      if (readId && inst && repository) {
        if (`${readId}|${inst}|${repository}` !== loadedKey) void read(readId, inst, repository);
        return;
      }
      // A published checkout preview links here with the scope, kind and its
      // retained reference already chosen, so the operator only picks the runs.
      const key = `${inst}|${repository}|${kindParam}|${revisionParam}`;
      if (key === prefilledKey) return;
      prefilledKey = key;
      if (inst) installation = inst;
      if (repository) repo = repository;
      if (kindParam === 'runner' || kindParam === 'workflow') kind = kindParam;
      if (revisionParam) revision = revisionParam;
    });
  });
  onDestroy(() => {
    operation++;
    controller?.abort();
    clearTimeout(timer);
  });
</script>

<PageHeader
  title="Comparisons"
  subtitle="Inspect selected historical runs. Missing evidence stays inconclusive."
  ><Button href="/optimisations/checkout" variant="secondary">Checkout preview</Button><Button
    href="/jobs"
    variant="secondary">Job history</Button
  ></PageHeader
>
{#if session.visitor}
  <ErrorState
    title="Sign in to read comparisons"
    description="Retained evidence requires repository reader access. Capture requires an operator."
  />
{:else}
  <div class="comparison-page">
    <section class="comparison-card" aria-labelledby="scope-title">
      <h2 id="scope-title">Open retained evidence</h2>
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
                bind:value={installation}
                disabled={busy}
                required
                placeholder="ins_…"
              />{/snippet}</Field
          >
          <Field label="Repository" required
            >{#snippet children(f)}<Input
                {...f}
                bind:value={repo}
                disabled={busy}
                required
                placeholder="OWNER/REPO"
              />{/snippet}</Field
          >
          <Field label="Comparison ID"
            >{#snippet children(f)}<Input
                {...f}
                bind:value={id}
                disabled={busy}
                placeholder="cmp_…"
              />{/snippet}</Field
          >
        </div>
        <Button type="submit" variant="secondary" disabled={busy || retryBlocked}
          >Open report</Button
        >
      </form>
    </section>
    {#if canCapture}
      <section class="comparison-card" aria-labelledby="capture-title">
        <h2 id="capture-title">Capture a comparison</h2>
        <p>
          Capture reads selected GitHub attempts and saves shared evidence for repository readers.
          It does not run jobs or change runners. History is bounded and expires after 30 days.
        </p>
        <form onsubmit={capture}>
          <div class="fields">
            <Field label="Comparison kind"
              >{#snippet children(f)}<Select
                  {...f}
                  bind:value={kind}
                  options={[
                    { value: 'runner', label: 'Runner' },
                    { value: 'workflow', label: 'Workflow' },
                  ]}
                  disabled={busy}
                />{/snippet}</Field
            >
            <Field label="Hypothesis" required
              >{#snippet children(f)}<Textarea
                  {...f}
                  bind:value={hypothesis}
                  disabled={busy}
                  required
                  rows={2}
                />{/snippet}</Field
            >
            <Field label="Intended change" required
              >{#snippet children(f)}<Textarea
                  {...f}
                  bind:value={delta}
                  disabled={busy}
                  required
                  rows={2}
                />{/snippet}</Field
            >
            {#if kind === 'workflow'}<Field
                label="Workflow revision-delta reference"
                required
                hint="Reference to the retained exact revision change."
                >{#snippet children(f)}<Input
                    {...f}
                    bind:value={revision}
                    disabled={busy}
                    required
                  />{/snippet}</Field
              >{/if}
            <Field label="Window start (UTC)" required
              >{#snippet children(f)}<Input
                  {...f}
                  bind:value={since}
                  disabled={busy}
                  required
                />{/snippet}</Field
            >
            <Field
              label="Window end (UTC)"
              required
              hint="End is excluded. The configured history window applies."
              >{#snippet children(f)}<Input
                  {...f}
                  bind:value={until}
                  disabled={busy}
                  required
                />{/snippet}</Field
            >
            <Field
              label="Baseline run attempts"
              required
              hint="One GitHub run ID:attempt per line, for example 123456:1."
              >{#snippet children(f)}<Textarea
                  {...f}
                  bind:value={baseline}
                  disabled={busy}
                  required
                  rows={3}
                  mono
                />{/snippet}</Field
            >
            <Field
              label="Candidate run attempts"
              required
              hint="At most 10 unique attempts across both arms. Include failed attempts."
              >{#snippet children(f)}<Textarea
                  {...f}
                  bind:value={candidate}
                  disabled={busy}
                  required
                  rows={3}
                  mono
                />{/snippet}</Field
            >
          </div>
          <Button type="submit" loading={busy} disabled={busy || retryBlocked}
            >Capture and retain evidence</Button
          >
        </form>
      </section>
    {:else}<p>
        Reading retained evidence is available to viewers. Capturing new evidence requires an
        operator.
      </p>{/if}
    {#if busy}<p role="status">Reading comparison evidence…</p>{/if}
    {#if error}<p role="alert" tabindex="-1" bind:this={alertElement}>{error}</p>{/if}
    {#if retryBlocked}<p role="status">
        Wait until {new Date(retryAt).toLocaleTimeString()} before another request. Retry manually; this
        page does not poll.
      </p>{/if}
    {#if report}<ComparisonReport {report} />{/if}
  </div>
{/if}

<style>
  .comparison-page {
    display: grid;
    gap: var(--z-space-6);
  }
  .comparison-card {
    padding: var(--z-space-6);
    background: var(--z-surface);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-lg);
    min-width: 0;
  }
  .fields {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 20rem), 1fr));
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
</style>
