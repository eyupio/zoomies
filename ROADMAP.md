# Zoomies follow-on roadmap

Version 2.14 · 8 September 2026 · derived from the owner's
[follow-on roadmap v1.0](roadmap/source/2026-09-06-follow-on-roadmap-v1.0.md)
after reconciling it against `main` at `6d12a72`, then updated for the
closed N02 incident and the deferred host-stewardship slice.

This is the working plan for the next programme: make Zoomies a dependable,
secure and easy-to-operate self-hosted GitHub Actions runner platform, and
prove it in real use before expanding into host provisioning and paid hosting.
It replaces nothing: [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) is the
finished record of the code review, and this document starts where it ends.

## 1. Where it starts

The improvement plan is complete. Waves 1 to 4d are merged; the only retained
open item is the brand decision (D16). N02, the dev-instance symptom in which
runners stayed in `registering`, is fixed and no longer gates this programme.
The baseline was tested here and in CI on the same commit,
and the result is recorded in
[roadmap/validation/baseline-6d12a72.md](roadmap/validation/baseline-6d12a72.md):
16 Go packages green under race detection, the UI lint, type check and build
green, CI run 169 green. The project is three days old. A passing suite is a
starting point, not operating history, and the programme is built around that
distinction: *implemented* and *validated* are different statuses in the
[work-package record](roadmap/progress.md).

## 2. How to use this document

Each work package keeps the ID the source roadmap gave it, so the two can be
read side by side. Each has a **classification** from the reconciliation:

* **existing, needs validation**: the capability is there; the work is proving
  it under the failure the package names, and the deliverable is mostly tests
  and evidence;
* **extension**: the seam exists; the work builds on it;
* **new**: nothing to extend.

Each names what to **implement**, what **acceptance** means, its
**dependencies**, its **size** (S under a day of one session, M a few days, L
a week or more, XL a phase in itself), and the **session** that should do it
per [roadmap/agent-models.md](roadmap/agent-models.md).

The decisions in section 3 are the ones only the owner can take. Each has a
recommendation, and the packages below are written as if the recommendation
were accepted; a different answer changes the package it names and nothing
else.

## 3. Decisions for the owner

Each is written as if the recommendation were accepted; the packages below
assume it. A different answer changes the package that names the decision and
nothing else. Records for the first two are in
[roadmap/decisions/](roadmap/decisions/); the rest get a record when
ratified.

### Programme

1. **Where this record lives.** `ROADMAP.md` and `roadmap/` at the root,
   outside the published site, as [decision 0001](roadmap/decisions/0001-planning-documents-live-beside-the-code.md)
   says. The source roadmap's `docs/` paths would publish gate evidence on
   zoomies.sh. *Recommend: accept.*
2. **Which model drives which stage.** Tiered by what the stage risks, per
   [decision 0002](roadmap/decisions/0002-choose-the-model-by-what-the-stage-risks.md)
   and [roadmap/agent-models.md](roadmap/agent-models.md); one model with
   effort as the only lever is the fallback. The review's document critic
   notes that the price table in that page will go stale and that vendor
   pricing is not otherwise a repository fact; keep the page, and strip the
   prices if that bothers you. *Recommend: tiered.*
3. **The reference configuration.** Ubuntu 24.04 LTS amd64, `native` under
   systemd, Docker Engine with rootless-or-not recorded as observed rather
   than prescribed, both the embedded and the remote-agent topologies, one
   organisation and one repository target, a real-delivery run and a
   poller-only run. Everything else is qualified only to the degree a test
   proves, per [roadmap/support-and-measurement.md](roadmap/support-and-measurement.md).
   *Recommend: accept.*
4. **Two assignments, not one.** Assignment A is Phase 0, Phase 1, the
   harness's honesty and drill slices (ZF-301a and 301b) and the hygiene
   package (ZF-003). Assignment B is Phase 2 and Phase 3, issued after A's
   evidence and the owner actions Gate F needs. Sixteen packages in one
   instruction contradicts the small-pull-request rule and cannot finish
   without owner input anyway. *Recommend: two.*
5. **Phases 5 to 8 stay as direction.** Kept in this document as section 9,
   labelled not authorised, with nothing stubbed for them: no operations
   table, no ownership column, no provider skeleton, no VM backend kind.
   Their contracts still inform Phase 1 and 2 schema choices, which is why
   they stay in the same file. *Recommend: accept.*
6. **Gate F as measured.** The p95 scheduling target is relative to the
   reconcile interval (interval plus two seconds), the 200 attempts include
   three bursts beyond fleet capacity, the denominator excludes the demo
   installation, `waiting` jobs and jobs GitHub dispatched elsewhere, and a
   Zoomies-caused failure is defined by the row. The numbers as proposed
   either fail by construction or pass by noise. *Recommend: adopt.*
7. **The schema rule.** Never edit or rename a shipped migration; the next
   file takes the next unused prefix alone; prefer adding a column;
   a data-preserving rebuild in a new file is acceptable when SQLite forces
   it, with the reason in the file header as migration 0009 does. Packages
   land their own migrations rather than pooling them into one: ZF-101 is
   ready and takes the next free prefix. *Recommend: accept.*
8. **What `:latest` means.** Your own PR #71 this morning made `:latest`
   track `main` again, reversing the decision recorded in the improvement
   plan. During an observation window an untagged pool then runs unreleased
   runner images, which breaks the exact-build requirement of the readiness
   record. Options: keep `:latest` on `main` and have the installer write the
   concrete tag it installed; or restore `:latest` to the newest release
   before the window. *Recommend: pin `vX.Y.Z` on the reference configuration
   either way, and restore `:latest` to the newest release before the
   observation window. This is your call, and it is recorded either way.*
9. **A tagged pre-release at the end of Assignment A** (for example
   `v0.2.0-beta.1`), as the artefact ZF-204's upgrade drill runs from;
   `v0.1-alpha` marked as a prerelease; immutable releases enabled in the
   repository settings, because that release's assets were rebuilt and
   re-uploaded two days after tagging. *Recommend: yes.*
10. **The Enterprise Server claim.** The README and FAQ say Zoomies works
    with GitHub Enterprise Server; the configuration supports it and no test,
    fake or run has ever targeted one. Options: obtain an instance and keep
    "yes"; or say "designed for, not yet verified against one". *Recommend:
    the second, now; the first when a beta user needs it.*
11. **Real-runtime evidence in CI.** A `workflow_dispatch` plus nightly
    `e2e.yml` reading credentials from a protected environment, never on a
    pull request from a fork, disabled until the secrets exist; and a
    fake-GitHub-plus-real-Docker drill tier on every pull request. You
    designate the disposable organisation, App, repository and the tunnel
    for the webhook run. *Recommend: both.*
12. **Owner actions now.** Deploy `main` on a fresh Ubuntu 24.04 LTS host
    with `install.sh --deployment native`, record the reference versions and
    begin the controlled reference deployment; recruit the second operator,
    who can run against the drill tier's injected failure before credentials
    exist. *Recommend: now.*

### Per package

13. **ZF-101, the authoritative installation for a job.** The installation
    found by the job's repository (repository target before organisation
    target), not the one whose secret verified the delivery, which the code
    deliberately lets be another installation's; jobs no installation covers
    are recorded and marked ineligible with a reason, never rejected; the
    migration backfills every unfinished row, waiting as well as queued and
    in-progress; the rate-limit hold is per installation only; `installation_id` is exposed read-only on the job.
    *Recommend: as stated.*
14. **ZF-102, how far to go.** No durable task queue (stamp the issue time
    on the row); adopt live workloads on agent start; a state-directory lock
    plus a controller lease with `--takeover`; a duplicated-agent fence that
    detects and warns rather than refuses; no resurrection of a terminal row
    when a lost host returns; the existing rate-bounded retry accepted as
    "bounded". *Recommend: as stated.*
15. **ZF-103, the reservation model.** Split in two: 103a is the reporting
    half — agents report their host's CPUs, memory and disk, and the host row
    carries them beside the operator's reserve — and 103b is the admission
    half, where the scheduler fits a reservation and the blocked reason says
    why. Both precede Gate F; the operator-visibility work that finishes the
    package need not. A pool that sets no limits reserves its host's
    allocatable share per slot, so a host carrying only such pools admits
    exactly what it admitted before the upgrade; a DinD pool is charged
    twice its limits; the host reserve is per host with a small documented
    floor; an agent that predates the fields is placed by slots with a
    visible badge; the `process` backend's non-enforcement is documented and
    warned about, not fixed here; disk is a gate, not an evictor.
    *Recommend: as stated.*
16. **ZF-104.** Streams re-check their credential on each heartbeat and end;
    a join token may not replace a host that is still heartbeating unless it
    is cordoned; the log viewer opens only `http(s)` links; forwarded-proto
    is believed only from a trusted proxy; per-identity stream caps wait for
    ZF-105 and evidence; the join route gets the login limiter.
    *Recommend: as stated.*
17. **ZF-105.** Failed cleanup lives on the runner row, not a new
    operations table; no maximum job duration (a drain timeout instead);
    the cache prune is guarded at runtime; Zoomies never deletes images and
    says so; disk-low handling lands with ZF-103; the ownership of
    `zoomies-*` registrations across two instances sharing one organisation
    is documented now and designed in ZF-101. *Recommend: as stated.*
18. **ZF-201.** Verify reports the installation's repository selection and
    first names rather than taking a repository as input; a test-only fake
    GitHub program under `test/` rather than a hidden subcommand in the
    shipped binary; the real-job proof is the end-to-end test asserting the
    page shapes, not a browser-driven real job; Add-a-host shows a resume
    notice, never the token. *Recommend: as stated.*
19. **ZF-202.** One JSON bundle from one admin route under a new
    `diagnostics.read` action; never workflow log bodies; the per-job
    explanation is its own endpoint rather than a field on every event
    frame; the stale-poller signals are fleet-wide; seeded failures come
    from an opt-in fixture, not the demo seed. *Recommend: as stated.*
20. **ZF-203.** Backup is a local command that opens the file, not an API
    route; the key is excluded unless asked for, with its fingerprint always
    in the manifest; restore invalidates sessions and unused join tokens by
    default, with flags for API and agent tokens; the store refuses a
    database newer than the binary; the fence lifts through one audited
    admin route. *Recommend: as stated.*
21. **ZF-204.** Protocol must match and an agent may lag one minor release;
    an agent found incompatible at heartbeat is flagged and excluded from
    placement like a cordon, never sent into a restart loop; the store takes
    a `VACUUM INTO` copy before pending migrations, using ZF-203's primitive;
    build-provenance attestations rather than signing keys; a GitHub-hosted
    path for the release and site workflows selectable by dispatch input;
    Dependabot weekly and grouped, actions pinned to commits.
    *Recommend: as stated.*
22. **ZF-205.** Audit rows stay unpruned, with a size-awareness problem;
    the load fixture is a test-only generator, never a knob on the demo
    seed; the p95 figures are recorded evidence, not a pull-request gate;
    the poller-pause gauge is fleet-wide; no stream caps until the drills
    show growth. *Recommend: as stated.*

### Noted, not yet due

23. **Licence and contributor terms.** AGPL-3.0 with no contributor
    agreement suits your own hosted service and shapes what contributors and
    self-hosting customers who modify the code can expect. Nothing in Phases
    0 to 3 depends on it; it is a Phase 5 precondition. *Recommend: decide
    before Phase 5.*
24. **Whether Phase 4's bootstrap may make the controller dial a machine**,
    reversing "the controller never dials an agent", stated in five places
    and underpinning the NAT-friendly design; and whether an operations
    table is introduced for it. Both are recorded before ZF-402 starts, not
    now. *Recommend: decide then, with the destination policy as the
    compensating control.*

