<!--
  Step three: which machines these runners land on.

  Placement is its own question, before the backend rather than beside it: an
  operator picks the machines and then decides how a runner is run on them, and
  the backend step's "offered by N hosts" counts only mean something once it is
  known which hosts are in play. Choosing a backend first and finding out
  afterwards that no arm64 box offers it is the wrong order to learn it in.
-->
<script lang="ts">
  import { ServerCog } from '@lucide/svelte';
  import type { Host } from '$lib/api/types';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import HostSelectorEditor from './HostSelectorEditor.svelte';
  import type { PoolDraft } from './PoolWizardForm.svelte';

  interface Props {
    draft: PoolDraft;
    touch: (field: string) => void;
    hosts: readonly Host[];
    /** False until the fleet cache has landed, so we do not cry wolf. */
    hostsKnown: boolean;
  }

  let { draft, touch, hosts, hostsKnown }: Props = $props();

  function change(next: Record<string, string>): void {
    draft.host_selector = next;
    touch('host_selector');
  }

  function restrict(next: boolean): void {
    draft.restrict_hosts = next;
  }
</script>

{#if !hostsKnown}
  <div aria-busy="true">
    <Skeleton width="40%" height="0.9rem" />
    <Skeleton lines={3} />
  </div>
{:else if hosts.length === 0}
  <!--
    Nothing to choose between yet. Saying so beats an empty dropdown labelled
    "Any operating system", which reads like a fleet that has no Linux in it.
  -->
  <div class="empty">
    <p class="empty-title">
      <ServerCog size={16} aria-hidden="true" />
      No host has joined yet
    </p>
    <p class="empty-body">
      This pool will reach every host that joins. Once machines are connected you can come back and
      keep it to some of them — an architecture, an operating system, or your own labels.
    </p>
  </div>
{:else}
  <HostSelectorEditor
    selector={draft.host_selector}
    {hosts}
    {hostsKnown}
    restricting={draft.restrict_hosts}
    onchange={change}
    onrestrict={restrict}
  />
{/if}

<style>
  .empty {
    padding: var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface-sunken);
  }
  .empty-title {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    margin: 0;
    font-size: var(--z-text-base);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .empty-body {
    margin: var(--z-space-2) 0 0;
    max-width: 66ch;
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-xs);
    color: var(--z-text-muted);
  }
</style>
