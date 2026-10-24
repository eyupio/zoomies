# 0014: Test sharding is offered for a native shard flag and preserves the whole suite

**Status**: proposed, for the owner to ratify. Nothing is built.

## Context

Test sharding is the next item in the E3 catalogue, after checkout scope, the
dependency cache and bounded readiness polling. The handoff says only that it
needs a long gating suite, a supported shard mechanism and available capacity,
and that it must preserve the complete test set and disclose the increase in
consumption. Unlike the earlier changes it can drop tests, rename a job that a
required check depends on, and spend more runner time than it saves. The full
reasoning and the open questions are in
[the 24 October plan](../plans/2026-10-24-eli-e3-test-sharding-contract.md).

## Decision

Offer sharding only for a single test command whose runner has its own
deterministic `--shard=INDEX/TOTAL` flag, starting with Playwright. Choose the
shard count from at least ten retained successful runs and the pool's real
headroom, never from repository size. Keep the original job name through an
aggregator job so required checks still resolve, set `fail-fast: false`, and
refuse anything that filters, narrows, merges reports, reads combined coverage or
already uses a matrix. Show runner time, concurrent runners, an estimated
wall-clock range and cost separately, label verification inconclusive, and claim
no saving. Approval and publication stay web-only, and a draft is never merged.

## Consequences

* A repository whose runner has no native shard flag gets no sharding preview.
* The first slice is narrower than "any test suite" on purpose.
* Runner time rises with the shard count, and the preview says so before approval.
* The owner's answers to the four questions in the plan decide the first slice's
  scope; until then no implementation starts.