25. **Host stewardship and bounded housekeeping.** Add a deferred Phase 4
    slice, **ZF-404b**, after the original maintenance and ownership controls.
    An imported or shared host remains non-invasive by default: no automatic
    OS package upgrades, firewall changes, reboots or broad Docker pruning.
    A deliberately opted-in, dedicated runner host may receive health and
    update reporting, planned maintenance windows, agent upgrades and
    retention/quota-driven cleanup of Zoomies-owned workspaces, runners,
    images and caches only. Every cleanup previews its scope, leaves
    non-Zoomies artefacts alone, is observable and is recoverable where
    practical. This profile does not authorise unrelated-customer workloads;
    that remains a Phase 7 isolation question. *Recommend: adopt before
    Phase 4 design begins.*
26. **Windows runners, and which kind.** Add **ZF-206**, and decide the shape
    before any of it is written, because the two shapes differ by about five
    times the work and by the product's central promise. *Process on a Windows
    host* runs `actions-runner-win-x64` directly on the machine: it is the
    smaller path, it reuses the backend that already exists, and it cannot
    give a job a container, so the ephemeral guarantee weakens from "the
    container is destroyed" to "a fresh work directory and a single-use
    registration on a machine whose state persists" — the same honest
    weakening the `process` backend already carries on Linux, and it must be
    said in the same words. *Windows containers* keeps the guarantee whole and
    needs a second runner image catalogue on Windows base images, a second
    build matrix, Docker Engine over a named pipe, and images measured in
    gigabytes. Neither may start before Gate F: the support matrix's rule is
    that a row moves right only when a test runs on the thing, and adding a
    platform while the project is trying to prove the one it has would widen
    Gate F rather than pass it. *Recommend: process first, containers only if
    a user asks for the isolation; and neither before Gate F.*

## 4. Delivery rules

The source roadmap's rules, corrected where the repository already answers
them. `CLAUDE.md` is the agent guidance file (there is no `AGENTS.md`) and
it is tested: its layout block and `README.md`'s must name every top-level
directory. Read it, `docs/architecture.md` and `docs/upgrading.md` before
anything structural.

1. Preserve the single binary, SQLite, the pure scheduler and the
   controller-and-agent shape. No Kubernetes, no broker, no database
   service, no microservices.
2. Host provisioning and runner execution are different things. The
   backend interface is the per-host execution contract and was written to
   admit a VM-per-job backend; a host provisioner would be a separate
   controller-side contract. The architecture page's sentence that conflates
   them is corrected in ZF-003.
3. One instance is one administrative trust domain. Two installations of
   that team still need correct target scoping, which is ZF-101.
4. The UI, the CLI and any bundle are clients of the same REST API; nothing
   is reachable from one that is not reachable from the others. Build UI
   workflows on validated application operations; never duplicate
   orchestration in Svelte or in a command.
5. Network side effects are restart-safe, bounded and observable. Durable
   identity, ownership checks and reconciliation; never a claim of
   exactly-once across GitHub, agents and providers.
6. Reuse the components, the tokens, the generated clients, the error
   shapes, the auth and the audit conventions. Every endpoint change updates
   `api/openapi.yaml`, regenerates both clients and touches
   `docs/api-surface.md`; every new problem code, metric and command needs
   its row on the page that `internal/docs` tests.
7. The schema rule is decision 7: never edit or rename a shipped
   migration; the next file takes the next unused prefix alone; add columns
   by preference; rebuild a table in a new file only when SQLite forces it,
   saying why in the header. Two tests are part of that rule rather than a
   consequence of it. Every new file is appended to `shippedMigrations`
   (`internal/store/migrations_test.go`), which says so when it fails. One
   that touches `jobs` must also be added to
   `TestTheJobsRebuildKeepsEveryRowAndItsIndexes`
   (`internal/store/store_test.go`): that test recreates `jobs` at the column
   list 0008 left behind, unwinds the ledger row and the effects of every
   later migration that touched it, then reopens the database to migrate the
   fixture forward. The names it unwinds move with each new migration, and
   every one since the rebuild that touched that table has had to amend it.
   Forgotten, it fails on a `SELECT` under "the completed row did not survive
   the rebuild", which blames the rebuild rather than the omission.
8. Small commits, small pull requests, one behaviour each, an imperative
   sentence in plain prose as the message, British spelling in prose, a test
   that reads as a sentence about the behaviour, and `make lint` and
   `make test` green before a push. Behavioural tests for concrete new
   failure modes, never tests that repeat implementation details.
9. New capabilities start disabled until their acceptance criteria are met.
   A disabled feature starts nothing, creates nothing and needs no
   credential on an existing installation.
10. Every dependency carries a one-line reason in `docs/dependencies.md`.
    A library for SSH or a VM runtime is reasonable and gets its row and a
    security note; a plugin framework for one integration is not.
11. Logs and bundles are sensitive. Platform-managed credentials are
    redacted by key name (`auth.Redact`); arbitrary secrets in workflow
    output cannot be recognised and `docs/security.md` says so; the bundle
    therefore never carries log bodies.
12. Never auto-upgrade a host, remove a customer-owned machine or weaken a
    trust check as a recovery shortcut.
13. A skipped test is not a pass. A harness that cannot run reports "not
    run" or "blocked", and a gate that counts it as passed is lying.
14. A behavioural test is kept only once it has been run against the code
    with the rule it asserts removed and seen to fail; an assertion that
    cannot be made to fail is deleted rather than shipped, and the pull
    request says which assertions were checked this way. It is what catches
    what a green suite hides — a check written as `err.Error() == ""`, which
    no error can satisfy; the two lines that put a host's resource figures on
    the wire, deletable with everything else still green; a lifecycle drill
    that passed with the registration deletion it existed to prove commented
    out.

### The work-package record

[roadmap/progress.md](roadmap/progress.md) has one row per package with its
classification, status, dependencies, the session that did it and the
evidence. Statuses are `not_started`, `in_progress`, `implemented`, `done`,
`validated`, `blocked` and `superseded`, each defined in the record itself.
The ladder that decides what is left to do runs `implemented` — merged, CI
green, tests present — then `done`, every pull request the package names
merged and its acceptance holding, then `validated`, which needs evidence in
[roadmap/validation/](roadmap/validation/) that a real run met the criteria
and which most of Phase 1 cannot reach until the reference deployment exists.
`superseded` links to the thing that meets the same criteria and its evidence. Keep it current in the pull
request that changes it. Small design decisions go in
[roadmap/decisions/](roadmap/decisions/), gate evidence in
[roadmap/validation/](roadmap/validation/). `IMPLEMENTATION_PLAN.md` is not
touched; its two open items are carried in the progress record.

## 5. Phase 0: the baseline

Two packages from the source roadmap, merged, plus one pulled forward. Phase
0's output is this document, the records it links to, and three small
changes that are already in the same pull request: the stuck-runner
tie-break that made a controller test fail without race detection, the
compatibility paragraph that contradicted the code, and the metrics page's
description of the scheduling-latency series as the proxy it is.

### ZF-001 and ZF-002: the baseline and the measurement contract

**Classification: mixed; merged into one package.** Almost every fact ZF-001
asks for was discoverable, and is now recorded in
[roadmap/validation/baseline-6d12a72.md](roadmap/validation/baseline-6d12a72.md).
Most of the lifecycle timestamps ZF-002 asks for exist and are tested, and
fleet-caused failure is already distinguished from a failing workflow
(`Job.RunnerFault`, the `runner_lost` event, `store.FailedConclusions`,
and `stale` for a job GitHub stopped reporting). The support matrix, the
measurement contract, the severity scale and the Gate F targets as adopted
are in [roadmap/support-and-measurement.md](roadmap/support-and-measurement.md).

What the reconciliation found that the source roadmap did not expect:

* `docs/upgrading.md` says an older agent ignores an unknown task kind. The
  agent reports it as a failed task, and the controller fails the runner for a
  lifecycle kind. The compatibility claim is corrected in this pull request;
  the policy is ZF-204's.
* There is no numeric schema version. The store keeps a filename-keyed ledger,
  and nothing surfaces it: a support bundle or a bug report has to open the
  database with `sqlite3` to say which migrations it carries.
* The Overview's success arithmetic counts an unknown outcome as a success.

**Do, in this pull request and one small code pull request:**

1. This pull request: the baseline record, the support matrix and
   measurement contract, the progress record, the decision records, and the
   `docs/upgrading.md` correction.
2. Code, size S: `store.AppliedMigrations` surfaced on `GET /readyz` as
   `schema` — how many migrations have applied and the name of the latest,
   which is the only schema version there is — with the `/readyz` row in
   `docs/api-surface.md` saying so; the `waiting` and `approved` timeline
   kinds; `Stats` split into succeeded,
   failed, cancelled and unknown, rendered on the Overview with the
   definitions in the contract. No migration.

**Cut from the source package:** "controller/agent versions" (one binary;
record the commit, the protocol version and the pinned runner release);
qualifying arm64, macOS, Podman or `process` (recorded as built or unit-tested,
not qualified); any new metrics framework (five stage histograms and the
runner timeline exist); the missing timestamp columns, which land with the
packages that define their semantics (ZF-101, ZF-102, ZF-105).

**Accept when:** every Phase 1 to 3 package has a classification and a size in
the progress record (done below); the next ready package is named (ZF-101);
the code slice is merged with tests.

**Needs from the owner:** a fresh Ubuntu 24.04 LTS amd64 host with `main`
deployed by `install.sh --deployment native`, so the exact OS and runtime
versions can be recorded. Size M. Session: Claude Fable 5.1 at `high` (this
session); Claude Sonnet 5 at `high` for the code slice. Decisions: 1, 2, 3,
6, 7, 12.

### ZF-003: supply-chain hygiene and three corrections

**Classification: new; small; no behaviour change.** Pulled forward from
ZF-204 because none of it depends on anything and all of it is cheaper the
earlier it lands. Every third-party action in the four workflows is pinned
to a major tag, which a moved tag can redirect with `contents: write` and
`packages: write` in hand; there is no dependency-update configuration and
no vulnerability scanning of Go modules, npm packages or images.

**Do, in one or two pull requests:**

1. Every `uses:` pinned to a full commit with its version in a comment; a
   Dependabot configuration for actions, Go modules, npm under `web/` and
   the images under `deploy/`, weekly and grouped per ecosystem;
   `govulncheck ./...` as a CI step and on a weekly schedule.
2. Three corrections that are misleading today regardless of the roadmap:
   the architecture page's "not a cloud provisioner" sentence, which
   justifies itself with the execution-backend interface; the
   capacity-demand receiver page, which lets an event-id-idempotent receiver
   add the same capacity once per cooldown because the sender mints a new
   event id on every re-delivery of one unmet shortfall, fixed by stating
   the target reading and adding an additive `schema_version: 1` field to
   the payload with the existing tests extended; and the security page's
   claim that repository and workflow names appear in metric labels, which
   the code stopped doing.

**Accept when:** CI and the release workflow run on pinned actions; the
first Dependabot pull requests arrive; the three pages say what the code
does.

Depends on nothing. Size S. Session: Claude Sonnet 5 at `high`. Decisions: 9, 21.

## 6. Phase 1: correctness and security under failure

The foundation the beta stands on. The reconciliation moved two of these
packages a long way from where the source roadmap placed them: ZF-101 is new
work with a schema change rather than verification, and ZF-102 contains the
one defect that decides whether a rolling upgrade is real. ZF-103 is split so
that its reporting half lands first and its admission half — the scheduler
fit and the blocked reason — still precedes Gate F. ZF-104 is mostly citing tests
that exist, with two real gaps. ZF-105 opens with a one-line correctness fix
that ships alone.

### ZF-101: one eligibility policy for GitHub targets

