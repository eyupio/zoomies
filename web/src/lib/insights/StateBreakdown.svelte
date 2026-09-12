<!--
  A proportional bar of states with exact counts: how the live runners, the
  provisioning queue or the last hour's completions divide up.

  Each segment is a way in as well as a share -- a link to the list it
  counts, with a tooltip that says the count, the share and what the state
  means -- so the bar is read and then used rather than read and then
  searched for. The legend under it carries the same figures as text, which
  is what the bar is for anyone who cannot see it.
-->
<script lang="ts">
  import { formatNumber, formatPercent } from '$lib/format';
  import Tooltip from '$lib/components/Tooltip.svelte';
  export interface Segment {
    label: string;
    value: number;
    tone: 'busy' | 'idle' | 'pending' | 'danger' | 'neutral' | 'accent';
    href?: string;
    /** One line on what the state means, for the tooltip. */
    hint?: string;
  }
  let {
    segments,
    label,
    noun = 'items',
  }: {
    segments: Segment[];
    label: string;
    /** What is being counted, in the plural: "live runners", "completed jobs". */
    noun?: string;
  } = $props();
  const total = $derived(segments.reduce((n, s) => n + s.value, 0));
  const share = (value: number) => (total > 0 ? value / total : 0);
  function sentence(segment: Segment): string {
    const part = `${segment.label}: ${formatNumber(segment.value)} of ${formatNumber(total)} ${noun} (${formatPercent(share(segment.value))})`;
    return segment.hint ? `${part}. ${segment.hint}` : part;
  }
</script>

<div class="breakdown" aria-label={label}>
  <p class="sr-only">
    {label}: {segments.map((s) => `${s.label} ${formatNumber(s.value)}`).join(', ')}.
  </p>
  <div class="bar" aria-hidden={total === 0 ? 'true' : undefined}>
    {#each segments as segment (segment.label)}
      {#if segment.value > 0}
        <span class="seg" style:flex={segment.value} data-tone={segment.tone}>
          <Tooltip text={sentence(segment)}>
            {#snippet content()}
              <strong>{segment.label}</strong>
              <span class="figures"
                >{formatNumber(segment.value)} of {formatNumber(total)} · {formatPercent(
                  share(segment.value),
                )}</span
              >
              {#if segment.hint}<span class="hint">{segment.hint}</span>{/if}
            {/snippet}
            {#if segment.href}
              <a class="fill" href={segment.href} aria-label="{segment.label} {noun}"></a>
            {:else}
              <span class="fill"></span>
            {/if}
          </Tooltip>
        </span>
      {/if}
    {/each}
  </div>
  <div class="legend">
    {#each segments as segment (segment.label)}
      <div class="item" data-tone={segment.tone}>
        <i aria-hidden="true"></i>{#if segment.href}<a href={segment.href}>{segment.label}</a
          >{:else}<span>{segment.label}</span>{/if}<strong>{formatNumber(segment.value)}</strong
        ><small>{total > 0 ? formatPercent(share(segment.value)) : '--'}</small>
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
  .seg {
    display: flex;
    min-width: var(--z-nudge-2);
    /* A live count that moves glides rather than jumps. */
    transition: flex-grow var(--z-motion-base) var(--z-ease);
  }
  .seg :global(.tip-wrap) {
    display: block;
    width: 100%;
    height: 100%;
  }
  .fill {
    display: block;
    width: 100%;
    height: 100%;
    background: var(--tone);
    transition: filter var(--z-motion-fast) var(--z-ease);
  }
  a.fill:hover,
  a.fill:focus-visible {
    filter: brightness(1.15);
  }
  a.fill:focus-visible {
    outline-offset: calc(-1 * var(--z-focus-width));
  }
  [data-tone='busy'] {
    --tone: var(--z-busy);
  }
  [data-tone='idle'] {
    --tone: var(--z-idle);
  }
  [data-tone='pending'] {
    --tone: var(--z-pending);
  }
  [data-tone='danger'] {
    --tone: var(--z-danger);
  }
  [data-tone='neutral'] {
    --tone: var(--z-neutral);
  }
  [data-tone='accent'] {
    --tone: var(--z-accent);
  }
  .figures,
  .hint {
    display: block;
  }
  .figures {
    font-variant-numeric: tabular-nums;
  }
  .hint {
    color: var(--z-text-muted);
  }
  .legend {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
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
    background: var(--tone);
    flex: none;
  }
  .item > span,
  .item > a {
    color: var(--z-text-muted);
  }
  .item > a:hover {
    color: var(--z-accent);
  }
  strong {
    margin-left: auto;
    color: var(--z-text);
    font-variant-numeric: tabular-nums;
  }
  small {
    min-width: 3ch;
    text-align: right;
    color: var(--z-text-subtle);
    font-size: var(--z-text-2xs);
    font-variant-numeric: tabular-nums;
  }
  p {
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    margin-bottom: 0;
  }
</style>
