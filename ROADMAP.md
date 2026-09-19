# Zoomies follow-on roadmap

Version 2.40 · 15 September 2026 · derived from the owner's
[follow-on roadmap v1.0](roadmap/source/2026-09-06-follow-on-roadmap-v1.0.md)
after reconciling it against `main` at `6d12a72`, then updated for the
closed N02 incident, the deferred host-stewardship slice, the four
packages an instance operated on somebody else's behalf needs from this
repository, and the publication of `v1.0.0`, which was ZF-218's precondition.

This is the sole active roadmap and delivery-order source of truth for the
next programme: make Zoomies a dependable, secure and easy-to-operate
self-hosted GitHub Actions runner platform, and prove it in real use while
improving performance, host operations and platform coverage. The ordered
delivery plan in section 10 supersedes all earlier assignment ordering;
completed work and operational qualification remain distinct.

[IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) is a finished code-review
record, and [roadmap/source/](roadmap/source/) contains historical inputs.
They explain why work existed, but neither may add, reorder or authorise
current work. [roadmap/progress.md](roadmap/progress.md) is the corresponding
status/evidence record: it records delivery of packages defined here but does
not define a competing plan. New or changed delivery work belongs in this
file first.

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

Each work package keeps the ID the source roadmap gave it, so historic material
can be read side by side without becoming an alternative instruction. Each has
a **classification** from the reconciliation:

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
5. **Keep this plan scoped to self-hosted development.** Section 9 records
   the next technical capabilities and their acceptance gates. Preserve existing
   package IDs and working implementations; assess a new backend or provider
   only when its own design and evidence justify it.
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

23. **Licence and contributor terms.** Keep the AGPL-3.0 licence visible
    and document contributor expectations. No licence change is part of this
    programme.
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
    practical. This profile does not establish isolation for mutually untrusted workloads; *Recommend: adopt before
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
    a user asks for the isolation; and neither before Gate F.* **Taken on
    12 September 2026** as process on a Windows host, per
    [decision 0003](roadmap/decisions/0003-windows-runners-are-processes-on-a-host.md),
    on the owner's instruction to implement it ahead of Gate F and flag it as
    a beta-testing item; the support matrix row says what has and has not
    run.
27. **Separate platform administration from fleet operation.** ZF-207 to
    ZF-210 support platform teams operating an instance for a product team:
    scoped administration, bounded resource use, durable usage reporting and
    repeatable lifecycle automation. One instance remains one trust domain.
    These packages are part of Phase 2 and progress alongside qualification.
28. **A container per job on Proxmox, from the runner image we already
    publish.** Proxmox VE 9.1 can create an LXC container from an OCI image:
    one API call pulls a reference into a container template, and `entrypoint`
    and `env` on the create call carry the OCI contract, so
    `ghcr.io/eyupio/zoomies-runner` could be a container per job with no VM
    and no Docker daemon under it. It would arrive as a *backend* beside
    Docker, Podman and process — not as a second provider — because renting a
    machine and placing a runner are separate contracts and this is the
    second. Three things make it a decision rather than a work package. It
    *weakens* isolation: today a job on a rented machine sits behind a
    hardware boundary, and an LXC per job shares the hypervisor's kernel, so
    it needs the honest warning the process backend already carries, in the
    same words. It gives jobs no Docker at first: the sidecar pattern has no
    equivalent when two containers cannot share a network namespace, and
    privileged nesting costs `Sys.Modify` on `/`. And there is no API to
    stream a container's stdout, which is what `Backend.Logs` feeds. Proxmox
    still calls application containers a technology preview and lists maturing
    them as future work. *Recommend: a one-day spike first — does the runner
    image boot as an application container, register with a JIT config, run
    one job as the runner account and stop — and assess the package only if
    it passes and ZF-214c has run. Not before either.*

29. **Whether any fleet fact may be read without an account.** ZF-222
    proposes one: a name-free, banded status projection for the developers
    whose jobs queue, who have no account here and no way to tell a fleet at
    capacity from a pool that matches nothing. Today the answer is no for
    everything except `/api/v1/meta`, and `metrics.public` is the only setting
    that changes it. The decision is not the page, which is small; it is
    whether "one instance is one administrative trust domain" — delivery rule
    3 — is also one readership. *Recommend: adopt, off by default, in the
    banded, name-free shape ZF-222 describes, and not before ZF-207 has split
    the problems list — a public projection built on today's undivided list
    leaks the platform's findings the first time a validator warning quotes a
    bind address.*

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

### ZF-004: six corrections from reading the instance as a service

**Classification: new; small; behaviour changes, all of them narrowings.**
Found by reading every package with one question — what breaks when the
person operating this controller and the people whose fleet it runs are not
the same — and kept here because none of the six depends on that question:
each is wrong for a single team too.

**Done, in one pull request:**

1. **A real identifier could pass as demo data.** `IsDemoID` was a prefix
   test for `demo` on the random part of an identifier, and the random part
   is base32 over `a-z2-7`, so about one real row in a million began with
   those four letters. Such an installation was skipped by the credential
   prober, the poller and the registration reap; such a host was never
   reclaimed when it went quiet. A fixture is now recognised by its shape as
   well as its prefix (`store.LooksGenerated`), the one fixture whose
   readable name was exactly thirteen letters is renamed, and a test seeds
   every fixture and holds each to the rule.
2. **Installation targets matched by case.** GitHub logins are
   case-insensitive and a delivery carries the canonical case, so an
   installation saved as `Acme` matched nothing: every delivery was recorded
   as "no installation covers" and the fleet scaled for nobody. Targets are
   folded on write and compared folded, including rows written before this,
   and two installations on one target resolve to the older one on every
   call rather than to whichever row SQLite reached first.
3. **The scheduler asked a rate-limited installation to register runners.**
   The poller and the reap already stood down from an installation inside
   its GitHub hold; `mintCredentials` did not, so every pass spent another
   call against a quota that was gone, left a failed row whose only message
   was the refusal, and the pool then backed off from its own failures on
   top of the hold GitHub asked for. The hold now travels in the scheduler's
   snapshot, a held pool creates nothing and says why, and a held pool with
   jobs waiting raises `pool.github_rate_limited`. Draining and removing ask
   GitHub for nothing and still happen.
4. **The usage report was silently short.** It accepts a 366-day range and
   is computed from rows the prune loop deletes on two clocks — jobs at
   thirty days, runners at seven — so a 30-day runner-hours figure was
   short by three weeks with nothing on the page saying so. The response now
   carries `history_from` for each side, and the page says where the figure
   is complete from.
5. **`retention.audit` pruned scaling events, not audit.** Audit rows are
   never pruned, deliberately, so the key promised a deletion that never
   happened and hid one that did. It is `retention.scaling_events` now; the
   old key is still read, and raises `retention.audit_renamed` once.
6. **The public webhook did its expensive work before verifying.** Up to
   five megabytes were read, parsed and checked against every installation's
   secret before the per-address limiter applied, which bounded only the
   record. A delivery with no signature header is refused before its body is
   read; the record and the limiter are unchanged.

**Accept when:** each has a test that fails with the change removed. It does.

Depends on nothing. Size S. Session: Claude Fable 5.1 at `high`, one
session. Decisions: 27.

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

**Do, in three pull requests. All three have landed -- the figures reach the
host view and the Hosts page, the scheduler places by them, and an operator can
now set the reserve and see what the fleet has promised away. The [work-package record](roadmap/progress.md) names the pull requests
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
3. **Done.** Operators can see it: the host view carries the reserve, the allocatable
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
3. **Done.** A standalone fake GitHub for Playwright: a test-only Go program under
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
3. `zoomies restore` (M): **done** — refuses a corrupt copy, a newer ledger and
   a wrong key fingerprint, all three *before anything is moved*, which is the
   property that matters: each is otherwise found after the controller is
   running on the restored data; never overwrites the only working database
   without `--replace`, and then moves it aside with its `-wal` and `-shm`
   rather than copying it, since leaving those beside the restored file would
   replay one database's log into another; deletes every session and unused
   join token while keeping the redeemed ones, which are history; flags to
   revoke API tokens and to reset agent tokens; sets the fence; writes an audit
   row. The fence is a row in the database's own `settings` table rather than a
   line in `zoomies.yaml`, because it belongs to the data: a restored database
   is fenced wherever it is put, and a copy carried to a second machine arrives
   fenced too.
