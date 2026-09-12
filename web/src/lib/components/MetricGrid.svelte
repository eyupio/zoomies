<script lang="ts">
  export interface Metric {
    label: string;
    value: string;
    detail: string;
    tone?: 'neutral' | 'success' | 'danger' | 'warning';
  }
  let { items }: { items: Metric[] } = $props();
</script>

<dl class="metrics">
  {#each items as item (item.label)}
    <div class="metric" data-tone={item.tone ?? 'neutral'}>
      <dt>{item.label}</dt>
      <dd>{item.value}</dd>
      <p>{item.detail}</p>
    </div>
  {/each}
</dl>

<style>
  .metrics {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(155px, 1fr));
    gap: var(--z-space-3);
    margin: 0 0 var(--z-space-5);
  }
  .metric {
    min-width: 0;
    padding: var(--z-space-4);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
  }
  dt {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  dd {
    margin: var(--z-space-2) 0;
    font-size: var(--z-text-2xl);
    font-weight: var(--z-weight-semibold);
    font-variant-numeric: tabular-nums;
    letter-spacing: var(--z-tracking-tight);
  }
  p {
    margin: 0;
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
  }
  [data-tone='success'] dd {
    color: var(--z-idle);
  }
  [data-tone='danger'] dd {
    color: var(--z-danger);
  }
  [data-tone='warning'] dd {
    color: var(--z-pending);
  }
</style>
