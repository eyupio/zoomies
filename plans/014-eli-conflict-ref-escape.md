# Plan 014: Escape the branch in Eli's conflict ref update

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first, including the section
> on Eli repair plans: this change must update
> `roadmap/eli-repair-implementation-handoff.md`. Follow this plan step by
> step, run every verification, and stop on any STOP condition there or
> here. Update this plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/github/eli_conflict_commit.go internal/github/eli_repairs.go
```

## Status

- **Priority**: P0
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug, security
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

After resolving a conflict, Eli moves the pull request's branch with a
PATCH whose URL has the branch name pasted in. The string is parsed as a
URL, so for a branch named `main#42-fix` everything from `#` on becomes a
fragment and the request goes to `refs/heads/main`. The new commit's second
parent is the tip of the base branch, so the non-force update is a
fast-forward: Eli pushes the merge to the target branch. `feature/#123`
instead fails every time, which plan 024 then retries.

## Current state

- `internal/github/eli_conflict_commit.go:131`:

```go
	if err := c.repairRequest(ctx, http.MethodPatch, base+"/git/refs/heads/"+p.Branch, map[string]any{
		"sha":   commit.SHA,
		"force": false,
	}, nil); err != nil {
```

- `internal/github/eli_repairs.go:129-139`, `repairRequest`, hands the
  string to `c.asInstallation.NewRequest`, which resolves it against the
  client's base URL.
- `internal/github/eli_repairs.go:140-146`, `repairRepo`, already escapes
  owner and name with `url.PathEscape`.
- The check-repair path near `eli_repairs.go:572` updates a ref through the
  client library's own ref call and is not affected.

## Scope

**In scope**: `internal/github/eli_conflict_commit.go`, its test file, and
any other `internal/github/eli_*.go` line found by Step 3.

**Out of scope**: `repairRequest`, the controller, the merge worker.

## Steps

### Step 1: Write the failing test

In `internal/github/eli_conflict_commit_test.go`, following the HTTP test
server pattern already there, add
`TestAConflictCommitMovesTheBranchItWasGivenWhateverItsName`, table-driven
over branches `feature`, `main#42-fix`, `feature/#123`, `what?now` and
`100%done`. Record the PATCH request's escaped path (`r.URL.EscapedPath()`
or `r.RequestURI`) and assert it ends with `/git/refs/heads/` followed by
the branch with each segment escaped and `/` kept as a separator.

**Verify**: the rows with `#`, `?` and `%` fail before the fix.

### Step 2: Escape each segment

Add a small unexported helper in the same file that splits the branch on
`/`, applies `url.PathEscape` to each part and joins with `/`, and use it
at line 131. Comment why: a branch name is somebody else's text, and `#`
or `?` in it would otherwise send the update to another ref.

**Verify**: the test passes.

### Step 3: Look for the same defect nearby

```
grep -n 'refs/heads/\|"/git/\|"/branches/\|"/contents/' internal/github/eli_*.go
```

For each hit that concatenates a branch, ref or path from outside into a
URL without escaping, apply the same helper and add a row or a test. List
what you found in the commit body.

### Step 4: Update the Eli handoff

Add the dated section and ledger row described in the shared rules.

## Done criteria

- [ ] The new test passes and failed before Step 2.
- [ ] `go test -race -count=1 ./internal/github/... ./internal/docs/...` passes.
- [ ] `gofmt -l internal/github` and `go vet ./internal/github/...` clean.
- [ ] `make test` exits 0.
- [ ] The handoff file has the new section and ledger row.
- [ ] Commit message: `Escape the branch in Eli's conflict ref update`.

## STOP conditions

- The test server shows the path already arrives escaped for
  `main#42-fix` before any change: the client library's behaviour differs
  from what the audit assumed, and the finding needs re-checking.
