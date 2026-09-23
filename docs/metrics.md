---
title: Prometheus metrics for your runner fleet
description: >-
  Every Prometheus metric Zoomies exposes, what it measures and what to alert
  on: fleet gauges, job counters and startup histograms.
---

# Metrics

The controller exposes Prometheus metrics at `/metrics`. They are the same
numbers the Overview draws, without the browser, and they are how you keep a
history of everything that moves minute to minute: Zoomies stores enough of
those to answer "what is happening now" and leaves their past to whatever you
already scrape with. What it does keep is the usage ledger — every finished
runner's session for a year (`retention.runner_sessions`) and a daily roll-up
of runner-hours and cost per pool, host and installation that is never pruned —
so the [usage report](api-surface.md) answers "how many runner-hours did we use
last month, and what did they cost" however short `retention.runners` is. Job
counts, execution time and queue waits still come from job rows and go with
`retention.jobs`.

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
| `zoomies_jobs_queued` | gauge | `pool` | Jobs waiting for a runner, under the pool that claimed them. This is the backlog worth alerting on. Work an operator removed from the queue is not waiting for anything and is not counted, nor is a job whose workflow run has been cancelled — from the moment GitHub accepts the cancellation rather than when it reports the job over. Paused work is still waiting and is still counted. |
| `zoomies_hosts` | gauge | `state` | Agent hosts, by `healthy`, `unhealthy` or `cordoned`. |
| `zoomies_host_capacity` | gauge | — | Configured runner slots across healthy, uncordoned hosts: what their operators set. |
| `zoomies_host_effective_capacity` | gauge | — | The same slots as the hosts' throttles leave them. Equal to the previous while no host is throttled; the gap between the two is the throttle. |
| `zoomies_host_capacity_used` | gauge | — | Slots occupied. Divide by the effective figure for utilisation. |
| `zoomies_host_cpu_usage_percent` | gauge | `host` | Recent whole-host CPU occupied, including I/O wait. Missing when stale or unmeasured. |
| `zoomies_host_memory_available_bytes` | gauge | `host` | Recent available memory including reclaimable cache. Missing when stale or unmeasured. |
| `zoomies_host_admission_held` | gauge | `host` | 1 while measured CPU or memory pressure holds new starts; running jobs continue. |
| `zoomies_host_usage_fresh` | gauge | `host` | 1 when usage is less than 90 seconds old; 0 when placement falls back to reservations. |
| `zoomies_host_load_average_1m` | gauge | `host` | Recent whole-host one-minute load average. Missing when stale or unmeasured. Past twice the host's CPUs it is what throttles the host; under one per CPU is calm. |
| `zoomies_host_runtime_recovering` | gauge | `host` | 1 while the host's agent reports its container runtime in a recovery cooldown — new starts held until one recovery attempt — and 0 otherwise. Reported for every host. The drawer's `host.runtime_recovering` says which failure it is and when the attempt is due. |
| `zoomies_host_throttle_level` | gauge | `host` | The rung of the throttle ladder the host is on after sustained pressure, 0 to 3. Reported for every host, throttled or not, so a threshold rule keeps matching when nothing is wrong. |
| `zoomies_host_allocatable_cpus` | gauge | — | CPUs across healthy, uncordoned hosts, less each host's reserve. |
| `zoomies_host_allocatable_memory_bytes` | gauge | — | The same for memory. |
| `zoomies_host_reserved_cpus` | gauge | — | What the live runners have promised away, as of the last scheduling pass. |
| `zoomies_host_reserved_memory_bytes` | gauge | — | The same for memory. Divide by the allocatable pair for "how full are the machines", which is a different question from how full the slots are. |
| `zoomies_job_queue_age_seconds` | gauge | `pool` | How long the oldest job still waiting has been waiting. Zero when the pool has nothing queued, and counted over the same jobs as the depth — an item removed from the queue would otherwise climb here for ever. |
| `zoomies_github_paused` | gauge | `installation` | 1 while that installation is inside its GitHub rate-limit backoff and every background sweep is standing down from it. |
| `zoomies_provider_machines` | gauge | `provider`, `state` | Machines a provider is renting, by state. Every state is reported including the zeroes, so a provider that has stopped buying is visible rather than absent. |
| `zoomies_provider_machines_quarantined` | gauge | — | Machines whose ownership could not be proved. Nothing will move one until a person does, so this is a queue of work rather than a shape — alert on any sustained non-zero value. |

