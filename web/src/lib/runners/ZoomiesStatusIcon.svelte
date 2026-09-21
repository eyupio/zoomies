<script lang="ts">
  import { prefs } from '$lib/state/prefs.svelte';
  import CuteStatusIcon from './CuteStatusIcon.svelte';
  import StandardStatusIcon from './StandardStatusIcon.svelte';

  let { state, seed = 'zoomies' }: { state: string; seed?: string } = $props();
</script>

{#if prefs.statusStyle === 'standard'}
  <StandardStatusIcon {state} {seed} />
{:else if prefs.statusStyle === 'cute'}
  <CuteStatusIcon {state} {seed} />
{/if}