4. The fence (M): **done** — a `recovery.fenced` setting the controller reads
   at start; reconcile still snapshots and decides so the UI shows what it
   would do, but applies nothing, reaps nothing and does not sweep the poller;
   a `recovery.fenced` problem whose fix names the checks to make; readiness
   answers 503 with the reason while liveness does not, so the container
   runtime does not restart a fenced controller; one audited admin route
   (`recovery.write`, admin) lifts it. Automatic lifting waits for ZF-102's
   definition of a reconciled fleet. **The mechanical drill the acceptance
   names is written** and passes: back up a running fleet, stop it, restore
   into a clean state directory, and assert the integrity, the fence, the
   data and the lift.

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
a clean state directory, integrity and fence asserted) — **done**, in
`test/drill`, where the binary is the one an operator has and the restore is a
command run against a database on disk — and the GitHub half
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
one runner vendor with no GitHub-hosted path; the releases -- `v0.1-alpha` when this was
written, and `v0.2-beta` since -- are mutable, not marked as prereleases, and had its assets rebuilt and
re-uploaded two days after tagging; and there is no upgrade test of any kind.

**Do, in six small pull requests, most of them S:**

1. Compatibility enforced and stated: **done** — the same protocol check at
   heartbeat, answered by flagging the host incompatible and excluding it from
   placement like a cordon (never a refusal that restarts every agent at
   once); the controller's lifecycle-task predicate is an explicit allowlist,
   so an unknown kind leaves the runner alone; the policy is written in
   `docs/upgrading.md`. **The verifier's finding about the agent's cordon flag
   is fixed with it**: it was log-only, and an agent now refuses a *create*
   while cordoned or incompatible and does everything else as normal —
   refusing every kind would strand the runners a cordoned host still has to
   drain. **A correction to this plan**: "an agent may lag by one minor
   release" is not what the code can enforce, because nothing compares release
   numbers; what it enforces is that the protocol matches, and lag beyond that
   is a fact the Hosts page shows rather than a rule. The badge for it is
   pull request 2.
2. Skew visible: **done** — a derived `host.version_behind` problem and a badge
   on the host card, with its row on the problem-codes page. **The verifier's
   finding that the two sides disagreed is fixed by making both use one
   comparison**: the agent compared its version *with the commit* against the
   controller's while the host row stores the bare version, so two builds of
   one tag warned in the agent's log and matched on the Hosts page. Releases
   are compared now, not commits -- a rebuild of one tag is the same release --
   and `version.CompareBuilds` is the single answer both use. It refuses to
   order what it cannot parse rather than guessing, because a wrong order
   sends an operator to upgrade the wrong side; a host *ahead* of its
   controller gets its own sentence for exactly that reason.
3. Schema safety: **done. The first half landed early, in ZF-203** — the store
   already refuses to open a database whose ledger names a migration the
   binary does not embed, because a safe restore needed it first; **no
   emergency override was added, and none should be**, since the refusal
   names the release to run and an override is a way to corrupt a database
   under pressure. The rest is now tested: a failing migration stops startup,
   leaves the ledger clean and applies on the re-run; and a database at an
   older release migrates to head. **A correction: there are two releases
   now**, not one — `v0.1-alpha` shipped `0001_init.sql` alone and `v0.2-beta`
   shipped through `0010`, so the fixture covers both, which are the two points
   somebody's database is actually sitting at.
4. Backup before migrate: **done** — when the store is file-backed and
   migrations are pending, `VACUUM INTO` a sibling copy first, keeping the last
   two, using ZF-203's primitive and file naming. **The naming is shared rather
   than merely matched**: the two constants moved into `internal/store`, which
   owns the layout, so a copy taken automatically is restorable by exactly the
   command that restores one taken by hand. **The copies live in their own
   `pre-migration/` directory** rather than beside the operator's: retention
   here deletes, and a rule that kept "the last two" in a directory somebody
   points `zoomies backup --dir` at would eventually take one of theirs.
   Nothing is copied on a first start or a restart with nothing pending.
5. Supply chain: **done. Three of its seven parts had already landed** —
   every action is pinned to a commit with its version in a comment, the
   Dependabot configuration covers actions, Go modules, npm and images weekly
   and grouped, and `govulncheck` runs in CI and on a weekly schedule; the
   plan's reconciliation predates them. **What was missing is now in**:
   build-provenance attestations for the binaries and the controller image's
   digest; OCI labels on both images, including the version, revision and
   creation date the runner images carried none of; a guard that refuses to
   rebuild a published tag; prerelease marking for a tag with a hyphen; and a
   GitHub-hosted path selectable by dispatch on the release and site
   workflows. **Two of the verifier's findings went with it**: the release
   workflow granted `contents: write` to every job and now grants each what it
   needs, and it had no dispatch trigger, so the runner-selection input now
   arrives with the tag it needs. **The pinning rule was a claim, not a
   check** -- CLAUDE.md said CI enforced it and nothing did; `internal/docs`
   tests it now, along with the permissions rule and the release workflow's
   own guards.
6. One upgrade job in CI: install the latest published release into a
   temporary prefix with the process backend, start it, stop it, start the
   freshly built binary on the same state, and assert the version changed,
   the config bytes did not, and the host row survived. Plus an "upgrading an
   agent host" runbook and a `zoomies hosts drain` composition of cordon and
   per-runner drain.

Six details from the verifier for the pull requests above: the runner and
runner-docker images were built with no version, commit or date build
arguments at all, so build identity was worse for them than for the
controller image -- **fixed in pull request 5**, which also gave the
controller image the OCI labels it had none of; the agent's cordon flag from the heartbeat was log-only
and did not gate anything, which the incompatible-host design reuses and had
to make real first -- **fixed in pull request 1**, by refusing creates alone
rather than gating the poll loop, since a cordoned host still has runners to
drain and an agent that stopped polling would strand every one of them; the release workflow had no dispatch trigger, so
the runner-selection input needed a dispatch path that takes a tag --
**both done in pull request 5**; the agent
compares the short version with commit while the host row stores the bare
version, so a skew badge and the agent's own warning disagree for two
builds of one tag; `contents: write` was granted to both release jobs when
only one needed it -- **fixed in pull request 5**, and now tested; and nothing tests the agent's re-adoption of a workload
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

### ZF-207 to ZF-210: repeatable instance operations

These packages make a self-hosted instance easier to administer for a team.
They separate process administration from fleet operation, bound resource use,
retain explainable usage and make installation and recovery repeatable. They
retain one database and one administrative trust domain per instance.

### ZF-207: two audiences for one instance

**Classification: extension; medium.** The RBAC table has three roles and
one policy, and `admin` covers both "manages the fleet's accounts and
installations" and "reads the process's bind address, TLS file paths,
trusted proxies, database path and encryption-key location, changes its
timers, unfences it after a restore and takes its support bundle". The
problems list mixes `bind.public_no_tls`, `proxy.trust_everyone`,
`crypto.*` and `controller.lease_lost` into what every viewer sees, and
several fleet problems name an example runner, repository or host from
wherever the fleet is worst. The copy says "this controller" forty-one times
and sends people to "the controller log" they cannot read.

**Do:**

1. A `platform` action family in `actionRoles` — `platform.settings.read`,
   `platform.settings.write`, `platform.diagnostics.read`,
   `platform.recovery.write`, `platform.problems.read` — with a fourth role,
   `platform`, above `admin` and holding only those. The bootstrap account is
   `platform`; an admin created by one is not. `GET /settings`, `PATCH
   /settings`, `/diagnostics/bundle` and `/recovery/unfence` move behind it;
   the RBAC walk test and `docs/security.md`'s table say so.
2. `Problems()` split into the platform's (every validator finding, the
   lease, loop panics, the update check, the capacity-demand receiver, and
   any problem whose detail names a bind address or a URL the process
   serves) and the fleet's; `/problems` and `problems.updated` carry the
   fleet's to everyone and the platform's only to a `platform` identity.
3. The copy: "this controller" becomes "Zoomies" or the fleet; `ErrorState`
   stops naming a log the reader may not have; the About panel keeps its
   version and drops the database path for anyone below `platform`; the
   Settings tabs a role cannot use are absent rather than disabled.
4. Audit: `audit.read` stays viewer for the fleet's actions, and the source
   address is shown only to `admin` and above — an IP is the one column
   that is about a person rather than about the fleet.

**Accept when:** a viewer, an operator and an admin fixture each see no
bind address, file path, key location or an unauthorised installation's example in any
page or event frame, with a Playwright test per role; the platform role is
the only one that can change a timer or lift the fence; the OpenAPI
document, both clients and `docs/api-surface.md` say the same.

Depends on ZF-202 (the problems drawer it splits). Size M. Session: Claude
Opus 5 at `high`. Decisions: 27.

### ZF-208: limits at the public and agent edges

