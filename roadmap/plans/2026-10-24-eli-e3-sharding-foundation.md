# Test sharding foundation: the pure preview

**Status:** implemented, awaiting pull request.

## Goal

A pure, supplied-evidence preview that rewrites one Playwright test job into a
shard matrix plus an aggregator that keeps the job's name, mirroring the cache and
readiness foundations. It reads no source, calls no model and has no runtime
surface.

## Owner decision

[Decision 0014](../decisions/0014-test-sharding-is-offered-for-one-native-shard-flag-and-preserves-the-whole-suite.md):
Playwright's native `--shard=INDEX/TOTAL` only; the original job name survives
through an aggregator; refusals are named; verification is inconclusive.

## What changed

* `workflow.FindPlaywrightTest` finds the one step whose run script is exactly
  `npx playwright test`, `pnpm exec playwright test` or `yarn playwright test`
  with only `--reporter`, `--workers`, `--retries`, `--timeout` and a literal
  `--project`. Anything else is a `ShardRefusal` that says why, including an
  argument that already shards, filters or narrows, `|| true`, and a path.
* It refuses a job that has a strategy, services, outputs, a condition,
  `continue-on-error`, a container or its own concurrency group; a non-Ubuntu or
  expression runner; a name that collides with `<id>-shards`; any artifact upload,
  codecov, coveralls or coverage step; an unnamed or conditional test step; and a
  job too large to review as one block.
* `workflow.ShardChange` renames the job to `<id>-shards`, adds
  `fail-fast: false` and a literal `shard: [1..N]` matrix, appends
  `--shard=${{ matrix.shard }}/N`, and adds a job with the original ID and name
  that `needs` the shards, runs `if: ${{ always() }}` and fails unless every shard
  succeeded. Bytes outside the job's block are untouched and line endings are kept.
* `optimisation.PreviewSharding` takes the source, the evidence and the policy.
  `ChooseShardCount` picks the largest count up to eight and the pool's headroom
  that leaves each shard at least 90 seconds of test. It refuses a suite under
  three minutes and a pool that cannot start two runners at once.
* `ShardEvidenceFromSamples` needs 10 to 100 distinct successful runs and uses the
  median of the test step and of everything before it, so a few slow runs do not
  move the count. The evidence ID is order independent.
* The preview discloses runner time before and after (every shard repeats the
  setup), the runners needed at once, and a wall-clock estimate, and says plainly
  that it is an estimate from medians and not a measured saving. Verification is
  always inconclusive.
* The optimisation purity test lists the new workflow helpers it may call.

## Refusals

No supported step; more than one step; a command that shards, filters or
narrows; a job shape above; fewer than ten runs; a suite too short for two
shards; no headroom; stale or foreign evidence; policy off.

## Verification

Table-driven tests for every refusal, the exact rewrite with CRLF and no job
name, a rewritten workflow that still parses and is refused a second time, the
count rule at its boundaries, medians that ignore outliers, and order-independent
evidence.

## Next

Bounded GitHub source binding and inspection: read the one selected push job and
its workflow, take the timings from the fleet's retained history, and read the
pool's headroom.
