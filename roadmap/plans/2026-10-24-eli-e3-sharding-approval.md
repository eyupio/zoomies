# Test sharding: attributed approval

**Status:** implemented, awaiting pull request.

## Goal

A kept sharding preview can be approved as a separate, attributed act, without
reading GitHub, opening a pull request or running a job. Mirrors readiness approval.

## Owner decision

Approval and publication stay web-only. Approval names the exact preview, its
commit, its workflow digest and its retained preview hash. It expires with the
preview. The CLI, MCP and Eli may read that an approval exists, its time and its
digests, but not who approved.

## What changed

* Migration `0130_optimisation_sharding_preview_approvals.sql` stores one approval
  per sharding preview, cascading with it. `GetOptimisationShardingPreview` joins and
  validates it; a partial or mismatched row is corrupt evidence.
* `ApproveShardingPreview` replays the retained preview, requires the named head and
  source digest, and records `optimisation.sharding_preview_approved`. A repeat by
  the same binding is a no-op with no second audit row; anything else is a conflict.
* `POST /optimisations/sharding/previews/{id}/approval` needs `optimisations:write`
  and `jobs:read`; visitors refused. The response is the same preview with `approval`.
* The page offers a declaration, which names the shard count and the extra runner
  time, and **Approve preview**, then shows the approver, time, commit and digests
  and says nothing was published.
* The CLI shows the approval line; `get_sharding_preview` projects the time and
  digests and drops `approved_by`.

## Refusals

A missing, expired, evicted or foreign preview; a head, digest or preview hash that
is not the retained one. Approval never reads GitHub, opens a pull request or runs
a job.

## Next

Publication of an approved sharding preview as one draft pull request (migration 0131).
