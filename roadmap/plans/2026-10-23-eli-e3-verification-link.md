# E3 checkout preview: the verification link

Date: 23 October 2026. Package: ZF-244. Follows reconciled draft publication.

## Outcome

After a draft pull request is opened, the preview names the next step honestly:
comparison is manual and inconclusive until a baseline and candidate exist. One
link opens E2's Comparisons page with the installation, repository, workflow
kind and the retained preview reference already filled in, so the operator
chooses the runs rather than retyping the scope.

## Decisions

* **A link, not a measurement.** Zoomies does not read the merged pull request,
  choose runs or claim a saving. The comparison remains an explicit operator
  capture through the existing E2 route.
* **The comparison is a workflow comparison.** A checkout-depth change is a
  workflow change, so the link preselects `kind=workflow`. The retained preview
  ID is passed as the revision-delta reference; the E2 comparison records it as
  operator context, not as proof of execution.
* **Prefill only the scope and reference.** The Comparisons page fills the
  installation, repository, kind and reference from the query when no retained
  comparison ID is being read. The hypothesis, window and run selections stay
  empty; those are the operator's evidence.
* **No new route, migration or model call.** The existing preview card and
  comparison page carry the whole change. A comparison captured afterwards is
  still a separate retained record, not an automatic verdict.

## Not in this slice

Reading the pull request's merge state, selecting control and candidate runs
automatically, writing a comparison ID onto the publication, and any claim that
the change saved time.

## Verification

Browser tests cover the published preview's link and its exact query, and the
Comparisons page opening with the scope, workflow kind and reference filled in
on desktop and phone. No Go route or store change is introduced.
