<!--
  What a host's registry mirror is doing: on, or what is wrong with it.

  The sentences are the controller's, so the page keeps no copy of them. A
  problem is drawn as a warning (the pending tone and a triangle), never as an
  error: a broken mirror slows a pull down and fails nothing, and the line says
  so before anything else, since a warning that looks red-handed would send an
  operator to a fault that is not there. Shape and words carry the state as
  well as colour.
-->
<script lang="ts">
  import { Check, TriangleAlert } from '@lucide/svelte';
  import type { HostRegistryMirror } from '$lib/api/types';
  import RemedyText from '$lib/components/RemedyText.svelte';
  import { registryMirrorLine } from './registry-mirror';

  interface Props {
    mirror: HostRegistryMirror | null | undefined;
    class?: string;
  }

  let { mirror, class: className = '' }: Props = $props();

  const line = $derived(registryMirrorLine(mirror));
</script>

{#if line}
  <div
    class="mirror {className}"
    class:problem={line.state === 'problem'}
    data-testid="host-registry-mirror"
    data-state={line.state}
  >
    <span class="mark" aria-hidden="true">
      {#if line.state === 'problem'}
        <TriangleAlert size={12} />
      {:else}
        <Check size={12} />
      {/if}
    </span>
    <span class="body">
      <span class="headline">{line.headline}</span>
      {#if line.detail}
        <span class="detail"><RemedyText text={line.detail} /></span>
      {/if}
      {#if line.consequence}
        <span class="note">{line.consequence}</span>
      {/if}
      {#if line.fix}
        <span class="detail"><RemedyText text={line.fix} /></span>
      {/if}
      {#each line.notes as note (note)}
        <span class="note">{note}</span>
      {/each}
    </span>
  </div>
{/if}

<style>
  .mirror {
    display: flex;
    align-items: flex-start;
    gap: var(--z-space-2);
    min-width: 0;
  }
  .mark {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--z-space-4);
    height: var(--z-space-4);
    border-radius: var(--z-radius-full);
    background: var(--z-idle-subtle);
    color: var(--z-idle);
    flex: none;
  }
  .problem .mark {
    background: var(--z-pending-subtle);
    color: var(--z-pending);
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-1);
    min-width: 0;
  }
  .headline {
    font-size: var(--z-text-sm);
    color: var(--z-text);
  }
  .detail {
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text);
    overflow-wrap: anywhere;
  }
  .note {
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
    overflow-wrap: anywhere;
  }
</style>
