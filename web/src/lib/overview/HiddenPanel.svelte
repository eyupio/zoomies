<!--
  A panel somebody hid, while the Overview is being arranged: one line in its
  place and the way to bring it back, so hiding is never a decision that has
  to be undone on another page.
-->
<script lang="ts">
  import Button from '$lib/components/Button.svelte';

  interface Props {
    title: string;
    onshow: () => void;
  }
  let { title, onshow }: Props = $props();
</script>

<div class="hidden-panel">
  <p>{title} is hidden.</p>
  <Button variant="secondary" size="sm" ariaLabel="Show {title}" onclick={onshow}>Show</Button>
</div>

<style>
  .hidden-panel {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-3);
    padding: var(--z-space-3) var(--z-space-5);
    border: var(--z-border-width) dashed var(--z-border);
    border-radius: var(--z-radius-xl);
    background: var(--z-surface);
  }
  p {
    margin: 0;
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
</style>
