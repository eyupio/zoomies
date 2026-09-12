---
description: >-
  The design contract for the Zoomies web UI: design tokens, the fixed status
  colours, the app shell budget, and the accessibility rules every page keeps.
---

# Zoomies UI guidelines

The Zoomies web UI is meant to be left open on a second monitor all day. That is
the bar every decision below is measured against: it should be calm when nothing
is happening, unmistakable when something is wrong, and never *need* the
operator to click refresh.

This document is the contract. If you are adding a component, take the tokens
from here rather than inventing values.

---

## 1. Design tokens

All tokens live in exactly one place: `web/src/lib/styles/tokens.css`, declared
as CSS custom properties on `:root` and overridden under `[data-theme="dark"]`.
Tailwind v4 consumes them through `@theme` so utility classes and hand-written
CSS resolve to the same values: colours, sizes, radii, shadows, weights,
tracking and the easing curve are mapped by name, and the whole spacing scale
is derived from the same 4px step by arithmetic rather than by a mapping that
could drift.

**Never write a raw colour, and never write a raw value that already has a
token.** Colour is absolute: a hex or `rgb()` in a component is a bug, because a
colour written by hand is a colour that only works in one theme. Everything
else is a rule about repetition — a value that appears twice is a decision being
made twice, and it belongs here where it can be made once. A hairline border,
the tracking on an uppercase label, the width of a dialog and the height of a
switch thumb were all raw in several dozen files before they were tokens.

Three things are still raw, and each is raw for a reason worth knowing:

* **Media query widths.** `@media (max-width: var(--z-bp-md))` is not valid CSS
  — a media query is evaluated before custom properties exist — so the two
  thresholds are written out. `--z-bp-md` and `--z-bp-lg` are declared anyway,
  so the number has one home and JavaScript can read it rather than repeat it.
* **A one-off content measure.** The width a filter row stops growing at, the
  column count a card grid wraps at: these are that component's own layout,
  answer to nothing else, and naming them would only add a lookup. The moment a
  second component wants the same measure it becomes a token.
* **One hex, in the log viewer.** xterm.js takes its theme as JavaScript values
  and insists on `#rrggbb`, so the bridge in `LogViewer.svelte` resolves every
  token through a probe element and converts the result. The single literal
  there is the fallback for a colour that failed to parse, which is a
  last-resort value rather than a design decision.

### 1.1 Colour

The palette is the brand palette: a cool near-black spine with Runner Blue as
the interactive accent. See [brand.md](brand.md) for the identity itself and
the two decisions -- darkening Runner Blue for text, and spending Fast Cyan on
the busy state -- that turn it into a usable UI palette.

Every pairing below has been measured; the numbers are real, not aspirational.

#### Brand constants

Fixed by the identity, available as `--z-brand-*` and unchanged between themes.

| Token | Value |
| --- | --- |
| `--z-brand-black` | `#080808` |
| `--z-brand-white` | `#FFFFFF` |
| `--z-brand-cool-grey` | `#B9BCC2` |
| `--z-brand-mid-grey` | `#666A73` |
| `--z-brand-runner-blue` | `#2F80ED` |
| `--z-brand-fast-cyan` | `#22D3EE` |
| `--z-brand-paw-black` | `#000000` |

Two of these are the record rather than a value a rule reads. Mid Grey is the
brand's light-theme secondary text, but `--z-text-muted` darkens it to `#5B6069`
for the same reason the accent is darkened below: at 13px on white the brand
value is 5.4:1, and text an operator reads all day should be past 7:1. Paw Black
is the favicon artwork, which is a PNG rather than a colour any rule sets.

#### Neutrals

| Token | Light | Dark | Use |
| --- | --- | --- | --- |
| `--z-bg` | `#F6F7F9` | `#0A0B0D` | Page background |
| `--z-surface` | `#FFFFFF` | `#131519` | Cards, panels, table body |
| `--z-surface-raised` | `#FFFFFF` | `#1B1E23` | Popovers, dialogs, dropdowns |
| `--z-surface-sunken` | `#EEF0F3` | `#050607` | Wells, code blocks, table headers |
| `--z-border` | `#E2E5EA` | `#262A31` | Default hairlines |
| `--z-border-strong` | `#C7CBD3` | `#363B44` | Inputs, focused containers |
| `--z-text` | `#0B0C0E` | `#F4F5F7` | Primary text |
| `--z-text-muted` | `#5B6069` | `#B9BCC2` | Secondary text, labels |
| `--z-text-subtle` | `#6E737C` | `#868B94` | Timestamps, placeholders |

Primary text is 18:1 in both themes. Muted is 5.9:1 light and 10.3:1 dark.
Subtle -- the weakest text in the product -- is 4.45:1 light and 5.75:1 dark, so
even the timestamps clear AA.

