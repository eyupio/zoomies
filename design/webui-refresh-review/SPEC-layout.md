# The Overview's layout, the operator's: specification

1 November 2026. This is the specification for the third pull request of the
shared look's arrival in the product, named in `SPEC-overview.md` ("The
contract with the third pull request"). It lets a signed-in operator rearrange
and hide the Overview's panels, and keeps the result with their account.

## Where the page is

After the second pull request the Overview's body is one grid drawn from
`web/src/lib/overview/panels.ts`: `OVERVIEW_PANELS` names the panels in their
default order with a stable id, a place (`full`, `main` or `side`) and the
fleet states in which a panel is absent, and `visiblePanels` is the one filter
over that list. The route renders what the filter returns and lays nothing by
hand. The account's private preference document (`GET` and `PUT
/api/v1/auth/preferences`) already carries `table_layouts`, `assistant` and
`bands`, read forgivingly on the client and validated on the server, with the
browser's copy as the fallback when nobody is signed in.

The owner chose this on 1 November 2026: a separate pull request after the
Overview's restructure; the controls in an Arrange mode on the page rather
than always on every card or only on Appearance; order only, with each
panel's width fixed by its place; the Overview first, with the preference
shaped per page so other pages can follow without a schema change.

## What is built

### The layout

```ts
export interface PanelLayout {
  /** Panel ids in the order drawn. A panel not named is appended in default order. */
  order: OverviewPanelId[];
  /** Panels not drawn. */
  hidden: OverviewPanelId[];
}
export const DEFAULT_LAYOUT: PanelLayout; // the registry's order, nothing hidden
export function readPanelLayout(
  raw: unknown,
  panels: readonly OverviewPanel[],
): PanelLayout;
export function visibleLayout(
  panels: readonly OverviewPanel[],
  layout: PanelLayout,
  fleet: { setupPending: boolean },
): OverviewPanel[];
```

`readPanelLayout` drops an id the registry does not have and appends, in
default order, any panel the saved order leaves out, so a build that adds a
panel never hides it and a build that removes one never breaks a layout.
`visibleLayout` applies the order, removes the hidden, then applies each
panel's `absentWhen`: the first run hides the attention card and the failures
whatever the layout says. It replaces `visiblePanels` as the one function the
route renders from. Both live in `panels.ts`.

### Where it is kept

The preference document gains `panels`, a map from page to layout:

```yaml
panels:
  overview:
    {
      order: [pools, attention, jobs, events, failures, history, diagnostics],
      hidden: [history],
    }
