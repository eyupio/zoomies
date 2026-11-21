# Plan 031: Delete what nothing uses

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first, and the `steward`
> skill under `.claude/skills/` if it is present, which covers removing
> code and files here. Follow this plan step by step, run every
> verification, and stop on any STOP condition there or here. Update this
> plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/aicontext internal/controller/optimisation_inspect.go internal/store/host_recovery.go internal/store/queries_eli_repair_watches.go internal/store/queries_ai_context.go web/src/lib/api/types.ts design
```

## Status

- **Priority**: P3
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: dead code
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

About 2,100 lines are referenced by nothing but themselves or their own
tests. They still have to be read, compiled and kept passing.

## The rule for every item

Before deleting anything, search the whole tree for its name, including
tests, fixtures, templates, the Makefile, `.github/`, docs and string
references:

```
grep -rIn --exclude-dir=.git --exclude-dir=node_modules '\bNAME\b' .
```

Delete an item only when the only hits are its declaration and tests that
exist for it alone. If the search shows a use the audit missed, leave the
item and record it in the commit body. The audit matched by identifier
name, so a method that shares its name with a live one needs a closer
look.

## Items

1. **`internal/aicontext/storage.go`** (131 lines): the `Storage`
   interface, `DiskStorage`, `OpenDiskStorage`, `Put`, `Get`, `Delete`,
   `Close`. The only callers at the planned commit are five calls to
   `OpenDiskStorage` in `internal/aicontext/snapshot_test.go` (lines 158,
   169, 199, 236, 242). Delete the file and the tests in
   `snapshot_test.go` that exist only for it (about lines 156-258); keep
   any helper those tests share with others.
2. **`DecimalID`** at `internal/controller/optimisation_inspect.go:90`.
   The differently named `cliWorkflowDecimalID` and `comparisonDecimalID`
   are live and stay.
3. **Store queries called only from tests**: `CheckHostRecovery`
   (`internal/store/host_recovery.go:132-143`), `EliRepairWatchForPull`
   (`internal/store/queries_eli_repair_watches.go:209-215`),
   `DeleteAIContextRepository` (`internal/store/queries_ai_context.go:167-173`).
   Where a test uses one as a probe for other behaviour, switch the test
   to an existing read path and keep its assertion. If that is not a
   small change, leave that query and say so.
4. **Three mock pages**: `design/cute-uplift-mocks.html` (902 lines),
   `design/off-uplift-mocks.html` (289), `design/standard-uplift-mocks.html`
   (581). `off-uplift-mocks.html` lines 2-3 call itself a throwaway
   decision mock, not shipped, not built, not linked. Nothing in the tree
   mentions `uplift-mocks`. The owner approved their removal on
   2026-11-21 by asking for every finding of the audit to be fixed.
5. **Unused exported types** in `web/src/lib/api/types.ts`: `GoDuration`
   (about line 24), `PanelLayoutPreference` (105), `SettingKind` (242),
   `SettingScope` (244), `SettingsImportAction` (264). Confirm the file
   is hand-written, not generated from `api/openapi.yaml`, and that no
   other type in it refers to them.

## Scope

**In scope**: the files named above and the test files that reference
them.

**Out of scope**: `eli-enhancement-roadmap-handoff.md` at the root, which
looks stale and is not: `roadmap/README.md` and
`internal/docs/planning_docs_test.go` pin it there. Anything not on the
list.

## Steps

1. Run the baseline: `go test -race -count=1 ./internal/aicontext/... ./internal/store/... ./internal/controller/...`.
2. Work through the items in order, searching first as the rule says.
3. For item 5 run the UI's type check (`cd web && npm ci`, then the check
   script `make lint` uses).

**Verify**:

```
go vet ./...
go test -race -count=1 ./internal/aicontext/... ./internal/store/... ./internal/controller/... ./internal/docs/...
```

both pass, and `gofmt -l` on the changed directories is empty.

## Done criteria

- [ ] Every item is deleted or recorded as kept with its reason.
- [ ] `go vet ./...` and `make test` exit 0; the UI type check passes.
- [ ] `git diff --stat` shows only removals and test adjustments.
- [ ] One commit, message: `Delete what nothing uses`, with the line count
      removed and anything kept in the body.

## STOP conditions

- A deleted name turns out to be reached through reflection, a
  `go:embed`, a `go:generate` directive or a string.
- A docs test in `internal/docs` names one of the files.
