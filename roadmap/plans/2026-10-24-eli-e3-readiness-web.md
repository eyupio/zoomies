# Let an operator review a bounded readiness poll in the web

**Status:** implemented, awaiting pull request.

## Goal

An operator can pick one successful push job, supply the readiness command and
review the exact bounded wait without the server guessing a condition.

## What changed

* Add `/optimisations/readiness`, reusing the checkout run picker and the
  one-press optimisation gate.
* The page lists successful push jobs; choosing one reveals a required
  one-line readiness command and **Read this workflow**.
* The review shows the file, commit, wait step, the original fixed duration (the
  bound), the observed wait mean, and the exact before/after block as untrusted
  text. It says a timeout fails the step and that passing CI alone cannot
  establish a saving.
* This slice adds no retention, approval, publication, CLI, MCP or Eli access;
  a later slice adds keeping and reopening.
* The cache page links to the new page; the router and API client carry it.
* The Eli guide, roadmap and handoff are updated.

## Refusals

* The route refuses a blank, multiline or oversized condition; the page disables
  the button until one valid line exists.
* The server refuses a job or workflow that the source binding cannot support.
* The page never infers a command and never offers a keep action before a
  retained envelope exists.

## Verification

* `npm run check` and unit tests pass.
* Browser tests cover picking a job, the exact inspect body, the bounded block,
  the no-retention statement and phone width, beside the cache and checkout specs.
* `make build`, `make lint`, `make test` and strict docs run before the pull
  request.
* No live GitHub, model or fleet.

## Next

A later slice retains the inspected preview and adds reopening; then CLI/MCP/Eli,
approval and publication follow. Test sharding remains a separate catalogue item.
