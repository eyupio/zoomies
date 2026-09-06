# Work-package record

The one list for the follow-on roadmap. Every package in
[ROADMAP.md](../ROADMAP.md) has a row here, and the row is where its state
lives: the plan says what a package is for, this file says where it has got
to. Keep it current in the same pull request as the work.

## How to read a row

**Status** is one of:

| Status | Meaning |
| --- | --- |
| `not_started` | Nothing merged. |
| `in_progress` | A branch exists. |
| `implemented` | Merged, CI green, tests present. The gate it serves may still be pending. |
| `validated` | Its acceptance criteria met with evidence linked from [validation/](validation/). |
| `blocked` | Waiting on something named in the row. |
| `superseded` | The need is met by something else; the row links to it and to the evidence that it meets the same criteria. |

**Classification** is what the reconciliation (ZF-001) found on `main` at
`6d12a72`: *existing, needs validation* means the capability is there and
the work is proving it; *extension* means the seam exists and the work
builds on it; *new* means there is nothing to extend.

**Session** records the Claude model and effort that did the work, whether it
ran as one session or an orchestration, and how many review rounds it took
before merge, so that [agent-models.md](agent-models.md) can be judged on
evidence.

## Phase 0: baseline

| ID | Package | Classification | Status | Depends on | Session | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| ZF-001 | Reconcile completed work and define the next slice | new | `implemented` | the improvement plan (done) | Claude Fable 5.1, `high`; one orchestration of 12 mappers, 12 adversarial verifiers and a document critic; 1 review round | [validation/baseline-6d12a72.md](validation/baseline-6d12a72.md); [ROADMAP.md](../ROADMAP.md) §5–7 carry the classification of every package |
| ZF-002 | Support matrix, invariants and measurement contract | mixed | `implemented` | ZF-001 | docs as ZF-001; code slice: Claude Fable 5.1, `high`, one session, 1 review round | [support-and-measurement.md](support-and-measurement.md); the code slice is merged: readiness names the schema, a held job's timeline says so with `waiting` and `approved`, and the completed count is split four ways. `validated` waits on the owner's reference host (decision 12) for the recorded OS and runtime versions |
| ZF-003 | Supply-chain hygiene and three corrections | new; no behaviour change | `implemented` | nothing | Claude Fable 5.1, `high`, one session, 1 review round | every `uses:` in the four workflows is a commit with its release beside it; `.github/dependabot.yml` covers actions, Go modules, npm under `web/` and the images under `deploy/`, weekly and grouped; `govulncheck` runs on every change and weekly against `main`; the three pages say what the code does, and the payload carries `schema_version: 1`. `validated` when CI has run on the pins and the first Dependabot pull requests arrive |

## Phase 1: correctness and security under failure

| ID | Package | Classification | Status | Depends on | Session | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| ZF-101 | Enforce GitHub target boundaries everywhere | new (jobs carry no installation identity; label-only matching on all four paths) | `not_started` | ZF-002 | | Size M; ready now; takes migration `0010` |
| ZF-102 | Make runner and agent reconciliation convergent | mixed (mechanics exist; adoption on restart, a controller lock and the log-relay host check do not) | `in_progress` | ZF-002; N02 needs `main` deployed | PR1: Claude Fable 5.1, `xhigh`, one session; two mapping-and-verification orchestrations over the four packages | PR1 of 4 done: the "Reconciliation invariants" section in `docs/architecture.md` lists every rule with its constant and owner, and `internal/controller/invariants_test.go` pins the silence ladder and the lease-outlasts-work relationship. PR2 (log-relay host binding, state-directory lock and controller lease), PR3 (adoption on agent start) and PR4 (late reports and the restart table) remain |
| ZF-103 | Reserve host resources and enforce bounded admission | extension (slot model complete; no host resource reporting) | `not_started` | ZF-102 | | Size L; 103a before Gate F, 103b after |
| ZF-104 | Verify control-plane access and secret boundaries | mixed (matrix and most tests exist; log relay unscoped to host; streams never re-check credentials) | `not_started` | ZF-101, ZF-102 | | Size M |
| ZF-105 | Bound cleanup, retention and external failure handling | mixed (local cleanup exists; failure invisible; one listing-failure defect) | `in_progress` | ZF-102, except the first pull request | first slice: Claude Fable 5.1, `high`, one session, 1 review round | Size M; the listing-failure defect is fixed in its own pull request with a test that reproduces it on the old code |

## Phase 2: operable and usable

| ID | Package | Classification | Status | Depends on | Session | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| ZF-201 | Prove the first successful job through the UI | existing, needs validation, with two narrow extensions | `not_started` | ZF-101 | | Size M |
| ZF-202 | Actionable day-to-day diagnostics | mixed (substrate exists; no bundle, no per-job explanation) | `not_started` | ZF-102 | | Size L; its failing tie-break test is fixed in this pull request |
| ZF-203 | Implement and prove backup and restore | new (prose only today) | `not_started` | ZF-102 for automatic unfencing only | | Size L |
| ZF-204 | Safe releases, upgrades and version compatibility | mixed (skeleton exists; policy, skew, guards, backup-before-migrate and any upgrade test do not) | `not_started` | ZF-203 for the backup primitive; ZF-003 first | | Size L across small pull requests |
| ZF-205 | Usable telemetry and bounded history | mixed (substrate exists; four series, prune tests, a load fixture and the measurement do not) | `not_started` | ZF-002; ZF-101 and ZF-102 for the real latency series | | Size M in three slices |

## Phase 3: real use, drills and Gate F

| ID | Package | Classification | Status | Depends on | Session | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| ZF-301 | Extend the real end-to-end harness | mixed (one polling-mode test; no result categories, no cleanup ledger, no real container in any test) | `not_started` | ZF-002; 301c on the owner's credentials | | Size L in three slices; 301a and 301b ready now |
| ZF-302 | Restart and recovery drills | mixed (fake-level tests exist for every fault class but disk; no process-level drill) | `not_started` | ZF-301b | | Size S once 301b exists |
| ZF-303 | Controlled beta and the readiness record | new; owner-gated | `not_started` | everything above; the owner's deployment, credentials and second operator | | Size M for the template and helper; the observation is the owner's |

## Open items carried over from the improvement plan

| ID | What | Where it goes |
| --- | --- | --- |
| N02 | Runners stuck in `registering` on a dev instance; not reproducible on `main` | The first observation in ZF-303 on a deployment of `main`; `runners.not_progressing` now names which half it is. |
| D16 | The brand descriptor drawn into the wordmark | The owner's; not roadmap work. |

## Log

Newest first. One line per event that changed a row.

* 2026-09-06: ZF-102's first pull request, the invariants written down and pinned; the package is `in_progress`.

* 2026-09-06: ZF-003 done in one pull request; the package is `implemented`.

* 2026-09-06: ZF-002's code slice done; the package is `implemented`.

* 2026-09-06: ZF-105's first slice, the listing-failure defect in the agent
  reconciler, fixed with a test; the package is `in_progress`.

* 2026-09-06: every Phase 1 to 3 package classified from the package-by-package
  review (12 mappers, 12 adversarial verifiers, one document critic); ZF-003
  added; ZF-001 implemented; two Phase 0 defects fixed in the same pull
  request (the stuck-runner tie-break, the compatibility paragraph).
* 2026-09-06: record created from the reconciliation of the follow-on roadmap
  against `main` at `6d12a72`.
