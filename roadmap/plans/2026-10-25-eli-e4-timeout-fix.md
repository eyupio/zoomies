# E4: a reviewed fix for a missing job timeout

**Status:** accepted by the owner on 25 October as the first useful slice of E4.
Step 1, the pure foundation, is built; the rest follow in order.

## Goal

An operator looking at a Kennel Club `ci.no_timeout` finding can supply how many
minutes the job should be allowed, read the exact change to the workflow file, keep
it, approve it, and have one draft pull request opened. The finding closes when
Kennel next reads the file on the branch and the job has a timeout, and not
because a pull request was opened.

## Owner decisions this follows

* **One fix first.** A missing timeout is the unambiguous one: the key is added to
  one job, nothing is removed, and no permission, pin or concurrency judgement is
  needed. Pins, concurrency groups and token permissions stay with the prompt a
  coding agent is handed, until each has its own evidence and refusals.
* **The minutes are the operator's.** Nothing in the file says how long a job
  should take, so the value is supplied, bounded to 1 to 1440, and never inferred.
  Too small a value fails a healthy job, and the preview says so.
* **Web-only writes, draft pull requests, never merged**, the same as the E3
  previews. The CLI, MCP and Eli read what was kept and write nothing.
* **A waiver is respected and stays visible.** A waived finding offers no fix.
* **It reuses the E3 policy and publisher.** `optimisation.source_reads` and
  `optimisation.workflow_edits` gate the read and the edit, `optimisation.publish`
  gates the pull request, and the existing single-file draft publisher opens it.

## How a finding is named

A Kennel finding names its file by Git blob SHA (its subject) and its job by
position (the evidence location). A fix is previewed only against content whose
Git blob SHA equals the one the finding names, so a preview cites the original
blob. If the file has changed, the blob has a new name and the preview is stale:
it is made again, never applied to other bytes.

## Steps

1. **Pure foundation (this change).** `workflow.TimeoutChange` and
   `workflow.TimeoutJobName` find the job, add `timeout-minutes` on the line after
   a one-line `runs-on` at its indentation, and keep every other byte, comment and
   line ending; `optimisation.PreviewTimeout` and `CheckTimeoutCurrent` bind it to
   the finding's blob and refuse a changed file, an out-of-range value, a job that
   already has one, a runs-on that spans lines, a flow-style job, a reusable job and
   a disabled policy. No route, migration or setting.
2. **Source and retention.** Read the one workflow file by its blob SHA, retain an
   immutable preview for 14 days with an audit row, and replay it, behind inspect,
   keep and replay routes and a migration.
3. **Web and read surfaces.** A fix opens from a `ci.no_timeout` finding on the
   repository page: the exact block, the minutes field, the waiver and stale-blob
   messages. The CLI, MCP and Eli read the same bounded preview.
4. **Approval and publication.** Attributed approval bound to the blob and digests,
   then one reconciled draft pull request through the existing publisher.
5. **Recheck.** The finding's page shows a published fix, and says it is closed only
   when a Kennel read after the merge no longer reports the job.

## Refusals

* A job that already has a timeout, a reusable-workflow job, a `runs-on` over
  several lines or in a flow mapping, a job written in flow style.
* Minutes below 1 or above 1440.
* A file that is not the blob the finding names, a partial file, a file outside the
  root `.github/workflows` directory, or one that the bounded parser refuses.
* Source reads or workflow edits switched off, and a waived finding.

## Verification

Step 1: the workflow package's table tests (LF and CRLF, a file with no final
newline, repeated `runs-on` lines, every refusal above), the optimisation package's
preview, currentness and refusal tests, and the package's purity test with
`crypto/sha1` added to its allowlist for Git's blob name. The rest are listed with
each step when it lands.

## Out of scope

Choosing the minutes from history, fixes for any other check, new checks, and a
score that moves when a fix is published.
