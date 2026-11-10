<!--
  The rows a rail list is about to draw, before its first read has answered.

  Drawn in the rows' own box (the rail's shared `.row`), so the list does not
  jump when the real rows replace these, and hidden from assistive technology:
  the section says it is busy instead, which is what a screen reader can use.
-->
<script lang="ts">
  import Skeleton from '$lib/components/Skeleton.svelte';

  let { rows = 3 }: { rows?: number } = $props();
</script>

<ul aria-hidden="true" data-testid="command-rail-skeleton">
  {#each Array.from({ length: rows }, (_, i) => i) as row (row)}
    <li class="row skeleton">
      <span class="mark"><Skeleton width="1rem" height="1rem" radius="full" /></span>
      <span class="what">
        <Skeleton width="70%" height="var(--z-text-sm)" />
        <Skeleton width="45%" height="var(--z-text-2xs)" />
      </span>
    </li>
  {/each}
</ul>

<style>
  /* Not pressable, so it does not answer the pointer as a row does. */
  .skeleton {
    gap: var(--z-space-2);
    cursor: default;
  }
  .what {
    gap: var(--z-space-1);
  }
</style>