#### Brand accent

| Token | Light | Dark | Use |
| --- | --- | --- | --- |
| `--z-accent` | `#1A63D8` | `#78B4FF` | Primary buttons, links, active nav, focus |
| `--z-accent-hover` | `#154FB0` | `#9BC9FF` | Hover |
| `--z-accent-subtle` | `#E8F0FD` | `#10203A` | Selected rows, badge backgrounds |
| `--z-accent-contrast` | `#FFFFFF` | `#08131F` | Text on an accent fill |

5.13:1 and 9.16:1 against their backgrounds; the text on an accent fill is
5.5:1 light and 8.7:1 dark. Full-strength Runner Blue (`--z-brand-runner-blue`) is
available for illustrative fills, where the bar is 3:1.

#### Status

Runner and job states get their own hue. The mapping is fixed -- do not use
these colours for anything else, because operators learn them.

| State | Token | Light | Dark |
| --- | --- | --- | --- |
| idle / healthy / success | `--z-idle` | `#177245` | `#5FD68C` |
| **busy / running** | `--z-busy` | `#0E7490` | `#3ED8F0` |
| provisioning / registering / pending | `--z-pending` | `#8A5200` | `#F0B34C` |
| draining / cordoned / paused | `--z-draining` | `#5E636D` | `#A6ABB5` |
| failed / error / destructive | `--z-danger` | `#B32218` | `#FF9089` |
| removed / neutral | `--z-neutral` | `#6E737C` | `#868B94` |

Busy is the Fast Cyan family: the brand asks for the accent to be used
sparingly, and *this runner is executing a job right now* is the single most
valuable "look here" signal on the page.

Each has a `-subtle` companion for badge and chart-fill backgrounds, and
`--z-danger` has a `-contrast` for text on a destructive fill. Every status is
**also** carried by a shape: a filled dot for busy, a hollow dot for idle, a
dashed ring for provisioning, a slash for draining, a triangle for failed.
Colour alone never encodes state -- that is both an accessibility requirement
and a practical one for an operator glancing at a sparkline from across the
room.

#### Charts

`--z-chart-1` … `--z-chart-6` are a categorical ramp derived from the status
hues, ordered for maximum adjacent separation. Sparklines use `--z-busy` for
running work and `--z-pending` for queue depth, because those are the two
quantities an operator is actually watching.

#### The mark

`--z-mark-chip` is the Zoomies Black ground the monochrome mark sits on. The
mark is white line art drawn for a dark surface, so the UI puts it on a small
black chip rather than inverting it -- one asset, correct in both themes,
legible at 24px. See [brand.md](brand.md), which lists every place the signed-in
product carries the mark, the name or the descriptor.

### 1.2 Type

Two faces, both self-hosted as woff2 so an air-gapped install has no network
dependency and no third-party font request:

* **Inter Variable** — UI. `--z-font-sans`. Geist and `system-ui` are the
  brand's sanctioned alternatives and sit in the fallback stack.
* **JetBrains Mono** — logs, IDs, labels, durations, anything monospaced.
  `--z-font-mono`.

The wordmark in the logo artwork is custom-rendered and is not Inter. Do not
try to set it in type.

Both declare a full system fallback stack so the first paint is never blank.

| Token | Size / line-height | Weight | Use |
| --- | --- | --- | --- |
| `--z-text-2xs` | 11px / 16px | `--z-weight-medium` | Table micro-labels, badge text |
| `--z-text-xs` | 12px / 18px | `--z-weight-normal` | Metadata, timestamps |
| `--z-text-sm` | 13px / 20px | `--z-weight-normal` | **Default table and body text** |
| `--z-text-base` | 14px / 22px | `--z-weight-normal` | Form controls, prose |
| `--z-text-lg` | 16px / 24px | `--z-weight-semibold` | Card titles |
| `--z-text-xl` | 20px / 28px | `--z-weight-semibold` | Page titles |
| `--z-text-2xl` | 28px / 34px | `--z-weight-bold` | Overview metrics |
| `--z-text-3xl` | 36px / 42px | `--z-weight-bold` | Hero numbers |

Four weights exist and the table names them rather than numbers, because the
numbers are Inter Variable's axis positions and are not round: `--z-weight-normal`
450, `--z-weight-medium` 500, `--z-weight-semibold` 550, `--z-weight-bold` 640.

Tracking is set for uppercase and for display sizes, and left alone in between:
`--z-tracking-wide` (0.04em) for the uppercase micro-labels above table headers
and drawer sections, `--z-tracking-wider` (0.08em) for the widely spaced brand
lockups in the footer, nav and About panel, and `--z-tracking-tight` (-0.01em)
for headings at `--z-text-2xl` and above, which set too loose at their default.

