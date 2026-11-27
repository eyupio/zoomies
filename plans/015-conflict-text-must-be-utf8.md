# Plan 015: Treat a conflicted file that is not UTF-8 as unsupported

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first, including the section
> on Eli repair plans: this change must update
> `roadmap/eli-repair-implementation-handoff.md`. Follow this plan step by
> step, run every verification, and stop on any STOP condition there or
> here. Update this plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/gitmerge/merge.go internal/gitmerge/merge_test.go
```

## Status

- **Priority**: P0
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug, data loss
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

A conflicted side counts as text when it holds no NUL byte. The text then
crosses to the controller as JSON strings, and `encoding/json` silently
replaces bytes that are not valid UTF-8 with U+FFFD. Every later check
passes, because U+FFFD is valid UTF-8. A Latin-1, Windows-1252 or Shift-JIS
file comes back with every non-ASCII character replaced, including on lines
nobody touched, and that is what gets committed. Reproduced by the audit.

## Current state

- `internal/gitmerge/merge.go:400-411`, the end of `side`:

```go
	content, err := run(ctx, in.Dir, "cat-file", "blob", object)
	if err != nil {
		return blobSide{}, fmt.Sprintf("read blob: %v", err)
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return blobSide{Mode: mode, Binary: true}, ""
	}
	*total += len(content)
	return blobSide{Mode: mode, Text: string(content)}, ""
```

- `side` returns a `blobSide` and a reason string; a non-empty reason marks
  the conflict as one the worker cannot hand on (the budget check just
  above returns `"conflict text exceeds the merge worker's budget"`).
- `internal/gitmerge/merge_test.go` near line 90 covers the NUL case.

## Scope

**In scope**: `internal/gitmerge/merge.go`, `internal/gitmerge/merge_test.go`.

**Out of scope**: the JSON encoding in `worker.go`, `internal/prrepair`,
the controller.

## Steps

### Step 1: Read how the two outcomes are consumed

Read `side`'s callers in `merge.go` and note what happens to a side with
`Binary: true` and to a non-empty reason. Choose the outcome that makes the
conflict unsupported without the content crossing as a string. The
expected choice is a reason, `"conflict text is not UTF-8"`, in the same
shape as the budget reason. If only `Binary: true` leads to "unsupported",
use that and say so in the commit body.

### Step 2: Write the failing test

Beside the NUL test add
`TestAConflictInAFileThatIsNotUTF8IsNotHandedOn`: build the same kind of
repository the neighbouring test builds, with a conflicting file whose
content includes the Latin-1 byte `0xE9` (`"caf\xe9\n"`) on both sides.
Assert the conflict is reported as unsupported with the reason from Step 1
and that no side's `Text` contains U+FFFD.

**Verify**: it fails before the fix.

### Step 3: Add the check

After the NUL check, add `if !utf8.Valid(content)` returning the chosen
outcome. Comment why: the text is carried as JSON, which replaces what it
cannot encode, so a file in another encoding would be rewritten wholesale.

**Verify**: `go test -race -count=1 ./internal/gitmerge/...` passes.

### Step 4: Update the Eli handoff

Add the dated section and ledger row described in the shared rules.

## Done criteria

- [ ] The new test passes and failed before Step 3.
- [ ] `go test -race -count=1 ./internal/gitmerge/... ./internal/controller/... ./internal/docs/...` passes.
- [ ] `gofmt -l internal/gitmerge` and `go vet ./internal/gitmerge/...` clean.
- [ ] `make test` exits 0.
- [ ] The handoff file has the new section and ledger row.
- [ ] Commit message: `Treat a conflicted file that is not UTF-8 as unsupported`.

## STOP conditions

- A controller test expects a non-UTF-8 conflict to reach the model.

## Maintenance notes

- Clean-merged files are not affected: `Change.Content` is `[]byte`.
