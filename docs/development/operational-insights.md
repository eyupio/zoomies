# Operational insights across Zoomies

The operator pages share the visual language introduced by Usage, with metrics
and links that explain the work visible on each page.

| Page | Operational context |
| --- | --- |
| Runners | Job queue depth, busy/idle/starting/draining/failed counts, lifecycle composition, provisioning status, oldest queued demand, inspectable one-hour history |
| Queue | Active provisioning demand and ready/expedited/paused/removed composition across matching records, beside the existing bulk controls |
| Pools | Matched queue depth, enabled/disabled totals, pools at their configured ceiling, ranked demand and headroom |
| Pool and runner detail | Pool queue depth, live runner state, headroom, provisioning state and links to the corresponding queue and usage report |
| Jobs | Current queue/running counts, completion success/failure/unknown outcomes, P95 queue wait and scoped one-hour fleet trends |
| Hosts | Eligible slot headroom, resource commitment map, disk-free context and direct access to host controls and usage history |
| Overview | Compact host capacity map beside existing fleet, pool, scaling and outcome information |
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

One-hour history contains at most 60 one-minute points. Stored samples and live
stats are coalesced by minute, with the newer input winning. Missing minutes are
gaps, not zeroes. History reloads on reconnect and aborts on unmount. Provisioning
counts use a throttled event-driven refresh and ignore late results after a
scope change. No scheduler behavior is changed by these views.

## Reusable presentation

- `MetricGrid` supports action links and optional progress tracks using semantic
  tones, accessible names and the existing responsive design tokens.
- `StateBreakdown` presents labelled proportional state bands with exact counts.
- `SignalTrend` provides a time axis, coverage strip and keyboard/touch range
  inspector. It breaks its line at missing samples.
- Domain components under `web/src/lib/insights` compose these pieces, keeping
  data interpretation out of generic presentation components.

Run `npm run test:unit` for aggregation and missing-data semantics. The Playwright
`visual-insights.spec.ts` tests both desktop/mobile and light/dark layouts and
writes screenshots to the test output directory. The normal runner, pool, job,
overview and queue suites protect existing actions and filtering.
