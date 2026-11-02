# The list pages as one card, Runners first: specification

1 November 2026. This is the specification for the next pull request of the
shared look's arrival in the product, after the Overview's two
(`SPEC-overview.md`, `SPEC-layout.md`). It builds the list card the signed-in
pack draws on every page that is a list, and converts the Runners page as the
proving page. Five pull requests follow it, one per remaining grid page
(Queue, Jobs, Workflows, Pools, Audit), each reading this document; the pages
whose lists are cards today (Hosts, Providers, Installations) are not in it,
since turning their cards into rows is a decision of its own.

## Where the pages are

Six pages draw a `DataGrid`: Runners, Queue, Jobs, Workflows, Pools and Audit.
Each is a page header, a band with the page's own sentence and figures
(`PageBand`), some context between the band and the list, a `FilterBar` of a
search field and menus, and the grid in its own frame with a Columns button,
selection, a bulk bar and pagination. The grid's saved layouts (column
visibility, width and order) follow the account through the preference
document (`table_layouts`).

The pack's mocks ([concept-runners-1440.png](signed-in/concept-runners-1440.png),
[concept-queue-1440.png](signed-in/concept-queue-1440.png),
[concept-pools-1440.png](signed-in/concept-pools-1440.png)) draw every list
the same way: under the band, one card with a tinted icon tile, a small-caps
count above the title, the primary filter as pills with counts, and a leaner
table; nothing else between the band and the list.

The owner chose this on 1 November 2026: the list pages next, as one grammar;
the context blocks folded under the list rather than dropped; the primary
filter as chips with counts and the default columns trimmed to the mock's,
with Columns still offering the rest; the kit and Runners in the first pull
request and one short pull request per grid page after it.

## What is built

### The kit: two small additions and one component

**`Panel`** gains two optional props and changes nothing for a panel that
leaves them out:

```ts
/** Small capitals above the title: a count or a scope. "11 known, 9 live". */
eyebrow?: string;
/** A row between the head and the body: the filter chips. */
toolbar?: Snippet;
```

The eyebrow is drawn in the titles block above the `h2`, in `--z-text-2xs`,
`--z-weight-semibold`, `--z-tracking-wider`, uppercase, `--z-text-muted`, as
the page header's eyebrow is. The toolbar is a `div.toolbar` after the header
and before the body, padded `--z-space-3 --z-space-5`, with the header's rule
under it.

**`DataGrid`** gains one optional prop:

```ts
/** Drop the grid's own border, radius and background: it sits flush inside a Panel. */
bare?: boolean;
```

With `bare`, `.grid` has no border, no radius and a transparent background;
the toolbar keeps its rule under it and the pagination foot its rule above
it. Nothing else changes: Columns, selection, the bulk bar, pagination, saved
widths and order, the phone's cards and rows are as they are.

**`FilterChips`**, new in `web/src/lib/components/`:

```ts
export interface FilterChipOption<T extends string> {
  value: T;
  label: string;
  /** Shown muted after the label when known. */
  count?: number;
}
interface Props<T extends string> {
  options: readonly FilterChipOption<T>[];
  /** The values in force. Empty is "All". */
  selected: readonly T[];
  /** What is being filtered, for the group's accessible name: "Filter by runner state". */
  label: string;
  /** The first chip's words. */
  allLabel?: string; // default "All"
  /** The count on the All chip, when known. */
  total?: number;
  onchange: (selected: T[]) => void;
}
```

A `div` with `role="group"` and `aria-label={label}`, holding one `button`
per option, `aria-pressed` true when its value is selected, a `Check` icon
(12px, `aria-hidden`) before the label of a pressed chip, and the count after
the label in `--z-text-muted` with tabular numerals. The All chip is pressed
when `selected` is empty; pressing it clears the selection; pressing another
chip toggles its value in the selection. A pressed chip has
`--z-border-strong` darkened to `--z-text` and `--z-surface-sunken` behind it,
in `--z-weight-semibold`; an unpressed one `--z-border-strong` on
`--z-surface-raised`. Every chip is `--z-radius-full`, at least `--z-space-10`
tall, and `--z-control-touch` tall on a phone; the row wraps. No colour
carries the state: the tick, the weight and `aria-pressed` do.

The gallery's Controls section gains a `FilterChips` block (All pressed, then
two of four pressed, with counts) and its Data section's grid becomes a bare
grid in a Panel with an eyebrow and chips, which is the list card shown once.

### Runners

Under the band, the page is:

1. **The analysis card** "What keeps failing to start", as now, only while the
   Failed state is in force.
2. **The list card**: a `Panel` with the runners icon (`Server`, the sidebar's
   for Runners), the eyebrow `{total} known, {live} live` where `total` is
   `fleet.stats.runners.total` and `live` is the sum of provisioning,
   registering, idle, busy and draining (the eyebrow is absent until the stats
   have arrived), the title "All runners", and in `actions` the search field,
   the pool menu, the host menu and the Include removed switch as they are
   today. Its `toolbar` is the `FilterChips` for the runner states in the
   order the product lists them (provisioning, registering, idle, busy,
   draining, failed; removed is not a chip, because the Include removed switch
   governs it), each labelled by `runnerStatus(state, prefs.quirkyStatus)` as
   the filter is today, each with its count from `fleet.stats.runners` when
   present, the All chip counting `total`. The chips read and write the
   `state` query parameter exactly as the popover did, so a pasted address,
   the band's running figure and the "Filters in effect" row behave as now.
   The grid is `bare` inside the panel's `flush` body, with everything it has
   today. `RunnerStateFilter` is no longer used by the page and is deleted
   when nothing else imports it.
