<!--
  One list in the command centre's rail: its heading and count, and the look
  every row in it shares.

  Waiting, Needs attention and Just finished are three lists with one shape: a
  heading with a count, rows that are a mark, two lines of somebody else's
  text and a figure at the side, and an "and N more" under them. The shape
  lives here, so each list owns its look rather than borrowing it from the
  page, and a list drawn anywhere else (the component gallery) looks the same.
  The rows are the lists' own markup, passed in, so the row rules reach into
  them by class.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    /** The heading's id, which the section is labelled by. */
    id: string;
    title: string;
    /** Shown beside the title once there is a figure to show. */
    count?: number;
    /** True while the list's first read has not answered. Absent for a list that never loads. */
    busy?: boolean;
    children: Snippet;
  }
  let { id, title, count, busy, children }: Props = $props();
</script>

<section class="rail-section" aria-labelledby={id} aria-busy={busy}>
  <h2 {id}>
    {title}
    {#if count !== undefined}<span class="count">{count}</span>{/if}
  </h2>
  {@render children()}
</section>

<style>
  .rail-section {
    display: grid;
    gap: var(--z-space-2);
    min-width: 0;
  }
  h2 {
    display: flex;
    align-items: baseline;
    gap: var(--z-space-2);
    margin: 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text);
  }
  .count {
    color: var(--z-text-muted);
    font-variant-numeric: tabular-nums;
  }
  .rail-section > :global(ul) {
    display: grid;
    gap: var(--z-space-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .rail-section :global(.row) {
    display: flex;
    align-items: center;
    gap: var(--z-space-2);
    box-sizing: border-box;
    width: 100%;
    min-width: 0;
    padding: var(--z-space-2);
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface-raised);
    color: var(--z-text);
    font: inherit;
    text-align: left;
    text-decoration: none;
    cursor: pointer;
    animation: rail-arrive var(--z-motion-base) var(--z-ease);
    transition: border-color var(--z-motion-fast) var(--z-ease);
  }
  .rail-section :global(.row:hover),
  .rail-section :global(.row:focus-visible) {
    border-color: var(--z-accent);
  }
  .rail-section :global(.row:focus-visible),
  .rail-section :global(.more:focus-visible) {
    outline: none;
    box-shadow: var(--z-focus-ring);
  }
  .rail-section :global(.mark) {
    display: inline-flex;
    flex: none;
  }
  .rail-section :global(.what) {
    display: grid;
    flex: 1;
    min-width: 0;
  }
  /* Names are somebody else's text: cut, never allowed to widen the rail. */
  .rail-section :global(.name),
  .rail-section :global(.where) {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .rail-section :global(.name) {
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-medium);
  }
  .rail-section :global(.where) {
    font-family: var(--z-font-mono);
    font-size: var(--z-text-2xs);
    color: var(--z-text-muted);
  }
  .rail-section :global(.side) {
    display: grid;
    flex: none;
    justify-items: end;
    font-size: var(--z-text-xs);
  }
  .rail-section :global(.empty) {
    margin: 0;
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
  .rail-section :global(.more) {
    justify-self: start;
    padding: 0;
    border: 0;
    background: none;
    color: var(--z-accent);
    font: inherit;
    font-size: var(--z-text-sm);
    text-decoration: underline;
    text-underline-offset: var(--z-underline-offset);
    cursor: pointer;
  }
  @keyframes rail-arrive {
    from {
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .rail-section :global(.row) {
      animation: none;
    }
  }
  @media (pointer: coarse) {
    .rail-section :global(.row) {
      min-height: var(--z-control-touch);
    }
  }
</style>
