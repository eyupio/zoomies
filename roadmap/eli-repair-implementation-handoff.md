# Eli Repair implementation handoff

Last updated: 6 November 2026.

This file is the continuation record for the repository-first Eli Repair setup,
continuous PR watching and merge-conflict repair work. Read it before changing
these features. Update it in every PR for this initiative, including corrective
PRs. `AGENTS.md` makes that requirement part of the repository guidance.

This is the shared workstream record for the whole initiative. S1 through S3a
are merged on `main`, as are S3b's merge package, the `zoomies git-worker`
command, the worker image, the repository writer and fetch, the archive download
and the repository transport package. The rest of S3b and S4 are an open stack of
PRs, not merged to `main`: #1407, #1409, #1410, #1411, #1412, #1413, #1416,
#1417 and #1418. Each is based on the previous one, and #1407 is based on `main`.
The controller can prepare a repository, an agent can run the worker, the model
can resolve supported conflicts, the controller can publish the two-parent
commit, and the watches are on the REST API and in the UI, with each watched pull
request showing the attempts already made on it. No conflict repair has been run
against a live model, a real worker image or a real GitHub pull request yet.
Describing a later slice here does not add its runtime implementation to an
earlier branch. Continue from the top of the stack after checking the live PR
states, or merge the stack in order first.

## Current checkpoint

