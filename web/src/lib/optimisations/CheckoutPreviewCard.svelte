<script lang="ts">
  import type { CheckoutPreview } from '../api/types';
  import { checkoutSeconds } from './input';
  import RelativeTime from '../components/RelativeTime.svelte';
  import Checkbox from '../components/Checkbox.svelte';
  import Button from '../components/Button.svelte';

  let {
    preview,
    canApprove = false,
    approving = false,
    onapprove,
    canPublish = false,
    publishing = false,
    onpublish,
  }: {
    preview: CheckoutPreview;
    canApprove?: boolean;
    approving?: boolean;
    onapprove?: () => void;
    canPublish?: boolean;
    publishing?: boolean;
    onpublish?: () => void;
  } = $props();
  let confirmed = $state(false);
  const p = $derived(preview.preview);
  const approval = $derived(preview.approval);
  const publication = $derived(preview.publication);
  const verification = $derived(preview.verification);
</script>

<section class="card" aria-labelledby="preview-title">
  <h2 id="preview-title">Retained preview</h2>
  <p>
    {p.explanation}
  </p>
  <dl class="facts">
    <div>
      <dt>Preview</dt>
      <dd class="mono">{preview.id}</dd>
    </div>
    <div>
      <dt>Repository</dt>
      <dd>{preview.repository}</dd>
    </div>
    <div>
      <dt>File</dt>
      <dd>{p.path}, line {p.line}</dd>
    </div>
    <div>
      <dt>Commit</dt>
      <dd class="mono">{p.head_sha}</dd>
    </div>
    <div>
      <dt>Source digest</dt>
      <dd class="mono">{p.source_sha256}</dd>
    </div>
    <div>
      <dt>Run</dt>
      <dd>{p.run_id}, attempt {p.attempt}</dd>
    </div>
    <div>
      <dt>Checkout step took</dt>
      <dd>{checkoutSeconds(p.checkout_ms)} (descriptive, not a saving)</dd>
    </div>
    <div>
      <dt>Kept</dt>
      <dd><RelativeTime value={preview.created_at} /> by {preview.reviewed_by}</dd>
    </div>
    <div>
      <dt>Verification</dt>
      <dd>{p.verification}: no measurement shows this change helps</dd>
    </div>
    <div>
      <dt>Currentness</dt>
      <dd>Not checked: the commit and your settings were not re-read</dd>
    </div>
  </dl>
  <div class="diff" role="group" aria-label="The one line this preview changes, untrusted text">
    <pre class="removed"><span aria-hidden="true">- </span><span class="sr"
        >Removed: </span>{p.before.replace(/\n$/, '')}</pre>
    <pre class="added"><span aria-hidden="true">+ </span><span class="sr"
        >Added: </span>{p.after.replace(/\n$/, '')}</pre>
  </div>
  {#if approval}
    <div class="approval">
      <h3>Approved</h3>
      <p>
        Approved by <strong>{approval.approved_by}</strong>
        <RelativeTime value={approval.approved_at} /> at commit
        <span class="mono">{approval.head_sha}</span>, source digest
        <span class="mono">{approval.source_sha256}</span>.
      </p>
      <p>
        This approval expires with the preview. It did not publish anything: no pull request was
        opened and no job was run.
      </p>
    </div>
  {:else if canApprove}
    <div class="approval">
      <h3>Approve this preview</h3>
      <p>
        Approval names this exact preview, its commit and its source digest. Neither is re-read, and
        approval publishes nothing: no pull request is opened and no job is run.
      </p>
      <Checkbox
        bind:checked={confirmed}
        label="I approve this exact preview at the commit and source digest above."
        disabled={approving}
      />
      <div class="approval-actions">
        <Button onclick={() => onapprove?.()} loading={approving} disabled={approving || !confirmed}
          >Approve preview</Button
        >
      </div>
    </div>
  {/if}
  {#if publication}
    <div class="approval">
      <h3>Draft pull request opened</h3>
      <p>
        <a href={publication.pull_url}>#{publication.pull_number}</a> from
        <span class="mono">{publication.branch}</span> into
        <span class="mono">{publication.base_branch}</span>, opened by
        <strong>{publication.published_by}</strong>
        <RelativeTime value={publication.published_at} />.
      </p>
      <p>
        Nothing was merged. Zoomies does not merge pull requests; review the draft and let the
        repository's own checks decide. Verification remains inconclusive until a baseline and
        candidate comparison exists.
      </p>
      {#if verification}
        <p>
          Verification comparison:
          <a
            href={`/comparisons?id=${encodeURIComponent(verification.comparison_id)}&installation_id=${encodeURIComponent(preview.installation_id)}&repo=${encodeURIComponent(preview.repository)}`}
            >{verification.comparison_id}</a
          >, linked by <strong>{verification.linked_by}</strong>
          <RelativeTime value={verification.linked_at} />. Read it there; Zoomies does not turn it
          into a saving claim.
        </p>
      {:else}
        <div class="approval-actions">
          <Button
            href={`/comparisons?installation_id=${encodeURIComponent(preview.installation_id)}&repo=${encodeURIComponent(preview.repository)}&kind=workflow&revision=${encodeURIComponent(preview.id)}&preview=${encodeURIComponent(preview.id)}`}
            variant="secondary">Compare runs for this preview</Button
          >
        </div>
      {/if}
    </div>
  {:else if approval && canPublish}
    <div class="approval">
      <h3>Publish a draft pull request</h3>
      <p>
        This commits the one reviewed line on a new branch and opens a draft pull request against
        the branch the run came from. It never merges, never force-pushes and runs no job. No
        measurement shows the change saves time.
      </p>
      <div class="approval-actions">
        <Button onclick={() => onpublish?.()} loading={publishing} disabled={publishing}
          >Open a draft pull request</Button
        >
      </div>
    </div>
  {/if}
  <p>
    {#if publication}
      The draft pull request is open; nothing was merged and no job was run. Measuring the change is
      a later step that is not built yet.
    {:else if approval}
      The approval is recorded, but nothing was published. Opening a pull request and measuring the
      result are later steps that are not built yet.
    {:else}
      Nothing was published. There is no pull request, no approval and no job run. Keeping a preview
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
    padding: var(--z-space-2) var(--z-space-4);
    white-space: pre;
    margin: 0;
  }
  .added {
    font-weight: var(--z-weight-semibold);
    background: var(--z-surface-sunken);
  }
  .approval {
    margin-block: var(--z-space-5);
    padding-block-start: var(--z-space-4);
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .approval p {
    overflow-wrap: anywhere;
  }
  .approval h3 {
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
    margin-block-end: var(--z-space-2);
  }
  .approval-actions {
    margin-block-start: var(--z-space-3);
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
</style>
