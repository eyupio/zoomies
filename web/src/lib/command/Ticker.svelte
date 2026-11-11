<!--
  The ticker: what has happened lately, newest first, moving slowly along the
  foot of the page.

  Content that moves for more than five seconds has to be stoppable, so it
  stops while the pointer is over it, while focus is inside it, and for as
  long as the Pause button is pressed, and while the page is not live, since a
  feed that has stopped arriving should not look as if it were still moving.
  The strip's speed is the same whatever it holds: its duration is worked out
  from its width, so twenty long entries do not race past and three short ones
  do not crawl. Under reduced motion it is the latest three entries, still.
-->
<script lang="ts">
  import { Pause } from '@lucide/svelte';
  import Button from '$lib/components/Button.svelte';
  import Tooltip from '$lib/components/Tooltip.svelte';
  import type { FeedEntry } from '$lib/feed/entries';
  import { toMillis } from '$lib/format';
  import { toneTokens } from '$lib/status';
  import { clock as pageClock } from './clock.svelte';
  import { tickerText, tickerTip } from './tips';

  interface Props {
    entries: readonly FeedEntry[];
    /**
     * The assistant's button is in the corner the strip ends in, so the strip
     * ends before it rather than running underneath.
     */
    clearAssistant?: boolean;
  }
  let { entries, clearAssistant = false }: Props = $props();

  const SHOWN = 20;
  // Pixels the strip moves in one --z-motion-slow, so the duration is the
  // width over this many slow beats: about forty pixels a second.
  const STRIDE = 12.8;

  const latest = $derived(entries.slice(0, SHOWN));
  let paused = $state(false);
  let width = $state(0);
  const beats = $derived(Math.max(1, Math.round(width / STRIDE)));
  /*
    The duration the strip is drawn with. A browser places a running animation
    by the time since it began over its duration, so changing the duration
    mid-lap moves the strip to wherever the new duration says it should be: a
    jump every time an entry arrives. The new duration is taken only at the end
    of a lap, where the strip is back at its start, and the lap is begun again
    there so it starts from nought rather than from the elapsed time.
  */
  let drawn = $state<number | null>(null);
  $effect(() => {
    if (drawn === null && width > 0) drawn = beats;
  });
  function onLap(event: AnimationEvent): void {
    // A status mark inside an entry animates too, and its laps bubble here;
    // only the strip's own lap is the moment to begin again.
    if (event.target !== event.currentTarget || drawn === beats) return;
    drawn = beats;
    const track = event.currentTarget as HTMLElement;
    // Set here as well as by the template, which would only set it after the
    // lap below had already begun with the old duration.
    track.style.setProperty('--ticker-beats', String(beats));
    track.style.animation = 'none';
    // Reading a layout value makes the browser drop the old animation before
    // the next line starts a new one.
    void track.offsetWidth;
    track.style.animation = '';
  }

  // One copy's width is one lap of the loop; the copy behind it is the same.
  function measure(copy: boolean, w: number): void {
    if (!copy) width = w;
  }

  const time = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' });
  function clock(at: string): string {
    const ms = toMillis(at);
    return ms === null ? '' : time.format(ms);
  }
</script>

