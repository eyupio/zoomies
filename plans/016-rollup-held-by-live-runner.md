# Plan 016: Let only finished runners hold the roll-up

> **Outcome**: see "What was done" at the end. Only the installation roll-up was narrowed; the usage roll-up's hold is deliberate and stays.

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) and `internal/store/AGENTS.md`
> first. Follow this plan step by step, run every verification, and stop on
> any STOP condition there or here. Update this plan's row in
> `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/store/queries_installation_report.go internal/store/queries_usage_daily.go internal/store/queries_runner_sessions.go internal/controller/background.go
```

## Status

- **Priority**: P0
- **Effort**: M
- **Risk**: MEDIUM (usage accounting)
- **Depends on**: none
- **Category**: bug, unbounded growth
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

Jobs and runner sessions are pruned only up to the day the roll-up has
counted. The roll-up stops at the creation day of the oldest runner that
has no `runner_sessions` row, and a session row is written only when a
runner ends. So one idle, warm or persistent runner holds the roll-up at
the day it was created, and nothing after that day is ever pruned.
Reproduced by the audit: 45 daily jobs, a 30 day cutoff, one idle
persistent runner: 0 days rolled, 0 jobs pruned.

## Current state

- `internal/store/queries_installation_report.go:243-250`, in
  `RollUpInstallations`:

```go
		if err := tx.QueryRowContext(ctx, `SELECT MIN(at) FROM (
			SELECT MIN(queued_at) AS at FROM jobs WHERE state != 'completed' AND queued_at >= ?
			UNION ALL SELECT MIN(created_at) FROM runners r
				WHERE NOT EXISTS (SELECT 1 FROM runner_sessions rs WHERE rs.runner_id = r.id))`, settled).Scan(&held); err != nil {
```

- `internal/store/queries_usage_daily.go:116-121`, in `RollUpUsage`, has
  the same hold on runners without a session.
- `internal/controller/background.go:350-362` clamps the prune cutoff to
  `InstallationRollupUntil`; `PruneRunnerSessions` clamps to the usage
  roll-up.
- `TestTheRollUpWaitsForARunnerThatHasNoSessionYet` covers a finished
  runner whose cleanup is not yet confirmed. That hold must stay.
- Runner states are in `internal/store/models.go`; the terminal ones are
  `removed` and `failed`.

## Scope

**In scope**: the two roll-up queries, their test files.

**Out of scope**: `background.go`, the pruning functions, any migration.

## Steps

### Step 1: Confirm a live runner needs no hold

Read `usageAllocation` (grep `internal/store`) and the installation counts
in `RollUpInstallations`. Write down, for the commit body, where a live
runner's time and counts come from for a day that has been rolled. The
audit's reading: the allocation of a live runner is read from the `runners`
row, and the installation counts only count terminal runners, so rolling
past a live runner loses nothing. If you find a count that is taken from
`runner_sessions` only and would miss a runner still alive when its day was
rolled, that is a STOP.

### Step 2: Write the failing tests

In the test file for each roll-up add
`TestALiveRunnerDoesNotHoldTheRollUp`: a store with an injected clock, 45
completed jobs one per day, one runner created on day 0 left in an idle
state with no session, roll up to the 30 day cutoff. Assert the roll-up
advanced past day 0. In the installation one, also call `PruneJobs` with
the cutoff clamped as `background.go` does and assert 15 jobs went.

**Verify**: both fail before the fix; the existing
`TestTheRollUpWaitsForARunnerThatHasNoSessionYet` passes.

### Step 3: Narrow the hold

In both queries add the terminal-state condition to the runners arm, so
only a runner in `removed` or `failed` with no session holds the roll-up.
Use the state constants' string values the way neighbouring queries in the
file do. Extend the comment above each query: a runner that is still alive
has nothing to record yet, and waiting for it stops every prune behind it.

**Verify**: `go test -race -count=1 ./internal/store/...` passes.

## Done criteria

- [ ] The new tests pass and failed before Step 3; the existing hold test
      still passes unchanged.
- [ ] `go test -race -count=1 ./internal/store/... ./internal/controller/...` passes.
- [ ] `gofmt -l internal/store` and `go vet ./internal/store/...` clean.
- [ ] `make test` exits 0.
- [ ] If `docs/architecture.md` or `docs/` states the hold (grep `roll-up`
      and `runner_sessions`), the sentence is updated.
- [ ] Commit message: `Let only finished runners hold the roll-up`.

## STOP conditions

- Step 1 finds a figure that would be undercounted for a live runner.
- An existing usage or installation report test changes its numbers.

## Maintenance notes

- An instance that has been held for months will prune a large backlog on
  its first pass after this lands. `PruneJobs` already works in batches of
  500; no change is needed, but expect a long first maintenance pass.

## What was done

Step 1 found that the two roll-ups are not alike, so the plan's premise held
for one of them only.

- **Installation roll-up** (`RollUpInstallations`): counts a runner on the day
  it finishes (`runner_sessions.finished_at`, or the row's own `finished_at`
  while its session is pending) and reads a live runner's create-task time from
  its own row. `finished_at` is stamped from the store's clock when a runner
  enters `removed` or `failed`, so a live runner cannot finish on a day that is
  already rolled. Its hold was never needed, and it gates the jobs prune.
  Narrowed to runners in `removed` or `failed` that have no session.
- **Usage roll-up** (`RollUpUsage`): a pure function of the sessions, and a
  session spreads its allocation over every day from its start to its end,
  arriving only at the end. If the roll-up passed a live runner's start day,
  those days would be closed before its session existed and its first days
  would be undercounted. This is the STOP condition in Step 1, and the hold
  stays. It gates only `PruneRunnerSessions`, so a runner that lives for longer
  than `retention.runner_sessions` (a year by default) still delays that prune;
  it does not stop the jobs prune.

Tests: `TestALiveRunnerDoesNotHoldTheInstallationRollUp` (failed before: 0 days
rolled; after: 45 rolled and the 15 jobs older than the cutoff pruned) and
`TestAFinishedRunnerWithNoSessionYetStillHoldsTheInstallationRollUp` (the
finished-runner hold, unchanged). `TestTheRollUpWaitsForARunnerThatHasNoSessionYet`
passes unchanged.
