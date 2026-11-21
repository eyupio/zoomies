# Plan 020: Act on a webhook's runner only inside its own installation

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first. Follow this plan step by
> step, run every verification, and stop on any STOP condition there or
> here. Update this plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/controller/webhooks.go internal/store/queries_fleet.go internal/store/queries_events.go
```

## Status

- **Priority**: P1
- **Effort**: S-M
- **Risk**: MEDIUM (webhook ingest)
- **Depends on**: none
- **Category**: security
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

A delivery is verified with the webhook secret of the installation that
covers its repository. After that, the runner it names is looked up across
the whole fleet. The holder of one installation's secret can sign a
`workflow_job` with `action: completed` naming another installation's idle
runner: the runner is marked removed and a remove task is queued. Sending
`in_progress` first marks it busy. Runner names are visible to any viewer
and in public job logs. The comment in `verifyDelivery` already treats one
installation's secret holder forging for another as in scope. It matters
only on a controller serving more than one installation.

## Current state

- `internal/controller/webhooks.go:448-454`, in `applyWorkflowJob`:

```go
	var runner *store.Runner
	if e.RunnerName != "" {
		if r, err := c.st.GetRunnerByName(ctx, e.RunnerName); err == nil {
			job.RunnerID = r.ID
			runner = r
		}
	}
```

- Lines 491-529 then call `StartRunnerJob` for `in_progress` and
  `CompleteRunnerJob` for `completed` on that runner, and queue
  `agent.TaskRemoveRunner` for an ephemeral one.
- `job.InstallationID` is set earlier in the same function from the
  verified delivery. A runner has a `PoolID`; a pool has an
  `InstallationID`.
- `verifyDelivery`'s comment is at `webhooks.go:276-284`.

## Scope

**In scope**: `internal/controller/webhooks.go`, its test file.

**Out of scope**: `GetRunnerByName`, `ApplyJob`, the signature checks.

## Steps

### Step 1: Read how the pool is at hand

In `applyWorkflowJob`, find how the matched pool `p` is read (lines just
above 448) and whether the controller has a cheap way to read a runner's
pool (`c.st.GetPool` or a cached lookup). Note it for Step 3.

### Step 2: Write the failing tests

In the webhook test file, following the tests that already deliver a
`workflow_job`, add:

- `TestADeliveryCannotReleaseAnotherInstallationsRunner`: two
  installations with a pool and an idle ephemeral runner each. Deliver a
  verified `completed` job for installation B's repository naming
  installation A's runner. Assert A's runner state is unchanged, no remove
  task was queued for it, and the saved job has no `RunnerID`.
- The same for `in_progress`: A's runner is not marked busy.
- A control row: the delivery names B's own runner and the runner is
  released as today.

**Verify**: the first two fail before the fix.

### Step 3: Check the installation before linking

After `GetRunnerByName` succeeds, read the runner's pool and keep the
runner (and set `job.RunnerID`) only when the pool's `InstallationID` is
non-empty and equals `job.InstallationID`. Otherwise leave `runner` nil,
do not set `job.RunnerID`, and log at warn level with the delivery's
installation, the runner and its pool, in the file's logging style. A pool
that cannot be read counts as a mismatch. Comment why: the secret proves
which installation sent the delivery, not that the runner it names is
theirs.

**Verify**: `go test -race -count=1 ./internal/controller/...` passes.

## Done criteria

- [ ] The new tests pass; the cross-installation ones failed before Step 3.
- [ ] `go test -race -count=1 ./internal/controller/... ./internal/store/...` passes.
- [ ] `gofmt -l internal/controller` and `go vet ./internal/controller/...` clean.
- [ ] `make test` exits 0.
- [ ] Commit message: `Act on a webhook's runner only inside its own installation`.

## STOP conditions

- An existing test delivers a job whose `InstallationID` is empty and
  expects the named runner to be released (a delivery verified by the
  fallback secret at `webhooks.go:314-329`). Report it: the owner decides
  whether an unattributed delivery may touch a runner.
- A runner can legitimately serve a repository of another installation
  (read `docs/hosts-and-pools.md` for shared pools before concluding).

## Maintenance notes

- Deliberately deferred: `ApplyJob` merges by GitHub job id alone
  (`internal/store/queries_events.go:172`), so a delivery from one
  installation can still mark another's queued job completed by reusing
  its id. Refusing that merge needs the owner's view on repository
  transfers between installations.

## What was done

The rule is not strict equality, which the plan's Step 3 sketched. A repository
can be covered by an installation of its own and by its organisation's;
`FindInstallationByTarget` prefers the repository's, and GitHub may still hand
the job to a runner of the organisation's installation. Strict equality would
have unlinked that runner, and `docs/hosts-and-pools.md` ("A pool belongs to one
installation") describes pools of both as legitimate. The runner is therefore
linked when its pool's installation is the job's installation (no lookup), or
when that installation's target covers the job's repository (one
`GetInstallation`, only in the second case). A job no installation covers, and a
runner whose pool is gone, link nothing.

Tests: `TestADeliveryCannotMoveAnotherInstallationsRunner` (for `in_progress`
and `completed`; before the change a globex delivery made acme's runner busy and
removed it) and `TestARunnerOfAnInstallationThatCoversTheRepositoryStaysLinked`
(the overlap, which passes before and after and guards against over-blocking).
The whole `internal/controller` package passes.