{#snippet line(entry: FeedEntry)}
  {@const tone = toneTokens(entry.tone)}
  <time datetime={entry.at}>{clock(entry.at)}</time>
  <span class="mark" style:color={tone.colour}><entry.icon size={14} aria-hidden="true" /></span>
  <span class="what">{entry.title}</span>
  {#if entry.target}<span class="target">{entry.target.label}</span>{/if}
{/snippet}

{#snippet item(entry: FeedEntry)}
  {#if entry.target?.href}
    <a class="entry" href={entry.target.href}>{@render line(entry)}</a>
  {:else}
    <span class="entry">{@render line(entry)}</span>
  {/if}
{/snippet}

<!--
  Only the first copy carries a card. The second is inert decoration, which a
  pointer passes through, and a second description for every entry would read
  each one twice. Hovering already holds the strip still, and the bubble is
  inside the strip, so the pointer on the card keeps it held.
-->
{#snippet list(copy: boolean)}
  <ul class="entries" bind:clientWidth={null, (w: number) => measure(copy, w)}>
    {#each latest as entry (entry.id)}
      <li>
        {#if copy}
          {@render item(entry)}
        {:else}
          <Tooltip
            bare
            quiet
            detail={() => tickerTip(entry, pageClock.now)}
            text={tickerText(entry)}>{@render item(entry)}</Tooltip
          >
        {/if}
      </li>
    {/each}
  </ul>
{/snippet}

<section
  class="ticker"
  class:clear={clearAssistant}
  aria-labelledby="command-ticker"
  data-paused={paused}
>
  <h2 id="command-ticker" class="sr-only">Recent events</h2>
  {#if latest.length === 0}
    <p class="empty">Nothing has happened yet.</p>
  {:else}
    <span class="control">
      <Button
        variant="ghost"
        size="sm"
        icon={Pause}
        pressed={paused}
        help={paused ? 'Let the events move again.' : 'Hold the events still.'}
        onclick={() => (paused = !paused)}
      >
        Pause
      </Button>
    </span>
    <div class="viewport">
      <div class="track" style:--ticker-beats={drawn ?? beats} onanimationiteration={onLap}>
        {@render list(false)}
        <!-- A second copy follows the first, so the strip loops without a gap.
             It is decoration: hidden from assistive technology and from Tab. -->
        <div class="copy" aria-hidden="true" inert>{@render list(true)}</div>
      </div>
    </div>
  {/if}
</section>

<style>
  .ticker {
    display: flex;
    align-items: center;
    gap: var(--z-space-3);
    min-width: 0;
    padding: var(--z-space-2) var(--z-space-6);
    border-top: var(--z-border-width) solid var(--z-border);
    background: var(--z-surface);
  }
  .ticker.clear {
    padding-inline-end: var(--z-eli-lane);
  }
  .control {
    flex: none;
  }
  .viewport {
    flex: 1;
    min-width: 0;
    overflow: hidden;
  }
  .track {
    display: flex;
    width: max-content;
    animation: ticker calc(var(--z-motion-slow) * var(--ticker-beats)) linear infinite;
  }
  .viewport:hover .track,
  .viewport:focus-within .track,
  [data-paused='true'] .track,
  :global(.command[data-live='false']) .track {
    animation-play-state: paused;
  }
  @keyframes ticker {
    to {
      transform: translateX(-50%);
    }
  }
  .entries {
    display: flex;
    flex: none;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .entry {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    max-width: 40ch;
    padding: var(--z-space-1) var(--z-space-4);
    color: var(--z-text);
    font-size: var(--z-text-sm);
    text-decoration: none;
    white-space: nowrap;
  }
  a.entry:hover .what {
    text-decoration: underline;
    text-underline-offset: var(--z-underline-offset);
  }
  a.entry:focus-visible {
    outline: none;
    border-radius: var(--z-radius-md);
    box-shadow: var(--z-focus-ring);
  }
  time {
    color: var(--z-text-muted);
    font-variant-numeric: tabular-nums;
  }
  .mark {
    display: inline-flex;
    flex: none;
  }
  /* A target's name is somebody else's text: cut, never allowed to widen. */
  .what,
  .target {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .target {
    color: var(--z-text-muted);
  }
  .empty {
    margin: 0;
    font-size: var(--z-text-sm);
    color: var(--z-text-muted);
  }
  @media (prefers-reduced-motion: reduce) {
    .track {
      animation: none;
    }
    .control,
    .copy,
    .entries li:nth-child(n + 4) {
      display: none;
    }
  }
  @media (max-width: 768px) {
    .ticker {
      padding: var(--z-space-2) var(--z-space-4);
    }
    .ticker.clear {
      padding-inline-end: var(--z-eli-lane);
    }
  }
</style>
