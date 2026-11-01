<!--
  The workflow snippet these labels produce.

  Pool labels only mean something in relation to a `runs-on:` line, so the
  editor shows that line as it is typed rather than describing it.

  It shows the shortest correct form. One branded label is enough to reach a
  pool -- `runs-on: zoomies-linux-x64` -- and it is what a workflow should
  write: the older `[self-hosted, linux, x64, ...]` habit is longer, says
  nothing about which fleet, and breaks the moment two pools share an
  architecture.
-->
<script lang="ts">
  import Terminal from '$lib/components/Terminal.svelte';
  import { BRAND_LABEL, isImplicit, normalizeLabels, runsOnSnippet } from '$lib/brand';

  interface Props {
    labels?: readonly string[];
    class?: string;
  }

  let { labels = [], class: className = '' }: Props = $props();

  const snippet = $derived(runsOnSnippet(labels));
  // A pool with nothing but the brand and the labels every runner already
  // advertises answers every job that asks for this fleet, which is rarely what
  // an operator meant. That is the state worth warning about -- not "no labels
  // typed yet", since the brand is always there.
  const specific = $derived(
    normalizeLabels(labels).filter((l) => !isImplicit(l) && l !== BRAND_LABEL),
  );
</script>

<figure class="preview {className}">
  <Terminal
    size="sm"
    title="What a workflow writes"
    command={snippet}
    copyLabel="Copy the runs-on snippet"
  >
    {#if specific.length === 0}
      <p>
        With no labels of its own this pool answers every job that asks for this fleet, which is
        rarely what an operator wants once there is more than one pool.
      </p>
    {/if}
  </Terminal>
</figure>

<style>
  .preview {
    margin: 0;
    min-width: 0;
  }
</style>
