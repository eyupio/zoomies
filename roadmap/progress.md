# Work-package record

The one list for the follow-on roadmap. Every package in
[ROADMAP.md](../ROADMAP.md) has a row here, and the row is where its state
lives: the plan says what a package is for, this file says where it has got
to. Keep it current in the same pull request as the work.

## How to read a row

**Status** is one of:

| Status | Meaning |
| --- | --- |
| `not_started` | Nothing merged. |
| `in_progress` | A branch exists. |
| `implemented` | Merged, CI green, tests present. The gate it serves may still be pending. |
| `validated` | Its acceptance criteria met with evidence linked from [validation/](validation/). |
| `blocked` | Waiting on something named in the row. |
| `superseded` | The need is met by something else; the row links to it and to the evidence that it meets the same criteria. |

**Classification** is what the reconciliation (ZF-001) found on `main` at
`6d12a72`: *existing, needs validation* means the capability is there and
the work is proving it; *extension* means the seam exists and the work
builds on it; *new* means there is nothing to extend.

**Session** records the Claude model and effort that did the work, whether it
ran as one session or an orchestration, and how many review rounds it took
before merge, so that [agent-models.md](agent-models.md) can be judged on
evidence.

## Phase 0: baseline

| ID | Package | Classification | Status | Depends on | Session | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| ZF-001 | Reconcile completed work and define the next slice | new | `implemented` | the improvement plan (done) | Claude Fable 5.1, `high`; one orchestration of 12 mappers, 12 adversarial verifiers and a document critic; 1 review round | [validation/baseline-6d12a72.md](validation/baseline-6d12a72.md); [ROADMAP.md](../ROADMAP.md) §5–7 carry the classification of every package |
| ZF-002 | Support matrix, invariants and measurement contract | mixed | `implemented` | ZF-001 | docs as ZF-001; code slice: Claude Fable 5.1, `high`, one session, 1 review round | [support-and-measurement.md](support-and-measurement.md); the code slice is merged: readiness names the schema, a held job's timeline says so with `waiting` and `approved`, and the completed count is split four ways. `validated` waits on the owner's reference host (decision 12) for the recorded OS and runtime versions |
| ZF-003 | Supply-chain hygiene and three corrections | new; no behaviour change | `implemented` | nothing | Claude Fable 5.1, `high`, one session, 1 review round | every `uses:` in the four workflows is a commit with its release beside it; `.github/dependabot.yml` covers actions, Go modules, npm under `web/` and the images under `deploy/`, weekly and grouped; `govulncheck` runs on every change and weekly against `main`; the three pages say what the code does, and the payload carries `schema_version: 1`. `validated` when CI has run on the pins and the first Dependabot pull requests arrive |

## Phase 1: correctness and security under failure

