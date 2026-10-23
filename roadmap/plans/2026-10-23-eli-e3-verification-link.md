# E3 checkout preview: the verification comparison link

Date: 23 October 2026. Package: ZF-244. Follows reconciled draft publication.

## Outcome

A published preview points the operator at the next step honestly: comparison
is manual and inconclusive until a baseline and candidate exist. The preview
opens E2's Comparisons page with the installation, repository, workflow kind and
retained preview reference filled in. After the operator captures a report,
that page offers to link it back to the preview, and the preview then shows the
comparison and who linked it.

## Decisions

* **A link, not a measurement.** Zoomies does not read the merged pull request,
  choose runs or claim a saving. The comparison remains an explicit operator
  capture through the existing E2 route.
* **The comparison must be about the preview.** It must be a retained workflow
  comparison whose `revision_delta_id` is the preview ID, which is what the
  prefilled link supplies. A runner comparison or one naming another revision is
  refused. The server replays the comparison from retained input and checks its
  scope before recording the link.
* **One comparison per publication.** Migration 0121 adds
  `optimisation_publication_verifications`, keyed by the publication and
  cascading from it. It records the comparison ID, actor and time; the
  comparison itself is not rewritten. A repeat returns the existing link; a
  different comparison is refused.
* **No duplicated report.** The preview shows the comparison ID and a link to
  the retained report. It does not copy the report or turn its result into a
  saving claim. The audit row is `optimisation.comparison_linked`.
* **Prefill only the scope and reference.** The Comparisons page fills the
  installation, repository, kind and reference from the query when no retained
  comparison ID is being read. The hypothesis, window and run selections stay
  the operator's.
* **No merge detection or automatic selection.** Reading the pull request's
  merge state, choosing runs and writing a result onto the publication remain
  out of scope.

## Verification

Store tests cover the approved-and-published requirement, scope, idempotency,
one-link rule, audit rollback, expiry, cascade and corruption. Controller tests
cover a workflow comparison naming the preview, an unpublished preview, another
revision and a runner comparison. API tests cover the route authorisation,
request validation, shape, audit and replay. Browser tests cover the preview's
link and exact query, the Comparisons page opening prefilled, and linking a
retained report back to the preview on desktop and phone. All against fakes and
mocked routes only.
