<!--
  The activity matrix at the top of the Overview: the fleet's days as one band
  of squares, a year of them by default, or today by the hour.

  The series comes from GET /usage grouped by pool -- the one grouping that
  carries capacity telemetry -- and is summed back into a fleet total here.
  A year is asked for once and cut to the width on screen, so a window being
  dragged wider costs no request; a shorter range is a different window and a
  different width of square, and is fetched afresh. The stream keeps today
  moving: every `job.updated` frame is folded into the square its moment
  belongs to, the way the metric tiles fold `stats` frames into their
  sparklines, and a reconnect ends in one reconciling fetch for the same
  reason it does everywhere else -- the replay buffer is finite.

  Two things a fetch-once panel would otherwise get wrong. A tab left open
  past midnight would have no square for the new day and would count nothing
  into it, so the clock is checked and the window moved on. And the page's
  refresh button means this panel too: the Overview hands its reload to the
  same handler the fleet cache runs.
-->
<script lang="ts">
  import { getUsage } from '$lib/api/client';
  import { events } from '$lib/api/sse';
  import type { Job } from '$lib/api/types';
  import { fleet } from '$lib/state/fleet.svelte';
  import { ACTIVITY_RANGES, prefs, type ActivityRangeKey } from '$lib/state/prefs.svelte';
  import { formatNumber, onClockTick, pluralise } from '$lib/format';
  import { managedJob } from '$lib/status';
  import Badge from '$lib/components/Badge.svelte';
  import ErrorState from '$lib/components/ErrorState.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import Segmented from '$lib/components/Segmented.svelte';
  import Select from '$lib/components/Select.svelte';
  import Skeleton from '$lib/components/Skeleton.svelte';
  import ActivityMatrix, { type ActivityRange } from '$lib/insights/ActivityMatrix.svelte';
  import {
    addDays,
    calendarWindow,
    fillWindow,
    foldJob,
    hasCapacity,
    localDate,
    mergeHistories,
    MODES,
    newSeen,
    rangeWindow,
    summarise,
    type ActivityBucket,
    type ActivityMode,
    type Interval,
    type RangeWindow,
  } from '$lib/insights/activity';

  interface Props {
    class?: string;
  }

  let { class: className = '' }: Props = $props();

  /**
   * The quick ranges. A day and a week are drawn by the hour -- a row of
   * twenty-four squares, and seven of them, which is the punch card that
   * shows when the fleet is busy -- and the rest by the day. The year is
   * fifty-one whole weeks and the days of this one, which is the most the
   * API's ceiling of 366 days allows, laid out as a contribution graph is;
   * what the fleet retains is usually less, and the squares before that are
   * simply quiet, as a graph's are before the first commit.
   */
  interface Range {
    label: string;
    name: string;
    interval: Interval;
    size: 'sm' | 'lg';
    window: () => RangeWindow;
  }
  const RANGES: Record<ActivityRangeKey, Range> = {
    '1d': {
      label: '1d',
      name: 'Today, by the hour',
      interval: 'hour',
      size: 'lg',
      window: () => rangeWindow(1, 'hour'),
    },
    '7d': {
      label: '7d',
      name: 'The last 7 days, by the hour',
      interval: 'hour',
      size: 'lg',
      window: () => rangeWindow(7, 'hour'),
    },
    '30d': {
      label: '30d',
      name: 'The last 30 days',
      interval: 'day',
      size: 'lg',
      window: () => rangeWindow(30, 'day'),
    },
    '90d': {
      label: '90d',
      name: 'The last 90 days',
      interval: 'day',
      size: 'lg',
      window: () => rangeWindow(90, 'day'),
    },
    '1y': {
      label: '1y',
      name: 'The last year',
      interval: 'day',
      size: 'sm',
      window: () => {
        const w = calendarWindow(52);
        return { from: w.from, to: w.to, count: w.days, interval: 'day' };
      },
    },
  };

  const chosen = $derived(RANGES[prefs.activityRange]);
  let range = $state.raw<RangeWindow>(RANGES[prefs.activityRange].window());
  let series = $state.raw<ActivityBucket[]>([]);
  let fetchedAt = $state(0);
  let loading = $state(true);
  let error = $state<unknown>(null);
  let mode = $state<ActivityMode>('outcomes');
  let visible = $state.raw<readonly ActivityBucket[]>([]);
  let selected = $state<number | null>(null);
  let seen = newSeen();
  let pending: AbortController | null = null;

  export async function reload(): Promise<void> {
    pending?.abort();
    const controller = new AbortController();
    pending = controller;
    const next = chosen.window();
    const started = Date.now();
    try {
      const report = await getUsage(
        {
          from: next.from.toISOString(),
          to: next.to.toISOString(),
          group_by: 'pool',
          // Said outright even where the route's own rule would agree, so a
          // week is a week of hours and never a week of days.
          interval: next.interval,
        },
        controller.signal,
      );
      if (controller.signal.aborted || pending !== controller) return;
      // The window moves with the reload, so a selection made against the
      // old one would point at a different square; it is let go rather than
      // silently moved.
      if (next.from.getTime() !== range.from.getTime() || next.interval !== range.interval) {
        selected = null;
      }
      range = next;
      series = fillWindow(mergeHistories(report.items ?? []), next.from, next.count, next.interval);
      seen = newSeen();
      fetchedAt = started;
      error = null;
    } catch (cause) {
      if (controller.signal.aborted || pending !== controller) return;
      error = cause;
    } finally {
      if (!controller.signal.aborted && pending === controller) loading = false;
    }
  }

  // The first load, and one for every change of range: a range is a
  // different window and a different width of square, not a different view
  // of the same series.
  $effect(() => {
    void chosen;
    loading = true;
    void reload();
    return () => pending?.abort();
  });

  // One reconciling fetch after a gap in the stream, for the same reason the
  // fleet cache does one: the replay buffer is finite.
  let previous: string | null = null;
  $effect(() => {
    const status = fleet.connection;
    if (previous !== null && previous !== 'live' && status === 'live') void reload();
    previous = status;
  });

  // Today moves as the stream reports jobs. Only this fleet's jobs, which is
  // the rule the report itself applies: a job GitHub ran on its own runners
  // used no runner here and is not in the series a frame is folded into.
  $effect(() =>
    events.subscribe('job.updated', (job: Job) => {
      if (!managedJob(job)) return;
      const next = foldJob(series, range.interval, job, fetchedAt, seen);
      if (next) series = next;
    }),
  );

  // A tab open past midnight gets the new day's squares rather than folding
  // its jobs into nothing.
  $effect(() =>
    onClockTick((now) => {
      if (!loading && now >= range.to.getTime()) void reload();
    }),
  );

  const totals = $derived(summarise(visible));
  const capacity = $derived(hasCapacity(series));
  const modes = $derived(MODES.filter((m) => m.value !== 'capacity' || capacity));

  /** The Jobs page cut to the day, and the Usage report for the same day. */
  function links(at: ActivityRange) {
    const day = localDate(at.from);
    const out = [{ href: `/jobs?since=${day}&until=${day}`, label: 'Jobs that day' }];
    if (at.bucket.failed > 0) {
      out.push({ href: `/jobs?failed=true&since=${day}&until=${day}`, label: 'Failed jobs' });
    }
    out.push({ href: `/usage?since=${day}&until=${day}`, label: 'Usage report for the day' });
    return out;
  }

  /** A day's hours, on demand, for the detail under the calendar. */
  async function hourly(day: Date): Promise<ActivityBucket[]> {
    const to = addDays(day, 1);
    const report = await getUsage({
      from: day.toISOString(),
      to: to.toISOString(),
      group_by: 'pool',
      interval: 'hour',
    });
    const count = Math.round((to.getTime() - day.getTime()) / 3_600_000);
    return fillWindow(mergeHistories(report.items ?? []), day, count, 'hour');
  }
