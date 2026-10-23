# E3 cache preview: one selected run and its source

Date: 23 October 2026. Package: ZF-244. Follows the retained setup history.

## Outcome

The controller can turn one selected successful push job and its exact GitHub
attempt into a cache preview: it reads the workflow and the one root lockfile at
the attempt's commit, matches the workflow's literal job and setup step names to
the fleet's job row, uses the retained setup samples for repeated cost, and runs
the pure cache preview. Nothing is retained and no route exists yet.

## Decisions

* **One bounded source read.** At most eight GitHub requests under a 20-second
  deadline: the attempt, the commit tree, the workflow, the one root lockfile and
  a final attempt recheck. No redirects, logs, history list or pagination.
* **Push only.** The attempt must be a completed successful push. This keeps the
  cache preview away from fork and pull-request-target cache poisoning. The
  commit tree must be complete, under ten thousand entries, and address both files
  as regular blobs with a matching content digest.
* **One supported lockfile.** The root files are tried in order; none is
  `ErrSetupCacheNotFound`, more than one is `ErrSetupCacheAmbiguous`. A nested,
  oversized, non-UTF-8 or changed file is refused.
* **Literal names, one job.** The workflow must have one pinned
  `actions/setup-node` step with a literal job name and step name. The selected
  fleet job must be the same installation and repository, the same run and
  attempt, the same head, successful, and carry exactly one completed step with
  that literal name and a positive duration.
* **The shared diagnostic admission.** The source read takes one of the same
  twenty-hourly admissions and the half-core-quota reserve as the checkout
  reader. The fleet's retained history supplies two to one hundred setup samples
  from the last ninety days; the selected job must be among them.
* **No retention.** The answer is the pure preview only. Approval, publication,
  storage, API, CLI, MCP, Eli and UI follow separately.

## Not in this slice

Persisting the preview, approval, publication, E2 verification, API routes,
CLI/MCP/Eli and UI. No model is called.

## Verification

GitHub tests cover the committed source and lockfile, exact job/step names,
push-only and successful attempts, one and two lockfiles, a truncated tree,
missing files, a changed final attempt and an oversized or corrupt file.
Controller tests cover the selected job binding, exact run/attempt/head and
literal names, two-sample history, the mean setup time, disabled policy, quota,
thin history, a wrong scope and a reader failure. No live GitHub, model or fleet.