| ID | Package | Classification | Status | Depends on | Session | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| ZF-101 | Enforce GitHub target boundaries everywhere | new (jobs carry no installation identity; label-only matching on all four paths) | `done` | ZF-002 | PR1 and PR2: Claude Opus 5, `ultracode`; one orchestration of 8 subsystem mappers and 8 adversarial verifiers for PR1, a smaller one of 3 and 3 for PR2, each followed by one session | PR1 merged as #99: a job carries the installation covering its repository, `scheduler.Eligible` asks enabled, then installation, then labels, and all four matching paths go through it. Migration `0012` (not `0010`: 0010 and 0011 shipped since the plan was written). PR2 merged as #100: poll freshness and the rate-limit hold are both per installation, freshness credited by the delivery's repository rather than by the secret that verified it. PR3 done: the runner-group fallback is a `pool.runner_group_unresolved` warning on the drawer and the pool's own page instead of a log line, and `docs/hosts-and-pools.md` says a pool belongs to one installation and that GitHub, not Zoomies, makes the final dispatch decision inside an organisation. Every behavioural test was run against the code with its rule removed and confirmed to fail first. All three pull requests are done: the two-installation tests pass on every path, and ineligible work carries its reason on the Jobs page through the drawer's note |
| ZF-102 | Make runner and agent reconciliation convergent | mixed (mechanics exist; adoption on restart and a controller lock do not; the log-relay host check landed with the security review) | `done` | ZF-002; N02 needs `main` deployed | PR1: Claude Fable 5.1, `xhigh`, one session. PR2: Claude Opus 5, `ultracode`; one orchestration of 3 subsystem mappers and 3 adversarial verifiers, then one session | PR1 and PR2 of 4 done. PR1: the "Reconciliation invariants" section in `docs/architecture.md` with `internal/controller/invariants_test.go`. PR2: a database lock and a controller lease, with `--takeover` and `controller.lease_lost`. **Half of PR2 was already done**: the log relay has checked the authenticated host since PR #90's security review, with a test (`TestLogRelayRefusesAnotherHostsStream`), so only the lock and lease were outstanding. PR3 done: an agent adopts every runner already on its host before its loops start, and the controller answers each heartbeat with the runners it has no row for so that only those are reaped. `docs/upgrading.md` no longer hedges: a restart keeps its runners on a single-VM install too. PR4 done: late reports settle the workload without resurrecting a terminal row, `task_issued_at` survives the queue a restart drops, `host.duplicate_agent` detects a shared credential by session alternation, and the restart-boundary table covers seven boundaries. All four pull requests are done |
| ZF-103 | Reserve host resources and enforce bounded admission | extension (slot model complete; no host resource reporting) | `in_progress` | ZF-102 | 103a: Claude Opus 5, `ultracode`; one orchestration of 30 reviewers over the diff | 103a done and merged as #115: an agent reports its host's CPUs, memory and the disk behind its work directory, sized by the daemon that will run the runners rather than by the agent's own cgroup share, and a re-join keeps the operator's reserve. Migrations `0016` and `0017`. Its own adversarial review then found three test gaps in it, all confirmed by mutation and closed in #118: the byte-to-MB conversion was unpinned (raw bytes into `disk_total_mb` passed the suite), nothing connected measuring a host to reporting it (the two lines that put the figures on the wire could be deleted with everything green), and `f_bavail` versus `f_bfree` had no test. The figures reach the Hosts page in #118. 103b, the admission half, remains |
| ZF-104 | Verify control-plane access and secret boundaries | mixed (matrix and most tests exist; log relay unscoped to host; streams never re-check credentials) | `done` | ZF-101, ZF-102 | Claude Opus 5, `ultracode`; one orchestration of 10 surveyors over the nine assertions of PR1, then one session per pull request | All five done. PR1 merged as #116 (the walks and the secret searches). **PR2 was already implemented**: the log relay has bound to the authenticated host since ZF-102, with a test -- what was missing was that the test's indistinguishability check could not fail, and it now can. PR5 merged as #118 with the proxy fix and the join limiter. PR3 and PR4 in #119. Every behavioural test was run against the code with its rule removed and confirmed to fail; two assertions that could not be made to fail were deleted rather than shipped. `docs/security.md` §7 now names the test behind each claim |
| ZF-105 | Bound cleanup, retention and external failure handling | mixed (local cleanup exists; failure invisible; one listing-failure defect) | `done` | ZF-102, except the first pull request | first slice: Claude Fable 5.1, `high`, one session, 1 review round | All six pull requests done: the listing-failure defect, the cleanup record on the runner row with `runners.cleanup_failed` (migration `0015`), orphaned sidecars, the cache prune guard, the drain timeout (migration `0016`), and the typed rate-limited error with a per-installation hold. Of the verifier's three extra findings for the second: the work-directory leak is closed on the backend where it is reachable, the GitHub-failure-during-cleanup one is what `runners.cleanup_failed` answers, and the third is stale -- there is no poll-interval channel in the protocol to leave unset |

## Phase 2: operable and usable

| ID | Package | Classification | Status | Depends on | Session | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| ZF-201 | Prove the first successful job through the UI | existing, needs validation, with two narrow extensions | `not_started` | ZF-101 | | Size M |
| ZF-202 | Actionable day-to-day diagnostics | mixed (substrate exists; no bundle, no per-job explanation) | `not_started` | ZF-102 | | Size L; its failing tie-break test is fixed in this pull request |
| ZF-203 | Implement and prove backup and restore | new (prose only today) | `not_started` | ZF-102 for automatic unfencing only | | Size L |
| ZF-204 | Safe releases, upgrades and version compatibility | mixed (skeleton exists; policy, skew, guards, backup-before-migrate and any upgrade test do not) | `not_started` | ZF-203 for the backup primitive; ZF-003 first | | Size L across small pull requests |
| ZF-205 | Usable telemetry and bounded history | mixed (substrate exists; four series, prune tests, a load fixture and the measurement do not) | `not_started` | ZF-002; ZF-101 and ZF-102 for the real latency series | | Size M in three slices |

## Phase 3: real use, drills and Gate F