**Classification: new.** The source roadmap says "verify that job-to-pool
eligibility requires the correct installation … before label matching". There
is nothing to verify. A job row has no installation identity: the webhook
parser reads `installation.id` and drops it, the poller discards the
installation it polled, and every consumer, webhook ingest, poller ingest, the
scheduler's assignment and the capacity-demand signal, calls
`scheduler.BestPool(pools, labels)`, which filters on enabled and labels only.
Two installations with the same pool labels cross-allocate deterministically
(the tie-break is the pool name), and a runner is then minted in the wrong
GitHub target. Poll freshness is the latest accepted delivery across *all*
installations, and the rate-limit hold is one atomic that pauses polling for
everyone. No test anywhere sets up two installations.

What is already right: pools carry `InstallationID`; registration
(`mintCredentials`) is scoped to the pool's installation and runner group;
webhook verification selects the installation by repository target; a
repository-scoped cache under an organisation installation raises a tested
warning; the per-repository throttle is documented as not being isolation.

**Do, in three pull requests:**

1. Migration `0012` adds `jobs.installation_id` (with a backfill over every
   unfinished row — waiting, queued and in-progress — by matching the
   repository to an installation, repository target before organisation
   target, and leaving a repository no installation covers empty) and
   `webhook_deliveries.installation_id`.
   Add `scheduler.Eligible(pool, job) (bool, reason)`: enabled, then
   installation match, then labels. Replace all four `BestPool` callers. Extend
   the `Plan` with per-job ineligibility reasons so the problems drawer can say
   "labels match pool X, which belongs to installation Y". Expose
   `installation_id` read-only on the job view and regenerate both clients.
   Tests: two installations, identical labels, on the webhook, poller and
   scheduler paths, asserting the job's pool and that no create task targets
   the other installation.
2. Per-installation freshness and backoff: `LastAcceptedDeliveryAt` takes an
   installation; the pause becomes a map keyed by installation; the poll loop
   continues past a rate-limited installation instead of returning.
   Tests: two fakes, one rate-limited, the other still polled; one fresh, the
   other still polled.
3. Visibility: a `pool.runner_group_unresolved` warning where the runner
   group falls back to default with only a log line today (the fallback path
   has no test at all); a section in `docs/hosts-and-pools.md` that says pools
   belong to one installation and that GitHub makes the final dispatch
   decision within an organisation. The repository-cache containment check
   the source roadmap asks for already exists and is tested; it is not
   repeated here.

Two things the verifier found that shape the first pull request: the job
merge makes `Matched` sticky, so re-evaluating eligibility needs an explicit
merge rule and the backfill must reset the match for rows it re-attributes;
and one fake GitHub can back two installations with different targets for
the webhook-path tests, so a second fake is needed only for the poller and
rate-limit cases.

**Cut from the source package:** a "trust policy" abstraction (the pure
function is the policy); a jobs-by-installation UI filter until someone asks;
the GitHub Enterprise Server plus github.com collision on `github_job_id`
(document one GitHub host per controller); rejecting repository caches on
organisation runners outright, which would break existing pools.

**Accept when:** the two-installation tests pass on every path; the
acceptance's "two repositories in one organisation" is limited to accounting
and cache-key correctness, because within one organisation installation
GitHub may hand repository B's job to a runner created for repository A and
Zoomies cannot stop it; ineligible work carries a reason on the Jobs page.

Depends on ZF-002. Size M. Session: Claude Fable 5.1 at `xhigh` for the first
pull request, Claude Opus 5 at `high` for the other two. Decisions: 7, 13.

### ZF-102: reconciliation that converges

**Classification: mixed; far smaller than the source text implies.** The
mechanics exist and are tested: the store-enforced state machine with legal
self-transitions; an idempotent per-host task queue with leases and three
attempts; agent-side claim dedupe; adopt-before-create on a redelivered create
(the workload is found by its runner-id label); host-ownership checks on task
results and runner reports; a five-minute host-lost reclaim; a provision
timeout that covers both `provisioning` and `registering`; an exponential
start-failure backoff with a visible problem code. "Registration timeout"
exists as `scheduler.provision_timeout`; do not add a second one.

What is missing, in order of consequence:

* **A restart of the agent, or of the controller on a single-VM install,
  kills that host's jobs.** Adoption happens only when a task arrives; the
  reconciler reaps every untracked managed workload two minutes after the
  first successful poll, running or not. The agent's unit is
  `Restart=always`, so an agent restart is an everyday event. Worse, the
  default deployment runs the agent inside the controller, and a controller
  restart builds a fresh embedded agent with an empty tracked set whose
  first poll succeeds at once, so every running job on that host is removed
  two minutes after the controller comes back. The upgrading page says a
  controller restart does not touch a running job; the reconciliation found
  the opposite by reading the code paths, no test exercises the case, and
  the drill tier's first drill is to demonstrate it. The page is hedged in
  this pull request. This is the one
  genuine defect in the package and the one that decides whether rolling
  upgrades are real. The former N02 `registering` symptom is fixed; the
  create-to-register boundary remains covered by this package's restart and
  drill scenarios, not by a separate release gate.
* No lock or lease refuses a second controller on the same database or state
  directory. SQLite's busy timeout serialises writes; it does not stop a
  second scheduler minting credentials and reclaiming hosts.
* The agent log upload checks an unguessable stream id, not the host.
* A controller restart loses the in-memory create task and its JIT
  configuration; the provisioning row waits out the five-minute timeout and
  is replaced with a freshly minted credential. Bounded, and by an explicit
  design decision (the queue is in memory so there is no second source of
  truth), but each restart costs every in-flight create five minutes.
* No fence for a duplicated agent credential (a cloned VM or a copied state
  directory): two agents share one host id and split its tasks.
* No test constructs a fresh controller over the same store with a task in
  flight, restarts an agent over live workloads, or delivers a late report
  after a host was declared lost; the cross-host refusal on task results has
  no test either. Two more shapes for the restart table: a create result
  lost during a controller outage is never retried by the agent and no task
  is redelivered, so the row waits out the provision timeout and its live
  container is later removed with its job; and the five-minute host-lost
  reclaim is shorter than the twenty-minute create lease, so a create in
  flight on a host that goes quiet is failed before its lease expires and,
  when the host returns, its registering report is refused silently while
  the workload registers with GitHub on a failed row.

**Do, in four pull requests:**

1. The invariants, written down (S): a "Reconciliation invariants" section
   beside the state diagram in `docs/architecture.md` listing each rule with
   its constant and owning package (at-least-once delivery; the lease
   lengths; three attempts; the two-minute orphan grace; host-lost at five
   minutes against a ninety-second heartbeat timeout; the provision timeout;
   the ten-minute failed retention; self-transitions legal; capacity derived
   from rows; an agent asserts no state for a live runner), and a test that
   pins the two relationships between them that nothing pins today.
2. Two one-line invariants (S): the log relay takes the authenticated host id
   (shared with ZF-104); a non-blocking lock on the state directory before the
   store opens, plus a controller lease row with a `--takeover` flag so a
   restored copy on a shared filesystem or a second host is also refused,
   with a problem-code row.
3. Adoption on agent start (M): before the loops start, list every backend
   and adopt each workload carrying a runner id; include them in the first
   heartbeat; an additive heartbeat response field names the runners the
   controller does not know, and only those are reaped. This is the one change
   that touches deletion semantics, so it gets its own review.
4. Late reports and restart boundaries (M): a report from the owning host for
   a runner failed as lost, with a running phase, updates the message, links
   the job's events and removes the workload once no in-progress job is
   linked, without resurrecting a terminal row; `task_issued_at` stamped on
   the runner row at enqueue and on redelivery (the ZF-002 contract's
   timestamp, without a durable queue); a session id in heartbeats, results
   and reports with a `hosts.duplicate_agent` problem when two sessions
   alternate, detect only; one table-driven test that, for each boundary
   (create enqueued, acknowledged, registering, busy, draining with a stop in
   flight, remove in flight), builds a fresh controller over the same store,
   replays the agent's late result, and asserts one live row per runner name.

**Cut from the source package:** a durable task queue (stamp the issue time
on the row and document that a create lost to a restart is failed by the
provision timeout and replaced; revisit if ZF-302's drills show it matters);
an enforcing session fence (detect and warn; re-join already rotates the
token); a count-bounded retry with a paused pool (the rate-bounded backoff
that says so is self-healing when the image is fixed); real-process kill
tests (ZF-302).

**Accept when:** the restart-boundary table passes; an agent restart keeps
its live runners; a second controller is refused with a message naming the
holder; a late report after host loss is recorded accurately without
resurrecting the row; the invariants page matches the constants.

Depends on ZF-002. Size M. Session: Claude Fable 5.1 at `xhigh` for the
invariants and the adoption change, Claude Opus 5 at `high` for the rest.
Decisions: 12, 14.

### ZF-103: resource reservation on top of the slot model

**Classification: extension.** The slot model is complete and tested, and it
already has the shape the source roadmap asks a reservation contract to have:
a create writes a `provisioning` runner row before a credential is minted, a
non-terminal row holds a slot, the in-tick host set decrements per placement
so two pools cannot share the last slot, release happens exactly once when the
store's state machine reaches `removed` or `failed`, and a restart rebuilds
everything from rows. Unhealthy and cordoned hosts are already refused, and
the blocked reason already counts slots, backend, selector, health and
policy. Pool `resources` are enforced as cgroup limits on Docker and Podman,
including the DinD sidecar, which carries the same limits and so doubles the
enforced footprint.

What is missing, now that the reporting half has landed: the scheduler never
reads `pool.Resources` — `internal/scheduler` has no CPU, memory or disk logic
at all, and the host set still seeds its free count from `Host.Free()`, which
is capacity minus active runners; the host row has no allocatable column; the
`process` backend enforces none of a pool's limits and nothing says so; and the
per-host reserve the row now carries can be set only from the store, because
`SetHostReserve` has no caller outside tests and neither `PATCH /hosts` nor the
host view mentions it.

**Do, in three pull requests. The first two have landed -- the figures reach
the host view and the Hosts page, and the scheduler now places by them; the
third is partly done. The [work-package record](roadmap/progress.md) names the pull requests
that carried each:**

1. Agents report host resources: CPUs, memory and free disk on the work
   directory, from the Docker or Podman `/info` where there is one and from the
   OS otherwise, as optional fields on join and heartbeat (protocol version
   stays 1). Its migration adds the observed columns and a per-host reserve
   to `hosts`; heartbeats write the observed values and never the reserve,
   mirroring how capacity is the operator's today.
2. **Done.** The scheduler fits a reservation: a pure `Reservation(pool, host)` that is
   the pool's resources, doubled for DinD, or the fallback profile for a pool
   that sets none; the host set tracks free CPU and memory seeded from
   allocatable minus live runners' reservations; `eligible()` also requires
   the fit and free disk; the blocked reason gains "short of CPU", "short of
   memory", "low on disk". `HostFit`, the capacity-demand signal and the
   prewarm path ask the same predicate, so there is still one placement rule.
   Tests: two pools cannot oversubscribe memory in one tick; a 32 GB host
   admits eight 4 GB runners and refuses the ninth; DinD counts the sidecar;
   unknown resources fall back to slots; reservations rebuild from rows.
3. Operators can see it: the host view carries the reserve, the allocatable
   and the reserved beside the observed figures it already carries;
   `PATCH /hosts` accepts the reserve beside capacity; the Hosts page already
   states a host's vCPUs, its memory and its free-of-total disk and marks a
   disk nearly gone, so what is left there is the reserved against the
   allocatable, as CPU and memory bars because a bar answers "how full is this
   host" faster than two numbers do, and a "resources unknown, upgrade the
   agent" badge with an info-severity problem code; the pool page says what an
   unlimited pool is assumed to reserve; a `pool.resources_unenforced` warning
   for a `process` pool that sets limits; gauges, docs and problem-code rows.

Three things the verifier added for the scheduler pull request. The first is
closed with that pull request: the reservation is rebuilt only from rows in a
live state, which is the same filter the slot count uses. The second is still
open: the agent already samples each container's enforced memory limit and the
controller drops it, an observed-versus-reserved signal that is already on the
wire. The third — that the heartbeat writes the
host row only when something changed, and free disk moves every beat — was
closed in the first pull request: `diskFreeMoved` is the tolerance rule, a
fractional band with a floor, a first reading always taken, and a silent agent
never overwriting what was known.

**Chosen while building the second, and not in the plan:** a field a pool
leaves unset is charged one slot's worth of the host rather than nothing, which
is what makes decision 15's "admits exactly what it admitted before" true when
an unlimited pool shares a host with a limited one; free disk is charged only
for the runners a pass adds, because it is a measurement that already contains
what the runners already there have written; and decision 15's "small
documented floor" under the reserve is 512 MB of memory and 2 GB of disk, with
no CPU floor, because CPU is the one resource that is contended rather than
exhausted.

**Cut from the source package:** CPU topology (reserve in logical CPUs and say
so); a separate reservations table (the runner row is the reservation, and a
second table would be a second source of truth against the store's one-writer
invariant); overcommit knobs (a hard fit check has no overcommit); disk
eviction (ZF-105 owns retention).

**Accept when:** the tests above pass; every existing installation admits
exactly what it admitted before the upgrade on hosts that carry only unlimited
pools; an agent that predates the fields is placed by slots with the badge
showing.

Depends on ZF-102 (fencing first, so a superseded agent cannot report
resources for a host it no longer owns). Size L. Session: Claude Fable 5.1 at
`xhigh` for the reservation decision and the scheduler pull request, Claude
Opus 5 at `high` for the rest. Decisions: 15.

### ZF-104: the access and secret boundary, verified

**Classification: mixed; about seventy percent is citing tests that exist.**
The authorisation layer is the best-tested part of the codebase: the policy
table is walked per role and per route, including the event stream, runner
logs, downloads and metrics; CSRF and origin checks, proxy trust, body
limits, login rate limiting, secret-free responses, audit and log redaction
and the process backend's environment allowlist are implemented and mostly
tested. Trust profiles are already explicit in `docs/security.md` and in the
pool wizard's risk badges. The `/jobs` and `/audit` routers, which the source
roadmap's focus question singled out, apply their guard with `r.Use`.

Five things are not proved, and two of them are bugs:

* The agent log relay accepts a chunk for any stream id regardless of which
  host authenticated. A second enrolled host that learns a stream id can
  inject bytes into another host's runner log.
* A live event or log stream never re-checks its credential. Logging out,
  revoking a token, disabling a user or the session expiring leaves an open
  stream open indefinitely.
* No negative route walk exists for scoped API tokens or agent tokens, and the
  cross-host check on task results has no test.
* Secret absence is proved for seven admin reads on the success path only, not
  for error bodies, audit rows written on failure, the controller log, fleet
  resources, an event frame or metrics.
* The `process` backend's child environment has no test at all (the Docker
  runner environment and the socket bind are tested), nothing greps a
  marshalled task for controller secrets, and no UI test feeds hostile
  strings or terminal escapes to the pages and the log viewer.

