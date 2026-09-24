<!--
  Which controller build this page is talking to: the release tag, or for a
  build from main the commit, since every one of those shares the dev tag. It
  sits at the foot of the sidebar and of the phone's More sheet -- the two
  pieces of chrome that never scroll away -- because a bug report needs it on
  whatever page the operator happens to be on.
-->
<script lang="ts">
  import { session } from '../state/session.svelte';
  import { buildLabel } from './build';

  const build = $derived(buildLabel(session.meta));
</script>

{#if build}
  <p class="build" title={build.title}>
    <span class="kind">{build.kind}</span>
    {#if build.href}
      <a class="name" href={build.href} target="_blank" rel="noopener noreferrer"
        >{build.name}<span class="sr-only"> (opens in a new tab)</span></a
      >
    {:else}
      <span class="name">{build.name}</span>
    {/if}
  </p>
{/if}

<style>
  .build {
    display: flex;
    align-items: baseline;
    gap: var(--z-space-1);
    min-width: 0;
    margin: 0;
    font-size: var(--z-text-2xs);
    color: var(--z-text-subtle);
  }
  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--z-font-mono);
    color: var(--z-text-muted);
  }
  a.name {
    text-decoration: underline;
    text-decoration-color: var(--z-border);
  }
  a.name:hover {
    color: var(--z-text);
  }
</style>
