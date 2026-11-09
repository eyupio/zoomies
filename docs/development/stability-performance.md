---
# Implementation notes remain available to contributors through existing links.
noindex: true
description: Implementation evidence and remaining work from the September 2026 stability and performance review.
---

# Stability and performance implementation

This page records the first safety and performance tranche from the September 2026 review (SP-01 to SP-29), which shipped in October 2026, and what is still open. It was checked against the code on 9 November 2026. Ephemeral runners remain the default. Persistent caches are independent, disposable acceleration data; a cache is neither a runner nor a workspace backup.

## Changes and priorities

| Priority | Review | Implemented behaviour | Reason |
|---|---|---|---|
| P0 | SP-01, SP-04 | Recovery fences and expired/lost controller authority stop new lifecycle dispatch, provider operations and the elastic CPU and memory directives. The host throttle directive is still sent while fenced. New agents honour a heartbeat pause for local cleanup, orphan reaping, queued lifecycle tasks and resource directives; only update tasks have a test on the agent side so far. | Prevent destructive work without current authority. Already executing external operations cannot be recalled. |
| P0 | SP-02, SP-03 | A backend whose startup inventory failed must first adopt its recovered inventory. Missing workload observations remain until acknowledged. | A temporary outage must not turn owned workloads into orphans or lose terminal evidence. |
| P0 | SP-05 | Native metadata (`runner.json`) records PID birth identity and is replaced atomically; the `runner.pid` file is a plain write. Stop, kill and status verify identity; uncertain inventory fails closed, while a proven identity mismatch reads as a gone runner. | Reduce the risk of signalling a reused PID. Verification and signalling are still separate system calls. |
| P0 | SP-06 | Controller task ACK follows the required lifecycle writes (runner state, handle, image digest, startup timings, prewarm state). A failed required write retains the in-flight task. Creation observations, image-pull metrics and tool-fill, host-check and update results are best effort and never hold a task. | Permit redelivery instead of silently forgetting an unapplied result. |
| P1 | SP-07, SP-08 | Durable, generation-checked enrichment queue; four bounded lookups per batch for readiness, run metadata and registration cleanup. | Keep remote GitHub latency out of webhook and heartbeat acknowledgements. |
| P1 | SP-09 | Backups use a read connection without the application writer mutex; job and per-job-event pruning use bounded batches (500 jobs or 1,000 rows, at most 100 batches per pass, with the writer released between batches). | Reduce ingestion stalls while retaining WAL snapshot semantics. Disk I/O can still contend. |
| P1 | SP-10 | Provider inventories run outside the machine pass lock, with four concurrent sweeps; lifecycle operations are capped at eight globally/four per provider. Inventory evidence is applied only to unchanged machine rows. | A slow remote provider must not block every machine pass or invalidate newer state. |
| P1 | SP-11–SP-13 | Transition-only job metrics; independent missed-event recovery and discovery loops; one runner snapshot query instead of one per pool. | Reduce duplicate metrics, discovery coupling and repeated reads. |
| P1 | SP-14–SP-16 | Resource samples do not invalidate table structure; browser event replay keeps the last 256 events, and a client whose gap is larger receives a `resync` frame and refetches; native (process backend) runner logs retain a 10 MiB tail plus the current file, checked each minute; password sign-in verification is capped at two concurrently (password change and two-step re-authentication are not). | Bound avoidable CPU, memory and disk growth. Log retention is periodic, not a hard filesystem quota. |
| P1 | SP-20–SP-22 | Versioned hashed cache identities; tool cache directories include resolved image identity; baked tools are linked into each disposable tool view. | Prevent namespace collisions and incompatible tool reuse without downloading baked tools again. |
| P1 | SP-23–SP-25 | Cache walks honour cancellation; images contain an installed-package/tool inventory; documented registry BuildKit cache workflow keeps DinD disposable. | Bound maintenance work and make reuse and included software inspectable. |
| P1 | SP-26, SP-27 | An independent monotonic watchdog expires CPU loans; failed boost withdrawals retry during controller outages; quota reductions precede increases; failed or pending updates suppress increases. | Avoid lending CPU before previous loans have been reclaimed. |
| P2 | SP-17, SP-28, SP-29 | Successful startup service time is recorded once; optional readiness placement uses recent host/pool/image history and pending starts, after existing hard fit checks. Versioned placement intent narrows boost start reservations for up to 30 seconds. | Prefer a host likely to admit new capacity sooner without moving running jobs or relaxing resource limits. |

