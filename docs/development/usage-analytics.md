# Usage analytics

The Usage page combines summary metrics, activity and runner-hour trends, an
inspectable activity matrix, ranked execution consumption, and the detailed CSV
report. Pool, host, repository, workflow and installation groupings share the
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

## Extending the visual system

`MetricGrid.svelte` provides responsive semantic definition lists and fixed
status tones. `ChartPanel.svelte` provides consistent chart framing, descriptions
and optional actions. Both consume the existing design tokens, supporting light
and dark themes without new palette values. Usage-specific aggregation and
visuals live in `lib/usage/UsageInsights.svelte`; keep domain calculations separate
when adding host resources, pool health or repository analytics elsewhere.

The API adds outcome counts and bounded history to each usage row, a `host`
grouping, and an exact `key` filter for JSON and CSV. The original detail columns
and CSV column order are preserved. Capacity is aggregated in SQLite before
crossing into the application, and a per-minute upsert avoids growing a row for
every scheduling pass. Read-only analytics does not change scheduler placement.
