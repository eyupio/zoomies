<!--
  The one card that says what to do next about this failure.

  Blocked, it names the thing in the way and puts the button that helps beside
  it. Ready, it says who pays, how many attempts are left and what Eli will and
  will not do, and offers the one button that spends an attempt. If the target
  changed under the person (the checks passed, the pull request closed, a newer
  commit replaced the one that failed) it says so in place of the offer, because
  starting a repair there would be the wrong thing to make easy. Checking and
  failing to check have their own states, so the card is never blank.

  Nothing here starts a repair except the Fix with Eli button.
-->
<script lang="ts">
  import { ExternalLink } from '@lucide/svelte';
  import Button from '$lib/components/Button.svelte';
  import Field from '$lib/components/Field.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import Textarea from '$lib/components/Textarea.svelte';
  import type { Schemas } from '$lib/api/types';
  import { blockedHeadline, blockingRow, couldNotCheck, type CheckRow } from './checks';
  import { safeHttpUrl } from './links';
  import { changedBlocker, type WorkspaceStage } from './workspace';

  interface Props {
    stage: WorkspaceStage;
    readiness: Schemas['EliReadiness'] | null;
    rows: CheckRow[];
    /** Readiness is being read, or a start is in flight. */
    busy: boolean;
    /** The note for Eli, which the person may leave empty. */
    instruction?: string;
    /** An offer starts with no note, so the page can turn the field off. */
    allowInstruction: boolean;
    onstart: () => void;
    onrecheck: () => void;
    onsetup: () => void;
    onchoose?: () => void;
    /**
     * Ask about the pull request as it stands now. A failure reached by its run is
     * checked against the commit that run ran on, so asking again by run says the
     * same thing for ever once a newer commit has landed; with this, the way forward
     * from that is a button that asks by the pull request's number instead.
     */
    oncurrent?: () => void;
    /** The failing run on GitHub, kept for the card that says the target changed. */
    runUrl?: string;
    /** What went wrong when readiness could not be read. */
    error?: string;
  }

  let {
    stage,
    readiness,
    rows,
    busy,
    instruction = $bindable(''),
    allowInstruction,
    onstart,
    onrecheck,
    onsetup,
    onchoose,
    oncurrent,
    runUrl,
    error = '',
  }: Props = $props();

  const headingId = $props.id();
  const blocking = $derived(blockingRow(rows));
  const changed = $derived(readiness ? changedBlocker(readiness) : undefined);
  const original = $derived(safeHttpUrl(runUrl));

  const CHANGED_HEADLINES = {
    fixed: 'Already fixed',
    closed: 'This PR is closed',
    superseded: 'A newer commit replaced this failure',
  } as const;

  /** An address within the product, or a web one: nothing else is a link. */
  function linkTarget(href: string): string | undefined {
    if (/^\/(?![/\\])/.test(href)) return href;
    return safeHttpUrl(href);
  }

  const action = $derived(blocking?.action);
  const actionLink = $derived(action?.kind === 'link' ? linkTarget(action.href) : undefined);
  // An external page is somewhere to go and come back from, so it is never the
  // main button; a page of our own, or a form to fill, is.
  const externalFix = $derived(action?.kind === 'link' && Boolean(action.external));
  const recheckLabel = $derived(blocking?.code === 'access' ? 'Check access again' : 'Check again');

  const heading = $derived.by(() => {
    switch (stage) {
      case 'checking':
        return 'Checking whether Eli can act';
      case 'error':
        return 'Could not check readiness';
      case 'changed':
        return changed ? CHANGED_HEADLINES[changed.code] : 'This failure has changed';
      case 'ready':
        return 'Ready to fix';
      default:
        return blockedHeadline(blocking?.code ?? '', blocking?.blocker);
    }
  });
</script>

