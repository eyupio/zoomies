# Plan 025: Keep a space before the comment when collapsing a runs-on list

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first. Follow this plan step by
> step, run every verification, and stop on any STOP condition there or
> here. Update this plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/migrate/runson.go internal/migrate/runson_test.go
```

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

The migration tool rewrites `runs-on` lines in workflow files. For a block
list whose key line carries a comment, the collapse writes the label and
then the comment with no space between them:

```yaml
    runs-on: # the big one
      - ubuntu-latest
```

becomes `runs-on: zoomies-linux-x64# the big one`. In YAML a `#` with no
space before it is part of the scalar, so the job asks for a label that
does not exist and waits for ever. The diff looks almost right in review.
Reproduced by the audit.

## Current state

- `internal/migrate/runson.go:237`:

```go
var runsOnKey = regexp.MustCompile(`^(\s*(?:-\s+)?(?i:runs-on)\s*:[ \t]*)(.*?)([ \t]*#.*)?$`)
```

  Group 1 takes the whitespace after the colon, so on a key line with no
  value the comment group starts at `#`.

- `internal/migrate/runson.go:529-535`:

```go
	// Collapse: the key line carries the value, and the item lines go.
	match := runsOnKey.FindStringSubmatch(lines[at].text)
	comment := ""
	if match != nil {
		comment = match[3]
	}
	lines[at].text = strings.TrimRight(lines[at].text[:strings.Index(lines[at].text, ":")+1], " \t") + " " + out.to + comment
```

- Item lines that carry a comment are already refused with a reason (the
  lines just above 529).

## Scope

**In scope**: `internal/migrate/runson.go`, `internal/migrate/runson_test.go`.

**Out of scope**: `plan.go`, `diff.go`, `badge.go`, the wizard's UI.

## Steps

### Step 1: Write the failing tests

In the file's existing table style add a case
"a comment on the key line of a block list keeps its space": the input
above, expecting `runs-on: zoomies-linux-x64 # the big one` (use the label
the neighbouring cases expect for `ubuntu-latest`). Add a second case
where the key line is `runs-on:   # spaced` to show extra whitespace
before the comment collapses to one space.

**Verify**: both fail before the fix.

### Step 2: Put the space back

After reading `comment` from the match, when it is not empty, replace it
with one space followed by the comment with its leading spaces and tabs
trimmed. Comment why: a `#` directly after a scalar is part of the scalar.

**Verify**: `go test -race -count=1 ./internal/migrate/...` passes.

### Step 3: Check the matrix axis named runs-on

Add a test with

```yaml
    strategy:
      matrix:
        runs-on: [ubuntu-latest]
```

and its block-list form. If the tool rewrites either into a scalar (the
audit saw this), a matrix axis is being mistaken for a job's `runs-on`.
Read how the file tracks the enclosing keys (`jobKey` and the indentation
handling) and leave a `runs-on` key whose parent mapping is `matrix`
alone, reporting it as skipped with a reason the way other skips are
reported. If both forms are already left alone, keep the test and change
nothing.

## Done criteria

- [ ] The new tests pass; the comment cases failed before Step 2.
- [ ] `go test -race -count=1 ./internal/migrate/...` passes.
- [ ] `gofmt -l internal/migrate` and `go vet ./internal/migrate/...` clean.
- [ ] `make test` exits 0.
- [ ] Commit message: `Keep a space before the comment when collapsing a runs-on list`.

## STOP conditions

- Step 3 needs more than a parent-key check (the file has no notion of
  the enclosing mapping). Report it and land Steps 1 and 2 alone.
