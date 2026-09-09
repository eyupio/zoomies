---
description: >-
  Every Prometheus metric Zoomies exposes, what it measures and what to alert
  on: fleet gauges, job counters and startup histograms.
---

# Metrics

The controller exposes Prometheus metrics at `/metrics`. They are the same
numbers the Overview draws, without the browser, and they are the only way to
keep a history: Zoomies stores enough to answer "what is happening now" and
leaves "what happened last month" to whatever you already scrape with.

**The agent exposes nothing.** Everything below is the controller's. Agent-side
work still reaches Prometheus, because the agent reports what it did and the
controller observes the result — the runner-startup histograms are all measured
this way.

## Reaching the endpoint

| Setting | Default | What it does |
| --- | --- | --- |
| `metrics.enabled` | `true` | When false the route is never registered, so the path 404s rather than 403s. |
| `metrics.path` | `/metrics` | Mounted at the root, not under `/api/v1`. A leading slash is added if you leave it off, and a path that collides with another mount is refused at startup. |
| `metrics.public` | `false` | Serves without authentication. Raises the `metrics.public` warning, because anyone who can reach it learns the shape of the fleet. |

Authenticated is the default: a scraper needs a token with the `metrics:read`
scope, which the viewer role already carries.

```sh
zoomies tokens create --name prometheus --role viewer --scope metrics:read
```

A session cookie works too, which is what makes the endpoint readable in a
browser while you are signed in. Sending `Accept: application/openmetrics-text`
gets OpenMetrics rather than the classic text format.

The Go runtime's `go_*` and `process_*` families are exposed alongside these.

## The fleet, right now

These five are read from the database on every scrape rather than kept in
memory, so they are always current and never drift.

| Metric | Type | Labels | What it is |
| --- | --- | --- | --- |
| `zoomies_runners` | gauge | `pool`, `state` | Runners by pool and state. The main shape-of-the-fleet series. |
| `zoomies_jobs_queued` | gauge | `pool` | Jobs waiting for a runner, under the pool that claimed them. This is the backlog worth alerting on. |
| `zoomies_hosts` | gauge | `state` | Agent hosts, by `healthy`, `unhealthy` or `cordoned`. |
| `zoomies_host_capacity` | gauge | — | Runner slots across healthy, uncordoned hosts. |
| `zoomies_host_capacity_used` | gauge | — | Slots occupied. Divide by the previous for utilisation. |
| `zoomies_host_allocatable_cpus` | gauge | — | CPUs across healthy, uncordoned hosts, less each host's reserve. |
| `zoomies_host_allocatable_memory_bytes` | gauge | — | The same for memory. |
| `zoomies_host_reserved_cpus` | gauge | — | What the live runners have promised away, as of the last scheduling pass. |
| `zoomies_host_reserved_memory_bytes` | gauge | — | The same for memory. Divide by the allocatable pair for "how full are the machines", which is a different question from how full the slots are. |
| `zoomies_job_queue_age_seconds` | gauge | `pool` | How long the oldest job still waiting has been waiting. Zero when the pool has nothing queued. |
| `zoomies_github_paused` | gauge | `installation` | 1 while that installation is inside its GitHub rate-limit backoff and every background sweep is standing down from it. |

**Slots and resources answer different questions too.** `zoomies_host_capacity`
counts what the fleet will *take*; the allocatable and reserved pairs say
whether the machines can carry it. A fleet with slots free and no memory left is
the case the slot gauges cannot describe, and it is the one somebody is looking
for when the queue will not drain. Disk is deliberately not in the reserved
pair: free disk already contains what the runners there have written.

**Queue depth and queue age answer different questions.** Ten jobs queued for
four seconds is a fleet working; one job queued for forty minutes is a fleet
that has stopped, and `zoomies_jobs_queued` reads lower for the second. Alert on
the age, and use the depth for capacity planning.

The pause gauge is **per installation**, because the backoff is: a fleet with
two installations, one of them rate-limited, is a fleet half working, and only
the label says which half. It reports 0 for every installation that is not held,
so a threshold rule keeps matching when nothing is wrong.

`zoomies_runners{state}` covers the live states only — `provisioning`,
`registering`, `idle`, `busy`, `draining` and `failed`. There is no `removed`
series, because a removed runner is not a runner.

**These five vanish rather than going to zero if the database cannot be read.**
The scrape logs a warning and returns what it has, so an alert on these should
use `absent()` as well as a threshold; a rule that only checks for a high value
will not fire when the numbers stop arriving altogether.

