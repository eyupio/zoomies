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
| ZF-001 | Reconcile completed work and define the next slice | new | `in_progress` | the improvement plan (done) | Claude Fable 5.1, `high`; orchestration of 12 mappers, 12 verifiers, 2 critics | [validation/baseline-6d12a72.md](validation/baseline-6d12a72.md), this review's pull request |
| ZF-002 | Support matrix, invariants and measurement contract | new | `in_progress` | ZF-001 | as ZF-001 | [validation/baseline-6d12a72.md](validation/baseline-6d12a72.md) §Support claims, §Lifecycle timestamps |

## Phase 1: correctness and security under failure

| ID | Package | Classification | Status | Depends on | Session | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| ZF-101 | Enforce GitHub target boundaries everywhere | | `not_started` | ZF-002 | | |
| ZF-102 | Make runner and agent reconciliation convergent | | `not_started` | ZF-002 | | |
| ZF-103 | Reserve host resources and enforce bounded admission | | `not_started` | ZF-102 | | |
| ZF-104 | Verify control-plane access and secret boundaries | | `not_started` | ZF-101, ZF-102 | | |
| ZF-105 | Bound cleanup, retention and external failure handling | | `not_started` | ZF-102 | | |

## Phase 2: operable and usable

| ID | Package | Classification | Status | Depends on | Session | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| ZF-201 | Prove the first successful job through the UI | | `not_started` | ZF-101 | | |
| ZF-202 | Actionable day-to-day diagnostics | | `not_started` | ZF-102 | | |
| ZF-203 | Implement and prove backup and restore | | `not_started` | ZF-102 | | |
| ZF-204 | Safe releases, upgrades and version compatibility | | `not_started` | ZF-203 | | |
| ZF-205 | Usable telemetry and bounded history | | `not_started` | ZF-002 | | |

## Phase 3: real use, drills and Gate F

| ID | Package | Classification | Status | Depends on | Session | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| ZF-301 | Extend the real end-to-end harness | | `not_started` | ZF-002 | | |
| ZF-302 | Restart and recovery drills | | `not_started` | ZF-102, ZF-105, ZF-301 | | |
| ZF-303 | Controlled beta and the readiness record | | `not_started` | everything above | | |

## Open items carried over from the improvement plan

| ID | What | Where it goes |
| --- | --- | --- |
| N02 | Runners stuck in `registering` on a dev instance; not reproducible on `main` | The first observation in ZF-303 on a deployment of `main`; `runners.not_progressing` now names which half it is. |
| D16 | The brand descriptor drawn into the wordmark | The owner's; not roadmap work. |

## Log

Newest first. One line per event that changed a row.

* 2026-09-06: record created from the reconciliation of the follow-on roadmap
  against `main` at `6d12a72`.