**Do, in five narrow pull requests:**

1. Tests only, first (S): scoped-token and agent-token route walks covering
   all five agent routes (the current walk omits tasks and logs); a
   cross-host result test; secret absence on failure paths with a captured
   log; the process backend's child environment against a poisoned parent
   environment; a marshalled create task grepped for the fake installation's
   secrets; a 413 for a body over the limit; disabling a user ends its
   sessions at the API, not only in the service; a cross-origin login post.
2. Bind the log relay to the host (S): the relay takes the authenticated host
   id and answers not-found for another host's stream, the same as a closed
   one, so nothing is enumerable.
3. Streams end on revocation (M): on every heartbeat tick the stream
   re-resolves its identity and ends with an `end` frame when the credential
   is gone or the action no longer allowed, and at the session or token expiry.
   The browser client retries a failed reconnect forever and never runs its
   401 hook on the event source, so the same pull request gives it a session
   probe or a bounded retry; otherwise a signed-out tab loops.
4. Hostile input (M): a Playwright spec that seeds names carrying markup and
   relays log chunks carrying hyperlink, title and clear-screen escapes,
   asserting nothing was injected, the title is unchanged and no dialog opened;
   an explicit link handler in the log viewer that activates only `http(s)`
   URLs with `noopener`.
5. Uniform proxy trust (S): `X-Forwarded-Proto` believed only from a trusted
   proxy, as `X-Forwarded-For` already is; and a "what is tested" paragraph in
   `docs/security.md` naming the tests the readiness record will cite.

One small decision the verifier surfaced: the join route has no rate limit,
so a join-token guess is bounded only by the token's entropy. Reuse the
login limiter on it, which is one call.