The base size is 13px, not 16px. This is a dense operational tool; 13px Inter at
450 weight on the warm background stays comfortably legible while fitting a
useful number of runners on screen. Prose in docs and empty states steps up to
14px.

Numerals in tables use `font-variant-numeric: tabular-nums` so columns align.
IDs use `--z-font-mono` at `--z-text-xs`.

**Form controls are the one exception, and only on a phone.** Below 768px,
`Input`, `Textarea` and `Select` step up to `--z-text-lg` (16px). Mobile Safari
zooms the whole viewport whenever a focused control is under 16px, and the
viewport meta deliberately sets no `maximum-scale`, so without this every field
tap on the first-run screens jumped a 360px page to roughly 410px of effective
width and ran the card off both edges — once per field. Control height comes
from the space scale, so nothing reflows; only the glyphs grow.

**A validation error takes the hint's place; it never appears beside it.**
`Field` renders one message row, because an error rendered *in addition* to the
hint adds a line and moves everything below it — including the submit button,
out from under a pointer already on its way down, so the click lands on whatever
takes its place and the operator experiences a button that does nothing. A field
on a form worth clicking therefore carries a hint, so the row is occupied before
an error needs it. Anything a field must say *alongside* an error goes in
`notice`, which is rendered independently, is `aria-live="polite"`, and is what
the caps-lock warning uses.

### 1.3 Spacing

A 4px base with a restricted scale. Only these values exist:

`--z-space-1` 4 · `-2` 8 · `-3` 12 · `-4` 16 · `-5` 20 · `-6` 24 · `-8` 32 ·
`-10` 40 · `-12` 48 · `-16` 64

Page gutters are `--z-space-6`, card padding `--z-space-5`, table cell padding
`--z-space-3` vertical / `--z-space-4` horizontal, form field gap
`--z-space-4`.

### 1.4 Radii, borders and geometry

`--z-radius-sm` 4 (badges, inputs) · `--z-radius-md` 8 (buttons, cards) ·
`--z-radius-lg` 12 (dialogs, panels) · `--z-radius-full` 9999 (dots, pills).

Borders come in three weights and the choice between them is meaning, not
taste. `--z-border-width` (1px) is the hairline that separates things belonging
to the same list. `--z-border-width-thick` (2px) marks the one that is selected,
focused or wrong. `--z-border-width-rail` (3px) is the flag down the left edge
of a problem, a disabled pool or a failed step — thicker because it is a signal
rather than a boundary.

Below the 4px spacing grid there are three optical nudges, `--z-nudge-1` to
`--z-nudge-3`. They exist because a checkbox aligned to the grid sits visibly
low against a 13px line. Anything that *can* sit on the spacing scale does.

The drawn controls have their own sizes, for the same reason: `--z-control-box`
(15px, the checkbox and radio box), `--z-control-thumb` (14px, the switch), and
`--z-control-icon` (14px, the icon inside a button). Field and button *heights*
are on the spacing scale and stay there.

Dialogs and drawers are cut to their content's comfortable measure rather than
to the viewport, so each has three widths and they are the three shapes we
actually put in one — a confirmation, a form, a table:
`--z-width-dialog-sm|md|lg` (400/560/820) and `--z-width-drawer-sm|md|lg`
(360/520/760).

### 1.5 Elevation

Shadows are cool-tinted, to match the near-black spine, and very restrained; in
dark mode elevation is carried mostly by surface colour, not shadow.

| Token | Light | Dark |
| --- | --- | --- |
| `--z-shadow-sm` | `0 1px 2px rgb(8 12 20 / .06)` | `0 1px 2px rgb(0 0 0 / .5)` |
| `--z-shadow-md` | `0 4px 12px -2px rgb(8 12 20 / .10)` | `0 4px 12px -2px rgb(0 0 0 / .6)` |
| `--z-shadow-lg` | `0 16px 40px -8px rgb(8 12 20 / .16)` | `0 16px 40px -8px rgb(0 0 0 / .7)` |

### 1.6 Focus

One look, two mechanisms. `app.css` draws the global ring with `outline`, from
`--z-focus-width`, `--z-focus-colour` and `--z-focus-offset`; that is what
almost everything gets, on `:focus-visible` only, so a mouse click draws no ring
and a keyboard tab does. `--z-focus-ring` is the same ring as a `box-shadow`,
for the controls whose outline a scrolling or clipping parent would cut off. A
consumer on a raised surface overrides `--z-focus-gap` so the ring's inner gap
matches what it is drawn on.

The ring is never removed. Removing it and replacing it with a colour change
does not count: a border that goes from grey to blue is invisible to anyone who
cannot see the difference, and is exactly the change a focused *and* invalid
field cannot make.

### 1.7 Motion

