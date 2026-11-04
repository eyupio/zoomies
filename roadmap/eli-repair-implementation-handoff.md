# Eli Repair implementation handoff

Last updated: 4 November 2026.

This file is the continuation record for the repository-first Eli Repair setup,
continuous PR watching and merge-conflict repair work. Read it before changing
these features. Update it in every PR for this initiative, including corrective
PRs. `AGENTS.md` makes that requirement part of the repository guidance.

This is the shared workstream record on all three PR branches. S1 code is in
PR #1368; S2a code is in PR #1369 only; S2b code is in this PR only, stacked on
#1369. Describing a later slice here does not add its runtime implementation to
an earlier branch. None of the three PRs is merged at this checkpoint. Continue
implementation from the newest branch after checking the live PR states.

## Current checkpoint

| Item | State |
| --- | --- |
| Latest slice | [#NEW](https://github.com/eyupio/zoomies/pull/NEW): S2h pull_request webhook wakeup |
| Investigated baseline | `c97f32a12` on `codex/eli-watch-discovery` |
| Working branches | `codex/eli-watch-webhook` for S2h, stacked on S2g |
| Verified code checkpoints | S1 through S2e merged through `8a827168d`; S2f in [#1384](https://github.com/eyupio/zoomies/pull/1384); S2g in [#1386](https://github.com/eyupio/zoomies/pull/1386); S2h implementation commit recorded after publication |
| Implementation | S2g adds explicit repository decisions and periodic discovery. S2h wakes the same create-only admission from a `pull_request` opened/reopened delivery when the App subscribes to it |
| Initial PR | [#1368](https://github.com/eyupio/zoomies/pull/1368), merged as `2d621add5` with all checks green |
| Follow-up PRs | [#1369](https://github.com/eyupio/zoomies/pull/1369), [#1372](https://github.com/eyupio/zoomies/pull/1372), [#1375](https://github.com/eyupio/zoomies/pull/1375), [#1378](https://github.com/eyupio/zoomies/pull/1378), [#1379](https://github.com/eyupio/zoomies/pull/1379) and [#1381](https://github.com/eyupio/zoomies/pull/1381) merged green; the handoff refresh merged as `07a49f664` |
| Current slice | S2h implemented on top of S2g and locally verified; awaits PR/CI review |
| Next implementation | S3 merge-conflict detection and repair, then S4 UX |
| CI snapshot | #1386 was running when this record was refreshed; this PR is stacked on it |
| S1 verification | Focused race tests, UI unit/type/lint/build, docs tests and browser regressions pass; exact coverage below |
| Persistent PR watcher | Due watches are observed, backfilled, retain per-check evidence, can start a bounded chain, and discover new PRs periodically or on a pull_request webhook |
| Merge-conflict repair | Planned, not implemented. Admission refuses the conflicts capability until S3 exists |
| Production configuration | No repository automation or model credentials changed by this work |

Do not enable or advertise Baby sit or conflict-only behaviour on the strength
of S1, S2b or S2c. The existing automatic mode still starts from a failed
`workflow_job`, attempts one repair per failing head and refuses automatic
repairs of its own published commit. It does not discover earlier failures when
enabled. S2c observes an explicitly admitted watch and records its check state,
but a failed watch does not start a repair, and conflict repair does not exist.

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

S2c scope, this PR only:

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

S2h scope, this PR only:

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

Status: not started. Required even when CI never starts: ordinary
`pull_request` workflows do not run while the PR has a merge conflict.

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

Execution decision still to make in the S3 foundation PR: where the trusted Git
merge engine runs. The controller currently does not execute repository code
and the deployed image is distroless. A bounded, isolated Git worker is a
candidate, not an approved dependency or deployment assumption. Disable hooks,
repository-configured external merge drivers and filters; keep model/provider
credentials out of that worker. Decide packaging and platform support before
implementing a solution that works only on a maintainer's host.

Minimum tests: competing text edits preserving both sides; changes on only one
side; rename and delete/modify policy; multiple merge bases; protected workflow
conflict; large source cap; binary/symlink/submodule refusal; head or base moves
during model work; two-parent history; non-force update failure; pause/revocation;
post-resolution CI failure; conflict-only mode not making unrelated code fixes.

### S4: final simplified controls and repository visibility

Status: not started. May accompany S2/S3 where the backend capability exists.

- Expose the final three behaviour choices only with actual capability support.
- Select across GitHub connections while preserving each repository's verified
  installation identity. Explain discovery caps and partial enablement.
- Show effective mode, watched PR count, last successful observation, active
  work, failures needing help and pause/resume for each repository.
- Show one current PR view plus the attempt history: intended action, evidence,
  original failure, actual changed files/commit, new check results and next action.
- Add repository-scoped activity access using verified repository membership.
  Keep raw source/model responses and job-log access independently restricted.
- Notify on publication, needs-help and completion using existing in-app/event
  patterns. Update one GitHub progress comment without comment spam. The user
  must not need an open tab for the backend to keep watching.
- Add pause PR, pause repository and stop-all controls with truthful in-flight
  semantics. Preserve policy/provider choices for resume.
- Validate desktop/mobile, keyboard/focus behaviour, accessible status changes,
  empty/loading/error/partial states and exact wording of current vs historical
  outcomes. Do not show an all-clear when an observation failed.

## Relevant code and tests

| Area | Entry points |
| --- | --- |
| Setup | `web/src/routes/EliRepairSetup.svelte`, `web/src/lib/repairs/RepairSetupPanel.svelte`, `RepositoryPicker.svelte` |
| Hub and attempt | `web/src/routes/EliRepairs.svelte`, `EliRepairAttempt.svelte`, `web/src/lib/repairs/` |
| Admission, work and verification | `internal/controller/eli_repairs.go`, `eli_experience.go`, `eli_automatic.go` |
| GitHub source and commit operations | `internal/github/eli_repairs.go`, `manifest.go`, `internal/controller/webhooks.go` |
| Bounded model editor | `internal/prrepair/repair.go`, `context.go`, `supplied.go`, `diff.go` |
| Durable state | `internal/store/queries_eli_repairs.go`, `queries_eli_repair_history.go`, `queries_eli_automatic.go` |
| API | `internal/api/handlers_eli_repairs.go`, `api/openapi.yaml` |
| Related read-only check evidence | `internal/github/pr_delay.go`, `internal/analysis/merge_delay.go`; these do not establish full merge readiness |
| Backend regressions | `internal/github/eli_repairs_test.go`, `internal/controller/eli_repairs_test.go`, `internal/api/eli_automatic_test.go`, `internal/store/eli_repair_history_test.go` |
| UI regressions | `web/tests/repairs.spec.ts`, `repair-hub.spec.ts`, `repair-attempt.spec.ts`, `repair-setup.spec.ts`, `web/unit/repair-*.test.ts` |

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
| [#NEW](https://github.com/eyupio/zoomies/pull/NEW), open, stacked on #1386 | S2h: pull_request webhook wakeup | Focused repair and webhook race suites, vet, staticcheck, docs and formatting pass; no UI or public API change | Conflict repair and the final controls |

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
must exercise the current built UI and include desktop and mobile. Live-model
evaluations and real GitHub mutation tests require separate configured access;
local fixtures are not evidence of those running.

In this managed workspace the Go toolchain is `/workspace/.tools/go/bin/go`;
`/usr/bin/go` is a different program. Cached modules/build data are under
`/workspace/.cache/go-mod` and `/workspace/.cache/go-build`. Do not repurpose HOME
or print credentials. Those paths are workspace conveniences, not production
requirements. Chromium is `/usr/bin/chromium`; use the test suite's existing
`PLAYWRIGHT_CHROMIUM` override when needed.

## Starting the next session

1. Read this file, root/scoped `AGENTS.md`, and the actual latest PR diff/status.
   Confirm which slices merged; a branch or checked box alone is not proof.
2. Check the working tree and live base branch. Preserve unrelated user changes.
   Review this PR before starting a new slice. For further implementation, start
   from `codex/eli-watch-webhook` or its merged successor. Do not redo S1 through
   S2h. The next unfinished work is conflict repair and the final UI.
3. Continue the first unfinished acceptance criterion. Read the existing store,
   API and model boundaries before adding another path around them.
4. Add regression tests for the observable behaviour and regenerate contracts
   when changed. Record actual results and limitations above.
5. Before creating/updating the PR, update the checkpoint, checklist and ledger
   in the same commit set. Add the real PR link once assigned. State explicitly
   which user-visible capabilities are still unavailable and what the next PR
   should implement. Do not require the next session to reconstruct this chat.