**Classification: extension; small.** Only the join route is rate-limited
among the agent routes. A host token can heartbeat at any frequency with a
megabyte of runner reports, each costing a read and possibly a write on the
single writer; one host can open thousands of parallel task polls, each
holding a goroutine for twenty-five seconds, and the fleet-wide poll shed
then slows every host's task delivery to fifteen seconds. Nothing bounds
the number of hosts, pools, join tokens or SSE subscribers, and the
per-tick create budget is allocated by pool priority, which any operator
sets, so one pool at priority 1000 starves the rest on every tick.

**Do:**

1. A per-host token bucket on heartbeat, report and results, sized from
   `agent.heartbeat_interval` so a well-behaved agent never meets it; one
   in-flight task poll per host, a second answered at once with an empty
   set; a cap on reports per request.
2. Fleet-wide ceilings as configuration — `limits.hosts`, `limits.pools`,
   `limits.runners`, `limits.join_tokens`, `limits.event_subscribers` --
   each zero by default meaning unlimited, each refused at the handler with
   a message naming the ceiling, and each a warning in the validator when
   set on a loopback bind, where it protects nothing.
3. The create budget shared fairly across pools of *different* priority
   before priority decides within a tier: a highest-priority pool may take
   the whole tick only when no lower tier has demand it has waited a full
   interval for. The scaling reason says when a pool was deferred by this.
4. The webhook body cap lowered from five megabytes to one; a
   `workflow_job` is tens of kilobytes.

**Accept when:** a load-tier drill with one hostile agent and one hostile
pool leaves every other host's task latency and every other pool's
time-to-runner within the ZF-002 targets; each limit has a test that
reaches it.

Depends on ZF-301b (the drill tier). Size S. Session: Claude Sonnet 5 at
`high`. Decisions: 27.

### ZF-209: a metering ledger the usage report can stand on

**Classification: extension; medium.** The usage report is computed at read
time from `jobs` and `runners` rows, and ZF-004's fourth correction made it
say where those rows begin rather than pretend otherwise. That is honest and
not enough: a figure an operator charges a team for, or reconciles against
an invoice from their own provider, has to survive the prune loop, be
additive across adjacent reports, and be recomputable from a record that was
written once and never edited. A single fleet needs this for capacity planning and internal cost allocation.

**Do:**

1. A `runner_sessions` table written once, at the moment a runner's cleanup
   is confirmed: runner, pool, host, installation, the job it ran if any,
   started, registered, finished, and the pool's cost rate at the time. Never
   updated; its own `retention.runner_sessions` window, a year by default.
2. A `usage_daily` roll-up per pool, host and installation, produced by the
   prune loop *before* it deletes the rows it is computed from, so the
   report is complete for every day the roll-up covers however short the
   row retention is. Integer seconds and integer minor currency units keep sums exact.
3. `/usage` reads the roll-up for days it covers and the rows for the rest,
   and `history_from` becomes the roll-up's start rather than the rows'.
   `/usage.csv` gains the same. `docs/metrics.md`'s "what happened last
   month is your scraper's problem" sentence is corrected to say what is
   kept.

**Accept when:** a 90-day report taken with seven-day runner retention
matches, to the second, one taken with retention off; a runner session is
written exactly once across a controller restart mid-cleanup (the ZF-302
restart drill); the roll-up is reproducible from the sessions table alone.

Depends on ZF-105 (cleanup confirmation is its write point) and ZF-205.
Size M. Session: Claude Opus 5 at `high`. Decisions: 27.

### ZF-210: unattended provisioning and lifecycle

**Classification: extension; small.** `zoomies init` is a conversation, and
the first administrator is created by reading a setup token out of the
controller's log and pasting it into a browser. That is right for a person
and wrong for a compose file, a Terraform module or anything else that
creates instances without a person watching, and every one of those has to
scrape a log to finish. Backup exists; export and delete of one
installation's whole history do not, and `DeleteInstallation` leaves jobs,
deliveries, scaling events and capacity samples behind by design.

**Do:**

1. `ZOOMIES_BOOTSTRAP_ADMIN` and `ZOOMIES_BOOTSTRAP_PASSWORD_FILE` (or
   `_TOKEN_FILE` for an API token instead of a password) create the first
   platform administrator at start when the users table is empty, audited as
   `auth.bootstrap` with actor `system`, and are ignored — with a warning
   naming them — once any user exists. The setup-token flow stays for
   people.
2. `/readyz` says whether bootstrap is still required, so a provisioner can
   wait on one endpoint.
3. `zoomies export --installation ID` writes everything about one
   installation — its pools, runners, jobs, deliveries, scaling events,
   sessions from ZF-209 and the audit rows that name any of them — as one
   archive, and `DELETE /installations/{id}?purge=true` removes the same set
   rather than only the rows a foreign key reaches. Both are audited;
   neither touches another installation's rows, and a test proves that with
   two installations side by side.
4. `zoomies init --answers FILE` runs the whole installer from a file with
   no prompt, refusing rather than guessing at anything the file leaves out.

**Accept when:** a compose file brings up a controller, an admin and one
joined agent with no human step and no log scraping; export then purge of
one installation leaves the other's rows and figures byte-identical.

ZF-210a (bootstrap, readiness and answer-file installation) depends on ZF-203
and ZF-207's role contract. ZF-210b (export/purge) additionally depends on
ZF-209's sessions. The parent remains incomplete until both are accepted. Size M. Session: Claude Sonnet 5 at `high`. Decisions: 27.

## 8. Phase 3: real use, drills and Gate F

The harness, the drills and the readiness record. Gate F's targets, as
adopted with the definitions the review corrected, are in
[roadmap/support-and-measurement.md](roadmap/support-and-measurement.md);
the record that says whether they were met goes in
[roadmap/validation/](roadmap/validation/), with "pending" and the exact
action beside anything only the owner can supply. The release remains a
trusted-workload beta; isolation between mutually untrusted workloads is not claimed.

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

## 9. Next technical capabilities

The [competitive review](roadmap/competitive-review-2026-09.md) informs this
order. A documented competitor capability is a reason to evaluate a gap, not
proof that Zoomies lacks it or an instruction to clone it. Reconcile each
package against current code before implementation. All new packages below
start as planned work; this roadmap update implements no runtime behaviour.

### ZF-211: qualification and reproducible performance

**Classification: extension; M.** Reconcile README, website, support matrix
and package record against the same release. Distinguish a compiled Windows
agent, a live Windows job, automated Docker tests and owner-run qualification.
Do not replace completed work or manufacture a historical observation window.

**Accept when:** every support claim names a platform, backend and evidence;
a reproducible workload reports cold and warm queue-to-start, image pull,
registration, execution and cleanup, with p50/p95, concurrency, versions,
hardware and sample counts. Run cancellation, host loss and restore cases.
Reuse ZF-002 and ZF-301/302; ZF-303 remains the readiness decision.

**Dependencies:** ZF-002 and existing harness. Documentation reconciliation
can start now; measurements wait only for their actual resources.

### ZF-212: cache performance and bounded retention

**Classification: extension, then design-led new work; M/L.** Inventory
existing caches and prewarming before adding a service. First publish tested
dependency and BuildKit cache recipes. Then assess S3-compatible storage with
an explicit compatibility matrix, key scoping, quotas, retention and UI status.
Do not promise transparent replacement of every GitHub cache action.

**Accept when:** two representative builds publish cold/warm timings and
storage costs; trust boundaries prevent untrusted jobs poisoning protected
caches; active caches survive pruning; credentials are scoped and redacted;
cache misses or storage failure leave builds usable. Existing defaults remain.

**Dependencies:** ZF-105, ZF-205; new storage design requires its own review.

### ZF-401 to ZF-404b: complete existing-host operations

**Classification: extension/new; L in narrow slices.** Prefer the existing
outbound join and Tailcat flow. Add preflight, resumable operation IDs, clear
partial failures, cancellation, verification and revocation. Explicit SSH
bootstrap is optional and follows a destination-policy and trust-verification
decision; it must not become a prerequisite for private hosts.

**Accept when:** a private host joins, reconnects after controller/agent restart
and can be drained and revoked from the UI. An interrupted operation resumes
without duplicate agents or retained bootstrap secrets. Imported hosts remain
non-invasive. Dedicated-host maintenance is opt-in, scoped to owned resources,
previewed and audited; no broad prune, automatic OS upgrade or unrequested reboot.

**Dependencies:** core qualification for release of expanded host operations;
ZF-207/208/210a for administration and onboarding boundaries. Tailcat regression
coverage and documentation can proceed before that release gate.

### ZF-213: pool configuration as code and portable migration

**Classification: extension; M.** Add a versioned, secret-free pool manifest
with export, validation and a reviewed diff before apply. Preserve labels,
the UI/API contract and existing migration PR behaviour; never silently
overwrite drift or broaden repository access.

