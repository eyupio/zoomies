# Plan 030: Fold the copied preview and comparison code into one of each

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first, plus
> `internal/store/AGENTS.md` for Part A and `web/AGENTS.md` for Part C.
> This plan has three independent parts; each is its own commit and may be
> its own pull request. Behaviour must not change in any of them. Follow
> each step, run every verification, and stop on any STOP condition there
> or here. Update this plan's row in `plans/README.md` after each part.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/store/queries_optimisation_sharding_previews.go internal/store/queries_optimisation_readiness_previews.go internal/store/queries_optimisation_cache_previews.go internal/store/queries_kennel_fix_previews.go cmd/zoomies/comparison_workflows.go cmd/zoomies/comparison_runners.go internal/mcp web/src/routes web/src/lib/optimisations web/src/lib/kennel
```

## Status

- **Priority**: P3
- **Effort**: L (three parts, M each)
- **Risk**: MEDIUM (refactor; no behaviour change intended)
- **Depends on**: none
- **Category**: duplication
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

Three families of code are near-copies that must change together. A rule
changed in one copy and not the others is a bug nobody sees. About 2,600
lines exist that need not.

## Rule for every part

Run the affected tests before changing anything and keep the output. The
same tests must pass afterwards with no assertion removed. Exported names,
routes, test ids and visible strings stay exactly as they are, so no
caller changes.

## Part A: Four store files with the same shape

**Current state.** `internal/store/queries_optimisation_sharding_previews.go`
(335 lines), `queries_optimisation_readiness_previews.go` (335),
`queries_optimisation_cache_previews.go` (337) and
`queries_kennel_fix_previews.go` (338) each have the same `Put...Preview`,
`Get...Preview`, `Approve...Preview`, `Put...Publication` and three
structs. After renaming, readiness and sharding differ on 16 lines, all
gofmt alignment; cache differs from sharding by about 18 lines a side;
kennel-fix overlaps about 85 per cent. Their tests are copies too
(`optimisation_sharding_previews_test.go` and
`optimisation_readiness_previews_test.go` are 215 lines each).

**Steps.**

1. Diff the four pairwise after renaming the noun, for example:

```
diff <(sed -E 's/Readiness/X/g;s/readiness/x/g' internal/store/queries_optimisation_readiness_previews.go) <(sed -E 's/Sharding/X/g;s/sharding/x/g' internal/store/queries_optimisation_sharding_previews.go)
```

   Write down every real difference: table names, id prefixes, version
   constants, limits, sentinel errors, extra columns or fields.
2. Write one unexported implementation taking a small descriptor struct
   holding exactly those differences. Table names come from compile-time
   constants in the descriptor, never from input.
3. Reduce each of the four files to its exported types and thin wrappers
   that call the shared code with the family's descriptor.
4. Where a family really differs (an extra field), keep that difference
   explicit in its wrapper. If kennel-fix needs more than two such
   special cases, leave it as it is, fold the three optimisation families,
   and say so.
5. Make the copied tests one table-driven test over the families, keeping
   every assertion.

**Verify**: `go test -race -count=1 ./internal/store/... ./internal/controller/... ./internal/api/...`
passes; `gofmt -l internal/store` is empty. Commit:
`Keep one implementation of the preview queries the four families share`.

## Part B: Comparison evidence in the CLI and in MCP

**Current state.** `cmd/zoomies/comparison_workflows.go:15-134` and
`internal/mcp/comparison_workflows.go:12-117` hold the same structs and
validator; so do `cmd/zoomies/comparison_runners.go:17-165` and
`internal/mcp/comparison_runners.go:15-171`. Both sides hard-code
`"runner-evidence-v1"`, `"push_head_sha_root_file"`, the 100-attempt and
500-job bounds and the 128 KiB cap. The one behavioural difference found:
the MCP side sets `side.Shown = len(side.Attempts)`. Five lower-case-hex
validators in `internal/mcp` do one job: `checkoutPreviewSHA`
(`optimisation.go:199`), `cacheSHA` (`optimisation_cache.go:187`),
`readinessSHA` (`optimisation_readiness.go:183`), `comparisonHash`
(`comparison_runners.go:126`), `comparisonWorkflowSHA`
(`comparison_workflows.go:103`).

**Steps.**

1. Read `internal/mcp/boundary_test.go` to see what `internal/mcp` may
   import, and the purity rules of the candidate home. The audit's
   suggestion is `internal/analysis`, which both sides already import.
   Choose a home that needs no rule loosened.
2. Move the shared structs, constants and validators there. Each side
   keeps its printing or projection, and the MCP side keeps setting
   `Shown` itself after validation.
3. Replace the five hex validators with one helper taking the accepted
   length or lengths; where two accept different lengths today, keep the
   difference as the argument.

**Verify**: `go test -race -count=1 ./cmd/... ./internal/mcp/... ./internal/analysis/...`
passes, including the boundary test unchanged. Commit:
`Validate comparison evidence in one place for the CLI and MCP`.

## Part C: Five preview routes and five preview cards in the UI

**Current state.** Routes `web/src/routes/CachePreview.svelte` (514),
`CheckoutPreview.svelte` (583), `ShardingPreview.svelte` (532),
`ReadinessPreview.svelte` (532), `KennelFixTimeout.svelte` (502). Cards
`web/src/lib/optimisations/{Cache,Checkout,Sharding,Readiness}PreviewCard.svelte`
(240 to 257 each) and `web/src/lib/kennel/KennelFixCard.svelte` (260).
`begin()` is byte-identical in all five routes; `failed()` and `pick()`
are identical in pairs; the Sharding and Readiness routes differ on 77 of
532 lines (API function names, two types, one field); the style blocks
differ by 1 to 12 lines.

**Setup.** `cd web && npm ci`. Run the unit tests, type check and lint
the Makefile runs for the UI (`make test-unit`, and the scripts `make
lint` calls) and keep the output. `make test-ui` runs Playwright against
a built binary; run the specs that cover these pages before and after.

**Steps.**

1. Diff the five routes pairwise and the five cards pairwise; write down
   every real difference.
2. Extract one shared route shell and one shared card shell using the
   runes and snippets the codebase already uses. Each takes the kind's
   API functions, its id pattern, its copy, and a snippet for the facts
   that are the kind's own. Shared styles live once.
3. Reduce each of the ten files to the part that is its own. Keep every
   route path, lazy-loading boundary, prop, test id, aria label and
   visible string.
4. Add no library. Keep the absent-value placeholder as `NO_VALUE` from
   `web/src/lib/format.ts`.
5. If one page does not fit without flags added for it alone (Checkout is
   the longest), leave that page and say so.

**Verify**: unit tests, type check, lint and a production build pass (the
build enforces the 200 KB shell budget); the Playwright specs for these
pages pass; a before and after screenshot of two of the pages match.
Commit: `Share one shell between the preview pages and one between their cards`.

## Done criteria

- [ ] Each part landed with its verification green, or is recorded in the
      index as left with a one-line reason.
- [ ] `make test` and `make lint` exit 0 after each part.
- [ ] `git diff --stat` for each part shows a net removal.

## STOP conditions

- A test has to lose an assertion to pass.
- An exported name, route, test id or visible string has to change.
- A boundary or purity test has to be loosened.
- Part C: the shell bundle passes its budget only by moving code that was
  lazily loaded into the shell.

## What was done

### Part A: the four store files (done)

`internal/store/queries_retained_previews.go` holds the one implementation
(`putRetainedPreview`, `getRetainedPreview`, `approveRetainedPreview`,
`putRetainedPublication`) and a `retainedPreviewKind` descriptor with everything the
families differ in: sentinel, version, ID prefix, limits, the three table names and
the audit actions. The four family files keep their exported types and method names,
so no caller changed; each is its constants, its preview struct, one descriptor and
four thin methods (about 103 lines instead of about 335). The table names are spliced
into SQL, so they are compile-time constants of this package's descriptors.

The pairwise diff the plan asked for found two real differences, not one: the kennel-fix
publication does not require the base commit to equal the approved head, in the
publication's own check and again in the validation of a stored publication. That is
the descriptor's `baseIsApprovedHead` (true for sharding, cache and readiness; false
for kennel-fix), and the kennel-fix doc comment already said why. Everything else was
names. The generation was mechanical (substitute each family-specific name with its
descriptor field), then read; reading caught one bug (an ID prefix that would have
been followed by two underscores), which the existing tests also fail on.

The types stay distinct: a family's `Approval` and `Publication` are defined types
over the shared shapes (so a pointer converts without copying), and its `Preview`
is its own struct because it holds pointers to those. Nothing of one family can be
passed for another's.

Tests: the seven tests the sharding, readiness and kennel-fix files shared are now
`retained_previews_test.go`, one table over the three families whose adapters convert
to the shared shapes. The audit actions, target kind, tables and branch prefix are
written there as literals and not read back from the descriptors, since they are what
other things read. The two copies that were identical are deleted, kennel-fix keeps
its fixture and the one test that is its own, and cache keeps its file: it has six
tests of bounds, corruption and backup beyond the common ones, and its copies of the
common ones differ in a few assertions. Mutation check: a wrong audit literal and a
wrong `baseIsApprovedHead` each fail a test.

### Part B: comparison evidence in the CLI and MCP (done in part, on purpose)

The plan proposed moving the evidence structs and validators into `internal/analysis`
and removing about 230 lines. Reading both sides showed why that would be wrong. The
two decode types are separate whitelists by design, and their own comments say so
("Independent REST whitelists keep private and future fields out of model input"):
the MCP type is also the shape returned to the model (it carries a `shown` field the
CLI's does not), while the CLI's only feeds human output. One shared struct would mean
a field added for the CLI is sent to a model without anyone deciding that.

So the structs and the two loops that walk them stay separate. What is shared is what
was identical and carries no such decision, in `internal/analysis/evidence_format.go`:

- `LowerHex(s, n)`, replacing eight copies of "exactly n lower-case hex digits" (they
  were spelled three ways, `hex.DecodeString` plus lower case, a character loop, and
  a loop plus lower case, and all accept the same strings), `DecimalID` and
  `EventName`, with a table test that pins the edge cases (upper case, wrong length,
  sign, leading zero, non-ASCII digits).
- Named constants for what both sides restated in long conditions: the two evidence
  versions, the one root-source basis, the 100-row bound, the 500-jobs-per-attempt
  bound and the 128 KiB root-file bound. The MCP schemas use them too, so a schema's
  `enum` cannot differ from the validator's version.

About 85 lines fewer in the callers and about the same added in `analysis`, so this
part does not shrink the tree; it makes each rule one rule. The existing 400-odd lines
of evidence tests on both sides pass unchanged, and so does the MCP boundary test.
If the owner wants the loops shared as well, the way to do it without coupling the
whitelists is a shared arm type embedded by each side's own, with a test that pins the
JSON field set of what MCP returns; that is a design decision, not a clean-up.

### Part C: the UI preview pages (not done; needs a decision)

Not changed. The plan's premise was five routes and five cards that are one template
with a different noun, and measuring them showed they are not. Against the sharding
route, readiness differs on 154 lines (77 each side), cache on 302, checkout on 445
and kennel-fix on 504, and what differs is behaviour, not wording: sharding sends
`shards` and no condition, readiness requires and validates a `condition` input, the
ID patterns, copy, header links, payload builders and which inputs exist all differ.
A shell that took all of that as props and snippets would be a configurable engine
about as large as the pages it replaced, and a UI change I cannot compare against the
old pages by eye. The plan's own rule covers it: leave a page that does not fit without
contortion, and say so.

What is shared, `begin()`, `failed()` and `pick()`, is not pure: each closes over the
route's `$state` (`error`, `busy`, `retryAt`, `operation`, `controller`, `chosen`…), so
sharing them means a reactive class in a `.svelte.ts` module (say `PreviewFlow`) and
editing every template reference to those variables in five files. That is the safe
slice if this is wanted: about 40 lines out of each route, no change to what any page
shows, checked by the Playwright specs for sharding, cache, checkout and kennel-fix
(`make test-ui` builds the binary and drives it; Chromium is installed here). It is
left for the owner to ask for, with a before and after screenshot of two pages.