| ID | Package | Classification | Status | Depends on | Session | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| ZF-301 | Extend the real end-to-end harness | mixed (one polling-mode test; no result categories, no cleanup ledger, no real container in any test) | `in_progress` | ZF-002; 301c on the owner's credentials | 301a and 301b: Claude Opus 5, `high`, one session each | Size L in three slices. 301a done: every prerequisite is checked before anything is created, a required mode makes a missing one `blocked` rather than a silent exit-zero skip, each scenario writes a JSON result carrying its category, commit, marker, run link and any residual cleanup, `test-e2e-required` runs the verifier over those records rather than trusting `go test`'s exit code, a ledger outside the temp directory records every resource before it exists and a start-up sweep clears what an earlier crashed run left on GitHub, the orphan check now asks GitHub and the host instead of asking Zoomies about Zoomies, per-run labels stop two runs colliding, and the waits fit the Makefile's timeout with an untagged test pinning that. 301b done: a `drill`-tagged tier runs the built binary as a real controller and a real remote agent joined by a join token, against an in-process fake GitHub and a stub runner on the `process` backend, and watches a workload actually appear on the machine and go away. Two drills, both green and on every pull request. **It found its own hole first**: written the obvious way the lifecycle drill passed with `deleteRegistration` commented out, because the ten-minute reaper's first sweep lands where it was looking -- it was testing the backstop. The sharp assertion moved to a second drill on the path where deleting the registration really is Zoomies' job, and that one fails in seventeen seconds when it stops. 301c (the real scenarios, owner-gated) remains |
| ZF-302 | Restart and recovery drills | mixed (fake-level tests exist for every fault class but disk; no process-level drill) | `not_started` | ZF-301b | | Size S once 301b exists |
| ZF-303 | Controlled beta and the readiness record | new; owner-gated | `not_started` | everything above; the owner's deployment, credentials and second operator | | Size M for the template and helper; the observation is the owner's |

## Open items carried over from the improvement plan

| ID | What | Where it goes |
| --- | --- | --- |
| N02 | Runners stuck in `registering` on a dev instance; not reproducible on `main` | The first observation in ZF-303 on a deployment of `main`; `runners.not_progressing` now names which half it is. |
| D16 | The brand descriptor drawn into the wordmark | The owner's; not roadmap work. |

## Log

Newest first. One line per event that changed a row.

* 2026-09-07: ZF-105 done. Five of its six pull requests had already landed in
  earlier sessions -- the cleanup record, sidecars, the cache guard, the drain
  timeout and the rate-limit hold are all in the code, which the package's own
  status had not caught up with. What was genuinely outstanding was one of the
  verifier's extra findings for the second: a work directory that leaks when its
  workload goes away out of band. **Half of that finding is not reachable**: the
  Docker backend learns the directory from the container's label, so losing the
  container loses the path, but nothing sets `Spec.WorkDir`, so the backend
  never creates one and there is nothing to leak. The process backend's half is
  live and is fixed here. Create makes the runner's directory and clones the
  tools tree into it -- hundreds of megabytes -- before it writes the metadata,
  and `List` skipped any directory whose metadata would not read, so an agent
  killed in that window left the tree on the host with nothing that would ever
  look at it again. Such a directory is now listed as a workload that is gone,
  and the agent's existing orphan path removes it under the same grace and the
  same "only after a successful poll" rule as a container nothing claims. It is
  reported only once nothing has written to it for fifteen minutes, which is
  what tells an abandoned clone from one in progress: the clone touches the
  directory with every file it lays down. Both halves are pinned -- skipping the
  directory fails the test, and so does dropping the grace.