**Cut from the source package:** WebSocket routes (there are none); a
diagnostics export (there is none; ZF-202 introduces one); task-queue bounds
(dedupe and a per-poll cap exist; the per-host pending cap is ZF-105's);
building a matrix (it exists as the policy table, the `x-zoomies-role` field
in the OpenAPI document and the role column on the API page, cross-checked by
four tests).

**Accept when:** the six identity classes have positive and negative tests;
cross-host reports, results and log chunks are refused; a revoked credential
ends its streams within one heartbeat; synthetic secrets are absent from
error bodies, audit rows and the log; the hostile-input spec passes.

Depends on ZF-101 and ZF-102 for the paths it walks. Size M. Session: Claude
Fable 5.1 at `high` for the matrix review and the stream-revocation design,
Claude Opus 5 at `high` for the rest, with Claude Opus 5 as the stated
fallback if the hostile-input session meets a safeguard refusal. Decisions: 16.

### ZF-105: cleanup that is visible and bounded

**Classification: mixed.** The local-cleanup half exists and is tested: the
agent deletes finished workloads after `agent.finished_retention` once the
controller has heard the exit; the orphan sweep sees only containers carrying
the managed and role labels, and only after a successful poll and a two-minute
grace; registration deletion is retried by a ten-minute reap under a
name-prefix ownership rule; provision, idle, lifetime and stop timeouts are
separate settings; database history is pruned hourly; task deliveries are
leased, requeued and dropped; the agent's poll loop backs off with jitter.
There is no maximum job duration by explicit design, delegated to the
workflow's own `timeout-minutes`, and this roadmap keeps it that way.

What is missing is the visibility half and the outage half, plus one
correctness defect the reconciliation found: when a backend's listing call
fails, the reconciler skips that backend and then declares every tracked
runner on it gone after a minute, so a transient Docker API timeout marks
busy jobs lost and, once the daemon answers again, reaps the live containers
as orphans two minutes later. Also: a failed remove result on an
already-removed row is silently dropped; failed registration deletes are
log-only with nothing on the row and no problem code; the rate-limit reset
GitHub sends is parsed into a string and discarded while the poller pauses
for a fixed fifteen minutes globally; nothing measures free disk or
recognises a full one; a DinD sidecar whose runner container is gone is
invisible to both listing and removal; the pool-scoped cache prune assumes
the cache is idle, which is false for a pool with more than one runner.

**Do, in six pull requests, each with its own test:**

1. The listing-failure defect (S, ships alone, no dependency): a backend
   whose listing failed is excluded from the missing-workload pass.
2. Failed cleanup persisted and visible (M, the heart of the package):
   migration columns on the runner row for the cleanup error, its time,
   attempts and the registration's deletion time; the result handler records
   a failed stop or remove on a terminal row instead of dropping it;
   registration deletion records its failure and the reap clears it; one
   `runners.cleanup_failed` problem; the fields on the runner view and in the
   OpenAPI document. `Runner.CleanedUpAt`, the measurement contract's end of
   the cleanup interval, lands here.
3. Orphaned sidecars (S): listing also queries the sidecar role and returns
   a sidecar whose runner is gone as an orphan.
4. Cache prune guard (S): skip eviction while another workload of the same
   pool is running.
5. A drain timeout (S): `scheduler.drain_timeout`, with a scheduler rule
   that fails a runner draining longer than it, so a draining row whose stop
   was lost to a controller restart no longer holds its slot indefinitely.
6. GitHub backoff honoured (M): a typed rate-limited error carrying the
   reset time; the poller pauses until it, per installation (shared with
   ZF-101); the reap applies the same; the start-failure backoff gains jitter
   passed through the snapshot so the scheduler stays pure.

Three more findings from the verifier for the second pull request: a work
directory leaks when its container goes away out of band, because removal
learns the directory from the container's label and nothing walks the
work-directory root; a GitHub failure during cleanup is a log line and a
counter, never a problem, so the row is the right place to record it; and
the controller's channel for slowing an agent's polling exists in the
protocol and is never set.

**Cut from the source package:** image retention (Zoomies never deletes an
image; say so and point at `docker image prune`); a maximum job duration;
provider backoff (no provider exists); disk-low handling, which lands with
ZF-103's host resource reporting so the protocol and the hosts table change
once.

**Accept when:** table-driven scenarios (cancel before start, cancel during
run, timed-out registration, daemon restart, listing failure) end with the
runner row, the queued tasks, the fake GitHub's registrations and the fake
backend's workloads consistent and nothing unexplained; a failed deletion is
on the Runners page and in the drawer and clears when it succeeds; a
troubleshooting section says what Zoomies cleans up and what it leaves.

Depends on ZF-102 for the cleanup-failure record's place in the state
machine, except the first pull request. Size M. Session: Claude Opus 5 at
`xhigh`. Decisions: 17.

## 7. Phase 2: operable and usable

Assignment B. Each of these builds on Phase 1's invariants and on the drill
tier from ZF-301b, which is why they come after. ZF-201 is mostly done;
ZF-203 is entirely new; the rest extend substrate that exists.

### ZF-201: the first successful job through the UI

**Classification: existing, needs validation, with two narrow extensions.**
The journey the source roadmap describes exists and had its own review pass:
bootstrap in four steps, the GitHub App manifest flow that resumes after a
wandering browser and keeps only public identifiers in the browser (the
private key never reaches it), the one-page Add-a-host that waits for the
machine to arrive, the five-step pool wizard with a server verdict from
`POST /pools/validate`, and an Overview checklist that ticks itself off and
offers the `runs-on` line to copy. Six of the seven recovery cases the roadmap
lists exist with tests. The seventh is narrower than the source text says: for a
repository-targeted installation the probe already lists that repository's
runners and fails if it cannot; the unchecked case is an organisation
installation granted "selected repositories" that exclude the one the
operator pushes to.

The other real gap is in what Playwright can reach. The suite drives the real
binary, but its fake GitHub is an in-process test server, so the exchange,
verify and any runner or job are never driven from a browser, and the only
fail-then-recover test is a wrong password.

The verifier also found three small defects on the journey itself: the
bootstrap page says "step 1 of 4" and "three steps left" while the Overview
checklist renders five steps on a controller with no host, which is the
state after every controller-only install; the CLI's tested "expired or
already used" remedy never fires for a real spent token, because the API
answers 422 and the transport maps only 401 to the sentinel the CLI keys on;
and the expired-token message is asserted nowhere at the API. All three ride
with the first pull request.

**Do, in three pull requests:**

1. Tests and small fixes: the three defects above; handler tests for verify
   that assert the message names the missing permission; join refusals
   (garbage, reused and expired tokens) asserted at the API and in
   `hosts.spec`; a Playwright test that opens the verify dialog against the
   seeded demo installation, whose probe succeeds deterministically without
   any fake; `/hosts/new` and `/pools/new` added to the accessibility audit
   (the phone audit already visits the pool wizard); a phone-width pass over
   bootstrap and login; the end-to-end test asserts the duration and stats
   shapes the Overview reads (it already asserts the runner id).
2. Verify answers "which repositories can this installation see": repository
   selection, count and the first names from the installation's repository
   list, rendered in the verify dialog. One spec change, both clients
   regenerated, one docs row.
3. A standalone fake GitHub for Playwright: a test-only Go program under
   `test/` serving the existing fake on a loopback port, started by the
   Playwright harness and pointed at through the per-installation API base
   URL the connect dialog already accepts. One spec: connect with a wrong key, verify fails, fix
   the key, verify passes. ZF-202 and ZF-301 reuse it. A resume notice on
   Add-a-host naming an outstanding token (prefix and age, never the plaintext)
   can ride along.

**Cut from the source package:** a browser-driven real job (it needs the same
credentials as the Go end-to-end test and belongs in ZF-301); any new
persistence layer; any rewrite of the wizards.

**Accept when:** the fail-then-recover spec passes in CI without secrets; the
verify dialog shows repository access; the accessibility and mobile audits
cover every journey page.

Depends on ZF-101 for the ineligible-installation message it will show. Size
M. Session: Claude Opus 5 at `high`; Claude Sonnet 5 at `high` for the
tests-only pull request. Decisions: 18.

### ZF-202: diagnostics an operator can act on

**Classification: mixed; two packages wearing one id.** The explanatory
substrate exists and is tested: a problems aggregator with fourteen runtime
codes plus every configuration finding, the two-shape `runners.not_progressing`
problem, a per-job event timeline in the database, a runner timeline rebuilt
from its stamps, the host-fit check the pool wizard uses, `zoomies status`
and `zoomies config print` with secrets blanked, health and readiness
endpoints, and a request id on every response.

What is missing: any support bundle or diagnostics route; a server-side
answer to "why is this job still queued" (the job drawer recomputes a reason
from pool counts in the browser, and the CLI knows only "unmatched"); the
runner's registration stage and its host's last contact on the runner page;
graceful partial failure (one failing store query blanks the whole problems
drawer and returns a 500); any signal that the poller is paused or stale;
and one secret the config blanking misses (the capacity-demand signing
secret). The reconciliation also found that a test on the code this package
extends fails without race detection because two runners created in one
pass share a millisecond and the selection had no tie-break; that is fixed in
this pull request as a Phase 0 defect.

**Do, in two halves:**

*The small fixes that make what exists trustworthy (S each):* **done** —
partial failure tolerance in the problems aggregator with a
`controller.problems_partial` entry naming what could not be gathered; the
missing secret blanked with a reflective test that every secret-shaped config
field is; and a last-poll stamp and pause state in `/meta` with `poller.paused`
and `poller.stale` problems — **not** both attributed to the whole poller as
this said, because ZF-101 has since made the rate-limit hold per installation,
so `poller.paused` names the installation it is holding;
container-started and registered
stamps on the runner view, the stage labels rendered on the runner timeline,
and "host last seen" on the runner facts — which also corrected a mislabel,
since the panel called `started_at` "Registered" and that is what
`registered_at` is; and one sentence for a `waiting` job, which was the one
kind with nothing said about it because every panel explaining a wait keys on
`queued`. **All of the small half is done**, and so is the opt-in fixture the
acceptance names: `ZOOMIES_SEED_STUCK` breaks three things in the demo fleet on
request and a `diagnostics` Playwright project runs against it. Building it
found the defect none of the earlier pull requests could have: a held job is
unmatched by construction, so the default Jobs view hid every one of them.

*The two new things (M and L):* **the explanation is done** — `GET
/jobs/{id}/explanation` returns one answer computed from the last plan, the
pool, the runner and the host, and separates `waiting` from `blocked` because
the two need different advice; and the drawer and the CLI render it
instead of reasoning for themselves, so the explanation is complete.
**And the bundle is done**, which finishes the package: `GET
/diagnostics/bundle` under a new `diagnostics.read` action, assembled section
by section so a failing section lands in an `errors` array rather than failing
the whole, capped by row count per section and by bytes overall, and
secret-free because every section is a rendering the API already serves --
the configuration in it is `/settings`' own key-by-key one, where a secret is
absent rather than blanked. It carries no workflow log body: runner ids and
the existing download route instead. `zoomies diagnostics` writes it to a
file and says what went in. **The action is admin, not viewer, and the
reasoning is worth keeping**: the weakest role that covers a bundle is the
strongest role inside it, because the bundle contains the settings section
and `settings.read` is admin -- a document assembled from admin-only material
does not become viewer material by being assembled.

**Cut from the source package:** request-id propagation into the agent
protocol; a delivery-id migration until a support case asks; any new
reasoning layer beyond the scheduler's own reason strings; log bodies in the
bundle (there is no log redaction and the roadmap's own rule says arbitrary
secrets cannot be recognised).

**Accept when:** an opt-in test fixture (not the demo seed, which
deliberately re-stamps its runners so a demo never reports them stuck)
carries one runner per stuck shape, one blocked pool and one `waiting` job,
and a Playwright spec follows the problems drawer to the runner page and
reads the stage and the fix; the bundle is admin-only, capped,
and passes the secret-absence test; an operator can diagnose a stuck
registration from the runner page alone.

Depends on ZF-102 for the reconciliation semantics the explanation names.
Size L. Session: Claude Opus 5 at `high`; the explanation endpoint's design
is a Claude Fable 5.1 slice. Decisions: 19.

### ZF-203: backup and restore that exist

**Classification: new.** Prose only today: the backup page tells the
operator to run the `sqlite3` command-line backup, which the controller's
container image does not ship, and to copy the encryption key beside it.
Two accidental building blocks are the right shape: an agent whose token the
restored database no longer holds exits with a re-join instruction, and the
embedded agent re-joins itself when the database is replaced. Two startup
behaviours work against a safe restore: the store silently accepts a database
whose ledger names migrations the binary does not embed, and a missing key
file is silently replaced by a freshly generated one, so a restore without
its key fails later inside the first GitHub call rather than at startup.

The driver question is settled: the bundled SQLite supports `VACUUM INTO`
through `database/sql`, producing a single non-WAL file that passes an
integrity check, so no new dependency is needed.

**Do, in four pull requests:**

1. Store primitives and guards (S): **done** — `Backup` by `VACUUM INTO` under
   the write mutex, refusing an existing destination and tightening the copy to
   owner only; `IntegrityCheck`; a read-only open that skips migration, taken
   by the installer's `finished` check and the uninstall's deregistration sweep
   as the verifier's constraint required; a refusal to start on a ledger with
   unknown names, which also corrected `docs/upgrading.md`, since it said an
   older binary against a newer database "will run"; a refusal to generate a
   key over a database that holds sealed secrets; and `crypto.key_mismatch`
   when the key cannot open one, which is the *wrong* key rather than a missing
   one — that case cannot be caught at startup, because a key is proven only by
   opening something.
2. `zoomies backup` (M): **done** — the copy, its integrity result, and a
   manifest with build identity, the migration ledger, the key's fingerprint
   and path, the secrets the restore will need, and the redacted
   configuration; `--keep` retention, which only ever removes a directory this
   command made; the key excluded unless `--include-key` is passed, and the
   summary says which of the two backups was taken **every** time rather than
   only when something is wrong. One backup is one timestamped directory
   rather than a pair of files, which is what makes retention deletable and
   restore a single argument. The backup page's `sqlite3` instructions are
   replaced, resolving the inconsistency the verifier found: the key is kept
   once, wherever secrets are kept, and the manifest's fingerprint is what
   says the two belong together.
3. `zoomies restore` (M): refuses a corrupt copy, a newer ledger and a wrong
   key fingerprint; never overwrites the only working database without
   `--replace`, and then takes a pre-restore copy first; deletes every
   session and unused join token, with flags to revoke API tokens and to
   reset agent tokens; sets the fence; writes an audit row.
4. The fence (M): a `recovery.fenced` setting the controller reads at start;
   reconcile still snapshots and decides so the UI shows what it would do,
   but applies nothing, reaps nothing and creates nothing from the poller;
   a `recovery.fenced` problem whose fix names the checks to make; readiness
   answers 503 with the reason; one audited admin route lifts it. Automatic
   lifting waits for ZF-102's definition of a reconciled fleet.

Five constraints the verifier found for the design: once the store refuses
a newer ledger, the restore command cannot take its pre-restore copy of a
newer live database through the store, so that copy is a raw file copy of
all three files; the installer's finish and uninstall paths open the store
read-write and migrate as a side effect, so they take the read-only option
too; the container's health check is the liveness route, not readiness, so a
fenced controller is not restarted by its runtime; the `settings` table and
the settings API share a name and not data, so the fence surfaces through
problems and readiness; and the backup page tells operators to copy the key
beside the database while the security page and the installer tell them
not to, an inconsistency the rewrite resolves in favour of the fingerprint.

**Cut from the source package:** scheduled backups (a timer and one doc line
suffice); a backup UI page; a separate encrypted key-escrow route; the
page-copy backup API; "reconciled" as a computed condition.

**Accept when:** the mechanical drill is a CI test (backup, stop, restore into
a clean state directory, integrity and fence asserted) and the GitHub half
(sign in, re-join a controlled agent, run a job after lifting the fence) is
recorded once in `roadmap/validation/` with the achieved time and the copy's
age. Cross-machine fencing is an operator procedure, stop the original first,
and the page says so.

Depends on ZF-102 only for automatic unfencing. Size L. Session: Claude
Fable 5.1 at `high` for the fence and guard semantics, Claude Opus 5 at
`high` for the commands. Decisions: 20.

### ZF-204: releases, upgrades and compatibility

**Classification: mixed.** A real skeleton exists: the protocol version is
refused at join; the agent's version is stored on the host and shown on the
Hosts page; version and commit are stamped into `/meta`, the build-info metric
and the User-Agent; the installer refuses an unverified download; the release
workflow publishes draft releases with checksums and multi-architecture
images; the installer upgrades in place preserving config, key, database and
unit identity; the migration ledger's naming rules are tested.

What the acceptance list names is largely missing, and one finding changes
the compatibility story: the protocol check runs at join only and is untested,
so an agent that joined before a protocol bump keeps polling and receiving
tasks; an older agent does not ignore an unknown task kind, it reports the
task failed, and the controller treats any kind it does not recognise as a
lifecycle task and marks the runner failed. `docs/upgrading.md` says the
opposite, and is corrected in this pull request. Also: nothing backs up
before a migration; an older binary silently runs against a newer schema;
every third-party action is tag-pinned with no dependency updates or
vulnerability scanning; every workflow, release and site included, depends on
one runner vendor with no GitHub-hosted path; the only release, `v0.1-alpha`,
is mutable, not marked as a prerelease, and had its assets rebuilt and
re-uploaded two days after tagging; and there is no upgrade test of any kind.

**Do, in six small pull requests, most of them S:**

1. Compatibility enforced and stated: the same protocol check at heartbeat,
   answered by flagging the host incompatible and excluding it from placement
   like a cordon (never a refusal that restarts every agent at once); the
   controller's lifecycle-task predicate becomes an explicit allowlist so an
   unknown kind leaves the runner alone; the policy written in
   `docs/upgrading.md`: protocol must match, an agent may lag by one minor
   release and is shown as behind, a newer agent is unsupported and warned.
2. Skew visible: a derived `host.version_behind` problem and a badge on the
   host card, with its row on the problem-codes page.
3. Schema safety: the store refuses to open a database whose ledger names a
   migration the binary does not embed, with an emergency override that is
   warned about; a test that a failing migration aborts startup, leaves the
   ledger clean and applies on the re-run; a fixture database at the
   `v0.1-alpha` schema migrated to head in a test.
4. Backup before migrate: when the store is file-backed and migrations are
   pending, `VACUUM INTO` a sibling copy first, keeping the last two, using
   ZF-203's primitive and file naming rather than a second one.
5. Supply chain: every action pinned to a commit with its version in a
   comment; a Dependabot configuration for actions, Go modules, npm and
   images, weekly and grouped; `govulncheck` in CI and on a weekly schedule;
   build-provenance attestations for binaries and image digests; OCI labels
   on the images; a release-workflow guard that refuses to re-run on a
   published tag and marks a tag with a hyphen as a prerelease; a
   GitHub-hosted runner path for the release and site workflows selectable by
   dispatch input.
6. One upgrade job in CI: install the latest published release into a
   temporary prefix with the process backend, start it, stop it, start the
   freshly built binary on the same state, and assert the version changed,
   the config bytes did not, and the host row survived. Plus an "upgrading an
   agent host" runbook and a `zoomies hosts drain` composition of cordon and
   per-runner drain.

Six details from the verifier for the pull requests above: the runner and
runner-docker images are built with no version, commit or date build
arguments at all, so build identity is worse for them than for the
controller image; the agent's cordon flag from the heartbeat is log-only
and does not gate its poll loop, which the incompatible-host design reuses
and must first make real; the release workflow has no dispatch trigger, so
the runner-selection input needs a dispatch path that takes a tag; the agent
compares the short version with commit while the host row stores the bare
version, so a skew badge and the agent's own warning disagree for two
builds of one tag; `contents: write` is granted to both release jobs when
only one needs it; and nothing tests the agent's re-adoption of a workload
after its own restart, on which "a binary swap is non-disruptive" rests.

**Cut from the source package:** capability negotiation and multiple
protocol versions; down-migrations; an SBOM pipeline; signature verification
inside `install.sh`; a matrix of upgrade jobs. "Separate trusted release jobs
from experimental Zoomies runners" has nothing to separate today: CI moved to
Zoomies runners and back within two hours on 5 September, and the live
concern is the single vendor.

**Accept when:** the incompatible-agent, interrupted-upgrade, bad-artifact
and old-binary cases have tests; the upgrade job is green; the policy page
matches the code; `v0.1-alpha` is marked a prerelease and the first real
drill, `v0.1-alpha` to the next tag, is recorded in `roadmap/validation/`.

Depends on ZF-203 for the backup primitive. Size L across small pull
requests. Session: Claude Opus 5 at `high`; Claude Sonnet 5 at `high` for
the pinning, Dependabot and labels work. Decisions: 8, 9, 21.

### ZF-205: telemetry and bounded history

**Classification: mixed; split in three.** The substrate exists: a
Prometheus registry whose labels are pool name, backend and installation only,
with a test that keeps repository and workflow names out of them; hourly
retention pruning of jobs, runners, samples, webhook deliveries and scaling
events (audit rows are never pruned, by a documented choice); a drop-not-block
log relay; an SSE ring with epoch-and-sequence ids and a `resync` frame the UI
answers by refetching; pagination clamped to 500 on the three long lists with
indexes shipped in migrations 0001 and 0009.

What is missing: a queue-age gauge, a reconcile-error counter, a cleanup
outcome counter, any exposure of the poller's pause; tests for three of the
five prune queries and the prune loop; any load fixture at all (the demo seed
is fixed at three hosts and fifty jobs and refuses to run on a real fleet); a
client-side dedupe of the scaling feed after a replay; and the stats window,
time zone and freshness rendered from data rather than hard-coded prose.

**Do, in three pull requests:**

1. Metrics and prune tests (S, ready now): the four missing series, a
   registry-wide test that every family's label names stay in the allowed set,
   the rows in `docs/metrics.md`, tests for the prune queries and loop. The
   poller-pause gauge is fleet-wide and named so, because the pause is global
   until ZF-101 makes it per installation.
2. UI honesty (S, ready now): window, time zone and freshness from the
   payload; the scaling feed dedupes on event id after a resync. The
   Overview's subtitle already names the scope it counts and follows the
   header's "Other runners" switch, so the window replaces the hard-coded
   half of that sentence and leaves the scope half standing. Both scopes
   shipped after the reconciliation and are not this package's to revisit:
   the stats payload carries `fleet` beside the unscoped figures rather than
   answering a query parameter, because one frame serves every viewer, and
   migration `0018` records both on every sample. Every new series and the
   load fixture keep both.
3. The measurement (L): a test-only generator that writes ten simulated
   hosts and ten thousand historical jobs through the store's single writer,
   never a knob on the demo seed; a harness that runs the paginated reads and
   the primary navigation against it while a live job runs; the result
   recorded in `roadmap/validation/`. Indexes and query changes only where the
   measurement shows the need, in a migration with the next unused prefix.
   Two code paths already load whole windows into memory (queue waits,
   startup samples) and the usage query cannot use the completed index, so
   expect at least one.

Four things the verifier added: `docs/security.md` still says repository
and workflow names appear in metric labels, which the code stopped doing,
so the sentence is corrected rather than extended; an hourly prune that
deletes more than 256 runner rows publishes one frame per row into a bus
whose per-subscriber ring is 256 deep, cutting off every subscriber and
making every open tab refetch six endpoints, which is exactly the reconnect
storm the measurement must include and a batched frame would bound; offset
pagination is untested at every layer, so the measurement needs a
regression net first; and the audit list filters on an action column with
no leading index, the likeliest scan the measurement will find. The
counters the contract names (observed, eligible, created for, ran here, ran
elsewhere, ran on another provider, Zoomies fault, cleanup pending, cleanup
converged) are this package's to name and bound.

**Cut from the source package:** durable operation ids (nothing in this
package is long-running; the contract belongs to Phase 4); pruning audit rows
(a deliberate never-prune choice that only an explicit decision reverses).

**Accept when:** the series and tests are merged; the measurement is recorded
with its setup; the roadmap's p95 figures are recorded evidence and not a
pull-request gate.

Depends on ZF-002 for the contract and on ZF-101 and ZF-102 for the
scheduling-latency series to consume the real timestamps rather than the
proxy. Size M overall. Session: Claude Sonnet 5 at `high` for the first two,
Claude Opus 5 at `xhigh` for the measurement. Decisions: 22.

### ZF-206: Windows runners

**Classification: new, and not authorised before Gate F.** The matching
vocabulary is already there and nothing behind it is: `naming.OSWindows` is a
legal operating system, `windows` is in `store.ImplicitLabels`, and
`docs/hosts-and-pools.md` even uses `os=windows` as a pool example — while no
Windows binary is built anywhere, no runner image has a Windows base, and the
`process` backend refuses the platform outright. A pool declaring it today
matches nothing and says nothing useful about why, which is the first thing
this package has to stop.

Decision 26 chooses the shape. Everything below is the *process on a Windows
host* path, because that is the recommendation; the container path is named at
the end and is a different package if it is ever wanted.

What is missing, and it is more than it looks:

* **The agent does not build for Windows.** `make dist` and CI's build matrix
  cross-compile linux and darwin on two architectures each, and `install.sh`
  refuses anything else — it is POSIX shell, so the enrolment path a Windows
  host would use does not exist either.
* **The release asset is a `.zip`.** `runnerAsset` knows `.tar.gz` names and
  `extractTarGz` is the only unpacker; GitHub publishes
  `actions-runner-win-x64-<version>.zip`. Its refusal message is also simply
  wrong today — it says "actions/runner ships for Linux and macOS", and
  `win-x64` and `win-arm64` have shipped for years.
* **No digests.** `runner_digests.go` is generated from a five-entry platform
  list with no Windows rows, and the tamper check refuses anything it cannot
  match — correctly, so this is a generator change and not a bypass.
* **Process control is a stub.** `process_windows.go` has a no-op
  `detachRunner` and a `signalRunner` that can only kill: there is no process
  group to kill a job's children with, and no interrupt to drain with. A job
  that spawns `msbuild` leaves it behind when the runner dies, which on a
  machine whose state persists is exactly the leak the container backend does
  not have.
* **A locked file cannot be deleted.** Cleanup on Windows meets open handles
  where POSIX meets none, so removing a work directory is a retry-with-backoff
  problem rather than one `RemoveAll`, and ZF-105's convergence target has to
  survive that.
* **Host resources report nothing.** `diskSpace` is `syscall.Statfs` behind a
  `linux || darwin` tag, and Windows falls to `disk_other.go`, which honestly
  answers "cannot measure". ZF-103a's design already handles that — the host
  keeps what was known and the badge says resources are unknown — so this is
  the one gap that is already survivable, and it stays survivable until
  somebody writes `GetDiskFreeSpaceEx`.

**Do, in four pull requests:**

1. **Done.** Tell the truth about what is not supported. `runnerAsset`'s
   message named a platform actions/runner does not have; it now names the one
   Zoomies does not ship for. A pool whose `host_selector` declares
   `os=windows` is refused at create and at the wizard's review step, with a
   reason that says adding a host would not help — the failure it replaces was
   a pool that matched nothing and read as a fleet short of capacity. The
   `os=windows` example is out of `docs/hosts-and-pools.md`. No behaviour was
   added, and the product stops implying a platform it has not got. It was
   worth doing whether or not the rest of this package is ever authorised, and
   the rest still waits on decision 26 and on Gate F.
2. Build and enrol. `windows/amd64` in `make dist` and the CI build matrix, a
   PowerShell counterpart to `zoomies agent join` that writes the service
   through the Windows service manager as the installer does through systemd,
   and the compatibility rows in `docs/upgrading.md`. Tests: the join token
   path is the same code, so what is new is the template and the argument
   quoting, and both are unit-testable without a Windows host.
3. Run a runner. The `.zip` asset name, a zip unpacker with the same
   path-traversal refusal `extractTarGz` has, the generator's platform list,
   and a job object so that killing a runner kills what it started. Drain
   stays a kill on Windows and the docs say so.
4. Cleanup that converges. Retry the work-directory removal against open
   handles, with the same `runners.cleanup_failed` record when it does not,
   so ZF-105's five-minute convergence target means the same thing on both
   platforms.

**Deliberately not in this package:** Windows containers, and therefore the
ephemeral guarantee. A Windows `process` pool gives a job a fresh work
directory and a single-use registration on a machine that keeps its state, and
every page that says "one job per runner, then the container is gone" has to
say so where that is not what happens — the same treatment
`docs/security.md` already gives the `process` backend on Linux. Also not
here: Windows on arm64, a second image catalogue, and any change to what
`:latest` means.

**Accept when:** a Windows host joins a fleet from a fresh machine using only
the documented commands; a queued job with a Windows pool's label runs on it
and the runner and its children are gone afterwards; a pool declaring
`os=windows` on a fleet with no Windows host is refused with a reason naming
what to add; the support matrix carries a Windows row that says shape and
runtime separately, like every other row; and no page claims the ephemeral
guarantee for it.

Depends on Gate F being attempted first — this widens the platform surface and
the project is trying to prove the one it has. Also on ZF-105 for the cleanup
record and ZF-103a for the unknown-resources badge, both of which are done.
Size L. Session: Claude Opus 5 at `xhigh` for the third pull request, which is
the one where a wrong answer leaves processes on somebody's machine; `high`
for the rest. Decisions: 26.

## 8. Phase 3: real use, drills and Gate F

The harness, the drills and the readiness record. Gate F's targets, as
adopted with the definitions the review corrected, are in
[roadmap/support-and-measurement.md](roadmap/support-and-measurement.md);
the record that says whether they were met goes in
[roadmap/validation/](roadmap/validation/), with "pending" and the exact
action beside anything only the owner can supply. The release remains a
trusted-workload beta; isolation between unrelated customers is not claimed.

### ZF-301, ZF-302 and ZF-303: the harness, the drills and the beta

**Classification: mixed; ZF-301 split in three, most of ZF-302 folded into
its middle slice.** What exists is one real-GitHub test that runs a trivial
workflow in polling mode with the embedded agent and a plain Docker pool,
skips with exit code zero on any missing prerequisite, is wired into no
workflow, checks for orphaned registrations against Zoomies' own API rather
than GitHub's, has no cleanup on failure despite its README, never asserts
the dispatch marker, and has waits that exceed its own Makefile timeout. The
fake GitHub is rich but never delivers a webhook; the controller tests sign
and post payloads themselves. Fake-level tests already exist for every fault
class ZF-302 names except disk pressure. No test anywhere has ever started a
real container.

**Do:**

*ZF-301a, honesty (S to M, ready now, first):* every prerequisite checked
before anything is created; a required mode in which a missing prerequisite
is `blocked` rather than a skip; one JSON result per scenario with a category
of passed, failed, not run or blocked, a reason, the commit, the run link and
any residual cleanup; a `test-e2e-required` target that fails unless every
scenario passed; a ledger written outside the temp directory before anything
is created, with a cleanup on failure that force-deletes the pool, waits for
its runners, deletes the installation and closes the ledger, and a start-up
sweep that cleans stale run ids and reports what it found; the
orphan-registration check against GitHub itself and the container check
against the host; per-run labels so parallel runs cannot collide; the
timeout corrected; the README corrected.

*ZF-301b, the drill tier (L, ready now, the highest-value item):* a package
behind a `drill` build tag that starts the fake GitHub in process, runs the
built binary as a controller against it, runs a second binary as a remote
agent joined by a join token, creates a pool on the `process` backend pinned
to a staged stub runner, drives jobs through the fake, and asserts that a real
workload appears on the host and disappears and the fake's registrations are
empty after removal. It is the first time the product runs as an operator gets
it — the built binary on both ends of a join, with work landing on a real
machine — the only place ZF-302's drills can run repeatably without
credentials, and the fixture ZF-201's fail-then-recover spec and the second
operator's injected failure can use. It qualifies the `process` backend and
not Docker: a tier that needed a daemon could not run on every pull request,
which is the one thing this tier is for, so the default backend's own runtime
qualification stays outstanding and needs a host that has one.
On every pull request for the one lifecycle scenario; nightly for the
drills: kill the controller mid-job and restart; kill the agent mid-job and
restart; a dead Docker socket then a restored one; a rate limit then its
reset; a failing JIT endpoint; a work directory on a small filesystem. Each
drill writes its row (expected, actual, recovery time, cleanup outcome,
human action needed) from a template into `roadmap/validation/`.

*ZF-301c, the real scenarios (L, blocked on the owner):* one workflow per
scenario in the disposable repository: cancellation, an intended failure, a
long job, a Docker build and a service container on a DinD pool, a remote
agent, and a webhook-delivery run distinct from polling through a tunnel the
harness starts. A `.github/workflows/e2e.yml` on dispatch and nightly,
reading credentials from a protected environment, never on a pull request
from a fork, retaining the results and a redacted controller log as
artefacts, disabled until the secrets exist.

*ZF-302, the record (S):* the drill template, the disk-pressure finding,
and the restore-and-rollback drill Gate F's sixth bullet asks for, which
nothing else in this phase records: backup taken, controller stopped,
restore into a clean state directory, fence lifted, one job; and an upgrade
followed by a restore of the pre-upgrade copy, which is what "rollback"
means here because migrations are one way. Nothing is built into the
product for fault injection.

*ZF-303, the readiness record (owner-gated):* the record template, an
evidence helper that pages jobs and runners over the window, classifies
outcomes with the contract's denominators, computes exact percentiles for
each interval (labelled as proxies until the platform timestamps land),
exports daily because the runner retention default equals the observation
window, counts lost jobs from the timeline and cross-scope refusals from the
audit log, and emits the readiness tables in Markdown; the seven days, the 200
attempts with their bursts, the reference deployment of `main`, and the
second operator are the owner's, and the record says "pending" with the exact
action until they happen.

**Accept when:** ZF-301a's categories are the only way a run reports; the
drill tier is green on a pull request; ZF-301c's scenarios each have a run
link; the readiness record's numbers come from exact timestamps with their
denominators, not from bucketed histograms.

Depends on ZF-002 (301a and 301b); on ZF-101, ZF-102 and ZF-105 for the
platform-recorded timings (otherwise proxies); on the owner for 301c and
ZF-303. Size L. Session: Claude Fable 5.1 at `high` for the drill designs
and the reading of their results, Claude Opus 5 at `high` for the harness
code. Decisions: 11, 12.

## 9. Phases 4 to 8: the direction, and what not to touch yet

The source roadmap's Phases 4 to 8 (host onboarding over SSH, a hosted control
plane, cloud providers, a VM backend, managed compute) are the commercial
direction and are gated behind Gate F by the roadmap itself. The
reconciliation confirmed that none of their deliverables exist and that the
seams they would build on do: the single-use join-token enrolment flow, a
narrow per-host execution backend interface with a registry, the advisory
capacity-demand receiver contract with durable per-pool delivery rows, the
read-time usage aggregate with an operator-supplied cost, and the host
actions (patch, cordon, delete).