```

In `api/openapi.yaml`: `PanelPreferences` (`additionalProperties:
PanelLayout` keyed by page), `PanelLayout` (`order`, an array of up to 12
`OverviewPanelId`-shaped strings; `hidden`, the same). The handler validates as
the band document is validated (`internal/api/band_preferences.go` is the
pattern): a page this build has no panels for, or an id not in that page's
list, is a 422 naming the field (`panels.overview.order`). Every field is
optional; absent is a choice never made. Both OpenAPI copies are regenerated
with `make openapi`.

On the client, `prefs` keeps `panelLayout(page)` and `setPanelLayout(page,
layout)` beside the band settings: stored in the browser under the account's
key, sent in the same `PUT` as the rest of the document, read forgivingly with
`readPanelLayout`. Signed out, the browser's copy is the layout, as the bands
behave.

### Arrange mode

An **Arrange** button in the Overview's page header, beside Refresh, toggles
the mode for this page view (not remembered). While it is on:

- Every card's head (the `Panel` tile row and the disclosures' summary row)
  gains, at its right: a grip (`GripVertical`, `aria-label="Move {title}"`),
  **Move up** and **Move down** buttons (`IconButton`, `ChevronUp`,
  `ChevronDown`, named `Move {title} up` and `Move {title} down`; the first
  has no up and the last no down, so that button is disabled with a reason)
  and **Hide** (`IconButton`, `EyeOff`, `Hide {title}`).
- A move swaps the panel with its neighbour in the order. Widths never
  change: a panel keeps its place, so a `main` and the `side` after it share
  a row wherever they stand together, and part if one is moved alone. Focus
  stays on the pressed button after a move, so a keyboard user can press it
  again.
- The grip starts a pointer drag (`startColumnDrag`'s gesture, generalised in
  `web/src/lib/actions/columnGesture.ts` to a `startPanelDrag` over
  `[data-panel]` slots): the slot under the pointer is marked as the drop
  target, and dropping places the dragged panel before it, or after the last.
  The same threshold and cancel rules as a column drag; a tap on the grip does
  nothing.
- A hidden panel is a one-line row in its place: `{title} is hidden.` with a
  **Show** button (`Show {title}`), so it can be brought back without leaving
  the page. Arrange off, a hidden panel is simply not drawn.
- A bar at the top of the body: `Arrange the Overview. Drag a card by its
grip, or use its buttons. {n} hidden.` with **Reset layout** (back to the
  default; disabled when the layout is the default) and **Done** (Arrange
  off). The bar has `role="region"` named "Arrange the Overview".
- Every change saves at once through `prefs`, as a band change does.

Arrange is available on a phone too, where the same order is one column; the
grip works by touch because the gesture is pointer events.

### Appearance

A new setting under Page bands, **Overview panels**: the panels in their
current order, each a `Switch` (`{title}` on the Overview) bound to the hidden
set, a line saying the order is changed on the Overview itself with Arrange,
and **Reset layout**. It follows the account as the bands do, and is one more
`.setting` block in `web/src/lib/settings/AppearancePanel.svelte`.

### What stays

- `OVERVIEW_PANELS` is still the only source of panels and ids; `absentWhen`
  still wins over a layout.
- The jump bar's section tracking reads element ids as it does now, so it
  follows whatever order is drawn.
- The band, `FleetStatus`, `PageBand` and the punch card are not touched.
- The pool cards, the attention card and the other panels are not changed
  inside; only their heads gain the Arrange controls, and only in Arrange
  mode.

## Constraints

- No raw colour and no raw value that has a token; no new token and no new
  dependency. The grip, the buttons and the hidden row use `IconButton`,
  `Button` and the kit's radii and surfaces.
- Both styles in both modes at 1440, 375 and 320 pixels with no horizontal
  overflow; 4.5:1 for text, 3:1 for a control's edge.
- Every control in Arrange mode has an accessible name that names the panel;
  the keyboard can do everything the pointer can.
- Nothing is reachable from the UI that the REST API does not expose: the
  layout is the preference document's.
- Every existing Overview behaviour and test is kept.
- British spelling, no em dash, no spaced double hyphen; imperative commit
  messages; every new behaviour arrives with a test watched to fail.

## How it is checked

- Unit tests for `readPanelLayout` (unknown id dropped, missing appended in
  default order, duplicates collapsed, nothing is a valid document) and
  `visibleLayout` (order, hidden, `absentWhen` over a layout that names an
  absent panel).
- `internal/api`: the preference round trip carries `panels`, and a page or id
  this build does not have is a 422 naming `panels.overview.order` or
  `panels.<page>`; `go test ./internal/docs/` for the API surface doc.
- Playwright on the built binary, chromium and mobile: Arrange on shows the
  controls and off hides them; Move down then Move up restores the order and
  focus stays on the button; a drag by the grip reorders (pointer events, as
  `column-layout.spec.ts` drags a column); Hide removes a panel and leaves its row,
  Show brings it back, and Appearance's switch does the same; the layout
  survives a reload; Reset puts the default back and is disabled at the
  default; the first-run fixture still hides the attention card under a
  layout that orders it first; no horizontal overflow at 375 with Arrange on.
- `docs/ui-guidelines.md` (the Overview section and Appearance) and
  `docs/api-surface.md` say what the product now does; `make screenshots` for
  the Overview and Appearance captures.

## Out of scope

- Other pages' panels. The preference is per page so they can follow; the
  registry and controls are the Overview's in this pull request.
- Changing a panel's width or column.
- A layout shared between accounts or set by an administrator.
