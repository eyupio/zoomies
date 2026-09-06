# 0002: Choose the Claude model by what the stage risks, and record it

**Status:** proposed, for the owner to ratify.
**Date:** 6 September 2026.

## Context

The roadmap is implemented by Claude Code sessions. Four models are available
at four prices, and the price per token ranges tenfold from Claude Haiku 4.5
to Claude Fable 5.1. Measured results from Anthropic's own runs, summarised in
[agent-models.md](../agent-models.md), say two things at once: Claude Opus 5
matches the Fable tier on a coding benchmark at about 60% of the cost, and
Claude Fable 5.1 at low effort is often competitive with the tiers below on
cost per task while performing better. Per-token price does not predict the
ranking; cost per completed task does, and it is workload-specific.

## Decision

Tier the programme's sessions by what the stage risks: Claude Fable 5.1 for
design, invariants, verification and review; Claude Opus 5 for implementation
on a settled design; Claude Sonnet 5 for bounded, checkable slices; Claude
Haiku 4.5 only as a reading sub-agent. Start every session at `high` effort,
raise to `xhigh` where the reasoning is the deliverable, and use the
run-cheap-then-re-run-failures pattern where tests are the checker. Record
the model, the effort and the review-round count in
[progress.md](../progress.md) for every package.

The alternative, one model (Claude Fable 5.1) with effort as the only lever,
is the fallback if the tiering proves not to pay: it is simpler to run and
removes the question of which session wrote what.

## Consequences

Some pull requests will be written by one tier and reviewed by another; the
review tier is the more capable one, deliberately. The progress record grows a
column that has to be filled in. After Phase 1 the owner can compare cost per
merged package across tiers and revise this record on evidence.