| Token | Value | Use |
| --- | --- | --- |
| `--z-motion-fast` | 120ms | Hover, focus, colour change |
| `--z-motion-base` | 200ms | Panels, dropdowns, row insert |
| `--z-motion-slow` | 320ms | Dialogs, drawer, route transition |
| `--z-ease` | `cubic-bezier(.2,.8,.2,1)` | Everything |

State changes animate the *colour and the dot*, never layout — a table that
reflows while you are reading it is worse than one that does not animate at all.
Everything inside `@media (prefers-reduced-motion: reduce)` collapses to `1ms`.

### 1.8 Theme

Three states: `light`, `dark`, `system`. `system` is the default and sets no
attribute, so `prefers-color-scheme` decides. An explicit choice writes
`data-theme` on `<html>` and persists to `localStorage` under `zoomies.theme`.
The choice is applied by a tiny inline script before first paint so there is no
flash.

### 1.9 Scrollbars

The scrollbar is the one control the browser draws for us, and the one that is
easiest to leave too faint to find. It is styled so that it belongs to the theme
and, more to the point, so that an operator can see it and take hold of it.

| Token | Light | Dark | Use |
| --- | --- | --- | --- |
| `--z-scrollbar-size` | `14px` | `14px` | The column, which is the whole hit target |
| `--z-scrollbar-inset` | `3px` | `3px` | Gap between thumb and column edge, each side |
| `--z-scrollbar-thumb` | `--z-text-subtle` | `--z-text-subtle` | Resting |
| `--z-scrollbar-thumb-hover` | `--z-text-muted` | `--z-text-muted` | Under the pointer |
| `--z-scrollbar-thumb-active` | `--z-text` | `--z-text` | While it is held |
| `--z-scrollbar-track` | `rgb(8 12 20 / .06)` | `rgb(255 255 255 / .07)` | Behind the thumb |

The thumb borrows the weakest text colour, so its ratios are the ones already
measured above: against every surface it can sit on it is never below 4.1:1 in
light or 4.8:1 in dark, past the 3:1 a control needs. The track is a
tint rather than a colour so that it reads on a white card and on a sunken well
alike. The visible thumb is the column less the inset on each side -- 8px -- and
the whole 14px column responds to the pointer.

Two rules keep it working, and both are written into `app.css` beside the
rules themselves:

* **Pointer only.** The styling sits inside `@media (pointer: fine)`. On a
  touchscreen a finger scrolls the content, never the bar, and the browser's
  overlay scrollbar -- which takes no space and fades when idle -- is the right
  control there. Styling it would pin a permanent column down the edge of a
  phone.
* **One mechanism per browser.** Chromium and WebKit draw from the
  `::-webkit-scrollbar` pseudo-elements, which are the only way to get a thumb
  that is inset and rounded. The standard `scrollbar-width` and
  `scrollbar-color` properties are set only where those pseudo-elements are
  unknown (`@supports not selector(::-webkit-scrollbar)`, which today means
  Firefox), because a browser that knows both ignores the pseudo-elements the
  moment the standard properties are set and draws its own thin bar instead.
  Setting `scrollbar-width: thin` "as well" is exactly how the thumb became a
  sliver nobody could find.

The log viewer's terminal draws its own scrollbar rather than the browser's, so
it is handed the same three thumb tokens through its theme.

---

## 2. Information architecture

Persistent left navigation, collapsible to icons only (persisted). Order is
fixed, because muscle memory is the point:

1. **Overview** — fleet health at a glance
2. **Pools** — what runners to make
3. **Runners** — what runners exist right now
4. **Jobs** — what has run
5. **Usage** — runner-hours and job activity by pool, repository or workflow
6. **Hosts** — where runners can go
7. **Installations** — GitHub App connections
8. **Migrate repositories** — move repositories off GitHub's runners onto this
   fleet. The navigation shortens it to *Migrate*, because a nav label has to
   fit beside an icon in a 232px column; every other place that names the page
   — its heading, the palette, the shortcut sheet, the browser title — uses the
   full name.
9. **Audit** — who did what
10. **Settings** — users, tokens, appearance, danger zone

Every page is shown, in both themes, in [The UI](ui.md).

The navigation is headed by the mark, the wordmark and the descriptor, and every
page ends in a hairline footer carrying the mark, the name, the running version,
a link to the docs and the credit *Developed by EyUp.io* — so a signed-in
screenshot says which product and which build it came from, and who makes it,
without anyone having to open Settings. On a phone the navigation moves to the
bottom edge and loses its masthead, so the mark appears in the top bar instead —
and again at the head of the side menu, which is the one place on a phone with
room to say the name.

