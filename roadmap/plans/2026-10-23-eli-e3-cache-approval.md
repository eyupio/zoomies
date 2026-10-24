# Let an operator approve a kept setup cache preview

**Status:** implemented, awaiting pull request.

## Goal

A cache preview kept from `/optimisations/cache` can be approved as a separate,
attributed act, without reading GitHub, opening a pull request or running a job.

## Owner decision

Approval and publication stay web-only. Approval names the exact preview, its
commit, its workflow digest and its retained preview hash. It expires with the
preview. The CLI, MCP and Eli may read that an approval exists, its time and its
digests, but not who approved.

## What changed

* Migration `0123_optimisation_cache_preview_approvals.sql` stores one approval
  per cache preview, cascading with it. `GetOptimisationCachePreview` joins and
  validates the approval; a partial or mismatched row is corrupt evidence.
* `ApproveCachePreview` replays the retained preview, requires the named head and
  source digest, and records an audit row `optimisation.cache_preview_approved`.
  A repeat by the same binding is a no-op with no second audit row.
* `POST /optimisations/cache/previews/{id}/approval` is the operator route,
  needing `optimisations:write` and `jobs:read`; visitors are refused. The
  response is the same cache preview with `approval` filled in.
* The cache page offers a declaration and **Approve preview** on the kept card,
  then shows the approver, time, commit and digests. It says nothing was
  published.
* `zoomies optimisations cache` shows the approval line. `get_cache_preview`
  projects the approval's time and digests, dropping `approved_by`, and says in
  its boundary that approval is evidence, not permission to write.
* OpenAPI, the generated TypeScript schema and Go source, the CLI/MCP reference
  and the handoff pages are regenerated or updated.

## Refusals

* A missing, expired, evicted or foreign preview is unavailable.
* A different head, source digest or preview hash is a conflict, not a new
  approval.
* Approval never reads GitHub, opens a pull request, runs a job or publishes.

## Verification

* Store tests cover the binding, repeat no-op, audit count, foreign scope and
  expiry with the preview.
* API tests cover the route, response shape, audit, replay after approval and a
  mismatched head. The auth matrix covers role, scopes and visitors.
* Browser tests cover the disabled button, the declaration, the request body,
  the approved card and the phrase that nothing was published, on desktop and
  phone.
* Full `make test`, `make lint`, UI check/unit tests and the focused browser
  specs run before the pull request.

## Next

Cache publication: one draft pull request for an approved preview, with fresh
source, branch and policy checks. Real-repository validation and a real E2
comparison remain the owner's after merge.