* 2026-09-07: ZF-104 done, in four pull requests rather than five. The package
  was seventy percent "tests that already exist", so the first work was finding
  out which of its claims anything actually held up: ten surveyors over the nine
  assertions of PR1 found that **PR2 was already implemented** -- the log relay
  has bound to the authenticated host since ZF-102 -- and that its test's
  indistinguishability check was `err.Error() == ""`, which is never true for a
  non-nil error. The agent route walk covered three of five routes, leaving the
  task poll and the log relay with no negative test; the scoped-token gate had
  one test across sixty-six routes, and that one was really testing a handler's
  second check on its body; secret absence was proved for seven admin reads on
  the success path only. All of it is now walked, with a coverage guard on each
  table so a new route or action that arrives without a row fails rather than
  ships. Two departures from "tests only", both stated in the pull requests: a
  body over the limit now answers 413 rather than 400, which is what the webhook
  endpoint has always answered for the same condition, and the test harness
  captures the controller's log because the secret-absence claim names it and
  there was no seam. **Two assertions were deleted rather than shipped**: a
  needle matched against marshalled JSON cannot match a PEM whose newlines the
  encoder escaped, and "the log relay is exempt from the body limit" was
  unfalsifiable where it was first written, because the relay answers 404 before
  it reads a byte. PR5's proxy bug was real: `X-Forwarded-For` has always been
  believed only from a trusted proxy and `X-Forwarded-Proto` was believed from
  anyone, in three places, which let a caller decide whether an `https://` Origin
  counted as same-origin and whether the response carried HSTS. One deliberate
  deviation: the package says to reuse the login limiter on the join route, and
  it got a counter of its own at the same setting instead -- sharing would let an
  attacker hammering `/agent/join` lock the administrators out of the page they
  would use to stop it. PR3 and PR4 close the last two: a live stream re-checks
  its credential on every heartbeat and ends when it no longer stands, and a
  Playwright spec proves a name carrying markup is text and a runner's output
  cannot retitle the page, clear it or open a dialog. **Not tested**: the log
  viewer's link predicate (the terminal draws to a canvas and there is no UI unit
  runner) and the client's `end` listener (the shipped heartbeat is twenty
  seconds); both are named in #119 rather than glossed.
* 2026-09-07: two reported UI defects, both of which turned out to be one layer
  deeper than the page. The Overview's tiles counted every job GitHub reported
  because `StatsSince` had no scope at all, while the panels below them already
  filtered and already carried the switch -- so the two halves of one page were
  answering different questions. The usage report had the same hole, and its own
  comment acknowledged the hosted-runner jobs as the reason for a `COALESCE`.
  Both now share `managedJobSQL()` with the Jobs page, so a repository's
  runner-hours and its job list are about the same jobs. The Overview keeps a
  switch; usage does not, because it answers what this fleet consumed and
  somebody else's hosted runner has no answer to contribute. Both scopes travel
  in one stats payload rather than behind a query parameter, because the same
  numbers arrive over the event stream and that is one frame for every viewer;
  migration `0018` records both on every sample for the same reason.
* 2026-09-07: ZF-301b, the drill tier, and the first time anything in this
  repository has run the product as an operator gets it: the built binary as a
  controller, a second copy of it as a remote agent that joined with a join
  token, and a real workload on the machine running the test. GitHub is the
  in-process fake; the backend is `process` with a stub runner staged on disk,
  which the backend accepts because it skips its download when the tools tree is
  already there. So the tier needs no credentials, no Docker daemon and no
  network, which is what lets the lifecycle drill run on every pull request.
  It also had to learn that the fallback poller only finds *queued* jobs, so the
  drill drives the fake and delivers signed webhooks -- which is what puts the
  timing in the drill's hands, and what ZF-302's fault drills will need.
  The finding worth keeping: the obvious version of the lifecycle drill passed
  with `deleteRegistration` commented out. The reaper's first sweep is a minute
  after the controller starts, which is where the drill was looking, so it was
  testing the backstop. It cannot be sharpened in place either -- an ephemeral
  runner that finishes its job exits by itself and nothing of Zoomies' deletes
  the registration, correctly, because real GitHub removes a just-in-time runner
  itself and the fake does not model that. The sharp assertion is a second
  drill, on an operator removing an idle runner, where GitHub tidies nothing
  and Zoomies must; it fails in seventeen seconds with the deletion removed.
  Both drills name the runner they assert about rather than counting what is
  left, because while a job is queued the scheduler is right to replace a
  removed runner immediately. Each run appends its row to
  `roadmap/validation/drills.md` with timings, so a pass that quietly becomes a
  different pass is visible. **Not done here**: the six fault drills (kill the
  controller mid-job, kill the agent, a dead Docker socket, a rate limit, a
  failing JIT endpoint, a small filesystem) and the Docker backend's own
  runtime qualification, which needs a daemon this container does not have.