**A link that leaves the product opens in a new tab; a link within it does
not.** An operator watching a fleet should not lose the page they were on to go
and read what a runner group is. Both footer links leave, so both take a new
tab, as does every link to GitHub or to the documentation from a dialog —
`Button` has a `newTab` prop for the ones that are buttons. Each of them adds
"(opens in a new tab)" to its accessible name, because a tab appearing under
somebody who cannot see it happen is disorienting rather than helpful.

A **command palette** on `Cmd/Ctrl+K` jumps to any page, any pool, any runner by
ID or name, and runs quick actions (drain a runner, cordon a host, create a
pool). It is the fastest path to everything and is discoverable from a hint in
the top bar.

### Overview

The one page that has to earn the second monitor.

* **Four metric tiles**: queued jobs, running jobs, live runners, median queue
  wait. Each carries a sparkline of the last hour.
* **Per-pool utilisation bars** — busy / live, with the pool's min and max marked
  so an operator can see a pool pinned at its ceiling.
* **Recent scaling activity** — a reverse-chronological list of decisions in the
  scheduler's own words: *"scaled `linux-x64` 2 → 4: 3 jobs queued > 30s"*.
  On a desktop the pools and the running jobs share the left-hand column and
  this feed takes the right, cut to their height and scrolling inside itself.
  It is the one panel whose length says nothing about the fleet, so it never
  decides the height of the page: a fleet with one pool does not get a screen
  of blank space under it because the scheduler has been busy.
* **Active jobs**, under the pools, and **Recent outcomes**, across the bottom
  of the page — what is running this moment, and how the last jobs ended,
  newest first. An outcome names the step a job failed at, and a job whose
  runner stopped under it is badged *Runner lost*: that failure is the fleet's,
  not the workflow's, and the page is where the two are told apart.
* **Problems summary** — one line saying how many things need a person, worst
  severity first, with a *Review* button that opens the problems drawer.
  **When everything is fine it is a single quiet line, not an empty box and not
  a green celebration.** It is a line rather than a list on purpose: the panels
  above are what the page is for, and a configuration warning somebody chose
  deliberately must not push the pools and the running jobs below the fold
  every day.

### The problems drawer

Reachable from the count in the top bar on every page, from the Overview's
*Review*, and from the command palette. It holds the list itself — unhealthy
hosts, failed registrations, webhook delivery failures, unmatched queued jobs,
jobs whose runner stopped under them in the last hour, and every dangerous
configuration setting the validator flagged — worst first,
each entry saying what is true, why it matters and what to change, with a link
to the pool, host, runner or installation it is about.

Every entry can be **dismissed**, which is a per-operator preference in
`localStorage` and never fleet state: `GET /api/v1/problems`, `zoomies status`
and any alerting rule still see everything. Two rules stop a dismissal from
hiding a real fault:

* it is forgotten the moment the controller stops reporting that problem, so
  the same fault happening again is news again; and
* it only covers the severity it was made at, so a warning that becomes an
  error comes back.

Dismissed entries stay listed at the bottom of the drawer, dated, and can be
restored one at a time or all at once.

---

## 3. Component inventory

The components below live in `web/src/lib/components/` and take their values
from tokens. Everything else lives in a folder named for the thing it serves --
`lib/pools/`, `lib/jobs/`, `lib/shell/` -- so a file's path says who owns it,
and a component earns its way into `components/` when a second feature needs it
rather than on the day it is written. Svelte 5 runes (`$state`, `$derived`,
`$props`, `$effect`) throughout — no stores except for genuinely global state.

### Primitives

| Component | Notes |
| --- | --- |
| `Button` | variants: `primary`, `secondary`, `ghost`, `danger`; sizes `sm`, `md`; `loading` swaps the label for a spinner without changing width |
| `IconButton` | square, always has an `aria-label` |
| `Input`, `Textarea`, `Select`, `Switch`, `Checkbox`, `RadioGroup` | all support `error` and `hint`; the error is announced with `aria-describedby` |
| `Field` | label + control + hint + error; the only way form controls are laid out |
| `Badge` | status pill: colour **and** shape from the state map |
| `StatusDot` | the shape half of the state encoding, reusable inline |
| `Tooltip` | on hover *and* focus; never the only place information lives |
| `Dialog` | focus trap, restores focus on close, `Esc` closes, backdrop click closes only non-destructive dialogs |
| `Drawer` | right-hand detail panel; same focus rules |
| `NavMenu` | the phone's side menu: every section, named; slides from the left, same focus rules, closes when one is chosen |
| `DropdownMenu` | roving tabindex, type-ahead |
| `Tabs` | `aria-controls`/`aria-selected`, arrow-key navigation |
| `Toast` | bottom-right, `aria-live="polite"` (`assertive` for errors), auto-dismiss except on error |
| `Skeleton` | **the only loading affordance for content.** Spinners are for in-flight *actions* only |
| `EmptyState` | icon + one sentence of what this is + the action that fills it; a `visual` snippet replaces the icon disc where artwork is wanted, as the not-found page does with the mark |
| `CopyButton` | wraps any ID; announces "copied" to screen readers |
| `RelativeTime` | "4m ago", absolute ISO timestamp in the tooltip, updates itself |
| `Duration` | humanised, tabular numerals |
| `Sparkline` | inline SVG, no chart library, `role="img"` with a text summary |
| `UtilisationBar` | busy/live with min and max ticks |
| `ConfirmDialog` | destructive confirmation that **names the thing** ("Delete pool `linux-x64`? 3 runners will be drained.") and requires typing the name for anything irreversible |

