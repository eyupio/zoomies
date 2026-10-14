<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import {
    ApiError,
    getOptimisationPolicy,
    inspectOptimisation,
    captureOptimisation,
    getOptimisation,
  } from '$lib/api/client';
  import type { OptimisationPolicy, OptimisationSelection, OptimisationView } from '$lib/api/types';
  import { router } from '$lib/router';
  import { session } from '$lib/state/session.svelte';
  import PageHeader from '$lib/components/PageHeader.svelte';
  import Field from '$lib/components/Field.svelte';
  import Input from '$lib/components/Input.svelte';
  import Button from '$lib/components/Button.svelte';

  let installation = $state(''),
    repo = $state(''),
    run = $state(''),
    job = $state('');
  let attempt = $state('1'),
    jobIndex = $state('0'),
    stepIndex = $state('0'),
    id = $state('');
  let policy = $state<OptimisationPolicy | null>(null);
  let inspected = $state<OptimisationView | null>(null),
    saved = $state<OptimisationView | null>(null);
  let reviewed = $state(false),
    busy = $state(false),
    error = $state(''),
    retryBlocked = $state(false);
  let inspectedKey = $state('');
  let dead = false,
    timer: ReturnType<typeof setTimeout> | undefined;
  const allowed = $derived(session.can('operator') && !session.visitor);
  const key = $derived(
    JSON.stringify([installation, repo, run, job, attempt, jobIndex, stepIndex]),
  );
  $effect(() => {
    if (inspectedKey && key !== inspectedKey) {
      inspected = null;
      reviewed = false;
    }
  });
  onDestroy(() => {
    dead = true;
    clearTimeout(timer);
  });
  function selection(): OptimisationSelection {
    if (
      !/^ins_[a-zA-Z0-9_-]+$/.test(installation) ||
      installation.length > 128 ||
      !/^[^\s/\\?#%]+\/[^\s/\\?#%]+$/.test(repo) ||
      repo.length > 240
    )
      throw new Error('Supply an installation ID and repository OWNER/REPO.');
    for (const value of [run, job])
      if (!/^[1-9][0-9]{0,18}$/.test(value) || BigInt(value) > 9223372036854775807n)
        throw new Error('Run and job IDs must be exact positive decimal integers.');
    const values = [attempt, jobIndex, stepIndex].map(Number);
    if (
      !values.every(Number.isSafeInteger) ||
      values[0]! < 1 ||
      values[0]! > 2147483647 ||
      values[1]! < 0 ||
      values[2]! < 0 ||
      values[1]! > 20000 ||
      values[2]! > 20000 ||
      [attempt, jobIndex, stepIndex].some((v) => !/^(0|[1-9][0-9]*)$/.test(v))
    )
      throw new Error('Supply a positive attempt and source indices from zero.');
    return {
      installation_id: installation,
      repository: repo,
      run_id: run,
      github_job_id: job,
      attempt: values[0]!,
      job_index: values[1]!,
      step_index: values[2]!,
    };
  }
  function supported(view: OptimisationView) {
    if (
      view.version !== 'checkout-web-v1' ||
      view.verification !== 'inconclusive' ||
      view.currentness !== 'not_checked'
    )
      throw new Error(
        'Unsupported preview version or verification state. Inspect with a matching client.',
      );
    return view;
  }
  async function action(kind: 'inspect' | 'capture' | 'read') {
    if (busy || retryBlocked || !allowed) return;
    busy = true;
    error = '';
    try {
      if (kind === 'read') {
        saved = null;
        inspected = null;
        reviewed = false;
        if (!/^opv_[a-zA-Z0-9_-]+$/.test(id) || id.length > 128 || !installation || !repo)
          throw new Error('Supply a preview ID, installation and repository.');
        const result = supported(await getOptimisation(id, installation, repo));
        if (!dead) saved = result;
      } else {
        const input = selection();
        if (kind === 'inspect') {
          inspected = null;
          saved = null;
          reviewed = false;
          const result = supported(await inspectOptimisation(input));
          if (!dead) {
            inspected = result;
            inspectedKey = key;
          }
        } else {
          if (!inspected || !reviewed || inspectedKey !== key)
            throw new Error('Inspect this selection and review its history needs first.');
          const result = supported(
            await captureOptimisation({
              ...input,
              reviewed_head_sha: inspected.head_sha,
              reviewed_source_sha256: inspected.source_sha256,
              history_not_required: true,
            }),
          );
          if (!dead) {
            saved = result;
            id = result.id;
            router.setQuery({ id, installation_id: installation, repo });
          }
        }
      }
    } catch (e) {
      if (!dead) {
        error = e instanceof Error ? e.message : 'Preview unavailable.';
        if (e instanceof ApiError && e.status === 429) {
          retryBlocked = true;
          clearTimeout(timer);
          timer = setTimeout(
            () => {
              retryBlocked = false;
            },
            Math.max(0, (e.retryAt ?? Date.now() + 60000) - Date.now()),
          );
        }
      }
    } finally {
      if (!dead) busy = false;
    }
  }
  onMount(async () => {
    if (!allowed) return;
    const query = new URLSearchParams(location.search);
    installation = query.get('installation_id') ?? '';
    repo = query.get('repo') ?? '';
    id = query.get('id') ?? '';
    try {
      const result = await getOptimisationPolicy();
      if (!dead) {
        policy = result;
        if (id && policy.history_enabled) await action('read');
      }
    } catch (e) {
      if (!dead) error = e instanceof Error ? e.message : 'Policy unavailable.';
    }
  });
  const view = $derived(saved ?? inspected);
</script>

<PageHeader
  title="Optimisation previews"
  subtitle="Inspect checkout history needs and save an exact change for review."
/>
<div class="content">
  {#if !allowed}
    <p>An identified operator is required to inspect or read private workflow source.</p>
  {:else}
    <p>
      GitHub reads use the selected installation. This deterministic preview does not call a model.
    </p>
    {#if policy && (!policy.source_enabled || !policy.preview_enabled || !policy.history_enabled)}
      <p>
        Administrator-managed controls in Settings: source inspection {policy.source_enabled
          ? 'enabled'
          : 'disabled'}, saving previews {policy.preview_enabled ? 'enabled' : 'disabled'}, retained
        history {policy.history_enabled ? 'enabled' : 'disabled'}.
      </p>
    {/if}
    <form
      onsubmit={(e) => {
        e.preventDefault();
        void action('inspect');
      }}
    >
      <div class="fields">
        <Field label="Installation ID" id="op-install"
          ><Input id="op-install" bind:value={installation} disabled={busy} /></Field
        >
        <Field label="Repository" id="op-repo"
          ><Input id="op-repo" bind:value={repo} disabled={busy} placeholder="OWNER/REPO" /></Field
        >
        <Field label="Run ID" id="op-run"
          ><Input id="op-run" bind:value={run} disabled={busy} inputmode="numeric" /></Field
        >
        <Field label="GitHub job ID" id="op-job"
          ><Input id="op-job" bind:value={job} disabled={busy} inputmode="numeric" /></Field
        >
        <Field label="Attempt" id="op-attempt"
          ><Input id="op-attempt" bind:value={attempt} disabled={busy} inputmode="numeric" /></Field
        >
        <Field label="Job index (from zero)" id="op-job-index"
          ><Input
            id="op-job-index"
            bind:value={jobIndex}
            disabled={busy}
            inputmode="numeric"
          /></Field
        >
        <Field label="Step index (from zero)" id="op-step-index"
          ><Input
            id="op-step-index"
            bind:value={stepIndex}
            disabled={busy}
            inputmode="numeric"
          /></Field
        >
      </div>
      <p>
        Choose a successful push checkout with explicit unique job and step names. Matrix, generated
        or ambiguous names remain unsupported. API step numbers are separate from source indices.
      </p>
      <Button type="submit" disabled={busy || retryBlocked || !policy?.source_enabled}
        >Inspect checkout</Button
      >
    </form>
    <form
      onsubmit={(e) => {
        e.preventDefault();
        void action('read');
      }}
    >
      <Field label="Retained preview ID" id="op-id"
        ><Input id="op-id" bind:value={id} disabled={busy} placeholder="opv_…" /></Field
      >
      <Button type="submit" disabled={busy || retryBlocked || !policy?.history_enabled}
        >Read retained preview</Button
      >
    </form>
    {#if retryBlocked}<p role="status">
        Rate-limited. Wait for Retry-After, then retry manually.
      </p>{/if}
    {#if error}<p role="alert">{error}</p>{/if}
    {#if view}
      <section aria-label="Checkout preview evidence">
        <h2>{saved ? 'Saved checkout preview' : 'Checkout inspection'}</h2>
        <p>
          Performance verification: Inconclusive. Currentness has not been checked. Passing CI alone
          does not demonstrate savings.
        </p>
        <p>
          Push source and unique name matching are associated evidence, not attestation of executed
          bytes.
        </p>
        <dl>
          <dt>Run / attempt</dt>
          <dd>{view.run_id} / {view.attempt}</dd>
          <dt>GitHub job / API step</dt>
          <dd>{view.github_job_id ?? 'Unknown'} / {view.step_number ?? 'Unknown'}</dd>
          <dt>Checkout duration</dt>
          <dd>{view.checkout_ms} ms</dd>
          <dt>Commit</dt>
          <dd>{view.head_sha}</dd>
          <dt>Reviewed source SHA-256</dt>
          <dd>{view.source_sha256}</dd>
          {#if view.after_sha256}<dt>Proposed source SHA-256</dt>
            <dd>{view.after_sha256}</dd>{/if}
          <dt>File / mode</dt>
          <dd>{view.path} / {view.mode}</dd>
          {#if saved}<dt>Review actor</dt>
            <dd>{view.reviewed_by ?? 'Unknown'}</dd>{/if}
        </dl>
        <details>
          <summary
            >{saved ? 'Proposed workflow source' : 'Review inspected workflow source'}</summary
          >
          <pre>{view.content}</pre>
        </details>
        {#if saved}
          <h3>Exact change</h3>
          <pre aria-label="Before change">{view.before ?? 'Unknown'}</pre>
          <pre aria-label="After change">{view.after ?? 'Unknown'}</pre>
          <p>Saved for review. Repository publication remains a later action.</p>
          <p>Bookmark this address to replay the same retained evidence.</p>
        {:else}
          <label class="review"
            ><input type="checkbox" bind:checked={reviewed} disabled={busy} /> I reviewed repository tooling
            and downstream consumers; full history and tags are not required.</label
          >
          <p>This is your declaration of history needs, not an independently verified fact.</p>
          <Button
            onclick={() => action('capture')}
            disabled={busy || retryBlocked || !reviewed || !policy?.preview_enabled}
            >Save checkout preview</Button
          >
        {/if}
      </section>
    {/if}
  {/if}
</div>

<style>
  .content {
    max-width: var(--z-content-max);
    padding: var(--z-space-6);
  }
  form,
  section {
    margin-block: var(--z-space-6);
  }
  .fields {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 16rem), 1fr));
    gap: var(--z-space-4);
  }
  p,
  details,
  h2,
  h3 {
    margin-block: var(--z-space-3);
  }
  dl {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 2fr);
    gap: var(--z-space-2);
  }
  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  pre {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    max-width: 100%;
    padding: var(--z-space-3);
    background: var(--z-surface-sunken);
  }
  .review {
    display: flex;
    align-items: start;
    gap: var(--z-space-2);
    margin-block: var(--z-space-4);
  }
</style>