* 2026-09-07: ZF-301a, the harness's honesty. The end-to-end harness could
  not have failed: a skip is exit code zero, so a run that had never once
  talked to GitHub reported the same green as one that had run the scenario --
  and it looked for the `gh` CLI in the middle of the scenario, after it had
  already made an installation and a pool on a real organisation, then skipped
  and left them there. Its waits totalled twenty-seven minutes behind a
  twenty-minute Makefile timeout, so it could never reach its own last
  assertion; that assertion asked Zoomies whether Zoomies had cleaned up, which
  is not evidence, and the README described a cleanup-on-failure that did not
  exist. Now: preflight before anything is created, a required mode where a
  missing prerequisite is `blocked`, a JSON result per scenario, a ledger
  outside the temp directory with a sweep for what an earlier crash left, the
  orphan and container checks asked of GitHub and the Docker daemon, per-run
  labels, and a `verify` command so the gate reads the records rather than an
  exit code. The budget guard carries no build tag, so it runs in ordinary CI
  rather than only where credentials exist -- it reproduced the timeout defect
  against the real Makefile before the fix. **No real run has happened**: this
  slice makes the harness capable of failing honestly, and the credentials it
  needs are the owner's (ZF-301c).
* 2026-09-07: ZF-101 is done. Its third pull request landed with ZF-102's, and
  the row had been left `in_progress`; all three are merged and the acceptance
  holds.
* 2026-09-07: ZF-102's fourth and last pull request, and the package is done.
  Four things. A runner given up as lost whose host comes back with the
  container still running is no longer dropped as an illegal transition: the
  terminal row stands, because the fleet has already told an operator and the
  job's timeline that the runner was gone, but the message is corrected, a new
  `runner_returned` timeline entry withdraws the `runner_lost`, and the
  workload is left alone while a job is still on it and removed once none is.
  Before this it ran for ever -- the reaper only takes untracked workloads and
  the retention sweep only finished ones, so nothing in the system would ever
  have collected it. `task_issued_at` on the runner row keeps the one fact the
  in-memory queue must not lose across a restart, stamped again on every
  redelivery so the provision timeout is not counted from an attempt the host
  never received. `host.duplicate_agent` sees two agents sharing one host id by
  the one thing they cannot hide -- handing a session back and forth, which a
  single agent cannot do because it never reuses one -- and reports without
  refusing, per decision 14. The restart-boundary table covers seven boundaries
  and would fail on all seven if a late result were dropped. Two departures
  from the package text, both deliberate: the code is `host.duplicate_agent`
  rather than `hosts.*`, to sit with `host.unhealthy` and
  `host.cordoned_with_work`; and the session travels as a header rather than a
  field on three messages, because the runner report's body is a bare JSON
  array by design and one middleware sees every authenticated agent call.
* 2026-09-07: ZF-102's third pull request, the package's one genuine defect.
  A fresh agent knew nothing, and the reconciler removes what nothing claims,
  so every job running on a host was destroyed two minutes after its agent came
  back -- on the default single-VM install, that is every controller restart.
  The fix is small because `adopt` already existed for the redelivered-create
  path; what was missing was calling it for everything on the host before the
  loops start.
  
  Adoption alone would have traded one fault for another: an agent that adopts
  everything can no longer tell litter from live work, so a workload whose
  runner the controller deleted while the agent was down would leak forever.
  Hence the second half, and the rule behind it -- the agent never decides on
  its own that something is litter, because it cannot tell "the controller
  deleted this" from "I have forgotten it", and only one of those costs
  somebody a job. The controller names the runners it has no row for, and only
  those are released. A runner belonging to another host is deliberately never
  named: that is a different fault, and answering it would invite one host to
  remove another's work.
  
  `TestWithoutAdoptionARestartWouldReapALiveRunner` is kept deliberately: it
  asserts the old behaviour still destroys the fixture, so the test above it
  cannot quietly stop testing anything.

* 2026-09-07: ZF-102's second pull request, one controller per database. Its
  first half was already merged: the plan says "the log relay takes the
  authenticated host id", and `AcceptLogStream` has compared the reporting
  host against the stream's owner since PR #90 closed the auth and web-UI
  security review, with a test that an intruding host is refused and told
  nothing it did not already know. The plan predates that merge. What was
  outstanding is the lock and the lease, and they are two different guards on
  purpose: an advisory lock on a file beside the database catches a second
  process on this host and is dropped by the kernel however the process dies,
  while the lease row catches the restored copy on a shared filesystem or the
  controller on another machine, which no lock here can see. A lost lease is
  reported and never cleared, because there is no state this process reaches
  on its own that makes two schedulers safe again.

  The lease nearly shipped with a defect worth recording, because the next
  slice touches the same restart path: the service unit is `Restart=always`,
  so a controller that is killed returns within seconds under a new holder id,
  finds its predecessor's lease renewed moments ago, and refuses to start --
  a restart loop for the whole lease window after every crash. A lease naming
  this same host is therefore reclaimed at once. That is sound only because
  the caller already holds the file lock, which proves nothing else on this
  host has the database; the cross-host case, the one the lease exists for,
  is untouched.

