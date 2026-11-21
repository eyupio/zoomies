# Plan 024: Bound how often a conflict repair's publication is retried

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first, including the section
> on Eli repair plans: this change must update
> `roadmap/eli-repair-implementation-handoff.md`. Follow this plan step by
> step, run every verification, and stop on any STOP condition there or
> here. Update this plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/controller/eli_conflict_repair.go internal/controller/eli_repair_watch_loop.go
```

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW-MEDIUM
- **Depends on**: none
- **Category**: bug, cost
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

When publishing a conflict resolution fails for any reason other than the
pull request having moved, the claim is released. The next observation, a
few minutes later, starts again: three archive downloads, a worker
container and a full model session. Nothing counts attempts. A pull
request whose publication can never succeed (a protected branch, a
permission the App lacks) bills a model session every few minutes until
somebody reads the logs.

## Current state

- `internal/controller/eli_conflict_repair.go:319-330`:

```go
	sha, err := writer.CommitConflictResolution(ctx, job.Repo, pull, job.BaseSHA, changes)
	if err != nil {
		if errors.Is(err, github.ErrSetupConflict) {
			c.log.Info("the pull request moved while the conflict was being resolved", ...)
			c.releaseConflict(key)
			return
		}
		c.log.Warn("could not publish a conflict resolution", ...)
		c.releaseConflict(key)
		return
	}
	c.releaseConflict(key)
```

- Lines 24-34: `conflictClaimWindow = 15 * time.Minute` and
  `conflictPermanentCooldown = time.Hour` ("a conflict no model may
  resolve ... is not retried every couple of minutes: it needs a person").
- Lines 61-99: `claimConflict`, `releaseConflict` and `holdConflict(key,
  for_)` work on `c.conflictClaims`, a map guarded by `c.conflictMu`;
  `claimConflict` sweeps expired entries.
- `key` identifies the incident (repository, pull request, head, base and
  merge base). A new push changes the key.
- `TestAConflictPublishFailureIsLetGoForTheNextObservation` asserts the
  release after one generic failure. The retry is deliberate; the missing
  bound is the defect.

## The rule to implement

A publish failure that is not `ErrSetupConflict` counts against the
incident's key. The first two release the claim as today. The third holds
the claim for `conflictPermanentCooldown` and logs that a person is
needed. A successful publication, or an expired hold, forgets the count.
The count lives in memory beside the claims; a restart forgets it, which
is recorded as a known limit.

## Scope

**In scope**: `internal/controller/eli_conflict_repair.go`, the file that
declares the controller's `conflictClaims` field, the conflict repair
test file, the Eli handoff file.

**Out of scope**: the store (no migration), the check-repair chain, the
watch loop.

## Steps

### Step 1: Write the failing tests

Beside the existing publish-failure test add:

- `TestAConflictPublicationThatKeepsFailingIsHeldForAPerson`: the fake
  writer fails with a generic error three times for the same key. After
  the first and second the key can be claimed again; after the third
  `claimConflict` refuses until the injected clock passes
  `conflictPermanentCooldown`.
- `TestAPublishedConflictForgetsEarlierFailures`: two failures, one
  success, then two more failures still leave the key claimable.
- A row showing `ErrSetupConflict` does not count.

**Verify**: the first test fails before the fix.

### Step 2: Count and hold

1. Add `const conflictPublishAttempts = 3` with a comment giving the
   reason (the check-repair chain allows three; a publication that has
   failed three times for the same head and base needs a person).
2. Add a `conflictPublishFailures map[string]int` field beside
   `conflictClaims`, guarded by `conflictMu`.
3. Add one small method that, under the lock, increments the count for a
   key and either deletes the claim (count below the limit) or sets the
   hold to now plus `conflictPermanentCooldown` and deletes the count
   (limit reached). Call it from the generic failure branch in place of
   `releaseConflict`. Log at warn level when it holds, naming the watch,
   repository, pull request, the error and that Eli will not try again
   for an hour unless the pull request changes.
4. On success, and in `claimConflict`'s sweep of expired entries, delete
   the key's count so the map cannot grow.

**Verify**: `go test -race -count=1 ./internal/controller/...` passes,
including the existing release test unchanged.

### Step 3: Update the Eli handoff

Add the dated section and ledger row described in the shared rules, and
state the known limit: the count is in memory, so a restart starts it
again.

## Done criteria

- [ ] The new tests pass; the first failed before Step 2.
- [ ] `TestAConflictPublishFailureIsLetGoForTheNextObservation` passes unchanged.
- [ ] `go test -race -count=1 ./internal/controller/... ./internal/docs/...` passes.
- [ ] `gofmt -l internal/controller` and `go vet ./internal/controller/...` clean.
- [ ] `make test` exits 0.
- [ ] The handoff file has the new section and ledger row.
- [ ] Commit message: `Bound how often a conflict repair's publication is retried`.

## STOP conditions

- The key does not include the head and base, so a new push would not
  reset the count.
