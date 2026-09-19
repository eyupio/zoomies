<!--
  What has happened to this fleet lately, newest first.

  One list for nine kinds of news, because an operator asking "what did I
  miss?" should not have to ask it of nine pages. Every line is the same four
  things -- a mark in one of the six status tones, what happened, what it
  happened to, and when -- and under it, where there is one, the controller's
  own sentence: the scheduler's reason for a decision, the throttle's
  explanation, the hypervisor's complaint. Those are printed verbatim, because
  paraphrasing the one sentence that explains why a runner exists is how a
  dashboard stops being trustworthy.

  What it carries is the operator's to choose, on Settings -> Events, and
  whenever that is not everything the panel counts what it is showing -- "7 of
  9 kinds" -- rather than leaving the rest out in silence: a feed that quietly
  omitted things would be worse than one that shows too much.
-->
<script lang="ts">
  import { History, SlidersHorizontal } from '@lucide/svelte';
  import { pluralise } from '$lib/format';
  import { feed } from '$lib/state/feed.svelte';
  import { fleet } from '$lib/state/fleet.svelte';
  import { toneTokens } from '$lib/status';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import RelativeTime from '$lib/components/RelativeTime.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import Panel from '$lib/components/Panel.svelte';

  interface Props {
    loading?: boolean;
    class?: string;
  }

  let { loading = false, class: className = '' }: Props = $props();

  /**
   * How much of the recent past fits on a dashboard before it is a log viewer.
   * The panel scrolls inside its column on a desktop, so this is about the
   * phone, where everything below it is a scroll further away.
   */
  const SHOWN = 12;

  const entries = $derived(feed.entries.slice(0, SHOWN));
  const categories = $derived(feed.categories);
  const hidden = $derived(feed.hidden);
  const everything = $derived(hidden === 0);
  const hasFleet = $derived(fleet.pools.length > 0 || fleet.hosts.length > 0);
</script>

<!-- `scroll`: on a desktop the Overview cuts this panel to the height of the
     column beside it, and the entries that do not fit are a scroll away
     rather than a screen of blank space under the pools. -->
<Panel
  title="Recent events"
  description={everything
    ? 'Newest first.'
    : `Newest first. ${categories.length - hidden} of ${categories.length} kinds.`}
  class={className}
  flush
  scroll
>
  {#snippet actions()}
    <a class="choose" href="/settings/events">
      <SlidersHorizontal size={13} aria-hidden="true" />
      Choose
    </a>
  {/snippet}

  {#if loading}
    <p class="sr-only">Loading the fleet's recent events.</p>
    <ul class="feed" aria-hidden="true">
      {#each [0, 1, 2, 3] as row (row)}
        <li class="item">
          <span class="mark"></span>
          <div class="lines">
            <Skeleton width="60%" height="var(--z-text-xs)" />
            <Skeleton width="90%" height="var(--z-text-sm)" />
          </div>
        </li>
      {/each}
    </ul>
  {:else if entries.length === 0}
    <EmptyState
      icon={History}
      compact
      title={everything ? 'Nothing has happened yet' : 'Nothing in the kinds you are watching'}
      description={everything
        ? hasFleet
          ? 'A line is written here every time the scheduler decides something, a runner or a job fails, or a host, machine or pool changes underneath them.'
          : 'Once there is a pool and a host, this is where the fleet says what it has been doing.'
        : `${pluralise(hidden, 'kind')} of event ${hidden === 1 ? 'is' : 'are'} switched off for this browser. Choose above turns them back on.`}
    />
  {:else}
    <ul class="feed">
      {#each entries as entry (entry.id)}
        {@const tone = toneTokens(entry.tone)}
        <li class="item">
          <span class="mark" style="color: {tone.colour}; background: {tone.subtle}">
            <entry.icon size={14} aria-hidden="true" />
          </span>
          <div class="lines">
            <p class="meta">
              <span class="what">{entry.title}</span>
              {#if entry.target}
                <span aria-hidden="true" class="dot">·</span>
                {#if entry.target.href}
                  <a href={entry.target.href}>{entry.target.label}</a>
                {:else}
                  <span>{entry.target.label}</span>
                {/if}
              {/if}
              <span aria-hidden="true" class="dot">·</span>
              <RelativeTime value={entry.at} />
            </p>
            {#if entry.detail}<p class="detail">{entry.detail}</p>{/if}
          </div>
        </li>
      {/each}
    </ul>
  {/if}
</Panel>

<style>
  .choose {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-1);
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    text-decoration: none;
  }
  .choose:hover {
    color: var(--z-accent);
    text-decoration: underline;
  }
  .feed {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .item {
    display: flex;
    align-items: flex-start;
    gap: var(--z-space-3);
    padding: var(--z-space-3) var(--z-space-5);
    border-bottom: var(--z-border-width) solid var(--z-border);
  }
  .item:last-child {
    border-bottom: 0;
  }
  .mark {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: var(--z-space-6);
    height: var(--z-space-6);
    border-radius: var(--z-radius-full);
    background: var(--z-surface-sunken);
    color: var(--z-text-muted);
  }
  .lines {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-1);
    min-width: 0;
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--z-space-1) var(--z-space-2);
    margin: 0;
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
  }
  .what {
    font-weight: var(--z-weight-medium);
    color: var(--z-text);
  }
  .dot {
    color: var(--z-text-subtle);
  }
  .meta a {
    color: var(--z-accent);
    text-decoration: none;
  }
  .meta a:hover {
    text-decoration: underline;
  }
  .detail {
    margin: 0;
    font-size: var(--z-text-sm);
    line-height: var(--z-leading-sm);
    color: var(--z-text);
    overflow-wrap: anywhere;
  }
</style>
