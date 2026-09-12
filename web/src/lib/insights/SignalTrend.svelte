<script lang="ts">
  import type { SignalPoint } from './signals';
  let {
    points,
    label,
    tone = 'accent',
  }: { points: SignalPoint[]; label: string; tone?: string } = $props();
  let selected = $state<number | null>(null);
  const known = $derived(points.filter((p) => p.value !== null));
  const ceiling = $derived(Math.max(2, ...known.map((p) => p.value ?? 0)));
  const active = $derived(points[selected ?? points.length - 1]);
  const path = $derived.by(() => {
    let pen = false;
    return points
      .map((p, i) => {
        if (p.value === null) {
          pen = false;
          return '';
        }
        const op = pen ? 'L' : 'M';
        pen = true;
        return `${op}${36 + (i * 688) / Math.max(1, points.length - 1)},${130 - (p.value / ceiling) * 108}`;
      })
      .join(' ');
  });
  const time = (at: number) =>
    new Date(at).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
</script>

<div class="trend" style:--signal={`var(--z-${tone})`}>
  {#if known.length}
    <svg
      viewBox="0 0 744 154"
      role="img"
      aria-label={`${label}; ${known.length} observed minutes. Inspect the timeline below for exact values.`}
    >
      <line x1="36" x2="724" y1="22" y2="22" /><line x1="36" x2="724" y1="130" y2="130" />
      <text x="28" y="26" text-anchor="end">{ceiling}</text><text x="28" y="134" text-anchor="end"
        >0</text
      >
      <path
        d={path}
        fill="none"
        stroke="var(--signal)"
        stroke-width="2.5"
        vector-effect="non-scaling-stroke"
      />
      {#each points as point, i (point.at)}{#if point.value !== null}<circle
            cx={36 + (i * 688) / Math.max(1, points.length - 1)}
            cy={130 - (point.value / ceiling) * 108}
            r={selected === i ? 4 : 1.8}
            fill="var(--signal)"
          />{/if}{/each}
      {#if points[0]}<text x="36" y="150">{time(points[0].at)}</text
        >{/if}{#if points[points.length - 1]}<text x="724" y="150" text-anchor="end"
          >{time(points[points.length - 1]!.at)}</text
        >{/if}
    </svg>
  {:else}<p class="empty">No samples recorded in this hour yet.</p>{/if}
  <div class="scrub">
    <label
      ><span>Inspect {label.toLowerCase()}</span><input
        type="range"
        min="0"
        max={Math.max(0, points.length - 1)}
        value={selected ?? points.length - 1}
        oninput={(e) => (selected = Number(e.currentTarget.value))}
        aria-valuetext={active
          ? `${time(active.at)}: ${active.value === null ? 'not sampled' : active.value}`
          : 'No samples'}
      /></label
    ><output
      >{active
        ? `${time(active.at)} · ${active.value === null ? 'Not sampled' : active.value}`
        : 'Not sampled'}</output
    >
  </div>
  <div class="coverage" aria-label={`${known.length} of ${points.length} minutes observed`}>
    {#each points as point (point.at)}<span
        class:observed={point.value !== null}
        title={`${time(point.at)}: ${point.value ?? 'not sampled'}`}
      ></span>{/each}
  </div>
  <p class="note">{known.length} / {points.length} minutes observed · gaps mean no sample</p>
</div>

<style>
  svg {
    display: block;
    width: 100%;
    height: auto;
  }
  line {
    stroke: var(--z-border);
    stroke-dasharray: 3 4;
  }
  text {
    fill: var(--z-text-muted);
    font-size: 12px;
  }
  .scrub {
    display: flex;
    align-items: center;
    gap: var(--z-space-4);
    flex-wrap: wrap;
    margin-top: var(--z-space-3);
  }
  label {
    display: flex;
    align-items: center;
    gap: var(--z-space-3);
    flex: 1;
    font-size: var(--z-text-xs);
    min-width: 0;
  }
  label span {
    white-space: nowrap;
  }
  input {
    width: 100%;
    min-width: 70px;
    accent-color: var(--signal);
    height: var(--z-space-6);
  }
  output {
    font-size: var(--z-text-xs);
    font-variant-numeric: tabular-nums;
  }
  .coverage {
    display: flex;
    gap: var(--z-border-width);
    margin-top: var(--z-space-3);
    height: var(--z-space-2);
  }
  .coverage span {
    flex: 1;
    background: var(--z-surface-sunken);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-sm);
  }
  .coverage .observed {
    background: var(--signal);
    border-color: var(--signal);
  }
  .note,
  .empty {
    color: var(--z-text-muted);
    font-size: var(--z-text-xs);
  }
  .empty {
    padding: var(--z-space-5) 0;
  }
</style>