3. **Fleet context**, one `details` under the card with the summary "Fleet
   context", styled as the page's 24-hour disclosure is today. Inside, in
   order: the context line and its usage link (`RunnerInsights`' `scope` row),
   the lifecycle chart and its note (`RunnerInsights`' `how` details, now
   open content rather than a disclosure inside a disclosure), and the
   existing "Explore fleet activity over the last 24 hours" disclosure. The
   `RunnerInsights` component keeps serving the pool detail page unchanged:
   it gains a `folded` prop that draws its scope row and lifecycle as plain
   content for the Runners page to put inside its own disclosure.

**Default columns.** Status, Name, Pool, Host, Current job, Jobs handled,
Memory and Actions stay visible by default; Age and CPU become
`hiddenByDefault`. A saved layout wins over the default, so an operator who
has arranged the grid sees no change: a grid arranged before a column became
hidden by default keeps that column (the grid names it as shown on its first
mount after the upgrade), and Columns restores any column.

_1 November 2026: the mock leaves Memory out too, and the first draft of this
document hid it. It stays, because that column carries the memory valve's
pills, the one place the list says what the valve did for a runner across the
fleet; a column that is the only place the list shows a feature is not trimmed.
The rule for the pages that follow is in the last section._

**Words.** The eyebrow and "All runners" and "Fleet context" are the only new
words; every sentence, label and count the page says today is unchanged.

### What stays

- `PageHeader`, `PageBand` and the band's figures and links.
- The grid's saved layouts and the `table_layouts` document.
- The pool detail page, which draws `RunnerInsights` as today.
- Every filter in the URL, with the same parameter names and values.

## Constraints

- No raw colour and no raw value that has a token; no new token and no new
  dependency.
- Both styles in both modes at 1440, 375 and 320 pixels with no horizontal
  overflow; 4.5:1 for text, 3:1 for a control's edge; a chip's state is never
  colour alone.
- Nothing is reachable from the UI that the REST API does not expose: the
  counts are the stats the page already reads.
- Every existing Runners behaviour and test is kept; a test whose selector
  changes is re-pointed, never deleted.
- British spelling, no em dash, no spaced double hyphen; imperative commit
  messages; every new behaviour arrives with a test watched to fail.

## How it is checked

- Unit: the chip model (a pure `chipSelection(selected, value)` toggle: All
  clears, a value toggles, a repeated value removes), and the live count
  (`liveRunners(stats)` sums the five live states and ignores failed and
  removed).
- Gallery (`gallery.spec.ts`): the `FilterChips` block has `aria-pressed` on
  the pressed chips and a tick drawn only on them, in every combination; the
  bare grid in a Panel has one border, the Panel's.
- Playwright on the built binary, chromium and mobile: the eyebrow counts
  match the stats; pressing the Busy chip narrows the rows, puts `state=busy`
  in the address and lists Busy in "Filters in effect"; two chips at once
  are two states in the address; All clears them; the default columns are the
  trimmed set and a column hidden before a reload stays hidden; the lifecycle
  is reachable inside Fleet context on a desktop and a phone; no horizontal
  overflow at 375 with every chip drawn. `runners.spec.ts`' state-filter test
  and `mobile.spec.ts`' lifecycle test are re-pointed.
- `docs/ui-guidelines.md`: the component inventory rows for `Panel`,
  `DataGrid` and the new `FilterChips`, and the Runners paragraph of the
  information architecture; `make screenshots` for the Runners captures in
  both styles.

## The contract with the pull requests that follow

Each of Queue, Jobs, Workflows, Pools and Audit does the same three things and
no more: wraps its grid, `bare`, in a `Panel` with the page's sidebar icon, an
eyebrow from the figures it already has and its search and menus in `actions`;
turns its primary filter (Queue's provisioning state, Jobs' and Workflows'
state, Pools' status, Audit's kind) into `FilterChips` in `toolbar`, keeping
the query parameter it writes today; and folds the context between its band
and its list into one disclosure under the card, trimming default columns to
the mock's set with `hiddenByDefault` unless a column is the only place the
list shows a feature. A page whose primary filter is already multi-valued
keeps it so.

_1 November 2026, Queue: the job filters the page shares with Jobs
(`JobFilters`, with its own fold on a phone and its row of filters in effect)
stay in their bar above the card, and only the controls that are the page's
alone go in `actions`: the branch field, the saved views, Reset and the hint.
A bar wide enough to wrap under the title on every screen is not a row of
actions; Jobs and Workflows, which share it, do the same. The Queue's default
of three statuses in force is three pressed chips, and the All chip is every
status, Removed included, as the page already spells it when nothing narrows._

_2 November 2026, Pools: the size-class panel (`AutoPoolsPanel`) is not
context and is not folded. Off, it is one quiet sentence; on, it opens by
itself when a change waits on an operator or the controller could not make
one, and a fold under the list would hide exactly that. It stays above the
card, as the Runners page's analysis card does. The status parameter holds one
value, so the chips press one at a time: a second chip replaces the first, and
the same chip again, or All, clears it. Lifetime and Docker are hidden by
default although a test read them from the row: the risk badge beside the name
already names docker-in-docker and persistent runners, so the row still says
the dangerous settings without the columns._

## Out of scope

- Hosts, Providers and Installations, whose lists are cards today.
- Any change to the band, the page header or the sidebar.
- The detail pages, the insight pages and the setup flows.
