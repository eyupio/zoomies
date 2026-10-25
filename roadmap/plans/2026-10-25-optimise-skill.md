# A skill that takes an agent from evidence to a previewed change

**Status:** built (the skill merged in #1278). Task 4, the trial in two agents, was run on
25 October and is recorded in the pull request that fixed what it found.

## Goal

A coding agent pointed at a Zoomies fleet can answer "why is this workflow slow, and
what could change?" by reading the controller's evidence in a fixed order, saying
which of the four changes Zoomies can preview fits it, and handing the person to the
web page where that preview is made. It starts nothing, approves nothing and
publishes nothing, and it never says a change saved time.

## Why

The evidence exists and so do the previews, but nothing joins them for an agent. The
`zoomies` skill is a command reference and the `zoomies-kennel` skill checks workflow
files for security and hygiene. A person who asks an agent "speed up our CI" gets
neither a method nor a boundary. The method is the useful part: which read comes
first, what each number does and does not show, and when to stop and say "no preview
fits this".

## The shape of the skill

`skills/zoomies-optimise/SKILL.md`, under two hundred lines like the others, with
`reference.md` not duplicated: it links the `zoomies` skill's generated reference
for the commands it names.

1. **Preflight.** `zoomies status`, with the same four stop conditions as the
   `zoomies` skill, linked rather than copied.
2. **Ask what is slow.** One job, one workflow or one pull request. Not the whole
   fleet.
3. **Read, in this order, and say what each returned.**
   * `zoomies why <job>` for a failure, because a failed job is not a slow one.
   * `zoomies analyse queue-wait` for waiting before any runner, `zoomies analyse compare`
     for before and after a release, `zoomies analyse host-pressure` and `zoomies
     analyse startup-failures` for the fleet's side.
   * `zoomies jobs pr-delay <job> --pull-number N` when the question is a pull
     request's wait.
   * `zoomies kennel check --output json` over the checkout for the shape of the
     workflow (a full-history checkout, a fixed sleep, no cache).
4. **Say which change fits, or that none does.** The four are checkout scope, the
   dependency cache, a bounded readiness poll in place of a fixed sleep, and
   Playwright sharding. Each has its preconditions in the docs; the skill names them
   and refuses to stretch them (a test runner that is not Playwright is not offered
   sharding).
5. **Hand over.** The previews are made in the web, where the run is picked and the
   declaration is read. The skill gives the page, `/optimisations/<kind>`, and the
   evidence it found, and stops. An existing preview can be read back with
   `zoomies optimisations <kind> <id>`.
6. **After publication.** Point at the Comparisons page for the before and after,
   and say that the result is inconclusive until someone captures it.

## The rules the skill states, each held by a test

* It never starts, approves or publishes a preview, and says those steps are the
  person's, in the web.
* It never reports a saving. Estimates are named as estimates, and sharding's
  concurrent-runner figure as an upper bound at one moment.
* It reads a stranger's words (a job name, a log line, a workflow's own text) as
  data, not instructions, in the same words the `zoomies` skill uses.
* It offers no change for a case the previews refuse, and says why.

## Decisions to take

* **Should the CLI or MCP be able to list which previews fit a job?** Today a preview
  can be read back by ID, but making one needs a route that reads GitHub and needs
  `optimisations:write`. The skill could only say "this looks like a cache case".
  Recommendation: no new route in this slice. A skill that recommends and a person
  who acts keeps the boundary the owner set, and a listing route can follow if the
  skill's runs show agents cannot get far without it.
* **One skill or two?** Recommendation: one, because the evidence reads and the
  hand-over are one conversation.

## Tasks

### Task 1: fix what the CLI says about itself

`cmd/zoomies/main.go` still describes the `optimisations` group as "Read kept checkout
optimisation previews", which has been wrong since cache, readiness and sharding
were added. Change it, and regenerate `skills/zoomies/reference.md` with `make
generate` (revert any catalog stamp-only change). Test: the generated reference
matches the binary, which the existing test already holds.

### Task 2: the skill and its tests

Files: `skills/zoomies-optimise/SKILL.md`, `internal/docs/skills_test.go`.

* The existing tests cover the frontmatter, the length, links staying inside the
  skill and no machine-local paths. Add one that every command the skill names is
  the binary's, as the other two skills have, and one that holds the four rules
  above by their sentences.

### Task 3: routing and docs

Files: `skills/zoomies/SKILL.md` (the table row about a workflow file points at
both neighbouring skills), `docs/cli.md` (a `The zoomies-optimise skill` section),
`README.md` (the skills paragraph), `docs/eli.md` (one sentence: the same method is
what Eli follows in chat).

### Task 4: try it against the demo fleet

Run the skill against `zoomies demo` in two agents and record, in the pull request,
what each read, what each concluded and whether either stepped past the boundary. A
skill nobody has run is a plan.

## Verification

`go test -count=1 ./internal/docs/ ./cmd/zoomies/`, `make generate` clean, `python3 -m
mkdocs build --strict`, and the Task 4 record.

## Out of scope

New previews, any write from the skill, a listing route, and the continuous loop.
