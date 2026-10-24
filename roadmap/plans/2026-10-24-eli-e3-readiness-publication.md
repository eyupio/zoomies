# Open a draft pull request for an approved readiness preview

**Status:** implemented, awaiting pull request.

## Goal

An approved bounded readiness preview can be published as one draft pull request
that replaces one fixed sleep with a bounded poll of the operator's condition,
without merging, force-pushing or running a job.

## Owner decision

Publication stays web-only. It needs the three live optimisation settings, a
fresh read of the selected job and its branch, the same head, workflow digest,
condition, bound and after block, and an approval bound to the same preview. The
CLI, MCP and Eli may read that a draft exists, its branch, URL, time and digests,
but not who published it.

## What changed

* Migration `0128_optimisation_readiness_publications.sql` stores one draft per
  approved preview, cascading with it. `GetOptimisationReadinessPreview` joins
  and validates the publication; a partial row is corrupt evidence.
* `PublishReadinessPreview` uses the provenance already stored in the retained
  envelope (selection run, attempt, fleet job and head branch). A preview kept
  without it cannot be published and is refused with a message to make a new one.
* It re-inspects the job with the stored condition, requires the same head,
  workflow digest, condition, bound, edit and branch, rechecks the live policy
  before and after the read, probes the App's permissions, and calls the existing
  optimisation publisher on `zoomies/readiness-<preview-id>`. It never merges or
  force-pushes; a retry reconciles the branch and draft.
* `POST /optimisations/readiness/previews/{id}/publication` needs
  `optimisations:publish` and `jobs:read`; visitors are refused. The response is
  the same preview with a nullable `publication`.
* The readiness card, once approved, offers **Open a draft pull request** and
  then shows the draft URL, branch and publisher. It says nothing was merged.
* The CLI shows the publication. MCP and Eli project the branch, URL, time and
  digests, dropping who published it. The tool name is unchanged.
* OpenAPI, both generated clients, the CLI/MCP references and the handoff pages
  are regenerated or updated.

## Refusals

* A missing, expired, evicted or foreign preview is unavailable.
* An unapproved preview, a moved branch or head, a changed workflow, condition,
  bound or after block, a withdrawn setting or a missing App permission is refused
  before any GitHub write.
* A retry returns the existing draft rather than opening another. A different
  draft for the same preview is a conflict and is never recorded.

## Verification

* Store tests cover one draft per approval, repeat no-op, audit, foreign scope,
  missing approval and expiry.
* Controller tests cover the exact publish request, one draft, the read count,
  audit, idempotent retry without a GitHub read or write, and every refusal above.
* API tests cover the route, response shape, request body, permission scopes,
  audit and replay after publication.
* Browser tests cover the publish button after approval, the exact request body
  and the draft link, on desktop and phone.

## Next

Test sharding has no agreed contract. A dated plan and, if appropriate, a decision
record come first; the owner accepts the contract before any implementation.
