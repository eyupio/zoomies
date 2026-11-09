# Plan 011: Let the state directory look after itself: a storage report, pruning and compaction

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report; do not improvise. The three parts are independent and shippable alone,
> in the order given; land each as its own pull request. When done, update the
> status row for this plan in `plans/README.md` unless a reviewer dispatched you
> and told you they maintain the index.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 1da1792..HEAD -- internal/store/store.go internal/store/backup.go internal/config/settings.go internal/config/config.go internal/controller/backups.go internal/backup/backup.go internal/api/handlers_backups.go api/openapi.yaml
```

> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: L (three parts, M each)
- **Risk**: MED (it changes the single writer's connection settings and adds a
  one-time rewrite of the database file; both are guarded and opt-in as below)
- **Depends on**: none
- **Category**: operations
- **Planned at**: commit `1da1792`, 2026-11-09

## Why this matters

A controller's state directory grows in two ways an operator is never shown.

1. **It keeps copies of its own database beside it.** Backups go to a `backups`
   directory next to `zoomies.db`, on the same volume, nightly, newest seven
   kept; the store keeps two more full copies in `pre-migration` before any
   migration; and every backup somebody asks for through the API lands in the
   same place. The directory can therefore hold about ten database-sized files,
   and nothing says which of them the controller needs and which it could make
   again.
2. **The database file never shrinks.** Retention deletes rows, but only
   backups use `VACUUM INTO`; the live file has no vacuum of any kind, so it
   stays at its high-water mark however much is pruned.

Both show when an operator moves a deployment to another machine (copying the
state directory carries every copy), sizes a disk, or wonders why a volume is
full after retention was lowered. This plan makes the directory say what is in
it, prune its own copies on a schedule, and give back the space retention
frees, each behind a setting.

## Current state

- `internal/store/store.go:212-222`: the connection opens with `journal_mode(WAL)`,
  `journal_size_limit(16 MiB)` and `mmap_size`. No `auto_vacuum`, and nothing in
  `internal/` runs `VACUUM` on the live file (`grep -rn "VACUUM" internal/` finds
  only `VACUUM INTO`, in `internal/store/backup.go:78`).
- `internal/store/backup.go:37`: `preMigrationKeep = 2`, a constant.
  `backupBeforeMigrate` (`:160-190`) writes the copy with `s.Backup` and prunes
  to the constant. It runs inside `Open`, before settings can be read, so the
  constant is the only number it can prune to. That is why Part 2 moves the
  pruning out of `Open`: a later pass can lower a count but cannot bring back a
  copy `Open` has already deleted.
- `internal/config/settings.go:939-951`: `backup.directory`, `backup.interval`
  and `backup.keep`; defaults `Interval: 24h, Keep: 7` at
  `internal/config/config.go:1351`; retention defaults at `:1308-1313`.
- `internal/backup/backup.go:652`: `Prune(root, keep)` removes the oldest
  scheduled, manual and CLI copies and never an uploaded or fetched one, for
  the reason in its comment: somebody put those there on purpose.
- `internal/controller/backups.go:298-317`: `backupLoop`, its own loop because
  `VACUUM INTO` on a large database takes as long as it takes. The pattern for a
  new loop that must not delay housekeeping.
- `internal/api/router.go:656-680`: every `/backups` route is gated by
  `auth.ActionBackupsRead` or `ActionBackupsWrite`, both mapped to the platform
  role (`internal/auth/rbac.go:423-424`). `POST /api/v1/backups/prune` already
  runs retention on demand.
- `api/openapi.yaml:7902` onwards says `x-zoomies-role: admin` for those routes.
  The code and `docs/api-surface.md:571` say platform. The contract is wrong, not
  the code (reading a backup is reading the whole database); fix it in Part 1.
- `docs/backup-and-restore.md:38-44` says where backups go and how many are kept.
- `internal/store/AGENTS.md`: one writer behind a mutex. Compaction must go
  through that connection, never a second one.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Placeholder for the embedded UI | `make build-nogui` | exit 0 |
| Focused tests | `go test -race -count=1 ./internal/store/ ./internal/backup/ ./internal/controller/ ./internal/api/ ./internal/config/ ./internal/docs/` | `ok` for each |
| Regenerate the contract | `make openapi` | exit 0; `git diff --stat` shows both copies of `openapi.yaml` and nothing else unexpected |
| Lint | `make lint` | exit 0 |
| Store benchmarks, before and after Part 3 | `make bench BENCH_PKGS=./internal/store` on an idle machine, `tee`d to a file each time | a `benchstat` comparison for the commit message |

Read `.github/AGENTS.md` before the first push: it lists the generated files CI
diffs against their sources, and the command reference for the CLI is one of them.

## Scope

In scope: `internal/backup` (the report), `internal/store` (pre-migration keep,
compaction), `internal/config` (the settings), `internal/controller` (the
maintenance loop), `internal/api` and `api/openapi.yaml` (one route), `cmd/zoomies`
(one command), the catalog and docs rows each of those needs.

Out of scope, deliberately:

- Compressing backups, or anything in flight. Compaction here means giving back
  the live file's dead space.
- The restore path and the backup remotes.
- The MCP tools. A tool for the report can follow once it has proved useful.
- Audit rows, which are never pruned. This plan does not change that.

## Part 1: a storage report

**Behaviour.** `GET /api/v1/storage` and `zoomies storage` say what is in the
state directory. Each top-level entry has a path relative to the directory, its
size in bytes, and a class:

- `essential`: the controller cannot start without it (the database, its `-wal`
  and `-shm` files when present, and the encryption key when it lives here). The
  WAL files carry `optional: true`, because a clean stop leaves none.
- `kept`: an uploaded or fetched backup, which retention never deletes.
- `regenerable`: a scheduled, manual or CLI backup, or a pre-migration copy.
- `unknown`: anything else. A reader that does not know what it is must treat it
  as essential.

The report also carries `version: 1` (so a reader can refuse a shape it does
not know), `database_bytes`, `database_free_bytes` (`freelist_count` times
`page_size`), `total_bytes`, `disk_free_bytes`, and the result of the last
maintenance pass (Part 2) once there is one. It is gated by `ActionBackupsRead`,
like the backups list.

**Steps.**

1. Add `internal/backup/storage.go` with the report and the classification. The
   classification is a table in code, not a guess from names, so it can be tested.
2. Add the route and handler beside the other backups handlers, the schema to
   `api/openapi.yaml`, and correct `x-zoomies-role` on the `/backups` routes to
   `platform`. Run `make openapi`.
3. Add `zoomies storage` (a table of path, class and size, and the free-space
   lines), and regenerate the CLI reference the way `.github/AGENTS.md` says.
4. Add the route to `docs/api-surface.md` and a short section to
   `docs/backup-and-restore.md`: what is state, what is a copy, and that moving a
   deployment to another machine needs only the `essential` and `kept` entries.

**Tests.**

- Every top-level entry the controller creates is classified. Build a state
  directory by running a real start (open the store on a file, take one backup of
  each source, trigger a pre-migration copy, create the key) and fail on any
  `unknown`. This is the test that keeps the table honest when a later change
  adds a file; its failure message names the file and says to add it to the table.
- An uploaded backup is `kept` and a scheduled one is `regenerable`.
- The WAL entries are `optional`; the database and key are `essential`.
- The route refuses a caller without the platform role.

## Part 2: pruning on a schedule, with settings

**Behaviour.** One maintenance pass, on its own interval, prunes the controller's
own copies to the configured counts and, in Part 3, compacts. Settings (rows in
`internal/config/settings.go`, each with a row in `docs/configuration.md`):

| Key | Kind | Default | Meaning |
|---|---|---|---|
| `backup.pre_migration_keep` | int | 2 | Copies kept in `pre-migration`. Floor 1. |
| `maintenance.interval` | duration | 24h | How often the scheduled pass runs. 0 switches the scheduled pass off, and not the prune of pre-migration copies at start (see the notes). Floor one minute, as `backup.interval`. |

`backup.keep` already exists and the pass applies it with `backup.Prune`. A pass
that deletes anything says what it removed in the controller log and in the
report's last-pass block.

**Notes that matter.**

- `Open` takes the pre-migration copy and prunes nothing. It runs before settings
  can be read, so the only number it could prune to is the compiled 2, and that
  would delete copies an operator's higher `backup.pre_migration_keep` was meant
  to keep: a later pass can only lower a count, never restore a copy that is
  gone. Remove the `pruneDirs` call from `backupBeforeMigrate`, and give the
  store a `PrunePreMigration(dbPath, keep)` that the controller calls. Do not try
  to read a setting inside `Open`.
- The controller prunes pre-migration copies once at start, as soon as the
  effective configuration is loaded, and again in every pass. The prune at start
  does not wait for `maintenance.interval` and is not switched off by `0`:
  otherwise an operator who turned the scheduled pass off would have copies that
  nothing ever removes. If the configuration cannot be loaded, nothing is pruned.
  Keeping too many copies is the safe direction.
- A one-off command that opens the database and migrates it (the upgrade's
  migrate step, `zoomies config import-env`) now leaves its copy and prunes
  nothing, so the directory can briefly hold more than the configured count until
  the controller next starts. The bound is deliberate: a copy taken before a
  migration is the one rollback there is.
- Put the pass in its own loop in `internal/controller`, as `backupLoop` is, so a
  slow pass never delays housekeeping. Nudge it from `POST /backups/prune` so an
  operator who lowers a count sees it take effect.
- The settings page reads the registry, so a new row appears there without UI
  work; check it renders and that `Live: true` is right (it is, the loop reads the
  effective configuration each pass).

**Tests.**

- With `pre_migration_keep: 1` and three copies present, a pass leaves the newest.
- With `backup.keep: 2`, a pass removes older scheduled copies and leaves an
  uploaded one.
- `Open` on a database with pending migrations takes a copy and removes none,
  with three older copies already there. This fails today, because `pruneDirs`
  leaves two.
- With `pre_migration_keep: 5` and four older copies, a migration followed by the
  controller's prune at start leaves all five. With `2` it leaves the newest two.
- `maintenance.interval: 0` runs no scheduled pass, and the prune of pre-migration
  copies at start still happens; a pass that fails on one directory still tries
  the other and reports both.
- A new setting without a `docs/configuration.md` row fails `internal/docs`.

## Part 3: compaction

**Behaviour.** The pass gives back the live file's dead space without a long
pause, using SQLite's incremental auto-vacuum.

| Key | Kind | Default | Meaning |
|---|---|---|---|
| `maintenance.compact` | enum `off`, `incremental`, `convert` | `incremental` | `off`: never. `incremental`: reclaim free pages from a database already in incremental mode. `convert`: also convert an older database to incremental mode, once. |
| `maintenance.compact_free_percent` | int | 20 | Act only when free pages exceed this share of the file (and at least 32 MiB, a constant, so a small file is left alone). |

- **New databases** are created in incremental mode (`auto_vacuum` is set before
  the first table exists, which is why it is the DSN's job for an empty file and
  only then). Existing databases stay as they are until `convert` is set.
- **A slice** is `PRAGMA incremental_vacuum(N)` through the writer connection
  (`wmu`), bounded to about 8 MiB of pages, with the lock released and the
  context checked between slices, ending when the free share is under the
  threshold or a time budget (30 seconds) is spent. Then a `wal_checkpoint`, so
  the file really shortens. Writes wait only for one slice at a time.
- **The conversion** is `PRAGMA auto_vacuum = INCREMENTAL` then `VACUUM`, once. It
  holds the writer for as long as the rewrite takes, so everything that writes
  waits (agent heartbeats, webhook ingest). It is therefore opt-in, and refused
  with a warning-level finding `maintenance.compact_skipped`, naming the reason,
  unless **both** hold: free disk is at least twice the database's size, and a
  backup of the database no older than a day exists in the backup directory (or
  the operator has switched backups off, which the finding says). A failed
  `VACUUM` leaves the original file intact; say so in the finding.
- Add `maintenance.compact_skipped` to `internal/catalog` and a row to
  `docs/problem-codes.md`; `internal/docs` tests both directions.

**Tests.**

- Fill a file-backed store with a few tens of megabytes, delete most rows, run a
  pass, and assert the file is smaller afterwards. This is the test that proves
  the mechanism; write it first and watch it fail.
- A database created fresh is in incremental mode; an older one is not until
  `convert` is set, and `incremental` mode leaves it alone.
- `convert` with too little disk, or no recent backup, converts nothing and raises
  the finding.
- A write issued while a slice runs completes within one slice's time, not after
  the whole pass (use the store's write observer).
- A backup taken by `VACUUM INTO` and restored keeps working. **Verify first
  whether a `VACUUM INTO` copy keeps the incremental mode.** If it does not, a
  restored database silently loses it; the next pass under `convert` puts it back,
  and the restore documentation says so. If that behaviour is surprising in
  practice, stop (see below).
- Run the store benchmarks before and after, and put the `benchstat` comparison in
  the commit message. This change is not made for speed, so the number to show is
  that the hot write paths did not get slower.

## STOP conditions

- `incremental_vacuum` does not shorten the file under WAL in the test above, even
  after a checkpoint. Report what was tried; do not fall back to a scheduled full
  `VACUUM` without the owner's say.
- A `VACUUM INTO` copy loses the incremental mode and the restore path cannot put
  it back without changing restore behaviour.
- The classification test finds an `unknown` entry that nobody can explain.
- Any change would add a second writer or read a setting inside `Open`.
- A path is found that migrates a database and never starts a controller, so
  copies would grow with nothing to prune them. Report it; do not put the compiled
  2 back into `Open`.
- A docs test demands a row not listed here for a reason you cannot see.

## Rules this plan keeps

- The store stays the only place SQL is written, and the one writer stays one.
- Prose is British, comments say why, there are no em dashes and no spaced `--`,
  commit messages are imperative sentences with no prefix, and each part's tests
  are seen to fail with the change removed.
- Nothing here names or assumes how a deployment is hosted. The report is for an
  operator moving, sizing or tidying their own controller.