### Composites

| Component | Notes |
| --- | --- |
| `DataGrid` | TanStack Table core + our own markup. Server-side pagination, sorting and filtering; column show/hide persisted per grid; sticky header; row selection with a bulk action bar; full keyboard navigation (`↑ ↓` rows, `Enter` opens, `Space` selects, `Shift+↑/↓` range) |
| `FilterBar` | chips for active filters, each individually removable, plus a clear-all |
| `PageHeader` | title, subtitle, breadcrumb, primary action, and the refresh button where the page passes `onrefresh` |
| `RefreshButton` | the one refresh: registers the page's handler so the button, `R` and the palette all run it; turning icon while it works |
| `MetricTile` | number, label, delta, sparkline |
| `LogViewer` | `lib/logs/`. xterm.js with search, follow/pause, wrap toggle, download, and a line counter |
| `RunnerTimeline` | `lib/runners/`. One row per state with how long the runner stayed there, reconstructed from the four timestamps a runner row carries -- and it says so, rather than letting an operator read it as an audit trail |
| `Wizard` | the pool creation flow: target → labels → backend → scaling → review |

### The log viewer

100k lines without jank is a hard requirement. xterm.js with the canvas/WebGL
addon handles the rendering; the constraints on our side are:

* a bounded scrollback (`scrollback: 100000`) — beyond that the oldest lines go;
* writes are **batched on `requestAnimationFrame`**, never once per SSE frame;
* `fit` is debounced and only runs on real size changes;
* search uses the search addon, not a DOM scan;
* "follow" is a mode, not a scroll position — leaving the bottom turns it off,
  and a floating "jump to latest" button turns it back on.

---

## 4. Interaction patterns

### Real time

A single SSE connection to `/api/v1/events` feeds a client-side cache; each
page subscribes to the event kinds it cares about. On reconnect the client
sends `Last-Event-ID` and the server replays what it buffered, then the page
does one reconciling fetch; frames that land while that fetch is in flight are
held and applied on top of its result, because they are newer than it. The
connection state is visible in the top bar: a quiet dot when live, an explicit
"reconnecting…" when not — never a silent stall.

A grid that fetches its rows from the server refetches on the cache's *shape*
— a runner appearing, changing state, pool, host or job — not on every frame,
so a heartbeat that only moves a runner's CPU and memory costs no round trip;
and it refreshes at most about once a second however fast the shape moves, so
a fleet in trouble is still readable while it is in trouble.

The one page that asks rather than listens is *Add a host*: while it waits for
the new machine it fetches its own join token every few seconds, because
credentials are deliberately not on the stream. Even there the stream is the
fast path — a host's first frame is the cue to ask at once — and the page says
in words that it is waiting and how.

### Refresh

Refreshing is never how the screen keeps up — the stream is — but it is how an
operator settles the question of whether it has. Some things genuinely do not
arrive over the stream: join tokens, users, API tokens, and the configuration.

So the control is one control, in one place, everywhere it means anything:

* `PageHeader` renders it, first in the actions row, ahead of the page's own
  actions. It is a `secondary` button, never the primary one — the primary
  action changes the fleet, not the view of it.
* A page declares what refreshing means for it by passing `onrefresh`, and
  `RefreshButton` registers that with `lib/state/refresh.svelte.ts`, which is
  also what the `R` shortcut and the palette entry run. The button and the key
  cannot drift apart, because there is only one handler.
* A page with nothing to fetch — a wizard mid-flight, the not-found page —
  passes nothing and shows no button. A control that succeeds at nothing is
  worse than no control.
* One at a time: a second press joins the refresh in flight rather than starting
  another, so a page that fans out to five requests cannot be made to send
  fifteen.
* The icon turns while the fetch runs and for a beat after it, because a refresh
  answered out of a warm cache in twenty milliseconds otherwise looks like
  nothing happened. The label does not move; `aria-busy` and one polite
  announcement say the same thing without the animation.

### Optimistic updates

