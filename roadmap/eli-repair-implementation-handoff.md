# Eli Repair implementation handoff

Last updated: 3 November 2026.

This file is the continuation record for the repository-first Eli Repair setup,
continuous PR watching and merge-conflict repair work. Read it before changing
these features. Update it in every PR for this initiative, including corrective
PRs. `AGENTS.md` makes that requirement part of the repository guidance.

## Current checkpoint

| Item | State |
| --- | --- |
| Investigated baseline | `871eff7d0073622f0b5f1c7d672583bd9288d633` on `main` |
| Working branch | `codex/eli-repair-setup` |
| Implementation | Initial commit `21c5e001b080f6162b702397d5404bc57181c36f`, plus the S1 UX follow-up in the same PR described below |
| Initial PR | [#1368](https://github.com/eyupio/zoomies/pull/1368), open for review; migration correction and full store race suite verified; CI rerunning |
| Current slice | S1 complete and locally verified; awaiting PR/CI review |
| S1 verification | Focused race tests, UI unit/type/lint/build, docs tests and browser regressions pass; exact coverage below |
| Persistent PR watcher | Planned, not implemented |
| Merge-conflict repair | Planned, not implemented |
| Production configuration | No repository automation or model credentials changed by this work |

Do not enable or advertise Baby sit or conflict-only behaviour on the strength
of S1. The existing automatic mode still starts from a failed `workflow_job`,
attempts one repair per failing head and refuses automatic repairs of its own
published commit. It does not discover earlier failures when enabled.

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
| F10 | Model generation runs synchronously ahead of other repairs' check polling in `repairLoop` | Independent bounded worker and observation loops |
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

Status: not started. Split foundation, execution and UI into reviewable PRs if
needed; update this file in every one.

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
- Separate observation from model generation. Limit concurrent model work,
  lease work durably, and permit only one publishing attempt per PR.
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
| [#1368](https://github.com/eyupio/zoomies/pull/1368), open | S1, UX follow-up and this handoff | Local checks below pass; latest CI pending | S2, S3, S4; start with independent observation and durable watches |

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

Correction `c1eab9dc` is published to PR #1368; CI is rerunning. Carry it into
the stacked [PR #1369](https://github.com/eyupio/zoomies/pull/1369) without dropping
its S2a implementation or handoff. The new CI result remains separate from
these local checks; an action-download failure is not an application test result.

The full repository `make test-short`, all browser suites outside the affected
repair pages, a live-model evaluation and end-to-end Eli repairs against real GitHub PR
branches were not run. Publishing this implementation PR is separate from
validating Eli against a real repair target. Initial sandbox-only unit failures were process-execution restrictions;
the complete unit suite passed with normal local process execution. One early
browser run used the previous binary during a rebuild and was discarded.

The next PR should address F10 (observation independent from model generation)
as part of the S2 foundation, then durable watch admission and recovery. F9's
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

Useful commands (see `AGENTS.md` for prerequisites):

```sh
make openapi
npm --prefix web run check
npm --prefix web run test:unit
make test TEST_PKGS='./internal/github ./internal/controller ./internal/api ./internal/store' TEST_RUN='Eli|Repair|MigrationNamesAreFixedAndNewPrefixesAreUnique'
make lint
```

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
2. Check the working tree and current base. Preserve unrelated user changes.
3. Continue the first unfinished acceptance criterion. Read the existing store,
   API and model boundaries before adding another path around them.
4. Add regression tests for the observable behaviour and regenerate contracts
   when changed. Record actual results and limitations above.
5. Before creating/updating the PR, update the checkpoint, checklist and ledger
   in the same commit set. Add the real PR link once assigned. State explicitly
   which user-visible capabilities are still unavailable and what the next PR
   should implement. Do not require the next session to reconstruct this chat.
