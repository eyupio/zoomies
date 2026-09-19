<script lang="ts">
  import type { Runner } from '$lib/api/types';
  import { cpuResourceStatus } from '$lib/status';
  import Badge from '$lib/components/Badge.svelte';

  interface Props {
    resource: Runner['cpu_resource'];
    compact?: boolean;
  }

  let { resource, compact = false }: Props = $props();
  const status = $derived(cpuResourceStatus(resource?.state, resource?.label));
  const figures = $derived(
    resource
      ? `${resource.guaranteed_cpus ?? 0} guaranteed · ${resource.current_cpus ?? 0} now · ${resource.ceiling_cpus ?? 0} ceiling`
      : '',
  );
</script>

{#if resource}
  <span class:compact class="cpu-resource" title={figures}>
    <Badge {status} icon dot={false} size="sm" />
    {#if !compact}<span class="figures">{figures}</span>{/if}
  </span>
{/if}

<style>
  .cpu-resource {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
  .figures {
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
    font-variant-numeric: tabular-nums;
  }
</style>
