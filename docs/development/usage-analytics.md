# Usage analytics

The Usage page combines summary metrics, activity and runner-hour trends, the
activity matrix, ranked execution consumption, and the detailed CSV report. Pool, host, repository, workflow and installation groupings share the
same date range. Selecting a ranked group or the focus selector narrows every
visual and the CSV export. Range, grouping and focused group are shareable URLs.
Hosts also exposes fleet summary metrics and links into a host's usage report.

## Reporting semantics

- Queued, started and completed counts belong to the interval in which each
  event occurred. Completed outcomes distinguish success, failure (including
  runner faults), cancelled/skipped and unknown. Success rate divides successes
  by **all** completions, including unknown outcomes.
- Execution and allocated runner time are clipped at the interval edges and the
  observation time. Ongoing jobs contribute execution and peak concurrency.
- Queue wait is weighted by jobs started, not by the number of groups. Busy
  share divides execution time by allocation; absent allocation is not zero.
- Idle runner allocation belongs to pools, hosts and installations. Repository
  and workflow reports cannot attribute it, and hide allocation and costs.
  Host assignment comes from the retained runner record; missing assignments
  remain unattributed. Purged records cannot be reconstructed.
- History uses elapsed-hour buckets for windows of at most 48 hours, otherwise
  elapsed-day buckets anchored at the requested start. These are fixed elapsed
  intervals, not local calendar days across daylight-saving transitions.
- Capacity history is collected from scheduler host-capacity placement blocks.
  It is **not** inferred from queue size, runner utilisation or today's limits.
  One row per pool/minute records whether any observed reconcile was blocked.
  Multiple blocked passes are one blocked pool-minute, not multiple incidents.
  Missing samples remain visibly unsampled. This does not report exact incident
  durations, pool max-runner limits or repository quotas as host-capacity blocks.
- Capacity history starts when this version runs, follows job retention, and is
  not backfilled. Job history likewise reflects retained jobs and runners. Costs
  remain estimates using administrator-configured pool rates.

## The activity matrix

`lib/insights/ActivityMatrix.svelte` draws a series of usage buckets as a
contribution graph, and the Overview and the Usage page both use it. Daily
buckets become a calendar, a column per week and a row per weekday; hourly
buckets become a row per day and a column per hour. Which one a range gets is
the API's own rule, hourly at 48 hours or less, so the page applies the same
rule to know what a square is.

The arithmetic is in `lib/insights/activity.ts` and is tested in Node: how a
square is painted under each mode, how the darkness steps are cut (quarters of
the busiest square on screen for counts; fixed bands for a failure share and
for a capacity share, so half of everything failing looks the same on a good
week as on a bad one), how a calendar is laid out across a daylight-saving
change, and how a `job.updated` frame is folded into the square its moment
belongs to. The Overview fetches a year once and folds frames into today;
runner time is not folded, because a frame cannot say which hour a job's
running fell in, and the next fetch carries the exact figure.

Selecting a day asks for that day again with hourly buckets, in the same
grouping and focus the page is showing, and draws the hours under the grid
with links to the Jobs page cut to the day. The links carry calendar dates
rather than instants because that is what the Jobs page filters by.

The Overview's quick ranges ask for the bucket width outright: `interval=hour`
for today and the last week, so a week is a week of hours and never a week of
days, and `interval=day` for the rest. The route refuses hourly buckets over
more than 14 days -- a year of hours, times every repository, is a payload
nobody wants -- and says so with what to ask for instead.

## Extending the visual system

`MetricGrid.svelte` provides responsive semantic definition lists and fixed
status tones. `ChartPanel.svelte` provides consistent chart framing, descriptions
and optional actions. Both consume the existing design tokens, supporting light
and dark themes without new palette values. Usage-specific aggregation and
visuals live in `lib/usage/UsageInsights.svelte`, and the matrix they share with
the Overview in `lib/insights/`; keep domain calculations separate when adding
host resources, pool health or repository analytics elsewhere.

The API adds outcome counts and bounded history to each usage row, a `host`
grouping, and an exact `key` filter for JSON and CSV. The original detail columns
and CSV column order are preserved. Capacity is aggregated in SQLite before
crossing into the application, and a per-minute upsert avoids growing a row for
every scheduling pass. Read-only analytics does not change scheduler placement.
