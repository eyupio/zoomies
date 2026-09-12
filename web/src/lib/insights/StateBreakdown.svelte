<script lang="ts">
  import { formatNumber } from '$lib/format';
  export interface Segment {
    label: string;
    value: number;
    tone: 'busy' | 'idle' | 'pending' | 'danger' | 'neutral' | 'accent';
    href?: string;
  }
  let { segments, label }: { segments: Segment[]; label: string } = $props();
  const total = $derived(segments.reduce((n, s) => n + s.value, 0));
</script>

<div class="breakdown" aria-label={label}>
  <div class="bar" role="img" aria-label={segments.map((s) => `${s.label}: ${s.value}`).join(', ')}>
    {#each segments as segment (segment.label)}{#if segment.value > 0}<span
          style:width={`${(100 * segment.value) / total}%`}
          style:background={`var(--z-${segment.tone})`}
          title={`${segment.label}: ${segment.value}`}
        ></span>{/if}{/each}
  </div>
  <div class="legend">
    {#each segments as segment (segment.label)}
      <div class="item">
        <i style:background={`var(--z-${segment.tone})`}></i>{#if segment.href}<a
            href={segment.href}>{segment.label}</a
          >{:else}<span>{segment.label}</span>{/if}<strong>{formatNumber(segment.value)}</strong>
      </div>
    {/each}
  </div>
  {#if total === 0}<p>No recorded activity in this scope.</p>{/if}
</div>

<style>
  .bar {
    display: flex;
    gap: var(--z-border-width);
    height: var(--z-space-4);
    overflow: hidden;
    border-radius: var(--z-radius-full);
    background: var(--z-surface-sunken);
  }
  .bar span {
    min-width: var(--z-border-width);
  }
  .legend {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(125px, 1fr));
    gap: var(--z-space-3);
    margin-top: var(--z-space-4);
  }
  .item {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    font-size: var(--z-text-xs);
    min-width: 0;
  }
  i {
    width: var(--z-space-2);
    height: var(--z-space-2);
    border-radius: var(--z-radius-full);
    flex: none;
  }
  .item > span,
  a {
    color: var(--z-text-muted);
  }
  a:hover {
    color: var(--z-accent);
  }
  strong {
    margin-left: auto;
    color: var(--z-text);
    font-variant-numeric: tabular-nums;
  }
  p {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    margin-bottom: 0;
  }
</style>
