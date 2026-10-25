<!--
  A kept Kennel fix: the exact change, who kept it, and the two decisions that
  follow, approving it and publishing it as a draft pull request. Neither is
  made by being shown. The file's text is repository text and is drawn as text
  only; the value in the change is the operator's.
-->
<script lang="ts">
  import type { KennelFixPreview } from '$lib/api/types';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import Button from '$lib/components/Button.svelte';
  import Checkbox from '$lib/components/Checkbox.svelte';
  import { appendedLines } from './words';

  interface Props {
    preview: KennelFixPreview;
    onapprove?: () => void;
    canApprove?: boolean;
    approving?: boolean;
    canPublish?: boolean;
    publishing?: boolean;
    onpublish?: () => void;
  }

  let {
    preview,
    onapprove,
    canApprove = false,
    approving = false,
    canPublish = false,
    publishing = false,
    onpublish,
  }: Props = $props();
  let confirmed = $state(false);
  const approval = $derived(preview.approval);
  const publication = $derived(preview.publication);
  const diff = $derived(appendedLines(preview.before, preview.after));
</script>

<section class="card" aria-labelledby="kennel-fix-title">
  <h2 id="kennel-fix-title">Kept Kennel fix</h2>
  <p>{preview.explanation}</p>
  <dl class="facts">
    <div>
      <dt>Fix</dt>
      <dd class="mono">{preview.id}</dd>
    </div>
    <div>
      <dt>Repository</dt>
      <dd>{preview.repository}</dd>
    </div>
    <div>
      <dt>Finding</dt>
      <dd><code>{preview.check_code}</code> in job {preview.job_name}</dd>
    </div>
    <div>
      <dt>File</dt>
      <dd>{preview.workflow_path}, line {preview.line}</dd>
    </div>
    <div>
      <dt>Timeout</dt>
      <dd>{preview.minutes} minutes, chosen by the operator</dd>
    </div>
    <div>
      <dt>Read at</dt>
      <dd>
        <span class="mono">{preview.head_sha}</span> on <span class="mono">{preview.branch}</span>
      </dd>
    </div>
    <div>
      <dt>File name (blob)</dt>
      <dd class="mono">{preview.blob_sha}</dd>
    </div>
    <div>
      <dt>Source digest</dt>
      <dd class="mono">{preview.source_sha256}</dd>
    </div>
    <div>
      <dt>Kept</dt>
      <dd><RelativeTime value={preview.created_at} /> by {preview.reviewed_by}</dd>
    </div>
    <div>
      <dt>Verification</dt>
      <dd>Kennel Club reads the file again after the change reaches the branch</dd>
    </div>
    <div>
      <dt>Currentness</dt>
      <dd>Not checked: the branch and your settings were not re-read</dd>
    </div>
  </dl>
  {#if diff}
    <div class="diff" role="group" aria-label="The change to this job, untrusted text">
      {#each diff.context as line, i (i)}
        <pre class="context"><span aria-hidden="true"></span><span class="sr"
            >Unchanged: </span>{line}</pre>
      {/each}
      {#each diff.added as line, i (i)}
        <pre class="added"><span aria-hidden="true">+ </span><span class="sr"
            >Added: </span>{line}</pre>
      {/each}
    </div>
  {:else}
    <div class="diff" role="group" aria-label="The change to this job, untrusted text">
      <pre class="removed"><span aria-hidden="true">- </span><span class="sr"
          >Removed: </span>{preview.before.replace(/\n$/, '')}</pre>
      <pre class="added"><span aria-hidden="true">+ </span><span class="sr"
          >Added: </span>{preview.after.replace(/\n$/, '')}</pre>
    </div>
  {/if}
  {#if approval}
    <div class="decision">
      <h3>Approved</h3>
      <p>
        Approved by <strong>{approval.approved_by}</strong>
        <RelativeTime value={approval.approved_at} /> at commit
        <span class="mono">{approval.head_sha}</span>, source digest
        <span class="mono">{approval.source_sha256}</span>.
      </p>
      <p>
        This approval expires with the fix. It did not publish anything: no pull request was opened
        and no job was run.
      </p>
    </div>
  {:else if canApprove}
    <div class="decision">
      <h3>Approve this fix</h3>
      <p>
        Approval names this exact fix, its commit and its source digest. Neither is re-read, and
        approval publishes nothing: no pull request is opened and no job is run.
      </p>
      <Checkbox
        bind:checked={confirmed}
        label="I approve this exact fix, including the timeout of {preview.minutes} minutes, at the commit and source digest above."
        disabled={approving}
      />
      <div class="actions">
        <Button onclick={() => onapprove?.()} loading={approving} disabled={approving || !confirmed}
          >Approve fix</Button
        >
      </div>
    </div>
  {/if}
  {#if publication}
    <div class="decision">
      <h3>Draft pull request opened</h3>
      <p>
        <a href={publication.pull_url}>#{publication.pull_number}</a> from
        <span class="mono">{publication.branch}</span> into
        <span class="mono">{publication.base_branch}</span>, opened by
        <strong>{publication.published_by}</strong>
        <RelativeTime value={publication.published_at} />.
      </p>
      <p>
        Nothing was merged, and the finding is not closed yet. Kennel Club closes it when it reads
        the file on the default branch and the job has a timeout, not when a pull request is opened.
        Review the draft, check that {preview.minutes} minutes is enough for a healthy run, and let the
        repository's own checks decide.
      </p>
    </div>
  {:else if approval && canPublish}
    <div class="decision">
      <h3>Publish a draft pull request</h3>
      <p>
        This commits the reviewed change on a new branch and opens a draft pull request against the
        default branch. It never merges, never force-pushes and runs no job. If the file has changed
        since you kept this fix, publishing is refused and you start again from the finding.
      </p>
      <div class="actions">
        <Button onclick={() => onpublish?.()} loading={publishing} disabled={publishing}
          >Open a draft pull request</Button
        >
      </div>
    </div>
  {/if}
  <p>
    {#if publication}
      The draft pull request is open; nothing was merged and no job was run.
    {:else if approval}
      The approval is recorded, but nothing was published.
    {:else}
      Nothing was published. There is no pull request and no job run. Keeping or approving a fix
      does not apply it.
    {/if}
  </p>
</section>

<style>
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
    padding: var(--z-space-1) var(--z-space-4);
    white-space: pre;
    margin: 0;
  }
  .added {
    font-weight: var(--z-weight-semibold);
    background: var(--z-surface-sunken);
  }
  .decision {
    margin-block: var(--z-space-4);
    padding-block: var(--z-space-3);
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .decision p {
    overflow-wrap: anywhere;
  }
  .decision h3 {
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
  }
  .actions {
    margin-block: var(--z-space-3);
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
</style>
