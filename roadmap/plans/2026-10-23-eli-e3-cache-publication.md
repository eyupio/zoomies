# Open a draft pull request for an approved cache preview

**Status:** implemented, awaiting pull request.

## Goal

An approved setup cache preview can be published as one draft pull request that
adds the reviewed `with:` cache block, without merging, force-pushing or running a
job.

## Owner decision

Publication stays web-only. It needs the three live optimisation settings, a
fresh read of the selected job and its branch, the same head, workflow digest,
lockfile digest and after block, and an approval bound to the same preview. The
CLI, MCP and Eli may read that a draft exists, its branch, URL, time and digests,
but not who published it.

## What changed

* Cache previews now record provenance: the selected run and attempt, the fleet
  job, and the run's head branch. Previews kept before this cannot be published;
  their branch cannot be rechecked.
* Migration `0124_optimisation_cache_publications.sql` stores one draft per
  approved preview, cascading with it. `GetOptimisationCachePreview` joins and
  validates the publication; a partial row is corrupt evidence.
* `PublishCachePreview` re-reads the source under the shared diagnostic
  admission, requires the same branch, head, workflow digest, lockfile digest and
  after block, rechecks the live policy and approval, probes the App's
  permissions, and calls the existing optimisation publisher on
  `zoomies/cache-<preview-id>`. It never merges or force-pushes; a retry
  reconciles the branch and draft.
* `POST /optimisations/cache/previews/{id}/publication` needs
  `optimisations:publish` and `jobs:read`; visitors are refused. The response is
  the same preview with a nullable `publication`.
* The cache card, once approved, offers **Open a draft pull request** and then
  shows the draft URL, branch and publisher. It says nothing was merged.
* The CLI shows the publication. MCP/Eli project its branch, URL, time and
  digests, dropping who published it.
* OpenAPI, both generated clients, the CLI/MCP references and the handoff pages
  are regenerated or updated.

## Refusals

* A missing, expired, evicted or foreign preview is unavailable.
* An unapproved preview, a moved branch or head, a changed workflow or lockfile,
  a changed after block, a withdrawn setting or a missing App permission is
  refused before any GitHub write.
* A retry returns the existing draft rather than opening another. A different
  draft for the same preview is a conflict.

## Verification

* Store tests cover one draft per approval, repeat no-op, audit, foreign scope,
  missing approval and expiry.
* Controller tests cover the exact publish request, one draft, the read count,
  audit, idempotent retry and refusal without approval or with a foreign head.
* API tests cover the route, response shape, request body, permission scopes,
  audit and replay after publication.
* Browser tests cover the publish button after approval, the exact request body
  and the draft link, on desktop and phone.
* Full `make test`, `make lint`, UI check/unit tests and the focused browser
  specs run before the pull request.

## Next

Bounded readiness polling and sharding. Real-repository validation and a real E2
comparison remain the owner's after merge; publication is not evidence of a
saving.
