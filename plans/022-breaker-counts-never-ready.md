# Plan 022: Trip the provider breaker on machines that never become ready

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first. Land this before plan
> 023 (same file). Follow this plan step by step, run every verification,
> and stop on any STOP condition there or here. Update this plan's row in
> `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/controller/machines.go internal/controller/machinerecovery.go internal/controller/machineenrol.go internal/controller/machines_test.go
```

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MEDIUM (rented machines cost money either way)
- **Depends on**: none (before 023)
- **Category**: bug
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

The breaker is meant to pause a provider after three machines in a row fail
before they are ever ready. Its counter is zeroed after any machine step
that returns nil, such as a clone or a start, and the timeouts that end a
machine (create, start, bootstrap, enrol) never count. With a template
whose agent cannot join, each new machine's successful clone resets the
count before its own bootstrap fails. The fleet keeps renting broken,
running, billed machines up to `provider.max_machines`.

## Current state

- `internal/controller/machines.go:1083-1088`, the end of `runMachineStep`:

```go
	case err != nil:
		c.recordMachineFailure(ctx, env, pr, m, opID, err)
	default:
		c.finishMachineOperation(ctx, m, opID, store.MachineOpSucceeded, "", nil)
		c.noteProviderSuccess(ctx, pr.row.ID)
	}
```

- `internal/controller/machines.go:1788-1805`, `noteProviderSuccess`:
  "One machine that worked is the whole of the evidence needed"; it clears
  `ConsecutiveFailures` and `PausedUntil`.
- `internal/controller/machines.go:1746`, `noteProviderFailure(ctx,
  providerID, m)`, is called from one place only, line 1162.
- `internal/controller/machinerecovery.go:151-157`, `failMachine`, records
  the failure and moves the machine to `MachineFailed`; it never counts.
- The move to ready happens in `stepEnrolling`
  (`internal/controller/machineenrol.go`); a machine that has been ready
  has `ReadyAt` set.
- `TestThreeMachinesThatNeverReachReadyStandTheProviderDown`
  (`machines_test.go` near 491) calls `noteProviderFailure` three times
  directly, so it does not cover this path.

## Scope

**In scope**: `internal/controller/machines.go`,
`internal/controller/machinerecovery.go`,
`internal/controller/machineenrol.go`, `internal/controller/machines_test.go`.

**Out of scope**: the breaker's thresholds and pause length, the store,
the provider packages.

## Steps

### Step 1: Read the three functions

Read `noteProviderFailure`, `recordMachineFailure` (around line 1162) and
`failMachine`, and the place in `stepEnrolling` where the state becomes
`MachineReady`. Write down, for the commit body, which failures count
today and which machine field `noteProviderFailure` uses to avoid counting
one machine twice (the audit saw an `Attempts == 0` condition).

### Step 2: Write the failing test

Add `TestMachinesWhoseCloneSucceedsButNeverJoinStandTheProviderDown`,
using the fake provider and the helpers the neighbouring machine tests
use: three machines in turn each pass create and start, then time out in
bootstrap or enrol (advance the injected clock past the timeout and run a
machine pass). Assert the provider is paused after the third. Add
`TestAMachineThatBecomesReadyClearsTheBreaker`: after two such failures,
one machine reaches ready, and the count is zero.

**Verify**: the first test fails before the fix (the count never passes
one or two).

### Step 3: Move the reset and count the timeouts

1. Remove `c.noteProviderSuccess(ctx, pr.row.ID)` from the default branch
   of `runMachineStep`.
2. Call `noteProviderSuccess` where `stepEnrolling` moves a machine to
   `MachineReady`, once per machine.
3. In `failMachine`, when the machine was never ready (`m.ReadyAt == nil`),
   call `noteProviderFailure` for its provider, unless that machine has
   already been counted by `recordMachineFailure` (use the guard found in
   Step 1 so one machine counts once).
4. Rewrite `noteProviderSuccess`'s comment: the evidence that a provider
   works is a machine that became ready, not a step that returned.

**Verify**: the new tests and `go test -race -count=1 ./internal/controller/...`
pass.

## Done criteria

- [ ] The new tests pass; the first failed before Step 3.
- [ ] `TestThreeMachinesThatNeverReachReadyStandTheProviderDown` passes.
- [ ] `go test -race -count=1 ./internal/controller/...` passes.
- [ ] `gofmt -l internal/controller` and `go vet ./internal/controller/...` clean.
- [ ] `make test` exits 0.
- [ ] If `docs/` describes when the breaker clears (grep
      `ConsecutiveFailures` and "three" in the provider docs), it matches.
- [ ] Commit message: `Trip the provider breaker on machines that never become ready`.

## STOP conditions

- `failMachine` is also used for machines that failed after being ready
  in a way the `ReadyAt` check does not separate.
- `noteProviderSuccess` is relied on elsewhere to clear a provider's
  "trouble" note after a single good call (`clearProviderTrouble`): if
  removing the per-step call leaves a stale trouble note on a healthy
  provider, keep `clearProviderTrouble` on the per-step path and move only
  the breaker reset, and say so in the commit body.

## What was done

- The per-step reset in `runMachineStep` is gone; a step that returns only
  clears the provider's "last trouble" note, which is about now and goes stale
  as soon as the provider answers. The breaker is cleared in `machineReady`,
  which `stepEnrolling` calls at both of its moves to ready (the host already
  linked, and the join token redeemed), and only when the move happened.
- The four timeouts in `recoverMachine` (create, start, bootstrap, enrol) go
  through `failMachineTimedOut`, which counts the machine against its provider.
  `failMachine` itself does not: it is also used when a machine's resource is
  destroyed outside Zoomies, which says nothing about the template.
- The count has to be taken **before** `failMachine`. `RecordMachineFailure`
  adds one to the machine's attempts and `transitionMachine` copies the row
  back, so afterwards `noteProviderFailure` sees attempts above nothing and
  takes the machine for a retry it has already counted. The first version of
  this change counted after, and the tests caught it.
- Known and accepted: every successful step resets `attempts` to 0, so a machine
  that fails one step, retries successfully and then times out counts twice. That
  errs towards standing the provider down, which is the safe side.
- Test-side: the two new tests use a clock the store and the controller share
  (`sharedClock`), because `harness.advance` moves only the controller's clock
  and every row stamped afterwards looks as old as the whole advance. And
  `tokenFromBootstrap` now reads the whole file before judging it, since with
  more than one machine in the fake it could return another machine's token.