Their full text stays in
[roadmap/source/](roadmap/source/2026-09-06-follow-on-roadmap-v1.0.md) and is
not repeated here. Three things about them are decided now, because they
change what the first assignment may do:

* **Nothing is stubbed for them during Phases 0 to 3.** No operations table,
  no host-ownership column, no provider interface skeleton, no VM backend
  kind. A wrong enum shipped in a migration is permanent under the
  never-rename rule, and each of those shapes depends on a design that has
  not been done. The one exception is documentation that is misleading today
  regardless of the roadmap: the architecture page's "not a cloud provisioner"
  sentence conflates a per-job VM backend (which the backend interface was
  written for) with a host provisioner (which would be a separate
  controller-side contract); and the capacity-demand receiver page lets an
  event-id-idempotent receiver add the same capacity once per cooldown,
  because the sender mints a new event id on every re-delivery of one unmet
  shortfall. Both are corrected in Phase 1, the second with an additive
  `schema_version` field on the payload.
* **Phase 4 will reverse a documented invariant.** "The controller never
  dials an agent" is stated in three places and underpins the NAT-friendly
  design. Onboarding a rented server from the browser needs the controller to
  dial that machine once. The decision to allow that for bootstrap only, with
  the invariant reworded to "never dials an enrolled agent" and the
  destination policy as the compensating control, is the owner's and belongs
  in a decision record before ZF-402 starts. So does the choice to introduce a
  durable operations table. The codebase has precedents to copy rather than
  a rule against it: the image-prewarm table keeps per-target pending,
  succeeded and failed state with its error and is returned in a 202 body;
  the capacity-demand deliveries table records status, attempts and the
  last error; the job-events table is an append-only history. The migration
  service, which deliberately stores nothing, is the one counter-example,
  and it re-plans a GitHub-side action rather than a half-installed host.