* 2026-09-07: ZF-101's third pull request, and the package's last. The runner
  group falling back to GitHub's Default was a log line; it is now a standing
  warning, because Default is the group every repository the installation
  covers can reach, so a pool that asked to be fenced into a group and quietly
  was not is running its jobs somewhere wider than its operator asked for. Two
  things the plan did not name but the code needed: the three fallbacks say
  which one happened, because "the group is not there", "GitHub would not say
  which groups exist" and "this is a repository, and groups are an
  organisation's" have three different fixes; and an unresolved name is
  deliberately not cached, so that the warning clears by itself on the next
  create once an operator makes the group, at the cost of one API call per
  runner on a pool that is misconfigured.

* 2026-09-07: ZF-101's second pull request, the poller made per-installation.
  The freshness question is answered from the delivery's repository, as the
  first pull request's record said it would have to be: a delivery credits the
  installation that owns the repository named in it, never the one whose secret
  verified it, so an installation whose secrets have drifted cannot be told to
  stand down on its neighbour's traffic. The rate-limit hold became a map and
  the sweep continues past a held installation. One thing worth knowing for the
  later slices: go-github caches an exhausted quota on the client, so a fake
  that sets the rate-limit headers globally rate-limits every installation at
  once; a test that wants one installation held has to use the 429 secondary
  form. The first draft of that test did not, and passed for the wrong reason.

  Two deliberate departures from the package's wording, recorded because the
  plan is the contract and the next reader should not have to rediscover them.
  The plan says "`LastAcceptedDeliveryAt` takes an installation"; instead it is
  left alone and a sibling added, because `Controller.Start` and the
  `pollingOnly` flag that ZF-202 keeps fleet-wide both still need the fleet-wide
  answer, and one query per sweep beats one per installation. The sibling is
  `InstallationsFreshSince`, bounded by the caller's own cutoff: asking for the
  last delivery per installation groups over every accepted row inside the
  retention window, which measured as a full scan of a week of history every
  thirty seconds, where the cutoff makes it an index range. And the plan says
  the tests need "two fakes"; one serves, because the fake's error injection is
  matched on the request path, so a repository-target installation can be
  rate-limited by its own path while its neighbour is not.

  One asymmetry the query inherits from the attribution rule, worth knowing
  before somebody reads it as a bug: where an organisation installation and a
  repository installation both cover a repository, only the repository one is
  credited, so an organisation whose only traffic is for a repository-scoped
  sibling looks silent and keeps being polled. That is the conservative
  direction -- poll rather than stand down -- and it is the same precedence
  every other part of ZF-101 uses.

* 2026-09-07: ZF-101's first pull request, the installation boundary. Three
  decisions taken inside decision 13's frame and recorded here because the
  later pull requests inherit them: a job is attributed to the installation
  covering its **repository**, never the one whose secret verified the
  delivery, which verification deliberately allows to be another's;
  `webhook_deliveries.installation_id` holds the **verifying** installation,
  because that is the one fact about a delivery which cannot be recomputed
  afterwards, and ZF-101's second pull request must therefore derive
  per-installation poll freshness from the repository rather than from that
  column; and the migration clears `matched` and `pool_id` on waiting and
  queued rows it re-attributes but leaves in-progress rows alone, because a
  running job's pool is the record of where it actually ran. The Jobs page
  carries the reason it can derive from the row itself -- the repository no
  installation covers -- and the cross-installation sentence reaches the
  operator through the problems drawer, which is where the scheduler's plan
  is read; a per-job reason on the Jobs page would be a new API field and is
  not in this pull request.

* 2026-09-06: ZF-102's first pull request, the invariants written down and pinned; the package is `in_progress`.

* 2026-09-06: ZF-003 done in one pull request; the package is `implemented`.

* 2026-09-06: ZF-002's code slice done; the package is `implemented`.

* 2026-09-06: ZF-105's first slice, the listing-failure defect in the agent
  reconciler, fixed with a test; the package is `in_progress`.

* 2026-09-06: every Phase 1 to 3 package classified from the package-by-package
  review (12 mappers, 12 adversarial verifiers, one document critic); ZF-003
  added; ZF-001 implemented; two Phase 0 defects fixed in the same pull
  request (the stuck-runner tie-break, the compatibility paragraph).
* 2026-09-06: record created from the reconciliation of the follow-on roadmap
  against `main` at `6d12a72`.
