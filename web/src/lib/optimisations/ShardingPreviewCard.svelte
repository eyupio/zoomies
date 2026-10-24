<script lang="ts">
  import type { ShardingPreview } from '../api/types';
  import { checkoutSeconds } from './input';
  import RelativeTime from '../components/RelativeTime.svelte';

  let { preview }: { preview: ShardingPreview } = $props();
</script>

<section class="card" aria-labelledby="sharding-preview-title">
  <h2 id="sharding-preview-title">Retained sharding preview</h2>
  <p>{preview.explanation}</p>
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
      <dd>{preview.workflow_path}, line {preview.line}</dd>
    </div>
    <div>
      <dt>Test step</dt>
      <dd>{preview.step_name} in job {preview.job_name}</dd>
    </div>
    <div>
      <dt>Commit</dt>
      <dd class="mono">{preview.head_sha}</dd>
    </div>
    <div>
      <dt>Source digest</dt>
      <dd class="mono">{preview.source_sha256}</dd>
    </div>
    <div>
      <dt>Shards</dt>
      <dd>{preview.shards}, which together run the whole Playwright suite</dd>
    </div>
    <div>
      <dt>Runners needed at once</dt>
      <dd>
        {preview.shards} of the {preview.headroom} the pool could start when this was kept (an upper bound
        at one moment, not a reservation)
      </dd>
    </div>
    <div>
      <dt>Runner time per run</dt>
      <dd>
        about {checkoutSeconds(preview.runner_ms_before)} now, about {checkoutSeconds(
          preview.runner_ms_after,
        )} sharded, plus one short aggregator job: every shard repeats the {checkoutSeconds(
          preview.setup_ms,
        )} of setup
      </dd>
    </div>
    <div>
      <dt>Wall clock</dt>
      <dd>
        about {checkoutSeconds(preview.wall_ms_before)} now, estimated about {checkoutSeconds(
          preview.wall_ms_after,
        )} if the shards start together (an estimate from medians over {preview.runs} runs, not a saving)
      </dd>
    </div>
    <div>
      <dt>Kept</dt>
      <dd><RelativeTime value={preview.created_at} /> by {preview.reviewed_by}</dd>
    </div>
    <div>
      <dt>Verification</dt>
      <dd>{preview.verification}: no measurement shows this change helps</dd>
    </div>
    <div>
      <dt>Currentness</dt>
      <dd>Not checked: the commit, the history and your settings were not re-read</dd>
    </div>
  </dl>
  <div class="diff" role="group" aria-label="The job block this preview changes, untrusted text">
    <pre class="removed"><span aria-hidden="true">- </span><span class="sr"
        >Removed: </span>{preview.before.replace(/\n$/, '')}</pre>
    <pre class="added"><span aria-hidden="true">+ </span><span class="sr"
        >Added: </span>{preview.after.replace(/\n$/, '')}</pre>
  </div>
  <p>
    Nothing was approved or published. There is no pull request and no job run. Keeping a preview
    does not apply it.
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
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
</style>
