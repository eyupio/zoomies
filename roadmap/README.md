# The roadmap folder

[ROADMAP.md](../ROADMAP.md) at the repository root says what work is authorised
and in what order. Everything in this folder supports it and none of it can add,
reorder or authorise work. This page says which document answers which question,
so that nobody has to guess which of fifty files is the current one.

| Question | Read |
| --- | --- |
| What is authorised, and in what order? | [ROADMAP.md](../ROADMAP.md) |
| Where has a package got to? | [progress.md](progress.md) |
| Why was a decision taken? | [decisions/](decisions/) |
| What was run, on what, and what did it show? | [validation/](validation/) |
| How was a package going to be built? | [plans/](plans/) |
| What did a review find, on the day it was written? | the reviews below |
| What was set aside, and why is it still here? | [archive/](archive/) |

When two documents disagree about delivery state, `progress.md` wins for status
and `ROADMAP.md` wins for order. A dated plan or review is what was thought on
the day it was written: its checkboxes are not kept up as the work lands, and
the row in `progress.md` is the record of what shipped.

## What is in here

* [progress.md](progress.md): the work-package record, one row per package, kept
  current in the same pull request as the work.
* [decisions/](decisions/): one numbered record per decision, never renumbered.
  Its [README](decisions/README.md) is the index.
* [validation/](validation/): evidence, written by tests and by hand, one file
  per gate or measured baseline. Some of it is a build product: the drill job
  in `ci.yml` appends to `drills.md`, and `make load` and `make test-e2e` write
  here, so a file moved out of this folder is quietly recreated.
* [plans/](plans/): dated implementation plans, one per pull-request series.
* [source/](source/): the owner's original follow-on roadmap, kept as the input
  the active roadmap was derived from. It explains why work existed.
* [archive/](archive/): documents whose job is done but whose reasoning is worth
  keeping: the theme plan, the first UI audit and the update-engine handoff.
* [agent-models.md](agent-models.md): which Claude model and effort to use for
  which kind of stage, and how to judge that choice on evidence.
* [agent-readiness.md](agent-readiness.md): the refined plan for making a
  repository legible to a coding agent.
* [audit-2026-10-16.md](audit-2026-10-16.md): the repository audit of 15 and 16
  October 2026: what was found, what was changed and merged in #1088, and what
  was left open.
* [ci-efficiency-2026-10-27.md](ci-efficiency-2026-10-27.md): what a `ci.yml`
  run costs, measured from 25 runs on 26 and 27 October 2026, the three changes
  worth making first, and what each is expected to save.
* [candidates-from-hosted-services.md](candidates-from-hosted-services.md) and
  [competitive-review-2026-09.md](competitive-review-2026-09.md): what hosted
  runner services ship, compared with this repository.
* [eli-review.md](eli-review.md): what Eli was, what competitors ship, what to
  do next.
* [fleet-analysis.md](fleet-analysis.md): the design for the fleet window and
  the analyses over MCP.
* [in-product-updates.md](in-product-updates.md),
  [in-product-updates-plan.md](in-product-updates-plan.md) and
  [in-product-updates-manual-check.md](in-product-updates-manual-check.md): the
  design, plan and hands-on checklist for updating from the web UI (ZF-232,
  delivered).
* [instance-transfer.md](instance-transfer.md): moving retained data between
  controllers (ZF-237).
* [directory-submissions/](directory-submissions/): the tracker of which directories
  Zoomies was listed in, and the draft positioning copy for them. Nothing in the
  code reads it; it is the record, kept with the other non-code records.
* [kennel-club.md](kennel-club.md): repository standards for the fleet (ZF-229).
* [rc1-triage.md](rc1-triage.md): the known findings before the first release
  candidate.
* [runner-working-room.md](runner-working-room.md): the design for charging
  every runner working room on disk, and for a provider learning the disk its
  machines have, so a job is never placed where it cannot fit.
* [support-and-measurement.md](support-and-measurement.md): the support matrix
  and the moments the platform must timestamp.
* [tailcat-private-hosts.md](tailcat-private-hosts.md): private runner hosts
  through Tailcat.

## Outside this folder

[eli-enhancement-roadmap-handoff.md](../eli-enhancement-roadmap-handoff.md) is
the live handoff for the Eli work. It stays at the repository root because the
agents working from it, and `progress.md`, link to it there.

`internal/docs` holds this folder to its index: a file added here without a line
above fails `TestEveryRoadmapFileIsIndexed`.