User actions apply locally first and roll back on failure with a toast that says
what failed and why. Draining a runner flips its badge immediately; if the API
returns 409 the badge flips back and the toast explains. Background outcomes
(the runner actually reaching `removed`) arrive over SSE and need no toast.

### Forms

Validation rules come from the same source as the API's, generated into
`web/src/lib/api/schema.d.ts` from the OpenAPI document. Errors appear inline on
blur and again on submit; the first invalid field receives focus. Defaults are
filled in from the host's detected capabilities, so pool creation is mostly
pressing *Next*.

### Destructive actions

Named, counted, and consequential. "Delete" says what will be destroyed and how
many things it affects. Anything that removes runners from GitHub says so
explicitly. Irreversible actions require typing the resource name.

### Empty states

Never a blank table. Each says what the thing is and what to do next, with the
action inline:

* Pools: *"No pools yet. A pool decides what labels your runners answer to and
  how many of them exist."* + **Create a pool**
* Runners: *"No runners right now. That is normal when nothing is queued —
  runners are created on demand."*
* Jobs: *"No jobs have run on this fleet. This view shows jobs a pool claims or
  a runner here ran."* + **Include other runners**

**An empty grid that is empty because of a filter says so instead**, naming the
filter rather than the noun: "No pools match those filters", "No runners match
these filters". The two are different facts and an operator acts on them
differently — one is a fleet with nothing in it, the other is a search with
nothing in it. The Jobs page carries this furthest, because an empty grid there
means three different things: with other runners included it is "no jobs
recorded yet", which points at webhook delivery; without them it is "no jobs
have run on this fleet", which offers to widen the view; and under the unmatched
filter it is "no unmatched jobs", which is good news and says so.

### Keyboard

Everything reachable, in a sensible order, with a visible focus ring
(`2px` `--z-accent`, `2px` offset — never removed). `Cmd/Ctrl+K` palette,
`g` then `o/p/r/j/u/h/i/m/a/s` to jump between sections, `/` focuses the current
page's search, `R` refreshes it, `?` opens the shortcut sheet, `Esc` closes the
topmost layer. While a dialog, drawer, menu or the palette is open, `Esc` is
the only one of these the shell answers; the rest belong to the overlay, so a
`g r` typed into a confirmation cannot navigate away from the thing being
confirmed. Tab stays inside the innermost open overlay, and everything outside
it is `inert`.

### Responsive

Two thresholds, `--z-bp-md` (768px) and `--z-bp-lg` (1180px), and so three
ranges:

* `< 768px` — **phone.** The navigation becomes a bar along the bottom edge
  carrying the four sections a fleet is watched with — Overview, Pools, Runners,
  Jobs — each under its own word, plus a **More** button opening a side menu
  that lists all ten. Ten icon-only targets across a 412px screen were 40px
  apart and told apart only by a glyph, which is not a navigation an operator
  can use one-handed at 3am. The menu is a modal overlay like any other: Escape
  closes it, the page behind it is inert, and choosing a section closes it.
  Collapsing is a *desktop* idea and the phone must never inherit it — a bar
  along the bottom has nothing to collapse, and `.nav.collapsed` outranking the
  phone's own rules is what once made that bar 56px wide with every entry piled
  into the corner. Metric tiles stack, a grid scrolls inside its own frame
  rather than widening the page, text controls step up to
  `--z-control-font-touch` so iOS does not zoom, and every control stays usable:
  the Playwright suite's mobile project runs the whole suite at this width,
  drains and wizard included.
* `768–1180px` — **tablet.** The nav starts collapsed to icons unless the
  operator has chosen otherwise. That default is bounded at both ends, in
  `prefs.svelte.ts` and in the inline script in `index.html` that applies it
  before first paint: a phone is not a narrow desktop and has no sidebar to
  collapse.
* `> 1180px` — **full.**

An overlay that covers the screen — the drawer, the dialogs, the command
palette, the side menu, the bar along the bottom and the toasts — takes its
width from `--z-window-width`, never from a bare `100%` or `inset`. A phone
answers a page that overflows sideways by laying the whole thing out in a wider
block and showing it smaller, and everything `position: fixed` is laid out
against that block rather than against the screen. One row a few pixels past
the edge is therefore not one row: it put the problems drawer at its desktop
760px, anchored to the right-hand end of a block twice the width of the phone,
and left an operator looking at a sliver of a panel whose close button was off
the side of the screen. `min(100vw, 100%)` is the window on a phone, and on a
desktop it is the side of the two that does not add a scrollbar.

The mobile project emulates a 412px Pixel 7, which is the forgiving end of the
range most phones are in, so two of its tests set 360px instead and walk every
section at it. That is where a settings row — 15rem of key, the value beside it
— ran **Change** over the edge, and with it the document and every overlay laid
out against it.

