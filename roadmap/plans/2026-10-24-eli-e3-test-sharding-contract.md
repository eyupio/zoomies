# Test sharding: a contract for the owner to accept

**Status:** proposed. Nothing here is built. No code, migration, route or
workflow edit follows until the owner accepts or amends
[decision 0014](../decisions/0014-test-sharding-is-offered-for-one-native-shard-flag-and-preserves-the-whole-suite.md).

## Why a contract first

Checkout scope, the dependency cache and bounded readiness polling each change
one literal line or block, and a bad guess costs a failed run. Sharding can cost
more: it can silently drop tests, break a required status check that names a job,
hide a failure behind a merged report, and spend more runner time than it saves.
The handoff's catalogue row says only "long gating suite, supported shard
mechanism and available capacity" and "preserve the complete test set; disclose
consumption increase". That is a principle, not a mechanism, so this plan states
the mechanism, the evidence, the disclosure and the refusals, and asks.

## What is supported

One mechanism, to start: **a test step whose command is a single invocation of a
runner with its own deterministic shard flag**, `--shard=INDEX/TOTAL`, run once
per matrix entry.

| Runner | Shard flag | Notes |
| --- | --- | --- |
| Playwright Test | `--shard=i/N` | The repository's own CI uses it. Partitions the whole discovered set. |
| Jest | `--shard=i/N` | Jest 28 and later. |
| Vitest | `--shard=i/N` | Vitest 0.30 and later. |

The recommendation is to ship **Playwright only** in the first slice, because it
is the one the repository exercises in CI and its shard semantics are
documented as a partition of the full set, then add Jest and Vitest as separate,
equally reviewed slices. The owner may widen or narrow this.

Not supported, and refused with a reason: pytest, Go, RSpec, Maven, Gradle and
anything that needs a plugin, a hand-written file list, a hash of test names or
an external splitter. Zoomies does not invent a partitioning scheme; if the
runner has no native, deterministic one, there is no preview.

## How the complete test set is preserved

* The runner's own flag does the partitioning, so Zoomies never lists, filters or
  orders tests.
* The edit adds exactly one matrix dimension of literal shard numbers `1..N` and
  appends `--shard=${{ matrix.shard }}/N` with the same literal `N`. Every other
  argument is left alone.
* The union of the shards is the runner's whole set by the runner's own
  guarantee. The preview states that guarantee and names the runner version it
  rests on, and refuses a version below the one that has the flag.
* Refused when the command already shards, filters or narrows: `--shard`,
  `--grep`, `--grep-invert`, `-t`, `--testNamePattern`, `--testPathPattern`,
  `--onlyChanged`, `--changedSince`, `--project` subsets chosen by an expression,
  `--last-failed`, or a list of paths that is not the
  runner's own default. Refused when the step is followed by `|| true`, has
  `continue-on-error: true`, or sets `if:` conditions that already skip it.
* Refused when the job has an existing matrix, a `strategy.fail-fast: true`
  that would stop sibling shards (the edit sets `fail-fast: false` and says so,
  but an existing matrix is not merged by guesswork), `needs` outputs consumed
  downstream, or `services:` that cannot be duplicated safely per shard.

## How the shard count is chosen

Evidence, all from the fleet's retained history and never from a model:

* the test step's own durations over at least 10 successful matching runs in the
  last 90 days, with their median and spread;
* the job's setup cost (checkout, install, build) from the same runs, because
  every shard pays it again;
* the pool's queue wait and the pool's free capacity for the job's labels.

Rule: choose the smallest `N` from 2 to 8 for which the per-shard test time is at
least a stated floor (proposed: 90 seconds, so setup does not dominate) and
`N` concurrent runners fit in the pool's current headroom without making shards
queue behind each other. If no `N` satisfies both, there is no preview and the
reason says which condition failed. Zoomies never chooses `N` from the size of the
repository or the number of test files.

## How increased consumption is disclosed

The preview is a table, not a promise:

* **Runner time:** estimated total runner minutes per run before and after. It
  rises by roughly `(N - 1)` times the setup cost, and the preview says so.
* **Concurrent runners needed:** `N`, against the pool's current maximum.
* **Wall clock:** an estimate range from the retained medians, labelled an
  estimate. Verification stays **inconclusive**; no saving is claimed, and passing
  CI alone cannot establish one.
* **Cost:** shown separately from latency when the fleet knows a rate, otherwise
  not shown. The three are never merged into one "saving".

## How failure diagnostics and required checks survive

This is the part most likely to hurt, so the contract is strict.

* **Required checks.** A matrix renames the job (`Build (1)`, `Build (2)`), so a
  branch-protection or ruleset check that names `Build` would wait forever. The
  edit keeps the original job name by adding one aggregator job with exactly the
  original name, `needs` on the shard job and `if: always()`, that fails unless
  every shard succeeded. If Zoomies cannot read whether a required check names the
  job, the preview says "unknown" and the operator must declare they checked.
* **Diagnostics.** `fail-fast: false`, so every failing shard reports. Shard
  index is in each job name. Artifacts and reports that upload under a fixed name
  are refused unless the name can take the shard number; merged reports (Playwright
  blob reports, coverage) are refused in the first slice because merging is a
  second change with its own failure modes.
* **Coverage thresholds** and any step after the test step that reads a single
  combined output are refused.
* **Retries and flake handling** are untouched. Sharding never adds a retry.

## What is refused, in one place

No supported runner flag; a command that already shards, filters or narrows; a
matrix that already exists; fewer than 10 matching successful runs; no `N` that
fits both the time floor and the pool's headroom; an unknown required-check
relationship the operator has not declared; combined reports, coverage or
single-name artifacts; services that cannot run per shard; and any workflow
outside the root `.github/workflows` set the earlier previews already restrict
to. Test removal, relaxed assertions and blanket retries are not offered here or
anywhere.

## How it would ship, if accepted

The same flow as cache and readiness, each its own reviewed slice: pure preview
and edit; bounded source binding and inspection; retention and replay;
`/optimisations/sharding` with its run picker; CLI, MCP and Eli replay; attributed
approval; one reconciled draft pull request. Approval and publication stay
web-only, and the draft is never merged or force-pushed. The existing settings
(`source_reads`, `workflow_edits`, `publish`) gate it; no new setting is proposed.

## Questions for the owner

1. Which runners in the first slice: Playwright only (recommended), or Playwright,
   Jest and Vitest together?
2. Is the 90 second per-shard floor and the cap of 8 shards right?
3. May the aggregator job be added by the edit, or should a job that a required
   check names be refused outright in the first slice (simpler, narrower)?
4. Are merged reports and coverage a permanent refusal or a later slice?

Real-repository validation and a real E2 comparison of a published change are
owner tasks and cannot be done from this environment.