| Item | State |
| --- | --- |
| Latest slice | [#1418](https://github.com/eyupio/zoomies/pull/1418): the attempt history beside each watched pull request, stacked on #1417 |
| Investigated baseline | `568ec4017` on `main`; the stack's tip is `31c0cdc72` on `codex/eli-repair-attempt-history` |
| Working branch | `codex/eli-repair-attempt-history`, branched from `codex/eli-repair-ui` |
| Verified code checkpoints | S1 through S3a, and the S3b foundation, merged through `568ec4017`. The open stack's newest local checkpoints: #1407 `c926789cd`, #1409 `27a075c80`, #1410 `63a4a921b`, #1411 `d290125ad`, #1412 `3ac6909e1`, #1413 `89f10cf8f`, #1416 `871ee5c14`, #1417 `264c4f6c6`, this slice `31c0cdc72` |
| Implementation | S3b records decision 0016, adds `internal/gitmerge` (a bounded `git merge-tree` merge with no hooks, external drivers or repository code), the `zoomies git-worker` command with a bounded JSON protocol and a build-time toolchain check, and the separate Git worker image that `make image-gitworker` builds. Decisions 0017 and 0018 add the controller-built repository, the archive download and the agent transport. The controller serves the repository over the agent API, an agent runs the worker in a one-shot container, `internal/conflictresolve` asks the model only for the conflicted files, and the controller publishes a two-parent non-force commit. The watch API can list, admit, pause, resume and stop the work, and the UI chooses a repository's behaviour, admits it, shows what is being followed and controls it |
| Initial PR | [#1368](https://github.com/eyupio/zoomies/pull/1368), merged as `2d621add5` with all checks green |
| Follow-up PRs | [#1369](https://github.com/eyupio/zoomies/pull/1369), [#1372](https://github.com/eyupio/zoomies/pull/1372), [#1375](https://github.com/eyupio/zoomies/pull/1375), [#1378](https://github.com/eyupio/zoomies/pull/1378), [#1379](https://github.com/eyupio/zoomies/pull/1379), [#1381](https://github.com/eyupio/zoomies/pull/1381), [#1384](https://github.com/eyupio/zoomies/pull/1384), [#1386](https://github.com/eyupio/zoomies/pull/1386), [#1388](https://github.com/eyupio/zoomies/pull/1388) and [#1389](https://github.com/eyupio/zoomies/pull/1389) merged green; the handoff refresh merged as `07a49f664` |
| Current slice | S4 UI follow-up. The watched pull request's disclosure now lists every attempt recorded on that pull request, newest first, with the current attempt marked. Each row says how it ended, when it was queued, who began it and the commit it published, and links out to the attempt page, where raw source, model output and job logs stay behind their existing permission. The history is read per repository and narrowed to the watched pull request, a failed read still says so, and a history longer than the page read says older attempts may be missing |
| Next implementation | Finish S4: live validation of conflict repair against a real model, the real worker image and a real GitHub pull request; one row per repository instead of the separate Automatic repairs panel; repository-scoped activity for people who are not administrators, or an explicit statement that it stays administrator-only; notifications on publication, needs-help and completion |
| CI snapshot | None of #1407, #1409, #1410, #1411, #1412, #1413, #1416, #1417 or #1418 is merged. Local focused checks pass on each; their CI has been uneven on the self-hosted pool. Recheck every PR before merging and merge in stack order, starting with #1407. A queued, cancelled or stale run is not a result |
| S1 verification | Focused race tests, UI unit/type/lint/build, docs tests and browser regressions pass; exact coverage below |
| Persistent PR watcher | Due watches are observed, backfilled, retain check and conflict evidence, can start a bounded chain, and discover new PRs periodically or on a pull_request webhook. Per-PR and repository-wide watches are listed, admitted, paused, resumed and stopped through the API, and chosen, shown and controlled in the UI |
| Watcher claim cost | [#1392](https://github.com/eyupio/zoomies/pull/1392), a controller-load change that is not a slice, makes the due-watch claim look on the reader pool before it takes the writer. What is due, how a watch is leased and what an observation records are unchanged |
| Merge-conflict repair | End to end in the controller: a conflicted watch prepares the repository from three pinned commits, an agent runs the worker, the model resolves supported text conflicts, and the controller publishes a two-parent, non-force merge commit after rechecking every authority. The UI can now choose the behaviour, admit, pause, resume and stop the work, and show what each repository and pull request is doing; live end-to-end validation remains |
| Production configuration | No repository automation or model credentials changed by this work |

The automatic mode still starts from a failed `workflow_job`, attempts one
repair per failing head and refuses automatic repairs of its own published
commit. A watch admitted through the controller APIs, or through repair settings
now, can observe checks, retain evidence, start a bounded chain of up to three
repairs and discover new PRs. A watch admitted with the conflicts capability can
also resolve supported merge conflicts and publish a two-parent commit. The
repository and pull-request controls are on the hub and in repair settings: pause
one pull request, pause a repository's admission, resume either, and stop one or
all of them, each with what happens to work already under way.

## Agreed product behaviour

The user wants to select a few repositories, or all connected repositories,
and choose the work Eli should do. They should not have to understand the
internal identity, consent, installation and provider ownership model first.

The final primary flow is:

1. Select repositories. Provide search, individual selection and selection across
   pages. Show the selected count and any discovery cap. Never silently include
   repositories the user could not inspect. Adding future repositories is a
   separate explicit choice, not a side effect of selecting today's list.
2. Choose the behaviour:
   * **Baby sit PRs:** follow checks and branch compatibility, repair failures
     and conflicts, verify the result and keep watching until closed or paused.
   * **Fix failed checks only:** do the same continuous monitoring and bounded
     follow-up work for checks, without resolving conflicts.
   * **Fix merge conflicts only:** monitor compatibility and resolve supported
     conflicts, then monitor the resulting CI without making unrelated CI fixes.
3. Enable with the saved installation/team provider. State who pays, what source
   is sent to the provider, and that Eli may publish commits to PR branches.
   Show missing setup inline. Prefer an enabled installation default, or the
   sole enabled installation provider; do not arbitrarily choose among several.

Keep **Fix this PR once** on an individual PR. Personal-provider requests,
personal GitHub linking and workflow-edit permission remain available without
leading repository automation setup. They are separate authorisation paths.
Do not silently spend a person's provider on unattended work.

The normal post-setup screen should list repositories with their effective
behaviour, PRs watched, current activity, attention needed, last successful
observation and Pause. Each PR should say what Eli changed, the commit, the
current check outcomes, what it will do next and how to pause that PR.

## Invariants to preserve

* No PR merging, branch-protection bypass, automatic approval or force push.
  Updating a PR branch is distinct from merging the PR into its target branch.
* Keep source, model output, logs, titles and comments untrusted. Use the existing
  bounded reads, path validation and redaction. Do not expose hidden reasoning.
* Recheck policy, provider, installation, access, target head and target branch
  before publication. Restart or discard work after concurrent movement.
* A policy can always be paused without fixing its unavailable provider first.
  Resume performs the normal validation. Explain what happens to work already
  publishing and to commits already pushed.
* Preserve individual authorisation for sensitive logs and model responses.
  Repository activity summaries need their own verified repository scope; do not
  make automatic rows globally readable to solve the visibility problem.
* Missing checks, incomplete pages, missing permissions and GitHub errors do not
  establish success. Reported checks passing does not prove full merge readiness.
* Workflow edits remain off unless explicitly authorised. Model-generated
  changes still require normal review. Infrastructure, inaccessible evidence,
  unsupported fork targets and ambiguous conflicts can require a person.
* Preserve the one-writer store and package boundaries from `AGENTS.md`.
  Settings and durable work live in SQLite, not browser preferences or dotfiles.
* Existing policies retain their meaning. A migration must not turn one-attempt
  Automatic into continuous watching without the user's explicit choice.

## Findings from the baseline

| ID | Finding and evidence | Required outcome |
| --- | --- | --- |
| F1 | `RepairSetupPanel.svelte` leads with personal account setup, although unattended repairs use an installation provider | Repository-first setup; optional personal setup |
| F2 | `savePolicies` catches each failed save while its caller always emits a success toast | Accurate all-success, partial-success and all-failed results; retry only failed repositories |
| F3 | The hub's Needs you section contains only offers. A failed automatic repair can be hidden in collapsed history while the page says no failed PR needs the user | Current failed/stopped attempts appear visibly in Needs you; failed reads never show an all-clear |
| F4 | `handleSetEliPolicy` requires a provider even for disabling an Automatic policy with a deleted provider | Pause works through settings as well as the dedicated stop endpoint |
| F5 | `RepairChecks` reads only 100 check runs | Read subsequent pages within an explicit bound; never certify a partial result |
| F6 | A typed closed-PR result in `checkEliRepairs` is treated as a transient error until the one-hour timeout | Finish promptly with a truthful closed/merged outcome |
| F7 | The published plan summary is overwritten by a generic outcome; the UI retains original failed jobs but no new per-check outcome record | Retain the published fix summary and paths separately; add per-check results with the watcher |
| F8 | `setupSummary` treats a confirmed personal link as full readiness | Describe what is actually known; distinguish repository policy setup from per-PR readiness |
| F9 | Automatic repairs and their raw detail are administrator-only | Add repository-authorised activity metadata in a later slice, without widening raw response/log access |
| F10 | Model generation runs synchronously ahead of other repairs' check polling in `repairLoop` | Separate serial model worker and guarded check observer implemented in S2a |
| F11 | No durable subscription, backfill or authorised follow-up repair chain exists | Persistent watcher with lifecycle, quotas, leases, stop controls and recovery |
| F12 | Repair targets carry a base branch name but no pinned base SHA, mergeability state or three-way conflict data | A separate conflict detection, resolution and publication path |

The investigation used local fakes and browser API fixtures, not a live model or
real PR branch writes. Temporary reproducers are not the implementation tests.
Replace their assertions about buggy behaviour with regression tests for the
desired result.

## Delivery slices and acceptance criteria

### S1: setup, attention and reliable existing outcomes

Status: implemented and locally verified on the initial branch. Not yet merged.

Scope:

- [x] Put repository selection before personal account setup.
- [x] Reuse a suitable saved team provider and explain a missing one.
- [x] Select matching repositories across discovery pages with cancellation,
  bounded requests and no partial selection on a failed read.
- [x] Preserve existing policy settings on edit. Provide per-repository Pause
  and Resume without forcing users through the complete setup form.
- [x] Give bulk saves truthful counts, persistent row results and retry of only
  failed rows using the settings that were reviewed for that save.
- [x] Add a bounded latest-attempt attention query. Superseded attempts, successful
  retries, in-progress retries and cleared newer attempts must not resurrect an
  older failed attempt in Needs you. Keep existing history filters compatible.
- [x] Surface failed and blocked current attempts on the hub. Retain a way to
  inspect all results when more than one page needs attention.
- [x] Allow disabling an unusable Automatic policy. Keep enable/resume validation.
- [x] Paginate check reads, retain confirmed failures across a later page error,
  and leave capped/incomplete coverage unverified.
- [x] Finish check monitoring promptly after authoritative PR closure.
- [x] Retain the published fix summary and changed paths separately from outcome.
- [x] Update generated API contracts, user documentation and focused tests.
- [x] Filter configured repositories and show enabled/paused counts. Keep edits
  scoped to one repository, move focus to the form and offer a fresh setup path
  that resets workflow permission. Link the change summary to the actual fix commit.

S1 intentionally uses truthful labels for the existing implementation:
**Fix failed checks automatically**, **Offer a fix for failed checks**, and
**Manual requests only**. Final Baby sit/conflict options require S2 and S3.
It does not change repository access, backfill failures, run local source code,
or remove the own-commit recursion guard.

### S2: persistent PR watcher and check repair chains

Status: S2a through S2e are on `main`; S2f adds per-check evidence, S2g adds
periodic new-PR discovery and S2h adds a pull_request wakeup. Conflict repair and
the final UI are not implemented. Split the remaining execution and UI into
reviewable PRs; update this file in every one.

S2a scope:

- [x] Keep model generation serial and observe published repair commits in an
  independent guarded loop. Do not claim or generate repairs in the observer.
- [x] Preserve recovery of uncertain working attempts, panic restart/reporting,
  cancellation, controller shutdown, fence, lease and transfer-drain gates.
- [x] Separate comment-ID persistence from lifecycle writes so a late comment
  cannot reopen a settled attempt or change its polling clock. Bound comments
  to ten seconds and skip notices whose attempt state has already changed.
- [x] Include GitHub client resolution in each check's thirty-second read budget.
- [x] Add the watcher loop that consumes due watches in S2c.
- [x] Add existing-PR discovery and backfill in S2d.
- [x] Add bounded repair chains in S2e.
- [x] Add per-check evidence in S2f.
- [x] Add periodic new-PR discovery in S2g.
- [x] Add pull_request webhook wakeups in S2h.
- [ ] Add conflict repair and the final repository and PR controls.

S2b scope, merged in #1372:

- [x] Add additive migration 0138 for durable watches and an optional `watch_id`
  on repair attempts. Watches carry installation/repository/PR identity, owner
  and policy provenance, the policy revision, check/conflict capabilities,
  enabled or paused state, head/base identity, last observation, next check,
  a work lease and a stop reason.
- [x] Admit a watch only through an explicit controller request that validates
  the current enabled Automatic policy, its installation provider, the live
  installation and the open pull request. A policy, webhook or restart never
  creates or resumes a watch.
- [x] Refuse the conflict capability with a clear message until the S3 conflict
  path exists, so no unavailable choice is advertised.
- [x] Give each watch a short work lease, and release a lease left by a previous
  controller at startup. Observation is read-only, so the interrupted watch
  becomes due again; an uncertain publication stays interrupted.
- [x] Keep watch state separate from attempt state. Only an explicit admission
  resumes a paused watch, and no existing policy changes to continuous watching.
- [x] Consume due watches with a GitHub observation loop, added by S2c.

S2c scope, merged in #1375:

- [x] Add migration 0139 for the last check state on a watch.
- [x] Start a guarded `eli-repair-watches` loop beside the model worker and the
  published-commit observer. It claims at most one due watch at a time through
  the existing lease and honours the controller fence, lease and transfer drain.
- [x] Pause a watch when its repository policy is no longer enabled or automatic,
  and stop it when the pull request is closed, merged, from a fork or no longer
  writable. No model work and no publication happen in this loop.
- [x] Read the pull request and, when the watch has the checks capability, its
  current check state. Record passed, failed, waiting or unknown, and never treat
  an incomplete or failed read as passing.
- [x] Reschedule pending and failing checks promptly, passed checks more slowly,
  and an error at the GitHub Retry-After time when GitHub gave one.
- [x] Start a repair from a failed watch and bound a chain, added by S2e.

S2h scope, merged in #1388:

- [x] Accept a verified `pull_request` opened or reopened delivery and admit its
  watch immediately, but only where an explicit repository decision exists.
- [x] Re-read the pull request through the GitHub client rather than trusting
  the delivery for head, base, fork or draft state.
- [x] Leave an existing paused or stopped watch untouched and do nothing for a
  repository with no repository-wide decision.
- [x] Keep the periodic five-minute pass as the fallback when the App is not
  subscribed to `pull_request`; that subscription remains optional.

S2g scope, merged in #1386:

- [x] Add migration 0142 for an explicit repository-wide watching decision,
  separate from any one PR's watch. A per-PR admission never creates one.
- [x] Scan the least recently discovered subscriptions every five minutes, with
  at most twenty per pass, and record each attempt so the next pass rotates
  through the remaining repositories.
- [x] Reuse the repository decision's owner, capabilities and current policy,
  and admit only newly opened pull requests through create-only admission.
- [x] Leave paused and stopped pull requests alone and skip a repository whose
  policy is no longer automatic.
- [ ] React to a pull_request webhook immediately. This PR is bounded periodic
  catch-up only; the App subscription change and webhook path remain.

S2f scope, merged in #1384:

- [x] Read bounded check-run and legacy commit-status evidence for a watched
  head, preserving the producer and each outcome alongside the overall state.
- [x] Store at most fifty checks per observation, with the total and a truncation
  flag. An incomplete read is still recorded as unknown, not passing.
- [x] Keep the evidence on the watch with the head it describes, and clear it
  when an explicit admission moves the watch to a different head.
- [ ] Expose the evidence in the API and UI, and map required-status coverage.
  That belongs with the final controls.

S2e scope, merged in #1381:

- [x] Add migration 0140 for a watch's chain count, last failure signature and
  stop reason.
- [x] Start at most one repair per watched head. The dedup key and the chain
  increment are written in one store transaction, so a redelivered check or a
  repeated observation cannot spend a second attempt.
- [x] Stop the chain after three attempts and record why. A signature is built
  from the failed job names and conclusions; a healthy observation resets the
  chain before a later incident.
- [x] Refuse a queued watched repair when its watch is paused or stopped, and
  check again after model work before publication.
- [ ] Retain per-check producers and required-status coverage. The chain uses one
  overall state and the failed job names, not a full check record.

S2d scope, merged in #1378:

- [x] Add `RepairOpenPulls` to the GitHub layer with bounded pages, open-state
  filtering and a discovery cap. A failed page returns no partial list.
- [x] Add repository-wide admission: validate the policy and provider once, read
  the open pull requests once, then admit eligible watches from that list.
- [x] Exclude forks, drafts and pull requests without a writable head branch in a
  report, rather than silently not watching them.
- [x] Never resume an existing watch during repository admission. An enabled
  watch is counted as existing; a paused or stopped one stays that way with its
  reason visible.
- [x] Record the report cap so the caller can say discovery was incomplete.
- [ ] Expose repository-wide admission through the API and UI, with new-PR
  discovery and webhook wakeups. Those remain S4 work.

Data and policy:

- Add additive store migrations for durable watches and attempts linked to a
  watch. A watch needs installation/repository/PR identity, owner or policy
  provenance, capability selection, enabled/paused state, last observation,
  current head/base identity, next check time, policy revision and stop reason.
- Keep watcher state separate from repair attempt state. Checks passing returns
  the watch to monitoring; it does not end the subscription.
- Preserve legacy policy defaults. Map new behaviour to explicit check/conflict
  capabilities, not contradictory boolean combinations in the public interface.
- Persist per-PR overrides. Pausing one PR wins over repository automation until
  an explicit resume. Repository disable stops admission for all its PRs.
- Record who authorised the watch, who pays, and which policy revision was used.

Observation and reconciliation:

- On enable, enumerate existing eligible open PRs and inspect their current
  checks. New PRs and new commits must be discovered as well. Exclusions such as
  forks and drafts must be visible, not silently counted as watched.
- Wake on PR lifecycle/target changes, check/status changes, Actions activity and
  target-branch pushes. Update App subscriptions and existing-installation setup
  guidance together. Authenticate webhook deliveries before any action.
- Use bounded periodic reconciliation for missed deliveries and restarts. Honour
  installation API limits, Retry-After and backoff. Deduplicate out-of-order
  deliveries and coalesce simultaneous failures on one PR.
- Observation is separate from the serial model worker in S2a. Add durable watch
  work leases and enforce one publishing attempt per PR for the later watcher.
- Identify check producers, workflow/run attempts and matrix members. Read check
  runs and legacy statuses. Keep original failures separate from checks on the
  published repair commit. Show pending, failed, passed and unknown explicitly.
- Check required-status/ruleset coverage where permitted. Keep missing coverage
  distinct from no requirements and from full merge readiness.

Bounded repair chains:

- Keep the existing own-commit guard for legacy Automatic. Permit follow-up on an
  Eli commit only through a live, explicitly authorised watch and its recorded
  attempt lineage.
- Start with at most three repair attempts per incident/chain. Normalise failure
  signatures and stop on the third identical failure; on repetition change the
  diagnosis, rather than replaying the same edit. Document the exact counters.
- New delivery IDs, check reruns and controller restarts must not reset counters.
  New incidents after a period of healthy observation need explicit semantics.
- Retry infrastructure-only failures at most once when evidence supports it.
  Do not hide deterministic failures by disabling tests, weakening assertions or
  skipping checks. Missing credentials or inaccessible diagnostics need a person.
- Stop, pause and authorisation revocation must be checked before publication.
  An interrupted publication has uncertain outcome: reconcile GitHub before any
  retry, rather than blindly replaying the write.

Minimum tests: duplicate and reordered deliveries; already-red PR at enable;
missed webhook; restart in each state; successful and failing reruns; latest
head movement; target movement; three repeated signatures; exhausted chain;
pause during generation and during publication; revoked provider/access;
missing permission; pagination/caps; multiple repos; one busy model not delaying
other PR observations; green PR remaining watched; closed PR terminating.

### S3: merge-conflict detection and repair

Status: S3a detection and evidence is merged. S3b is implemented in the open
stack: the Git worker and image, the controller-built repository, the agent
transport and one-shot run, bounded model resolution, the merged-tree change
list and the two-parent non-force publication are all present, and a conflicted
watch starts them. Live model and real-PR validation remain. Required even when
CI never starts: ordinary `pull_request` workflows do not run while the PR has a
merge conflict.

S3a scope, merged in #1389:

- [x] Read GitHub's current mergeability without guessing while it is null, and
  pin the head, target tip and merge base for that head.
- [x] Keep that evidence on the watch with the head it describes, written only
  by the observation that still holds the lease.
- [x] Detect target-branch movement through the pinned base SHA, even when the
  head did not move.
- [ ] Compute or resolve a three-way merge in S3a; that work starts in S3b.

S3b scope, this PR only:

- [x] Record [decision 0016](decisions/0016-eli-conflict-repair-runs-in-a-separate-git-worker.md):
  a separate bounded Git worker, never Git inside the controller.
- [x] Add `internal/gitmerge`: verify the pinned merge base, run
  `git merge-tree` with hooks and external merge drivers refused, and return the
  clean tree or bounded ancestor/base/head text for supported conflicts.
- [x] Refuse binary, symlink, submodule, delete/modify and mode-changing
  conflicts explicitly, and stop at the configured conflict and byte budgets.
- [x] Add the worker command and image, fetch the sanitised bare repository, and
  transport the request and result from the controller.
- [x] Run the worker from an agent in a one-shot container against the prepared
  workspace, with no network and no credential.
- [x] Ask the model for a resolution in `internal/conflictresolve`, bounded to
  the conflicted files and refusing any answer that leaves a marker.
- [x] Build the two-parent commit from the worker's merged-tree changes and the
  model's resolved files.
- [x] Advance the pull request branch without force after rechecking the head and
  the pinned target tip.
- [x] Recheck pause and authorisation at the publication boundary, and wire the
  watch to the worker, model and commit.

Detection and evidence:

- Pin actual target branch, base tip SHA, head SHA and merge base. Handle GitHub's
  temporarily unknown mergeability with a retry, not a guessed conflict.
- Detect target-branch movement even when the PR head did not move. Deduplicate
  work by watch, head/base pair and conflict identity.
- Compute a real three-way merge. Preserve all cleanly merged changes and all
  unaffected file modes. Give the model bounded ancestor, base and PR-side text
  only for supported conflicting regions, together with relevant intent/context.
- Classify unsupported binary, symlink, submodule, delete/modify, rename and
  protected-path conflicts explicitly. A conflict-marked tree is not a valid fix.

Publication:

- Default to merging the pinned base into the PR branch, preserving history.
  The resolved merge commit needs both parents, with the PR head first.
  The existing single-parent `CommitRepair` cannot provide this contract.
- Validate the complete merged tree and the model's allowed resolution edits.
  Do not discard base changes by treating a conflict as an ordinary head edit.
- Re-read head/base identity and authorisation before publication; discard and
  recompute after movement. Update the PR branch without force. Never call the
  PR merge endpoint as part of conflict resolution.
- Return to the watcher and inspect CI on the actual published commit. Do not
  report a conflict resolution as verified just because the merge tree exists.

The execution decision is recorded in
[decision 0016](decisions/0016-eli-conflict-repair-runs-in-a-separate-git-worker.md),
with the repository built by the controller in
[decision 0017](decisions/0017-the-controller-builds-the-workers-repository.md)
and run by an agent host in
[decision 0018](decisions/0018-an-agent-runs-the-git-worker.md). The worker runs
with no network, no credential, hooks and external merge drivers refused, and
only the prepared workspace mounted. The controller publishes the commit; the
worker never holds the GitHub token.

Minimum tests: competing text edits preserving both sides; changes on only one
side; rename and delete/modify policy; multiple merge bases; protected workflow
conflict; large source cap; binary/symlink/submodule refusal; head or base moves
during model work; two-parent history; non-force update failure; pause/revocation;
post-resolution CI failure; conflict-only mode not making unrelated code fixes.

### S4: final simplified controls and repository visibility

Status: the backend is in #1416, the first half of the UI in #1417, and the
attempt history beside the current pull request in #1418. Repair settings offer
the three behaviours, admit repository watching when a repository is saved, and
show what is being watched; the hub lists each repository and its watched pull
requests with the pause, resume and stop controls; one pull request shows its
intended action, evidence, first failure, published commit and files, the checks
now, and every attempt made on it. Not yet done: folding the Automatic repairs
panel into these repository rows so one repository is one row, repository-scoped
activity for people who are not administrators, and notifications. Nothing here
has run against a live model, a real worker image or a real GitHub pull request.

- Expose the final three behaviour choices only with actual capability support.
  Done in #1417: the three choices are the two capabilities, and a repository
  recording neither is not offered a fourth.
- Select across GitHub connections while preserving each repository's verified
  installation identity. Explain discovery caps and partial enablement.
  Partly done: setup keeps each repository's connection, the admission report
  says when discovery stopped at its page limit, and each repository reports its
  own result. Choosing repositories across several connections in one pass is
  still one connection at a time.
- Show effective mode, watched PR count, last successful observation, active
  work, failures needing help and pause/resume for each repository.
  Done in #1417, on the hub and in repair settings.
- Show one current PR view plus the attempt history: intended action, evidence,
  original failure, actual changed files/commit, new check results and next action.
  Done in #1417 for the current pull request, and the history beside it in #1418:
  the disclosure lists every attempt on that pull request, newest first, marks the
  current one and links each to its own page. Raw source, model output and job
  logs still stay on those pages behind their existing permission.
- Add repository-scoped activity access using verified repository membership.
  Keep raw source/model responses and job-log access independently restricted.
  Not done: the watches are administrator-only, as #1416 left them, and the rows
  here do not widen raw access to solve it. This stays administrator-only until a
  separate permission change, and the UI does not offer the section to anyone
  else.
- Notify on publication, needs-help and completion using existing in-app/event
  patterns. Update one GitHub progress comment without comment spam. The user
  must not need an open tab for the backend to keep watching.
  Not done. Watching is server-side already, but nothing notifies yet beyond the
  section saying a pull request needs a person.
- Add pause PR, pause repository and stop-all controls with truthful in-flight
  semantics. Preserve policy/provider choices for resume.
  Done in #1417.
- Validate desktop/mobile, keyboard/focus behaviour, accessible status changes,
  empty/loading/error/partial states and exact wording of current vs historical
  outcomes. Do not show an all-clear when an observation failed.
  Done in #1417 for the pages it adds: 26 Playwright cases on both devices, the
  repairs pages of the accessibility audit, and unit cases for every sentence a
  failed observation produces. F7's per-check outcomes are visible here too: the
  retained fix summary and changed paths, and each check's outcome for the head
  last observed.

## Relevant code and tests

| Area | Entry points |
| --- | --- |
| Setup | `web/src/routes/EliRepairSetup.svelte`, `web/src/lib/repairs/RepairSetupPanel.svelte`, `RepositoryPicker.svelte` |
| Hub, watching and attempt | `web/src/routes/EliRepairs.svelte`, `EliRepairAttempt.svelte`, `web/src/lib/repairs/`, in particular `watches.ts` (the three behaviours, one watch or repository summarised, the control copy and its in-flight promise), `watchActions.ts`, `WatchedRepositories.svelte` and `PullRequestWatch.svelte` |
| Admission, work and verification | `internal/controller/eli_repairs.go`, `eli_experience.go`, `eli_automatic.go` |
| GitHub source and commit operations | `internal/github/eli_repairs.go`, `manifest.go`, `internal/controller/webhooks.go` |
| Bounded model editor | `internal/prrepair/repair.go`, `context.go`, `supplied.go`, `diff.go` |
| Durable state | `internal/store/queries_eli_repairs.go`, `queries_eli_repair_history.go`, `queries_eli_automatic.go` |
| API | `internal/api/handlers_eli_repairs.go`, `handlers_eli_watches.go`, `api/openapi.yaml` |
| Conflict merge and publication | `internal/gitmerge/merge.go`, `internal/gitbare`, `internal/gitfetch`, `internal/controller/eli_merge_session.go`, `eli_merge_attempts.go`, `eli_conflict_repair.go`, `internal/conflictresolve/resolve.go` |
| Agent worker and backend | `internal/agent/merge.go`, `internal/backend/mergeworker.go`, `internal/agent/protocol.go` (`FeatureMergeWorker`) |
| Related read-only check evidence | `internal/github/pr_delay.go`, `internal/analysis/merge_delay.go`; these do not establish full merge readiness |
| Backend regressions | `internal/github/eli_repairs_test.go`, `internal/controller/eli_repairs_test.go`, `internal/api/eli_automatic_test.go`, `internal/store/eli_repair_history_test.go`, `internal/store/eli_repair_watches_test.go` and `internal/store/idle_claims_test.go` (the watch claim) |
| UI regressions | `web/tests/repairs.spec.ts`, `repair-hub.spec.ts`, `repair-attempt.spec.ts`, `repair-setup.spec.ts`, `repair-watching.spec.ts`, `web/unit/repair-*.test.ts` (including `repair-watches.test.ts`) |

The repository's `.claude/skills/babysit/SKILL.md` describes a maintenance skill,
not a shipped product watcher. Its bounded-loop principles are relevant; do not
claim that installing that skill enables this feature.

## Validation and PR ledger

Record exact commands, outcomes and omissions here in every PR. Do not replace
the previous PR's evidence with a claim that the entire feature is tested.

| PR | Scope | Validation | Remaining work |
| --- | --- | --- | --- |
| [#1368](https://github.com/eyupio/zoomies/pull/1368), open | S1, UX follow-up and this handoff | Local checks below pass; latest CI pending | Review S2a in #1369, then durable watches; S3 and S4 remain |
| [#1369](https://github.com/eyupio/zoomies/pull/1369), open | S2a: independent check observation and comment-write isolation | Repair and lifecycle race tests, vet, docs and formatting pass; no completed check failure at this checkpoint | Durable watches and admission/recovery, then check evidence and repair chains |
| [#1372](https://github.com/eyupio/zoomies/pull/1372), merged | S2b: durable watch state, explicit admission, work leases and restart recovery | Focused repair and migration race suites, vet, docs and formatting pass; merged in the #1368 stack | Watcher loop, existing-PR discovery and bounded repair chains |
| [#1375](https://github.com/eyupio/zoomies/pull/1375), merged as `77a821023` | S2c: due-watch observation loop and check state | Focused repair and migration race suites, vet, staticcheck, docs and formatting pass; no UI or public API change | Existing-PR discovery, bounded repair chains and per-check evidence |
| [#1378](https://github.com/eyupio/zoomies/pull/1378), merged as `85a7f6ca1` | S2d: repository-wide watch discovery and backfill | Focused repair race suites, vet, staticcheck, docs and formatting pass; merged green | Bounded repair chains and per-check evidence |
| [#1379](https://github.com/eyupio/zoomies/pull/1379), merged as `80ed57e94` | S2d correction: create-only backfill admission in one transaction | Focused repair and store race suites, vet, staticcheck, docs and formatting pass; merged green | Bounded repair chains and per-check evidence |
| [#1381](https://github.com/eyupio/zoomies/pull/1381), merged as `8a827168d` | S2e: bounded repair chains for watched failures | Focused repair and store race suites, full store suite, vet, staticcheck, docs and formatting pass; merged green | Per-check evidence, then conflict repair and UI |
| [#1384](https://github.com/eyupio/zoomies/pull/1384), open | S2f: per-check evidence on watched heads | Focused repair and store race suites, full store suite, vet, staticcheck, docs and formatting pass; no UI or public API change | Conflict repair and the final controls |
| [#1386](https://github.com/eyupio/zoomies/pull/1386), open, stacked on #1384 | S2g: periodic new-PR discovery | Focused repair and store race suites, vet, staticcheck, docs and formatting pass; no UI or public API change | Webhook wakeups, conflict repair and the final controls |
| [#1388](https://github.com/eyupio/zoomies/pull/1388), merged | S2h: pull_request webhook wakeup | Focused repair and webhook race suites, vet, staticcheck, docs and formatting pass; merged green | Conflict repair and the final controls |
| [#1389](https://github.com/eyupio/zoomies/pull/1389), merged as `039aa7b1a` | S3a: merge-conflict detection and evidence | Focused repair and conflict race suites, full store suite, vet, staticcheck, docs and formatting pass; merged green | S3b resolution and publication, then S4 UX |
| [#1391](https://github.com/eyupio/zoomies/pull/1391), open | S3b: bounded Git merge worker foundation, and the `--merge-base` portability fix below | `internal/gitmerge` race tests against real repositories on Git 2.39.5, vet, staticcheck, docs and formatting pass; no runtime surface yet | Worker command, image, transport, model resolution and publication |
| [#1395](https://github.com/eyupio/zoomies/pull/1395), merged into #1391's branch as `6a9cb316` | S3b: the `git-worker` command, its bounded protocol and its image target | `internal/gitmerge` and `cmd/zoomies` race suites, the docs suite from a clean worktree, build, vet and formatting pass; the image is built by `make image-gitworker` and #1398 wires its publication | The controller fetch, transport and cleanup |
| [#1396](https://github.com/eyupio/zoomies/pull/1396), open, stacked on #1391 | S3b: decision 0017 and `internal/gitbare`, the bare repository the worker merges | `internal/gitbare` race tests, including an end-to-end merge of a reconstructed repository through `internal/gitmerge`, vet and formatting pass; no caller yet | The archive download, the controller fetch, the worker transport, timeouts and cleanup |
| [#1399](https://github.com/eyupio/zoomies/pull/1399), merged into #1396's branch as `fce82c8e`, stacked on #1396 | S3b: decision 0018, the archive reader and `internal/gitfetch` | `internal/gitbare` and `internal/gitfetch` race suites, including an end-to-end merge of what the fetch produced, docs, vet and formatting pass; nothing downloads an archive yet | The archive download over HTTP, then the `merge_conflict` task and the agent transport |
| [#1402](https://github.com/eyupio/zoomies/pull/1402), open, stacked on #1399 | S3b: the archive download, the commit read and the `gitfetch` source adapter | Archive and commit tests through the production factory path, the no-credential-to-storage assertion, `staticcheck ./...`, build, vet and formatting pass | The controller adapter and the agent transport |
| [#1404](https://github.com/eyupio/zoomies/pull/1404), open, stacked on #1402 | S3b: `gitbare.Pack` and `Unpack`, and the controller's attempt registry | Round-trip tests through the worker, the read-only loose objects, every refusal, the attempt lifecycle, `staticcheck ./...`, build and formatting pass | The `merge_conflict` task and the endpoints the agent uses |
| [#1407](https://github.com/eyupio/zoomies/pull/1407), open, stacked on `codex/eli-git-merge-worker` | S3b: the `merge_conflict` task and `QueueMergeConflict` | Task payload, queue key and lease tests, the older-agent compatibility suite, build and formatting pass | The two endpoints over `mergeSession` and the agent handler |
| [#1409](https://github.com/eyupio/zoomies/pull/1409), open, stacked on #1407 | S3b: pull and answer endpoints over `mergeSession`, their agent transport client, and the prepared attempt's three pins on the task | Race tests for the controller session, the agent transport, the API routes and the task payload; `make openapi`, vet and formatting pass; no agent handler, model or publication | The agent handler that pulls, unpacks, runs the worker image and posts the answer, then model resolution and two-parent publication |
| [#1410](https://github.com/eyupio/zoomies/pull/1410), open, stacked on #1409 | S3b: the agent runs the prepared merge in a one-shot worker container, posts its answer and removes the workspace; the Docker and Podman backends gain the worker run | Race tests for the agent handler, the container backend, the controller result path and the docs; vet and formatting pass; no model resolution or publication | Model resolution, the two-parent commit and non-force publication, then S4 UX |
| [#1411](https://github.com/eyupio/zoomies/pull/1411), open, stacked on #1410 | S3b: bounded model resolution of the text conflicts a worker returned, producing complete resolved files and rejecting markers, protected paths and oversized conflict sets | Race tests for the resolver, vet, staticcheck and formatting pass; no caller, two-parent commit or publication | The two-parent commit and non-force publication, then wiring a conflicted watch to the worker, model and publisher |
| [#1412](https://github.com/eyupio/zoomies/pull/1412), open, stacked on #1411 | S3b: the worker's merged-tree change list, applying resolved files to it, and a two-parent commit with the head first that advances the branch without force after rechecking head and base | Race tests for the change list, the apply step and the GitHub commit path; `make openapi`, vet, staticcheck, docs and formatting pass; no watch wiring or pause/authorisation boundary | Wire a conflicted watch through the worker, resolver and commit, with pause and authorisation rechecked before the ref update |
| [#1413](https://github.com/eyupio/zoomies/pull/1413), open, stacked on #1412 | S3b: the conflicted watch starts the worker, the answer loop runs the model and publishes the two-parent commit, admission accepts conflicts, and agents advertise the worker capability | Race tests for the start, answer, pause, unsupported-conflict and already-waiting-answer paths; vet, staticcheck, docs and formatting pass; no live model, real worker image or real GitHub mutation | S4 controls and live end-to-end validation |
| [#1416](https://github.com/eyupio/zoomies/pull/1416), open, stacked on #1413 | S4 backend: REST routes to list, admit, pause, resume and stop per-PR watches, and to list, admit, pause and resume repository-wide decisions, with owner, role and audit handling | API and store tests, spec/router coverage, `make openapi`, vet, staticcheck, docs and formatting pass; no UI | The final UI and live end-to-end validation |
| [#1417](https://github.com/eyupio/zoomies/pull/1417), open, stacked on #1416 | S4 UI: repair settings lead with the three behaviours and admit repository watching on save; the hub and repair settings list each repository's effective behaviour, watched count, last successful observation, activity and attention with its pull requests underneath; one pull request shows its intended action, evidence, first failure, published commit and files and the checks now; pause one pull request, pause a repository, resume either and stop all, each saying what happens to work already under way; a failed read never shows an all-clear | UI unit, type, lint and production build pass; 26 Playwright cases on desktop and mobile for the watching spec, the repairs, setup, hub and automatic suites and the repairs accessibility audit; Go and docs tests unchanged and green; no live model, worker image or GitHub PR | Live end-to-end validation; the attempt history beside the current pull request; one row per repository; non-administrator activity; notifications |
| [#1418](https://github.com/eyupio/zoomies/pull/1418), open, stacked on #1417 | S4 UI follow-up: the watched pull request's disclosure lists every attempt on that pull request, newest first, with how it ended, when, who began it and the commit it published, and links each to the attempt page; the list is narrowed to the watched pull request, a failed read says so, and a truncated history says older attempts may be missing | UI type, lint and production build pass; 1,335 unit cases pass including two new history-order cases; 28 Playwright cases on desktop and mobile for `repair-watching`, including the new history case; the repairs pages of the accessibility audit pass on both devices; no Go or API change | One row per repository; non-administrator activity or an explicit administrator-only statement; notifications; live end-to-end validation |
| [#1392](https://github.com/eyupio/zoomies/pull/1392), open | Controller load, not a slice: the S2c due-watch claim looks on the reader pool before taking the writer, alongside the enrichment claim, the event stream's figures and the database file's mapping | Full `make test` race suite (55 packages), vet, staticcheck, gofmt and `go mod tidy -diff` pass; no UI, public API or watch behaviour change | Unchanged: S3b resolution and publication, then S4 UX |

S4 attempt-history validation on 6 November 2026:

- `npm run check` in `web`: zero errors and zero warnings in both configurations.
- `npm run test:unit` in `web`: 1,335 passed, zero failures, including the two new
  `pullAttempts` cases for ordering, repository and pull-request narrowing, and a
  timestamp that cannot be read.
- `npm run lint` in `web`: prettier, eslint and the block-spacing check pass.
- `npm run build`: the app shell at 149.1 KB of its 200 KB budget and the status
  page at 29.5 KB of 30 KB.
- Browser tests against a locally built binary, desktop and mobile, on a
  temporary config using the shared fixture only (port 8099, `reuseExistingServer`
  false): `tests/repair-watching.spec.ts` ran 28 cases, all passed, including the
  new attempt-history case on both devices. The repairs pages of
  `tests/a11y.spec.ts` ran 12 cases, all passed, on both devices.
- No Go code changed: `go test ./internal/docs` is run below for the handoff edit.
- Not validated: a live model, the real Git worker image and a real GitHub pull
  request, which remain the rest of S4.


S4 UI validation on 6 November 2026:

- `npm run check` in `web`: zero errors and zero warnings in both configurations.
- `npm run test:unit` in `web`: 1,333 passed, zero failures, including the 16 new
  cases for `watches.ts`.
- `npm run lint` in `web`: prettier, eslint and the block-spacing check pass.
- `npm run build`: the app shell at 149.1 KB of its 200 KB budget and the status
  page at 29.5 KB of 30 KB.
- `make openapi`: both generated copies are unchanged, because this slice adds no
  API shape. It consumes the routes #1416 already put on the API, and
  `docs/api-surface.md` now describes them.
- Browser tests against a locally built binary, desktop and mobile: the new
  `tests/repair-watching.spec.ts` ran 26 cases and passed. The existing repairs
  suites (`repairs`, `repair-setup`, `repair-hub`, `repair-automatic`,
  `repair-page`) ran 160 cases with 158 passes; the two failures were the one
  assertion the new form moved behind its fold, corrected in
  `tests/repairs.spec.ts`, and its rerun passed all ten cases. The repairs pages
  of the accessibility audit passed on both devices.
- `go test ./internal/docs -count=1`, `go vet` on `internal/api`,
  `internal/controller`, `internal/store` and `internal/docs`, and `staticcheck`
  on the first two: pass. No Go code changed in this slice.
- Not validated: a live model, the real Git worker image and a real GitHub pull
  request, which is the rest of S4.

S1 validation on 3 November 2026:

- `go test -race -count=1 -run 'Eli|Repair|NeedsAction|PausingRepairs|ClosedPR'`
  on `./internal/github ./internal/controller ./internal/api ./internal/store`:
  all four packages pass, including the new index migration.
- `go vet` on those four packages: pass.
- `go test ./internal/docs -count=1`: initially passed before the new handoff
  was tracked. The tracked-file rerun found its root placement invalid; the S1
  follow-up moves it into `roadmap/`, updates the index and reruns the suite.
- `make openapi`: both generated contracts regenerated with the repository tools.
- `npm run check` in `web`: zero errors and zero warnings in both configurations.
- `npm run test:unit` in `web`: 1,313 passed, one skipped, zero failures.
- `npm run lint` and `npm run build` in `web`: pass, including asset budgets.
- Browser tests against the rebuilt local controller: the affected desktop/mobile
  suites ran 246 cases, with 244 passes and two outdated assertions. The assertions
  were corrected (configured ask-first policies do not need setup; secondary
  navigation is behind More on mobile). A focused six-case desktop/mobile rerun
  passed, covering both corrections and a new selection-cancellation case on
  both devices. This covers 248 distinct current browser cases across the two
  runs; it is not a claim that one 248-case run was executed.
- New fixture captures in `design/eli-repair-review/` show setup on desktop/mobile
  and a failed automatic repair visible in Needs you.
- `git diff --check`: pass.

### S1 CI correction in PR #1368, 3 November 2026

The full Linux arm64 run on `a640c3da` found that migration
`0136_eli_repair_latest_attempt.sql` was missing from `shippedMigrations` in
`internal/store/migrations_test.go`. The earlier focused repair regex did not
select `TestMigrationNamesAreFixedAndNewPrefixesAreUnique`. Add the new name to
that existing contract without changing a shipped name or relaxing the test.

Ten other completed failed jobs in CI run `41644271159` stopped before checkout:
GitHub returned HTTP 429 while downloading the pinned `actions/checkout`
archive. Those jobs did not execute the application checks. The arm64 run
completed the other Go packages and failed in the store migration-name guard.

Correction validation:

- `go test -race -count=1 -run TestMigrationNamesAreFixedAndNewPrefixesAreUnique ./internal/store`:
  passes.
- `go test -race -count=1 -timeout 30m ./internal/store`: the complete store
  suite passes in 390.769 seconds, without a test-name filter.
- `go vet ./internal/store`, changed-file `gofmt` and `git diff --check`: pass.

Correction `c1eab9dc` was published to PR #1368 and carried into the stacked
[PR #1369](https://github.com/eyupio/zoomies/pull/1369), retaining its S2a
implementation and handoff. CI is rerunning. The new CI result remains separate from
these local checks; an action-download failure is not an application test result.

PR #1369 incorporates this correction through a merge from PR #1368, retaining
its S2a implementation and continuation record. The initial correction sync
changed only the migration contract and this handoff. Its migration-name and comment metadata
race regressions pass, as do documentation tests after `make build-nogui`
provides the isolated worktree's embedded UI placeholder. No S2a runtime code
changed in that initial sync. The complete store-suite result above is from S1.

`main` then advanced to `ce17858c` through PR #1367, adding controller profiling
and metrics. The generated `internal/api/openapi_spec.go` conflicted and GitHub
stopped scheduling the normal pull-request checks. That main revision was merged
in `43217764`; both API clients were regenerated with `make openapi` from the
merged YAML, retaining the CPU-profile endpoint and repair `needs_action` contract. No store files,
`go.mod` or `go.sum` changed relative to the verified correction, so the complete
store result above still applies. Merge validation passes:

- `go test -race -count=1 -run 'Eli|Repair|CPUProfile|Profile|Route|Stream|Timed|Timing|Loop|MigrationNamesAreFixedAndNewPrefixesAreUnique'`
  on `./internal/api ./internal/controller ./internal/store ./internal/github`.
- `go vet` on those four packages, `go test ./internal/docs -count=1` and
  `npm run check` (zero errors/warnings in both configurations).
- `make openapi` regenerated both clients; `git diff --check` passes.

Before publishing, check the live base branch ref as well as PR metadata; the
PR's recorded base SHA can lag behind the branch that mergeability uses.

The full repository `make test-short`, all browser suites outside the affected
repair pages, a live-model evaluation and end-to-end Eli repairs against real GitHub PR
branches were not run. Publishing this implementation PR is separate from
validating Eli against a real repair target. Initial sandbox-only unit failures were process-execution restrictions;
the complete unit suite passed with normal local process execution. One early
browser run used the previous binary during a rebuild and was discarded.

The S2a follow-up below addresses F10 (observation independent from model generation).
The next slice is durable watch admission and recovery. F9's
repository-authorised visibility and F7's post-repair per-check outcomes remain
explicit follow-ups; S1 does not widen raw response or log permissions.

### S1 UX follow-up in PR #1368, 3 November 2026

The existing PR also includes three small improvements requested after
the first checkpoint:

- Configured-repository search is case-insensitive and ignores surrounding
  whitespace. The status line shows matching, enabled and paused counts. Search
  performs no policy writes.
- Edit identifies the repository and moves keyboard focus to its form. The
  target repository and connection stay fixed while editing. **Add repositories
  instead** clears the selection and prior save results, restores the team
  provider default and enables repairs with workflow edits off. Saving an edit
  preserves the existing paused/workflow settings unless explicitly changed.
- **Review fix** next to **What Eli changed** opens the actual published commit,
  including when subsequent checks fail. Changed paths remain visible without
  a retained summary, and a missing commit never links to the original PR head.

The handoff now lives at `roadmap/eli-repair-implementation-handoff.md`, indexed
in `roadmap/README.md`. `AGENTS.md`, the design record and PR description point
there. This follows the existing root-document rule without weakening its test.

Follow-up validation:

- UI type-check: zero errors and warnings in both configurations. UI lint and
  production build pass, including asset budgets. The binary was rebuilt after
  the UI for browser testing.
- All 74 cases in the affected setup/settings/attempt browser suites pass on
  desktop and mobile, including filtering without writes, focused/scoped edits,
  fresh defaults and the actual fix link. Setup screenshots refreshed from fixtures.
- `go test ./internal/docs -count=1` passes after the handoff move is staged,
  so the tracked-file layout and roadmap index rules are exercised.
- `git diff --check` passes. Backend regression and complete UI unit coverage
  remain the initial S1 results above; no backend or shared helper logic changes
  in this follow-up. Live-model and real repair-target testing remain unrun.

### S2a: independent published-commit observation, 3 November 2026

PR [#1369](https://github.com/eyupio/zoomies/pull/1369) is stacked on PR #1368 while that PR is open. Retarget it to
`main` after S1 merges and verify the resulting diff contains only S2a work.
Do not merge S1 as part of this slice without the normal review.

`startRepairLoops` starts the existing `eli-repairs` worker and the separate
`eli-repair-checks` observer through the controller's guarded, joined `spawn`.
The worker still processes one queued repair at a time and recovers `working`
attempts as `interrupted` on start or panic restart. The observer reads only
published attempts in `checking`. It wakes every three seconds; the existing
thirty-second per-attempt eligibility and one-hour verification window remain.
Pausing repository automation still allows a pushed commit to finish checking.

Check reads include client resolution in a thirty-second context. The observer
checks controller authority and cancellation before each attempt and after its
network reads. The model retains its existing ten-minute deadline. Each loop
has independent panic recovery/reporting and is joined at controller shutdown.

Lifecycle saves no longer write `comment_id`. Only `SetEliRepairComment` writes
that field, without touching state, details or `updated_at`. `repairNotice`
reads the latest state/comment ID before posting, skips a notice whose state is
stale, and bounds the request to ten seconds. This prevents a delayed worker
comment from overwriting a newer terminal outcome, and prevents a check save
from erasing a comment ID written while the check read was in flight.

### S2c: due-watch observation loop, 3 to 4 November 2026

This PR starts from `main` after #1368 merged. It adds `0139` and the watcher
loop; `0138` remains the S2b watch table.

The Codex review found two edges, both fixed. A delayed closed or unsupported
pull response could pause or stop a watch that had since been paused and
explicitly re-admitted; observer decisions now update only the row if the
claimed lease token is still present, while the person-facing pause and stop
remain unconditional. Re-admitting an idle watch also reset the stored check
state but returned the old state in memory; the returned row now says unknown
with the new head.

Validation on 3 to 4 November 2026:

- `go test -race -count=1 -run 'Eli|Repair|MigrationNamesAreFixedAndNewPrefixesAreUnique'`
  on `./internal/controller ./internal/store ./internal/api ./internal/github`:
  all four packages pass (controller 21.489s, store 37.215s, api 54.514s,
  github 1.277s). This includes the new observation, disabled-policy pause,
  closed-PR stop and rate-limit scheduling cases.
- `go vet ./internal/controller ./internal/store ./internal/api ./internal/github`:
  pass.
- `go test -count=1 -timeout 10m ./internal/store`: pass (119.667s), the complete
  store suite without the race detector, including migration 0139.
- `go test ./internal/docs -count=1`: pass (27.450s).
- `staticcheck ./internal/controller`: pass. The first CI run rejected an
  initial `next` value that every branch overwrote; it is now declared without
  that unused assignment. The review fixes add the stale-lease and returned
  check-state regressions.
- `git diff --check` and changed-file `gofmt`: pass.
- No OpenAPI regeneration was needed because no public contract changed.

#1375 merged as `77a821023` with all checks green. The review corrections are in
`a3912e4e7`; the final focused tests above were run after them.

Test correction after the merge, 4 November 2026, in PR #1380 (the controller
load work, which carries it because its `Linux arm64` check failed on this test).
It is test-only: no runtime behaviour, migration or contract changed.
`TestARateLimitedCheckReadUsesTheRetryItWasGiven` compared the stored retry with
its own clock reading plus 45 seconds to the millisecond, but the loop takes its
own reading a moment later, so the test failed whenever a millisecond boundary
fell between the two. It failed once on a hosted arm64 runner and once in 600 runs
under CPU load. It now bounds the stored time between a reading taken before the
call and one taken after it. With a 3 ms pause forced between the readings the old
assertion failed 5 of 5 and the new one passed 20 of 20, and the new one still
fails when the loop ignores GitHub's Retry-After. The test that checks the pending
schedule reads the clock first too, but only asserts a lower bound, so a later
reading cannot fail it. The next unfinished step is unchanged.

Limits of this slice:

- The loop observes one watch per pass and does not start a repair. A failed
  check state is recorded for the next slice, not acted on.
- Existing-PR enumeration, new-PR discovery, webhook wakeups and target-branch
  movement are still absent; a watch only exists after an explicit admission.
- Per-check producers, run attempts, matrix members and required-status coverage
  are not retained yet. The watch records one overall state.
- Full repository `make test-short`, browser suites, the complete store suite with
  the race detector, a live model and end-to-end GitHub mutations were not run.

### S3b: the merge task, 5 November 2026

This slice adds the task an agent is given: `merge_conflict`, which names the
attempt whose repository to run the worker against and the image to run.

- `agent.TaskMergeConflict` with `MergeID` and `MergeImage`. The repository's
  bytes are not in the task: the agent pulls them from the controller by attempt
  id, which is what keeps a GitHub credential on the controller and megabytes out
  of the queue.
- The task has a lease (`mergeLease`, ten minutes: a merge is seconds, but a cold
  image pull on that host is not), the shared attempt bound, and a queue key per
  attempt, so a repair that gave up leaves its task behind without holding up the
  next conflict on the same host. It is deliberately not a lifecycle task: an
  agent one release behind reports an unknown kind as failed, and a merge failing
  must not mark a runner failed.
- `Controller.QueueMergeConflict` is the one call that hands it to a host.

Validation on 5 November 2026, on Git 2.39.5:

- `go test -race -count=1 -run MergeTask ./internal/controller`: pass (5.956s):
  the task carries the attempt and the image and names no runner, it is queued
  once per attempt while a second attempt still gets through, and its lease,
  attempt bound, lifecycle answer and cleanup answer are all the ones this task
  needs.
- `go test -race -count=1 -run 'Compat|Task|Agent' ./internal/agent ./internal/controller ./internal/api`:
  pass (13.195s, 14.860s, 10.766s), so an older agent's answer about a kind it
  does not know still changes nothing.

Not run: the two agent endpoints over `mergeSession`, the agent handler that
unpacks and runs the worker image, and the answer's return path. Those are the
next slice.

### S3b: the agent merge transport, 6 November 2026

This slice is the controller's side of decision 0018. An agent can now pull a
prepared attempt's repository with its three pinned commit ids and post exactly
one worker answer. Nothing pulls the task yet: the agent handler that unpacks,
runs the image and posts the answer is the next slice, and no conflict is
resolved or published here.

- `GET /api/v1/agent/merge/{merge_id}` serves the packed repository with
  `X-Zoomies-Merge-Ancestor`, `-Base` and `-Head`, behind the agent credential and
  the host budget. A second pull gets the same bytes; an answered or expired
  attempt is refused rather than served.
- `POST /api/v1/agent/merge/{merge_id}` accepts one `gitmerge.WorkerResponse`.
  It must carry exactly one of a result and an error code, and a second answer is
  a conflict, not a replacement. The answer is bounded by the worker's own 8 MiB
  response budget, not the ordinary 1 MiB JSON limit.
- `agent.Transport` gains `PullMergeRepository` and `ReportMergeAnswer`, with HTTP
  and embedded implementations. The pull has its own five-minute deadline and is
  bounded by `gitbare.MaxRepositoryBytes`; the answer uses the ordinary call
  deadline.
- The `merge_conflict` task now carries `MergeAncestor`, `MergeBase` and
  `MergeHead`, as decision 0018 requires. `Controller.PrepareMerge` builds and
  holds the attempt, and `QueueMergeConflict` takes the prepared attempt rather
  than loose ids.
- `mergeSession` is now owned by the controller. The housekeeping tick removes
  attempts past their deadline, and shutdown removes every repository still held,
  so neither a lost host nor a restart leaves prepared repositories on disk.
- The private-connection allowlist admits the two new agent paths. Both are in
  `api/openapi.yaml` as `x-internal` agent operations, and `make openapi`
  regenerates the embedded document and the UI client as it does for every
  served route.

Validation on 6 November 2026, on Git 2.39.5:

- `go test -race -count=1 -run 'Merge|AgentRoutes' ./internal/controller ./internal/api ./internal/agent`:
  pass (controller 11.256s, api 15.054s, agent 1.054s). This covers the pull and
  answer endpoints, a second pull and answer, an answer with neither or both
  things, the prepared pins in headers and on the task, expiry and shutdown
  cleanup, and the transport's own request shapes.
- `go test -race -count=1 -run 'Merge|AgentRoutes' ./internal/gitmerge ./internal/gitbare ./internal/gitfetch`:
  pass (1.625s, 1.385s, 1.013s; the last has no matching test).
- `go vet` on the six changed packages and `staticcheck` on them: pass.
- `go test -count=1 ./internal/docs`: pass (33.078s), with the handoff tracked.
- `make openapi` regenerated `internal/api/openapi_spec.go` and
  `web/src/lib/api/schema.d.ts`; `go test -count=1 -run 'TestTheSpecCoversTheRouter|TestEveryOperationRecordsItsRole|TestNoNewDescriptionInTheSpecIsCutAtItsFirstComma' ./internal/api`
  passes. The first CI run found the new routes missing from the spec and two
  unquoted flow descriptions; both are fixed.
- The existing failed-fetch cleanup test now compares the temporary merge
  directories before and after its own call. It previously failed when another
  package's test binary happened to be preparing a repository at the same
  moment; that is a test isolation fix, not a runtime change.

Limits of this slice:

- No agent handles `merge_conflict` yet. It is not queued by any production path,
  and admission still refuses the conflicts capability.
- No model resolution, two-parent commit or publication exists.
- Full repository `make test`, browser suites, the complete store suite with the
  race detector, a live model and end-to-end GitHub mutations were not run.

### S4 backend: durable watches on the REST API, 6 November 2026

This slice puts the watcher on the API the UI will call. It changes no runtime
behaviour: the same loops admit and observe the same rows.

- `GET /assistant/repairs/watches` lists per-PR watches newest first, filtered by
  exact repository and by `enabled`, `paused` or `stopped`, with a page and a
  total. `POST` admits one pull request; the caller becomes its owner.
- `POST /assistant/repairs/watches/{id}/pause`, `/resume` and `/stop` change one
  watch. Resume goes through the normal admission path, so the policy, provider,
  access and pull request are checked again; stop stays terminal until a new
  explicit admission.
- `GET/POST /assistant/repairs/watches/repositories` list and admit
  repository-wide decisions. `POST .../{id}/pause` stops new-PR discovery but
  leaves already admitted watches alone; `POST .../{id}/resume` runs the normal
  repository admission and turns discovery back on.
- Reads need `assistant.read`, changes need `assistant.write`, and every change
  writes its own audit row. The list never carries the observation lease, which
  stays internal.
- A repository decision needed two small store additions: reading one by id, and
  pausing or resuming it without creating a watch.

Validation on 6 November 2026:

- `go test -count=1 -run 'EliWatch|Spec|Route' ./internal/api`: pass (11.934s).
  The tests cover list filters, the lease never being serialised, pause and stop
  reasons, a missing watch, repository listing and repository pause.
- `go test -count=1 -run 'TestRouteTableCoversTheSpec|TestTheSpecCoversTheRouter|TestNoNewDescriptionInTheSpecIsCutAtItsFirstComma|TestEmbeddedSpecMatchesTheSource' ./internal/api`:
  pass after quoting three descriptions and adding the watch id prefixes to the
  route-table normaliser.
- `make openapi` regenerated the embedded spec and the UI client; `go build ./...`
  and `go vet` on the changed packages pass.
- Changed-file `gofmt` and `git diff --check`: pass.

Limits of this slice:

- No UI uses these routes yet. Nothing has been validated against a live model,
  real worker image or real GitHub PR.
- The list is scoped by the same read rule as repair history and is otherwise a
  snapshot of the watch rows; a repository-membership-scoped activity view is
  still S4.
- Admission still enumerates at most the discovery cap and reports it.

### S3b: the conflicted watch, 6 November 2026

This slice joins the pieces: a watch that follows conflicts now starts the
worker, resolves the answer with the model and publishes the two-parent commit.
It adds no migration and no public contract change; the conflict capability
becomes available through the controller's existing admission.

- `eliWatchCapabilities` accepts `checks`, `conflicts` or both. The observer
  reads GitHub's mergeability as it already did; when the head is known not to
  be mergeable and a merge base is pinned, it claims the incident by watch,
  head, base and merge base, prepares the repository in a tracked goroutine, and
  queues `merge_conflict` on a host advertising the new `merge-worker` feature.
  The watch's next check is kept at two minutes while the incident is carried.
- An agent advertises `merge-worker` only when one of its registered backends
  implements the one-shot worker run. The worker image tag follows the
  controller's channel (`version.Channel`), through
  `naming.GitWorkerImage`, so a dev controller asks for `:dev`.
- A separate `conflict-repairs` loop takes answered attempts. It rechecks the
  watch, policy, provider, installation and pull request before model work, asks
  the model only for the conflicted files, applies those files to the worker's
  merged-tree changes, and rechecks the same authority, the conflict evidence
  and the head and target tip again before the ref update.
- A paused or stopped watch, a changed policy or provider, a moved head or base,
  a conflict that is no longer present, or a lost capability each discards the
  work rather than publishing it. A second observation of the same incident does
  not start a second worker.
- A conflict no model may resolve is held for an hour for a person rather than
  retried every couple of minutes. A transient model or provider failure is let
  go so the next observation can try again.

Validation on 6 November 2026, on Git 2.39.5:

- `go test -race -count=1 -run 'Conflict|Conflicted|AdmitEliRepairWatch|DueWatch' ./internal/controller`:
  pass (15.665s). The tests start the worker from a real prepared repository,
  publish the model's resolved file through a two-parent commit, refuse to
  publish after a pause, hold an unsupported conflict without opening the model,
  and process an answer that arrived before the loop.
- `go test -race -count=1 -run 'Merge|Feature|GitWorkerImage' ./internal/agent ./internal/naming`:
  pass (agent 1.187s; naming has no matching test).
- `go build ./...`, `go vet` on the changed packages and `staticcheck` on them:
  pass.
- `go test -count=1 ./internal/docs`: pass (23.769s), with the architecture text
  updated.
- Changed-file `gofmt` and `git diff --check`: pass.

Limits of this slice:

- The conflict claim is in memory. A controller restart during the worker or the
  model loses that attempt; the watch starts again from fresh evidence.
- No UI exposes the conflict capability or shows its activity yet, and no live
  model, real worker image or real GitHub mutation was run.
- The final S4 controls and the conflict-only presentation remain.

### S3b: the merged tree and the two-parent commit, 6 November 2026

This slice carries the merge out of the worker's repository and into GitHub. The
worker's tree object does not exist there, so the worker now reports every file
the merged tree has that the pull request head does not; the controller can
therefore ask GitHub to build the same tree and commit it.

- `gitmerge.Result` gains `Changes`: added, modified, deleted and mode-changed
  entries relative to the pull request head, with blob content or a submodule
  commit id. The set is bounded at 4,000 files and 4 MiB, and an oversized merge
  is refused with the worker's existing `too_large` code rather than truncated.
- `conflictresolve.ApplyChanges` puts the model's resolved files back into that
  list, keeping the worker's mode and refusing a path the merge did not change, a
  deleted path, or a resolved file that still carries a marker.
- `github.CommitConflictResolution` re-reads the pull request, then creates a
  tree from the head tree plus those changes, a commit whose parents are the head
  first and the pinned base second, and advances the branch with `force: false`.
  It reads the pull request again immediately before the ref update, so a head or
  base movement after the model's work discards the attempt rather than
  publishing a commit for a stale incident.
- A deletion is sent as a null `sha`; a submodule as a commit entry; a file mode
  outside the blob modes this path supports is refused.
- The agent's merge answer schema in `api/openapi.yaml` now documents `changes`,
  and `make openapi` regenerated both clients.

Validation on 6 November 2026, on Git 2.39.5:

- `go test -race -count=1 -run 'Merge|ConflictCommit|Resolve|ApplyChanges' ./internal/gitmerge ./internal/github ./internal/conflictresolve`:
  pass (gitmerge 1.671s, github 1.167s, conflictresolve 1.022s). The tests cover
  the head-relative change list including a deletion, a mode change and binary
  content; applying a resolution while keeping an executable mode; refused
  deletions, markers and out-of-scope paths; two parents with the head first;
  a null deletion entry; a submodule entry; a non-forced ref update; and a head
  or base movement refusing before the ref moves.
- `go build ./...`, `go vet` on the changed packages, `staticcheck` on them,
  `go test -count=1 ./internal/docs` (25.791s), changed-file `gofmt` and
  `git diff --check`: pass.
- `make openapi` regenerated `internal/api/openapi_spec.go` and
  `web/src/lib/api/schema.d.ts`; the spec-cover-the-router and description-lint
  tests pass.

Limits of this slice:

- No watch calls any of this yet. The pause, authorisation and transfer-drain
  checks at the publication boundary, and the conflict capability's admission,
  are still to write.
- The worker's change list is the complete difference from the head, so a merge
  whose base moved thousands of files is refused rather than partially published.
- No live model, real GitHub mutation or real worker image was run. The tests use
  real local Git repositories and a scripted GitHub API.

### S3b: bounded model resolution of a merge conflict, 6 November 2026

This slice adds `internal/conflictresolve`, the step between a worker's conflict
answer and a commit. It asks a model to resolve only the text conflicts the
worker classified as supported, and it returns complete files for those paths.
It has no caller yet: nothing is committed, pushed or watched.

- The two sides are rendered in the marker form a person sees, with the common
  ancestor supplied beside them as evidence. The model is told to keep both
  intents and remove every marker.
- The resolver uses the existing bounded editor (`internal/prrepair`), so the
  model reads and edits only the conflicted files, workflow paths stay subject
  to the repository policy, and the source, file and step budgets are the ones
  every repair already has.
- A path protected by the repair policy, a conflict set larger than the editing
  allowance, or a conflict file larger than the blob limit is refused before the
  model is called.
- The answer must name a resolved file for every conflict, and no resolved file
  may still contain a marker line. A model that adds a path outside the conflict
  set, leaves one unresolved or leaves markers is refused.

Validation on 6 November 2026:

- `go test -race -count=1 ./internal/conflictresolve`: pass (1.021s). The tests
  cover both sides reaching the model, a complete resolved file, a marker left
  in a changed file, a protected workflow path refused before any model call,
  too many conflicts, an empty set and a missing resolution.
- `go vet ./internal/conflictresolve` and `staticcheck ./internal/conflictresolve`:
  pass.
- Changed-file `gofmt` and `git diff --check`: pass.

Limits of this slice:

- There is no caller. The two-parent commit, the non-force publication, the
  merged-tree change list and the wiring from a conflicted watch do not exist.
- The model is not given the rest of the repository, only the conflicted files
  and their common ancestor. That is deliberate for this stage; the next stage
  may supply further context if the merge evidence supports it.
- No live model was run; the tests use a scripted model. No real GitHub mutation
  was made.

### S3b: the agent runs the merge worker, 6 November 2026

This slice completes decision 0018's agent half. A `merge_conflict` task now
pulls the prepared repository, unpacks it, runs the published worker image in a
one-shot container and posts the one answer. It adds no migration and no public
API; admission still refuses the conflicts capability, and no conflict is
resolved or published.

- The task is dispatched beside runner work, not through the lifecycle claim:
  it names no runner, a failure changes no runner state, and a cordoned host may
  still merge.
- `internal/agent/merge.go` pulls the attempt, refuses a repository whose three
  pins differ from the task's, and makes a workspace under `agent.work_dir`
  (or the temporary directory). It unpacks with `gitbare.Unpack`, writes the
  worker request beside the repository, parses the answer with unknown fields
  refused, posts it, and removes the workspace whatever happened.
- A second answer the controller already holds (409) is a lost reply, not a
  failure: the task reports success and stops. A worker answer carrying an
  error code is posted and reported as a failed task with that sentence.
- `backend.MergeWorkerRunner` is the new optional contract. Docker and Podman
  implement it by preparing the image, creating a container with only the
  workspace mounted at `/work` and no network, waiting with a deadline, reading
  `answer.json` rather than mixing Git's stderr into the answer, and removing the
  container before and after. The container carries no managed label, so
  reconciliation never mistakes it for a runner.
- The worker runs as the image's own user while the workspace belongs to the
  agent, so the agent makes the workspace directories writable and passes
  `safe.directory` for `/work/repository`; file modes, including read-only loose
  objects, are left alone. The request carries no credential and the container
  has no route out.
- The controller's result path recognises a merge task and only logs the host's
  failure; the prepared repository is left to its deadline rather than deleted
  under a late answer. The docs now say the agent, not the controller, starts the
  worker.

Validation on 6 November 2026, on Git 2.39.5:

- `go test -race -count=1 -run 'Merge|AgentRoutes' ./internal/agent ./internal/backend ./internal/controller ./internal/api`:
  pass (agent 1.223s, backend 1.058s, controller 11.951s, api 15.196s). This
  covers the pull/unpack/run/answer path, mismatched pins, an unreadable answer,
  an already-answered attempt, a worker error answer, a backend without the
  capability, workspace removal, the dispatch path and the container config.
- `go test -race -count=1 -run RunMergeWorker ./internal/backend`: pass (1.041s).
- `go vet` on the four packages and `staticcheck` on them: pass.
- `go test -count=1 ./internal/docs`: pass (24.949s), with the three documentation
  pages updated.
- `go build ./...`, changed-file `gofmt` and `git diff --check`: pass.

Limits of this slice:

- The container backend is tested through the repository's fake engine; no real
  worker image was built or run here and no container runtime was available.
- Model resolution, the two-parent commit, non-force publication and the final
  controls do not exist yet.
- Full repository `make test`, browser suites, the complete store suite with the
  race detector, a live model and end-to-end GitHub mutations were not run.

### S3b: the repository's journey to the agent, 5 November 2026

Decision 0018 has an agent pull the repository and run the worker against it. This
slice is that journey's data half: the packing and the unpacking, bounded and
path-safe, so the bytes can cross without either side trusting the other.

- `gitbare.Pack` writes a built repository as a gzipped tarball, refusing a
  symlink or a file that is not regular, because a repository this package wrote
  has neither and one that did would point at whatever the packer's host had.
- `gitbare.Unpack` writes it back, refusing an absolute path, a path that climbs
  with `..`, an entry that is neither a file nor a directory, and a tarball past
  its budget. It sets each file's mode after writing, because `WriteFile` applies
  the umask and Git will not trust a loose object that can be rewritten.
- `zoomies git-worker` gained `--request` and `--answer`: the agent runs it as a
  one-shot container with the workspace mounted, and plumbing an attach into a
  container runtime for a process that runs once is a lot of machinery for two
  files. Standard streams remain the default, so the earlier examples still hold.
- `mergeSession` is the controller's whole transport surface: prepare an incident
  (build, pack, hold), hand its bytes out as often as an agent needs, take exactly
  one answer, or abandon it. Every path removes the repository, and the agent's
  endpoints become thin handlers over those three calls.
- `mergeAttempts` is the controller's half: one prepared repository per attempt,
  fetched as often as a lost reply needs and answered exactly once. It has three
  ways to let one go (an answer, a deadline, shutdown) and every one of them
  removes the temporary directory, so a controller that leaked one per conflict
  would not fill its own disk with the evidence of repairs it gave up on. A
  duplicate answer or a pull after answering says so rather than pretending the
  attempt never existed.

Validation on 5 November 2026, on Git 2.39.5:

- `go test -race -count=1 ./internal/gitbare`: pass (1.522s). The round trip packs
  a repository built from a real repository's three commits, unpacks it elsewhere
  and merges it there with `internal/gitmerge`, getting the same conflict back;
  the unpacked loose objects are checked read-only; and each refusal is covered:
  a byte budget, a body that is not a tarball, a path that climbs out, and a
  symlink at pack time.
- `go test -race -count=1 -run MergeAttempt ./internal/controller`: pass (1.064s).
  An attempt is fetchable until it is answered; a second answer and a pull after
  answering each say which they are; a deadline removes the repository and the
  entry; the cap refuses a further attempt; and shutdown gives back every
  repository it holds, including none of the ones it never held.
- `go test -race -count=1 -run GitWorker ./cmd/zoomies`: pass (1.223s), including a
  request read from a file and an answer written to one, with stdout left empty,
  and a request path that cannot be read being a usage error that names it rather
  than a hang.
- `go test -race -count=1 -run MergeSession ./internal/controller`: pass (1.320s),
  which drives the whole surface through the worker: an incident is prepared, its
  bytes are unpacked elsewhere and merged there with `internal/gitmerge`, the one
  answer is accepted, and the repository is gone. An answered attempt serves
  nothing, an abandoned one removes its repository and cannot be abandoned twice,
  and a pin naming a branch prepares nothing at all.
- `staticcheck ./...` and `go build ./...`: pass.

Not run: the `merge_conflict` task, the endpoints the agent pulls and posts to,
and the agent's own side of them. No container image was built (no Docker here)
and no browser suite ran (no UI changed).

### S3b: the controller's side of the fetch, 5 November 2026

The controller can now prepare the repository a repair merges in, from three
pinned commits, without Git and without a credential leaving it.

- `mergeRepository` in `internal/controller` builds the workspace: a fresh
temporary directory, the three commits fetched through the installation the watch
was admitted under, and the worker's request names the ids it wrote. The caller's
context bounds the whole fetch, and a fetch that fails removes what it wrote, so a
controller that keeps failing does not keep filling its host.
- `mergeLookup` is the adapter: a commit's tree and parents from `CommitOf`, the
files from `ArchiveOf` with the provider's wrapper directory stripped. The pair is
deliberate, because the two can disagree and `gitbare` refuses when they do.
- `gitbare.TreeID` writes one tree and returns the id its files hash to, so a
caller can check a provider's tree without a commit and a fixture can be built
whose tree and archive agree by construction.
- The fake's client now carries both transports a real one does, so a test
exercises the two-request archive split rather than a shortcut.

Validation on 5 November 2026, on Git 2.39.5:

- `go test -race -count=1 -run MergeRepository ./internal/controller`: pass
  (1.222s). Three pinned commits registered in the fake GitHub are fetched,
  written and merged by `internal/gitmerge` into the conflict with its three
  sides; a fetch whose target tip is missing removes the directory it made; and a
  pin that names a branch is refused before GitHub is asked anything.
- `staticcheck ./...`: pass (SC_EXIT=0), and `go build ./...` with it.
- `go test -race -count=1 ./internal/gitbare ./internal/gitfetch ./internal/github`:
  pass (1.395s, 1.585s, 3.538s).

This slice also fixed a real bug the test above found: `ArchiveOf` took a
repository name and pasted it into the path beside the installation's owner, so a
full `owner/name` became `acme/acme%2Fwidgets` and answered 404. It now resolves
the repository exactly as `CommitOf` does.

Not run: the `merge_conflict` task, the pull endpoint and the agent transport,
with the timeouts and cleanup around them. No container image was built (no
Docker here) and no browser suite ran (no UI changed).

### S3b: the archive download, 5 November 2026

This PR is stacked on the fetch in #1399. It adds no migration, no public API and
no UI; nothing calls it yet.

- `internal/github` gains `ArchiveOf`: one commit's tree, read from the
  repository's archive, bounded on the way in (64 MiB compressed) and on
  expansion (the caller's `gitbare.Budget`).
- The download is deliberately two requests. The API request carries the
  installation token and is made with redirects refused, because the archive
  endpoint answers with a redirect to storage whose URL is already signed;
  following it with the client's headers would hand the token to whoever the
  redirect names. The storage request carries no credential at all.
- A failure names the commit and the status, because the operator has to know
  which of the three pinned commits went, and an archive over a budget is refused
  rather than truncated: half an archive fails the tree check with a message
  about a missing file, which says the wrong thing.
- `FakeGitHub` serves the same shape, with a storage handler that refuses a
  request carrying a credential and records what it saw.
- `CommitOf` reads the commit's tree id and parents, which is what the archive's
  files are checked against: the archive says what a commit's files are, and this
  says what they must hash to, so a reconstruction the provider would not
  recognise is refused instead of merged.
- `gitfetch.NewSource` turns that pair into the `Source` `Build` asks for, so the
  controller's side of the fetch is one adapter over a client and a repository.
  A `Lookup` whose two halves disagree is refused by the tree check that was
  already there.

Validation on 5 November 2026, on Git 2.39.5:

- `go test -race -count=1 -run Archive ./internal/github`: pass (1.147s). The
  tests read a commit's tree through the production factory path and check the
  files and modes; assert the storage request carried no credential; name the
  commit and the 404 when GitHub will not hand one over; honour a caller's byte
  budget; and refuse to archive a repository nobody named for an organisation
  installation.
- `go test -race -count=1 ./internal/github`: pass (3.311s), covering the commit
  read as well: the tree and parents come back for a commit the fake has, a
  commit it does not names itself, and an organisation installation refuses a
  repository nobody named.
- `go test -race -count=1 ./internal/gitfetch ./internal/gitbare`: pass (1.557s
  and 1.356s), including a `Lookup` over a real repository feeding `Build` and
  the worker's merge, and a `Lookup` whose tree does not match its files being
  refused.
- `staticcheck ./...`: pass. The first CI run of this branch failed it on three
  things this slice had added: `gh.String`, which go-github now deprecates in
  favour of `gh.Ptr`, and `archive/tar.TypeRegA`, which is deprecated as a name.
  The marker it stands for is still what some archives write, so it is spelled
  once in `internal/gitbare` as `typeRegOld` with the reason beside it. Both were
  fixed and the whole tree re-checked clean.
- `go build ./...`, `go vet ./...`: pass. The interface gains are implemented by
  the demo client, which says it has nothing to serve rather than pretending.
- `gofmt -l internal/github internal/controller`: pass.

Not run: the controller call that binds a client and a repository into a
`Lookup` (it needs the transport to have somewhere to put the result), the
`merge_conflict` task and the agent transport, and the timeouts and cleanup
around them. No container image was built (no Docker here) and no browser suite
ran (no UI changed).


### S3b: publish the Git worker image, 5 November 2026

[#1398](https://github.com/eyupio/zoomies/pull/1398) is stacked on
`codex/eli-git-merge-worker` because #1391 remains
open and carries #1395's command and image target. It wires `gitworker` into the
CI Images job, including its own cache and main-only dev/main/SHA tags. Release
builds use both existing builders, release tags and stable-only latest, with
build provenance. Cleanup now covers six package families. Documentation and a
workflow regression test keep the worker from silently going unpublished again.
No worker build passes a runner channel argument, and pull requests publish
nothing. The controller does not start the worker yet.

Before editing, a scratch commit appending a comment to `ci.yml` was pushed to
`codex/check-workflow-permission-20261105115907` successfully, then the remote
branch was deleted. This verifies workflow write permission for this session.

Validation on 5 November 2026 (Go 1.27.1):

- `make ui-stub`: pass.
- `go test -count=1 ./internal/docs`: pass (209.427s).
- `go vet ./...`: pass.
- `gofmt -l internal/docs/workflows_test.go` and `git diff --check`: pass.
- `make image-gitworker`: attempted with Docker 28.4.0. It failed at
  `apk add --no-cache git`: the environment network policy denies
  `dl-cdn.alpinelinux.org`. The in-image `git-worker --check` did not run;
  the PR Images job must prove the target builds before merging.
- Full repository tests, browser suites and a release publication were not run.
  This slice changes no UI or runtime behaviour.

The workspace uses `PATH=/workspace/.tools/go/bin:$PATH`,
`GOCACHE=/workspace/.cache/go-build` and `GOMODCACHE=/workspace/.cache/go-mod` for
Go commands. Its HOME is outside the checkout, so no extra worktree is needed.

CI monitoring: #1398 was merged externally into the worker branch as
`9656b8602e0e5f77ea931260dfe22513bcee954e` while local validation was running.
Its first CI job had been queued without a runner; merging cancelled the suite,
so it did not verify the image. The automated review could not run because the
account review quota is exhausted; it reported no actionable code finding.
#1391 remains open against main. Image publication awaits that integration and
its successful Images job; a queued or cancelled check is not validation.

Next unfinished step: bounded archive download and the controller fetch using
`internal/gitbare`, then worker transport, timeouts and cleanup. Model resolution,
two-parent publication and S4 UX remain later slices.

### S3b: fetching the incident, 5 November 2026

This PR is stacked on the repository writer in #1396. It adds no migration, no
public API, no UI, and nothing in the controller calls it yet.

- [Decision 0018](decisions/0018-an-agent-runs-the-git-worker.md) settles the
  question 0016 and 0017 left open, what starts the worker container: an agent
  host does, because it already runs containers through its backends and the
  controller already reaches it for work. The controller queues a `merge_conflict`
  task, the agent pulls that attempt's repository from the controller over its
  authenticated transport, runs the worker image against it and posts the one
  answer back. A repository travels as a tarball, never inside a task payload.
- `internal/gitbare` gains the archive reader: a provider's gzipped tarball of one
  commit becomes the file list the writer takes. It refuses rather than repairs:
  an absolute path, a path that climbs with `..`, a hard link, a device, a named
  pipe, more files or bytes than its budget, and a body that is not an archive all
  say which entry caused it.
- `internal/gitfetch` is the controller's half of the fetch: it asks a `Source`
  for each of the three pinned commits, writes them with `gitbare` (the ancestor
  first, named as the parent of both sides), refuses an answer about a commit
  nobody asked for, refuses a pin that is not three commit ids, and removes a
  built repository on every path out, including Windows, where a read-only loose
  object cannot be deleted until its bit is cleared.

Validation on 5 November 2026, on Git 2.39.5:

- `go test -race -count=1 ./internal/gitbare`: pass (1.373s), including a
  provider-shaped tarball whose files hash to the very tree id Git wrote for the
  same commit, and each refusal naming its entry.
- `go test -race -count=1 ./internal/gitfetch`: pass (1.378s), including three
  pinned commits fetched one at a time and merged by `internal/gitmerge` into the
  real conflict with its three sides, a source that answers about another commit,
  a provider tree the files do not make, a pin that is not commit ids, and the
  removal of a built repository.
- `gofmt -l` on both packages: pass.

Not run: the HTTP download of an archive, the `merge_conflict` task, the pull
endpoint, the agent's temporary directory and the transport's timeouts. Those are
the next slice. No container image was built (no Docker here) and no browser suite
ran (no UI changed).


### S3b: the repository the worker merges, 5 November 2026

This PR is stacked on the worker command and image in #1395. It adds no
migration, no public API, no UI and no controller wiring: nothing calls the new
package yet.

- [Decision 0017](decisions/0017-the-controller-builds-the-workers-repository.md)
  settles how a controller with no Git and no repository code hands the worker a
  repository: it downloads each pinned commit's archive, hashes the files into
  blobs and the directories into trees, and writes a bare repository. Blobs and
  trees are content-addressed, so every id the controller computes is the id Git
  computes, and every commit's root tree is checked against the id the provider
  reported before anything is merged.
- `internal/gitbare` is that writer: a bare repository with objects, HEAD, refs
  and a format-version config, and nothing else (no hooks, no remotes, no
  credentials). A path in an archive that is absolute or climbs with `..` is
  dropped, and dropping it cannot go unnoticed because the tree then fails the
  check.

Validation on 5 November 2026, on Git 2.39.5:

- `go test -race -count=1 ./internal/gitbare`: pass (1.305s). The tests use a real
  repository with nested directories, an executable and a symlink, and check that
  hashing its files reproduces the very tree id Git wrote; that a claimed tree
  which does not hash is refused and named; that an escaping path changes nothing;
  and, end to end, that `internal/gitmerge` merges three commits
  `internal/gitbare` wrote and returns the conflict with its three sides. That
  last test is the point of the package: it proves a reconstruction without Git
  feeds the worker.
- `gofmt -l internal/gitbare`: pass.

Not run: the download itself, the controller's use of the writer, the worker
transport, timeouts, cleanup and failure handling. Those are the next slice. No

Correction after CI, 5 November 2026: this branch's staticcheck failed on
`archive/tar.TypeRegA`, which Go has deprecated as a name. The marker it stands
for is still what some archives write, so the reader keeps accepting it by
spelling the byte once, as `typeRegOld`, with the reason beside it. `staticcheck
./...` passes on the whole tree after the change, and the archive tests still
pass.


container image was built (no Docker in this environment) and no browser suite
ran (no UI changed).


### S3b: the bounded Git worker command and image, 5 November 2026

This PR is stacked on the merge package in #1391. It adds no migration, no public
API, no UI and no controller behaviour: nothing in the running controller can
start the worker yet.

What it adds:

- `zoomies git-worker`, a subcommand of the one binary that reads one JSON merge
  request on standard input and writes one JSON answer on standard output, then
  exits. It refuses unknown request fields, a request over 64 KiB, a response
  over 8 MiB, a relative or climbing repository path, and a second request on the
  same input. A conflicted merge is a successful answer, because the conflict is
  the evidence a repair is made from.
- `--check`, which asks the host's Git whether it can merge and is the worker
  image's own last build step, so a base image that lost `merge-tree
  --write-tree` fails the build rather than the first conflict.
- `deploy/Dockerfile`'s `gitworker` target, built by `make image-gitworker`:
  Alpine with Git, the same static binary, running as 65534, with no database, no
  GitHub App key and no model provider key. It holds nothing to steal and nothing
  to drive but one request.
- `docs/cli.md`, the generated `skills/zoomies/reference.md` and
  `docs/architecture.md` say what the worker is and where it runs.

The image is **not published**, and that is a limitation of this PR rather than a
decision: `ci.yml`, `release.yml` and `cleanup-packages.yml` each need a line for
it, and the token available to this session cannot push a change to a workflow
file (`refusing to allow a GitHub App to create or update workflow ... without
'workflows' permission`). The wiring was written, checked against
`internal/docs`' workflow tests and then removed rather than left half-pushed. The
first step of the next slice is to land it, with a token that has that permission:
build `target: gitworker` on every `Images` run, publish
`ghcr.io/eyupio/zoomies-git` as `:dev`, `:main` and `:sha-<commit>` from `main`,
publish the release tag and `:latest` from `release.yml` with an attestation, and
add `zoomies-git` to the cleanup matrix whose header would then say six families
rather than five.

Validation on 5 November 2026, on Git 2.39.5 (the Git Debian 12 ships):

- `go test -race -count=1 ./internal/gitmerge ./cmd/zoomies`: pass (gitmerge
  1.897s, cmd/zoomies 158.633s). The worker protocol is covered by real temporary
  repositories for a clean merge, a text conflict with its three sides, an unknown
  field, a relative path, a wrong merge base, a request over budget, an answer over
  budget and a second request; the command is covered end to end through `dispatch`,
  including that stdout carries exactly one JSON object and that a refusal still
  answers in JSON and exits 1. The same suite caught the one thing this slice
  missed at first: `skills/zoomies/SKILL.md` places every command in exactly one of
  its three lists, and `zoomies git-worker` is now under "Leave to the user", with
  the controller, the agent and the gateway.
- `go test -count=1 ./internal/docs`: pass (20.519s in a clean worktree; this
  environment's own `$HOME` inside the checkout makes several of its tree-wide
  scans fail, which is why the suite was run from `git worktree`). It covers the
  new command's documentation row, the regenerated skill reference and the labels
  on the new image target.
- `go build ./...`, `go vet ./internal/gitmerge ./cmd/zoomies ./internal/docs`,
  `gofmt -l` and `git diff --check`: pass.
- `git merge-tree --write-tree` was checked by hand against the Alpine 3.22 Git
  the image installs; the `--check` step is what the image build runs.

Not run: the container image itself was not built here (no Docker in this
environment), so the `gitworker` target's build, the `apk add git` layer and the
in-image `--check` await the CI job that will build it once the wiring above
lands. `staticcheck` was not available in this environment and runs in CI. No
browser suite was run: this slice changes no UI.

Omissions and the next unfinished step:

- The image is unpublished and unnamed by any workflow, so the controller has no
  image to start even if it tried.
- No controller fetch of a sanitised bare repository, no worker transport, no
  timeouts, no cleanup and no failure handling. `internal/naming` does not yet
  name the worker image for the controller, and admission still refuses the
  conflicts capability.
- After that: model resolution and the two-parent commit, then S4 UX.


### S3b: bounded Git merge worker foundation, 5 November 2026

This PR records decision 0016 and adds the merge package. It does not add a
command, image or controller transport, so no production path can invoke it yet.

Validation on 5 November 2026:

- `go test -race -count=1 ./internal/gitmerge`: pass. The tests use real temporary
  Git repositories for a clean merge, a text conflict, binary refusal,
  delete/modify refusal, a wrong merge base, an external merge driver and the
  byte budget.
- `staticcheck ./internal/gitmerge` and `go test ./internal/docs -count=1`:
  pass.
- `git diff --check` and changed-file `gofmt`: pass.

Limits of this slice:

- No worker process, container image, controller client or transport exists. The
  merge package is not reachable from the running controller.
- No model resolution, no two-parent commit and no publication.
- The worker input contract assumes a sanitised bare repository; the controller
  fetch and cleanup are still to write.
- Full repository `make test-short`, browser suites, the complete store suite
  with the race detector and end-to-end GitHub mutations were not run.

Correction after review, 5 November 2026: the merge called
`git merge-tree --merge-base`, which does not exist in Git 2.39. Every merge
answered `exit status 129` (`error: unknown option 'merge-base'`) on that Git, so
the package passed only on a host with a newer Git than Debian 12 ships. The
ancestor is already checked against `git merge-base` immediately before the
merge, so the flag was redundant; `merge-tree` now re-derives the same base from
the two commits it is given. The floor is Git 2.38, where `--write-tree` arrived,
and the worker image asserts at build time that its Git has that option.
Re-run on Git 2.39.5: `go test -race -count=1 ./internal/gitmerge` passes.

### S3a: merge-conflict detection and evidence, 4 November 2026

This PR adds `0143` and pins the conflict facts a later resolution can trust. It
does not compute a three-way merge, run repository code or publish anything.

Validation on 4 November 2026:

- `go test -race -count=1 -run 'Eli|Repair|MigrationNamesAreFixedAndNewPrefixesAreUnique|Conflict'`
  on `./internal/controller ./internal/store ./internal/api ./internal/github`:
  all four pass (controller 23.038s, store 38.305s, api 67.645s, github 1.310s).
- `go test -count=1 -timeout 10m ./internal/store`: pass (74.399s), the complete
  store suite without the race detector, including migration 0143.
- `go vet` on those four packages and `staticcheck ./internal/controller ./internal/store ./internal/github`:
  pass.
- `go test ./internal/docs -count=1`: pass (25.180s), changed-file `gofmt` and
  `git diff --check`: pass.
- New regressions cover mergeable, conflicted and still-unknown GitHub answers,
  the pinned merge base, recording conflict evidence and refusing a stale lease.

Limits of this slice:

- Detection is read-only. There is no three-way merge, conflict classification,
  model resolution, two-parent commit or publication.
- The Git merge engine decision is now 0016 in S3b: a separate bounded worker.
  That worker and its transport are not connected yet.
- Mergeability can remain unknown; the watch records that rather than treating it
  as a conflict.
- Full repository `make test-short`, browser suites, the complete store suite
  with the race detector, a live model and end-to-end GitHub mutations were not
  run.

### Controller load: the watch claim looks before it takes the writer, PR #1392, 5 November 2026

This PR is about the controller's own cost and is not an Eli slice. It touches
the watch loop in one place. `ClaimDueEliRepairWatch` (S2c) used to run its
`SELECT` inside a write transaction, and the loop asks every three seconds
whether or not anybody has set a watch up, so a controller with no watches took
the single database writer for nothing every three seconds, queuing every webhook
and heartbeat behind the question. It now runs the same predicate as a one-row
`SELECT EXISTS` on the reader pool and takes the writer only when a watch is due.
The claim under the writer reads again and is unchanged, so leasing, ordering and
what an observation records are as they were. A watch that falls due just after
the look is claimed by the next ask, and a look that fails falls through to the
claim.

Two later commits on the same PR change how the pacing treats what lands inside
its gap. The first push turned the `Playwright (other)` check red on the Updates
page specs, so a saved setting skips the gap once (`UpdateConfig` calls
`demandDerived`). Running the broader UI specs then found that a pass the gap
turned away was dropped, so it now sets one timer that nudges the scheduler when
the gap ends, and what an operator does skips the gap too (`NudgeOperator`,
which the handlers that edit a pool, a host, a runner or an installation call).
They change `internal/controller`, fifteen call sites in `internal/api` and a
comment in `web/tests/updates.spec.ts`, and no Eli code.

Validation on 5 November 2026:

- `TestAWatchClaimWithNothingDueNeverTakesTheWriter` (new, in
  `internal/store/idle_claims_test.go`) counts writer acquisitions through
  `ObserveWrites`. It failed on the previous code for the right reason (an empty
  table, a watch not yet due and a watch already leased each took the writer
  once) and passes now. The existing lease tests in
  `internal/store/eli_repair_watches_test.go` pass unchanged.
- `BenchmarkClaimDueEliRepairWatchIdle`, six runs each on an idle machine,
  `benchstat`: an idle claim 67.5us to 12.7us (-81%), and writer takes per claim
  1 to 0, with and without fifty watches waiting.
- `make test` (`go test -race -count=1 ./...`): 55 packages ok, none failed, no
  data race reported, 25 minutes 46 seconds (store 762.7s, controller 721.0s).
- `go build ./...`, `go vet ./...`, `staticcheck ./...`, `gofmt -l` and
  `go mod tidy -diff`: clean.
- The later commits: `go test -race -count=1 ./internal/controller/...
  ./internal/api/... ./internal/docs/...` passes (controller 729.8s, api 466.8s,
  docs 141.8s), and `make lint` and `staticcheck` on the controller and API
  packages are clean. Playwright against the real binary: the group CI calls
  `other`, with CI's flags, 233 passed in 9.9 minutes (before the last commit);
  its `updates` and `updates-mobile` projects again on the final tree, with the
  473 specs of the `chromium` project that follow the event stream, 592 passed,
  none failed, 7 skipped, in 20.3 minutes. `updates.spec.ts:411` failed on the
  first push after 23 seconds and passes in 2.5. `live.spec.ts:152` failed on the
  head before the last commit (14.0 seconds), passes on `main` (1.7 seconds) and
  on this branch (1.2 to 1.6 seconds, `chromium` and `mobile`).
- Four new tests, each failing with the call it guards removed and passing with
  it: `TestAChangedSettingIsShownByTheNextPassWithoutWaitingOutTheGap`,
  `TestAnOperatorsNudgeSkipsTheGapAndAnOrdinaryOneDoesNot`,
  `TestAPassTheGapTurnedAwayIsFollowedByOneWhenTheGapIsOver` and, in
  `internal/api`, `TestAPoolEditIsShownInTheProblemsListWithoutWaitingOutTheGap`.

Limits of this slice:

- No watch behaviour, API, UI, migration or capability changed, and nothing here
  advances S3b.
- The look is a hint. No new test races two observers for one due watch; the
  existing lease test covers the claim under the writer, which is unchanged.
- `npm run check` and `test:unit` were not run, because the only change under
  `web/` is a comment. The `chromium` and `mobile` Playwright groups were not run
  in full (1,155 specs each); the 473 specs of `chromium` that follow the event
  stream were, and `live.spec.ts` on `mobile`. A live model and end-to-end GitHub
  mutations were not run.

### S2h: pull_request webhook wakeup, 4 November 2026

This PR is stacked on S2g (#1386). It adds no migration.

Validation on 4 November 2026:

- `go test -race -count=1 -run 'Eli|Repair|MigrationNamesAreFixedAndNewPrefixesAreUnique|Webhook'`
  on `./internal/controller ./internal/store ./internal/api ./internal/github`:
  all four packages pass (controller 23.560s, store 36.222s, api 56.848s,
  github 1.296s).
- `go vet` on those four packages and `staticcheck ./internal/controller ./internal/store ./internal/github`:
  pass.
- `go test ./internal/docs -count=1`: pass (28.374s).
- `git diff --check` and changed-file `gofmt`: pass.
- New regressions cover a webhook admitting a new watch where the repository was
  chosen, no watch without a repository decision, and a paused watch staying
  paused.

Limits of this slice:

- A `pull_request` subscription is optional. Without it, the five-minute pass is
  the only discovery path.
- Only opened and reopened actions are acted on. Synchronise and target-branch
  changes still rely on observation or a later S3 change.
- The delivery is read again through the GitHub client, so a closed, draft, fork
  or unwritable PR is skipped without trusting the payload.
- Full repository `make test-short`, browser suites, the complete store suite
  with the race detector, a live model and end-to-end GitHub mutations were not
  run.

### S2g: periodic new-PR discovery, 4 November 2026

This PR is stacked on S2f (#1384). It adds no migration: it reuses the watch
rows and create-only admission.

Validation on 4 November 2026:

- `go test -race -count=1 -run 'Eli|Repair|MigrationNamesAreFixedAndNewPrefixesAreUnique'`
  on `./internal/controller ./internal/store ./internal/api ./internal/github`:
  all four pass (controller 21.508s, store 36.581s, api 45.503s, github 1.313s).
- `go vet` on those four packages and `staticcheck ./internal/controller ./internal/store ./internal/github`:
  pass.
- `go test -count=1 -timeout 10m ./internal/store`: pass (47.447s), the complete
  store suite without the race detector, including migration 0142.
- The first Linux arm64 run exposed a pre-existing flaky assertion in the AI
  Context upload test: it looked for `aaaa`, which can occur by chance in a
  commit hash. It now looks for a run of 32 a's from the file's actual content.
  `go test -count=1 -run TestAnUploadThatOmitsAFileTellsEveryReaderWhatIsMissing ./internal/api` passes.
- `go test ./internal/docs -count=1`: pass (30.172s), changed-file `gofmt` and
  `git diff --check`: pass.
- New regressions cover admitting a new open PR, leaving the existing watch and
  a draft alone, not scanning a repository from a single-PR watch, recording an
  explicit repository decision and rotating past the twenty-entry bound.

Limits of this slice:

- Discovery is periodic, not webhook-driven. A newly opened PR can wait up to
  five minutes, and the App subscription still has to be updated in a later
  slice for an immediate wakeup.
- A repository with no explicit repository-wide decision is not scanned. A
  single-PR admission does not imply one. Repository-wide admission creates the
  decision after its first successful list; a failed list creates none.
- At most twenty repositories are scanned per pass; each can return up to the
  GitHub pull discovery cap.
- Full repository `make test-short`, browser suites, the complete store suite
  with the race detector, a live model and end-to-end GitHub mutations were not
  run.

### S2f: per-check evidence, 4 November 2026

This PR adds `0141` and records the bounded producers and outcomes behind a
watch's overall check state. It does not add a route or UI.

Validation on 4 November 2026:

- `go test -race -count=1 -run 'Eli|Repair|MigrationNamesAreFixedAndNewPrefixesAreUnique'`
  on `./internal/controller ./internal/store ./internal/api ./internal/github`:
  all four packages pass (controller 41.311s, store 76.862s, api 67.826s,
  github 1.301s).
- `go test -count=1 -timeout 10m ./internal/store`: pass (53.953s), the complete
  store suite without the race detector, including migration 0141.
- `go vet` on those four packages and `staticcheck ./internal/controller ./internal/store ./internal/github`:
  pass.
- `go test ./internal/docs -count=1`: pass (28.180s).
- `git diff --check` and changed-file `gofmt`: pass.
- No OpenAPI regeneration was needed because no public contract changed.

Limits of this slice:

- Evidence is stored on the watch for the head it describes. It is not exposed
  through the API or UI yet.
- The evidence cap is fifty checks. The total and a truncation flag say when
  that happened.
- Required-status and ruleset coverage, workflow run attempts and matrix
  identity beyond a check's name are still absent.
- Full repository `make test-short`, browser suites, the complete store suite
  with the race detector, a live model and end-to-end GitHub mutations were not
  run.

### S2e: bounded repair chains, 4 November 2026

This PR adds `0140` and starts repairs from failed watches. The chain count is
stored on the watch, not in memory, so a restart, a redelivered check or a check
rerun cannot reset it.

Validation on 4 November 2026:

- `go test -race -count=1 -run 'Eli|Repair|MigrationNamesAreFixedAndNewPrefixesAreUnique'`
  on `./internal/controller ./internal/store ./internal/api ./internal/github`:
  all four packages pass (controller 20.567s, store 36.538s, api 57.152s,
  github 1.264s). This includes the chain limit, repeated head, paused queue and
  one-repair-per-head cases.
- `go test -count=1 -timeout 10m ./internal/store`: pass (61.523s), the complete
  store suite without the race detector, including migration 0140.
- `go vet` on those four packages and `staticcheck ./internal/controller ./internal/store ./internal/github`:
  pass.
- `go test ./internal/docs -count=1`: pass (28.965s).
- `git diff --check` and changed-file `gofmt`: pass.
- No OpenAPI regeneration was needed because no public contract changed.

Limits of this slice:

- The chain uses an overall check state and a signature from failed job names.
  Per-check producers, matrix members and required-status coverage are still to
  be retained.
- Infrastructure-only retries are not distinguished from code failures yet; the
  three-attempt chain bound applies to both.
- A watched repair is blocked rather than queued when its watch is paused. A
  repair paused during model work is stopped before publication.
- Full repository `make test-short`, browser suites, the complete store suite
  with the race detector, a live model and end-to-end GitHub mutations were not
  run.

### S2d: repository-wide watch discovery and backfill, 4 November 2026

This PR starts from `main` after the handoff refresh. It adds no migration: the
watch table and its check state are already in `0138` and `0139`.

Validation on 4 November 2026:

- `go test -race -count=1 -run 'Eli|Repair|MigrationNamesAreFixedAndNewPrefixesAreUnique'`
  on `./internal/controller ./internal/store ./internal/api ./internal/github`:
  all four packages pass (controller 21.366s, store 38.197s, api 51.090s,
  github 1.287s). This includes the new open-pull pagination, eligible backfill,
  visible exclusions, cap, existing-watch and failed-list cases.
- `go vet ./internal/controller ./internal/store ./internal/api ./internal/github`
  and `staticcheck ./internal/controller`: pass.
- `go test ./internal/docs -count=1`: pass (28.929s).
- `git diff --check` and changed-file `gofmt`: pass.
- No OpenAPI regeneration was needed because no public contract changed.

The Codex review of S2d found that the existence check and the create happened
in separate transactions. A concurrent admission could create a watch between
them, and the backfill write would then resume a watch somebody had paused.
Backfill now calls `CreateEliRepairWatchIfAbsent`, which reads and inserts in one
store transaction and returns the existing row untouched.

Validation of the correction:

- `go test -race -count=1 -run 'Eli|Repair|MigrationNamesAreFixedAndNewPrefixesAreUnique'`
  on `./internal/controller ./internal/store ./internal/api ./internal/github`:
  all four pass (controller 19.837s, store 34.957s, api 56.454s, github 1.293s).
  The new store regression keeps an existing paused watch exactly as it was.
- `go vet` on those four packages and `staticcheck ./internal/controller ./internal/github`:
  pass.
- `go test ./internal/docs -count=1`: pass (28.596s), changed-file `gofmt` and
  `git diff --check`: pass.

Limits of this slice:

- Repository-wide admission is a controller entry point, not a route or UI. No
  new-PR discovery or webhook wakeup runs yet.
- A successful backfill admits watches due immediately; the S2c loop reads their
  checks on following passes. It does not inspect each check during admission.
- A failed or capped list is reported, but a caller must decide whether to retry
  or narrow the repository selection.
- Full repository `make test-short`, browser suites, the complete store suite with
  the race detector, a live model and end-to-end GitHub mutations were not run.

### Main integration and migration renumbering, 3 November 2026

`main` advanced to `f19751696` while the stack was open and added
`0137_host_samples_steal.sql`. The S1 migration stays `0136` and the S2b watch
migration is now `0138`, so the fixed migration list reads 0136, 0137, 0138 in
that order. The merge from `main` also regenerated both OpenAPI clients with
`make openapi`; the only conflict outside the migration list was the generated
`internal/api/openapi_spec.go`.

Integration validation:

- `go test -race -count=1 -run 'MigrationNamesAreFixedAndNewPrefixesAreUnique|Eli|Repair|HostSample|Steal|OpenAPI|Contract|Route'`
  on `./internal/store ./internal/controller ./internal/api ./internal/machine ./internal/agent ./internal/mcp`:
  all six packages pass.
- `go vet` on those six packages and `go test ./internal/docs -count=1`: pass.
- `make openapi` regenerated both clients; `git diff --cached --check` passes.
- #1369 and #1372 were merged into this branch after the review corrections, so
  this branch now carries S1, S2a and S2b. #1368 remains the PR into `main`.

Limits of this slice:

- It monitors existing repair attempts, not arbitrary PRs. It adds no durable
  watch, backfill, repeated model work, conflict repair, new setting or API.
- Check observations remain serial within the observer; a slow GitHub read can
  still delay later observations within its thirty-second budget. Per-watch
  scheduling, fair admission, rate backoff and durable leases belong to the
  persistent watcher.
- Opportunity discovery and retirement retain their existing schedule on the
  worker loop, so model work can still delay offer maintenance. They cannot
  delay the independent check observer.
- GitHub comments remain best effort. Remote comment edits are not atomic with
  database updates; retained repair outcomes remain the authoritative record.

Validation on 3 November 2026:

- `go test -race -count=1 -run 'Eli|Repair|EveryGoroutine|GoroutineCheck|PanickingLoop|PanicInDetached'`
  on `./internal/controller ./internal/store ./internal/api ./internal/github`:
  all four packages pass. This includes the new blocked-model, independent panic
  recovery, restart interruption, paused-policy checking, cancellation, authority
  gates before/during reads, delayed-comment and store metadata regressions.
- `go test -race -count=1 -run 'TestStartAndStopLeaveRunnersAlone|TestRestartLoadsTransferPreparationBeforeSchedulingWork' ./internal/controller`:
  passes, exercising production startup/shutdown and transfer-drain startup.
- `go vet ./internal/controller ./internal/store ./internal/api ./internal/github`:
  passes.
- `go test ./internal/docs -count=1`: passes with the new files staged.
- Changed Go files pass `gofmt`; `git diff --check` passes.

A sandbox-only first test attempt could not bind the fixture server's socket;
the tests pass with local loopback access. Full repository tests, UI/browser
suites, a live model and end-to-end repairs against real PR targets were not run
for S2a. No UI or public API changed; S1's UI evidence above remains its own.
The new PR's CI result must be checked separately after publication.

The subsequent base sync incorporates PR #1368's main-integration commit
`43217764`, including PR #1367's profiler labels. Keep both the independent
`startRepairLoops` wiring and the updated guarded `spawn` implementation.
The generated API clients come from the resolved S1 tree without further edits.
Sync validation passes:

- `go test -race -count=1 -run 'Eli|Repair|EveryLoopRunsUnderItsOwnProfilerLabel|MigrationNamesAreFixedAndNewPrefixesAreUnique'`
  on `./internal/controller ./internal/store ./internal/api ./internal/github`.
- `go test ./internal/docs -count=1`, vet on those four packages, changed-file
  `gofmt` and `git diff --check`.

Initial documentation/vet launches hit workspace capacity before completing.
Generated binaries were removed, recent Go cache objects moved to temporary
storage, and the checks rerun sequentially with temporary cache/build paths.
These were environment failures; the completed checks above pass.

Useful commands (see `AGENTS.md` for prerequisites):

```sh
make openapi
npm --prefix web run check
npm --prefix web run test:unit
make test TEST_PKGS='./internal/github ./internal/controller ./internal/api ./internal/store' TEST_RUN='Eli|Repair|MigrationNamesAreFixedAndNewPrefixesAreUnique'
make lint
```

### S2b: durable watch state, explicit admission and restart recovery, 3 November 2026

This PR is stacked on `codex/eli-repair-monitoring` (#1369) because #1368 and
#1369 were both open at the checkpoint. Retarget it to `main` after those merge
and verify the resulting diff contains only S2b work. Do not merge S1 or S2a as
part of this slice without their normal review.

The existing PRs were checked before extending them. Both were open; #1368 had
no completed failed check and #1369 had Playwright checks still in progress.
The Codex review of #1369 reported a failed run because its usage limit was
reached, not a finding, and there were no human review comments.

The later Playwright results confirmed two regressions in the stacked S1 code.
The setup page now leads with repositories and folds the optional personal
account card, but the a11y and mobile setup tests still waited for the folded
heading. The query-string test also expected a repository picker for a
repository an earlier test had already configured, which correctly opens in
edit mode now. A mobile failure setup test navigated to the new repair with the
suite's broad stub still answering an empty list, so the attempt page read
`head_sha` from an object that had none. The fixes change only browser tests:
wait for `Choose repositories`, use an unconfigured repository for the
query-string case, and answer the new repair endpoint with a complete row. They
are committed on #1368 and merged through #1369 into this PR.

The Codex review of the implementation found two concurrency and authorisation
edges, both now fixed. A case-insensitive policy lookup could select an enabled
row while the exact row for that repository was disabled; the lookup now gives
an exact name priority, falls back to a case-insensitive match only when it is
unique, and refuses an ambiguous list. A repeated admission also cleared a live
watch lease and made the watch claimable again; an admission now leaves the
lease, identity and next check of an observation in flight untouched. Both have
regressions beside the existing watch tests.

S2b adds no user-visible control. The repository policy modes and
`/api/v1/assistant/repairs/*` contract are unchanged, so no OpenAPI client was
regenerated. The `watch_id` field is store-internal and the policy revision is
not serialised. `AdmitEliRepairWatch`, `PauseEliRepairWatch` and
`StopEliRepairWatch` are controller entry points for the later watcher and API,
not a new public route.

Validation on 3 November 2026:

- `go test -race -count=1 -run 'Eli|Repair|MigrationNamesAreFixedAndNewPrefixesAreUnique'`
  on `./internal/controller ./internal/store ./internal/api ./internal/github`:
  all four packages pass (controller 29.999s, store 55.432s, api 56.105s,
  github 1.422s). This includes the new watch admission, idempotency, pause and
  stop, unknown-watch refusal, lease, repeated-admission, stale-release,
  restart-recovery, attempt-link, policy-casing and capability-refusal cases.
- `go vet ./internal/controller ./internal/store ./internal/api ./internal/github`:
  pass.
- `go test -count=1 -timeout 10m ./internal/store`: pass (59.851s), the complete
  store suite without the race detector, including every migration test.
- `go test ./internal/docs -count=1`: pass (9.395s), with the architecture text
  and this handoff tracked.
- `git diff --check` and changed-file `gofmt`: pass.
- `npx prettier --check` on the four changed browser specs: pass.
- Targeted Playwright on `chromium` and `mobile` for the five previously failing
  cases: 9 passed, 3 skipped, 0 failed. The cases were `/repairs/setup` a11y,
  the mobile 16px audit, the mobile setup width check, the failure setup flow and
  the query-string selection. A temporary config on port 8199 was used because
  another service already held the suite's port 8093; the application and specs
  were the repository's own. The full browser suite was not rerun.
- No OpenAPI regeneration was needed because no public contract changed.

Full repository `make test-short`, browser suites, the complete store suite with
the race detector, a live model and end-to-end repairs against real GitHub PRs
were not run for S2b. `make test` was not run. The full store suite without the
race detector, the focused store repair suite and the migration-name contract
all passed.

Limits of this slice:

- No loop claims due watches yet, so a watch costs nothing and observes nothing.
  The next slice must add bounded claim/observe/release work, honour Retry-After
  and policy or pause changes, and keep one observation per watch.
- Existing-PR enumeration, new-PR and new-commit discovery, webhook wakeups and
  target-branch movement are still absent.
- Repair chains, per-check evidence, follow-up repair of an Eli commit and
  merge-conflict detection remain later slices. Admission refuses conflicts
  until that backend exists.
- A repository policy becoming disabled or non-automatic does not yet pause its
  existing watch rows, because no consumer can act on them. The next slice must
  enforce that before any publication.

Use focused tests while iterating. The controller package is large, so record
which tests actually ran and whether the race detector was used. Browser specs
must exercise the current built UI and include desktop and mobile. A browser run
serves the controller the fixture boots, which is the binary at the repository
root, so rebuild it (`npm run build`, then `go build -o zoomies ./cmd/zoomies`)
after any UI change: `web/playwright.config.ts` reuses a server already listening
on the fixture port, and a stale binary silently serves the previous page, which
looks like a test failure rather than an old build. Live-model evaluations and
real GitHub mutation tests require separate configured access; local fixtures are
not evidence of those running.

A managed workspace may put the Go toolchain somewhere other than the default
`go` on the path; run `go version` before trusting it, because an older build
fails on `go.mod`'s floor. Earlier sessions in one workspace found
`/workspace/.tools/go/bin/go` beside an unrelated `/usr/bin/go`, with module and
build caches under `/workspace/.cache/`. Whether that applies is a property of
the workspace, not of this repository, so nothing here depends on it. Playwright
downloads its own Chromium; where a sandbox has a compatible build elsewhere,
`PLAYWRIGHT_CHROMIUM` points at it and `playwright.config.ts` applies it to every
project. Do not repurpose HOME or print credentials.

## Starting the next session

1. Read this file, root/scoped `AGENTS.md`, and the actual latest PR diff/status.
   Confirm which slices merged; a branch or checked box alone is not proof.
2. Check the working tree and live base branch. Preserve unrelated user changes.
   Review the newest PR before starting a new slice. For further implementation,
   merge #1407, #1409, #1410, #1411, #1412, #1413, #1416, #1417 and #1418 in
   that order and start from `main`, or branch from the top of the stack. Do not
   redo S1 through S3b or the S4 UI already in #1417 and #1418. The next unfinished
   work is live validation of conflict repair against a real model, the real worker
   image and a real GitHub pull request, then the remaining S4 items listed above.
3. Continue the first unfinished acceptance criterion. Read the existing store,
   API and model boundaries before adding another path around them.
4. Add regression tests for the observable behaviour and regenerate contracts
   when changed. Record actual results and limitations above.
5. Before creating/updating the PR, update the checkpoint, checklist and ledger
   in the same commit set. Add the real PR link once assigned. State explicitly
   which user-visible capabilities are still unavailable and what the next PR
   should implement. Do not require the next session to reconstruct this chat.

## Review corrections for #1413

Merged assets with invalid UTF-8 now use base64 blob creation and tree SHA
references, preserving their bytes. A merge task without an explicit backend
selects a registered container worker when the default backend runs processes.
Answered conflict jobs remain queued beyond the worker deadline until the
resolver takes them; the attempt bound and shutdown cleanup still apply.
Regression tests cover all three cases. `go test -race -count=1 -run
'Conflict|Merge' ./internal/github ./internal/controller ./internal/agent`
passed. The next unfinished step remains live end-to-end validation and the
remaining S4 presentation work recorded on the later stacked branches.
## Review corrections for #1411

Ancestor text is now supplied as read-only virtual source through the repair
reader and its existing window and byte budgets. It is no longer embedded in
failure evidence. Conflicts carry the worker's file mode, and a resolution keeps
an executable file executable. Tests cover a 200 KiB ancestor and mode 100755.
`go test -race -count=1 ./internal/conflictresolve ./internal/gitmerge` passed.
The next unfinished step remains live end-to-end validation and the later S4
controls described in the newer stacked branches.

## Review correction for #1410

The Git worker now supplies its prepared repository's exact `safe.directory`
through trusted Git command arguments, after environment sanitisation. The
container no longer sets environment variables that the worker discards.
A regression test simulates differing repository ownership at Git's boundary.
`go test -race -count=1 ./internal/gitmerge ./internal/backend` passed.
The next unfinished step remains model resolution and publication in the later
stacked slices, followed by live end-to-end validation.
