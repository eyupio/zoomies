# Zoomies: foundation-first follow-on roadmap

Version: 1.0 · Prepared: 6 September 2026  
Repository: https://github.com/eyupio/zoomies  
Audience: a coding AI agent implementing the next development programme

## 1. Objective and starting assumption

Make Zoomies a dependable, secure and easy-to-operate self-hosted GitHub Actions runner platform. Prove that foundation in real use before expanding into automated infrastructure provisioning and paid hosting.

The owner is implementing the current improvement plan. **Assume that plan, including its remaining waves and subsequently reported fixes, completes before this roadmap begins.** Its work is a prerequisite, not a new backlog to repeat. At implementation time, inspect the resulting branch and map existing solutions to this document. Extend or verify working capabilities; do not build replacements merely because they appear below.

Zoomies is only a few days old. A passing test suite, a long feature list and a completed review do not constitute operating history. This roadmap therefore distinguishes implementation completion from operational acceptance. No existing deployment, benchmark, paid customer or production-readiness claim is assumed.

This is an implementation handoff, not authorisation to purchase servers, publish releases, change live customers' infrastructure or activate billing. Use existing authorisation where it covers an action. Finish local, reversible implementation and reviewable deployment preparation without repeatedly asking for routine decisions.

### Evidence used to prepare this plan

Repository architecture and implementation were inspected at commit `67ffddc4e60708f261517a826e59a318b7aa5b8b`. The live `IMPLEMENTATION_PLAN.md` was also read on 6 September while further work was underway. File locations below are navigation hints from that baseline; the coding agent must resolve their current equivalents.

