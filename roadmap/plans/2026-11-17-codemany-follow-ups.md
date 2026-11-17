# Codemany follow-ups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn a review of Codemany's public documentation into the small set of changes it justifies: the comparison page (done), the pointers that send macOS readers to it, a design note and a competitor-list entry that need no decision, and three pieces of work that wait for the owner.

**Architecture:** Documentation and planning changes only until the owner decides. Nothing in this plan is authorised delivery work: it is an analysis and a handoff, in the sense of [ROADMAP.md](../../ROADMAP.md) and [roadmap/README.md](../README.md), and Tasks 5 to 7, each marked as gated on a decision, do not start until ROADMAP.md says they may.

**Tech Stack:** Markdown, MkDocs (`mkdocs build --strict`), Go tests in `internal/docs`.

**Written:** 17 November 2026 against `3b86503`. Method as in [competitive-review-2026-09.md](../competitive-review-2026-09.md): public documentation compared with this repository's code and record, and "not verified" is never a claim that something is absent.

## Where this stands

**Done, in the pull request that added this file:**

- [x] `docs/alternatives/codemany.md`, the comparison page, wired into `mkdocs.yml` and `docs/alternatives/index.md` (both tables, the macOS question under "How to choose", and the page description).
- [x] Task 2, the macOS pointers, in `docs/hosted-runner-services.md`, `docs/should-i-self-host.md` and `docs/home-lab.md`. `docs/faq.md` was read and left alone: its answer links onward only for Windows qualification and the image catalogue.

**Next unfinished step:** Tasks 3 and 4, which need no decision, then the owner's three decisions. Update this section, and tick the boxes below, in every pull request that works from this plan.

## Global Constraints

- British spelling, no em dashes, no spaced `--`, comments say why, commit messages are sentence-case imperative sentences with no prefix.
- A comparison says where the other product is better, plainly, and is dated. It is a comparison of documentation, not a benchmark.
- Do not publish a claim of faster execution, stronger isolation or unique capability without evidence ([competitive-review-2026-09.md](../competitive-review-2026-09.md), "Positioning and evidence").
- Do not claim macOS is supported. The support matrix says "Nothing planned" for macOS runners and a test holds the docs to it.
- Nothing here edits `ROADMAP.md`: new or changed delivery work belongs there first, and only the owner puts it there.

## What the review found

Codemany is a proprietary, paid desktop app that runs GitHub Actions jobs on Apple silicon Macs and systemd Linux machines the customer owns. It costs $20 a month for two machines and $10 for each after that, with a 14-day trial. Its pitch is one changed `runs-on` line, a menu bar app and a public benchmark. The sources were its home, pricing, features, docs (get started, machines, advanced, security, FAQ, compare), changelog and benchmark pages, read on 17 November 2026; the page records the same date.

The two overlap less than the shared goal suggests. Codemany is an app for a few owned machines, strong on macOS. Zoomies is a controller for a fleet, Linux only. Seen from Zoomies, five things came out of it, and three were already on the record before this review:

| Finding | Already tracked? | Where it goes |
| --- | --- | --- |
| macOS runners are Codemany's main draw, and Zoomies has none | Not planned: [support-and-measurement.md](../support-and-measurement.md) says "Nothing planned" | Decision 1, Task 5 |
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

## Decisions for the owner

Each has a recommendation. None is taken by writing it here.

1. **macOS runners.** (a) Out of scope for now; keep the hybrid advice and point to Codemany, runscaler and the hosted services. (b) A one-day spike on an Apple silicon host, in the shape of decision 28's Proxmox spike: can an agent start a macOS virtual machine, register a just-in-time runner, run one job and destroy it. (c) Commit to a backend. **Recommended: (a).** Apple limits a Mac to two concurrent macOS virtual machines, so capacity grows by buying Macs, which suits a per-machine desktop app better than a fleet scheduler; and the project's own rule is to claim only what a test has run on. Revisit (b) when someone with Apple silicon asks for it.
2. **Cache follow-ups.** (a) Authorise ZF-246, the per-host registry mirror, as proposed. (b) Also add Go, npm and git mirrors. **Recommended: (a) only.** ZF-212's recipes already keep Go and npm downloads between runners, and a package mirror would add a second path to the same saving. Measure before adding one.
3. **A performance story.** (a) None. (b) Publish Zoomies' own measures, queue wait and runner startup, which the job history already records, with the method. (c) A like-for-like comparison with GitHub-hosted runners. **Recommended: (b).** A comparison with a shared 2-core virtual machine measures the hardware you own more than the controller, which is the criticism this plan's page makes of Codemany's; if (c) is wanted anyway, Task 7 gives the protocol.

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

No decision needed. [candidates-from-hosted-services.md](../candidates-from-hosted-services.md) section 4 weighs a micro-virtual-machine backend against a provider used per job. Codemany adds evidence and a third shape.

**Files:** modify `roadmap/candidates-from-hosted-services.md`, under its "Checked again" convention: a dated note, not a rewrite of the section.

