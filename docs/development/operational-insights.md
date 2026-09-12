# Operational insights across Zoomies

The operator pages share the visual language introduced by Usage, with metrics
and links that explain the work visible on each page.

| Page | Operational context |
| --- | --- |
| Runners | Job queue depth, busy/idle/starting/draining/failed counts, the runner lifecycle drawn as the state machine with live counts and links, provisioning status, oldest queued demand, an inspectable trend over the last hour, six hours or day |
| Queue | Active provisioning demand and ready/expedited/paused/removed composition across matching records, beside the existing bulk controls |
| Pools | Matched queue depth, enabled/disabled totals, pools at their configured ceiling, ranked demand and headroom |
| Pool and runner detail | Pool queue depth, live runner state, headroom, provisioning state and links to the corresponding queue and usage report |
| Jobs | Current queue/running counts, completion outcomes as a bar whose segments are ways into the jobs they count, P95 queue wait and the same fleet trend |
| Hosts | Eligible slot headroom, resource commitment map, disk-free context and direct access to host controls and usage history |
| Overview | The activity matrix across the top: a year of days as a contribution graph, coloured by outcome, queue depth, runner time or capacity pressure, with a tooltip per square and an hourly breakdown per selected day; then a compact host capacity map beside existing fleet, pool, scaling and outcome information |
| Installations | Connection health, dependent pool totals, low API quota count and per-connection quota meters |

## Counting rules

The job queue and the provisioning queue are different measures. A GitHub job
can still be queued after an operator pauses or removes its provisioning demand.
Runner pages show both, using the merged provisioning API from PR #219. Ready
and expedited demand still obey pool priorities, limits, backoff and host fit.
The oldest demand timestamp is from the server's oldest-first query, not the
currently visible runner rows. Removed demand is excluded from that oldest item.

Fleet and pool context on Runners is independent of the runner grid's search,
host and state filters. Pool context follows the pool selector. Jobs summaries
and history follow the Other runners scope, but are intentionally independent
of table filters. These boundaries are stated next to the visuals. Pool demand
on Pools follows that page's search and enabled/disabled filters. Queue status
composition uses the server's counts: other filters apply, but the status filter
does not reduce the breakdown.

No aggregate is calculated from the first page of a runner or job cache. Pool
signals prefer the controller's per-pool stats. A pool at its ceiling with
queued work is a pressure signal, not proof that the scheduler is failing or
that every queued job should provision immediately.

Host free slots include only healthy, uncordoned, compatible hosts. Slots do not
promise that a particular platform, backend or resource request fits. CPU and
memory percentages show reserved commitments against allocatable resources,
not measured CPU or memory utilisation. Disk is reported free space. Missing
resource telemetry stays unknown; commitments above 100% retain the exact
percentage even though the visual meter stops at its boundary.

The fleet trend is one-minute points: sixty of them for the last hour, and for
the six-hour and one-day windows the minutes are folded into five- and
fifteen-minute intervals that carry their peak, because a queue that hit twelve
for two minutes is what somebody looking at a day is looking for. Stored
samples and live stats are coalesced by minute, with the newer input winning.
Missing minutes are gaps, not zeroes, and a folded interval with no observed
minute is a gap too. Hovering the chart reads every figure at that moment; the
slider is the same inspection for a keyboard. History reloads on reconnect and
aborts on unmount. Provisioning
counts use a throttled event-driven refresh and ignore late results after a
scope change. No scheduler behavior is changed by these views.

## Reusable presentation

- `MetricGrid` supports action links and optional progress tracks using semantic
  tones, accessible names and the existing responsive design tokens.
- `StateBreakdown` presents labelled proportional state bands with exact counts
  and shares; a segment with a destination is a link, and hovering one says the
  count, the share and what the state means.
- `LifecycleFlow` draws the runner state machine in the store's own order, each
  step with its count and a link to the runners in it.
- `SignalTrend` provides a time axis, the area under the line, a hover crosshair
  with a card of every figure at that moment, a coverage strip and a
  keyboard/touch range inspector. It breaks its line at missing samples.
- `ActivityMatrix` draws usage buckets as a contribution graph with one tab
  stop, a tooltip per square and an inline detail per selection; its
  arithmetic is `activity.ts`, which `npm run test:unit` covers. See
  [usage analytics](usage-analytics.md).
- Domain components under `web/src/lib/insights` compose these pieces, keeping
  data interpretation out of generic presentation components.

Run `npm run test:unit` for aggregation and missing-data semantics. The Playwright
`visual-insights.spec.ts` tests both desktop/mobile and light/dark layouts and
writes screenshots to the test output directory. The normal runner, pool, job,
overview and queue suites protect existing actions and filtering.
