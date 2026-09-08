# Support matrix and measurement contract

The ZF-002 deliverable: one reference configuration the foundation is proved
on, an honest statement of what else is supported and to what degree, the
moments the platform must timestamp so that Gate F can be measured rather
than asserted, and the words *failure* and *severity* defined once.

Everything here is a proposal for the owner to ratify; the evidence it rests
on is in [validation/baseline-6d12a72.md](validation/baseline-6d12a72.md).

## The reference configuration

| | Choice | Why |
| --- | --- | --- |
| Controller | Linux amd64, Ubuntu 24.04 LTS, `native` deployment under systemd | The only platform CI runs tests on, and what the installer was written against. |
| Agents | Linux amd64, Ubuntu 24.04 LTS, Docker backend, **rootless** daemon where the host offers one, rootful otherwise | The installer looks for a rootless socket first and the security posture assumes it; both must be exercised because most hosts still have only the rootful one. |
| Runner image | `ghcr.io/eyupio/zoomies-runner` at the pinned `actions/runner` release (2.337.0 on this commit), `pull_policy: if-not-present` | The image the docs tell people to use. |
| Topology under test | Two shapes, each in its own harness run: one VM with the embedded agent; a controller with one remote agent on a second host | The two deployment shapes the docs promise. The remote shape is what proves outbound-only agents, join tokens and host loss. |
| GitHub | github.com, a GitHub App installed on the owner's disposable organisation, with one repository-scoped and one organisation-scoped target | Both target kinds the docs support, because ZF-101's isolation tests need two installations. |
| Webhooks | A run with real delivery and a run on the poller alone | The harness today runs on the poller only. Real delivery needs a reachable URL; the tunnel or public host that provides it is part of the fixture. |

Everything else stays supported as it is documented today and is qualified
only to the degree a test proves:

| Configuration | Qualified for | Not qualified for | What would qualify it |
| --- | --- | --- | --- |
| Linux arm64 controller and agent | Build: CI cross-compiles and builds the arm64 images | Runtime: no test runs on arm64 | An arm64 host in the real-runtime tier of the harness |
| **Docker backend** | Shape: the Engine API client, the create arguments, the DinD refusal, the cache suffix and the permission checks are unit-tested against a fake Engine | **Runtime: nothing in this repository has ever started a container.** It is the default backend, the one the docs tell people to use, and the reference configuration's own | A Docker daemon in the real-runtime tier of the harness (ZF-301c). This is the largest single gap in the qualification, and it is listed first because the table would otherwise read as though every unproven configuration were an exotic one |
| Podman backend | Shape: probe, defaults, DinD refusal and the SELinux cache suffix are unit-tested | Runtime: no test starts a Podman daemon | A Podman host in the harness |
| `process` backend | Shape: layout, signals, process groups, archive tamper check, JIT config off argv. Runtime: the drill tier runs the built binary as a real remote agent on this backend and watches a workload appear on the machine and go away | Runtime with the real runner: the drill's workload is a stub staged on disk, so nothing has yet run `actions/runner` itself | A bare host in the harness with the real runner tree; it is the least isolated backend and the docs already say so |
| macOS controller | Build; launchd plist rendering | Everything else; the docs say development only | Nothing planned |
| Debian, Fedora, Alpine hosts | Installer parses under `dash` and `shellcheck`; detection is unit-tested | No install runs on them in CI | One real install per distribution in the installer tier of the harness |
| `compose` and `docker` deployments | Templates are unit-tested; images build in CI | No CI job brings a deployment up and joins an agent to it | A compose-up smoke test in the harness |
| GitHub Enterprise Server | `github.api_base_url` is validated | No test speaks to a GHES | Access to a GHES; not in the first assignment |

Cross-compilation is not qualification. A row moves right only when a test
runs on the thing.

## The moments to timestamp

Gate F asks for two timings and one convergence, and each needs a start and an
end that the store records. The baseline lists the fields as they stood at
`6d12a72`; every gap it recorded has since been closed, so the table below is
the current answer and the baseline is history.

