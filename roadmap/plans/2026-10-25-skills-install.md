# Skills: one published install command, tried before it is claimed

**Status:** proposed. Task 1 is blocked: the sandbox this was planned in refused to run the installer, so the run is the owner's, or needs the permission given. The findings below are from reading, not from a run.

## Goal

A person who wants the Zoomies skills in their coding agent runs one command that
is written down, and that command has been run. Today the README says to copy a
folder by hand. The command `npx skills add eyupio/zoomies` exists only in
[the ZF-233 plan](2026-10-09-zf-233-skills.md), and no document a reader sees names
it.

## Why

The other CI services an agent might be pointed at publish a one-line install for
their skills, and a skill that takes three manual steps is a skill that is not
installed. The cost here is small: the skills already exist, are held by tests, and
sit in `skills/<name>/SKILL.md`, which is the layout the installer looks for. What
is missing is the claim, and the evidence for it.

## What must be true before anything is written

The command has to be run, not assumed. The plan's first task is to run it against
this repository from a clean directory with a throwaway home directory, and record
three things: that it finds both skills, where the files land for each agent it
detects, and whether it asks for any frontmatter the skills do not carry. If the
installer needs a different layout or a field, the skills change first, and the
docs follow. If the build environment cannot reach the network, that run is the
owner's, and the docs wait for it. A published command nobody has run is the one
failure this plan exists to avoid.

## What reading found, 25 October

The installer's own documentation says it searches `skills/` and a list of agent
folders, `.claude/skills/` among them, walks each up to three levels deep, and
requires `name` and `description` in a skill's frontmatter. Three consequences, none
of them tried:

* **The command must name the skills.** This repository also tracks
  `.claude/skills/babysit` and `.claude/skills/steward`, which are maintenance
  skills for people working on Zoomies, not for users of it. A bare `skills add
  eyupio/zoomies` would find four skills and offer them all. The command to publish
  is `npx skills add eyupio/zoomies --skill zoomies --skill zoomies-kennel`.
* **The provenance test accepts that text.** A throwaway line with the command in
  `README.md` passed `go test ./internal/provenance/`, so naming the installer is
  not blocked by it.
* **Task 3's second test already exists.** `TestEverySkillNamesItselfAndSaysWhenToUseIt`
  holds that every folder under `skills/` has a `SKILL.md`, that `name` is the
  folder's name and that the description says when to use the skill. Only the first
  test in Task 3, the docs naming the command and both skills, is new.

## Owner inputs

* **Name the installer in the docs?** It is a third-party tool, and no page names
  it as an installer today. The recommendation is yes, with the copy-by-hand route
  kept beside it so nobody depends on the tool. The provenance test decides whether
  the name is allowed at all; if it is not, this stops here and the owner is told.
* **A second agent for the Kennel skill.** ZF-233's gate asks for the offline
  skill to be tried in two agents, and one run is recorded. That second run is
  still the owner's.

## Tasks

### Task 1: run the command and record it

Files: none changed. The result goes in the pull request's description: the exact
command, the directory it ran in, the files it wrote and the agents it found.

* Run it with `HOME` pointing at an empty directory and the working directory empty,
  naming both skills as above, and once with `--list` to see everything it finds.
* Run `zoomies kennel check` from one installed copy against a checkout, and `zoomies
  status` from the other against `zoomies demo`, to show the installed files work
  where the installer put them.
* If it fails or wants more, stop and report; the later tasks do not start.

### Task 2: say it in the places a reader looks

Files: `README.md` (the skills paragraph at about line 459), `docs/cli.md` (the
`The zoomies skill` and `The zoomies-kennel skill` sections), `docs/ai-context.md`
(the sentence that already names the skill), `skills/zoomies/SKILL.md` (the row that
says a skill for auditing workflows "is a separate install" now says which command
installs both).

* The command comes first and the copy-by-hand route second, in the docs' own
  voice, with the directory each agent reads.
* The command is stated once per page and linked from the others.

### Task 3: hold it with a test

File: `internal/docs/skills_test.go`.

* `TestTheInstallDocsNameTheCommandAndBothSkills`: `README.md` and `docs/cli.md`
  each contain the command, `skills/zoomies` and `skills/zoomies-kennel`.
* `TestEverySkillDirectoryHasASkillFileNamedForIt`: for every directory under
  `skills/`, `SKILL.md` exists and its `name:` equals the directory name, which is
  what the installer relies on to find and label it. Add a skill later and a typo
  in either is caught before a person meets it.

### Task 4: the record

Files: `roadmap/progress.md` (the ZF-233 row's **Open** sentence), `ROADMAP.md` (its
Done paragraph), and this plan's status.

## Verification

`go test -count=1 ./internal/docs/ ./internal/provenance/`, `python3 -m mkdocs build
--strict`, and the Task 1 run, recorded.

## Out of scope

Publishing to a skills directory or registry, versioning the skills separately from
the binary, and any new skill (the next two plans add one).
