# E6: a findings-only CI review of a pull request

**Status:** accepted by the owner on 25 October as the first useful slice of E6.
Built: the pure review, the pinned GitHub read, the controller, the API route, the CLI
command and the MCP tool. Not built: any model, any comment on the pull request, any
stored review, any use of fleet capacity evidence, and an Ask Eli route.

## Goal

An operator or an agent can ask what a pull request changes in the repository's
workflows that Kennel Club would flag, and be told only what the change introduced,
with what was not looked at stated beside it.

## Owner decisions this follows

* **Findings only.** The review proposes nothing, edits nothing, executes nothing,
  approves nothing and merges nothing. It posts nothing to the pull request.
* **Trusted-base rules.** The rules are the controller's own checks, built into the
  binary. A pull request cannot rewrite them, and a changed instruction file is
  reported as a rule change and never read as an instruction.
* **Repository text is untrusted.** The review's sentences are Kennel Club's. The
  only repository text in it is a path git named, and every reader quotes paths as
  untrusted data.
* **No broad all-clear.** Every changed path the review did not judge is listed with
  a reason. `complete` is true only when every changed workflow was judged and nothing
  was cut or unreadable.
* **Reading source is a grant.** The review needs `optimisation.source_reads`, the
  setting that already gates every other read of repository source, and Kennel Club's
  workflow checks.

## How it is bounded

* Two commits are pinned: the merge base (what GitHub's file list is a diff against)
  and the head. The pull request is read again at the end, and a head that moved fails
  the read rather than mixing two heads.
* At most 46 GitHub requests: the pull request twice, the merge base, three pages of
  changed files, and a base and head read for at most 20 root workflow files in path order.
* A file GitHub will not return whole, a missing file the list says exists, or a
  file the parser refuses is unreviewed, never read as deleted.
* A finding is matched between revisions by check and job position, with counts, so
  a second unpinned action in an already-flagged job is still introduced.

## What is not done

* No capacity or trust-boundary evidence from the fleet is attached yet, so a
  finding is explained by Kennel Club's own text and not by what this fleet observed.
* No review summary is posted or updated on the pull request, so there is nothing
  to supersede when the head changes: a later push is a different review.
* No script, cache or runner-configuration review beyond the workflow checks.