## What the fleet has done

| Metric | Type | Labels | What it is |
| --- | --- | --- | --- |
| `zoomies_jobs_total` | counter | `pool`, `conclusion` | Jobs seen to completion. The denominator for every other job rate. `conclusion` is GitHub's, or `unknown` when it sent none. |
| `zoomies_jobs_runner_lost_total` | counter | `pool` | Jobs whose runner died before GitHub reported the job over. These are the fleet's failures rather than the workflow's, and any sustained rate is worth waking up for. |
| `zoomies_scaling_events_total` | counter | `pool`, `direction` | Scheduler decisions that changed a pool's size, `up` or `down`. Flapping shows up here first. |
| `zoomies_webhook_deliveries_total` | counter | `status` | Inbound deliveries by `accepted`, `rejected` or `error`. A rising `rejected` count is a signing-secret mismatch or somebody probing. |
| `zoomies_runner_cleanups_total` | counter | `outcome` | Attempts to take a runner off its host: `succeeded`, or `failed` and recorded on the runner's row. This is the half of a runner's life that goes wrong on the host rather than in the fleet, so it appears in no other series here. |
| `zoomies_reconcile_errors_total` | counter | — | Reconcile passes that failed. A pass that fails observes no duration, so without this a controller deciding nothing looks exactly like one with nothing to decide. |
| `zoomies_github_api_requests_total` | counter | `installation`, `result` | GitHub API calls by outcome: `ok`, `rate_limited`, `forbidden`, `not_found`, `error`. Where rate-limiting and a broken installation become visible. |

Every `pool` label is the pool's **name**, so a query can join these against
the gauges above on `pool`. Work no pool claims is counted under the literal
`unmatched` — a real pool of that name would merge with it, which is a reason
not to name one that.

## How long things take

| Metric | Type | What it is |
| --- | --- | --- |
| `zoomies_job_queue_wait_seconds` | histogram | Queued to picked up. The number that answers "is the fleet big enough?". |
| `zoomies_job_duration_seconds` | histogram | How long jobs ran once started. Capacity planning. |
| `zoomies_reconcile_duration_seconds` | histogram | One reconcile pass, including its GitHub calls. A rising p99 means the control loop is being held up by GitHub rather than by itself. |

## Where a slow start actually goes

Five histograms, all labelled `pool` and `backend`, cutting the path from a
queued job to a running one into stages. The last is the whole path; the four
before it say which stage owns a regression.

| Metric | The stage |
| --- | --- |
| `zoomies_runner_queued_to_create_seconds` | Job queued → runner row created. A proxy for scheduler latency: it starts at GitHub's own queued time, so it includes webhook delivery and any configured scale-up delay, and the runner is attributed to the oldest queued job in the pool rather than the job it will run. |
| `zoomies_runner_create_to_container_started_seconds` | Runner created → container started. Where image pulls show up. |
| `zoomies_runner_container_started_to_registered_seconds` | Container started → registered with GitHub. |
| `zoomies_runner_registered_to_ready_seconds` | Registered → idle or busy. |
| `zoomies_runner_queued_to_job_started_seconds` | The whole path. Put this one on the dashboard. |

## Build information

`zoomies_build_info` is a gauge that is always 1, labelled `version` and
`commit`. It is there to be joined against: it tells you which build a series
came from, which matters while a fleet is mid-upgrade.

## A starting point for alerts

Four rules cover most of what goes wrong, and none of them is about a machine
being down:

* `zoomies_jobs_queued` above zero for longer than a job normally waits, which
  is the fleet failing at its one job.
* Any sustained `rate(zoomies_jobs_runner_lost_total[15m])`, because that is
  work being lost rather than failing.
* `zoomies_job_queue_age_seconds` above the longest wait you would accept,
  which is the same question as the first rule asked in the units somebody
  actually complains in.
* Any sustained `rate(zoomies_reconcile_errors_total[15m])`, or
  `zoomies_github_paused` stuck at 1: both are the control loop not running,
  which no gauge about the fleet's shape will show you.
* `zoomies_hosts{state="unhealthy"}` above zero.
* `absent(zoomies_runners)`, which catches the case above where the gauges stop
  being reported at all.

The [problems drawer](problem-codes.md) answers a different question — it says
what is wrong *now*, in sentences, with a fix. Metrics say what has been
happening. An operator wants both.
