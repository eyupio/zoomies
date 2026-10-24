# Test sharding: keep a reviewed preview privately

**Status:** implemented, awaiting pull request.

## Goal

An operator can keep the sharding preview they reviewed, and replay it later,
without the source or the history being read again. Mirrors readiness retention.

## What changed

* Migration `0129_optimisation_sharding_previews.sql` and the `ops_` ID prefix store
  one immutable envelope per preview: the exact workflow, the timing samples, the
  evidence (including the pool headroom, which cannot be derived from the samples)
  and the preview. It is private, bounded (20 per installation, 100 overall, 256 KiB
  each), expires after fourteen days and is audited as
  `optimisation.sharding_preview_retained`. An existing ID cannot overwrite evidence
  or renew its lifetime.
* `Controller.CaptureShardingPreview` repeats the source read, the history query and
  the headroom read, and keeps the preview only if the head, the workflow digest and
  the shard count are the ones the operator reviewed. The count is part of what was
  reviewed: the pool's headroom can move between inspecting and keeping, and a
  different number of shards is a different change.
* Provenance (the selected run, attempt, fleet job and head branch) is recorded for
  every preview and is required, so every kept preview can be rechecked before
  anything is published.
* `Controller.ReplayShardingPreview` validates the stored digest and recomputes the
  evidence from the samples and the preview from the evidence, and refuses any
  difference. It reads no GitHub.
* `POST /optimisations/sharding/previews` and
  `GET /optimisations/sharding/previews/{id}` need `optimisations:write` or `:read`
  and `jobs:read`; visitors are refused. The replay returns the job block, never the
  whole workflow.

## Refusals

A head, digest or count that is not the one reviewed; a count outside 2 to 8; an
altered envelope (digest, count, headroom, a sample, provenance); an expired, evicted
or foreign preview.

## Verification

Store tests for immutability, scope, audit and expiry. Controller tests for what is
kept, no GitHub read on replay, and every altered field. API tests for the routes,
shape, audit, replay equality, wrong scope, a count that was not reviewed, and the
auth matrix.

## Next

The `/optimisations/sharding` page with its run picker, review block and keep flow.
