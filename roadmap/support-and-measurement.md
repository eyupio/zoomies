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
| Podman backend | Shape: probe, defaults, DinD refusal and the SELinux cache suffix are unit-tested | Runtime: no test starts a Podman daemon | A Podman host in the harness |
| `process` backend | Shape: layout, signals, process groups, archive tamper check, JIT config off argv | Runtime: no test runs a real runner process | A bare host in the harness; it is the least isolated backend and the docs already say so |
| macOS controller | Build; launchd plist rendering | Everything else; the docs say development only | Nothing planned |
| Debian, Fedora, Alpine hosts | Installer parses under `dash` and `shellcheck`; detection is unit-tested | No install runs on them in CI | One real install per distribution in the installer tier of the harness |
| `compose` and `docker` deployments | Templates are unit-tested; images build in CI | No CI job brings a deployment up and joins an agent to it | A compose-up smoke test in the harness |
| GitHub Enterprise Server | `github.api_base_url` is validated | No test speaks to a GHES | Access to a GHES; not in the first assignment |

Cross-compilation is not qualification. A row moves right only when a test
runs on the thing.

## The moments to timestamp

Gate F asks for two timings and one convergence, and each needs a start and an
end that the store records. Today's fields are listed in the baseline; the
table below says which the contract uses and which are missing.

| Interval | Start | End | Today |
| --- | --- | --- | --- |
| **GitHub queue wait** (not Zoomies' time) | GitHub's `started_at` on the job, absent for a queued job | `Job.QueuedAt` as first seen here | Reported by `JobEvent.Source`; the poller-only case inflates it and must be labelled |
| **Scheduling latency** (the Gate F p95 ≤ 10 s) | the job became *eligible*: an enabled pool claims its labels, its installation is healthy, capacity exists or can be created | the create task was *issued* to an agent | **Missing both ends.** `JobEvent{claimed}` is close to eligible but is recorded once, not re-evaluated; `Runner.CreatedAt` is the row, not the task. ZF-002 adds `Job.EligibleAt` (re-set when eligibility is regained) and `Runner.TaskIssuedAt`. |
| **Configured scaling delay** (not Zoomies' time) | eligible | the scheduler's hold expires (`scheduler.scale_up_delay`, the start-failure backoff) | Reasons are in `ScalingEvent`; the delay is subtracted, not counted against the platform |
| **Provisioning** | task issued | `Runner.ContainerStartedAt` | Exists; `Runner.ImagePullDuration` separates the pull where the backend can |
| **Registration** | `Runner.ContainerStartedAt` | `Runner.RegisteredAt` | Exists |
| **Assignment** (GitHub's decision, not Zoomies') | `Runner.RegisteredAt` | `Job.StartedAt` | Exists; GitHub may hand the job to a different idle runner, which the contract accepts and reports as *ran elsewhere in the fleet* |
| **Execution** | `Job.StartedAt` | `Job.CompletedAt` | Exists |
| **Cleanup convergence** (the Gate F ≤ 5 min) | `Runner.FinishedAt`, or the retention deadline where one applies | the registration is gone from GitHub, the workload is gone from the host, the work directory is gone | **Missing the end.** The row reaching `removed` is not the same as the three resources being gone. ZF-002 adds `Runner.CleanedUpAt`, set only when all three are confirmed, and a `cleanup_pending` reason when one is not. |

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
| Observation | 7 consecutive days, 200 representative attempts | 7 days, 200 attempts, of which at least 20 intended failures and 20 cancellations | The roadmap's floor, with the mix made explicit so the denominators are known in advance |
| Outcome rate | ≥ 99% of eligible attempts reach the expected terminal outcome without a Zoomies-caused failure | same | 200 attempts allows two Zoomies faults; report the count, not only the percentage |
| Scheduling latency | p95 ≤ 10 s from eligible to create task issued, with capacity and a prepared image | same, once `EligibleAt` and `TaskIssuedAt` exist | Report registration and start latency beside it, unweighted, so GitHub's part is visible |
| Cleanup convergence | within 5 min of the retention deadline with dependencies healthy; offline hosts converge within 5 min of recovery | same, once `CleanedUpAt` exists | `agent.finished_retention` is the deadline for a finished runner |
| Restore | demonstrated on a clean environment; reference target 30 min | same | Record the achieved time and the backup's age |
| Second operator | completes setup and diagnoses an injected failure from the docs and UI | same | Cannot be produced by the agent; the gate stays pending until a person does it |
| Defects | zero unresolved critical or high; zero unexplained lost jobs; zero cross-scope access | same | |

None of these are commitments to anyone outside the project. They are what
"good enough to call it a trusted-workload beta" means here.
