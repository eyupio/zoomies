# E5: recurring failure evidence

**Status:** accepted by the owner on 25 October as the first useful slice of E5.
Built: the pure analysis, the store read, the controller, the API route, the CLI
command, the MCP tool, Eli's allowlist and the Failed view's card.

## Goal

An operator, an agent or Eli can ask which jobs keep failing in a period and be
told what the failures have in common, from outcomes the fleet already keeps and
nothing else: how often, on how many branches, since when, at which step, how
many recorded a runner fault, how many passed when the same revision ran again,
and whether the job has passed since.

## Owner decisions this follows

* **Evidence, not diagnosis.** The analysis reads job outcomes. It names the
  step a failure stopped at and never why; it reads no log, runs nothing and
  calls no model. Where the outcomes cannot separate the causes it says so, as
  `intermittent` with a medium confidence and its reason.
* **It is the fifth analysis, not a new mechanism.** It uses the shared finding
  shape (`analysis.Finding`), the `/analysis` routes, the MCP renderer and the
  CLI reader, so the page, the CLI, the tool and Eli cannot disagree.
* **Names stay out of the sentence.** Repository, workflow, job and step names
  are written by whoever controls a workflow. The summary, the dominant value
  and the next steps point at the latest failed job by id; the names live in the
  evidence, which MCP readers quote in the untrusted block.
* **Read-only.** Nothing is written, proposed or published.

## How a failure is classed

Tried in this order, for a job with three or more failures in the period:

1. `infrastructure`: at least half of its failures recorded a runner fault.
2. `flaky`: at least half passed when a later attempt of the same revision ran.
3. `persistent`: it has not passed since its latest failure.
4. `intermittent`: none of those.

## What is not done

* No log is read, so a step's output is never summarised.
* No failure is grouped by message or by test, because the outcomes carry
  neither.
* Cancelled and skipped jobs are left out: they stop when somebody says so.
* A period with more than 5000 finished jobs reads the newest and says so.
