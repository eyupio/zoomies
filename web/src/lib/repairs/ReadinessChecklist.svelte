<!--
  Every check readiness makes, in the order it makes them: what passed, what is
  in the way and what to do about it, and what was never reached because an
  earlier check stopped the run. When everything passes it folds to one line.

  Checking never calls a model and never uses an attempt, and the panel says so,
  because a person who is about to press a button that costs one wants to know
  that this one does not.
-->
<script lang="ts">
  import { Check, ChevronRight, Circle, ExternalLink, X } from '@lucide/svelte';
  import Badge from '$lib/components/Badge.svelte';
  import Button from '$lib/components/Button.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import { pluralise } from '$lib/format';
  import type { CheckRow } from './checks';
  import { safeHttpUrl } from './links';

  interface Props {
    rows: CheckRow[];
    /** A check is in flight. */
    busy: boolean;
    onrecheck: () => void;
    onsetup: () => void;
    onchoose: () => void;
  }

  let { rows, busy, onrecheck, onsetup, onchoose }: Props = $props();

  const blockedRows = $derived(rows.filter((row) => row.status === 'blocked'));
  const blockedCount = $derived(blockedRows.length);
  const otherRows = $derived(rows.filter((row) => row.status !== 'blocked'));
  const allPassed = $derived(rows.length > 0 && rows.every((row) => row.status === 'ok'));

  const words = { ok: 'Passed', blocked: 'Blocked', unchecked: 'Not checked yet' } as const;

  /** An address within the product, or a web one: nothing else is a link. */
  function linkTarget(href: string): string | undefined {
    if (/^\/(?![/\\])/.test(href)) return href;
    return safeHttpUrl(href);
  }
</script>

{#snippet list(shown: readonly CheckRow[])}
  <ul>
    {#each shown as row (row.code)}
      {@const link = row.action?.kind === 'link' ? linkTarget(row.action.href) : undefined}
      <li class="row {row.status}">
        <span class="ico" aria-hidden="true">
          {#if row.status === 'ok'}<Check size={12} />{:else if row.status === 'blocked'}<X
              size={12}
            />{:else}<Circle size={5} />{/if}
        </span>
        <div class="text">
          <div class="label">{row.label}<span class="sr-only">: {words[row.status]}</span></div>
          <div class="message">{row.message}</div>
        </div>
        <div class="go">
          {#if row.action?.kind === 'recheck'}
            <Button variant="secondary" size="sm" loading={busy} onclick={onrecheck}
              >Check again</Button
            >
          {:else if row.action?.kind === 'setup'}
            <Button variant="secondary" size="sm" onclick={onsetup}>Set up repairs</Button>
          {:else if row.action?.kind === 'choose'}
            <Button variant="secondary" size="sm" onclick={onchoose}>Choose a pull request</Button>
          {:else if row.action?.kind === 'link' && link}
            <Button
              variant="secondary"
              size="sm"
              href={link}
              newTab={Boolean(row.action.external)}
              iconAfter={row.action.external ? ExternalLink : undefined}>{row.action.label}</Button
            >
          {/if}
        </div>
      </li>
    {/each}
  </ul>
{/snippet}

<Panel
  title="Can Eli act on this?"
  description="Checked on GitHub just now. Checking never calls a model or uses an attempt."
  flush
>
  {#snippet actions()}
    {#if blockedCount}
      <Badge
        tone="pending"
        label={blockedCount === 1 ? '1 thing to fix' : `${pluralise(blockedCount, 'thing')} to fix`}
      />
    {/if}
  {/snippet}

  {#if rows.length === 0}
    <p class="empty">Nothing has been checked yet.</p>
  {:else if allPassed}
    <details class="folded">
      <summary
        ><ChevronRight size={16} class="chev" aria-hidden="true" /><span
          class="pass"
          aria-hidden="true"><Check size={12} /></span
        >All {rows.length} checks passed</summary
      >
      {@render list(rows)}
    </details>
  {:else}
    {@render list(blockedRows)}
    {#if otherRows.length}
      <details class="folded">
        <summary
          ><ChevronRight size={16} class="chev" aria-hidden="true" />Show all {rows.length} checks</summary
        >
        {@render list(otherRows)}
      </details>
    {/if}
  {/if}
</Panel>

<style>
  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .row {
    display: grid;
    grid-template-columns: 1.375rem minmax(0, 1fr) auto;
    gap: var(--z-space-3);
    padding: var(--z-space-3) var(--z-space-5);
    border-top: var(--z-border-width) solid var(--z-border);
  }
  .row:first-child {
    border-top: 0;
  }
  .ico {
    display: grid;
    place-items: center;
    width: 1.25rem;
    height: 1.25rem;
    margin-top: var(--z-nudge-1);
    border: var(--z-border-width) solid var(--tone-border);
    border-radius: var(--z-radius-full);
    background: var(--tone-subtle);
    color: var(--tone);
  }
  .ok {
    --tone: var(--z-idle);
    --tone-subtle: var(--z-idle-subtle);
    --tone-border: var(--z-idle-border);
  }
  .blocked {
    --tone: var(--z-danger);
    --tone-subtle: var(--z-danger-subtle);
    --tone-border: var(--z-danger-border);
  }
  /* Never reached: a dashed, empty ring, so it cannot be mistaken for a pass. */
  .unchecked {
    --tone: var(--z-text-subtle);
    --tone-subtle: transparent;
    --tone-border: var(--z-border-strong);
  }
  .unchecked .ico {
    border-style: dashed;
  }
  .text {
    min-width: 0;
  }
  .label {
    font-weight: var(--z-weight-medium);
    overflow-wrap: anywhere;
  }
  .message {
    margin-top: var(--z-space-1);
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
    overflow-wrap: anywhere;
  }
  .unchecked .label {
    color: var(--z-text-muted);
    font-weight: var(--z-weight-normal);
  }
  .unchecked .message {
    color: var(--z-text-subtle);
  }
  .go {
    align-self: center;
  }
  .empty {
    margin: 0;
    padding: var(--z-space-4) var(--z-space-5);
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
  }
  .folded > summary {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    padding: var(--z-space-3) var(--z-space-5);
    cursor: pointer;
    list-style: none;
    font-weight: var(--z-weight-medium);
  }
  .folded > summary::-webkit-details-marker {
    display: none;
  }
  .pass {
    display: grid;
    place-items: center;
    width: 1.25rem;
    height: 1.25rem;
    border: var(--z-border-width) solid var(--z-idle-border);
    border-radius: var(--z-radius-full);
    background: var(--z-idle-subtle);
    color: var(--z-idle);
  }
  .folded > summary :global(.chev) {
    color: var(--z-text-subtle);
    transition: transform var(--z-motion-fast) var(--z-ease);
  }
  .folded[open] > summary :global(.chev) {
    transform: rotate(90deg);
  }
  @media (max-width: 640px) {
    .row {
      grid-template-columns: 1.375rem minmax(0, 1fr);
      padding: var(--z-space-3) var(--z-space-4);
    }
    .go {
      grid-column: 2;
      justify-self: start;
    }
  }
</style>