- [ ] **Step 1:** Add a note dated 17 November 2026 saying that a commercial product in this market ships a virtual machine per job (KVM on Linux) and a container or virtual machine per job through LXD, both opt-in, and reports that they are slower and start with cold caches. Cite its [advanced documentation](https://codemany.com/docs/advanced#isolation).
- [ ] **Step 2:** Name the third shape: an LXD container or LXD virtual machine per job as a backend that needs LXD on the host and no Firecracker or Kata. Say it is a shape to weigh when the work is picked up, not a proposal.
- [ ] **Step 3:** Leave the status line, "demand-gated", exactly as it is.
- [ ] **Step 4:** `go test -count=1 ./internal/docs/`. Expected: pass.

### Task 4: Watch Codemany in the weekly search check

No decision needed. `weekly-seo/config.yml` lists the domains compared in the AI citation check. It names Cirun and four hosted services and not Codemany, which publishes Markdown versions of its pages and a `llms.txt` for agents and so is likely to be cited.

**Files:** modify `weekly-seo/config.yml`.

- [ ] **Step 1:** Add `codemany.com` to `competitors`. Do not touch `index_sample`: its comment says to change that list only on purpose.
- [ ] **Step 2:** Leave the new page out of `index_sample`. That list is held steady week to week so "N of M indexed" can be compared; the page joins it only when the owner wants it counted, with a line in the change log.
- [ ] **Step 3:** `go test -count=1 ./internal/docs/`. Expected: pass.

### Task 5 (gated on decision 1): record it

**Files:** create `roadmap/decisions/0020-<what-was-decided>.md` and add its line to `roadmap/decisions/README.md`. The number is the next free one at the time; check the directory.

- [ ] **Step 1:** Write the record with the five parts the README requires, in the owner's words if they gave them. If the owner chose (a), the Consequences say that `docs/alternatives/index.md` and `docs/hosted-runner-services.md` keep "Zoomies has no macOS runner image", and that nothing in `internal/backend` changes.
- [ ] **Step 2:** If the owner chose (b), write the spike as `roadmap/plans/<date>-macos-vm-spike.md` in the shape of decision 28's, one day, with a stop condition that nothing is merged into `internal/backend`. Do not start it before an Apple silicon host is available.
- [ ] **Step 3:** Do not edit `ROADMAP.md`. Say in the pull request that the owner adds the line.

### Task 6 (gated on decision 2): the registry mirror

Only once ROADMAP.md lists ZF-246 as authorised. The design is already sketched in [candidates-from-hosted-services.md](../candidates-from-hosted-services.md) section 1, layer 2: a read-only, per-host pull-through mirror that the agent owns, with `agent.registry_mirror.enabled` and `agent.registry_mirror.max_bytes` and their `ZOOMIES_AGENT_REGISTRY_MIRROR_*` overrides. Codemany's version is a useful bound on scope: it holds public Docker Hub images only, refuses pushes, never caches private images and falls back to a direct pull when it is down. Take those four as requirements, and write the failure rule the cache service already has: a broken mirror slows a pull and never fails a job.

- [ ] **Step 1:** Read [cache-service.md](../cache-service.md) and `internal/cacheservice`. The mirror is not part of that package, which is stdlib only and knows nothing about the fleet.
- [ ] **Step 2:** Write a dated design under `roadmap/plans/` before any code, and stop for review.
- [ ] **Step 3:** Each setting arrives with a `docs/configuration.md` row and its override; each finding code with a `docs/problem-codes.md` row; `make generate` is run, not hand-edited.

### Task 7 (gated on decision 3): the performance evidence

If the owner chose (b), the page reports what the fleet already measures. If they chose (c), use this protocol, which is Codemany's good idea (an open repository, every number linked to its run) with its confound removed.

- [ ] **Step 1:** Fix the hardware first. Compare GitHub's standard 2-core runner with a 2-core virtual machine running a Zoomies container runner, then separately with the best machine you own, and say which is which on the page. The first comparison is about the controller; the second is about owning a fast machine.
- [ ] **Step 2:** Pin each project to one commit. Start both sides in one run. Count a timing only when both sides finished the build in it, and report the number that failed or was stopped on each side, never leaving them out.
- [ ] **Step 3:** Time cold and warm builds separately, report the median over a stated window, and publish the raw data beside the page.
- [ ] **Step 4:** Do not print a single headline multiplier. The existing `make bench` rule applies to anything that changes code for speed: run before and after, once per package, and put the `benchstat` comparison in the commit message.
- [ ] **Step 5:** Put the evidence in `roadmap/validation/`, not in prose.

### Task 8: Keep the page honest

- [ ] **Step 1:** Re-read Codemany's pricing, docs and changelog three months after the review date, or sooner if the owner hears it has added a Windows, GPU or hosted-controller option. Update the page's "reviewed in" month and every figure on it, or change the sentence that went stale.
- [ ] **Step 2:** If the page and Codemany's own compare page disagree about what Codemany does, the page follows Codemany's documentation and says when it was read.

## STOP conditions

- A test in `internal/docs` fails on the new page for a reason this plan does not mention: report it, do not loosen the test.
- `internal/provenance`'s list, which the owner supplies and which is not in the tree, turns out to include Codemany. The comparison pages name their subjects by design, so report it and ask; do not remove the name from the page.
- Task 5, 6 or 7 is started without the owner's decision, and for Task 6 without a line in ROADMAP.md that authorises it.
- A statement about Codemany cannot be traced to a page read on the date given. Remove the statement; do not soften it into a guess.

## Review Focus

- Every claim about Codemany carries the page it came from, in this plan or on the comparison page, and the date.
- The comparison page says plainly where Codemany is better: macOS, an app instead of a controller, a virtual machine per job, and mirrors without building them.
- Nothing says Zoomies supports macOS runners, is faster than anything, or is safer than a sandbox.
