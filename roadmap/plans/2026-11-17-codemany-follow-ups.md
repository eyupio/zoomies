# Codemany follow-ups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn a review of Codemany's public documentation into the small set of changes it justifies: the comparison page (done), the pointers that send macOS readers to it, a design note and a competitor-list entry that need no decision, and three pieces of work that wait for the owner.

**Architecture:** Documentation and planning changes in the pull request that added this file. [ROADMAP.md](../../ROADMAP.md) version 3.15 now carries the three decisions as 33 to 35 and the work as ZF-246, ZF-247 and ZF-248, so Tasks 5b, 6 and 7 are authorised, in the order section 10 gives. The page and the handoff are an analysis in the sense of [roadmap/README.md](../README.md); the delivery is the roadmap's.

**Tech Stack:** Markdown, MkDocs (`mkdocs build --strict`), Go tests in `internal/docs`.

**Written:** 17 November 2026 against `3b86503`. Method as in [competitive-review-2026-09.md](../competitive-review-2026-09.md): public documentation compared with this repository's code and record, and "not verified" is never a claim that something is absent.

## Where this stands

**Done, in the pull request that added this file:**

- [x] `docs/alternatives/codemany.md`, the comparison page, wired into `mkdocs.yml` and `docs/alternatives/index.md` (both tables, the macOS question under "How to choose", and the page description).
- [x] Task 2, the macOS pointers, in `docs/hosted-runner-services.md`, `docs/should-i-self-host.md` and `docs/home-lab.md`. `docs/faq.md` was read and left alone: its answer links onward only for Windows qualification and the image catalogue.
- [x] Task 3, the dated note on candidate 4 in `roadmap/candidates-from-hosted-services.md`.
- [x] Task 4, `codemany.com` in the weekly search check's `competitors`.
- [x] The three decisions were put to the person running the session on 17 November 2026 and taken; see "Decisions taken".
- [x] Task 5, [decision 0020](../decisions/0020-macos-runners-are-processes-on-a-mac.md), written and indexed.
- [x] `ROADMAP.md` version 3.15, at the request of the person running the session: decisions 33 to 35, ZF-246 to ZF-248 in section 8, three rows in section 10, one input in section 11, the change record, and three `not_started` rows in [progress.md](../progress.md). Decision 0020 is now accepted on that instruction; the owner has not separately confirmed the shape.

