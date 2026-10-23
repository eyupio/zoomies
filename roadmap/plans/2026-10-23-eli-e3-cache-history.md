# E3 cache preview: repeated setup from fleet history

Date: 23 October 2026. Package: ZF-244. Follows the pure cache preview
foundation.

## Outcome

The fleet's own retained job history supplies the repeated setup cost a cache
preview needs. For one repository, workflow name, job name and literal setup
step name, it returns the newest successful GitHub-reported step timings with
their exact local job identities. A pure helper turns those samples into the
preview's evidence.

## Decisions

* **No extra GitHub reads.** The `jobs` table already keeps GitHub's step names
  and timings for jobs this fleet ran. The query is scoped to one repository,
  workflow, job name and exact step name; a 30-day window and a bounded limit
  keep it cheap.
* **Exact, not guessed.** A job with two steps of that name is ambiguous and left
  out. A step without both timestamps or a positive duration is left out. The
  samples carry the local job ID, GitHub run and attempt, head, step number and
  the GitHub-reported start and end.
* **Two to one hundred samples.** The pure helper sorts them by job ID, refuses
  duplicates and malformed values, and computes the mean setup milliseconds and a
  deterministic evidence ID from the exact identities and timings. The same set
  gives the same evidence whoever queries it and in whatever order.
* **The window is caller-chosen.** The store takes `since` and a limit; the
  controller that uses it will choose 30 days and at most the preview's bound.
  This slice adds no hidden background scan.

## Not in this slice

Reading the workflow and root lockfile from GitHub, finding the setup step in the
source, matching the selected job's literal names, retention, routes, CLI, MCP,
Eli or UI. The next slice binds this evidence to the selected run and source.

## Verification

Store tests cover success/failure filtering, the exact step name, ambiguous and
untimed steps, window and limit, order and exact timing. Optimisation tests cover
the mean, deterministic evidence ID independent of order, and refusals for one
sample, duplicates, malformed run or job identity, zero time, bad head/digests
and more than one hundred samples. No live GitHub, model or fleet.