| Interval | Start | End | Today |
| --- | --- | --- | --- |
| **GitHub queue wait** (not Zoomies' time) | GitHub's `started_at` on the job, absent for a queued job | `Job.QueuedAt` as first seen here | Reported by `JobEvent.Source`; the poller-only case inflates it and must be labelled |
| **Scheduling latency** (the Gate F p95 ≤ 10 s) | the job became *eligible*: an enabled pool of the job's own installation claims its labels — `scheduler.Eligible`, and no more than that. Installation health and free capacity do not hold the stamp back, so a rate-limit hold or a fleet with no room is time inside the interval rather than before it | the create task was *issued* to an agent | **Both ends are recorded since Assignment A; the series is not, and the end is not exportable.** `Job.EligibleAt` (migration `0019`) is stamped the first time an enabled pool claims the job's labels and is never moved afterwards — not re-set when eligibility is regained, because a later delivery finding the job still matched is not a new moment. It therefore records the first of this row's three clauses and not the other two, which is the honest reading: a job whose fleet is full is one this platform could act on and has not. `Runner.TaskIssuedAt` (migration `0014`, ZF-102's fourth pull request rather than a durable queue — the queue is in memory by design) is stamped when a lifecycle task is handed to an agent, and that means *every* lifecycle task, so on a runner that has been removed the column holds the removal's issue time and not the create's. It is also on no view and in no OpenAPI schema. So the interval cannot be reconstructed from the store after the fact, and `zoomies_runner_queued_to_create_seconds` still measures from `Job.QueuedAt` and must stay labelled a proxy. Both are ZF-205's to close. |
| **Configured scaling delay** (not Zoomies' time) | eligible | the scheduler's hold expires (`scheduler.scale_up_delay`, the start-failure backoff) | Reasons are in `ScalingEvent`; the delay is subtracted, not counted against the platform |
| **Provisioning** | task issued | `Runner.ContainerStartedAt` | Exists; `Runner.ImagePullDuration` separates the pull where the backend can |
| **Registration** | `Runner.ContainerStartedAt` | `Runner.RegisteredAt` | Exists |
| **Assignment** (GitHub's decision, not Zoomies') | `Runner.RegisteredAt` | `Job.StartedAt` | Exists; GitHub may hand the job to a different idle runner, which the contract accepts and reports as *ran elsewhere in the fleet* |
| **Execution** | `Job.StartedAt` | `Job.CompletedAt` | Exists |
| **Cleanup convergence** (the Gate F ≤ 5 min) | `Runner.FinishedAt`, or the retention deadline where one applies | the registration is gone from GitHub, the workload is gone from the host, the work directory is gone | **Recorded, and named differently from this contract.** `Runner.CleanedUpAt` (migration `0015`) landed with ZF-105. Two corrections to what this row expected: there is no `cleanup_pending` reason and there never was one — what shipped is `CleanupError`, `CleanupFailedAt` and `CleanupAttempts` on the row, raised to an operator as `runners.cleanup_failed`; and the stamp is not three positive confirmations but the absence of a complaint, set by `RecordRegistrationDeleted` once GitHub confirms the registration is gone and no cleanup error stands, and by `ClearCleanupFailure` when a stop or remove succeeds. Read it as *nothing is known to be left*, which is what a ≤ 5 min convergence target needs. |

Two smaller gaps the reconciliation found belonged to Phase 0 itself because
they needed no migration, and the code for both has landed. A job GitHub holds
for a deployment review is `waiting`, and the hold and its release now have
timeline kinds of their own — `waiting` and `approved`, written by the existing
job-change path — so a held job's timeline says whose time the review was. The
number has not followed the timeline: `QueuedAt` is stamped when the job is
first seen and never rewritten, so the reported queue wait still spans the
review, and a fleet whose repositories gate deployments reads high. Moving that
start to the approval changes a stored moment rather than adding one, so it
belongs to ZF-205 with the rest of the series work. `Stats` is split four ways
— succeeded, failed, cancelled and unknown — so a `stale` or empty conclusion
is no longer counted as a success.

Two rules go with the table. Every rate has a denominator written beside it,
and an observation the platform did not see (a job that completed while the
controller was down and was learned about later) is *unknown*, never
*success*. And a failure is attributed to Zoomies only when the platform's own
record says so: `Job.RunnerFault` set, a `runner_lost` event, a runner that
went `failed` before a job started, or a cleanup that did not converge. A
workflow whose steps failed is the workflow's business.

## Counters

The metrics page already exposes fleet counts. The contract adds, per pool
and per installation: jobs observed, eligible, created for, ran here, ran
elsewhere in the fleet, ran on another provider, failed with a Zoomies fault,
cleanup pending, cleanup converged. Names and label cardinality are ZF-205's
to settle; the counts are what Gate F is computed from.

## Severity

| Severity | Definition | Gate F |
| --- | --- | --- |
| **Critical** | Compromise of a credential, a host or another job's data; loss of fleet state | Zero unresolved |
| **High** | A job the platform should have run did not, or was lost, with no reasonable workaround; a recovery path that needs database surgery | Zero unresolved |
| **Medium** | A reasonable workaround exists, or the effect is confined to diagnostics, timing or one deployment shape | Counted and listed |
| **Low** | Cosmetic, copy, or a limitation the docs already state | Listed |

## Gate F targets as adopted

| Target | As proposed | Adopted | Note |
| --- | --- | --- | --- |
| Observation | 7 consecutive days, 200 representative attempts | 7 days, 200 attempts, of which at least 20 intended failures and 20 cancellations, and at least three bursts in which the queued jobs exceed the fleet's total capacity | 200 attempts over 7 days is about 29 a day and never exercises contention on its own; the bursts are what prove scheduling under load rather than elapsed time |
| Denominator | "eligible controlled attempts" | completed jobs on non-demo installations whose runner this controller created; `waiting` jobs and jobs GitHub dispatched elsewhere are counted and reported but excluded | The seeded demo installation has no GitHub behind it and must never appear in a count; `retention.runners` defaults to 7 days, so raise it on the reference instance or export evidence daily |
| Outcome rate | ≥ 99% of eligible attempts reach the expected terminal outcome without a Zoomies-caused failure | same; a Zoomies-caused failure is `Job.RunnerFault` set, or a runner that failed before registration for a job that then ran elsewhere or expired | 200 attempts allows two Zoomies faults; report the count, not only the percentage |
| Scheduling latency | p95 ≤ 10 s from eligible to create task issued, with capacity and a prepared image | p95 ≤ `scheduler.interval` + 2 s from the later of the delivery's receipt and eligibility to the create task being issued, reported beside the interval it ran at | With the default 10 s interval, the wait for the next tick alone is uniform over 0 to 10 s and its p95 is about 9.5 s before any processing, so the figure as proposed fails by construction or passes by noise. The reference run may use a 5 s interval and keep 10 s if the owner prefers the round number |
| Cleanup convergence | within 5 min of the retention deadline with dependencies healthy; offline hosts converge within 5 min of recovery | same; convergence is measured to `Runner.CleanedUpAt`, which has landed and which closes at the first cleanup signal rather than the last, so read it as a lower bound | `agent.finished_retention` is the deadline for a finished runner |
| Restore | demonstrated on a clean environment; reference target 30 min | same | Record the achieved time and the backup's age |
| Second operator | completes setup and diagnoses an injected failure from the docs and UI | same | Cannot be produced by the agent; the gate stays pending until a person does it |
| Defects | zero unresolved critical or high; zero unexplained lost jobs; zero cross-scope access | same | |

None of these are commitments to anyone outside the project. They are what
"good enough to call it a trusted-workload beta" means here.