**Next unfinished step:** Task 6's design (ZF-246, which needs nothing but the repository), Task 7's evidence (ZF-248, which needs read access to this repository's own CI fleet) and Task 5b's tests and launchd unit (ZF-247, which need no Mac); only Task 5b's real join and job needs an Apple silicon Mac. Task 8 is due three months after 17 November 2026. Update this section, and tick the boxes below, in every pull request that works from this plan.

## Global Constraints

- British spelling, no em dashes, no spaced `--`, comments say why, commit messages are sentence-case imperative sentences with no prefix.
- A comparison says where the other product is better, plainly, and is dated. It is a comparison of documentation, not a benchmark.
- Do not publish a claim of faster execution, stronger isolation or unique capability without evidence ([competitive-review-2026-09.md](../competitive-review-2026-09.md), "Positioning and evidence").
- Do not claim macOS runners until a job has run on a real Mac. [Decision 0020](../decisions/0020-macos-runners-are-processes-on-a-mac.md) chose the shape; the support matrix still says "Nothing planned" and the eight pages that say there is no macOS runner image stay as they are until the evidence exists.
- New or changed delivery work belongs in `ROADMAP.md` first. Version 3.15 added ZF-246 to ZF-248 at the request of the person running the session; anything beyond them goes there before it goes here.

## What the review found

Codemany is a proprietary, paid desktop app that runs GitHub Actions jobs on Apple silicon Macs and systemd Linux machines the customer owns. It costs $20 a month for two machines and $10 for each after that, with a 14-day trial. Its pitch is one changed `runs-on` line, a menu bar app and a public benchmark. The sources were its home, pricing, features, docs (get started, machines, advanced, security, FAQ, compare), changelog and benchmark pages, read on 17 November 2026; the page records the same date.

The two overlap less than the shared goal suggests. Codemany is an app for a few owned machines, strong on macOS. Zoomies is a controller for a fleet, Linux only. Seen from Zoomies, five things came out of it, and three were already on the record before this review:

| Finding | Already tracked? | Where it goes |
| --- | --- | --- |
| macOS runners are Codemany's main draw, and Zoomies has none | Not planned: [support-and-measurement.md](../support-and-measurement.md) says "Nothing planned". Now [decision 0020](../decisions/0020-macos-runners-are-processes-on-a-mac.md), accepted | Decision 1, Tasks 5 and 5b |
| Codemany ships a virtual machine or an LXD container per job on Linux, opt-in | Yes: [candidate 4](../candidates-from-hosted-services.md) (VM per job), ZF-216's surviving half and decision 28's spike, all demand-gated | Task 3 adds one design option and the evidence |
| Codemany ships mirrors on Linux: Go, npm, Docker Hub pull-through and git | Partly: ZF-212's pool-cache recipes cover Go and npm downloads, ZF-245 covers `actions/cache`, and the registry mirror is proposed as ZF-246 | Decision 2, Task 6 |
| Codemany's headline benchmark compares owned, warm hardware with GitHub's 2-core runner | Not applicable: the rule against unevidenced speed claims already holds | Decision 3, Task 7 |
| Nothing in the repository mentioned Codemany | No | Task 1 (done), Tasks 2 and 4 |

The first review in the conversation that produced this plan listed "cold caches by default" as a gap. That was overstated: the per-pool cache, the tool cache and the recipes already ship, and ZF-245 slices 1 to 3 remove the workflow edit for `actions/cache`. What is missing is the registry mirror, and it is already proposed.

## What this review did not check

- Codemany was not run. Every statement is from its public pages, which change several times a week (its changelog had eight releases between 10 and 16 November).
- Its guides, articles and "problems" pages were not read, and neither was its benchmark repository's code. The benchmark's numbers are quoted nowhere in the repository.
- Whether Codemany works without a GitHub App or webhook, and how it learns of queued jobs, is not documented on the pages read. The comparison page makes no claim about it.
- Whether a Zoomies controller can run usefully with no address GitHub can reach, on polling alone, is documented only as a degraded fallback ([quickstart.md](../../docs/quickstart.md), [configuration.md](../../docs/configuration.md)). Codemany's lighter onboarding makes that question worth an answer; it is not answered here.

## Decisions taken

Put to the person running the session on 17 November 2026, one at a time, each with a recommendation. They were authorised as work by `ROADMAP.md` version 3.15 on the same day, at the request of the person running the session.

1. **macOS runners: build a backend, as a process on a Mac first.** The recommendation was to leave macOS out for now, because Apple caps a Mac at two concurrent macOS virtual machines and the project claims only what a test has run on. The choice went the other way, and it was narrowed to the cheap shape in the same way [decision 0003](../decisions/0003-windows-runners-are-processes-on-a-host.md) did for Windows: actions/runner's `osx` release as a process through the `process` backend, which the code already maps `darwin` to, with no isolation, flagged beta, and a virtual machine per job only if a user asks. Recorded as [decision 0020](../decisions/0020-macos-runners-are-processes-on-a-mac.md).
2. **Cache follow-ups: ZF-246 only.** As recommended. The per-host registry mirror is authorised; Go, npm and git mirrors are not added, because ZF-212's recipes and ZF-245's cache service already cover those downloads. Measure before adding one.
3. **Performance story: Zoomies' own measures.** As recommended. Publish queue wait and runner startup, which the fleet already records, with the method and the evidence. No multiplier against GitHub-hosted runners and no like-for-like comparison.

### What ROADMAP.md now says

Version 3.15 of [ROADMAP.md](../../ROADMAP.md), 17 November 2026:

* **Section 4, "Taken on 17 November 2026":** decision 33 (macOS runners, as processes on a Mac, against the recommendation), decision 34 (ZF-246 only) and decision 35 (the fleet's own measures).
* **Section 8:** ZF-246, a registry mirror on every host; ZF-247, a Mac as a runner host on the `process` backend; ZF-248, the fleet's own queue-wait and startup figures, published.
* **Section 10:** three `later` rows, after ZF-245. **Section 11:** one owner input, an Apple silicon Mac for ZF-247's real run. **Section 13:** the change record.
* **[progress.md](../progress.md):** three `not_started` rows after ZF-245.

The owner has not separately confirmed decision 33's shape; [decision 0020](../decisions/0020-macos-runners-are-processes-on-a-mac.md) says so in its status line.

## Tasks

### Task 1: The comparison page

Done. See "Where this stands".

### Task 2: Send macOS readers to the page

Done. No decision needed. Each of these already tells a reader "Zoomies has no macOS runner image" and sends them somewhere; add the page to where they are sent.

**Files:** modify `docs/hosted-runner-services.md` (the "You need macOS" bullet), `docs/should-i-self-host.md` (near line 32), `docs/home-lab.md` (near line 28); read `docs/faq.md` near line 128 and change it only if it already links onward.

- [x] **Step 1:** In each file, read the sentence first. Add one clause linking to `alternatives/codemany.md` (the path relative to that file) naming Codemany as a way to run macOS jobs on Macs the reader owns, beside the options already listed. Do not add it where the text lists only hosted services and has no sentence about owned hardware to attach it to. *Outcome:* three files changed. `docs/should-i-self-host.md` also says "no macOS runner image" under "Plan capacity across platforms and sizes"; that sentence is about the support matrix, links to it, and was left alone so the page does not link to Codemany twice. In `docs/hosted-runner-services.md` the bullet sits under "When a runner service is the better choice", so the added sentence says outright that Codemany is not a per-minute service.
- [x] **Step 2:** `make build-nogui`, then `go test -count=1 ./internal/docs/`. Expected: pass.
- [x] **Step 3:** Build the site with `pip install --require-hashes -r docs/requirements.txt` and `mkdocs build --strict`. Expected: no warnings, and `site/alternatives/codemany/index.html` exists.

### Task 3: Add the evidence and one design option to candidate 4

Done. No decision needed. [candidates-from-hosted-services.md](../candidates-from-hosted-services.md) section 4 weighs a micro-virtual-machine backend against a provider used per job. Codemany adds evidence and a third shape.

**Files:** modify `roadmap/candidates-from-hosted-services.md`, under its "Checked again" convention: a dated note, not a rewrite of the section.

- [x] **Step 1:** Add a note dated 17 November 2026 saying that a commercial product in this market ships a virtual machine per job (KVM on Linux) and a container or virtual machine per job through LXD, both opt-in, and reports that they are slower and start with cold caches. Cite its [advanced documentation](https://codemany.com/docs/advanced#isolation). *Outcome:* a new "Checked again, 17 November 2026" section after the September one. It reports what Codemany's page says about each mode, and that an LXD container starts in a second or two while the virtual machine is the slower one; "slower" is not said of the container.
- [x] **Step 2:** Name the third shape: an LXD container or LXD virtual machine per job as a backend that needs LXD on the host and no Firecracker or Kata. Say it is a shape to weigh when the work is picked up, not a proposal. *Outcome:* done, with one addition this step did not ask for: the note says an LXD container shares the host's kernel and an LXD virtual machine does not, so the entry's reason for wanting a virtual machine applies only to the second.
- [x] **Step 3:** Leave the status line, "demand-gated", exactly as it is. *Outcome:* the entry and the Summary table are untouched.
- [x] **Step 4:** `go test -count=1 ./internal/docs/`. Expected: pass.

### Task 4: Watch Codemany in the weekly search check

Done. No decision needed. `weekly-seo/config.yml` lists the domains compared in the AI citation check. It names Cirun and four hosted services and not Codemany, which publishes Markdown versions of its pages and a `llms.txt` for agents and so is likely to be cited.

**Files:** modify `weekly-seo/config.yml`.

- [x] **Step 1:** Add `codemany.com` to `competitors`. Do not touch `index_sample`: its comment says to change that list only on purpose. *Outcome:* added beside `cirun.io`, with a comment saying why and when. The file still parses and `index_sample` still has its ten entries.
- [x] **Step 2:** Leave the new page out of `index_sample`. That list is held steady week to week so "N of M indexed" can be compared; the page joins it only when the owner wants it counted, with a line in the change log.
- [x] **Step 3:** `go test -count=1 ./internal/docs/`. Expected: pass.

### Task 5: Record the macOS decision

Done. [Decision 0020](../decisions/0020-macos-runners-are-processes-on-a-mac.md) is written, in the five parts the decisions README requires, and indexed there. It was written as "proposed, for the owner to ratify", and is now "accepted, on the instruction of the person running the session", with a note that the owner has not separately confirmed the shape.

- [x] **Step 1:** Write the record. *Outcome:* it names the two shapes weighed, what the code already does on `darwin` (read, not run), what is not known, and what it costs. It keeps "Zoomies has no macOS runner image" on all eight pages that say it, until a job has run on a Mac.
- [x] **Step 2:** Add its row to `roadmap/decisions/README.md`.
- [x] **Step 3:** `ROADMAP.md` was left alone when the record was written, and edited afterwards when the person running the session asked for it; see "What ROADMAP.md now says".

### Task 5b (ZF-247): prove a Mac joins and runs a job

Authorised as ZF-247 in `ROADMAP.md` version 3.15. Steps 1 and 2 need no Mac. Step 3 needs an Apple silicon Mac the owner provides, and steps 4 and 5 wait for it. The first slice is proof, not a feature, and it is the cheapest thing that can say whether decision 0020 holds.

- [ ] **Step 1:** Write tests that pin what the `process` backend does on `darwin`: `runnerAsset("darwin", "arm64", v)` names the `osx-arm64` archive, a 32-bit `darwin` build is refused, and the preflight reaches the same answers off Linux that it does on Linux. Put them beside `process_test.go`, table-driven, and watch each fail with the change it guards removed.
- [ ] **Step 2:** Find out whether the installer writes a launchd unit for an agent, not only for a controller (`internal/installer`, `service_*.go`). If it does not, that is the one piece of real code in this slice; write it the way the systemd and Windows units are written, behind the same tests.
- [ ] **Step 3:** On the Mac: `zoomies agent join`, create a pool selected by `os=darwin`, run a workflow with one job that prints `uname -m` and `sw_vers`, and confirm the work directory is removed and the registration is single-use. Write what happened, including every failure, into `roadmap/validation/`, dated, with the commands.
- [ ] **Step 4:** Add the macOS agent row to [support-and-measurement.md](../support-and-measurement.md), in the form the Windows row has: shape and runtime stated separately, built and unit-tested on one side and "never yet run" or "run once on <date>" on the other. Do not move any docs page off "no macOS runner image" until Step 3 has evidence.
- [ ] **Step 5:** Only then change the eight pages that say it (`docs/index.md`, `docs/home-lab.md`, `docs/should-i-self-host.md`, `docs/hosted-runner-services.md`, `docs/costs.md`, `docs/github-actions-alternatives.md`, `docs/alternatives/index.md` and the comparison page), and the security page's wording for a macOS pool. Say "beta" and say that a job runs as the agent's user.

STOP if: the agent does not start on `darwin` for a reason other than the launchd unit; the runner's `osx` release needs something the backend does not provide (a signing or quarantine step, for instance); or the job can reach another user's files by a path the docs do not already warn about. Report it, and do not widen the slice.

### Task 6 (ZF-246): the registry mirror

Authorised in `ROADMAP.md` version 3.15 (decision 34), as ZF-246 only. The design is sketched in [candidates-from-hosted-services.md](../candidates-from-hosted-services.md) section 1, layer 2: a read-only, per-host pull-through mirror that the agent owns, with `agent.registry_mirror.enabled` and `agent.registry_mirror.max_bytes` and their `ZOOMIES_AGENT_REGISTRY_MIRROR_*` overrides. Codemany's version is a useful bound on scope: it holds public Docker Hub images only, refuses pushes, never caches private images and falls back to a direct pull when it is down. Take those four as requirements, and write the failure rule the cache service already has: a broken mirror slows a pull and never fails a job. Go, npm and git mirrors are out of scope.

- [ ] **Step 1:** Read [cache-service.md](../cache-service.md) and `internal/cacheservice`. The mirror is not part of that package, which is stdlib only and knows nothing about the fleet.
- [ ] **Step 2:** Write a dated design under `roadmap/plans/` before any code, and stop for review.
- [ ] **Step 3:** Each setting arrives with a `docs/configuration.md` row and its override; each finding code with a `docs/problem-codes.md` row; `make generate` is run, not hand-edited.

### Task 7 (ZF-248): publish what the fleet measures

Authorised in `ROADMAP.md` version 3.15 (decision 35). The decision chose Zoomies' own measures: queue wait and runner startup, which the product already records and shows (the Overview's startup and registration times, the Usage page's queue wait, and the MCP tool `queue_wait_breakdown`). No like-for-like comparison with GitHub-hosted runners, and no multiplier. The evidence comes before the page.

- [ ] **Step 1:** Read [ui.md](../../docs/ui.md) (Overview and Usage) and the store fields behind them, and write down exactly what each figure measures, from which event to which event. A figure the page cannot define in one sentence is left out.
- [ ] **Step 2:** Take the data from a real fleet, not the demo. This repository's own CI runs on a Zoomies fleet (`zoomies-linux-x64`), so use that over a stated window. Report the number of jobs, the median and the 90th percentile of each figure, and the runner startups that failed. Never leave failures out.
- [ ] **Step 3:** Put the evidence in `roadmap/validation/`, dated, with the commands or API calls that produced it, before any page exists. The `make bench` rule applies to anything that changes code for speed: run before and after, once per package, with the `benchstat` comparison in the commit message.
- [ ] **Step 4:** Write the page in `docs/`, with the method in the same place as the numbers. Say what the fleet is (hardware, host count, pool settings), because a startup time is a statement about a fleet.
- [ ] **Step 5:** Do not compare with GitHub-hosted runners on this page. If the owner later wants that, the protocol is: fix the hardware first (a 2-core virtual machine running a Zoomies container runner beside GitHub's 2-core runner, then separately the best machine you own); pin each project to one commit; start both sides in one run and count a timing only when both finished; time cold and warm builds separately; publish the raw data; and print no single multiplier.

### Task 8: Keep the page honest

- [ ] **Step 1:** Re-read Codemany's pricing, docs and changelog three months after the review date, or sooner if the owner hears it has added a Windows, GPU or hosted-controller option. Update the page's "reviewed in" month and every figure on it, or change the sentence that went stale.
- [ ] **Step 2:** If the page and Codemany's own compare page disagree about what Codemany does, the page follows Codemany's documentation and says when it was read.

## STOP conditions

- A test in `internal/docs` fails on the new page for a reason this plan does not mention: report it, do not loosen the test.
- `internal/provenance`'s list, which the owner supplies and which is not in the tree, turns out to include Codemany. The comparison pages name their subjects by design, so report it and ask; do not remove the name from the page.
- Task 6's code is written before its dated design has been reviewed, or Task 5b's steps 4 and 5 are done without the evidence from step 3. The roadmap authorises the work; it does not lift either gate.
- Any docs page says macOS runners exist before a job has run on a real Mac.
- A statement about Codemany cannot be traced to a page read on the date given. Remove the statement; do not soften it into a guess.

## Review Focus

- Every claim about Codemany carries the page it came from, in this plan or on the comparison page, and the date.
- The comparison page says plainly where Codemany is better: macOS, an app instead of a controller, a virtual machine per job, and mirrors without building them.
- Nothing says Zoomies supports macOS runners, is faster than anything, or is safer than a sandbox.
