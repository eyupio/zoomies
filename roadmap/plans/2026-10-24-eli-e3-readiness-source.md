# Bind a selected job and its wait history to a readiness preview

**Status:** implemented, awaiting pull request.

## Goal

An operator can inspect a bounded readiness preview for one successful push job
without the server guessing which command makes the service ready.

## What changed

* `github.ReadReadinessSource` reads the pinned attempt, its complete commit
  tree and the root workflow, at most four requests under the shared diagnostic
  admission. It follows no redirect, reads no lockfile and rechecks the attempt
  and file identity before returning.
* `ValidReadinessSource` holds the source to the literal job and step, the
  workflow digest, the fixed sleep and the Ubuntu runner.
* `InspectReadinessPreview` matches the selected fleet job, finds its one
  completed wait step, reads 2 to 100 retained samples from the last 90 days and
  computes the evidence ID and mean wait. The operator's condition is attached
  only after that evidence exists, so a condition cannot be inferred from the
  repository.
* `POST /optimisations/readiness/inspections` returns the exact before/after
  block, bound, condition, evidence and inconclusive verification. It needs
  operator, `optimisations:write` and `jobs:read`; visitors are refused. Nothing
  is retained.
* OpenAPI and both generated clients carry the route and view. No web page,
  CLI, MCP or Eli surface is added yet.

## Refusals

* A missing, foreign or changed preview scope.
* An attempt that is not a completed successful push, an incomplete or oversized
  tree, or a file whose blob identity or digest does not match.
* A workflow without one literal fixed sleep on an Ubuntu runner, or with
  ambiguous literal names.
* A selected job that does not match the attempt and head, or whose step has no
  timing. Fewer than two retained wait samples.
* A blank, multiline or oversized condition.

## Verification

* The controller test binds the selected job, two wait samples and the supplied
  condition, and refuses an empty one.
* The API test covers the response shape, one source read, no retention and a
  blank condition.
* The auth matrix covers role, scopes and visitors.
* `make test`, `make lint`, `make openapi` and strict docs run before the pull
  request.

## Next

The readiness web page: pick a job, enter the condition, review the exact block,
then keep and approve it. CLI/MCP/Eli read access and publication follow the
cache pattern. Test sharding remains a separate catalogue item.
