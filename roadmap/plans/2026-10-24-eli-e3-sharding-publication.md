# Test sharding: one draft pull request for an approved preview

**Status:** implemented, awaiting pull request.

## Goal

An approved sharding preview can be published as one draft pull request that splits
the Playwright job into shards and keeps a job with the original name, without
merging, force-pushing or running a job. Mirrors readiness publication.

## Owner decision

Publication stays web-only. It needs the three live optimisation settings, a fresh
read of the selected job, its branch, the workflow and the history, and an approval
bound to the same preview. The CLI, MCP and Eli may read that a draft exists, its
branch, URL, time and digests, but not who published it.

## What changed

* Migration `0131_optimisation_sharding_publications.sql` stores one draft per
  approved preview, cascading with it. `GetOptimisationShardingPreview` joins and
  validates the publication; a partial row, or one bound differently from the
  approval, is corrupt evidence.
* `PublishShardingPreview` re-inspects the job with the stored provenance, rechecks
  the live policy before and after the read, probes the App's permissions, and calls
  the existing optimisation publisher on `zoomies/sharding-<preview-id>`. It never
  merges or force-pushes; a retry reconciles the branch and draft, and a retry that
  finds the record returns it without another GitHub read or write.
* **The shard count may only rise on the fresh read.** The pool's headroom moves
  with the fleet's load, so insisting on the same count would refuse a good change
  whenever a runner happened to be busy. A count the pool or the history no longer
  supports is refused ("preview it again"), and what is published is always the
  approved change, never a recomputed one. A moved head, workflow or branch, or a
  different test step, is refused.
* The pull request description says what the change is, that a job with the original
  name remains, that runner time rises (with the before and after figures) and that
  it needs N runners at once, that the wall clock is an estimate from medians, that
  verification is inconclusive, and that Zoomies will not merge it.
* `POST /optimisations/sharding/previews/{id}/publication` needs
  `optimisations:publish` and `jobs:read`; visitors refused. The request names no
  shard count.
* The page offers **Open a draft pull request** after approval, then shows the draft
  link, branch and publisher and says nothing was merged. The CLI shows the
  publication; the model projection drops `published_by`.

## Refusals

An unapproved preview; a head, branch or workflow that moved; a different step; a
shard count the pool or history no longer supports; a withdrawn setting, including
one withdrawn while the source was being read; a missing App permission; a
conflicting existing draft, which is never recorded.

## Verification

Store tests for one draft per approval, repeat no-op, audit, foreign scope, missing
approval and expiry. Controller tests for the exact request, the body, one draft, the
read count, audit, idempotent retry, and each refusal above. API tests for the route,
shape, scopes, audit and replay. Browser tests for the publish step, the request body
(which names no count) and the draft link, on desktop and phone.

## Next

Nothing in this track is unbuilt. A run against one real repository and a real E2
comparison of a published change are the owner's.
