<!--
  What stands at the top of the body while the Overview is being arranged:
  what to do, how many panels are hidden, and the two ways out, back to the
  default or done.
-->
<script lang="ts">
  import Button from '$lib/components/Button.svelte';
  import { pluralise } from '$lib/format';

  interface Props {
    hidden: number;
    isDefault: boolean;
    onreset: () => void;
    ondone: () => void;
  }
  let { hidden, isDefault, onreset, ondone }: Props = $props();
</script>

<section class="bar" aria-label="Arrange the Overview">
  <p>
    Arrange the Overview. Drag a card by its grip, or use its buttons.
    {hidden > 0 ? `${pluralise(hidden, 'panel')} hidden.` : 'Nothing hidden.'}
  </p>
  <div class="actions">
    <Button
      variant="secondary"
      size="sm"
      disabled={isDefault}
      reason={isDefault ? 'The layout is the default.' : undefined}
      onclick={onreset}>Reset layout</Button
    >
    <Button variant="primary" size="sm" onclick={ondone}>Done</Button>
  </div>
</section>

<style>
  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--z-space-2) var(--z-space-4);
    padding: var(--z-space-3) var(--z-space-5);
    border: var(--z-border-width) dashed var(--z-border-strong);
    border-radius: var(--z-radius-xl);
    background: var(--z-surface);
  }
  p {
    margin: 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    color: var(--z-text-muted);
  }
  .actions {
    display: flex;
    gap: var(--z-space-2);
  }
</style>
