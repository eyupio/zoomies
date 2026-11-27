# Plan 028: Reuse the pre-migration copy when a migration fails again

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) and `internal/store/AGENTS.md`
> first. Follow this plan step by step, run every verification, and stop on
> any STOP condition there or here. Update this plan's row in
> `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/store/store.go internal/store/backup.go
```

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: MEDIUM (the copy is the safety net for a migration)
- **Depends on**: none
- **Category**: bug, disk exhaustion
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

Whenever a migration is pending, `Open` takes a full copy of the database
before applying it. Copies are pruned only after the controller's
configuration is loaded, which needs a successful start. If a migration
fails, the service unit restarts every five seconds
(`internal/installer/templates/zoomies.service.tmpl:33-34`), and every
start adds another full copy and fails again. A 700 MB database fills the
disk it shares with runner images in minutes, which makes the migration
less likely ever to succeed. Two starts in the same second also fail on
the copy's name before the migration is tried. Reproduced by the audit.

## Current state

- `internal/store/store.go:328` calls `backupIfMigrationsPending` before
  `migrate`; `store.go:461-500` reads the applied names from
  `schema_migrations`, works out what is pending and calls
  `backupBeforeMigrate(ctx, pending)`.
- `internal/store/backup.go:166-186`:

```go
	root := filepath.Join(filepath.Dir(abs), PreMigrationDir)
	dest := filepath.Join(root, BackupDirPrefix+s.Now().Format("20060102-150405"))
	if err := s.Backup(ctx, filepath.Join(dest, BackupDBName)); err != nil {
		return "", err
	}
	// Nothing is pruned here. This runs inside Open, before any setting can be
	// read, ...
	return dest, nil
```

- `NewestPreMigrationName` (just below) returns the newest copy's
  directory name; `PrunePreMigration` prunes later.
- A copy is a complete SQLite database (`VACUUM INTO`), so it carries its
  own `schema_migrations` table.

## The rule to implement

Before copying, look at the newest existing pre-migration copy. If its
`schema_migrations` holds exactly the names the live database holds now,
the copy was taken at the same point and nothing has been applied since:
reuse it and take no new copy. Otherwise copy as today.

## Scope

**In scope**: `internal/store/backup.go`, `internal/store/store.go` only
to pass the applied names along, a test file in `internal/store`.

**Out of scope**: `PrunePreMigration`, the copy's name format, the
installer's unit template, `internal/backup`.

## Steps

### Step 1: Write the failing tests

In `internal/store`, following `migrations_upgrade_test.go` or whichever
test opens a file-backed store with pending migrations:

- `TestAMigrationThatFailsAgainDoesNotCopyTheDatabaseAgain`: a database
  on disk with a pending migration that fails (the audit pre-created a
  conflicting table so a later migration's `CREATE TABLE` errors). Open
  it three times with an injected clock that advances between opens.
  Assert each open fails and exactly one copy exists.
- `TestALaterMigrationStillGetsItsOwnCopy`: after one copy exists at an
  older set of applied names, an open at a different applied set takes a
  second copy.
- A copy that cannot be opened or read is not reused: a fresh one is
  taken.

**Verify**: the first test fails before the fix, with three copies.

### Step 2: Compare the newest copy with the live database

1. In `backupBeforeMigrate`, before building `dest`, find the newest copy
   (`NewestPreMigrationName` and the same path pieces). Open its database
   file read-only and read the names in its `schema_migrations`; compare
   with the applied names the caller already read. Equal sets: return the
   existing copy's path and nil.
2. Any error reading the copy means "not reusable": fall through to the
   normal copy. Never fail `Open` because an old copy is unreadable.
3. Keep SQL in `internal/store`; open the copy with the same driver and a
   read-only, immutable DSN in the style the package already uses for
   opening another file (look at how `Backup` or the compaction code
   opens a second database).
4. Comment why: a start that fails its migration is restarted within
   seconds, and a copy per start fills the disk the migration needs.

**Verify**: `go test -race -count=1 ./internal/store/...` passes.

### Step 3: Check the caller's log line

`backupIfMigrationsPending` logs or records the copy it took. Make the
message true when a copy was reused (say that the existing copy from the
earlier attempt is being kept).

## Done criteria

- [ ] The new tests pass; the first failed before Step 2.
- [ ] `go test -race -count=1 ./internal/store/... ./internal/installer/... ./internal/backup/...` passes.
- [ ] `gofmt -l internal/store` and `go vet ./internal/store/...` clean.
- [ ] `make test` exits 0.
- [ ] If `docs/` describes the pre-migration copy (grep `pre-migration`),
      it says a failed migration reuses its copy.
- [ ] Commit message: `Reuse the pre-migration copy when a migration fails again`.

## STOP conditions

- The copy does not carry `schema_migrations` (the first open of a brand
  new database, or a copy format other than a full database).
- Rows other than migrations could have changed between two failed starts
  in a way that matters for the copy's purpose. The controller does not
  serve until `Open` returns, so nothing should write; if you find a
  writer before the migration step, stop.

## What was done

`reusablePreMigrationCopy` reads the newest copy's `schema_migrations` through a
read-only, immutable connection (so looking creates no file and takes no lock)
and reuses it only when its ledger is exactly the live one's; a copy that cannot
be read, or differs, means a new copy, which is what the existing test that plants
empty directories as older copies relies on. A migration that fails after an earlier
one has applied moves the ledger, so the next start takes one copy of where the
database now is and the start after that reuses it: the second test pins that
(`[1 2 2]`). The first (`[1 1 1]` copies after three starts) failed before with
three. Known limit: the check compares ledgers, not rows, so a write made to the
database between two failed starts (a CLI command, say) is not in the reused copy;
nothing writes while the controller cannot start, and the copy exists to get back
past a schema change.