<section class="next" data-stage={stage} aria-labelledby={headingId}>
  <div class="inner">
    <h2 id={headingId} tabindex="-1">{heading}</h2>
    {#if error && stage !== 'error'}
      <p class="problem" role="alert">{error}</p>
    {/if}

    {#if stage === 'checking'}
      <div role="status" aria-busy="true" class="stack">
        <p class="muted">
          Reading the pull request, your access and your allowance from GitHub. Nothing is spent.
        </p>
        <Skeleton lines={2} height="0.9rem" />
      </div>
    {:else if stage === 'error'}
      <div role="alert" class="stack">
        <p>{error || 'The controller could not be asked.'}</p>
      </div>
      <div class="buttons">
        <Button variant="primary" full loading={busy} onclick={onrecheck}>Try again</Button>
        <Button variant="ghost" full onclick={onsetup}>Set up repairs</Button>
      </div>
    {:else if stage === 'changed' && changed}
      <p class="muted">{changed.message}</p>
      <div class="buttons">
        {#if changed.code === 'superseded'}
          <Button variant="primary" full loading={busy} onclick={oncurrent ?? onrecheck}
            >Check the current PR</Button
          >
        {:else}
          <Button variant="secondary" full loading={busy} onclick={onrecheck}>Check again</Button>
        {/if}
        {#if original}
          <Button variant="secondary" full href={original} newTab iconAfter={ExternalLink}
            >View the original run on GitHub</Button
          >
        {/if}
      </div>
      <p class="fine">No repair was started and no attempt was used.</p>
    {:else if stage === 'ready' && readiness}
      <dl class="facts">
        <dt>Paid by</dt>
        <dd>
          {#if readiness.model}
            {readiness.provider} <span class="muted">({readiness.model})</span>
          {:else}
            {readiness.provider}
          {/if}
        </dd>
        <dt>Tries left</dt>
        <dd>
          {#if readiness.reset_at}
            {readiness.remaining} today,
            <span class="muted">resets <RelativeTime value={readiness.reset_at} plain /></span>
          {:else}
            {readiness.remaining} today
          {/if}
        </dd>
        <dt>Workflow files</dt>
        <dd>{readiness.allow_workflows ? 'Eli may edit them' : 'Eli will not edit them'}</dd>
        {#if readiness.pull?.branch}
          <dt>Pushes to</dt>
          <dd><code>{readiness.pull.branch}</code></dd>
        {/if}
      </dl>
      <Button variant="primary" full loading={busy} onclick={onstart}>Fix with Eli</Button>
      {#if allowInstruction}
        <Field
          label="Add a note for Eli (optional)"
          hint="The failed checks are included automatically."
        >
          {#snippet children({ id, describedBy })}
            <Textarea {id} {describedBy} bind:value={instruction} rows={3} />
          {/snippet}
        </Field>
      {/if}
      <p class="fine">
        Eli pushes one commit, then watches CI. It never merges and never force-pushes.
      </p>
    {:else}
      {#if blocking}<p class="muted">{blocking.message}</p>{/if}
      {#if blocking?.code === 'pull' && !couldNotCheck(blocking.blocker)}
        <p class="muted">
          Eli repairs open PR branches in this repository. Choose or paste a pull request if this
          run has no eligible PR.
        </p>
      {/if}
      <div class="buttons">
        {#if action?.kind === 'setup'}
          <Button variant="primary" full onclick={onsetup}>Set up repairs</Button>
          <Button variant="secondary" full loading={busy} onclick={onrecheck}>{recheckLabel}</Button
          >
        {:else if action?.kind === 'choose' && onchoose}
          <Button variant="primary" full onclick={onchoose}>Choose a pull request</Button>
          <Button variant="secondary" full loading={busy} onclick={onrecheck}>{recheckLabel}</Button
          >
        {:else if action?.kind === 'link' && actionLink && !externalFix}
          <Button variant="primary" full href={actionLink}>{action.label}</Button>
          <Button variant="secondary" full loading={busy} onclick={onrecheck}>{recheckLabel}</Button
          >
        {:else}
          <Button variant="primary" full loading={busy} onclick={onrecheck}>{recheckLabel}</Button>
          {#if action?.kind === 'link' && actionLink}
            <Button variant="secondary" full href={actionLink} newTab iconAfter={ExternalLink}
              >{action.label}</Button
            >
          {/if}
        {/if}
        {#if action?.kind !== 'setup'}
          <Button variant="ghost" full onclick={onsetup}>Set up repairs</Button>
        {/if}
      </div>
      <p class="fine">
        Nothing has been spent. Checking again never uses an attempt, and once this is fixed the
        page shows what Eli will do and a Fix with Eli button.
      </p>
    {/if}
  </div>
</section>

<style>
  .next {
    --tone: var(--z-pending);
    min-width: 0;
    border: var(--z-border-width) solid var(--z-border);
    border-top: var(--z-border-width-rail) solid var(--tone);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  .next[data-stage='checking'] {
    --tone: var(--z-busy);
  }
  .next[data-stage='ready'] {
    --tone: var(--z-idle);
  }
  .next[data-stage='changed'] {
    --tone: var(--z-neutral);
  }
  .next[data-stage='error'] {
    --tone: var(--z-danger);
  }
  .inner {
    display: grid;
    gap: var(--z-space-4);
    padding: var(--z-space-5);
  }
  h2 {
    margin: 0;
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
    line-height: var(--z-leading-base);
    overflow-wrap: anywhere;
  }
  p {
    margin: 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    overflow-wrap: anywhere;
  }
  .problem {
    padding: var(--z-space-2) var(--z-space-3);
    border: var(--z-border-width) solid var(--z-danger-border);
    border-radius: var(--z-radius-sm);
    background: var(--z-danger-subtle);
  }
  .stack {
    display: grid;
    gap: var(--z-space-3);
  }
  .muted {
    color: var(--z-text-muted);
  }
  .fine {
    color: var(--z-text-subtle);
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
  }
  .buttons {
    display: grid;
    gap: var(--z-space-2);
  }
  .facts {
    display: grid;
    grid-template-columns: minmax(0, 7.5rem) minmax(0, 1fr);
    gap: var(--z-space-2) var(--z-space-3);
    margin: 0;
    font-size: var(--z-text-sm);
  }
  .facts dt {
    color: var(--z-text-muted);
  }
  .facts dd {
    min-width: 0;
    margin: 0;
    overflow-wrap: anywhere;
  }
  code {
    font-family: var(--z-font-mono);
    font-size: var(--z-text-xs);
  }
</style>