## Persistent caches with ephemeral runners

See [Persistent caches](../persistent-caches.md) for storage boundaries, trust scope, DinD reuse and the cache namespace change. The kept tool cache and the host's cache service are opt-in per pool and reuse the same hashed cache identities. The change does not enable shared writable caches for every pool automatically.

## Placement rollout

`scheduler.placement_mode` / `ZOOMIES_PLACEMENT_MODE` accepts:

- `headroom` (default): existing resource-aware placement.
- `shadow`: retain existing placement and log a readiness comparison at debug level.
- `readiness`: use estimated startup queue delay. When two hosts' delays differ by less than five seconds, the normal ranking decides: host priority, then `scheduler.host_order`, then headroom.

Evidence is limited to the latest 1,000 successful starts in 24 hours, with at least three samples per host/pool/image. Service time includes image preparation and backend create; it excludes admission waiting and GitHub registration. Unknown history uses a one-minute prior. Each per-start service estimate is bounded to 5 seconds to 10 minutes; a host's queue delay multiplies it by the starts already ahead, so the total can be longer. Estimates for an image other than the pool's current one are ignored. Existing connectivity, labels, backend, capacity, CPU, memory and disk checks still apply. Pending starts count even with missing host telemetry.

This is an admission heuristic, not a promise of job completion time. GitHub assigns jobs to eligible runners. Zoomies places new runners; it does not move jobs already assigned by GitHub. Use immutable image references when comparing generations: a mutable tag can describe changed image contents.

Start in shadow mode for a representative work week. Compare queue-to-start p50/p95/p99, failed starts, host skew, cache hit/miss and startup durations, provider latency, CPU throttling and memory/disk pressure. Enable readiness for a canary controller only when tail latency improves without worse failures, fairness or host pressure. Restore `headroom` immediately if those regress.

## Upgrade and rollback

1. Take and verify a database backup. Migrations 0055 and 0056 add enrichment state and startup service time.
2. Drain native-process hosts before upgrading agents: legacy PID-only metadata cannot prove process ownership. Resolve old processes explicitly; do not delete metadata to force adoption or kill an unverified PID. While one such runner is alive, the native backend's inventory fails as a whole on that host, so the agent can adopt or reap none of its native runners until it is resolved.
3. Upgrade controller and agents together. Old agents ignore the new pause field, so local cleanup suppression requires new agents. Controller-side dispatch fencing applies independently.
4. Expect cold caches in the new namespace. Old caches are not copied into new trust/image scopes or deleted automatically. Remove old generations only after all their runners and maintenance tasks have drained.
5. Verify the image/package matrix before publishing runner images. Revert readiness configuration independently of code. Database rollback requires the usual compatible binary/backup procedure, not deleting migration records.

## Release evidence and remaining work

These are explicit follow-ons, not claims delivered by this patch:

