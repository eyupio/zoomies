<!--
  A status pill. Colour and shape both, always, from the state map in
  lib/status.ts. Never hand-write a colour here.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { StatusMeta, StatusTone } from '../status';
  import StatusDot from './StatusDot.svelte';
  import Tooltip from './Tooltip.svelte';

  interface Props {
    /** Preferred: the entry from the state map, carrying colour, shape and label. */
    status?: StatusMeta;
    /** For labels that are not a status: a count, a role, a backend name. */
    tone?: StatusTone | 'accent';
    label?: string;
    size?: 'sm' | 'md';
    /** Draw the shape. On by default whenever a status is given. */
    dot?: boolean;
    /** Draw the status's branded Lucide icon instead of its abstract shape. */
    icon?: boolean;
    /**
     * What the badge means, in the shared tooltip. A status carries its own hint,
     * which is what this says unless told otherwise.
     */
    help?: string;
    class?: string;
    children?: Snippet;
  }

  let {
    status,
    tone,
    label,
    size = 'md',
    dot = true,
    icon = false,
    help,
    class: className = '',
    children,
  }: Props = $props();

  const resolvedTone = $derived(tone ?? status?.tone ?? 'neutral');
  const text = $derived(label ?? status?.label ?? '');
  const StatusIcon = $derived(status?.icon);
</script>

<Tooltip bare quiet text={help ?? status?.hint ?? ''}>
  <span
    class="badge {size} {className}"
    data-tone={resolvedTone}
    style="--badge-colour: var(--z-{resolvedTone}); --badge-subtle: var(--z-{resolvedTone}-subtle); --badge-border: var(--z-{resolvedTone}-border)"
  >
    {#if status && icon && StatusIcon}<StatusIcon
        size={size === 'sm' ? 12 : 14}
        strokeWidth={2}
        aria-hidden="true"
      />{:else if status && dot}<StatusDot {status} size="sm" tip={false} />{/if}
    {#if children}{@render children()}{:else}{text}{/if}
  </span>
</Tooltip>

<style>
  .badge {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    border: var(--z-border-width) solid var(--badge-border);
    border-radius: var(--z-radius-full);
    background: var(--badge-subtle);
    color: var(--badge-colour);
    font-weight: var(--z-weight-medium);
    white-space: nowrap;
  }
  .md {
    height: var(--z-space-5);
    padding: 0 var(--z-space-2);
    font-size: var(--z-text-xs);
  }
  .sm {
    height: var(--z-space-4);
    /* The curve of a pill meets the dot sooner than a corner did. */
    padding: 0 var(--z-space-2);
    font-size: var(--z-text-2xs);
  }
  .badge[data-tone='accent'] {
    background: var(--z-accent-subtle);
    border-color: var(--z-accent-border);
    color: var(--z-accent);
  }
</style>
