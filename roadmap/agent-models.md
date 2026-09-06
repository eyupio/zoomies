# Which Claude model drives which stage

The roadmap is implemented by Claude Code sessions, and the model behind a
session is a choice with a cost and a failure mode of its own. This page says
which model and which effort setting to use for each kind of work in the
programme, and why. It is guidance to be measured against, not a rule to be
obeyed: every entry in [progress.md](progress.md) records the model and effort
that produced it, so the choice can be revisited on evidence rather than on
price per token.

The rule underneath all of it: **choose by what the stage risks, not by what a
token costs.** The measure is cost per merged, green pull request whose
behaviour survived review, and a cheap session that leaves a wrong invariant
behind, with tests that enshrine it, is the most expensive session in the
programme.

## The models

Prices are Anthropic's first-party API rates per million tokens, as cached on
24 June 2026 by the reference this page was written from. Check the Models API
before a large run; the point of the table is the ratios, which move less than
the numbers.

| Model | ID | Input | Output | Context | What it is for here |
| --- | --- | --- | --- | --- | --- |
| Claude Fable 5.1 | `claude-fable-5-1` | $10 | $50 | 1M | Design, invariants, verification, review. The most capable widely released model; thinking is always on and controlled by effort. |
| Claude Opus 5 | `claude-opus-5` | $5 | $25 | 1M | The bulk of implementation on a settled design. The recommended default for agentic coding. |
| Claude Sonnet 5 | `claude-sonnet-5` | $2 | $10 | 1M | Bounded, checkable slices with a specification in hand. |
| Claude Haiku 4.5 | `claude-haiku-4-5` | $1 | $5 | 200K | Sub-agent reading with a checkable output. Not for code changes in this repository. |

Three properties of Claude Fable 5.1 matter operationally. Its turns are
longer: a single request on a hard task can run many minutes while it gathers
context, builds and verifies, which is what you want for ZF-102 and not what
you want for a documentation row. It carries safety classifiers that can end
a request with a refusal, which matters for the hostile-input tests in ZF-104
(see below). And it is only available to organisations on 30-day data
retention, so a workspace on zero data retention cannot run it at all.

## The tiers

### Claude Fable 5.1: where a wrong answer is expensive

Use it where the deliverable is an invariant, a boundary, or a judgement about
whether something is proven, and where the design space is still open. Its
measured gains over the tier below are on exactly this work: long-horizon
autonomous runs, first-shot implementations of well-specified systems, code
review and debugging, and searching a repository's history for how something
came to be.

| Work | Why this tier |
| --- | --- |
| ZF-001 and ZF-002, the reconciliation and the measurement contract | The rest of the programme is only as sound as the classification of what exists. This document was produced by a Claude Fable 5.1 session orchestrating fresh-context verifier sub-agents, and that shape is the recommendation for every later gate review too. |
| ZF-101, one eligibility policy across ingest, scheduler and explanations | A missed call site is a cross-target allocation. The work is reading every path a job takes, not writing much code. |
| ZF-102, convergent reconciliation and fencing | Concurrency and restart semantics; the failure mode is a race that a test written by the same session will not think to exercise. Run at `xhigh`. |
| ZF-103, the reservation model | The design decision (what is reserved, when it is released, how an old pool migrates) is the whole package; the code that follows is Opus-tier work. |
| ZF-104, the access and secret matrix | Building the matrix from the router and proving each row negative is review work. If a session writing hostile-input or escape-sequence tests hits a safeguard refusal, move that slice to Claude Opus 5 rather than rephrasing around it. |
| ZF-302, the design of each drill and the reading of its result | Deciding what a drill proves, and whether a green result is honest, is judgement. |
| Every decision record in [decisions/](decisions/) and every gate review | A gate is a claim about evidence. Give it the model that is best at finding what is missing. |
| Review of every pull request in the programme, whichever model wrote it | Its precision and recall on real bugs is the reason to pay for it here. A second, fresh-context session reviewing is worth more than the writing session reviewing itself. |

Effort: start at `high`, which is the default. Use `xhigh` for ZF-101,
ZF-102 and ZF-103 design, where the reasoning is the deliverable. Do not run a
long document or a whole-file rewrite at `xhigh` or `max`: the model drafts
it in its thinking and again in its reply, which doubles the tokens for no
gain. Give it the whole task specification up front, tell it where the memory
surface is (the progress record and the decisions directory), and require it
to audit every progress claim against a tool result before reporting.

### Claude Opus 5: implementation on a settled design

The recommended starting point for agentic coding. On a coding benchmark it
matched Claude Fable 5 within noise at about 60% of the cost, and it completes
tasks rather than leaving stubs. Use it once the invariant is written down and
the work is to make the code and its tests match it.

