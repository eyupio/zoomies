# Retain a reviewed bounded readiness preview

**Status:** implemented, awaiting pull request.

## Goal

An operator can keep the exact readiness preview they reviewed, reopen it for
fourteen days, and read it without a GitHub request or a model call.

## What changed

* Migration `0126_optimisation_readiness_previews.sql` adds a separate,
  installation-scoped history with a fourteen-day lifetime, twenty per
  installation and one hundred globally. It cascades on installation deletion.
* The private envelope keeps the full workflow source, the operator's condition,
  the wait samples, the computed preview and provenance (run, attempt, fleet job
  and branch) for a later publication slice. The API never returns the full
  workflow.
* `CaptureReadinessPreview` re-reads the source and wait history and requires the
  same head, workflow digest and condition. A moved source or condition is
  refused. `ReplayReadinessPreview` recomputes from stored input only.
* `POST /optimisations/readiness/previews` and `GET .../{id}` carry the route,
  with an audit row for retention. They need operator, `optimisations:write` or
  `:read`, and `jobs:read`; visitors are refused.
* The readiness page adds a declaration and **Keep a preview for 14 days**, a
  retained card and an `opr_` link. It never approves or publishes.
* OpenAPI, both generated clients, the Eli guide, roadmap and handoff are
  updated.

## Refusals

* A missing, expired, evicted or foreign preview is unavailable.
* A different head, workflow digest or condition is a conflict.
* A preview whose stored envelope or recomputation is inconsistent is corrupt
  evidence, not an empty preview.
* The page disables keeping until the declaration is checked.

## Verification

* Store tests cover immutability, scope, audit and expiry.
* The controller test covers capture, provenance, replay and a moved source.
* API tests cover the route, response shape, one read, audit and replay.
* Browser tests cover the disabled keep button, declaration, exact capture body,
  retained card and opening from a link, on desktop and phone.
* `make test`, `make lint`, `make openapi` and strict docs run before the pull
  request.

## Next

Readiness CLI/MCP/Eli read access, then approval and publication. Test sharding
remains separate.