- **P1, SP-06/17:** durable agent result outbox across restart, explicit credential-age/admission metrics and safe renewal; current redelivery still depends on existing task leases and reconciliation. Still open: the agent keeps no outbox, and no credential-age metric exists.
- **P1, SP-08/12/13:** per-installation fairness/rate budgets, batched readiness caching, keyset recovery and compact historical scheduler reads. The new bounded enrichment worker and split loops provide isolation, not complete fleet-scale optimisation. What exists beyond them is a per-installation cap on registration requests (`scheduler.registration_concurrency`) and a rate-limit stand-down; neither is a fairness scheme or a rate budget, and readiness listings are shared within one report batch but not cached across batches.
- **P1, SP-23/24:** per-filesystem byte/inode admission, retained-cache quotas, capability-tested DinD digest/version matrix and cache telemetry. Existing cache limits, including the host cache service's size limit and its least-recently-used eviction, are eviction targets, not admission quotas; workloads can still fill a filesystem, and inode use is only reported by the host health checks. There are no cache hit or miss metrics yet. Do not mount one live Docker data-root into multiple daemons.
- **P1, SP-27:** explicit desired/applied generation acknowledgements. Reductions-first and retry are necessary but do not yet constitute a distributed capacity transaction.
- **P2, SP-18:** leave Tailcat connection reuse disabled until relay/session failure tests demonstrate safe pooling; no speculative keepalive change.
- **P2, SP-25/28:** the installed-software inventory in each runner image exists and is checked for shape, but it is not an SBOM. Still open: a complete software smoke matrix and SBOM publication, cache-warmth inventory, job-duration/reliability models, cold-host exploration and percentile prediction. Current history is bounded mean startup service time.

No percentage speedup is asserted without production measurements. Expected improvements are fewer lost observations and unsafe cleanup attempts, shorter ingestion stalls during remote outages, fewer cache misses caused by incompatible or hidden tool caches, and more informed placement on heterogeneous hosts. Correct cache isolation may initially lower hit rates because it intentionally starts fresh namespaces.

### Repeatable release drills (SP-19)

Run normal CI, targeted race tests and platform-native integration tests. Use disposable hosts for these drills, preserving task, runner and provider IDs in the evidence. None has a recorded run yet: `make test-drill` uses the process backend and a fake GitHub, so it covers controller and agent restarts and the rate-limit stand-down but never Docker, providers, caches or CPU boosts. Unit tests in `internal/controller/stability_regression_test.go` cover the lease, fence and duplicate-completion rows in process only.

| Drill | Required observation |
|---|---|
| Lose controller lease while cleanup/create tasks are queued | No new task dispatch; observations still ingest; recovery does not duplicate starts. |
| Restart Docker during agent adoption, then restore it | Owned workloads adopted before orphan reaping. |
| Lose the terminal report response and retry it | One terminal transition; observation retained until ACK. |
| Stall provider List while creating a machine | Other machine passes continue; old inventory does not mark the new machine missing. |
| Return GitHub 429/5xx during readiness and webhook traffic | Ingestion stays responsive; bounded enrichment retries survive controller restart. |
| Fail quota reduction while another runner requests a boost | No new increase in that batch; withdrawal retries after recovery and controller outage. |
| Run cold/warm dependency and Docker builds, then delete caches | Equivalent outputs; cache absence costs time only; fork workflows cannot publish trusted cache. |
| Fill cache filesystem and exhaust inodes on a disposable host | Record actual admission behaviour and recovery; do not infer protection from eviction limits. |
| Native agent restart and PID reuse on Linux/macOS/Windows | Logging survives restart; unverified PID never signalled; bounds apply to diagnostic files. |
| Compare identical workload replays across headroom/shadow/readiness | Record p50/p95/p99, failure rate, host skew, throughput and cost with equal resource budgets. |

Image builds, real provider and relay drills and native process integration need environments with their actual runtimes; unit tests do not substitute for them.

### Release gates

Until each of these has evidence, treat the matching claim above as implemented but unproven:

- Native integration cases that need matching PID and proc namespaces (they skip in sandboxes) pass on CI and on a native host of each platform.
- Docker image builds and registry cache performance are measured on real hosts.
- Real remote-provider and relay failure drills from the table above are run and their evidence kept.
- Production performance is compared against the `headroom` baseline before `readiness` is enabled anywhere.

Some claims above have no direct test yet: the batch size of four for enrichment lookups, the concurrency caps in the machine pass, the placement-intent reservation window, the order of quota reductions and increases, and the agent-side pause for anything but update tasks. Add one when touching that code.
