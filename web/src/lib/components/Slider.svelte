<!--
  A slider that moves between notches.

  The input is the browser's own range control, so the keyboard, the screen
  reader and the phone's thumb all work without a line of script; what is
  drawn is the row of notches beneath it and the words at the ones worth
  naming -- "recommended", "one per core" -- because a bare number line
  leaves the operator to work out what four means on this machine.

  The value is one of `values`, and the control moves by index, so a memory
  slider can step 512 MB, 1 GB, 2 GB rather than crawling through megabytes.
  `valuetext` is what a screen reader says at each notch and what the
  readout shows, so the two never disagree.
-->
<script lang="ts">
  interface Mark {
    value: number;
    label: string;
    /** The notch the recommendation sits on, drawn with a ring. */
    recommended?: boolean;
  }
  interface Props {
    values: readonly number[];
    value: number;
    /** The accessible name. */
    label: string;
    /** The words for a value, on the readout and for assistive technology. */
    valuetext: (value: number) => string;
    marks?: readonly Mark[];
    /** The control's tone: `warning` past the recommendation. */
    tone?: 'accent' | 'warning';
    id?: string;
    describedBy?: string;
    disabled?: boolean;
    /** Print the words beside the control. Off where an exact field sits there. */
    readout?: boolean;
    onchange?: (value: number) => void;
  }

  let {
    values,
    value = $bindable(),
    label,
    valuetext,
    marks = [],
    tone = 'accent',
    id,
    describedBy,
    disabled = false,
    readout = true,
    onchange,
  }: Props = $props();

  /** The nearest notch to the value, so a value between two lands on one. */
  const index = $derived.by(() => {
    let best = 0;
    values.forEach((v, i) => {
      if (Math.abs(v - value) < Math.abs((values[best] ?? 0) - value)) best = i;
    });
    return best;
  });
  const share = (i: number) => (values.length > 1 ? (100 * i) / (values.length - 1) : 0);

  /**
   * Where each mark's words go. Two marks a few percent apart -- "none" and
   * "recommended" on an eight-core machine -- would print over each other,
   * so a mark too close to the one before it drops to a second row.
   */
  const placed = $derived.by(() => {
    const out: { value: number; label: string; recommended?: boolean; at: number; row: number }[] =
      [];
    let lastAt = -Infinity;
    let lastRow = 0;
    for (const mark of [...marks].sort((a, b) => a.value - b.value)) {
      const i = values.indexOf(mark.value);
      if (i === -1) continue;
      const at = share(i);
      const row = at - lastAt < 18 && lastRow === 0 ? 1 : 0;
      out.push({ ...mark, at, row });
      lastAt = at;
      lastRow = row;
    }
    return out;
  });

  function move(event: Event): void {
    const next = values[Number((event.currentTarget as HTMLInputElement).value)];
    if (next === undefined || next === value) return;
    value = next;
    onchange?.(next);
  }
</script>

