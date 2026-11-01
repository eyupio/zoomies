# The Overview from its mock: specification

1 November 2026. This is the specification for the second pull request of the
shared look's arrival in the product, named in `SPEC-product.md` ("2. The
Overview, from its mock") and written once the first pull request and the page
bands' punch card were on their way to `main`. It changes one page, the
Overview, below its band.

## Where the page is

The band at the top of the Overview is the product's own and stays as `main`
has it: the sentence, the four figures along its lower edge (queued, running,
live runners, median wait), its cuts and switches on the Appearance page, and
the week as a punch card beside the words. Under it the page is a problems
line with a drawer, the pools as rows with a utilisation bar, the recent
failures, the running jobs beside the events feed, then History and
Diagnostics folded.

The signed-in pack's mock of the page
([concept-overview-1440.png](signed-in/concept-overview-1440.png)) was drawn
before three of the product's decisions, each written down beside the code it
protects:

| The mock draws                             | The product decided                                                                                              | This specification                                                                    |
| ------------------------------------------ | ---------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| four figure cards under the band           | the figures moved into the band "so the page starts on its work rather than on a second row of numbers"          | no figure cards; the band's figures are the four numbers                              |
| Needs attention as a card of every problem | one line and a drawer, because the list "pushed the pools, the scaling feed and the running jobs below the fold" | a card of the worst three, the rest behind the drawer; one line when nothing is wrong |
| a card per pool                            | rows, so "a pool sitting on its ceiling while jobs are still queued" is the one thing you see                    | cards, with the ceiling tick and the badge carried onto the card, at most six         |

The owner chose each of those on 1 November 2026, and chose to keep every
panel the mock leaves out (the failures, the running jobs, the events, History
and Diagnostics) in the same grammar rather than drop or leave them. The result
is drawn as the `built` state of the pack's own Overview mock:
[concept-overview-built-1440.png](signed-in/concept-overview-built-1440.png),
[concept-overview-built-375.png](signed-in/concept-overview-built-375.png) and
[concept-overview-built-classic-light-1440.png](signed-in/concept-overview-built-classic-light-1440.png).

## What is built

### The body is a list of panels

`web/src/lib/overview/panels.ts` exports `OVERVIEW_PANELS`, the body's panels
in their default order. A panel is:

```ts
interface OverviewPanel {
  /** Stable, never renamed: a saved layout names panels by it. */
  id:
    | "attention"
    | "pools"
    | "failures"
    | "jobs"
    | "events"
    | "history"
    | "diagnostics";
  title: string;
  /** Where it sits on a desktop: across both columns, or in the two-thirds or one-third column. */
  place: "full" | "main" | "side";
  /** The fleet states in which the panel is not drawn at all. */
  absentWhen?: (fleet: { setupPending: boolean }) => boolean;
}
```

`Overview.svelte` renders the list, in order, into one grid: `full` panels
span both columns; a run of `main` and `side` panels shares a row, the main
column two thirds and the side column one third, as the jobs and events do
today. On a phone (768px and under) it is one column in list order. The route
holds no hand-written order; a later pull request reads an order and a hidden
set from the account's preferences and hands the same list through them, which
is the contract in the last section.

The default order: `attention`, `pools`, `failures`, `jobs` (main) beside
`events` (side), `history`, `diagnostics`. The first-run checklist and the
stale-figures line stay above the list, where they are.

### One card grammar

`OverviewCard.svelte` wraps `Panel` with the mock's head: a tinted icon tile
(`--z-surface-sunken` ground, hairline edge, `--z-radius-md`) beside the title,
the description under it, the actions opposite. Every panel in the list draws
through it, so the page has one card design. The tile's icon is the panel's
own (the warning triangle, boxes, the bolt, the workflow glyph, activity, the
clock, the drive).

### The panels

