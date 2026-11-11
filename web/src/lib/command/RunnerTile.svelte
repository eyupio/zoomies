<!--
  One cell of a host lane: a runner, a runner on its way, or the room left on
  the host, counted in one cell.

  The whole tile is one link and holds no second control, so a wall display can
  be driven with Tab and every tile opens in a new tab like any other link. It
  is one height whatever it holds. A runner arriving or leaving moves the tiles
  after it along by one; each fades in where it lands, and nothing slides.

  Everything a workflow names (the job, the repository, the branch) is
  somebody else's text: it is rendered as text and cut with an ellipsis, so a
  four-hundred-character job name cannot widen the cell it sits in.
-->
<script lang="ts">
  import type { Runner } from '$lib/api/types';
  import { formatDuration, NO_VALUE } from '$lib/format';
  import { readableJobText } from '$lib/jobs/name';
  import { dogPhase } from '$lib/mascot/dog-motion';
  import RunnerStatus from '$lib/runners/RunnerStatus.svelte';
  import { cpuShare } from './chart';
  import { clock } from './clock.svelte';
  import { freeSlots, type Cell } from './lanes';
  import { elapsedMs } from './time';

  interface Props {
    cell: Cell;
    /** The host the lane is for, so a free cell says where it leads. */
    hostName: string;
  }
  let { cell, hostName }: Props = $props();

  const STEP: Partial<Record<NonNullable<Runner['state']>, string>> = {
    provisioning: 'Provisioning',
    registering: 'Registering',
  };

  const runner = $derived(cell.runner);
  const job = $derived(runner?.current_job);
  const title = $derived.by(() => {
    if (!runner) return freeSlots(cell.count ?? 0);
    if (cell.kind === 'starting') return STEP[runner.state ?? 'provisioning'] ?? 'Starting';
    if (job) return readableJobText(job.job_name) || 'A job';
    if (runner.state === 'draining') return 'Draining';
    if (runner.state === 'failed') return 'Failed';
    return 'Idle';
  });
  const age = $derived.by(() => {
    if (!runner) return null;
    const since = job ? (job.started_at ?? undefined) : runner.created_at;
    const ms = elapsedMs(since, clock.now);
    return ms === null ? NO_VALUE : formatDuration(ms);
  });
  const where = $derived(job ? [job.repo, job.head_branch].filter(Boolean).join(' · ') : '');
  const share = $derived(runner ? cpuShare(runner) : null);
  const busy = $derived(runner?.state === 'busy');
</script>

{#if runner}
  <a
    class="tile"
    class:failed={runner.state === 'failed'}
    class:starting={cell.kind === 'starting'}
    href={cell.href}
    data-testid="command-tile"
    data-state={runner.state}
    style:--phase={dogPhase(runner.id ?? cell.key)}
  >
    <span class="line">
      <RunnerStatus mark {runner} />
      <span class="title">{title}</span>
      <span class="age">{age}</span>
    </span>
    {#if where}<span class="where">{where}</span>{/if}
    <span
      class="bar"
      class:sweep={busy}
      class:unknown={share === null}
      style:--share={share ?? 0}
      aria-hidden="true"
    ></span>
  </a>
{:else}
  <a
    class="tile free"
    href={cell.href}
    data-testid="command-free"
    aria-label="{title} on {hostName}"
  >
    <span class="line">
      <RunnerStatus mark runner={{ id: cell.key, state: 'idle' }} />
      <span class="title">{title}</span>
    </span>
  </a>
{/if}

<style>
  .tile {
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    box-sizing: border-box;
    min-width: 0;
    height: var(--z-command-tile-height);
    padding: var(--z-space-1) var(--z-space-2);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface-raised);
    color: var(--z-text);
    text-decoration: none;
    animation: arrive var(--z-motion-base) var(--z-ease);
    transition:
      border-color var(--z-motion-fast) var(--z-ease),
      background-color var(--z-motion-fast) var(--z-ease);
  }
  .tile:hover,
  .tile:focus-visible {
    border-color: var(--z-accent);
  }
  .tile:focus-visible {
    outline: none;
    box-shadow: var(--z-focus-ring);
  }
  .tile.failed {
    border-color: var(--z-danger);
    background: var(--z-danger-subtle);
  }
  .tile.free {
    justify-content: center;
    border-style: dashed;
    background: transparent;
    color: var(--z-text-muted);
  }
  .line {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    min-width: 0;
  }
  .title,
  .where {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .title {
    font-size: var(--z-text-xs);
    line-height: var(--z-leading-2xs);
    font-weight: var(--z-weight-medium);
  }
  .starting .title {
    color: var(--z-pending);
  }
  .failed .title {
    color: var(--z-danger);
  }
  .age {
    flex: none;
    font-size: var(--z-text-2xs);
    line-height: var(--z-leading-2xs);
    color: var(--z-text-muted);
    font-variant-numeric: tabular-nums;
  }
  .where {
    flex: none;
    font-family: var(--z-font-mono);
    font-size: var(--z-text-2xs);
    line-height: var(--z-leading-2xs);
    color: var(--z-text-muted);
  }
  .bar {
    position: relative;
    flex: none;
    height: var(--z-space-1);
    overflow: hidden;
    border-radius: var(--z-radius-full);
    background: var(--z-surface-sunken);
  }
  .bar::before {
    content: '';
    position: absolute;
    inset: 0;
    border-radius: inherit;
    background: var(--z-busy);
    transform: scaleX(var(--share));
    transform-origin: left;
    transition: transform var(--z-motion-slow) var(--z-ease);
  }
  .bar.unknown::before {
    display: none;
  }
  /* The sweep is a light passing along a working bar, offset for each runner
     so a deck of busy tiles does not pulse in step. */
  .bar.sweep::after {
    content: '';
    position: absolute;
    inset: 0;
    width: 30%;
    background: linear-gradient(90deg, transparent, var(--z-busy-border), transparent);
    animation: sweep calc(var(--z-motion-slow) * 8) linear infinite;
    animation-delay: calc(var(--phase) * var(--z-motion-slow) * -8);
  }
  @keyframes sweep {
    from {
      transform: translateX(-100%);
    }
    to {
      transform: translateX(340%);
    }
  }
  @keyframes arrive {
    from {
      opacity: 0;
    }
  }
  /* A page that has stopped hearing from the controller cannot vouch for any
     of this work, so nothing on the tile moves as if it could: the sweep goes
     and the status mark holds its pose. The page's root says when. */
  :global([data-live='false']) .bar.sweep::after {
    display: none;
  }
  :global([data-live='false']) .line :global(*) {
    animation: none;
  }
  @media (prefers-reduced-motion: reduce) {
    .tile {
      animation: none;
    }
    .bar.sweep::after {
      display: none;
    }
  }
  @media (pointer: coarse) {
    .tile {
      min-height: var(--z-control-touch);
    }
  }
</style>