* **Two of the source packages are already costed wrong.** ZF-402's "use the
  existing Go SSH dependency" refers to a dependency that is present only for
  argon2 and has no SSH code behind it. ZF-702's "behind the existing backend
  interface" is true for Go but not for the schema: the pool backend is a SQL
  `CHECK` over three names, so a VM kind needs a table-rebuild migration and
  three enumerations, not a new struct.

Two facts for the later design, found now: no outbound destination policy
exists for any configurable callback (the capacity-demand URL only has to be
an absolute address, so nothing today denies loopback, link-local or
metadata destinations), and enrolling a host under an existing name reuses
the row and cascades the old row's runners away, untested. ZF-104 records
the first; ZF-102 tests the second.

What that leaves for the first assignment is exactly what the source roadmap
says: Phases 0 to 3. Phase 4 additionally carries the deliberately deferred
ZF-404b host-stewardship slice from decision 25: use it to define safe,
opt-in maintenance for dedicated runner hosts without making imported or shared
machines invasive by default.

## 10. The first sequence

Dependency order. Slices marked ∥ can run in parallel sessions. This section
says what order the work goes in; where a package has got to is
[roadmap/progress.md](roadmap/progress.md), which is the only place a status
lives.

**Assignment A**, from `main` at `6d12a72` to `a829a85`; its last package
merged in pull request #120. Every step below is merged except where it says
otherwise.

1. ZF-001 and ZF-002: the reconciliation pull request that wrote this
   document, plus its two Phase 0 fixes (the stuck-runner tie-break, the
   compatibility paragraph) and the small code slice (applied migrations on
   `/readyz`, the `waiting` and `approved` timeline kinds, the four-way stats
   split). ZF-002 cannot reach `validated` without the owner's reference host.
2. ZF-003, hygiene; no dependency.
3. ZF-105's first pull request, the listing-failure defect; it ships alone.
4. ZF-101 (first pull request, migration `0012`) ∥ ZF-102 (invariants, lock
   and lease, log-relay host binding).
5. ZF-301a (the harness's honesty) then ZF-301b (the drill tier with a
   remote agent), the fixture everything after it uses. It runs the built
   binary against a fake GitHub on the `process` backend, so what it
   qualifies is the controller, the agent, the join and the backend seam --
   not the Docker backend, whose runtime the readiness note records as the
   largest untested surface.
6. ZF-102 (adoption on agent start; late reports and the restart table) ∥
   ZF-101 (per-installation freshness and backoff; visibility).
7. ZF-105 (the rest) ∥ ZF-103a, the reporting half alone: an agent reports
   its host's CPUs, memory and work-directory disk, the `hosts` row gains
   those columns beside the operator's reserve, and the Hosts page shows the
   figures. The scheduler still places by slots at the end of it.
8. ZF-104.
9. **Not done: tag the pre-release (decision 9).** Assignment A ends with a
   readiness note in `roadmap/validation/` saying what Gate F still needs,
   and that note is written; the tag is the owner's and has not been cut.

**Assignment B**, issued after A's evidence and the owner actions in
decision 12, and not before — the gate is not a formality, because ZF-301c
and Gate F need the disposable organisation and everything with it, and
ZF-204's upgrade drill needs the pre-release tag step 9 leaves open. In order:
ZF-103b first, the half of ZF-103 that changes a placement decision and so the
half every scheduling-latency and capacity figure taken after it is measured
against — section 6's second ZF-103 pull request entire, plus what its third
left behind. Then ZF-201, ZF-202, ZF-203, ZF-204, ZF-205 in whichever order
the sessions are available (203 before 204; 202 after 102), then ZF-301c,
ZF-302's record and ZF-303.

**After Gate F**, and not before it: ZF-206, Windows runners. It is sequenced
here rather than in Assignment B because it widens the platform surface, and
the support matrix's rule — a row moves right only when a test runs on the
thing — means a platform added while the project is still proving the one it
has would widen Gate F rather than pass it. Its first pull request was the
exception and **has been taken**: it added no behaviour and only stopped the
product implying a platform it has not got. The other three stay where this
paragraph puts them, and decision 26 comes before any of them.

## 11. What the owner provides, and when

**When** is the commitment as it was set, not a forecast; **State** is where it
stands. A session that finds one of these done updates the row rather than
leaving the table to rot. Every row was outstanding at the end of Assignment A,
which is why the assignment could end while three of its gates stayed shut.

| Needed for | What | When | State |
| --- | --- | --- | --- |
| ZF-002 | A fresh Ubuntu 24.04 LTS amd64 host with `main` deployed natively; its versions recorded in `roadmap/validation/` | Now | Outstanding since Phase 0. Nothing in `roadmap/validation/` records a host, and ZF-002 cannot reach `validated` without one |
| ZF-301c, Gate F | A disposable organisation, a GitHub App installed on it with one organisation and one repository target, a repository carrying the scenario workflows, secrets in a protected environment, a tunnel or public host for the webhook run | Before Assignment B | Outstanding. Every real-GitHub scenario and Gate F itself waits on it; the fake and the drill tier go no further |
| ZF-204 | Immutable releases enabled; `v0.1-alpha` marked as a prerelease; the pre-release tag at the end of Assignment A | End of Assignment A | Outstanding on all three parts. No tag has been cut on Assignment A's work, and the new one needs a name of its own because `v0.2-beta` is taken |
| ZF-303 | A second operator for one setup-and-diagnose session | Any time; the drill tier provides the injected failure | Outstanding |
| Everything | The decisions in section 3 ratified or changed | Now | Records exist for decisions 1 and 2 in `roadmap/decisions/`, both still marked proposed; the rest have been worked to as written without being ratified |