A third threshold is not a judgement call to be made per component. Four
components had one each — 560, 640 and 720 — and every one of them was a "stack
this on a phone" rule written to a width somebody eyeballed. They are all 768px
now. A media query cannot say "above 1180" without naming the next pixel, so the
one `min-width: 1181px` in `FleetMetrics.svelte` is the same threshold from the
other side and carries a comment saying so.

---

## 5. Performance budget

| Budget | Limit |
| --- | --- |
| App shell (JS + CSS, gzipped) | **< 200 KB** |
| Route chunk | < 80 KB gzipped, with named exceptions |
| First contentful paint on a warm cache | < 400 ms |
| Interaction to next paint | < 200 ms |

How it is kept:

* route-level code splitting; xterm.js and TanStack Table load only on the
  routes that use them;
* no chart library — sparklines and bars are hand-written SVG;
* Lucide icons imported individually so tree-shaking works;
* fonts declared by hand as four Latin faces with `font-display: swap`,
  rather than @fontsource's own stylesheets, which pull in Cyrillic, Greek and
  Vietnamese in both woff and woff2;
* the SSE cache is a plain `Map`, not a reactive deep-proxy over thousands of
  rows.

`npm run build` prints every chunk's gzipped size and **fails** if the shell or
any route is over its budget, so this stays true.

The shell is the entry chunk, what it imports statically, and the CSS those
bring with them — not every stylesheet in the build, which is what it used to
count and why the printed number overstated the first paint.

One route is over the 80 KB line and is named in `ROUTE_ALLOWANCES` in
`web/vite.config.ts` with the size it is allowed: `xterm`, the terminal
emulator behind live runner and job output, which loads only on the two pages
that show it. A route that grows past the budget without an entry there fails
the build; adding one is a decision to make in a pull request, not a number to
nudge.

---

## 6. Accessibility checklist

Every PR that touches the UI should be able to answer yes to all of these:

- [ ] Operable with the keyboard alone, including grids and dialogs
- [ ] Focus visible everywhere, and moved into and restored out of dialogs
- [ ] Custom widgets carry the right `role`, and state via `aria-*`
- [ ] Colour is never the sole carrier of meaning
- [ ] Text meets WCAG AA in **both** themes (4.5:1 body, 3:1 large and UI)
- [ ] Live regions announce async outcomes once, not on every keystroke
- [ ] Reduced-motion honoured
- [ ] Zoom to 200% without loss of function

---

## 7. Adding something new

1. Look for an existing component first. The inventory above is deliberately
   small.
2. If a token you need does not exist, add it to `tokens.css` in **both**
   themes, check the contrast, and record it here.
3. If it is a status, add it to the state map (colour **and** shape).
4. Write the empty, loading and error states before the happy path. They are
   most of what an operator actually sees.

---

### Testing a fleet in trouble

The Playwright suite's shared fixture is a fleet with nothing wrong with it,
and deliberately so: the demo keeps its two starting runners young
(`freshenDemoRunners`) because an instance left open on somebody's desk must
not report a fault in a fleet that has no agent to have one. The cost is that
every page which explains a fault has nothing to render there — the problems
drawer, both stuck-runner shapes, a pool nothing can place, a job GitHub is
holding — so all of them went untested.

`ZOOMIES_SEED_STUCK=true`, on top of `ZOOMIES_SEED_DEMO`, is the opt-in that
breaks three things in that fleet: it ages the demo's two starting runners past
the point where `runners.not_progressing` says so, adds a pool whose host
selector nothing answers, and adds a job held for a deployment review. Its rows
carry demo identifiers, so every guard that keeps a fixture out of a real fleet
applies to them unchanged.

The `diagnostics` Playwright project runs `tests/diagnostics.spec.ts` against
its own controller on that fixture. Put a spec there when what it protects is a
diagnosis — the words an operator reads on their worst day — rather than a
grid.

## 8. Screenshots

The screenshots in `docs/screenshots/` — the ones [The UI](ui.md), the site's
home page and the repository README embed — are captured from the real binary,
never from a design file or a retouched page. `make screenshots` builds, boots
a controller with the demo fleet (`ZOOMIES_SEED_DEMO`) and authentication on,
signs in as a freshly bootstrapped administrator, and photographs every page in
both themes at 1440×900 on a 2× display, plus the Overview on a phone. The
files are lossless WebP. The script is `web/tests/support/screenshots.mjs`,
and it needs Pillow (`pip install pillow`) to encode them.

Refresh them when a page changes in a way a reader would notice, and commit
the whole set: a gallery in which one page has the new navigation and the rest
the old one is worse than one that is uniformly a release behind. If a shot
needs the fixture to show something new, change the fixture in
`internal/controller/seed.go` rather than the image — that is the fleet the
Playwright suite asserts on, so the picture and the tests stay honest together.