**Configured and effective slots are kept apart rather than one replacing the
other.** The effective figure is the utilisation denominator while a throttle
stands — it is what `free` is measured against, on the Hosts page and in the
scheduler — and the configured one is what the operator set, so an alert on
"the fleet shrank" can tell a host somebody resized from a host the controller
stepped down. `zoomies_host_throttle_level` names which.

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
| `zoomies_job_reruns_total` | counter | `pool`, `trigger` | Re-runs Zoomies has asked GitHub for. `trigger` is `operator` for the button on a job and `fleet_fault` for [`scheduler.auto_rerun`](configuration.md#schedulerauto_rerun-and-schedulerauto_rerun_limit). A rising `fleet_fault` rate is the fleet breaking jobs and paying to run them again: read it beside `zoomies_jobs_runner_lost_total` rather than on its own. |
| `zoomies_job_failures_total` | counter | `pool`, `domain`, `fault` | Job failures split by whose they are. `domain` is `fleet` or `workflow`; `fault` is the category — `out_of_memory`, `host_lost`, `image`, `registration`, `backend`, `backend_busy`, `container_conflict`, `config`, `out_of_disk`, `removed`, `runner_exited` — and is empty for a workflow's own failure. The fleet domain is the same set of jobs as `zoomies_jobs_runner_lost_total`, which is kept so an alert written against it does not disappear on upgrade. |
| `zoomies_runner_start_failures_total` | counter | `pool`, `fault` | Runners that failed before they could ever take a job, by category. These reach no job at all — the job they were meant for stays queued and waits for the next one — so a pool climbing here while its queue never moves is a fleet failing with nothing in the failed-jobs count to show for it. |
| `zoomies_registrations_deferred_total` | counter | `installation` | Runner creations held back because the installation was already at `scheduler.registration_concurrency` credential requests in flight. The scheduler had already chosen a host for each of them, so this counts work deferred rather than work refused: the demand is kept and a later pass takes it. A rising rate alongside a growing queue means runners are appearing slowly for a reason no number of extra hosts will change. |
| `zoomies_scaling_events_total` | counter | `pool`, `direction` | Scheduler decisions that changed a pool's size, `up` or `down`. Flapping shows up here first. |
| `zoomies_webhook_deliveries_total` | counter | `status` | Inbound deliveries by `accepted`, `rejected` or `error`. A rising `rejected` count is a signing-secret mismatch or somebody probing. |
| `zoomies_runner_cleanups_total` | counter | `outcome` | Attempts to take a runner off its host: `succeeded`, or `failed` and recorded on the runner's row. This is the half of a runner's life that goes wrong on the host rather than in the fleet, so it appears in no other series here. |
| `zoomies_reconcile_errors_total` | counter | — | Reconcile passes that failed. A pass that fails observes no duration, so without this a controller deciding nothing looks exactly like one with nothing to decide. |
| `zoomies_agent_polls_shed_total` | counter | — | Task polls answered with a backoff because the controller was holding too many at once. A fleet that keeps working while every host hears about its tasks a little later moves nothing else here, so this is the only place that pressure shows. |
| `zoomies_github_api_requests_total` | counter | `installation`, `result` | GitHub API calls by outcome: `ok`, `rate_limited`, `forbidden`, `not_found`, `error`. Where rate-limiting and a broken installation become visible. |
| `zoomies_provider_operations_total` | counter | `kind`, `outcome` | Provider operations by what was attempted — `create`, `start`, `stop`, `bootstrap`, `delete` — and how it went: `ok`, `ambiguous`, `quota`, `unreachable` or `refused`. `ambiguous` is separated from the failures because it means something different: a create that failed cost nothing, and a create whose answer was lost may already be a machine somebody is paying for. Any sustained rate of it is worth looking at. |
| `zoomies_host_runtime_failures_total` | counter | `kind` | Container-runtime failures agents have reported, each one opening or extending a cooldown on new starts: `unavailable` (the daemon could not be reached) or `timeout` (it did not answer in time). Counted when the controller hears of them, so failures during a controller outage arrive as one step. |
| `zoomies_image_pull_failures_total` | counter | `pool`, `kind` | Runner starts (`kind="start"`) and prewarms (`kind="prewarm"`) that failed because the pool's image could not be made ready on the host. The registry is on the host card and in `host.image_pull_failed` rather than a label, to keep the series bounded. |
| `zoomies_image_prewarms_total` | counter | `pool`, `backend`, `outcome` | Background image preparations by outcome: `prepared`, `cache_hit`, `failed`, or `unknown` for a successful older agent. The hit share says whether shared-image coalescing is saving runtime work. |
| `zoomies_elastic_cpu_decisions_total` | counter | `pool`, `mode`, `outcome` | Elastic CPU plans by pool. `outcome` is `burst`, `base`, `host_busy` — the host was held, throttled, or too busy on CPU, load or memory to lend anything — or `unsupported_agent`; observe mode records the same decisions without changing quotas. |

Every `pool` label is the pool's **name**, so a query can join these against
the gauges above on `pool`. Work no pool claims is counted under the literal
`unmatched` — a real pool of that name would merge with it, which is a reason
not to name one that.

`zoomies_image_prewarm_duration_seconds` is the matching histogram, with the
same `pool`, `backend` and `outcome` labels. It measures the complete agent-side
operation, including fast metadata cache hits; compare `prepared` p95 between
backends and watch `failed` rise before reducing refresh intervals.

## How long things take

| Metric | Type | What it is |
| --- | --- | --- |
| `zoomies_job_queue_wait_seconds` | histogram | Queued to picked up. The number that answers "is the fleet big enough?". |
| `zoomies_job_duration_seconds` | histogram | How long jobs ran once started. Capacity planning. |
| `zoomies_store_write_wait_seconds` | histogram | How long a database write waited for the single writer before it could start. Zoomies has one writer by design, and every heartbeat, webhook, scheduling pass and API write queues for it, so this is where a busy instance slows down first. A p99 climbing into whole seconds means too much is writing at once; past ten, SQLite gives up on the write and GitHub has given up on the webhook it came from. |
| `zoomies_store_write_held_seconds` | histogram | How long each write then held the writer, with every other write waiting behind it. Read beside the wait: a long wait with short holds is a queue, and a long hold is one slow write. A backup holds the writer for its whole copy, so its interval shows here as a spike. |
| `zoomies_reconcile_duration_seconds` | histogram | One reconcile pass, including its GitHub calls. A rising p99 means the control loop is being held up by GitHub rather than by itself. |
| `zoomies_provider_operation_seconds` | histogram | One request to a provider, labelled `kind`. It measures the request, not the clone the request starts: a create that takes four minutes at the hypervisor appears here as the second it took to accept the job. |
| `zoomies_elastic_cpu_target_factor` | histogram | Planned CPU target divided by the runner's guaranteed CPU, labelled by `pool` and policy `mode`. A value of 2 means the planner found room to double the quota. |

## Where a slow start actually goes

Five histograms, all labelled `pool` and `backend`, cutting the path from a
queued job to a running one into stages. The last is the whole path; the four
before it say which stage owns a regression.

| Metric | The stage |
| --- | --- |
| `zoomies_runner_queued_to_create_seconds` | Job queued → runner row created. A proxy for scheduler latency: it starts at GitHub's own queued time, so it includes webhook delivery and any configured scale-up delay, and the runner is attributed to the oldest queued job in the pool rather than the job it will run. |
| `zoomies_runner_create_to_container_started_seconds` | Runner created → container started. Where image pulls show up. |
| `zoomies_runner_startup_queue_seconds` | Host startup admission wait for successful creates. |
| `zoomies_runner_dind_ready_seconds` | Sidecar creation and health readiness after its image is available, for successful creates. |
| `zoomies_runner_container_started_to_registered_seconds` | Container started → registered with GitHub. |
| `zoomies_runner_registered_to_ready_seconds` | Registered → idle or busy. |
| `zoomies_runner_queued_to_job_started_seconds` | The whole path. Put this one on the dashboard. |

Two further histograms use the confirmed lifecycle timestamps, with the same
`pool` and `backend` labels:

| Metric | The interval |
| --- | --- |
| `zoomies_runner_eligible_to_create_task_seconds` | Job eligibility → first create task delivered to the job's actual runner host. Observed when the job is linked to its runner, once per job; retries and later stop/remove tasks do not move its end. Includes capacity waits and configured scaling delays after eligibility. GitHub deployment-review holds are excluded. |
| `zoomies_runner_cleanup_duration_seconds` | Runner finished → both host removal and GitHub registration absence confirmed. Includes the configured retention period; a process exit or successful stop alone does not complete it. Repeated confirmations do not add samples. |

Prewarmed runners, jobs whose eligibility was not observed, older runners with
no first-delivery timestamp, and cleanup with a missing confirmation do not
produce a sample. Missing is not zero. Demo installations are excluded. These
histograms describe observations since the controller started; they are not an
exact historical percentile or a count of every attempted job.

For exact historical intervals, the runner API exposes `create_task_issued_at`,
`host_removed_at`, `registration_deleted_at` and `cleaned_up_at`; join the job's
`runner_id` and `eligible_at`. The runner details show the separate timestamps.
Older cleanup timestamps are retained as `cleanup_estimated_at`, labelled as
estimates, and never used as confirmed completion. Compare scheduling figures
with the scheduler interval and available capacity; compare cleanup duration
with retention before interpreting either as a delay.

## Machines a fleet is renting

Present only when a [provider](providers.md) is configured.

| Metric | Type | Labels | What it is |
| --- | --- | --- | --- |
| `zoomies_provider_machines` | gauge | `provider`, `state` | Machines by provider and lifecycle state, read from the rows at scrape time so it cannot drift across a restart. |
| `zoomies_provider_machines_quarantined` | gauge | — | Machines whose ownership could not be proved. Nothing will act on one until a person does, so this is a queue of work rather than a shape: **alert on it above zero**. |
| `zoomies_provider_operations_total` | counter | `kind`, `outcome` | Provider operations, by what was attempted and how it ended: `ok`, `ambiguous`, `quota`, `unreachable` or `refused`. `ambiguous` is its own value on purpose — a create that failed costs nothing, and a create whose answer was lost may already be a machine somebody is paying for. |
| `zoomies_provider_operation_seconds` | histogram | `kind` | How long one provider request took. It measures the request, not the clone the request starts. |

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
  work being lost rather than failing. The same question with the reason
  attached is
  `sum by (fault) (rate(zoomies_job_failures_total{domain="fleet"}[15m]))`,
  which is the one to put on a dashboard: it says which of your problems this
  is, and the fix differs for every category.
* Any sustained `rate(zoomies_runner_start_failures_total[15m])`, which is the
  failure that shows up nowhere else. A pool that cannot start a container
  fails silently: the jobs stay queued, nothing is marked failed, and the fleet
  reads as busy. Alert on it separately from the line above.
* `zoomies_job_queue_age_seconds` above the longest wait you would accept,
  which is the same question as the first rule asked in the units somebody
  actually complains in.
* Any sustained `rate(zoomies_reconcile_errors_total[15m])`, or
  `zoomies_github_paused` stuck at 1: both are the control loop not running,
  which no gauge about the fleet's shape will show you.
* `zoomies_hosts{state="unhealthy"}` above zero.
* `zoomies_host_throttle_level` above zero for longer than a job takes, which
  is a host with too many slots for its machine rather than a host having a
  bad afternoon.
* `absent(zoomies_runners)`, which catches the case above where the gauges stop
  being reported at all.

The [problems drawer](problem-codes.md) answers a different question — it says
what is wrong *now*, in sentences, with a fix. Metrics say what has been
happening. An operator wants both.
