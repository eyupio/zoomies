# A Kennel score, with the formula published

**Status:** proposed. The weights, the caps and the roll-up below are the owner's to
accept, and nothing is built until they are.

## Goal

Say in one number how a repository's workflows measure up against what Kennel Club
checks, and publish the formula that makes the number, so a person can recompute it
from the findings by hand. Today the standing is a word (`best_in_show`,
`attention`, `partial`, `pending`) and four counts. A word cannot be compared
between two repositories or two weeks, and a number whose rule is not published is
a number nobody can argue with.

## What the number is, and is not

It is a measure of configuration: the open findings Kennel's checks produce. It is
not a measure of how fast CI is, how often it fails or how much it costs, and the
page that shows it says so. It is computed from what Kennel already stores, at read
time, by a pure function, so there is no migration and a change to the formula
changes every number at once and says so through a version.

## The formula, as proposed (version 1)

Start at 100. Take away, for each open finding that is not waived:

| Severity | Taken away |
| --- | --- |
| error | 20 |
| warning | 6 |
| info | 1 |

Two findings with the same check code and the same subject count once. The result is
floored at 0, then capped by the worst severity open:

* any open **error**: at most **59**
* any open **warning** (and no error): at most **89**

Why the caps: without them, one exposed repository with a lone error and nothing
else wrong scores 80, which reads like a good grade. A security finding is not
offset by a tidy README. The existing rule already says an error is never hidden
behind "we could not read everything"; the cap is the same rule in numbers.

**What the score is of.** It is computed over the checks that ran. It carries
`checked` and `unread`: how many checks ran and how many could not be run for want
of a permission or a read. When any could not, the score is labelled "of the checks
that could be read", and the Overview never shows it as a total. This follows the
page's existing wording, "these counts are a minimum".

**Waived findings** take nothing away and are shown beside the number, exactly as
they are beside the badge today, so a waiver cannot make a score look better
without the page saying how many there are.

**Disabled checks** are not counted as run and not as unread; the score says how
many are disabled.

## The fleet figure

The median of the repositories' scores, shown with the lowest score and how many
repositories are under 60. Not the mean: twenty clean repositories would average
one with an open error up to a good-looking figure, and the lowest score beside the
median is what stops that.

## Decisions for the owner

Each has a recommendation, and the recommendation is what the tasks below build.

1. **The weights (20, 6, 1) and the caps (59, 89).** Recommendation: as written. They
   are easy to change before release and impossible to change quietly after, which
   is what the version field is for.
2. **Letter grades?** Recommendation: no. A number and the existing standing word are
   enough, and a letter invites an argument about the boundary between two letters
   that the formula does not settle.
3. **A score for a repository with unread checks?** Recommendation: yes, labelled as
   above. The alternative, no score until everything reads, hides the repositories
   Kennel can see least, which are usually the ones that most need looking at.
4. **Fleet figure: median with the lowest beside it.** Recommendation: as written.

## Tasks

### Task 1: the pure function

Files: `internal/kennel/score.go`, `internal/kennel/score_test.go`.

* `Score(Evaluation) ScoreResult` with `Value`, `Version`, `Checked`, `Unread`,
  `Disabled`, `Waived`, the open counts it was built from and the cap that applied,
  if any. The weights and caps are exported constants, because the docs test reads
  them.
* Table tests at every boundary: no findings, one of each severity, the cap taking
  effect, the floor, the same finding twice, a waived finding, a skipped check.
* Properties, held with a generated set of findings: adding an open finding never
  raises the score; waiving one never lowers it; the same evaluation gives the same
  result whatever order its findings arrive in.
* The package's purity test is unchanged: this imports nothing new.

### Task 2: carry it on the surfaces that carry the standing

Files: `internal/controller/views.go` (the repository row and the Overview gain
`score`), `api/openapi.yaml` and the two generated copies, `internal/api` tests, the
CLI's `kennel` output (including the offline `kennel check`, which gains a score line
and the field in JSON), `internal/mcp` (the Kennel tools' projection), and the Kennel
skill's summary step.

* The offline check and the controller compute the same number from the same
  findings; a test runs one fixture through both.
* The event stream's `kennel.summary` frame carries it too, because the UI drops a
  frame straight into its cache and a frame without the field would repaint the
  page without a score.

### Task 3: show it

Files: `web/src/routes/Kennel.svelte` (a tile beside the counts, with the lowest
repository and the number under 60), `KennelRepositories.svelte` (a sortable
column), the repository header, and Playwright specs for each, with the stream taken
away where a test rewrites the Overview, as `web/AGENTS.md` says.

* Phone width and both themes, as every page has.
* The unread wording sits under the number, not in a tooltip.

### Task 4: publish the formula

Files: `docs/kennel-club.md` (a section, `The score`, with the formula, the caps,
three worked examples and the version), `mkdocs.yml` untouched.

* `TestTheScoreDocsMatchTheConstants`: the docs' weights and caps are the code's.
  A weight changed in one place and not the other fails the build.
* The three worked examples are run through `Score` by the same test, so an example
  that stops being true is caught.

### Task 5: the record

`ROADMAP.md` and `roadmap/progress.md` once the coordinator allocates a package
number, and a decision record for the formula, because it is a published rule.

## Verification

`go test -count=1 ./internal/kennel/... ./internal/controller/ ./internal/api/
./internal/mcp/ ./internal/docs/ ./cmd/zoomies/`, `make openapi` and `make generate`
clean, `npm run check`, `npm run test:unit`, and Playwright for the Kennel specs
against the built binary.

## Out of scope

A history of scores over time, an alert when one falls, and a score for anything but
Kennel's checks. A trend is the obvious next question and is a separate plan, since
it needs stored scores and the rule for what to do when the formula's version changes.
