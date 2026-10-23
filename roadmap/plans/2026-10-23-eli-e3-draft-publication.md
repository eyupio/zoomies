# E3 checkout preview: reconciled draft publication

Date: 23 October 2026. Package: ZF-244. Follows the attributed approval slice.

## Outcome

An operator can turn an approved checkout preview into one draft pull request.
The server re-reads the run, its branch, the live settings, the source and the
approval, refuses any change, commits the one reviewed line on a new branch and
opens a draft against the branch the push ran on. It never force-pushes and
never merges.

## Decisions

* **A third instance setting.** `optimisation.publish` is off by default and
  live. Publication needs `optimisation.source_reads`,
  `optimisation.workflow_edits` and `optimisation.publish`; the one-press card
  beside the approved preview turns the publish switch on and turns the two
  preview grants on with it. Turning it off leaves the preview grants alone.
* **Its own action.** `optimisations.publish` is separate from
  `optimisations.write`, so a token that may read, keep and approve previews
  need not be able to write to a repository. It is an operator action, as
  retaining and approving are.
* **The branch is the run's branch.** Checkout evidence now records the push's
  head branch. A preview retained before that has none; it stays readable and
  reproducible, and publishing it is refused with a request to preview again.
  Older evidence needs no migration because the field is optional.
* **The base is pinned.** The draft is based on the preview's head commit and
  refuses if the selected branch has moved. The new branch is derived from the
  preview ID, so a retry finds the same branch.
* **Fresh checks before writing.** The controller re-reads the run, job and
  source through the same admitted read as inspection, rechecks the live policy
  before and after, runs `optimisation.CheckCurrent`, and re-reads the approval
  and retained record. A changed head, source, branch, policy, approval or
  expiry is a refusal.
* **The permission trio.** GitHub needs Contents write, Pull requests write and
  Workflows write to change a file under `.github/workflows`. The App is probed
  before any write and a missing permission is a 403 that names it.
* **Reconciliation, not blind replay.** The branch name is deterministic. A
  retry reuses an existing branch only when its commit has the same tree,
  message and base, and reuses an existing pull request only when its title and
  body match and it is open. Anything different is preserved and refused. A
  lost pull-request response is reconciled by listing the branch's pull
  request; if that read fails, the branch is kept for the retry.
* **One publication per preview.** Migration 0120 adds
  `optimisation_publications`, keyed by the preview and cascading from it. It
  records the branch, base, both commits, the exact approval and preview
  digests, the actor, and the pull request number and URL. The store refuses a
  preview that is expired, unapproved or bound differently, and a repeat
  returns the existing record. Publishing is audited as
  `optimisation.preview_published`.
* **No untrusted text in the pull request.** The title, body and commit message
  are built from the preview ID and server-owned wording, not from repository
  content or the changed line. The diff on GitHub shows the exact change.
* **Not a merge.** Zoomies never merges, never force-pushes and runs no job.
  Verification stays `inconclusive`; the draft is for the repository's own
  review and checks.

## Not in this slice

E2 verification of the published change, CLI, MCP and Eli access. The retained
envelope remains private; publication is web-only.

## Verification

Store: binding, approval requirement, idempotency, audit rollback, expiry,
cascade and corruption. Controller: fresh source/branch/policy/approval checks,
one draft, retry reconciliation, permission refusals and no record after a
failed GitHub write. GitHub: pinned base, draft flag, one commit on a new
branch, reconciliation after a lost response, permission hint and conflicting
branch preservation. API: route authorisation, scopes, moved binding, missing
approval, live publish setting, audit and replay shape. Web: approval gates
publication, the publish button sends the approved binding, the draft link and
no-form state render, publishing turns on from the page, and a phone does not
overflow. All against fakes and mocked routes only.