**Accept when:** export/import round-trips platform, labels and limits; repeat
apply is idempotent; conflicting edits produce a readable diff; absent fields
have documented semantics; dry-run creates no resources. Cover reusable
workflows and matrix labels by explicit mapping or a stated refusal.

**Dependencies:** ZF-201 and existing migration/API contracts.

### ZF-214: shared provider lifecycle, Proxmox first

**Classification: extension with new provider boundary; L, delivered in slices.**
**Owner decision, 13 September 2026: Proxmox VE is the first target.**
Use the existing capacity-demand contract as the entry point. Keep infrastructure
provisioning outside the pure scheduler and separate from runner execution
backends. A provisioned VM initially hosts the existing Zoomies agent and
supported runner backend; this does not establish VM-per-job isolation.

**214a — contract and fake provider (M).** Define a small, versioned contract:
describe capabilities, validate configuration, create, inspect, list and delete;
start/stop are optional capabilities. Define structured failure categories,
operation IDs, bounded deadlines for every call, and an explicit compatibility
policy. Use shared schemas for validation and guided UI forms, with advanced
provider settings available. Keep provider credentials outside runner guests
and bootstrap with scoped, short-lived enrollment credentials.

One durable reconciler owns desired capacity, in-flight reservations, retries,
backoff, operation recovery and ownership-aware cleanup. Providers implement
infrastructure operations, not another scheduler. Preserve signed event
verification, replay protection and schema checks. Reject stale observations;
translate runner slots to machines explicitly and account for pending creates
and overlapping pools without counting shared capacity twice. Prove demand
from zero eligible hosts, extending the publisher if its current eligibility
rules cannot emit that signal. Imported hosts never acquire deletion authority.

**214b — complete Proxmox integration (M/L).** Connect using scoped API
credentials and verified TLS; guide selection of allowed nodes, storage,
network bridge and a prepared Linux VM template. Validate prerequisites before
provisioning. Clone/bootstrap a VM, track asynchronous operations durably,
enroll the Zoomies agent, run a real job, drain, and delete only resources whose
recorded ownership has been verified. Persist resource identity before retrying
ambiguous outcomes. A timeout is not evidence that creation failed.

Reuse the existing host UI and expose provision/enroll/ready/drain/delete
progress, actionable failure reasons, limits and pending cleanup. Allow only
explicitly configured resource ranges and capacity/concurrency limits; report
infrastructure costs as estimates where applicable. Provide an operator runbook
for credentials, template preparation, recovery and orphan review. The initial
template/OS/backend combination is qualified explicitly, not advertised as
support for every Proxmox configuration.

**214c — validate reuse with a second provider (M; after 214b).** Choose the
second target from demonstrated demand and repeatable test access. Confirm it
uses the same contract, reconciler, bootstrap, UI and acceptance suite without
controller-specific branches. A broad catalogue or public plugin marketplace
is not required. ZF-215 may start after 214b acceptance; it need not wait for
214c.

