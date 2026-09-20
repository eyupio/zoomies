<!--
  The two levels of one history: workflow runs, and the jobs inside them.

  A pair of links rather than a tab strip, because each is a page with an
  address of its own -- the problems drawer and the Overview link straight to
  the jobs, and a colleague pastes either. The filters in force travel with the
  switch, so an operator who narrowed the runs to a repository sees that
  repository's jobs rather than everything; the page's own position -- its
  offset and sort -- does not, because a page of runs and a page of jobs are
  different pages.
-->
<script lang="ts">
  import { SvelteURLSearchParams } from 'svelte/reactivity';
  import { router } from '$lib/router';

  interface Props {
    level: 'runs' | 'jobs';
  }

  let { level }: Props = $props();

  const query = $derived.by(() => {
    const params = new SvelteURLSearchParams(router.query);
    for (const key of ['offset', 'limit', 'sort', 'order']) params.delete(key);
    const text = params.toString();
    return text ? `?${text}` : '';
  });
</script>

<nav class="levels" aria-label="Level of detail">
  <a href="/workflows{query}" aria-current={level === 'runs' ? 'page' : undefined}>Workflow runs</a>
  <a href="/jobs{query}" aria-current={level === 'jobs' ? 'page' : undefined}>Every job</a>
</nav>

<style>
  .levels {
    display: inline-flex;
    border: var(--z-border-width) solid var(--z-border-strong);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
    overflow: hidden;
  }
  a {
    display: inline-flex;
    align-items: center;
    height: var(--z-space-8);
    padding: 0 var(--z-space-3);
    color: var(--z-text-muted);
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
    text-decoration: none;
    white-space: nowrap;
    transition:
      background-color var(--z-motion-fast) var(--z-ease),
      color var(--z-motion-fast) var(--z-ease);
  }
  a + a {
    border-left: var(--z-border-width) solid var(--z-border-strong);
  }
  a:hover {
    background: var(--z-surface-hover);
    color: var(--z-text);
  }
  a[aria-current='page'] {
    background: var(--z-accent-subtle);
    color: var(--z-accent);
  }
  a:focus-visible {
    outline: var(--z-focus-width) solid var(--z-focus-colour);
    outline-offset: calc(-1 * var(--z-focus-offset));
  }
</style>
