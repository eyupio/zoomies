<script lang="ts">
  import { untrack, type Snippet } from 'svelte';
  import { ChevronRight, type LucideIcon } from '@lucide/svelte';
  import { remember, remembered } from '$lib/state/prefs.svelte';

  let {
    id,
    title,
    icon: Icon,
    summary = '',
    children,
    actions,
    class: className = '',
  }: {
    id: string;
    title: string;
    /** Drawn in a tile before the title, as the Overview's cards carry one. */
    icon?: LucideIcon;
    summary?: string;
    children: Snippet;
    actions?: Snippet;
    class?: string;
  } = $props();
  const key = untrack(() => `zoomies.overview.${id}.open`);
  let open = $state(
    remembered(key, false, (value): value is boolean => typeof value === 'boolean'),
  );
  let previous = untrack(() => open);
  let details: HTMLDetailsElement | null = $state(null);

  // The state drives the element from an effect of its own rather than an
  // attribute in the template. A page jump opens the element directly and the
  // toggle event that would sync the state back is asynchronous; an attribute
  // shares its template effect with the summary text, so a summary that changed
  // in that gap (History's does, when its range moves) wrote the stale state
  // back over the open the jump had just made.
  $effect(() => {
    if (details && details.open !== open) details.open = open;
  });

  function toggled(event: Event): void {
    open = (event.currentTarget as HTMLDetailsElement).open;
    // Initial paint is not a choice. Native toggles and page jumps are.
    if (previous !== open) remember(key, open);
    previous = open;
  }
</script>

<details {id} class="overview-disclosure {className}" bind:this={details} ontoggle={toggled}>
  <summary>
    <ChevronRight size={16} class="chevron" aria-hidden="true" />
    {#if Icon}<span class="tile" aria-hidden="true"><Icon size={16} /></span>{/if}
    <span class="title">{title}</span>
    <span class="context">{summary}</span>
    {#if actions}{@render actions()}{/if}
  </summary>
  <div class="content">{@render children()}</div>
</details>

<style>
  details {
    min-width: 0;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
    scroll-margin-top: calc(var(--z-topbar-height) + var(--z-control-touch) + var(--z-space-3));
  }
  summary {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-1) var(--z-space-2);
    min-height: var(--z-control-touch);
    padding: var(--z-space-2) var(--z-space-3);
    cursor: pointer;
    list-style: none;
    font-size: var(--z-text-sm);
  }
  summary::-webkit-details-marker {
    display: none;
  }
  summary:hover {
    background: var(--z-surface-hover);
  }
  summary :global(.chevron) {
    flex: none;
    color: var(--z-accent);
  }
  details[open] > summary :global(.chevron) {
    transform: rotate(90deg);
  }
  .tile {
    display: inline-flex;
    flex: none;
    align-items: center;
    justify-content: center;
    width: var(--z-space-8);
    height: var(--z-space-8);
    border: var(--z-border-width) solid var(--z-border-strong);
    border-radius: var(--z-radius-md);
    background: var(--z-surface-sunken);
    color: var(--z-text);
  }
  .title {
    font-weight: var(--z-weight-semibold);
  }
  .context {
    margin-left: auto;
    color: var(--z-text-muted);
    overflow-wrap: anywhere;
    font-size: var(--z-text-xs);
  }
  .context:empty {
    display: none;
  }
  .content {
    padding: var(--z-space-3);
    border-top: var(--z-border-width) solid var(--z-border);
  }
  @media (max-width: 768px) {
    .context {
      width: 100%;
      margin-left: var(--z-space-6);
    }
    .content {
      padding: var(--z-space-2);
    }
  }
</style>