The existing architecture includes a Go controller and agents, a pure scheduler, SQLite, a Svelte web UI, Docker/Podman/process backends, an installer, GitHub App integration, ephemeral runners, RBAC, OIDC, audit records, live events, usage reporting, image prewarming and capacity-demand notifications. Preserve those investments. See the [repository architecture](https://github.com/eyupio/zoomies/blob/67ffddc4e60708f261517a826e59a318b7aa5b8b/docs/architecture.md) and [current implementation plan](https://github.com/eyupio/zoomies/blob/main/IMPLEMENTATION_PLAN.md).

## 2. Wider roadmap and dependency gates

| Phase | Outcome | Dependency | Delivery boundary |
|---|---|---|---|
| 0 | Reconciled baseline and measurable acceptance criteria | Current improvement plan complete | A small, evidence-backed implementation backlog |
| 1 | Correct, secure runner lifecycle and scheduling | Phase 0 | Foundation candidate; no new infrastructure providers |
| 2 | Usable setup, maintenance and recovery | Phase 1 contracts established | An application an operator can run without developer assistance |
| 3 | Real usage and failure-recovery evidence | Phases 1–2 implemented | Gate F: trusted self-hosted beta |
| 4 | Add and maintain existing hosts from the web UI | Gate F | IP/SSH onboarding, including rented dedicated servers |
| 5 | Light paid hosted control-plane pilot | Phase 4 accepted, Gate F retained | Gate C: invitation-only service; customers own compute |
| 6 | Provider-managed on-demand hosts | Phase 4 accepted; Phase 5 optional for self-hosted use | Gate P: one proven provider and bounded cloud scaling |
| 7 | Dedicated-server fleet with isolated disposable VMs | Phase 4 and foundation gates; can precede Phase 6 | Gate V: managed-compute technical readiness |
| 8 | Fully managed paid compute | Gates C and V; Gate P required if offering cloud overflow | Gate M: limited paid compute pilot, then evidence-led expansion |

**Immediate programme: Phases 0–3.** Phase 4 is the first substantial feature expansion. Phases 5–8 express the commercial direction and have their own acceptance gates; they are not instructions to build the entire business in one change.

Use milestones rather than calendar promises. Phase 3 necessarily includes elapsed observation and real users; an agent cannot accelerate that by declaring tests equivalent. While waiting for external evidence, complete other ready work within the authorised scope and record the exact outstanding gate.

## 3. Architecture and delivery rules

1. Preserve the single-binary experience, SQLite and the existing controller/agent model. Keep scheduling decisions pure and deterministic. Do not introduce Kubernetes, a message broker, a separate database service or a microservice estate without measured need.
2. Distinguish **host provisioning** from **runner execution**. A provider creates a host; the agent executes workloads on it. Monthly rented hardware can run disposable jobs. Provider billing intervals and customer billing units are independent concepts.
3. Keep one self-hosted instance within one administrative trust domain initially. Several GitHub installations belonging to that team still require correct target scoping. Public shared tenancy is a separate product boundary.
4. Keep existing manual setup and APIs available. Build UI workflows on the same validated application operations; do not duplicate orchestration in Svelte or CLI commands.
5. Make network side effects restart-safe, bounded and observable. Use durable operation identities, ownership checks and reconciliation. Never claim exactly-once execution across GitHub, agents and provider APIs.
6. Reuse current UI components, design tokens, generated API types, error shapes, auth and audit conventions. Update OpenAPI, generated clients and user documentation alongside behavioural changes.
7. Prefer additive schema changes. Never rewrite applied migrations. Discover the repository's migration ordering rules, including existing filenames with repeated numeric prefixes, before adding a uniquely identified migration.
8. Keep commits and PRs small and independently understandable. Each work package can contain several narrow PRs. Use the current repository's checks; add targeted behavioural tests for concrete new failure modes, not tests that simply repeat implementation details.
9. Start new capabilities disabled until their acceptance criteria are met. A disabled feature must not start background provisioning, create resources or require credentials on existing installations.
10. Record dependencies and architecture decisions. A library needed for SSH or a VM runtime is reasonable; explain its purpose, maintenance and security implications. Do not invent a large plugin framework for one integration.
11. Treat logs and support exports as potentially sensitive. Redact platform-managed credentials and explicitly document the limits of recognising arbitrary secrets emitted by customer workflows. GitHub likewise cautions that automatic redaction is not guaranteed. [GitHub secure-use reference](https://docs.github.com/en/actions/reference/security/secure-use)
12. Never auto-upgrade hosts, remove customer-owned machines or silently weaken trust checks as a recovery shortcut.

### Work-package record

For every ID below, maintain: status (`not_started`, `in_progress`, `implemented`, `validated`, `blocked`, or `superseded`), dependency IDs, relevant existing implementation, commit/PR, tests and results, operational evidence, rollback/recovery notes and outstanding risks. `Superseded` requires a link to the existing solution and evidence that it meets the same criteria.

Keep this in `docs/roadmap-progress.md` when implementing. Put small design decisions in `docs/decisions/` and gate evidence in `docs/validation/`. Do not overwrite the owner's current implementation plan or erase unresolved findings.

## 4. Phase 0 — reconcile and establish the baseline

### ZF-001 — reconcile completed work and define the next slice

**Implement:** Inspect the current branch, applicable `AGENTS.md`, implementation plan, API schema, architecture, security model, release workflow and tests. Map this roadmap to what now exists. Identify any regression that prevents reaching the presumed completed baseline. Historical symptoms, including stuck registration or misleading external-job status, become regression scenarios if already fixed rather than repeated feature tickets.

Create the progress record and a concise architecture map. Record the tested commit, schema version, controller/agent versions, supported deployment modes and outstanding evidence. Keep the existing branch's unrelated work intact.

**Acceptance:** Every Phase 1–3 package is classified as existing-and-needs-validation, extension, or new capability. Each required fix has a reproducible problem and a bounded change. The agent can name the next ready package without asking the owner to redesign the roadmap.

**Locations:** `IMPLEMENTATION_PLAN.md`, `docs/architecture.md`, `docs/security.md`, `api/openapi.yaml`, `.github/workflows/`, `Makefile`.

### ZF-002 — support matrix, invariants and measurement contract

**Implement:** Select one initial reference configuration: Linux amd64 controller, Linux amd64 Docker agents, one embedded-agent test and one remote-agent test. Start with a supported Ubuntu LTS after checking the installer; record the exact OS and runtime versions. Preserve other existing modes, but qualify Podman, process, arm64 and macOS only when their actual runtime tests exist. Cross-compilation alone is not qualification.

Define timestamps and counters for job observation, scheduling eligibility, allocation, runner registration, job execution, completion and cleanup. Separate GitHub waiting/approval time, configured scaling delay, provider boot time and image download from Zoomies' own processing time. Identify failures caused by Zoomies separately from deliberately failing workflows.

Adopt the proposed Gate F targets in Phase 3, recording the reference hardware, dataset and any justified adjustment before measurement. Define severity: critical covers compromise/data loss; high covers core job or recovery failure without a reasonable workaround.

**Acceptance:** Support claims match test evidence; success and failure denominators are explicit; unknown observations are not counted as success. The core safety invariants in Phase 1 have concrete tests or named operational exercises.

## 5. Phase 1 — correctness and security under failure

### ZF-101 — enforce GitHub target boundaries everywhere

**Implement:** Verify that job-to-pool eligibility requires the correct installation and repository/organisation scope before label matching. Apply one eligibility policy to webhook ingestion, polling, scheduler assignment, queue explanations, quotas, statistics and capacity-demand generation. A team using two installations with identical labels must not cause runners to register to the wrong target.

Scope fallback freshness and rate-limit backoff per installation. A healthy webhook stream for installation A must not suppress polling for B; a rate-limited B must not stall A.

Account for GitHub's final dispatch decision: Zoomies cannot guarantee that a newly created org runner receives the particular job that prompted its creation. Use repository registration or enforced GitHub runner-group access for repository isolation. Labels and cache path names alone are not access control. Reject unsupported promises of private repository caches on broadly accessible org runners.

**Acceptance:** Tests use two installations, overlapping labels, differing webhook health and rate limits, plus two repositories in one organisation. No cross-target allocation, cache authorisation, job linking or accounting occurs. Ineligible work has an actionable reason; jobs run elsewhere remain accurately distinguished.

**Locations:** `internal/scheduler/labels.go`, `scheduler.go`, `internal/controller/{webhooks,poller,reconcile,capacity_demand}.go`, `internal/github/`, `internal/store/`.

### ZF-102 — make runner and agent reconciliation convergent

**Implement:** Document the authoritative runner/task transitions and invariants. Cover duplicate tasks, lost responses, out-of-order reports, agent/controller restarts, registration timeout, jobs cancelled before startup and late completion after a host was declared lost. Use stable workload identity and inspect/adopt before replaying creation. A timeout means the outcome may be unknown, not that creation certainly failed.

Enforce one active controller scheduler per state directory/database; refuse an accidental second writer/scheduler process rather than adding distributed leader election. A restored controller must not run concurrently with its unfenced original. Verify host ownership on every task result, report and log upload. Add a generation/session fence where needed so a superseded agent cannot update a replacement's resources. A reconnected host must reconcile its old workloads before accepting new work. Recover outstanding tasks from durable state without minting endless new JIT credentials.

A controller losing contact does not prove a GitHub job stopped. Do not automatically redispatch that workflow or promise exactly-once job effects; recover runner capacity and report the uncertain job accurately.

**Acceptance:** Kill/restart at each create/register/acknowledge/complete/remove boundary. One intended runner identity has at most one adopted active workload. Replayed results do not resurrect terminal runners or release capacity twice. Invalid images produce bounded retries and a visible error. Long jobs survive supported agent/controller restarts or fail in the explicitly documented way.

**Locations:** `internal/controller/{agents,reconcile,heartbeat}.go`, `internal/agent/`, `internal/backend/`, `internal/store/`.

### ZF-103 — reserve host resources and enforce bounded admission

**Implement:** Extend slot-based placement with CPU and memory reservations and a configurable host reserve. Track allocatable, reserved and observed usage separately; observed low utilisation must not erase a reservation. Reserve resources for starting workloads and pending create operations as well as running jobs. Release only once, after a terminal or reconciled outcome.

Report CPU topology where detectable and distinguish logical CPUs from physical cores. Use conservative explicit allocation units; a hardware thread is not a promised modern dedicated core. Enforce memory limits and bounded CPU allocation in supported runtimes. Include DinD sidecar and later VM overhead in the resource model.

Stop new admission on unhealthy, cordoned or disk-pressured hosts. Default CPU overcommit off; memory reservation overcommit unsupported initially. Define migration behaviour for existing pools without resource limits: retain their configured slot cap, use a documented conservative reservation profile and show the assumption. Unknown host resources must have a visible conservative fallback, not unlimited capacity.

**Acceptance:** Multiple pools competing in one tick cannot oversubscribe allocatable resources. Pending tasks count; restart rebuilds reservations consistently; failed operations release correctly. Placement reports whether slots, CPU, memory, disk, backend, labels or policy blocked it. A 32 GB host cannot admit twenty 4 GB runners.

**Locations:** `internal/scheduler/`, `internal/store/models.go`, `internal/agent/protocol.go`, `internal/backend/`, Hosts and Pool UI.

### ZF-104 — verify control-plane access and secret boundaries

**Implement:** Build an endpoint/action access matrix from the actual router, including SSE, runner logs, downloads, diagnostics, join tokens, WebSocket routes if any, and configuration mutations. Verify session expiry/revocation, CSRF/origin checks, proxy trust and scoped API token restrictions. Continue using current RBAC; add permissions only for genuinely new operations.

Prove that an agent credential can act only for its own host and assigned resources. Bound request sizes, streams, task queues and authentication work. Keep GitHub App keys, encryption keys and agent/provider credentials out of runner-visible mounts, settings responses, errors, metrics and support exports. Test stored strings and hostile job output in the UI, including terminal escape handling and unsafe links.

Make trust profiles explicit: existing Docker/Podman/process modes serve a trusted administrative domain. Privileged DinD and host Docker socket access must never be represented as isolation for unrelated customers.

**Acceptance:** Anonymous, viewer, operator, admin, scoped API token and agent identities are covered by positive and negative tests. Cross-host reports/logs are rejected. Revocation terminates or bounds access to existing long-lived streams. Synthetic platform secrets are absent from persisted errors and exported diagnostics. Existing safe deployment paths remain usable behind TLS proxies.

**Locations:** `internal/auth/`, `internal/api/`, `internal/cryptox/`, `internal/config/`, runner/log UI and `docs/security.md`.

### ZF-105 — bound cleanup, retention and external failure handling

**Implement:** Verify cleanup across local workloads, GitHub runner registrations, work directories, task records and cache/image retention. Persist unsuccessful cleanup with retry timing and a visible reason. Cleanup must select only owned resources; similarly named containers or other GitHub runners are out of scope.

Protect active work and in-use images/caches from retention sweeps. Bound retries with jitter and honour provider/GitHub backoff information where supplied. Configure startup, idle, drain and maximum-job-duration policies separately; do not make a short provisioning timeout the limit for legitimate long jobs. Apply bounded recovery after Docker outage, disk full, network loss and GitHub outage without growing queues indefinitely.

**Acceptance:** Cancel-before-start, cancel-during-run, timed-out registration, daemon restart and disk exhaustion leave no unexplained resources. Failed deletion stays visible and retryable. A host returning after cleanup reconciles stale workloads safely. Recovery never deletes an unowned resource or interrupts busy work as ordinary scale-down.

**Locations:** `internal/controller/`, `internal/backend/`, `internal/agent/`, store retention queries, Problems and Host detail UI.

## 6. Phase 2 — make the foundation operable and highly usable

### ZF-201 — prove the first successful job through the UI

**Implement:** Complete a coherent journey from first login to GitHub App setup, manual agent enrolment, compatible pool creation, an exact workflow snippet and a successful real job. Reuse the current setup pages and wizard. Show whether the configured installation actually has access to the chosen repository and the required runner operations.

Keep progress across page refreshes without retaining credentials in browser storage. Expose clear recovery for invalid credentials, inaccessible repository, wrong labels, expired join token, unavailable runtime, unreachable controller and bad runner image. Success means the agent heartbeats and the job runs; saving configuration alone is not setup completion.

**Acceptance:** A new operator can complete the journey using documented prerequisites without editing the database or reading server logs. Exercise the real application/API with Playwright, including one failed step and recovery. Verify narrow mobile widths, keyboard focus, labels, readable errors, loading/empty/disconnected states and copyable commands. Add targeted interaction assertions instead of a large screenshot-only suite.

**Locations:** `web/src/routes/{Bootstrap,GithubSetup,AddHost,PoolWizard,Jobs}.svelte`, existing UI components and API handlers, `docs/quickstart.md`.

### ZF-202 — actionable day-to-day diagnostics

**Implement:** Extend current Problems, timeline and logs into a consistent answer to “why is this job not running?” Show expected versus observed state, last agent contact, registration stage, effective pool constraints, relevant quota and the next sensible action. Distinguish stale data, a paused pool, GitHub waiting for approval, external runners and Zoomies failures.

Provide a read-only diagnostic command and downloadable support bundle through existing CLI/API patterns. Include versions, redacted effective config, recent structured events and health results. Omit credentials and workflow log bodies by default; require explicit selection for potentially sensitive output. Role-check access and bound bundle size.

**Acceptance:** Seed realistic failures and verify the UI points to their cause and recovery. Correlate job, runner, host, installation and request IDs. An operator can diagnose a stuck registration without SQL. Diagnostics remain accessible during a provider outage and fail gracefully if one subsystem cannot be queried.

**Locations:** `internal/controller/{problems,derived,stats}.go`, `internal/api/`, `cmd/zoomies/`, job/runner/host detail components.

### ZF-203 — implement and prove backup/restore

**Implement:** Use a SQLite-consistent backup method; copying only a live database file while ignoring WAL is not sufficient. Include configuration, schema/build metadata and an inventory of required secrets. Back up the encryption key through a separately protected route and explain that a database backup alone cannot decrypt secrets. Add integrity checks and retention with protected filesystem permissions.

Restore into a clean stopped instance, verify compatibility and start in a fenced recovery mode: no new scheduling or provisioning until the operator reconciles live agents and external resources. Restore must not revive expired/revoked sessions or credentials solely because an old database contains them; define and test the required invalidation/rotation policy. Recover agent trust deliberately.

**Acceptance:** Restore a backup into an isolated clean environment, authenticate, reconnect a controlled agent and run a job after reconciliation. Wrong/missing keys and incompatible schemas fail safely. A corrupt backup never overwrites the only working copy. Record actual recovery time and the backup's recovery-point age.

**Locations:** `internal/store/`, `internal/cryptox/`, CLI, installer/deploy tooling and operator docs.

### ZF-204 — safe releases, upgrades and version compatibility

**Implement:** Define a controller/agent compatibility policy that supports a documented rolling upgrade, or explicitly requires a coordinated upgrade when the protocol cannot. Display skew and prevent incompatible agents from accepting work. Verify clean install and upgrade from the latest supported release using real build artifacts.

Check artifact integrity, preserve ownership/configuration and back up before database migration. A failed upgrade must leave a recoverable previous deployment. Document whether rollback can use the existing database or requires restoring the pre-upgrade backup; do not run an old binary against a newer schema speculatively. Drain hosts before disruptive agent/runtime upgrades.

Keep CI/release dependency checks, vulnerability scanning and immutable third-party workflow references proportionate and current. Separate trusted release jobs from code execution on experimental Zoomies runners. Preserve an independently hosted recovery/release workflow.

**Acceptance:** Test successful upgrade, interrupted upgrade, incompatible agent, bad artifact and restore-based rollback. No silent config reset or removal of active workloads. Version, commit and image digest identify the deployed code consistently. An operator can perform the procedure using the shipped documentation.

**Locations:** `internal/installer/`, `internal/agent/protocol.go`, `internal/store/`, `install.sh`, `deploy/`, `.github/workflows/`.

### ZF-205 — establish usable telemetry and bounded history

**Implement:** Build on existing metrics and usage pages. Measure queue age, scheduling latency, registration latency, successful cleanup, reconciliation errors, host capacity/pressure and GitHub backoff. Keep high-cardinality identifiers in logs/events rather than unbounded metric labels. Show collection window, timezone and data freshness; distinguish observed measurements from estimates.

Keep job history, audit retention and log buffering bounded. Use server-side filtering/pagination and indexes where measurements show need. Verify reconnect/resync does not duplicate rows or revert newer state. Long-running operations use durable operation IDs rather than hanging an HTTP request indefinitely.

**Acceptance:** On the recorded reference configuration, exercise at least 10,000 historical jobs and 10 simulated hosts while running a live job. Representative paginated API reads should have p95 below 500 ms and primary navigation below 2 seconds on that setup, excluding external calls. Record achieved results and justified limits; these are initial engineering targets, not an SLA. No ongoing memory growth from abandoned streams, unbounded history or repeated reconnects.

**Locations:** `internal/controller/{metrics,stats,logs}.go`, `internal/events/`, `internal/store/`, `web/src/lib/api/`, existing grids, Overview and Usage.

## 7. Phase 3 — real use, failure drills and the foundation gate

### ZF-301 — extend the existing real end-to-end harness

**Implement:** Extend `test/e2e/`, rather than replacing it. The inspected harness exercises a real GitHub App and workflow through polling, but needs credentials and can skip; fake webhook tests do not establish real webhook operation. Add explicit result categories: passed, failed, not-run and blocked. A missing secret or daemon cannot become a passing release gate.

Cover controller + remote Docker agent, scale from zero runners on an existing host, workflow completion and cancellation, registration cleanup, a Docker build, a service container, a failed workflow and a legitimate long-running job. Exercise webhook delivery and polling fallback in separately identified runs. Add scoped test fixtures and cleanup records so a crashed harness can be cleaned safely on the next run.

**Acceptance:** Retain links to real workflow runs and redacted lifecycle evidence. Verify both local workloads and GitHub registrations disappear after cleanup. Tests fail on unexplained orphaned resources and report residual cleanup separately when an upstream outage prevents deletion.

### ZF-302 — run restart and recovery drills

**Implement:** Add repeatable, bounded fault exercises around the real controller/agent path. Inject failure at the lifecycle points in ZF-102, webhook duplicates/out-of-order delivery, agent network partition, Docker unavailability, disk pressure, GitHub 429/5xx, token revocation and browser disconnect. Use deterministic fakes for failure cases that cannot responsibly be forced against GitHub, with at least real restart, connectivity and restore exercises.

For each drill, document expected effect, actual effect, recovery time, cleanup outcome and whether a human action was needed. Log loss or telemetry gaps are findings even if the workflow eventually succeeds.

**Acceptance:** Recovery requires no database surgery. No unlimited restart/provision loop, privilege bypass, lost durable operation or accidental cleanup of external resources. Unrecoverable cases become actionable incidents with documented recovery, not silently green statuses.

### ZF-303 — run the controlled beta and publish a readiness record

**Implement:** Begin with the owner's disposable test repository and trusted workloads. Then run a small representative set of the owner's actual CI jobs. Retain another CI provider for release and recovery so Zoomies failing cannot prevent Zoomies being fixed. Introduce at least one additional operator to validate documentation and usability.

Capture defects in the progress record, fix the narrow root causes and repeat affected evidence. Preserve earlier evidence but identify which results remain valid after a change. A lifecycle/security change resets the affected observation period; a copy edit need not reset every test.

**Gate F — all required before feature expansion:**

- The reference configuration passes the established CI gates, real workflow tests and relevant failure drills.
- At least seven consecutive observation days and 200 representative real job attempts, including success, intended workflow failure and cancellation. This is an initial evidence floor, not proof of broad production readiness.
- At least 99% of eligible controlled attempts reach their expected terminal outcome without a Zoomies-caused failure. Report exact counts and exclusions; the observation window also has zero unresolved critical/high defects, zero unexplained lost jobs and zero cross-scope access failures.
- With capacity and a prepared image available, p95 time from scheduling eligibility to issuing the create task is at most 10 seconds on the reference setup. Report registration/start latency separately rather than attributing GitHub delays to Zoomies.
- With dependencies healthy, owned ephemeral resources converge to cleanup within five minutes after the configured retention deadline. Offline-host cases remain visibly pending and converge within five minutes of recovery.
- Backup restore and supported upgrade/rollback are demonstrated on clean environments; the reference restore target is 30 minutes. Record achieved time rather than promising it commercially.
- A second operator completes setup and diagnoses an injected failure using the UI and documentation, without undocumented developer intervention.
- Known limitations, supported configurations, exact build and evidence links are published in the readiness record. The release remains a trusted-workload beta; unrelated-customer isolation is not claimed.

If evidence cannot be collected in the agent's environment, complete the harness, fixtures and operator runbook, mark Gate F pending and state precisely what access or observation remains. Never invent the seven days, job counts or operator feedback.

## 8. Phase 4 — manage existing hosts from the web UI

This is the first feature expansion after Gate F. The initial target is an existing reachable Linux host, including an already rented OVH/Kimsufi dedicated server. Server purchase, OS reinstall and automatic bare-metal ordering are outside this phase.

### ZF-401 — durable host onboarding operations

**Implement:** Add a small operation service within the existing application for host onboarding. Persist operation ID, actor, target, phase, desired configuration, timestamps, attempts, redacted error and resulting host ID. Bound concurrency and lock one active onboarding operation per target. Reuse an idempotency key for a retried submission.

Recommended states: `queued`, `inspecting`, `awaiting_trust`, `awaiting_credentials`, `installing`, `verifying`, `succeeded`, `failed`, `cancelled`, `cleanup_required`. Only expose actions valid for the current state. The HTTP request creates or reads an operation; a background worker performs the work. Reuse existing live events for progress and resynchronise after browser reconnect.

**Acceptance:** Refresh, double-click and network retry do not install twice. Cancellation prevents later steps and reports any completed changes. Restart resumes from inspected state or requests credentials again. Success requires authenticated agent enrolment, heartbeat and usable runtime. Partial installation is represented honestly and has a safe retry/cleanup route.

**Locations:** new `internal/provisioning/` or equivalent small package, `internal/store/`, controller background worker, API/OpenAPI and AddHost UI.

### ZF-402 — secure SSH bootstrap and preflight

**Implement:** Support host/IP, port, username, SSH private key with optional passphrase, or password. Prefer keys, but make password-based bootstrap functional for providers that issue an initial password. Support a separate sudo credential only where the installer needs it. Use the existing Go SSH dependency where appropriate and reuse the non-interactive installer/join logic.

Obtain and display the server's host-key fingerprint before transmitting authentication credentials; require an operator to verify/pin it or use an already trusted value. A changed key blocks reconnection. Do not offer a hidden insecure fallback. Run fixed, versioned commands with structured inputs; never interpolate hostnames, labels, passwords or user-supplied scripts into a shell command.

Restrict who may initiate SSH, which destination networks/ports are permitted and how names are resolved. Permit explicitly configured private networks for self-hosted LAN deployments. For hosted service deployments, deny control-plane, metadata, loopback and internal service destinations, with a separately configured customer gateway if private access is later required. Validate IPv4/IPv6 and pin a validated resolved address for the connection to prevent DNS rebinding. Apply similar destination policy to new configurable outbound callbacks.

Keep bootstrap secrets in memory for the active operation by default; never persist them in browser storage, argv, audit, ordinary database rows or logs. A controller restart can require re-entry. Do not claim reliable memory zeroisation in Go. If persisted secret references are later introduced, give them encryption, explicit expiry, access controls and deletion after bootstrap. Redeem a short-lived single-use join token; ordinary agent control then uses outbound authenticated connections.

**Acceptance:** Fake/real SSH fixtures cover bad key, changed key, expired credential, password auth, sudo denial, command-injection strings, IPv6 and forbidden targets. Failed preflight makes no host changes. No bootstrap secret appears in an operation record, process arguments or diagnostics. TLS verification remains enabled when the enrolled agent connects.

### ZF-403 — complete the guided host journey

**Implement:** Present connection details, host trust, read-only preflight, proposed changes, install progress and verification. Preflight checks OS/architecture, disk, CPU/RAM, runtime, service manager, permissions, existing Zoomies identity and outbound reachability. Preview changes before installation, including any runtime installation or service replacement. Initial automation should support the reference OS/runtime; unsupported hosts receive the existing manual path with specific guidance.

Detect an existing host belonging to this controller and offer repair without duplicating identity. An existing agent belonging elsewhere requires explicit migration intent. Preserve existing services and firewall rules; do not open the Docker API or disable the firewall. Suggest conservative capacity using ZF-103 and let the operator choose labels/pools.

**Acceptance:** A clean supported host completes the UI flow and runs a real job. A second attempt is idempotent. Failed download, unsupported OS and unreachable controller recover in the UI. Keyboard/mobile users can verify trust, see progress and recover without losing non-secret inputs. Existing manual enrolment remains fully functional.

### ZF-404 — maintenance and ownership controls

**Implement:** Add safe repair, drain, reconnect, credential rotation and agent upgrade using current host actions where available. Separate “remove from Zoomies” from “uninstall the agent” and from provider destruction. Host records distinguish imported/manual ownership from resources created by Zoomies. An imported dedicated server is never eligible for automatic destruction.

Agent upgrades use verified artifacts and the compatibility policy from ZF-204. Drain blocks placement first, then waits for running work; forced interruption is a distinct action showing affected jobs. Re-request SSH access when required rather than retaining a root password for future maintenance. Keep a redacted operation history.

**Phase acceptance:** Complete onboarding, a deliberate failed attempt, recovery, agent restart, drain and supported upgrade on at least one real remote host. Re-run the foundation tests affected by provisioning. This evidence is required before marketing “manage hosts from the web UI”.

## 9. Phase 5 — light paid control plane, customer-owned compute

Recommended first commercial shape: invitation-only hosted Zoomies for a customer's own trusted fleet. Each customer pays their infrastructure provider directly. Container runner use remains within that customer's trust domain.

Across Phases 5–8, the initial deployment model keeps every worker agent bound to one customer controller. The first managed-compute offer uses customer-exclusive worker hosts as well as isolated control planes. Pooling one physical worker across unrelated customer controllers is a later architecture project; do not attach the same agent to several controllers or bypass host ownership to achieve it.

### ZF-501 — isolate each customer's control plane

**Implement:** Start with a separate controller process/container, database, encryption key, hostname, agent credentials, backup set and resource limits per customer. Reuse the existing application; build only the small service-management layer needed to provision and supervise those instances. Do not convert every core query to shared SaaS tenancy as the first commercial step.

Authenticate customer access and bind routing to an authoritative account-to-instance mapping. Never trust a customer-supplied hostname/header to choose another customer's data. Separate service-administrator operations, audit support access and keep worker credentials out of the service-management layer wherever feasible. Worker hosts must not carry a container socket capable of managing the hosted control plane.

**Acceptance:** Two test customers cannot access each other's API, live events, files, backups, secrets, agents or support bundles. Restoring or upgrading A does not affect B. Per-customer limits bound noisy-neighbour effects. An unavailable customer instance does not take down all account routing.

### ZF-502 — account lifecycle and simple subscription entitlements

**Implement:** Add invitation, account activation, subscription state and explicit plan limits. Prefer a fixed control-plane subscription initially; keep compute usage informational because the customer pays their provider. Use a payment provider's hosted checkout if payment integration is included. Store billing identities and entitlement state, not card details.

Process payment events through authenticated, durable, idempotent handling and periodic reconciliation. Test duplicate and out-of-order events. Define trial, active, grace period, suspended and closed states. Non-payment must not silently delete infrastructure, backups or running jobs; define what blocks new activity and what stays accessible for recovery/export.

**Acceptance:** Sandbox billing flows cover activation, duplicate webhook, failed payment, cancellation, restoration and support correction. Customer-facing usage and plan limits match server enforcement. Account closure explains retention and executes verified credential revocation and scoped deletion when due.

### ZF-503 — service operations and limited pilot

**Implement:** Automate per-customer backup checks, staged upgrades, health monitoring, support exports and offboarding. Define a measured support process and realistic initial service limits. Before launch, the owner completes pricing, service terms, privacy/retention and the project's applicable licence/contribution review; record these as business dependencies, not fabricated code-agent approvals or legal conclusions.

**Gate C:** Two isolated test accounts pass ZF-501/502, a customer restore and upgrade are demonstrated, and a small invited pilot runs for at least 14 observation days without unresolved critical/high defects. Retain an incident record and support-access audit. Live payments and external launch require the owner's applicable authorisation. No full shared-compute service is implied by this gate.

## 10. Phase 6 — provider-managed on-demand hosts

This can ship for self-hosted customers after Phase 4 without waiting for Phase 5. Hosted use also requires Gate C. Begin with customer-owned cloud accounts and one provider.

### ZF-601 — introduce the smallest viable provider contract

**Implement:** Separate `InfrastructureProvider` from the existing runner `Backend`. Define credential validation, supported regions/sizes/images, capacity estimate, create, lookup/list-owned, inspect and delete, plus optional capabilities such as interruption notices. Start with a fake adapter and one real adapter. Keep provider calls outside the pure scheduler and SQLite transactions.

Store provider/account identity, credential reference, host template, immutable operation identity, resource ID, ownership marker, desired/observed lifecycle, retry information and cost metadata with an observation timestamp. Represent unsupported capabilities explicitly; do not assume all providers can stop billing when a machine is stopped.

Recommended first adapter: DigitalOcean Droplets, subject to the implementer's live API check. It accepts cloud-init through `user_data`, and its pricing documentation exposes billable lifecycle rules and size information. Use those as adapter data, not hard-coded universal cloud assumptions. [DigitalOcean user data](https://docs.digitalocean.com/products/droplets/how-to/provide-user-data/), [pricing and sizes](https://docs.digitalocean.com/products/droplets/details/pricing/)

**Acceptance:** Contract tests cover pagination, eventual consistency, expired credentials, quota, unavailable sizes, 429, 5xx and context cancellation. Listing resources never exposes secrets. Manual/imported hosts remain outside provider deletion authority.

### ZF-602 — make cloud creation and deletion restart-safe

**Implement:** Extend the operation engine for `requested → allocating → bootstrapping → ready → draining → deleting → deleted`, with explicit failed, unknown-outcome and cleanup-required states. Reserve capacity and estimated budget before the API call. A timeout after creation triggers lookup/reconciliation using trusted account and operation identity before retry; it must not immediately create another host. Use provider idempotency mechanisms only where verified to exist.

Bootstrap a verified image with a short-lived, single-use, template/operation-scoped enrolment credential. Do not embed provider API keys or GitHub App keys in cloud-init. Account for cloud-init logs and provider-visible user data. A redeemed/expired token must be useless to later workflow code. Validate returned host capabilities before scheduling.

On failure, clean up created instances and owned ancillary resources, or persist a visible cleanup obligation. Tags aid discovery but do not alone authorise deletion; check trusted ownership records, provider account and immutable resource IDs. Scale-down fences admission, drains, verifies no pending creates/running jobs, then requests deletion and confirms disappearance.

**Acceptance:** Fault injection after every network/DB boundary creates at most the intended owned capacity after reconciliation. Ambiguous resources are quarantined for review. Failed deletion remains chargeable/unknown in the UI until provider state confirms otherwise. Browser retries and controller restarts cannot destroy imported resources.

### ZF-603 — bounded autoscaling, including zero hosts

**Implement:** Extend the existing capacity-demand design rather than discarding it. It currently describes an external advisory receiver; preserve that integration or explicitly version the contract. Add scoped desired-capacity reconciliation for the native provisioner, including the case where no eligible hosts exist yet. Compute demand from authorised eligible work and subtract compatible healthy, booting and pending capacity.

Treat demand as a desired target with durable reconciliation. Repeated notifications or new event IDs must not repeatedly add the same capacity. If changing event fields/semantics, version them and provide compatibility tests. Include installation/pool/template scope and reasons for blocked demand.

Set min/max hosts, scale-up batch size, queue-age threshold, cooldown, maximum pending creates, idle TTL, region/size allowlists and concurrent cost reservations. Estimate committed spend and enforce conservative admission limits. Present budget estimates honestly: delayed provider billing, egress, failed deletions and external account activity prevent a perfect hard currency ceiling. Expose a kill switch for new provisioning that keeps cleanup and running-job visibility active.

**Acceptance:** Zero hosts boot to a real completed job and return to the configured floor. Duplicate demand, simultaneous pools and slow boots cannot exceed host/pending limits. Invalid templates and unavailable regions stop with actionable reasons. Scale-down never kills busy hosts as an ordinary cost optimisation.

### ZF-604 — cloud UI, credentials and real lifecycle evidence

**Implement:** Add provider connection, restricted templates, estimated cost, scale policies and an operation/resource history using the existing UI patterns. Credentials are encrypted, redacted and scoped to an account/project where the provider supports it. Rotation validates the replacement before retiring the prior secret. Display estimates versus provider-confirmed usage distinctly.

**Gate P:** In an authorised disposable account/project, complete at least 20 create/run/drain/delete cycles, including controller restart, bootstrap failure and deletion retry. Reconcile the resource inventory and observed charges with the provider. No unexplained billable resource remains. Evidence includes one scale-from-zero burst and one multi-pool burst bounded by the configured budget/host policy. Stub-only tests do not pass this gate.

Afterward add another adapter only in response to actual users: Hetzner Cloud for hourly VMs or AWS EC2 for broader capacity are candidates, each requiring current API/billing verification and the same contract suite. Serverless container runtimes require explicit Docker/service-container capability checks; they are not automatically interchangeable with a general-purpose runner host.

## 11. Phase 7 — dedicated servers and isolated disposable VMs

Cheap dedicated hardware is a fixed monthly capacity source. Zoomies can create ephemeral execution environments on it without renting a new physical server for every job. An existing Kimsufi server can already join through Phase 4; bare-metal provider APIs are optional management enhancements. [OVH Kimsufi catalogue](https://eco.ovhcloud.com/en-gb/kimsufi/)

### ZF-701 — benchmark a real dedicated-host reference

**Implement:** Define a reproducible workload suite: Go build/test, Node build/test, Docker build, service containers, parallel matrix jobs, cold/warm images and sustained writes. Run against an authorised available dedicated host and the cloud reference. Capture physical/logical CPU topology, memory, disk type/space, runtime, kernel and software versions.

Measure throughput, p50/p95 job duration, start delay, peak memory, disk pressure and safe concurrency. Include host/VM overhead and leave operational headroom. Store monthly hardware cost as configurable dated input; do not hard-code a Kimsufi promotion or assume stock. Calculate cost per completed reference job and capacity utilisation, not just advertised cores per pound.

**Acceptance:** Publish recommended runner sizes and concurrency backed by actual runs. A 32 GB configuration reserves memory for the host and all sidecars/VMs. Benchmarks show when adding jobs hurts completion time. Unmeasured estimates stay clearly labelled. Hardware purchase remains a separate authorised action.

### ZF-702 — add one VM execution backend

**Implement:** Select one Linux VM implementation through a short decision record and a bounded prototype, then implement it behind the existing backend interface. Firecracker is a candidate; validate KVM, kernel, networking and image support on the chosen hardware. Use its maintained security guidance and jailer where applicable; do not write a hypervisor. [Firecracker project](https://github.com/firecracker-microvm/firecracker)

Use the host agent to create, inspect, log and remove each guest; avoid a second competing scheduler. Build versioned, verified guest kernel/root filesystem images with an ephemeral GitHub runner and Docker support where offered. Give each job a clean writable disk and scratch area, explicit CPU/memory/disk limits and a unique network identity. Manage the privileged host helper narrowly and keep its control socket inaccessible to guests.

Support one job per guest and teardown after completion. Defer snapshot reuse and live migration until clean boots and cleanup are proven. Never reuse a guest snapshot containing another job's credentials or writable state. Hostinger's [Fireactions](https://github.com/hostinger/fireactions) provides a useful implementation reference for ephemeral GitHub runners on Firecracker; assess its design rather than adding a second controller product inside Zoomies.

**Acceptance:** Real jobs, Docker builds and service containers work on the supported guest image. KVM absence fails preflight. Agent restart adopts the correct guest, failed guest boot times out, and cleanup removes the guest process, disks, network devices and temporary credentials. Re-run ZF-102/103/105 scenarios for this backend.

### ZF-703 — establish a managed-workload isolation boundary

**Implement:** Treat workflow code as hostile for the prospective shared service. Keep provider credentials and control-plane administration entirely outside guest access. Restrict guest access to host management, cloud metadata, other guests and private control networks. Explicitly allow necessary DNS, GitHub and supported package/artifact endpoints; document unrestricted public egress if that is the chosen product policy.

For shared service profiles, prohibit process execution, host Docker socket mounts and privileged host containers as customer-selectable alternatives. Docker inside a VM must not expose the host daemon. Enforce account ownership through job eligibility, runner registration, guest lifecycle, network policy, secrets, logs and cache authorisation. Initially use per-job scratch and either no persistent cache or an authenticated repository/trust-scoped cache. Fork/untrusted jobs must not poison a trusted branch cache through a shared write namespace.

**Acceptance:** Negative tests attempt cross-guest file/network/log/cache access, metadata access and control API access. Resource-exhaustion tests remain bounded to their allocation. Cleanup leaves no prior-job credential or writable filesystem accessible to the next guest. Tests demonstrate the designed controls, not a claim that all VM escape classes have been disproved. Independent security review is required before unrelated customers share a worker host.

### ZF-704 — run and maintain the dedicated fleet

**Implement:** Separate fixed base hosts from elastic cloud hosts in inventory and scheduling policy. Define placement preference, spare capacity, maintenance drain, image rollout, failed-hardware replacement and worker-host evacuation. Ordinary maintenance drains rather than live-migrates running jobs. A physical server failure can fail a job; reflect that honestly and rely on explicit GitHub/user retry rather than silently replaying its side effects.

Track uptime, hardware/disk health signals available to the OS, saturation and pending maintenance. Support at least one replacement host or an explicitly documented degraded-capacity recovery arrangement. Keep the hosted controller off customer worker machines. For the initial managed pilot, allocate each physical worker host exclusively to one customer and its controller; its per-job VMs all belong to that customer. Record the resulting utilisation and pricing limits honestly.

**Gate V:** Complete seven observation days and at least 200 representative VM-backed jobs, including failure/cleanup drills, on the intended hardware class. Show job isolation tests, a host replacement procedure, restore/upgrade evidence and measured cost/capacity. No unresolved critical/high defects. The first Gate V acceptance uses customer-exclusive physical hosts. Before any later cross-customer sharing, require a separate design for trusted dispatch between customer controllers and the worker fleet, explicit account/host authority, authenticated resource reservations, independent isolation review and renewed adversarial testing. Passing the VM tests alone does not authorise shared-host tenancy.

## 12. Phase 8 — fully managed paid compute

### ZF-801 — durable, explainable usage metering

**Implement:** Extend existing usage reporting into an immutable metering ledger tied to account, job/run attempt, runner allocation, resource class and a unique source-event identity. Choose and publish the charging interval: recommended initial policy is actual GitHub job execution time, excluding queue wait and platform provisioning failures. Define rounding, cancelled jobs, user workflow failures, retries, disconnects and corrections explicitly. Infrastructure occupancy remains a separate cost measure.

Use integer durations and integer minor-currency units or an appropriate exact representation. Keep rate versions and currency on billing records. Deduplicate out-of-order/replayed completion events, make corrections append-only and reconcile against GitHub and provider records. Uncertain completion time produces a reviewable pending charge, not an invented duration. Support customer-visible itemised exports.

**Acceptance:** Replayed events never double-charge; retries are distinct attempts; unknown or conflicting intervals are quarantined. A fixed fixture dataset independently calculates the expected invoice. Usage shown to the customer reconciles with the bill and support can explain every line.

### ZF-802 — start with a bounded managed-compute offer

**Implement:** Offer a small set of measured runner sizes and concurrency limits. Prefer monthly included capacity/minutes with an explicit overage ceiling for the initial pilot. Users can see quota, queue state and estimated remaining allowance before work is blocked. Enforce account quotas and fair scheduling server-side.

If cloud overflow is offered, require Gate P, explicit customer policy and a separate cap. Describe where execution may occur and how overflow affects pricing. Keep fixed dedicated-host costs, cloud boot/idle costs, storage, egress, spare capacity and support in the internal unit-economics model. Do not derive selling prices from 100% theoretical occupancy.

Add account-level suspension of new admission, credential revocation and operational isolation for abusive workloads. Keep audit and cleanup available; cancellation of running work is a separately controlled incident action. Define supported workflow types and trust/isolation profiles accurately.

**Acceptance:** Quota exhaustion, simultaneous submissions, billing downtime and noisy-account tests cannot bypass limits or starve all other accounts. Pricing examples match the metering ledger. No customer is billed for an automatically retried platform provisioning error under the chosen policy.

### ZF-803 — managed pilot and later expansion

**Implement:** Pilot with a small invited set and documented workload/support limits. Exercise an infrastructure outage, support access, account closure, billing correction and customer-data deletion. Complete owner-led commercial/security readiness activities before public launch, using actual policies and evidence rather than generic compliance claims.

**Gate M:** At least 30 days of pilot operating evidence, reconciled usage/invoices, measured unit economics including idle/spare capacity, successful incident recovery and no unresolved critical/high defects. Gates C and V remain valid; P also applies when cloud overflow is sold. Publish supported behaviour and observed service performance before choosing any contractual SLA.

Expand only against recorded demand and measurements: additional providers, modern high-performance hardware, arm64, shared-tenant control-plane consolidation, advanced cache services, larger team features or higher availability. Cross-customer physical-host pooling requires the separate dispatch/isolation design and renewed Gate V review described above. PostgreSQL/multiple schedulers require demonstrated limits and an explicit coordination design. GPU, Windows/macOS managed execution, Kubernetes, a public provider marketplace, VM live migration and arbitrary remote terminals are separate future initiatives.

## 13. Cross-cutting implementation contracts

These are proposed conceptual contracts, not instructions to rename existing types. Implement them incrementally in their owning phase, using repository conventions.

| Contract | Required behaviour | First owner |
|---|---|---|
| Job eligibility | Installation/registration target and trust policy checked before labels; shared across scheduling/explanations | ZF-101 |
| Resource reservation | Host + allocation identity + CPU/memory/overhead + lifecycle; atomic admission and single release | ZF-103 |
| Operation | Durable identity, actor, target, desired/observed state, attempts, deadlines, ownership and redacted errors | ZF-401 |
| Bootstrap secret | Short-lived input or encrypted expiring reference; never returned through operation status | ZF-402 |
| Host ownership | Imported/manual versus Zoomies-created; provider account/resource identity and deletion authority | ZF-404 |
| Provider template | Restricted provider, region, image, size, bootstrap version and capability requirements | ZF-601 |
| Provider resource | Operation-to-resource association; reconcile unknown outcomes and all ancillary costs/resources | ZF-602 |
| Capacity policy | Scoped desired target, pending capacity, limits, cooldown, budgets and disabled-state semantics | ZF-603 |
| Customer boundary | Separate controller state/secrets first; authoritative account routing and scoped support access | ZF-501 |
| VM execution profile | Verified images, enforced limits, isolated network/storage and clean one-job lifecycle | ZF-702/703 |
| Usage ledger | Deduplicated source events, explicit attempts, exact arithmetic, versioned rates and corrections | ZF-801 |

For each API change, specify request validation, authentication/authorisation, idempotency behaviour where relevant, response/error schema, audit fields, pagination and live-update behaviour. Add a CLI equivalent when it materially helps automation or recovery; do not mechanically add a command for every UI affordance.

For long-running operations, use a create/status/cancel pattern with durable IDs, commonly HTTP 202 for accepted work. Endpoint names must follow the current router, and state transitions must be enforced server-side. Cancellation is best effort with a defined point of no return; do not display “cancelled” while untracked paid infrastructure continues running.

## 14. Test and release discipline for the coding agent

Use the repository's current commands and toolchain as the authority. At the inspected baseline these include:

```sh
make lint
make test
make build
make test-ui
make test-e2e
```

`make test` includes Go race detection. UI checking and generated-schema checks are also separate CI steps; verify `npm run check` and API generation consistency when relevant. `make test-e2e` requires external GitHub credentials and a real Docker environment and can otherwise skip. Confirm the command actually exercised its intended tests.

Run targeted tests while developing, then the required CI checks once the work package is ready. Broaden testing only for an affected boundary or release gate. Do not weaken tests, replace failed assertions with sleeps, hide failures with retries, or change runtime/toolchain versions merely to avoid investigating a defect. New long-running tests belong in an explicit integration/soak workflow rather than making every pull request wait days.

Use fake providers and disposable local fixtures for ordinary development. Real tests must target designated repositories/hosts/accounts with bounded spend and scoped cleanup. Never execute untrusted contribution code in a privileged release/test job with production credentials. Proposed workflows should be ready for the owner to enable with the correct environment; unavailable access is a named external dependency.

For every package, report what changed, why, how it was checked, which scenarios remain untested and how to recover/revert. Include the exact tested commit and relevant artifacts. A PR can be code-complete while its operational gate remains pending.

## 15. Ready-to-use coding-agent instruction

Copy this section together with the complete roadmap into the coding agent's task:

> Work in `eyupio/zoomies` using this roadmap. Assume the owner's current improvement plan is completed before this programme starts. Read the resulting repository and applicable `AGENTS.md` first. Preserve existing work, architecture, UI conventions and public behaviour; map already implemented requirements to evidence instead of implementing them again.
>
> The first assignment is Phases 0–3 only. Begin with ZF-001 and ZF-002, then implement ready foundation packages in dependency order. Make the next bounded slice concrete and continue through the authorised foundation work without requesting approval for ordinary reversible edits or routine design choices. Resolve current code against the roadmap rather than trusting historical line numbers.
>
> Maintain `docs/roadmap-progress.md` with statuses, decisions, commit/PR links, validation evidence and blockers. Keep changes in small reviewable commits/PRs according to the repository workflow. Add meaningful regression, integration and UI interaction coverage for the behaviour being changed. Update schema, generated clients and docs in the same change where relevant.
>
> Prioritise target isolation, correct restart-safe runner lifecycle, bounded resource admission, access/secret boundaries, a successful first-job journey, diagnostics and tested recovery. Reuse the existing end-to-end harness. Preserve an independent route to build and release Zoomies while it is being tested as a runner platform.
>
> Do not start SSH/cloud provisioning, a payments system, public multi-tenancy or a VM backend during this first assignment. Do not add a database service, Kubernetes or a new distributed architecture. Later phases have separate scopes and gates.
>
> Complete all local implementation, fixtures and reviewable runbooks possible with the available environment. Run real external tests only within the existing authorisation and designated test resources. Never invent elapsed soak time, real GitHub runs, benchmark results, human usability feedback or completed security review. A skipped test is not a pass.
>
> When an operational dependency is unavailable, mark that gate pending with the exact action/access needed and continue other ready foundation work. Report final code status separately from Gate F status. If the only remaining work is real observation or owner-provided access, leave a precise executable handoff. Do not announce production readiness merely because the code compiles or CI is green.

For a later assignment, replace the paragraph beginning “The first assignment” with the chosen phase and retain its dependencies and gates. Authorisation to implement a phase does not by itself authorise live purchases, public rollout or changes outside its designated resources.

## 16. Recommended first implementation sequence

1. ZF-001/002: establish the completed baseline and evidence contract.
2. ZF-101: verify installation/repository isolation and independent polling.
3. ZF-102: make create/report/restart behaviour converge safely.
4. ZF-103/105: resource admission, retry/cleanup and pressure handling.
5. ZF-104: complete access/secret-boundary verification across those paths.
6. ZF-201/202: prove first-job usability and make failures diagnosable.
7. ZF-203/204/205: backup/restore, safe upgrades and measurable operation.
8. ZF-301/302/303: real workflows, recovery drills and the observed beta gate.

Start fixture development for a package alongside its implementation; do not postpone discovering real runtime incompatibilities until the final phase. End-to-end harness work can begin after ZF-002, but final acceptance depends on the completed foundation.

The intended next milestone is a small, useful self-hosted runner platform with honest support limits, repeatable recovery and real operating evidence. Automated hosts and paid services then build on that verified behaviour.