**Needs attention** (`attention`, full). A card with the worst problems as rows:
a `Badge` for the severity, the title in the strong face, the controller's fix
under it in one sentence, and an Open button opposite that opens the problem's
own page (the same target the drawer's row has). Worst first, at most three;
with more, the card's action reads "and N more" and opens the drawer. With one
or two, the action reads "Open the drawer". With nothing wrong it is the one
quiet line the page has today ("Nothing needs your attention"), never a green
card, and during the first-run checklist it is absent. The band's Review button
keeps opening the drawer.

**Pools** (`pools`, full). A section title with the enabled count, then cards
in a grid of three at 1440 (`minmax(min(20rem, 100%), 1fr)`), one on a phone.
A card: the tile, the pool's name as the link to its page, the status pill
(the same `poolStatus` the rows use), Runners `live of max` and Waiting `n`
as a facts pair, and the utilisation meter with the ceiling tick at `max`.
A pool at its ceiling with work waiting is the mock's card with the pending
tone on its edge, the badge "At its ceiling" and the waiting figure followed by
"and nowhere to run", so it is still called out three ways. At most six cards;
past six, a line "and N more pools" linking to the Pools page. Disabled pools
are not drawn, as today. Empty and loading states are the panel's own today.

**Recent failures** (`failures`, full). The panel as today, in the card
grammar: its one-line form (count, newest, the link to the failed view) is a
single-row card; its list form keeps its rows and the mark-as-known control.
Absent during the first run.

**Active jobs** (`jobs`, main) and **Recent events** (`events`, side). Each as
today in the card grammar. The events feed's rows gain a status pill in place
of the icon alone, with the same words the row has now, so the kind of news is
said as well as drawn. The two still share a row on a desktop and the events
card is as tall as the jobs card beside it.

**History** (`history`, full) and **Diagnostics** (`diagnostics`, full, Host
capacity beside Startup and registration). The disclosures as today, drawn
through the card grammar with the tile, the title and the summary on the
summary line.

### Words

The page's subtitle stays "What the fleet is doing, and what needs you." No
sentence the panels say today changes; the only new words are the card
actions ("and N more", "Open the drawer", "and N more pools") and the pending
pool's "and nowhere to run".

## Constraints

- No raw colour and no raw value that has a token (`web/AGENTS.md`); no new
  token and no new dependency. The tile and the pill use what the kit already
  has (`Badge`, the radii, the surfaces).
- Status colours keep their fixed mapping; a pill never carries a state by
  colour alone.
- Both styles in both modes at 1440, 375 and 320 pixels with no horizontal
  overflow; 4.5:1 for text, 3:1 for a control's edge.
- Nothing that needs a person pushes the pools below the fold of a 1440x900
  laptop with a slim band: three attention rows and the band fit above it.
- Every existing Overview behaviour and its test are kept; a test whose
  selector changes is re-pointed, never deleted.
- The band, `FleetStatus`, `PageBand`, `BrandStage` and the punch card are
  not touched: that is the other session's work and lands separately.
- Prose in the repository's voice: British spelling, no em dash, no spaced
  double hyphen; imperative commit messages.

## How it is checked

- `npm run lint`, `npm run check` and the unit tests in `web/unit/`, with a
  unit test that `OVERVIEW_PANELS` has unique ids and that every `side` panel
  follows a `main` one.
- `overview.spec.ts`, `calm-overview.spec.ts` and `overview-columns.spec.ts`
  against the built binary, re-pointed where the DOM changed, plus new tests:
  the attention card shows the worst three with the fix and the Open target, and
  "and N more" opens the drawer; one line when nothing is wrong; a pool at its
  ceiling is badged and its waiting figure says so; a seventh pool becomes
  "and N more pools"; the rendered order of panels is `OVERVIEW_PANELS`'s; at
  375 the panels are one column in that order; the pools are above the fold at
  1440x900 with three problems and the band at Slim.
- The component gallery gains the pool card (ready, waiting, at its ceiling,
  disconnected) and the attention row, in all four style and mode
  combinations.
- `go test ./internal/docs/` for the guidelines change.
- Before and after photographs of the Overview at 1440 and 375 in both styles
  in the pull request, and `make screenshots` for the Overview captures.

## Out of scope

- Rearranging and hiding panels, the Appearance switches and the `panels`
  entry in the preference document: the third pull request, below.
- Any other page.
- The band and anything in it.

## The contract with the third pull request

The third pull request adds, for the Overview first: a grip on each card's
head for pointer drag, "Move up", "Move down" and "Hide" in each card's menu
for the keyboard, a Panels line per page on Appearance with a switch per panel
and "Reset layout", and `panels: { overview: { order: id[], hidden: id[] } }`
in the account's preference document, validated by the handler. It depends on
this pull request for exactly three things, which must not change under it:

1. `OVERVIEW_PANELS` is the only source of the body's panels and their ids.
2. `Overview.svelte` renders the body from a list of panels it is given, in
   that order, and draws nothing for a panel not in the list.
3. An unknown id in a saved layout is dropped on read, and a panel missing
   from a saved order is appended in default order, so a build that adds a
   panel never hides it and a build that removes one never breaks a layout.
