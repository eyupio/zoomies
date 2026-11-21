# Plan 018: Roll an upgrade back only to the binary that upgrade kept

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first. Follow this plan step by
> step, run every verification, and stop on any STOP condition there or
> here. Update this plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/installer/upgradewait.go internal/installer/selfupdate.go internal/installer/upgrade.go internal/installer/upgrade_rollback_test.go cmd/zoomies
```

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MEDIUM (upgrade path)
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

When the service does not answer after an upgrade, the installer puts back
`<binary>.previous`. It checks only that the file exists. That file is
written by `Candidate.Install`, so after an upgrade that did not go through
it (`install.sh --upgrade`, `zoomies upgrade --no-download`, or "already on
the latest build") the file is left over from an older upgrade. A slow
start then swaps in a binary from two releases ago, which cannot read the
newer schema, under a message saying the previous release was put back.

## Current state

- `internal/installer/upgradewait.go:307-314`:

```go
func (p *upgradePlan) canRollback() bool {
	if p.record.Deployment.Containerised() || p.opts.BinaryPath == "" {
		return false
	}
	_, err := os.Stat(p.opts.BinaryPath + PreviousSuffix)
	return err == nil
}
```

- `internal/installer/selfupdate.go:265-286`, `Candidate.Install`, links or
  copies the installed binary to `.previous` and renames the candidate into
  place. It is called at `selfupdate.go:137`.
- `internal/installer/selfupdate.go:30-35`: `SelfUpdateEnv`
  (`ZOOMIES_NO_SELF_UPDATE`) is set on the re-executed process so it does
  not fetch again; it is an environment variable so an older release
  ignores it. The re-exec itself is `cmd/zoomies/reexec_unix.go:15`
  (`syscall.Exec(path, args, env)`).
- `internal/installer/upgrade_rollback_test.go:28` plants `.previous`
  unconditionally in every test.
- `backup.FileSHA256` is already used by `selfupdate.go`.

## The design to implement

A second environment variable, next to `SelfUpdateEnv`, carries the SHA-256
of the `.previous` file this run wrote across the re-exec. `canRollback`
requires the variable to be set and to equal the SHA-256 of the file now at
`BinaryPath + PreviousSuffix`. A run that swapped nothing sets nothing, so
a stale file is never used.

## Scope

**In scope**: `internal/installer/selfupdate.go`,
`internal/installer/upgradewait.go`, the file in `cmd/zoomies` that builds
the re-exec environment, `internal/installer/upgrade_rollback_test.go`,
`internal/installer/selfupdate_test.go`.

**Out of scope**: `install.sh`, `restorePreviousBinary`, the container
upgrade paths, the update helper.

## Steps

### Step 1: Find where the re-exec environment is built

Grep for `SelfUpdateEnv` across `cmd/` and `internal/installer`. Find the
place that appends it to the environment before the re-exec, and how
`upgradePlan` learns its options. Note both for Step 3.

### Step 2: Write the failing tests

In `upgrade_rollback_test.go`:

- `TestAStaleKeptBinaryIsNotRolledBackTo`: plant `.previous` but leave the
  new variable unset (use `t.Setenv` with an empty value). Assert
  `canRollback` is false and that the failure the plan returns does not
  claim the previous release was put back.
- Adjust the existing rollback tests to set the variable to the planted
  file's SHA-256, so they still exercise a rollback.
- A row where the variable is set to another digest: no rollback.

**Verify**: the first test fails before the fix.

### Step 3: Carry the marker

1. Add `const KeptPreviousEnv = "ZOOMIES_UPGRADE_KEPT_PREVIOUS"` beside
   `SelfUpdateEnv`, with a comment in the same voice: what it carries and
   why an environment variable (an older release ignores it).
2. After a successful `cand.Install(opts)`, compute the SHA-256 of
   `opts.BinaryPath + PreviousSuffix` and make it available to the code
   from Step 1 so the re-executed process receives the variable. If the
   digest cannot be computed, do not set it; say so in a comment (no
   marker means no rollback, which is the safe side).
3. In `canRollback`, after the existing checks, read the variable, hash
   the file, and return true only when both are non-empty and equal.
   Rewrite the doc comment to say the kept binary must be the one this
   upgrade wrote.

**Verify**: `go test -race -count=1 ./internal/installer/...` passes.

### Step 4: Check the message on the no-rollback path

Read `waitServing` (`upgradewait.go:216-228`) and confirm that when
`canRollback` is false the error tells the operator the service did not
answer and that nothing was put back. If it reads as if a rollback
happened, fix the wording.

## Done criteria

- [ ] The new tests pass; the stale-file test failed before Step 3.
- [ ] `go test -race -count=1 ./internal/installer/... ./cmd/...` passes.
- [ ] `gofmt -l internal/installer cmd` and `go vet` clean.
- [ ] `make test` exits 0.
- [ ] If `docs/upgrading.md` describes the rollback (grep `.previous`), it
      says the kept binary is used only by the upgrade that wrote it.
- [ ] Commit message: `Roll an upgrade back only to the binary that upgrade kept`.

## STOP conditions

- The upgrade does not re-execute on some supported platform (read the
  Windows counterpart of `reexec_unix.go`): the marker then needs another
  carrier and the owner decides which.
- The rollback is reached from a process that never passed through
  `Install` in the same invocation chain by design (for example the update
  helper calling it directly); read `updatehelper.go` callers before
  concluding.
