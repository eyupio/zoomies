# Plan 017: Stage a restore's copy before moving the live database aside

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first. Follow this plan step by
> step, run every verification, and stop on any STOP condition there or
> here. Update this plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/backup/restore.go internal/backup/backup_test.go cmd/zoomies/controller.go
```

## Status

- **Priority**: P0
- **Effort**: M
- **Risk**: MEDIUM (restore path)
- **Depends on**: none
- **Category**: data loss
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

`Restore` renames the live database aside and then copies the backup into
its place. If the copy fails, `copyFile` removes the partial file and the
error is returned bare. No database is left at the path. A staged restore
from the Backups tab then logs the error and the controller carries on to
open the store, which creates a new, empty database: the fleet comes up
with no pools, hosts or users. A full disk is the likely trigger, because
the file moved aside still takes its space.

## Current state

- `internal/backup/restore.go:210-257`, in `Restore`: `setAsideLiveDatabase(live, stamp)`
  (which also moves `-wal` and `-shm`) runs first, then:

```go
	if err := copyFile(backupDB, live); err != nil {
		return nil, err
	}
	if err := invalidate(ctx, live, dir, m, opts, report); err != nil {
		return nil, restoreUnfinished(err, live, report)
	}
```

- `internal/backup/restore.go:434-457`, `copyFile`, opens the destination
  with `O_CREATE|O_EXCL`, copies, and removes the destination on failure.
  It does not sync.
- `internal/backup/restore.go:627-660`, `ApplyStaged`, records
  `err.Error()` as the outcome.
- `cmd/zoomies/controller.go:278-283` logs a failed staged restore and
  returns nil, so the controller starts.
- `TestARestoreThatFailsAfterTheCopySaysSoAndKeepsTheFenceUp`
  (`backup_test.go` near 426) shows how a failure is injected.

## Scope

**In scope**: `internal/backup/restore.go`, `internal/backup/backup_test.go`.

**Out of scope**: `cmd/zoomies/controller.go`, `invalidate`, the orphaned
log handling's meaning, the staging file format.

## Steps

### Step 1: Write the failing test

Add `TestARestoreWhoseCopyFailsLeavesTheLiveDatabaseWhereItWas`. Make a
live database with known content and a backup, then make the copy fail.
Follow the injection style the existing failure test uses; if there is no
seam for the copy, add an unexported package variable for the copy
function that the test swaps and restores with `t.Cleanup`. Assert: the
restore returns an error; the file at the live path exists and is
byte-identical to before; no partial or temporary file is left beside it.

**Verify**: it fails before the fix (the live path is empty).

### Step 2: Copy first, then rename

Reorder `Restore` so that, after the pre-flight checks and before anything
is moved aside:

1. The backup is copied to a temporary file in the same directory as the
   live path (a name such as `<live>.restoring-<stamp>`), and synced
   (`File.Sync`) before it is closed. On failure remove it and return the
   error; nothing has moved.
2. Only then run the existing switch that moves the live database, or the
   orphaned logs, aside.
3. Rename the temporary file to the live path.
4. If that rename fails, move the set-aside database (and its `-wal` and
   `-shm`, the same files `setAsideLiveDatabase` moved) back, remove the
   temporary file, and return an error. If moving back also fails, the
   error must name the path the original is now at.

Keep `copyFile`'s owner-only mode. Comment the order with the failure it
designs out: a copy that fails after the original has moved leaves nothing
for the controller to start on but an empty database.

**Verify**: the new test and every existing test in the package pass.

### Step 3: Cover the rename failure

Add a second test for step 4 if the code has a seam for it (a swapped
rename variable is acceptable, with the same clean-up). Assert the
original is back in place.

## Done criteria

- [ ] The new tests pass; the first failed before Step 2.
- [ ] `go test -race -count=1 ./internal/backup/... ./cmd/...` passes.
- [ ] `gofmt -l internal/backup` and `go vet ./internal/backup/...` clean.
- [ ] `make test` exits 0.
- [ ] If `docs/` describes the restore order (grep `before-restore`), the
      sentence is updated.
- [ ] Commit message: `Stage a restore's copy before moving the live database aside`.

## STOP conditions

- The temporary file cannot be in the same directory as the live path for
  a reason the code states (the rename would then cross filesystems).
- An existing test asserts the old order.

## Maintenance notes

- The restore now needs room for the backup while the original is still in
  place, which is the same room it needed before, sooner. A restore that
  would have failed half way now fails before anything moves.