**Optional compatibility experiment (S; at most two engineering days).**
Evaluate one pinned GARM provider's infrastructure operations and whether its
bootstrap can be replaced with Zoomies enrollment. Record adopt/defer, licence
and attribution obligations, dependency cost and a working proof if feasible.
Do not assume binary compatibility: GARM bootstrap uses its own registration,
callback and metadata lifecycle. Stop the experiment if adapting it costs more
than implementing our small contract. It must not gate Proxmox delivery.
Use [GARM's provider interface](https://github.com/cloudbase/garm/blob/main/doc/external_provider.md)
and [bootstrap helpers](https://github.com/cloudbase/garm-provider-common/blob/main/README.md)
as design references; verify the selected release before code reuse.

**Accept when:** a reusable fake-provider suite covers duplicate/out-of-order
events, concurrent demand, delayed creation, quota exhaustion, controller
restart at every operation boundary, failed bootstrap and deletion retries.
Desired targets converge without duplicate hosts; unknown outcomes are
reconciled before another create. A kill switch blocks new provisioning while
allowing drain, recovery and cleanup. Only owned, idle, drained resources can
be removed; ambiguous ownership is quarantined for review.

Proxmox release qualification requires at least 20 create/enroll/run/drain/delete
cycles in designated disposable resources, including scale from zero, a
multi-pool burst, restart, bootstrap failure and deletion retry. Reconcile the
final VM and associated storage inventory: no unexplained owned resources may
remain. Record exact versions, template, limits, timings and cleanup evidence.
Fixture success is not live qualification.

**Dependencies and ordering:** 214a design and fixtures can follow ZF-210a
contract work while cache and portability slices continue. Enabling 214b
mutations requires ZF-207/208 boundaries, ZF-210a enrollment/readiness,
ZF-404 ownership/drain controls and accepted recovery evidence. Prepare the
Proxmox test runbook while access is pending; missing live resources block
qualification, not contract work. The parent is complete only when 214a–c meet
their acceptance; record first-provider qualification separately.

### ZF-215: capacity fallback and scheduled readiness

**Classification: extension; M.** Reconcile existing idle/prewarm features.
Add bounded schedules and explicit fallback among compatible pools or provider
choices. Fallback never changes installation, OS, architecture, trust policy
or cost ceiling without an operator-approved mapping.

**Accept when:** scarce capacity follows the declared policy with a visible
reason; no duplicate jobs or retry storm; schedule timezone and idle cost
are explicit; cold/warm measurements show the benefit.

**Dependencies:** ZF-208 and qualified ZF-214b for provider fallback; existing pools can
be assessed independently.

### ZF-216: staged platform, GPU and VM coverage

**Classification: validation first, new backends separately; L/XL.** Qualify
the already implemented ZF-206 Windows process backend before expanding it.
Next qualify Linux arm64 and a documented GPU path. Evaluate macOS on owned
Apple hardware and one disposable-VM backend as separate designs with real
test hardware, licensing prerequisites and a clear isolation model.

**Accept when:** every promoted support row has a real job and cleanup/recovery
evidence. Process runners explicitly retain host state; container disposal
is not described as VM isolation. GPU admission prevents oversubscription.
VM work proves reset, network/storage isolation and bounded resource use.
No support claim is inferred merely from cross-compilation.

**Dependencies:** ZF-211; new backends also need ownership and lifecycle gates.
macOS and VM expansion do not block improvements to existing Linux fleets.

### ZF-217: GitHub scale-set integration assessment

**Classification: time-boxed design; S.** Compare native scale-set APIs with
the existing webhook/poller path for reliability, permissions, GitHub Enterprise
Server compatibility and operator complexity. Preserve current workflows.

**Accept when:** a decision record uses a small disposable prototype and
states adopt/defer, measured benefit, compatibility and migration cost. This
authorises evaluation, not a second scheduler or a wholesale rewrite.

**Dependencies:** ZF-211 baseline; no new runtime dependency before decision.

### ZF-218: VPS marketplace one-click deployment foundation

**Classification: new; L, implementation first.** **Owner decision, 13
September 2026:** Zoomies `v1.0.0` is being released today and becomes the
first stable baseline for this work once published. Build the complete
provider-neutral deployment package before undertaking its end-to-end test
campaign. This is preparation for one friendly VPS-provider pilot, not an
official marketplace submission or a multi-provider programme.

**218a — stable release and image-reference contract (S).** Record the
controller, agent and runner image references used by the deployment package
from the `v1.0.0` release, including immutable digests where the registry
provides them. The generated deployment configuration must keep controller and
agent on the same release unless an operator deliberately overrides it. `latest`
may remain a convenience channel for ordinary self-installs, but it is never
the unqualified reference in a marketplace artefact.

**218b — provider-neutral package (M).** Add `deploy/marketplace/` containing
a generic Cloud-Init/bootstrap artefact, a non-interactive answer file and a
small, documented set of provider inputs: hostname, public HTTPS URL, DNS
assumption, image reference, administrator bootstrap and persistent-data
location. It must install a pinned Zoomies release, persist only the minimum
configuration needed for upgrades, start the controller safely and make its
health endpoint locally observable. It must not embed GitHub App private keys,
registration tokens or a provider credential in image metadata, cloud-init
user-data examples or logs.

**218c — provider-neutral HTTPS and secure first-run journey (M).** Deliver a
documented reverse-proxy/TLS path that does not require Cloudflare and works
with a provider's DNS and certificate offering or an operator-owned proxy.
The first-run journey must take the administrator from the deployed controller
to a secure GitHub App connection, external URL and webhook delivery without
asking them to place long-lived secrets in a marketplace form. Retain the
existing Cloudflare path as an optional deployment choice.

**218d — operator and partner hand-off (S).** Add a marketplace-facing
deployment guide, a 10-minute first-workflow guide, sizing profiles, data and
backup/restore guidance, upgrade and clean-uninstall instructions, a
`SUPPORT.md` escalation boundary and early-pilot wording. Make the public
positioning explicit: Zoomies is open source, self-hosted and
bring-your-own-infrastructure; the one-click install deploys the controller,
then the customer connects and owns their runner capacity.

**Implementation acceptance:** the four artefacts above are reviewable,
provider-neutral and use the published `v1.0.0` image contract; no secret is
required before the secure bootstrap flow; and no documentation claims an
untested provider integration. Functional and pristine-VPS testing deliberately
follows this completed implementation in ZF-219 rather than being represented
as already proven by this package.

**Dependencies:** the `v1.0.0` release and its published images. This work may
proceed alongside ZF-211 and does not wait for Proxmox lifecycle work. It does
not change the controlled hosted-control-plane/BYO-compute direction.

### ZF-219: one-click verification and friendly-provider pilot readiness

**Classification: validation after ZF-218; M.** Start only when ZF-218a–d are
implemented. Test the exact, released deployment artefact on a pristine Ubuntu
24.04 LTS VPS using the pinned images and a real HTTPS/DNS configuration. Add
the smallest sustainable automated checks after the implementation exists:
Cloud-Init/answer-file syntax and rendering, secret-redaction assertions,
health/readiness, restart and upgrade/rollback checks. Then run the human
first-workflow journey through GitHub connection, webhook delivery, host join,
one workflow and backup/restore evidence.

**Accept when:** the documented path reaches a secure, healthy controller and
a completed workflow without manual file edits or leaked credentials; the
exact release tag/digests, VPS size, timings, DNS/TLS route and recovery
results are recorded; and an independent operator can follow the guide.
Complete one friendly VPS-provider pilot review only after that evidence. Do
not submit to an official marketplace, advertise broad provider support or
begin a second provider before the pilot produces a supportable result.

**Dependencies:** complete ZF-218 implementation, disposable VPS/DNS/GitHub
resources and an authorised pilot provider. This is validation evidence, not a
new runner engine or a hosted Zoomies service.

### ZF-220: resource-aware host allocation for the stable release

**Classification: extension; bounded release slice. Owner direction, 13
September 2026:** make allocation across a pool's compatible hosts account for
machine size, CPU and memory usage, and existing reservations. Prioritise this
small reliability change for the immediate `v1.0.0` review, ahead of the
one-click deployment package. It does not authorise publishing or replacing a
release tag.

Extend the pure scheduler's existing eligibility and reservation rules with a
deterministic CPU/memory headroom score. Sample whole-host Linux usage through
normal agent heartbeats, account for starts not yet represented by the sample,
and retain reservation-based behaviour when usage is stale or unavailable.
Under CPU pressure start one runner at a time; sustained pressure holds new
starts and recovers with hysteresis. Available memory must cover the next
runner and the host reserve. Existing jobs, operator cordons, capacity and
installation/platform boundaries stay authoritative.

Show actual usage separately from commitments, explain admission holds, and
export bounded per-host monitoring gauges. Keep the single binary, SQLite,
outbound agents and existing scheduler; no prediction engine, new dependency,
live job migration or automatic runtime restart.

**Acceptance:** different host sizes and loads produce sensible placements;
cross-pool and pending-start reservations cannot spend the same budget twice;
stale/unknown readings preserve existing reservation limits; a short spike does
not hold the host; recovery never clears a manual cordon; API, UI and metrics
agree on measured usage. CPU and memory pressure mitigation does not prove or
repair the underlying Docker/containerd stall. Runtime-operation health,
durable incident records and richer diagnostics remain separate follow-up
work, rather than an implied part of this release slice.

**Dependencies:** existing ZF-103 reservations and ZF-202 diagnostics. Record
implementation checks separately from live fleet qualification. The release
owner decides inclusion after review; ZF-218 continues against the published
stable baseline.

### ZF-221: runner resilience and efficient startup

**Classification: correctness fixes and bounded extensions. Owner approved
14 September 2026.** Build on PR #276's per-agent start serialisation. Keep
ZF-211 as the measurement programme and ZF-212 as the cache programme.

**Implementation prepared, awaiting CI and live qualification:**

- Keep Docker request contexts alive through response-body consumption and
  preserve explicit graceful-stop budgets. Distinguish timeouts from missing
  sockets in diagnostics.
- Refuse create-by-name after an uncertain existing-runner lookup. Leave the
  task unacknowledged for existing bounded redelivery; do not turn an unknown
  inventory into an immediate failed-runner report.
- Require Docker readiness before the stock Docker-capable runner registers,
  with a validated overall deadline and individually bounded probes.
- Queue starts FIFO, ahead of background prewarming. Runtime failures impose
  a five-second exponential cooldown capped at one minute; successful startup
  clears it. Existing jobs and lifecycle cleanup remain independent.
- Sample resource usage separately from lifecycle reconciliation with bounded
  concurrency and deadlines.
- Prewarm the configured DinD image and coalesce equivalent successful
  background preparations across pools for one minute.

**Further correctness work implemented (September 2026):** per-installation
credential admission, scheduler allocation order preserved through execution,
credential expiry and provision-deadline checks before new agent starts,
outdated-runner diagnostics, owned-container conflict recovery, busy-registration
cleanup deferral and independent host/GitHub cleanup confirmation. Startup
queue and Docker readiness metrics and resource sample timestamps are available.
Regression tests use fake runtimes and GitHub; live burst qualification remains
outstanding. Deadline checks remain compatible with older tasks that omit them;
older agents must be upgraded to enforce them.

**Remaining implementation, not implied complete by this slice:**

1. Extend the implemented admission and queue-deadline controls with separately
   configurable active-create and registration budgets if measurements justify
   them. Preserve cancellation, ownership and restart recovery; never extend
   credentials past validity.
2. Durable runtime incidents and visible recovery status, including operation,
   duration, retry history and host pressure. Surface resource sample age in
   API/UI rather than presenting cached readings as current.
3. Safely reconcile ambiguous create/start responses against labels and
   ownership before retrying. Existing failed-create cleanup remains in place;
   no blanket retries of mutating Docker calls are authorised by this design.
4. Evaluate separate runner/sidecar budgets using actual workloads before
   changing allocation defaults. Continue ZF-212 dependency/BuildKit recipes
   and ZF-215 scheduled warm capacity under their existing boundaries.

**Acceptance and qualification:** run bursts of 1, 4, 8 and 16 requests on a
reference host with cold and warm images. Record startup success, p50/p95
queue-to-ready latency, image pull and registration time, host CPU/memory,
Docker request latency and remaining runner/sidecar containers after cleanup.
Inject delayed response bodies, a stalled Docker probe, inventory failure,
cancellation and an agent restart. Record host size, versions and sample
counts. Automated fixtures are not live Docker evidence.

Automated Go tests can run in the authoring environment. Live measurements still
require a reference Docker host. No performance gain, benchmark result or runtime
recovery on the affected host is claimed.

**Dependencies:** PR #276, ZF-102/105 lifecycle invariants, ZF-211 measurement
and ZF-220 admission. Fix critical correctness defects before optional cache
or platform expansion. Keep the above status current in this root roadmap.

### ZF-222: a status view for the audience that cannot sign in

**Classification: extension; medium. Owner decision pending (decision 29).**
Every fleet fact needs `viewer`. The developer whose job has been queued for
twenty minutes has no account here and no page to read, and GitHub tells them
only that it is queued: it cannot distinguish "no host has a free slot" from
"no pool's labels match this job", from "the App lost a permission", from "the
webhook secret stopped verifying, so nothing was ingested". This controller
knows which — `Problems()` computes it after every reconcile pass and
`/api/v1/stats` carries the queue wait — and none of it reaches the person
waiting.

Most of the material is already public or already derived. `/api/v1/meta`
answers unauthenticated and already carries `version`, `commit` and
`version_channel`. `controller.Problem` already carries `Severity` and `Since`,
which is a computed component status with a start time rather than one a person
sets by hand and forgets to clear. What is missing is a projection, and a tier
below `viewer` allowed to read it.

Two things this is not. It is not a report of whether Zoomies is up: a page
served by the process it describes disappears with it, which is why a hosted
status page is hosted somewhere else, and `/healthz` watched from outside this
machine is the answer to that question. And it is not an incident tracker with
components an operator sets by hand — a hand-set state drifts from the
scheduler's within a day, and the whole reason this is cheap is that the fleet
already knows.

The cost is disclosure. `metrics.public` is a warning today because job and
repository names are visible in the label set; a page on the same listener
carrying pool names, host names and an exact queue depth is the same leak with
better typography. So the projection is name-free and banded rather than
trimmed: proving a body contains no names is a test, and keeping a view's prose
from naming things is a promise renewed every time somebody edits it.

**Do:**

1. `GET /api/v1/status`, a projection rather than a view — the one place in the
   API that is deliberately not a resource's `GET` shape. `state` (`healthy`,
   `degraded` or `blocked`, from the highest severity among the fleet's
   problems), `since`, `version`, and counts as bands (`none`, `few`, `many`,
   `backed_up`) rather than integers, with the median and p95 queue wait
   rounded to the minute. Its `reasons` carry `code`, `severity` and `since`
   and nothing else: no `title`, `detail`, `fix`, `setting`, `target_kind` or
   `target_id`, each of which names a host, a pool, a repository or a bind
   address. A public sentence per code lives beside the operator's in
   `docs/problem-codes.md`, tested in both directions by `internal/docs` the
   way that page already is.
2. `status.mode: off | authenticated | public`, off by default, with
   `ZOOMIES_STATUS_MODE`. `public` raises a `status.public` warning naming what
   becomes readable without an account, and an error when the bind is not
   loopback and TLS is off — the severity-from-circumstances shape
   `security.disable_auth` already has. `off` means all three routes answer
   404.
3. `/status` as its own Vite entry (`web/status.html`), not a route in the app.
   An anonymous visitor should not be handed the router, the event client, the
   command palette and the auth state, and the 200 KB shell budget is for the
   application; the page gets its own, much smaller budget in
   `web/vite.config.ts`. It polls `/api/v1/status` every thirty seconds and
   never opens `/api/v1/events`, because every frame on that bus is a resource
   view by design and carries names.
4. The three states map onto the existing `danger`, `pending` and `idle` tokens
   rather than introducing a fourth status vocabulary for operators to learn;
   `docs/ui-guidelines.md` records the mapping, because the rule being bent is
   that page's.
5. `GET /status.svg`, the fleet's state as a self-contained badge on the
   `docs/badge.svg` pattern, for a team's wiki or a repository README. It is
   served by the controller, so a badge that fails to load is also information;
   `docs/ui.md` says so rather than leaving somebody to read absence as health.
6. The paperwork an endpoint change carries here: `api/openapi.yaml` and
   `go run internal/api/gen_openapi.go`, `make openapi`, rows in
   `docs/api-surface.md` and `docs/configuration.md`, a dangerous-toggle
   section in `docs/security.md`, and the page in `docs/ui.md`.

**Accept when:** a fixture fleet whose every pool, host, repository and runner
is named something distinctive produces a `/api/v1/status` body and a rendered
`/status` page containing none of those names, asserted by a test that searches
each response for every fixture name — that test is the boundary, and the rest
of this package is a page. Each of the four reasons a job waits produces a
distinguishable public state from a fixture. Every problem code has a public
sentence, and no code has only one of the two. `status.mode: public` on a
public bind without TLS refuses to start and names which of the two settings to
change. The default leaves all three routes 404 while the OpenAPI document
still describes them. The app shell's gzipped size is unchanged and the status
entry is inside its own budget. Playwright covers the page signed out, on a
phone, and through the accessibility pass.

Depends on ZF-207, whose platform/fleet problem split this extends with a third
tier below `viewer`, and on ZF-202 for the problems it projects. Size M.
Session: Claude Opus 5 at `high` — the disclosure boundary is the work and the
page is the easy half. Decisions: 29.

## 10. Ordered delivery plan

This sequence supersedes earlier Assignment A/B scheduling, which described
a baseline from 6 September. Consult [progress.md](roadmap/progress.md) before
starting: Phase 1 and substantial Phase 2 work have already landed. An old
description of a missing feature is not evidence it remains missing.

| Order | Work | Exit criterion |
| --- | --- | --- |
| 0a | ZF-221 runner resilience: correctness fixes, then controller admission and recovery visibility | CI plus live failure and burst qualification; cached metrics expose freshness before being treated as current |
| 0 | ZF-220 resource-aware host allocation and pressure admission | Reviewed implementation and automated checks; live fleet qualification reported separately before release inclusion |
| 1 | ZF-218a–d one-click deployment foundation, against the published `v1.0.0` release | Complete provider-neutral artefact and partner hand-off; **implementation complete, not qualified** — the artefact is in `deploy/marketplace/` and no deployment has been run |
| 2 | ZF-219 post-implementation verification and friendly-provider pilot readiness — **now the next unblocked work** | Pristine-VPS and first-workflow evidence, recorded in [marketplace-deployment.md](roadmap/validation/marketplace-deployment.md); one pilot can be invited, not yet broadly listed |
| 3 | ZF-211 documentation reconciliation and evidence inventory | One current support story; historical gaps clearly dated |
| 4 | ZF-208 resource limits; ZF-207 administration boundaries | Host/pool pressure and API/UI access boundaries verified |
| 4a | ZF-222 status view for the queued, if decision 29 is adopted | A name-free projection proven by the fixture-name test; a default install still serves nothing new |
| 5 | ZF-210a unattended bootstrap and readiness; then ZF-214a contract and fake-provider slice | Fresh instance and agent without prompts or log scraping; provider recovery contract proven in fixtures |
| 6 | ZF-209 durable usage; then ZF-210b export and purge | Usage survives retention; export/purge respects installation boundaries |
| 7 | ZF-212 cache recipes; existing-host/Tailcat reliability and ZF-401/403 UX | Faster representative builds and recoverable private-host onboarding |
| 8 | ZF-213 configuration portability; ZF-404/404b ownership and maintenance | Repeatable fleet configuration and safe host operations |
| 9 | ZF-214b Proxmox integration; then ZF-214c second-provider validation and ZF-215 fallback/readiness | Proxmox lifecycle qualified; reuse and fallback assessed without delaying the first provider |
| 10 | ZF-216 platform/VM expansion; ZF-217 scale-set decision | Evidence per new platform and an explicit integration decision |

**Keep two work streams moving:** operational qualification (ZF-301/302/303
and ZF-211) runs alongside the next ready product slice. An unavailable
reference host blocks that evidence, not unrelated documentation or already
authorised core hardening. Critical correctness and security defects interrupt
either stream. Use roughly two core-operation slices for each performance or
platform-expansion slice until steps 2–4 are accepted, then rebalance using
measured operator pain. This is a capacity guideline, not a promise of dates.

**Split ZF-210 without changing its ID:** 210a is bootstrap, answer-file
installation and readiness; it depends on ZF-203 and the ZF-207 role design,
not on the usage ledger. 210b is installation export/purge and depends on
ZF-209. The parent is complete only when both meet acceptance. Bootstrap must
use the authoritative platform role and preserve interactive self-hosted setup.

**Gate F:** retain the Linux reference and actual readiness requirements.
The owner's RC1 live qualification is recorded in the support matrix; it
does not invent the historical seven-day dataset. Do not revive superseded
owner prerequisites as universal blockers, or label an unmeasured target as
passed. Windows implementation remains a beta-testing item until live
qualification. Expanded host provisioning and new isolation backends keep
their own release gates.

## 11. Evidence and owner inputs

| Input | Needed for | Current treatment |
| --- | --- | --- |
| Exact reference versions and existing run links | ZF-211 support reconciliation | Reuse existing evidence; request only missing facts |
| Disposable GitHub resources, runtime and credentials | Remaining real scenarios | Run only where authorised and available; otherwise record blocked |
| Second operator session | Setup and diagnosis usability | Schedule when an operator is available; do not invent feedback |
| Disposable Proxmox VE test scope, template, API credentials and resource limits | ZF-214b live qualification | Proxmox selected; designate allowed resources before live provisioning |
| Windows, arm64, GPU or Apple hardware | ZF-216 | Qualify only the platforms actually exercised |
| Review of provider, SSH and VM designs | Expanded execution/network boundaries | Record the decision before adding the relevant runtime capability |

## 12. Instruction for the next implementation session

Read `CLAUDE.md`, `docs/architecture.md`, `docs/upgrading.md`, this
roadmap and `roadmap/progress.md`. Start at the first unfinished,
unblocked slice in section 10. Inspect current code before interpreting
historical classifications. Preserve completed work and package IDs.

Deliver one reviewable behaviour per PR, with acceptance evidence, applicable
API/client/documentation updates and a progress-row update. Preserve the single
binary, SQLite and pure scheduler. No provider or VM framework is introduced
by planning alone. No live infrastructure is changed without task authority.

Use the repository's applicable checks for behaviour changes. Documentation-only
changes need link, cross-reference, status and scope checks. Do not invent live
runs, elapsed observation, benchmark results or user feedback. Keep implemented,
validated and blocked distinct, and report the exact remaining dependency.

## 13. Change record

* **15 September 2026 — Version 2.40:** add ZF-222, a name-free fleet status
  projection for the audience that has no account and no way to tell a fleet at
  capacity from a pool that matches nothing, and decision 29, which is whether
  one administrative trust domain is also one readership. Sequenced behind
  ZF-207's problem split. Nothing is enabled by default and this entry
  implements no runtime behaviour.

* **13 September 2026 — Version 2.38:** add the owner's bounded pre-release
  allocation request as ZF-220. Prioritise measured host headroom, existing
  reservations and automatic pressure admission; retain separate scope for
  runtime incident diagnosis and remediation.

* **13 September 2026 — Version 2.37:** make repository-root `ROADMAP.md` the
  sole active source of truth for scope, ordering and authorisation of roadmap
  work. `IMPLEMENTATION_PLAN.md` and `roadmap/source/` are historical records;
  `roadmap/progress.md` records status and evidence only.

* **13 September 2026 — Version 2.36:** record the `v1.0.0` release in
  progress as the stable baseline for a VPS one-click-install track once
  published. Add ZF-218 for the
  tangible, provider-neutral deployment, HTTPS/bootstrap and partner-hand-off
  work, followed by ZF-219 for testing and one friendly-provider pilot
  readiness. Testing intentionally follows completed implementation; no
  official marketplace submission is implied.

* **13 September 2026 — Version 2.35:** confirm Proxmox as the first provider.
  Split ZF-214 into shared contract/recovery, complete Proxmox lifecycle and
  second-provider validation. Bring contract fixtures forward after bootstrap
  design, retain mutation/qualification gates, and time-box optional GARM reuse.

* **13 September 2026 — Version 2.34:** reconcile the delivery order with
  completed packages and a current competitor review. Add ZF-211 to ZF-217,
  prioritise ZF-207/208 and split ZF-210 by its actual dependencies. Keep
  qualification alongside product work. Scope the roadmap to self-hosted
  development and retain existing package acceptance criteria.

* **12 September 2026 — Version 2.33:** record six narrow correctness fixes
  as ZF-004 and add ZF-207 to ZF-210 for repeatable instance administration.

* **9 September 2026 — Version 2.32:** the dead-socket drill, in the half a
  tier with no daemon can do honestly: a second agent joins with its Docker
  socket missing, and the fleet has to say so. It does, and well — the host
  names the backend it cannot use and the socket it looked for, and the pool
  with work waiting raises an error quoting that sentence with a fix. The
  reason to write it anyway was the failure mode where saying nothing is
  worst: the jobs still queue and the pool still looks configured, so a fleet
  that quietly places nothing is indistinguishable from a fleet with nothing to
  do. What is left of ZF-302 is the two halves that need a machine this tier
  deliberately has not got — starting a daemon back up, and filling a
  filesystem — and both belong on the reference host the owner actions ask for.

* **9 September 2026 — Version 2.31:** the restore-and-rollback drill Gate F's
  sixth bullet asks for, in the two tiers its halves belong to, and both were
  weaker than they looked. The restore drill stopped at "the fence lifts and the
  controller answers", which proves a fleet *starts*; it now runs a job on the
  restored database, with the agent never told any of it happened. And the
  upgrade check now rolls back — the old binary started again on the copy the
  new build took before migrating. **Written the obvious way, that check passed
  without restoring anything at all.** The published release predates the ledger
  check that refuses a database written by a newer build, so it comes up on the
  migrated one and looks perfectly healthy: the silent start
  `docs/upgrading.md` warns about, and what somebody rolling back under pressure
  would read as success. The check asserts the schema went back now, and the
  documentation says plainly that rolling back *to* `0.2-beta` is the one case
  where nothing will stop you. Twice in two days a check has passed for a reason
  that was not the one it claimed — a directory that outlived its process, and
  now a binary that started on the wrong database. Both were caught by asking
  what would have to be true for the assertion to be worth making.

* **9 September 2026 — Version 2.30:** two more fault drills, and the second
  one found a defect in the stand-down ZF-105 built. A rate limit is the fault
  a real fleet is most likely to meet and the least visible from inside it —
  nothing has crashed, the fleet simply stops — so the product answers it with
  a per-installation hold and a gauge saying so. **Neither engaged.** GitHub
  refuses the *installation token refresh* first, ghinstallation mints that
  inside its transport, and the refusal reached `classify` unclassified: the
  poller kept calling every two seconds while the quota was gone, and
  `zoomies_github_paused` — the signal `docs/metrics.md` tells an operator to
  alert on — stayed at zero. It is fixed, and deliberately only when the
  refusal's own headers say the quota is actually gone: a 403 for a permission
  the App has not been granted comes back with quota to spare, and standing an
  installation down for fifteen minutes over that is a wait that fixes nothing.
  The other drill is the failing JIT endpoint, where the whole claim is that
  the fleet does not make a bad situation worse: the attempt is recorded rather
  than deleted, nothing is left on the host or on GitHub, and it recovers
  without anybody restarting anything. This is what a drill tier is for. Both
  faults have been covered by in-process tests for months; neither test could
  see that the stand-down was never reached, because in process the call that
  fails is the one the test makes.

* **9 September 2026 — Version 2.29:** the first two fault drills, and what
  they found. Both inject their fault while a job is actually running: killing
  the controller leaves the work running and the fleet finishes it after the
  restart, and killing the agent leaves the work running and the agent comes
  back as the same host rather than as a second one. **The finding is the
  agent's:** the exit code is written by the parent that reaped the process, so
  an agent restarted mid-job finds its adopted runner gone with nothing recorded
  and calls that a failure — racing the removal the finished job set off, so the
  same successful build is recorded `removed` on one run and `failed` on the
  next. That is a decision rather than a bug fix, and it is now a row in the
  drill record with the fleet's own accounting named as what is at stake.
  Two smaller things worth keeping: a drill that checked a directory was
  satisfied by a runner that had died, because the marker file outlives the
  process, and a drill that checked once was satisfied by a process that was
  already doomed — a death that follows its parent's arrives a beat later.

* **9 September 2026 — Version 2.28:** ZF-302 starts with the piece the plan
  says has to come first: keeping the drill rows. They were written to
  `roadmap/validation/drills.md` and reached one job summary and nowhere else,
  so the comparison the record exists for — recovered cleanly forty times and
  then did not — could not be made at all. The drill job now commits the rows it
  wrote, from the default branch only: a pull request's rows describe a commit
  that may never exist. Each row gained the run that wrote it, because a row
  worth keeping a month is one that can be taken back to the logs behind it.
  Nothing here is a fault drill yet; those are next, and they are worth writing
  only now that their rows will survive the run that produced them.

* **9 September 2026 — Version 2.27:** ZF-105's last open finding is closed,
  and with it the package. `TaskBatch.Backoff` had been on the wire since the
  protocol was written, published in the OpenAPI document and waited on by the
  agent, and no controller path ever set it — a load-shedding channel that
  existed everywhere except where the load is. The controller now counts the
  polls it is holding and asks the ones that found nothing to come back later,
  by an amount that rises with the excess. What the shape says: the pressure is
  the fleet's *size*, not its workload, because an idle host costs a held
  connection for the whole of a long poll; a batch with work in it is never
  delayed, because the long poll exists so a task reaches its host in the
  instant it is queued; and the wait is jittered, because a controller sheds
  every agent it is holding in the same instant, and an unjittered backoff
  would bring the fleet back together and re-form the queue it was spreading.

* **9 September 2026 — Version 2.26:** ZF-201 is done. Its last pull request
  gives the suite's GitHub fake a port, so the connect and verify pages are
  tested where they live rather than at every layer beneath them, and the suite
  gains its only fail-then-recover journey outside a wrong password. **That is
  what exposed the gap**: replacing an installation's private key had a route
  and no way in, so recovering from the commonest credential mistake there is —
  pasting the wrong `.pem`, a file GitHub hands over exactly once — meant
  disconnecting the installation, which takes its pools and their runner rows
  with it. A key is replaceable; a fleet should not have to be. Two of the
  harness defects were self-inflicted and worth recording as such: a fixture
  that published the fake's address *after* the controller answered `/healthz`,
  so a spec could read a port from the previous run; and a defensive
  `os.Stdout.Sync()` in the fake, which fails on a pipe and so killed the
  program the instant it had said where it was. A guard that turns a working
  program into a dead one is worse than no guard.

* **9 September 2026 — Version 2.25:** ZF-201's first pull request is done, and
  its three defects were all the same kind of thing: the product saying
  something that was not true on the journey a new operator takes. The bootstrap
  page claimed a length it cannot know — four steps, while the Overview's
  checklist draws five on exactly the install that reaches it. The installer's
  remedy for a spent join token had never fired, because a refused token on an
  anonymous route is a 422 and the transport mapped only 401. **And the third
  was larger than the package recorded**: `MissingRequirements` bundled the
  `workflow_job` subscription in with the permissions, so an App with every
  permission it needs but no subscription probed as a *broken credential* and
  was recorded unhealthy — while the fleet it describes works perfectly well on
  the fallback poller, which is a supported way to run. Every other entry in
  that list is something the fleet cannot work without; that one is something it
  works without. Writing the dialog's own test then found a fourth: the demo
  client's shortcut lived at one call site, so pressing Verify on the seeded
  installation — the one a new fleet has — answered "the stored private key is
  not a PEM-encoded RSA key". It is in the client cache now, where every path
  goes through it.

* **9 September 2026 — Version 2.24:** ZF-103's third pull request is done, and
  with it Phase 1: every package in it is `done`. The reserve had a column, a
  store statement and no way in — no route, no caller outside tests — so the
  documented floors were the only reserve any host has ever had. `PATCH /hosts`
  takes it now, **refused rather than clamped** when it would leave nothing to
  place on or is held back from a figure the host has never reported, and
  written by its own statement so a heartbeat can never touch it. The finding
  worth keeping is about provenance: what the Hosts page shows a host has
  promised away is *the scheduler's own sum*, recorded from the snapshot each
  pass decided on rather than recomputed for the page — a figure that
  disagreed with the one placement used would be worse than none, because it
  would be believed, and it reads as unknown until a pass has run rather than
  as zero. `pool.resources_unenforced` says the quiet part: a `process` pool's
  limits bind nothing, the reservation still holds the room, and the room is
  bookkeeping. `host.resources_unknown` is a note rather than a warning, because
  an agent too old to measure its machine is placed by slots exactly as every
  host was before it could.

* **8 September 2026 — Version 2.23:** ZF-205 is done, and with it every
  unblocked package in Phase 2 — ZF-201 waits on the owner's disposable
  organisation and ZF-206 on Gate F. **Two corrections to the package as
  written.** The poller's rate-limit pause is *not* fleet-wide and never was:
  it is keyed by installation, so the gauge carries the installation, and a
  fleet with two of them, one held, is a fleet half working that the described
  flag could not have said. And the time zone and freshness the package asked
  to be rendered from data already were; the window was the only prose, and it
  was worse than prose — a fetch of `/stats` defaulted to a day and a `stats`
  frame to an hour, so the Overview's completed counts and wait percentiles
  changed under the operator a second after every page load while the
  specification said an hour throughout. **Three findings.** A registry-wide
  label rule has to read the collectors' descriptors rather than a scrape: a
  vector with no observations reports nothing at all, so the version that
  gathered and inspected passed happily with a `repository` label added, and
  would have failed on the day something first incremented it. The load
  measurement earned its keep on its first run, finding that the audit log's
  action filter scanned the one table Zoomies deliberately never prunes. And
  the bulk prune's announcement, not its deletion, was the reconnect storm:
  past sixty-four rows it now sends the one frame that already means "fetch
  the resources again".

* **8 September 2026 — Version 2.22:** ZF-204 is done. Its last pull request
  is the upgrade check nothing else could stand in for: every other tier builds
  one binary and asks what it does, and upgrade day asks whether an
  installation is still an installation once the binary under it is replaced.
  `make test-upgrade` installs the last published release the way an operator
  does, swaps this build in over its state, and checks that the version moved,
  the operator's `zoomies.yaml` did not, the host row is the same row, and the
  pre-migration copy exists. **Writing it found that `curl … install.sh | sh`
  did not work at all**: GitHub's /releases/latest only knows about full
  releases, every Zoomies release so far is a prerelease, and the redirect the
  installer reads therefore landed on the release index with no tag in it. The
  documented one-line install has been broken for as long as there have been
  only prereleases, and nothing could have caught it — the installer's only CI
  coverage was a syntax check. **This also discharges an owner action**: the
  package asked for a fresh pre-release tag to upgrade from, and the last
  published release is a better fixture than a tag cut for the test, because it
  is what people actually have. **And a drill was written and thrown away**,
  which is the delivery rule working as intended: an agent-upgrade drill on the
  runtime tier passed with adoption removed, because the reaping it claimed to
  prevent sits behind a two-minute constant a separate process cannot move. The
  wiring is pinned in-process instead, where the clock can be — and the gap it
  exposed was real, since every existing test called `adoptExisting` directly
  and none of them noticed the call disappearing from the startup path.

* **8 September 2026 — Version 2.21:** the supply-chain work is done, and it
  found that three of its seven parts had already shipped: the plan's
  reconciliation predates the action pinning, the Dependabot configuration and
  the govulncheck workflow. The finding worth keeping from the rest is that
  **the pinning rule was a claim rather than a check** — CLAUDE.md has said CI
  enforces it for some time and nothing did. It is tested now, with the
  permissions rule and the release workflow's own guards. The image builds are
  written twice, once per runner vendor, rather than once with a swapped
  action: this workflow runs only on a tag, so CI cannot tell anybody it has
  been broken until the moment somebody needs a release, and the path that
  works today therefore stays byte for byte what it was.

* **8 September 2026 — Version 2.20:** backup-before-migrate is done, and the
  interaction it uncovered is worth recording: `zoomies restore` opens the
  database it restored, so restoring a backup from an *older* release migrates
  it — and now keeps a copy of it as it was first. Restoring an old backup no
  longer consumes it. Both pages say so, which they did not before, because
  until this the migration on restore was invisible.

* **8 September 2026 — Version 2.19:** ZF-204's schema-safety tests are done,
  and writing them corrected the plan's picture twice. There are two releases
  now rather than one, so the upgrade fixture covers both points a real
  database sits at. And the fixture has to be a database built by applying the
  old migrations, not a current one with its ledger trimmed: trimming leaves
  the columns the later migrations add, so re-applying them fails on a
  duplicate and the thing being tested is an upgrade from a database that
  never existed. The first attempt did exactly that and the test caught it.

* **8 September 2026 — Version 2.18:** ZF-204's second pull request is done.
  The distinction it settled is what "skew" means: a *release* difference, not
  a commit difference. Two builds of one tag are the same release — worth
  knowing in a bug report, and not skew — and treating a rebuild as skew was
  what made the agent's log and the Hosts page disagree. The comparison behind
  both refuses to order what it cannot parse, because a wrong order would send
  an operator to upgrade the wrong side, and a host ahead of its controller is
  called out separately for the same reason: there the fix is the other
  machine.

* **8 September 2026 — Version 2.17:** ZF-204's first pull request is done, and
  it corrected two things in this plan. "An agent may lag by one minor release"
  is not a rule any code here can enforce, because nothing compares release
  numbers — what is enforced is that the protocol matches, and lag beyond that
  is a fact to show rather than a rule to apply. And the verifier's cordon
  finding has a sharper answer than "gate the poll loop": an agent refuses a
  *create* while cordoned or incompatible and serves every other task kind,
  because a cordoned host still has runners to drain and an agent that stopped
  polling would strand them. Pull request 3's first item is also already done:
  the ledger refusal landed in ZF-203, which needed it for a safe restore, and
  the emergency override that item asks for should not be built — the refusal
  names the release to run, and an override is a way to corrupt a database
  under pressure.

* **8 September 2026 — Version 2.16:** the fence is done, and ZF-203's code is
  complete; only the owner-run GitHub half of its acceptance remains. Two
  distinctions the work sharpened. Readiness fails while fenced and liveness
  does not, because the container image's health check is `/healthz`: a fenced
  controller must be taken out of rotation and must **not** be restarted, which
  would achieve nothing and lose the operator's session. And the fence stops
  the fleet *acting* without stopping it *deciding* — the plan is still
  computed and published, so an operator can tell "nothing to do" from "not
  allowed to", which is the difference a fenced fleet otherwise cannot show.

* **8 September 2026 — Version 2.15:** `zoomies restore` is done, and it fixed
  the fence's home. `recovery.fenced` is a row in the restored database's own
  `settings` table — the one the verifier noted shares a name with the settings
  API and none of its data — rather than a line in `zoomies.yaml`. The fence
  belongs to the data: a restored database is fenced wherever it is put, and a
  copy carried to a second machine arrives fenced too, which is the case the
  fence exists for. An unparseable value reads as fenced, because the fence is
  the safe side of its own question and a half-finished restore is exactly what
  writes one.

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