## 12. The coding-agent instruction

One instruction per assignment, copied with the whole document into the
session's first message. It replaces the source roadmap's section 15.

### Assignment A — spent

This is the instruction Assignment A was issued with, kept as the record of
what was asked for. **Do not copy it into a new session as it stands.**
Assignment A has run and ended with the readiness note in
[roadmap/validation/](roadmap/validation/); what it did not reach is section
10's step 9 and the owner rows in section 11. The commit it names is the one
sections 5 to 8's classifications were taken against, not the state of `main`,
so read a classification as history and confirm it against the code before
acting on it. Everything it says about *how* to work still stands, and the
Assignment B instruction below inherits it rather than repeating it.

> Work in `eyupio/zoomies` on the follow-on roadmap in `ROADMAP.md`. Read
> `CLAUDE.md`, `docs/architecture.md` and `docs/upgrading.md` first; there is
> no `AGENTS.md`. `main` at `6d12a72` is the reconciled baseline and
> `roadmap/validation/baseline-6d12a72.md` says what was proved on it.
> Preserve existing work, architecture, UI conventions and public behaviour;
> the progress record already says which packages exist and need evidence
> rather than code.
>
> This assignment is Phase 0, Phase 1, ZF-301a, ZF-301b and ZF-003, in the
> order section 10 gives, with the decisions in section 3 taken as written
> unless the owner has changed one in `roadmap/decisions/`. Keep every change
> in a small pull request with one behaviour and one imperative-sentence
> message; add the tests the package names; update the OpenAPI document,
> both generated clients and the docs pages in the same change; keep
> `roadmap/progress.md` current, including the model and effort that did the
> work. Migrations take the next unused prefix; never rename a shipped one.
>
> Do not build for Phases 4 to 8: no schema, configuration keys, RBAC
> actions, UI or dependencies for them. Do not add a database service,
> Kubernetes or a distributed architecture. Do not start SSH or cloud
> provisioning, payments, multi-tenancy or a VM backend.
>
> Run real external tests only with the designated disposable resources.
> Never invent elapsed observation time, real GitHub runs, benchmark
> results, operator feedback or a completed security review; a skipped test
> is not a pass. When something needs access or a decision only the owner
> can give, do everything that does not depend on it, then say exactly what
> is needed. Report code status separately from gate status, and never
> announce readiness because CI is green.
>
> Before reporting progress, audit each claim against a tool result from
> this session; report failures with their output and skipped steps as
> skipped. Do not add features, refactor or introduce abstractions beyond
> what a package asks; report anything else you notice as a follow-up.

### Assignment B — not yet issued

Decision 4 holds this until Assignment A's evidence and the owner actions in
decision 12 are in hand, and section 11 says every one of those rows is still
outstanding. Do not send it because ZF-201's dependencies inside the repository
happen to be merged: ZF-201 proves a job through the UI against real GitHub,
and ZF-204's upgrade drill runs from a tag nobody has cut.

> Work in `eyupio/zoomies` on the follow-on roadmap in `ROADMAP.md`. Read
> `CLAUDE.md`, `docs/architecture.md` and `docs/upgrading.md` first; there is
> no `AGENTS.md`. Then read `roadmap/progress.md`, which is the only current
> record of where each package has got to, and
> `roadmap/validation/gate-f-readiness-2cc7d9c.md`, which says what Gate F
> still needs and what has never been run. `main` at `6d12a72` is only the
> commit sections 5 to 8's classifications were taken against; the whole of
> Phase 1 has landed since, so confirm a classification against the code
> before acting on it.
>
> This assignment is ZF-103b, then Phase 2 and the rest of Phase 3, in the
> order section 10 gives, with the decisions in section 3 taken as written
> unless the owner has changed one in `roadmap/decisions/`. Keep every change
> in a small pull request with one behaviour and one imperative-sentence
> message; add the tests the package names; update the OpenAPI document, both
> generated clients and the docs pages in the same change; keep
> `roadmap/progress.md` current, including the model and effort that did the
> work. Migrations take the next unused prefix; never rename a shipped one,
> and a migration that touches `jobs` is added to the rebuild test as well as
> to `shippedMigrations`.
>
> `make test` and `make lint` are the floor before a push, `make test-ui`
> when the UI changed, and `make test-drill` alongside them: the drill tier in
> `test/drill/` is the only place the product runs as two real processes, and
> CI runs it on every pull request. `make test-e2e-required` is the
> owner-gated one; without the credentials it records `blocked`, which is not
> a pass.
>
> A behavioural test is kept only once it has been run against the code with
> its rule removed and seen to fail. An assertion that cannot be made to fail
> is deleted rather than shipped, and the pull request says which assertions
> were checked this way. Assignment A found three real defects this way that a
> green suite hid, and one of its own packages was marked done for a column
> that had never existed — so before building anything a package describes,
> check whether it is already there.
>
> Do not build for Phases 4 to 8: no schema, configuration keys, RBAC
> actions, UI or dependencies for them. Do not add a database service,
> Kubernetes or a distributed architecture. Do not start SSH or cloud
> provisioning, payments, multi-tenancy or a VM backend.
>
> Run real external tests only with the designated disposable resources.
> Never invent elapsed observation time, real GitHub runs, benchmark results,
> operator feedback or a completed security review; a skipped test is not a
> pass. When something needs access or a decision only the owner can give, do
> everything that does not depend on it, then say exactly what is needed.
> Report code status separately from gate status, and never announce
> readiness because CI is green.
>
> Before reporting progress, audit each claim against a tool result from this
> session; report failures with their output and skipped steps as skipped. Do
> not add features, refactor or introduce abstractions beyond what a package
> asks; report anything else you notice as a follow-up.


## 13. Change record

* **8 September 2026 — Version 2.14:** `zoomies backup` is done. Two shapes
  settled while writing it. A backup is one directory rather than a database
  file beside a manifest file: retention has something whole to delete, and
  `zoomies restore` will take one argument. And the summary states the key's
  presence or absence on every run, not only on the dangerous one — an
  operator who reads "backed up" and stops is exactly the person the sentence
  is for.

* **8 September 2026 — Version 2.13:** ZF-203's first pull request is done, and
  it sharpened one distinction the package's prose had left implicit: a
  *missing* encryption key over a sealed database is a startup refusal, and a
  *wrong* one cannot be, because a key is proven only by opening something.
  So the two guards live in different places — one in the command that would
  have generated a key, one as a problem code the drawer raises once the
  controller is running — and the restore documentation now says which failure
  looks like which.

* **8 September 2026 — Version 2.12:** the support bundle is done and **ZF-202
  is finished**. Two decisions in it are worth recording. The action is admin
  rather than viewer, because the weakest role that covers a bundle is the
  strongest role inside it — it carries the settings section, and that has
  always been admin. And the byte cap sheds sections rather than refusing the
  request: explanations first because they are recomputable from the jobs
  beside them, then the scaling history, then jobs and runners; the fleet's own
  shape is what a bundle is for and never goes. A short bundle answers some
  questions and a 500 answers none.

* **8 September 2026 — Version 2.11:** the explanation is finished: the drawer
  and the CLI render it rather than reasoning for themselves, which is the
  defect this package named and the reason the endpoint exists. What the work
  clarified is where the boundary falls — a pool's live counts are facts and
  stay in the panel; only the reason moved.

* **8 September 2026 — Version 2.10:** `GET /jobs/{id}/explanation` is done.
  The shape it settled on is worth recording because the rest of the package
  will render it: one summary that is always set, a detail in the scheduler's
  own words where it has any, a fix that is absent when there is nothing to do,
  and `waiting` separated from `blocked` — a fleet that is merely busy clears
  itself and a pool nothing can place never will.

* **8 September 2026 — Version 2.9:** ZF-202's small half and its test fixture
  are done. The fixture is worth a line of its own because of what it changes
  about how this package is verified: the suite's shared fleet is deliberately
  healthy, so every page that explains a fault was unreachable from a test, and
  three pull requests in a row shipped without a pin for that reason. It found
  a defect on its first run — a held job is unmatched by construction, and the
  Jobs page hid every one of them by default.

* **8 September 2026 — Version 2.8:** the poller can be seen. Writing it found
  a stale instruction in this document: ZF-202 asked for `poller.paused` to be
  attributed to the whole poller "because the pause is fleet-wide until ZF-101
  changes it", and ZF-101 changed it — the hold has been per installation since
  its second pull request. The entry names the installation, and the package's
  text is corrected rather than left to instruct the next session wrongly.

* **8 September 2026 — Version 2.7:** the two of ZF-202's small fixes that
  need no API shape are done, and one of them was a disclosure rather than a
  gap: `zoomies config print` has printed `capacity_demand.signing_secret` in
  full for as long as that feature has existed, because the blanking is a
  hand-written list. The replacement guard does not read the list. Nothing in
  the plan changed; the package's entry now says which half of its small fixes
  remains and why (an OpenAPI change and both generated clients).

* **8 September 2026 — Version 2.6:** ZF-103b is done, so Phase 1 is complete
  as code: every placement decision now costs a reservation, and the figures
  ZF-103a taught agents to report are read by something. Two choices the
  package did not name are worth keeping in front of the third pull request,
  because they decide what its numbers mean: the fallback share (a field a pool
  leaves unset costs one slot's worth of the host) is what makes the upgrade
  admit exactly what it admitted before, and free disk is charged only for the
  runners a pass adds, because the measurement already contains what the
  runners already there have written. The floors under a host's reserve are
  documented in section 6 and in `docs/hosts-and-pools.md`, and until the third
  pull request gives the reserve a route they are the only thing holding
  anything back.

* **8 September 2026 — Version 2.5:** ZF-206's first pull request is done, and
  it is the only part of that package this document authorises before decision
  26 and Gate F. Nothing about the plan changed; what changed is that three
  places no longer imply Windows support. The package entry and section 10 say
  so, and the rest of ZF-206 is untouched.

* **8 September 2026 — Version 2.4:** **ZF-206, Windows runners**, added to
  Phase 2 with decision 26 choosing its shape, and sequenced after Gate F in
  section 10. The vocabulary for it already shipped — `naming.OSWindows` is a
  legal operating system and `docs/hosts-and-pools.md` used `os=windows` as a
  pool example — while nothing behind it did, which is the gap the package's
  first pull request closes on its own.

* **8 September 2026 — Version 2.3:** Assignment A is finished, and this
  document is reconciled against the code rather than against itself. Section
  10 says where the assignment started and ended and which of its steps is
  still open; section 11 gains a **State** column, because a table of owner
  actions with no state is a table nobody updates; section 12 becomes one
  instruction per assignment, with Assignment A's marked spent so that a fresh
  session cannot re-issue a finished assignment, and Assignment B's written but
  explicitly not yet sent. Decision 15 and the Phase 1 preamble had ZF-103's
  two halves the wrong way round against how they shipped: 103a is the
  reporting half, 103b the admission half, and both precede Gate F. Delivery
  rule 14 writes down the discipline the whole assignment actually ran on --
  a test is kept only once it has been seen to fail — and rule 7 names the
  two tests a migration must be added to. The ZF-002, ZF-101, ZF-103, ZF-205
  and ZF-301b entries are corrected where they described work that has since
  landed differently.

* **7 September 2026 — Version 2.2:** the migration prefixes this document
  reserved are stale. `0010` and `0011` shipped from other work between the
  plan being written and ZF-101 starting, so ZF-101 took `0012`, and ZF-103's
  host resource reporting took `0017` when it landed later the same day. The
  schema rule itself is
  unchanged: the next file takes the next unused prefix, whatever that is by
  then, which is why the rule and not a number is what this document now
  names.

* **6 September 2026 — Version 2.1:** N02 is fixed and removed as a programme
  gate. Added the deliberately deferred **ZF-404b** host-stewardship and
  bounded-housekeeping slice; it is not authorised for implementation before
  Gate F.

