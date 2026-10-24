# Let an operator approve a kept readiness preview

**Status:** implemented, awaiting pull request.

## Goal

A readiness preview kept from `/optimisations/readiness` can be approved as a
separate, attributed act, without reading GitHub, opening a pull request or
running a job.

## Owner decision

Approval and publication stay web-only. Approval names the exact preview, its
commit, its workflow digest and its retained preview hash. It expires with the
preview. The CLI, MCP and Eli may read that an approval exists, its time and its
digests, but not who approved.

## What changed

* Migration `0127_optimisation_readiness_preview_approvals.sql` stores one
  approval per readiness preview, cascading with it.
  `GetOptimisationReadinessPreview` joins and validates the approval; a partial
  or mismatched row is corrupt evidence.
* `ApproveReadinessPreview` replays the retained preview, requires the named head
  and source digest, and records an audit row
  `optimisation.readiness_preview_approved`. A repeat by the same binding is a
  no-op with no second audit row.
* `POST /optimisations/readiness/previews/{id}/approval` is the operator route,
  needing `optimisations:write` and `jobs:read`; visitors are refused. The
  response is the same readiness preview with `approval` filled in.
* The readiness page offers a declaration and **Approve preview** on the kept
  card, then shows the approver, time, commit and digests. It says nothing was
  published.
* `zoomies optimisations readiness` shows the approval line. `get_readiness_preview`
  projects the approval's time and digests, dropping `approved_by`.
* OpenAPI, the generated clients and the handoff are updated. `source_sha256` is
  now required on `ReadinessPreview`.

## Refusals

* A missing, expired, evicted or foreign preview is unavailable.
* A different head, source digest or preview hash is a conflict, not a new
  approval.
* Approval never reads GitHub, opens a pull request, runs a job or publishes.

## Next

Readiness publication: one draft pull request for an approved preview, from the
provenance already retained. Previews kept without provenance cannot be published.