<div class="slider" class:warning={tone === 'warning'} class:disabled>
  <div class="row">
    <input
      type="range"
      {id}
      min="0"
      max={Math.max(0, values.length - 1)}
      step="1"
      value={index}
      {disabled}
      aria-label={label}
      aria-valuetext={valuetext(values[index] ?? 0)}
      aria-describedby={describedBy}
      style:--fill="{share(index)}%"
      oninput={move}
    />
    {#if readout}<output aria-hidden="true">{valuetext(values[index] ?? 0)}</output>{/if}
  </div>
  {#if placed.length > 0}
    <div class="marks" class:two-rows={placed.some((m) => m.row === 1)} aria-hidden="true">
      {#each placed as mark (mark.value)}
        <span
          class="mark"
          class:recommended={mark.recommended}
          class:reached={mark.value <= (values[index] ?? 0)}
          class:below={mark.row === 1}
          class:first={mark.at === 0}
          class:last={mark.at === 100}
          style:left="{mark.at}%"
        >
          <span class="notch"></span>
          <span class="text">{mark.label}</span>
        </span>
      {/each}
    </div>
  {/if}
</div>

<style>
  .slider {
    --track: var(--z-surface-sunken);
    --thumb: var(--z-accent);
    --readout: 8ch + var(--z-space-4);
    display: flex;
    flex-direction: column;
    gap: var(--z-space-1);
    /* Room for the words under the notches, which hang below the track. */
    padding-bottom: var(--z-space-6);
  }
  .slider:has(.two-rows) {
    padding-bottom: calc(var(--z-space-6) + var(--z-space-4));
  }
  .slider:not(:has(output)) {
    --readout: 0px;
  }
  .slider.warning {
    --thumb: var(--z-pending);
  }
  .slider.disabled {
    opacity: 0.6;
  }
  .row {
    display: flex;
    align-items: center;
    gap: var(--z-space-4);
  }
  output {
    min-width: 8ch;
    text-align: right;
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-semibold);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  /* The track is painted by the input itself: the filled share to the left
     of the thumb in the control's tone, the rest sunken. One gradient, both
     engines, no pseudo-element for each. */
  input {
    -webkit-appearance: none;
    appearance: none;
    flex: 1;
    min-width: 0;
    height: var(--z-space-6);
    margin: 0;
    background: transparent;
    cursor: pointer;
  }
  input:disabled {
    cursor: not-allowed;
  }
  input::-webkit-slider-runnable-track {
    height: var(--z-space-1);
    border-radius: var(--z-radius-full);
    background: linear-gradient(
      to right,
      var(--thumb) 0 var(--fill),
      var(--track) var(--fill) 100%
    );
  }
  input::-moz-range-track {
    height: var(--z-space-1);
    border-radius: var(--z-radius-full);
    background: var(--track);
  }
  input::-moz-range-progress {
    height: var(--z-space-1);
    border-radius: var(--z-radius-full);
    background: var(--thumb);
  }
  input::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: var(--z-space-5);
    height: var(--z-space-5);
    margin-top: calc((var(--z-space-1) - var(--z-space-5)) / 2);
    border: var(--z-border-width-thick) solid var(--z-surface);
    border-radius: var(--z-radius-full);
    background: var(--thumb);
    box-shadow: var(--z-shadow-md);
    transition: transform var(--z-motion-fast) var(--z-ease);
  }
  input::-moz-range-thumb {
    width: var(--z-space-5);
    height: var(--z-space-5);
    border: var(--z-border-width-thick) solid var(--z-surface);
    border-radius: var(--z-radius-full);
    background: var(--thumb);
    box-shadow: var(--z-shadow-md);
  }
  input:active::-webkit-slider-thumb {
    transform: scale(1.15);
  }
  input:focus-visible {
    outline: none;
  }
  input:focus-visible::-webkit-slider-thumb {
    box-shadow: var(--z-focus-ring);
  }
  input:focus-visible::-moz-range-thumb {
    box-shadow: var(--z-focus-ring);
  }
  /* The notches sit under the track, inset by half a thumb on each side so
     they line up with where the thumb's centre actually goes. */
  .marks {
    position: relative;
    height: 0;
    margin: 0 calc(var(--readout) + var(--z-space-5) / 2) 0 calc(var(--z-space-5) / 2);
  }
  .mark {
    position: absolute;
    top: calc(-1 * var(--z-space-3));
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--z-nudge-2);
    transform: translateX(-50%);
    color: var(--z-text-subtle);
    font-size: var(--z-text-2xs);
    line-height: 1.2;
    white-space: nowrap;
  }
  .mark.first {
    align-items: flex-start;
    transform: none;
  }
  .mark.last {
    align-items: flex-end;
    transform: translateX(-100%);
  }
  /* The second row keeps its notch on the track and hangs its words lower,
     with a hairline down to them so the words still belong to a notch. */
  .mark.below .text {
    margin-top: var(--z-space-4);
    position: relative;
  }
  .mark.below .text::before {
    content: '';
    position: absolute;
    left: 50%;
    top: calc(-1 * var(--z-space-4));
    height: var(--z-space-4);
    border-left: var(--z-border-width) solid var(--z-border-strong);
  }
  .mark.below.first .text::before {
    left: var(--z-nudge-2);
  }
  .mark.below.last .text::before {
    left: auto;
    right: var(--z-nudge-2);
  }
  .notch {
    display: block;
    width: var(--z-nudge-2);
    height: var(--z-space-2);
    border-radius: var(--z-radius-full);
    background: var(--z-border-strong);
  }
  .mark.reached .notch {
    background: var(--thumb);
  }
  .mark.recommended .text {
    color: var(--z-text);
    font-weight: var(--z-weight-medium);
  }
  .mark.recommended .notch {
    width: var(--z-space-2);
    height: var(--z-space-2);
    box-shadow: 0 0 0 var(--z-border-width-thick) var(--z-accent-subtle);
    background: var(--z-accent);
  }
</style>