</script>

<!-- No description line: the legend says what the colours mean, the tooltip
     says what a square holds, and the band is at the top of the page, where
     every line it spends is a line the fleet's own numbers lose. -->
<Panel title="Activity matrix" class="band {className}" flush>
  {#snippet actions()}
    <Segmented
      label="Range"
      value={prefs.activityRange}
      options={ACTIVITY_RANGES.map((key) => ({
        value: key,
        label: RANGES[key].label,
        name: RANGES[key].name,
      }))}
      onchange={(key) => (prefs.activityRange = key as ActivityRangeKey)}
    />
    <Select
      ariaLabel="Colour the matrix by"
      value={mode}
      size="sm"
      options={modes}
      onchange={(v) => (mode = v as ActivityMode)}
    />
  {/snippet}
  <div class="body">
    {#if error}
      <ErrorState
        {error}
        compact
        title="The activity matrix could not be loaded"
        onretry={() => void reload()}
      />
    {:else if loading}
      <p class="sr-only">Loading the activity matrix.</p>
      <div class="placeholder" aria-hidden="true">
        <Skeleton width="100%" height="var(--z-space-10)" />
        <Skeleton width="100%" height="var(--z-space-10)" />
      </div>
    {:else}
      <ActivityMatrix
        buckets={series}
        interval={range.interval}
        first={range.from}
        {mode}
        weeks={prefs.activityRange === '1y' ? 'fit' : 'all'}
        size={chosen.size}
        bind:visible
        bind:selected
        hourly={range.interval === 'day' ? hourly : undefined}
        {fetchedAt}
        {links}
        subject="this fleet's jobs"
      >
        <!-- The totals sit with the legend rather than in the header: a
             header that wraps onto a second line is a band a line taller. -->
        {#snippet caption()}
          {#if totals.completed > 0}
            <span class="totals">
              {pluralise(totals.completed, 'job')} finished
              {#if totals.failed > 0}
                <Badge tone="danger" label="{formatNumber(totals.failed)} failed" dot={false} />
              {/if}
            </span>
          {:else}
            <span class="totals">No jobs finished in this window</span>
          {/if}
        {/snippet}
      </ActivityMatrix>
    {/if}
  </div>
</Panel>

<style>
  /* A band rather than a card: the vertical padding of a table row rather
     than of a card, because this sits above the fleet's own numbers and
     every line it spends is a line they lose. The sides keep the card
     measure. */
  .body {
    padding: var(--z-space-3) var(--z-space-5);
  }
  .totals {
    display: inline-flex;
    align-items: center;
    gap: var(--z-space-2);
    font-size: var(--z-text-xs);
    color: var(--z-text-muted);
    white-space: nowrap;
  }
  .placeholder {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-2);
  }
</style>