| Work | Effort |
| --- | --- |
| ZF-103 implementation after the reservation decision record is accepted | `high` |
| ZF-105, cleanup, retention and bounded retry | `xhigh`: retry and cleanup interact with the reconciliation invariants from ZF-102 |
| ZF-201, the first-job journey in Svelte and its Playwright coverage | `high` |
| ZF-202, diagnostics and the support bundle | `high` |
| ZF-203, the backup and restore commands and recovery mode | `high`; the fenced-recovery design itself is a decision record (Fable tier) |
| ZF-204, compatibility policy, upgrade and rollback tooling | `high` |
| ZF-205, telemetry, retention bounds, pagination, the load fixture | `xhigh` for the reconnect and resync path, `high` for the rest |
| ZF-301, extending the end-to-end harness | `high` |
| ZF-302, implementing the drills a Fable session designed | `high` |

On long-horizon coding this model gave up about two points of pass rate at
`medium` for half the cost, and about eight points at `low` for a quarter of
it. That is a real trade, so `medium` is for a slice whose tests are the
checker, not for one whose correctness is judged by reading. Fast mode is an
option for an interactive loop with an operator watching; it changes speed, not
the model.

### Claude Sonnet 5: bounded and checkable

Use it where the specification is complete, the output is mechanically
checkable, and the surrounding code already shows the pattern.

* Rows in `docs/problem-codes.md`, `docs/configuration.md` and `docs/cli.md`
  for a code, key or command another session added, with the `internal/docs`
  tests as the checker.
* The OpenAPI document and the two generated clients for an endpoint whose
  shape is already decided; CI diffs them.
* Table-driven tests for a listed set of scenarios, sized like their neighbours.
* Playwright specifications for a journey that is written down step by step.
* A migration that adds a column, with the next unused prefix.
* Lint, format and generated-file fix-ups on a red pull request.
* Bookkeeping in the progress record.

Effort: `high`, and `xhigh` for anything that has to reason about more than
one file. It respects effort strictly at the low end and will under-think a
moderately complex task at `low`, so do not use `low` on this repository for
anything beyond a lookup. Its tokenizer uses about 30% more tokens than the
Opus tier for the same text, so compare cost per task, not per token.

### Claude Haiku 4.5: reading, never writing

Not for code changes here. As a sub-agent it is right for bulk reading with a
checkable output: summarising a CI log, listing the files that match a
criterion, extracting evidence links for the progress record. It answered
knowledge questions at about a tenth of Claude Opus 5's cost per question and
about two-thirds of its accuracy, which is the shape of a task where the
orchestrator checks the answer, not one where the answer is the deliverable.
It has a 200K context and the older `budget_tokens` thinking control rather
than an effort setting.

## Effort before model

Effort is the first lever to reach for; the model tier is the last. Two
patterns from Anthropic's measurements apply directly:

* **Re-run failures at higher effort.** Where a slice has a failure signal (the
  Go tests, `svelte-check`, Playwright), run it at a lower effort and re-run the
  failures at the default. In the coding runs measured, running everything at
  `low` and re-running failures at the default passed about 93% of tasks for
  about half the cost of running everything at the default, which passed about
  92%. Starting at `medium` passed about 94%. Use this for the saving on Sonnet
  and Opus slices, and price in the doubled wall clock on the failures.
* **Sweep effort on the current model before dropping a tier.** Lower effort
  on Claude Fable 5.1 often exceeds the top effort of the previous generation,
  and at `low` it is frequently competitive with the tiers below on cost per
  task while performing better. So the alternative posture to the tiers above
  is one model with effort as the only lever. It is simpler to run and to
  reason about; it costs more on the mechanical slices; and it removes the
  question of which session wrote what. That choice is recorded as a decision
  for the owner in [ROADMAP.md](../ROADMAP.md).

## Orchestration

The review that produced this roadmap ran as a Claude Fable 5.1 orchestrator
with one sub-agent per work package mapping it to the code, one adversarial
verifier per mapping trying to refute it, and a final critic asking what the
whole set had missed. That shape is right for every gate review and for the
larger reviews in ZF-104 and ZF-302: fresh-context verifiers outperform a
session critiquing its own work, and independent packages are exactly the bulk
an orchestrator is for.

It is the wrong shape for one dependent chain. A package that is one sequence
of edits, each depending on the last, is cheaper and better in a single
session at lower effort than as a plan, a handoff and a merge. Reading-heavy
fan-out (many files to read, one judgement to make) can go to Claude Sonnet 5
sub-agents; the judgement stays with the orchestrator.

## What to write in the progress record

For every package entry: the model, the effort, whether the work ran as one
session or an orchestration, and the number of review rounds before merge.
Once a phase is complete, that column answers the only question that matters
about this page, which is whether the tiering paid for itself.
