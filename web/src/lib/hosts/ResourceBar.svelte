<!--
  How much of one resource this host has already promised away.

  A bar rather than two numbers, because "how full is this machine" is a
  question about a proportion and a proportion is what a bar is for: 12 of 14
  CPUs reads as a fraction only after arithmetic, and the answer an operator
  wants -- nearly full, half empty -- is in the shape.

  The value under it is still spelled out, because a bar cannot be read aloud
  and cannot be compared between two hosts of different sizes.
-->
<script lang="ts">
  interface Props {
    label: string;
    /** What the live runners have promised away. */
    used: number;
    /** What the scheduler may place onto: the machine less its reserve. */
    total: number;
    /** The used and total figures, already in the units a person reads. */
    text: string;
    /** What the operator holds back, spelled out for the title. */
    hint?: string;
  }

  let { label, used, total, text, hint = '' }: Props = $props();

  const pct = $derived(total > 0 ? Math.min(100, Math.max(0, (used / total) * 100)) : 0);
  // Above this, the next runner is the one that does not fit. It is the same
  // judgement the disk figure makes on the card above: worth attending to
  // before it becomes the reason a queue stops draining.
  const tight = $derived(total > 0 && used / total >= 0.9);
  const summary = $derived(`${label}: ${text}${hint ? `. ${hint}` : ''}`);
</script>

<div class="row" title={summary}>
  <span class="label">{label}</span>
  <div class="track" role="img" aria-label={summary}>
    <span class="fill" class:tight style="width: {pct}%"></span>
  </div>
  <span class="value tabular" class:tight>{text}</span>
</div>

<style>
  .row {
    display: grid;
    grid-template-columns: 3.5rem 1fr auto;
    align-items: center;
    gap: var(--z-space-2);
    min-width: 0;
  }
  .label {
    font-size: var(--z-text-2xs);
    color: var(--z-text-muted);
  }
  .track {
    position: relative;
    height: 6px;
    border-radius: var(--z-radius-full);
    background: var(--z-surface-sunken);
    overflow: hidden;
  }
  .fill {
    position: absolute;
    inset-block: 0;
    left: 0;
    border-radius: var(--z-radius-full);
    background: var(--z-neutral);
  }
  /* The same pending hue the disk figure takes when it is nearly gone: one
     colour for "this is what will stop the next runner", wherever it appears. */
  .fill.tight {
    background: var(--z-pending);
  }
  .value {
    font-size: var(--z-text-2xs);
    color: var(--z-text-subtle);
  }
  .value.tight {
    color: var(--z-pending);
    font-weight: var(--z-weight-medium);
  }
</style>
