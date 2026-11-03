# The Overview's layout, the operator's: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a signed-in operator reorder and hide the Overview's panels from an Arrange mode on the page and from Appearance, and keep the layout with their account.

**Architecture:** The layout (`order`, `hidden`) is pure data in `panels.ts`, read forgivingly and applied by one function the route renders from. It travels in the account's preference document as `panels.overview`, validated by the handler as the band document is, and kept by `prefs` beside the band settings with the browser as the fallback. Arrange mode is state in the route: on, each slot carries a small toolbar (grip, Move up, Move down, Hide) above its card, hidden panels appear as a Show row, and a bar at the top holds Reset and Done. The column drag gesture is generalised to any `data-*` slot so the grip reuses it.

**Tech Stack:** Svelte 5 runes, TypeScript, Go `net/http` handler, OpenAPI regenerated with `make openapi`, Node's test runner for units, Playwright on the rebuilt binary (`cd web && npm run build && cd .. && CGO_ENABLED=0 go build -trimpath -o ./zoomies ./cmd/zoomies`; then `cd web && npx playwright test <spec> --project chromium`).

**Spec:** `design/webui-refresh-review/SPEC-layout.md`

**One deviation, stated here for the review:** the spec puts the Arrange controls "at the right of every card's head". The panels' heads belong to seven components with their own `actions` snippets, so the controls are a toolbar the route draws across the top edge of each slot in Arrange mode (`ArrangeControls`), which touches no panel component and still reads as the card's own. Ledgered as a ruling at execution.

## Global Constraints

- No raw colour and no raw value that has a token; no new token and no new dependency.
- Every control in Arrange mode has an accessible name that names the panel; the keyboard can do everything the pointer can.
- Both styles in both modes at 1440, 375 and 320 pixels with no horizontal overflow; 4.5:1 for text, 3:1 for a control's edge.
- The layout is the preference document's: nothing reachable from the UI that the REST API does not expose. Both OpenAPI copies regenerated with `make openapi`, never edited.
- `OVERVIEW_PANELS` stays the only source of panels and ids; `absentWhen` wins over a layout.
- The band, `FleetStatus`, `PageBand` and the punch card are not touched; no panel component's inside changes.
- Every existing Overview behaviour and test is kept.
- British spelling, no em dash, no spaced double hyphen; comments say why; imperative commits; each new behaviour arrives with a test watched to fail.

## Review Focus

1. A saved layout from a newer build that names an id this build lacks, or omits a panel this build has: the unknown id is dropped and the missing panel appended in default order, so nothing is lost or stuck. Pinned in Task 1 (unit).
2. A layout that orders `events` (side) before `jobs` (main): the events panel stands alone on a full-width row of its own column kind, nothing overlaps, and the grid still has no horizontal overflow. Pinned in Task 5.
3. Move down on the last visible panel and Move up on the first: the buttons are disabled with a reason, and a keyboard user is not stranded. Pinned in Task 5.
4. Hiding every panel: the body is a list of Show rows in Arrange mode and empty when Arrange is off; Reset brings them all back. Pinned in Task 5.
5. Two browsers on one account: a layout saved in one is the one the other draws after a reload, and signing out leaves the browser's own copy. Pinned in Task 3 (the account round trip in Go) and Task 5 (reload).

---

### Task 1: The layout as data

**Files:**

- Modify: `web/src/lib/overview/panels.ts`
- Test: `web/unit/overview-panels.test.ts`

**Interfaces:**

- Produces:

  ```ts
  export interface PanelLayout {
    order: OverviewPanelId[];
    hidden: OverviewPanelId[];
  }
  export const OVERVIEW_PANEL_IDS: readonly OverviewPanelId[]; // OVERVIEW_PANELS.map(p => p.id)
  export const DEFAULT_LAYOUT: PanelLayout; // { order: OVERVIEW_PANEL_IDS, hidden: [] }
  export function isPanelId(value: unknown): value is OverviewPanelId;
  export function readPanelLayout(
    raw: unknown,
    panels: readonly OverviewPanel[],
  ): PanelLayout;
  export function visibleLayout(
    panels: readonly OverviewPanel[],
    layout: PanelLayout,
    fleet: { setupPending: boolean },
  ): OverviewPanel[];
  export function hiddenPanels(
    panels: readonly OverviewPanel[],
    layout: PanelLayout,
    fleet: { setupPending: boolean },
  ): OverviewPanel[]; // hidden and not absent, in layout order
  export function movePanel(
    layout: PanelLayout,
    id: OverviewPanelId,
    delta: -1 | 1,
  ): PanelLayout; // swaps with its neighbour in `order`; unchanged at an end
  export function placePanel(
    layout: PanelLayout,
    id: OverviewPanelId,
    before: OverviewPanelId | null,
  ): PanelLayout; // removes id and inserts before `before`, or last when null
  export function setHidden(
    layout: PanelLayout,
    id: OverviewPanelId,
    hidden: boolean,
  ): PanelLayout;
  export function isDefaultLayout(layout: PanelLayout): boolean;
  ```

  `visiblePanels` is removed; Task 5 moves the route onto `visibleLayout`. All functions return new objects.

- [ ] **Step 1: Write the failing tests** (append to the unit file)

```ts
import {
  DEFAULT_LAYOUT,
  hiddenPanels,
  isDefaultLayout,
  movePanel,
  placePanel,
  readPanelLayout,
  setHidden,
  visibleLayout,
} from "../src/lib/overview/panels.ts";

test("a saved layout is read forgivingly: unknown ids dropped, missing panels appended, duplicates collapsed", () => {
  const layout = readPanelLayout(
    {
      order: ["pools", "nope", "attention", "pools"],
      hidden: ["history", "nope"],
    },
    OVERVIEW_PANELS,
  );
  assert.deepEqual(layout.order, [
    "pools",
    "attention",
    "failures",
    "jobs",
    "events",
    "history",
    "diagnostics",
  ]);
  assert.deepEqual(layout.hidden, ["history"]);
  assert.deepEqual(readPanelLayout(undefined, OVERVIEW_PANELS), DEFAULT_LAYOUT);
  assert.deepEqual(
    readPanelLayout({ order: "pools" }, OVERVIEW_PANELS),
    DEFAULT_LAYOUT,
  );
});

test("the visible layout applies the order, drops the hidden, and the first run still wins", () => {
  const layout = {
    order: [
      "history",
      "attention",
      "pools",
      "failures",
      "jobs",
      "events",
      "diagnostics",
    ],
    hidden: ["events"],
  } as const;
  assert.deepEqual(
    visibleLayout(OVERVIEW_PANELS, layout, { setupPending: false }).map(
      (p) => p.id,
    ),
    ["history", "attention", "pools", "failures", "jobs", "diagnostics"],
  );
  assert.deepEqual(
    visibleLayout(OVERVIEW_PANELS, layout, { setupPending: true }).map(
      (p) => p.id,
    ),
    ["pools", "jobs", "diagnostics"],
  );
  assert.deepEqual(
    hiddenPanels(OVERVIEW_PANELS, layout, { setupPending: false }).map(
      (p) => p.id,
    ),
    ["events"],
  );
});

test("a move swaps neighbours and stops at the ends; a place puts a panel before another or last", () => {
  assert.deepEqual(movePanel(DEFAULT_LAYOUT, "pools", -1).order.slice(0, 2), [
    "pools",
    "attention",
  ]);
  assert.deepEqual(movePanel(DEFAULT_LAYOUT, "attention", -1), DEFAULT_LAYOUT);
  assert.deepEqual(movePanel(DEFAULT_LAYOUT, "diagnostics", 1), DEFAULT_LAYOUT);
  assert.deepEqual(
    placePanel(DEFAULT_LAYOUT, "diagnostics", "attention").order[0],
    "diagnostics",
  );
  assert.deepEqual(
    placePanel(DEFAULT_LAYOUT, "attention", null).order.at(-1),
    "attention",
  );
  assert.deepEqual(setHidden(DEFAULT_LAYOUT, "pools", true).hidden, ["pools"]);
  assert.deepEqual(
    setHidden(setHidden(DEFAULT_LAYOUT, "pools", true), "pools", false),
    DEFAULT_LAYOUT,
  );
  assert.equal(isDefaultLayout(DEFAULT_LAYOUT), true);
  assert.equal(
    isDefaultLayout(setHidden(DEFAULT_LAYOUT, "pools", true)),
    false,
  );
});
```

- [ ] **Step 2: Run them to see them fail.** Run: `cd web && node --import ./unit/register.mjs --test unit/overview-panels.test.ts`. Expected: FAIL, `readPanelLayout` is not exported.
- [ ] **Step 3: Implement the interface in `panels.ts`; delete `visiblePanels` and the test that named it (its assertions move into the visible-layout test above, with `DEFAULT_LAYOUT`).** `readPanelLayout` filters with `isPanelId`, dedupes keeping first, appends missing in `panels` order; `hidden` likewise filtered and deduped.
- [ ] **Step 4: Run them to see them pass.** Expected: `# pass 5` (the two existing tests plus three).
- [ ] **Step 5: Commit.** `git add web/src/lib/overview/panels.ts web/unit/overview-panels.test.ts && git commit -m "Read, order and hide the Overview's panels as a layout"`

### Task 2: The preference on the server

**Files:**

- Create: `internal/api/panel_preferences.go`
- Modify: `internal/api/handlers_auth.go:316-324` (the `userPreferences` struct) and `:371` (validation)
- Modify: `api/openapi.yaml` (`UserPreferences.properties.panels`; new `PanelPreferences`, `PanelLayout`, `OverviewPanelId` schemas beside `BandPreferences`)
- Modify: `docs/api-surface.md:95-96`
- Test: `internal/api/auth_test.go`

**Interfaces:**

- Produces: `type panelLayout struct { Order []string; Hidden []string }` (`json:"order,omitempty"`, `json:"hidden,omitempty"`); `type panelPreferences map[string]panelLayout`; `func (in panelPreferences) validate(field string) []fieldError`; `var panelPages = map[string][]string{"overview": {"attention", "pools", "failures", "jobs", "events", "history", "diagnostics"}}`; `Panels panelPreferences` on `userPreferences` as `json:"panels,omitempty"`. OpenAPI: `OverviewPanelId` enum of those seven; `PanelLayout` with `order` and `hidden` arrays of `OverviewPanelId`, `maxItems: 12`; `PanelPreferences` with `additionalProperties: PanelLayout`. The generated client gains `Schemas['PanelPreferences']` and `Schemas['PanelLayout']`.

- [ ] **Step 1: Write the failing tests** in `auth_test.go`, after `TestBandPreferencesRefuseAPageOrCutThisBuildDoesNotHave`:

```go
func TestPanelLayoutsFollowTheSignedInAccount(t *testing.T) {
	h := newHarness(t)
	_, aliceCookie := h.user("alice", store.RoleViewer)
	_, bobCookie := h.user("bob", store.RoleViewer)
	put := h.do(request{method: http.MethodPut, path: "/api/v1/auth/preferences", cookie: aliceCookie,
		body: map[string]any{"panels": map[string]any{"overview": map[string]any{
			"order": []string{"pools", "attention"}, "hidden": []string{"history"}}}}})
	put.mustStatus(t, http.StatusOK, "save a panel layout")
	get := h.do(request{method: http.MethodGet, path: "/api/v1/auth/preferences", cookie: aliceCookie})
	get.mustStatus(t, http.StatusOK, "read it back")
	var doc map[string]any
	get.into(t, &doc)
	panels, _ := doc["panels"].(map[string]any)
	overview, _ := panels["overview"].(map[string]any)
	if order, _ := overview["order"].([]any); len(order) != 2 || order[0] != "pools" {
		t.Fatalf("order = %v, want pools first", overview["order"])
	}
	other := h.do(request{method: http.MethodGet, path: "/api/v1/auth/preferences", cookie: bobCookie})
	other.mustStatus(t, http.StatusOK, "another account")
	var bob map[string]any
	other.into(t, &bob)
	if _, has := bob["panels"]; has {
		t.Fatalf("bob sees alice's layout: %v", bob["panels"])
	}
}

func TestPanelLayoutsRefuseAPageOrPanelThisBuildDoesNotHave(t *testing.T) {
	h := newHarness(t)
	_, cookie := h.user("alice", store.RoleViewer)
	cases := []struct{ name string; panels map[string]any; field string }{
		{"a page with no panels", map[string]any{"settings": map[string]any{"order": []string{"a"}}}, "panels.settings"},
		{"a panel the Overview does not have", map[string]any{"overview": map[string]any{"order": []string{"pools", "weather"}}}, "panels.overview.order"},
		{"a hidden panel the Overview does not have", map[string]any{"overview": map[string]any{"hidden": []string{"weather"}}}, "panels.overview.hidden"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := h.do(request{method: http.MethodPut, path: "/api/v1/auth/preferences", cookie: cookie,
				body: map[string]any{"panels": tc.panels}})
			resp.mustStatus(t, http.StatusUnprocessableEntity, tc.name)
			var env errorEnvelope
			resp.into(t, &env)
			if len(env.Errors) == 0 || env.Errors[0].Field != tc.field {
				t.Fatalf("errors = %+v, want one on %s", env.Errors, tc.field)
			}
		})
	}
}
```

- [ ] **Step 2: Run them to see them fail.** Run: `make build-nogui >/dev/null && go test -count=1 -run 'TestPanelLayouts' ./internal/api/`. Expected: FAIL, the first on `order = <nil>` (the field is dropped), the second on status 200.
- [ ] **Step 3: Write `panel_preferences.go` (a header comment in the band file's voice: the page and panel ids are the UI's list, here so a document naming one this build cannot draw is refused with a sentence), add `Panels` to `userPreferences`, and the `validate` call after the bands' with the message `a panel layout names a page or panel this build does not have`.**
- [ ] **Step 4: Run them to see them pass.** Expected: ok.
- [ ] **Step 5: OpenAPI and docs.** Add the three schemas and the property; `make openapi`; the two `docs/api-surface.md` rows gain "and how its panels are arranged under `panels` (per page, `order` and `hidden`)" and "page or panel" in the 422 sentence. Run `go test -count=1 ./internal/api/ ./internal/docs/` (the embedded copy test and the surface doc test). Expected: ok.
- [ ] **Step 6: Commit.** `git add internal/api api/openapi.yaml web/src/lib/api docs/api-surface.md && git commit -m "Keep an account's panel layout in its preference document"`

### Task 3: The preference on the client

**Files:**

- Modify: `web/src/lib/state/prefs.svelte.ts` (StoredPrefs, the private fields, `load`/constructor, `syncAccount`, `#persist`, `#scheduleAccountSave`)
- Test: `web/unit/overview-panels.test.ts` (none new; the reader is Task 1's), `web/tests/overview-arrange.spec.ts` (Task 5 holds the round trip through the UI)

**Interfaces:**

- Consumes: `PanelLayout`, `readPanelLayout`, `DEFAULT_LAYOUT`, `OVERVIEW_PANELS` from Task 1; `Schemas['PanelPreferences']` from Task 2.
- Produces on `prefs`: `panelLayout(page: 'overview'): PanelLayout`; `setPanelLayout(page: 'overview', layout: PanelLayout): void` (persists and schedules the account save); `resetPanelLayout(page: 'overview'): void`. `StoredPrefs.panelLayouts?: Record<string, unknown>`. The account document carries `panels: { overview: layout }` only when the layout is not the default, as the bands carry overrides only; `syncAccount` takes `remote.panels` when present, clears to default when a different previous account's browser copy would otherwise carry over, as the bands do.

- [ ] **Step 1: There is no unit seam for the class; the behaviour is held in Task 5's Playwright tests ("survives a reload", "a signed-out browser keeps its own"). Write the code against the band pattern in the same file.**
- [ ] **Step 2: `cd web && npm run check && npm run lint`.** Expected: clean.
- [ ] **Step 3: Commit.** `git add web/src/lib/state/prefs.svelte.ts && git commit -m "Carry the Overview's panel layout with the account, the browser as the fallback"`

### Task 4: A drag for any slot

**Files:**

- Modify: `web/src/lib/actions/columnGesture.ts`
- Test: `web/unit/slot-gesture.test.ts` (new; Node, no DOM: the pure target-finding helper only)

**Interfaces:**

- Produces: `export interface SlotDragHandlers { over(id: string): void; drop(id: string): void; cancel(): void }` (the same shape as `ColumnDragHandlers`, which becomes an alias); `export function startSlotDrag(event: PointerEvent, root: HTMLElement, attribute: string, handlers: SlotDragHandlers): void`, the body of today's `startColumnDrag` with `columnAt` generalised to `slotAt(root, attribute, x, y)` reading `[data-<attribute>]` via `closest` and `dataset`; `startColumnDrag` becomes `startSlotDrag(event, root, 'table-column', handlers)`. `export function slotAt(root: HTMLElement, attribute: string, x: number, y: number): string` is exported for the unit test.

- [ ] **Step 1: Write the failing test**: a unit test that calls `slotAt` with a fake `root` and a stubbed `document.elementsFromPoint` returning elements whose `closest('[data-panel]')` yields `{ dataset: { panel: 'pools' } }`, asserting `'pools'`, and `''` when the element is outside `root` (`root.contains` false). Run: `cd web && node --import ./unit/register.mjs --test unit/slot-gesture.test.ts`. Expected: FAIL, `slotAt` is not exported.
- [ ] **Step 2: Generalise the module; `startColumnDrag` keeps its signature.** Run the unit test (`# pass 1`) and `npx playwright test tests/column-layout.spec.ts --project chromium --project mobile` on the rebuilt binary. Expected: all pass (the grid's drag is unchanged).
- [ ] **Step 3: Commit.** `git add web/src/lib/actions/columnGesture.ts web/unit/slot-gesture.test.ts && git commit -m "Let any slot be dragged by the gesture the grid's columns use"`

### Task 5: Arrange mode on the Overview

**Files:**

- Create: `web/src/lib/overview/ArrangeControls.svelte`, `web/src/lib/overview/ArrangeBar.svelte`, `web/src/lib/overview/HiddenPanel.svelte`
- Modify: `web/src/routes/Overview.svelte`
- Test: `web/tests/overview-arrange.spec.ts` (new)

**Interfaces:**

- Consumes: Task 1's functions; `prefs.panelLayout`, `setPanelLayout`, `resetPanelLayout` from Task 3; `startSlotDrag` from Task 4; `IconButton`, `Button`.
- Produces: `ArrangeControls` props `{ title: string; first: boolean; last: boolean; onmove: (delta: -1 | 1) => void; onhide: () => void; ondrag: (event: PointerEvent) => void }`, rendering a `<div class="arrange" role="toolbar" aria-label="Arrange {title}">` with a grip `<button class="grip" aria-label="Move {title}" onpointerdown={ondrag}>` (`GripVertical`), `IconButton`s `Move {title} up` (`ChevronUp`, `disabled={first}`, `reason="It is already first."`), `Move {title} down` (`ChevronDown`, `disabled={last}`, `reason="It is already last."`) and `Hide {title}` (`EyeOff`). `ArrangeBar` props `{ hidden: number; isDefault: boolean; onreset: () => void; ondone: () => void }`: `<section role="region" aria-label="Arrange the Overview">` with the sentence `Arrange the Overview. Drag a card by its grip, or use its buttons. {n} hidden.` (`pluralise(n, 'panel')` + ' hidden', or 'Nothing hidden.'), `Button` "Reset layout" (`disabled={isDefault}`, secondary) and `Button` "Done" (primary). `HiddenPanel` props `{ title: string; onshow: () => void }`: a row `{title} is hidden.` with `Button` size sm "Show" (`ariaLabel="Show {title}"`).
  In `Overview.svelte`: `let arranging = $state(false)`; the header's actions gain `<Button variant="secondary" aria-pressed={arranging} onclick={() => (arranging = !arranging)}>Arrange</Button>` before the scope select; `const layout = $derived(prefs.panelLayout('overview'))`; `const panels = $derived(visibleLayout(OVERVIEW_PANELS, layout, { setupPending }))`; `const hidden = $derived(hiddenPanels(...))`; in Arrange mode the body renders `ArrangeBar`, then each visible slot with `ArrangeControls` above the card (slot `data-drop={dropTarget === panel.id}`, class `.dragging` on the dragged), then a `HiddenPanel` row per hidden panel in a `.slot.full`. Drag: `startSlotDrag(event, body, 'panel', { over, drop, cancel })`, `drop(id)` → `setPanelLayout('overview', placePanel(layout, dragged, id || null))`. Focus after a move: `tick()` then focus the same button in the moved slot (query `[data-panel="{id}"] .arrange button[aria-label="Move {title} {up|down}"]`).

- [ ] **Step 1: Write the failing tests** in `web/tests/overview-arrange.spec.ts`:

```ts
import { expect, test } from '@playwright/test';
import { goto, reload } from './support/fixtures';

const ORDER = ['attention', 'pools', 'failures', 'jobs', 'events', 'history', 'diagnostics'];
const order = (page) => page.locator('[data-panel]').evaluateAll((n) => n.map((e) => e.getAttribute('data-panel')));

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.removeItem('zoomies.prefs'));
  await goto(page, '/', 'Overview');
});

test('Arrange shows the controls on every card and Done puts them away', async ({ page }) => {
  await expect(page.getByRole('toolbar', { name: /^Arrange / })).toHaveCount(0);
  await page.getByRole('button', { name: 'Arrange' }).click();
  await expect(page.getByRole('region', { name: 'Arrange the Overview' })).toBeVisible();
  await expect(page.getByRole('toolbar', { name: /^Arrange / })).toHaveCount(7);
  await expect(page.getByRole('button', { name: 'Move Needs attention up' })).toHaveAttribute('aria-disabled', 'true');
  await expect(page.getByRole('button', { name: 'Move Diagnostics down' })).toHaveAttribute('aria-disabled', 'true');
  await page.getByRole('button', { name: 'Done' }).click();
  await expect(page.getByRole('toolbar', { name: /^Arrange / })).toHaveCount(0);
});

test('Move down then Move up restores the order, with focus staying on the button', async ({ page }) => {
  await page.getByRole('button', { name: 'Arrange' }).click();
  await page.getByRole('button', { name: 'Move Pools down' }).click();
  expect(await order(page)).toEqual(['attention', 'failures', 'pools', 'jobs', 'events', 'history', 'diagnostics']);
  await expect(page.getByRole('button', { name: 'Move Pools down' })).toBeFocused();
  await page.getByRole('button', { name: 'Move Pools up' }).click();
  expect(await order(page)).toEqual(ORDER);
});

test('a hidden panel leaves a Show row while arranging and nothing when done, and the layout survives a reload', async ({ page }) => {
  await page.getByRole('button', { name: 'Arrange' }).click();
  await page.getByRole('button', { name: 'Hide History' }).click();
  await expect(page.getByText('History is hidden.')).toBeVisible();
  await expect(page.locator('#history')).toHaveCount(0);
  await expect(page.getByRole('region', { name: 'Arrange the Overview' })).toContainText('1 panel hidden');
  await page.getByRole('button', { name: 'Done' }).click();
  await expect(page.getByText('History is hidden.')).toHaveCount(0);
  await reload(page, 'Overview');
  await expect(page.locator('#history')).toHaveCount(0);
  await page.getByRole('button', { name: 'Arrange' }).click();
  await page.getByRole('button', { name: 'Show History' }).click();
  await expect(page.locator('#history')).toHaveCount(1);
});

test('Reset layout puts the default back and is disabled at the default', async ({ page }) => {
  await page.getByRole('button', { name: 'Arrange' }).click();
  await expect(page.getByRole('button', { name: 'Reset layout' })).toHaveAttribute('aria-disabled', 'true');
  await page.getByRole('button', { name: 'Move Pools down' }).click();
  await page.getByRole('button', { name: 'Hide History' }).click();
  await page.getByRole('button', { name: 'Reset layout' }).click();
  expect(await order(page)).toEqual(ORDER);
  await expect(page.getByRole('button', { name: 'Reset layout' })).toHaveAttribute('aria-disabled', 'true');
});

test('the events panel moved above the jobs stands alone, with no horizontal overflow', async ({ page }) => {
  await page.getByRole('button', { name: 'Arrange' }).click();
  await page.getByRole('button', { name: 'Move Recent events up' }).click();
  expect((await order(page)).slice(3, 5)).toEqual(['events', 'jobs']);
  expect(await page.evaluate(() => document.documentElement.scrollWidth - innerWidth)).toBeLessThanOrEqual(0);
});

test('hiding every panel leaves Show rows, and Reset brings them all back', async ({ page }) => {
  await page.getByRole('button', { name: 'Arrange' }).click();
  for (const title of ['Needs attention', 'Pools', 'Recent failures', 'Active jobs', 'Recent events', 'History', 'Diagnostics'])
    await page.getByRole('button', { name: `Hide ${title}` }).click();
  await expect(page.getByRole('button', { name: /^Show / })).toHaveCount(7);
  await page.getByRole('button', { name: 'Reset layout' }).click();
  expect(await order(page)).toEqual(ORDER);
});

test('a card can be dragged by its grip to a new place', async ({ page }) => {
  await page.getByRole('button', { name: 'Arrange' }).click();
  // The touch helper from column-layout.spec.ts: pointerdown on the grip, moves to the centre of the target slot, pointerup.
  ...
  expect((await order(page)).slice(0, 2)).toEqual(['pools', 'attention']);
});
```

The drag test copies `touchDrag` and `centre` from `column-layout.spec.ts` into this spec (or lifts them into `tests/support/gestures.ts` and imports from both).

- [ ] **Step 2: Run them to see them fail.** Build, then `npx playwright test tests/overview-arrange.spec.ts --project chromium`. Expected: FAIL, no button named Arrange.
- [ ] **Step 3: Write the three components and the route changes.** Styles: the slot in Arrange mode has a dashed `--z-border-strong` outline at `--z-radius-xl`; the toolbar sits above the card with `--z-space-2` gap, `--z-surface-raised` ground, `--z-radius-full`; the drop target slot draws a `--z-border-width-thick` top edge in `--z-accent`; the dragged slot is at `opacity: 0.6`. The hidden row is a `Panel`-less card: hairline, `--z-radius-xl`, `--z-surface`, text muted.
- [ ] **Step 4: Run the new spec, then `overview.spec.ts`, `calm-overview.spec.ts`, `overview-columns.spec.ts` on chromium and mobile on the rebuilt binary.** Expected: all pass (the existing tests never enter Arrange mode).
- [ ] **Step 5: Commit.** `git add web/src/lib/overview/ArrangeControls.svelte web/src/lib/overview/ArrangeBar.svelte web/src/lib/overview/HiddenPanel.svelte web/src/routes/Overview.svelte web/tests/overview-arrange.spec.ts && git commit -m "Let the Overview be arranged: move, drag and hide its panels"`

### Task 6: Appearance

**Files:**

- Modify: `web/src/lib/settings/AppearancePanel.svelte` (a `.setting` block after Page bands)
- Test: `web/tests/overview-arrange.spec.ts` (one test)

**Interfaces:**

- Consumes: `prefs.panelLayout('overview')`, `setPanelLayout`, `resetPanelLayout`, `setHidden`, `isDefaultLayout`, `OVERVIEW_PANELS`.

- [ ] **Step 1: Write the failing test**:

```ts
test("Appearance lists the Overview's panels as switches, and one switched off is hidden on the page", async ({
  page,
}) => {
  await goto(page, "/settings/appearance", "Appearance");
  const group = page.getByRole("group", { name: "Overview panels" });
  await expect(group.getByRole("switch")).toHaveCount(7);
  await group.getByRole("switch", { name: "History on the Overview" }).click();
  await goto(page, "/", "Overview");
  await expect(page.locator("#history")).toHaveCount(0);
  await goto(page, "/settings/appearance", "Appearance");
  await page
    .getByRole("group", { name: "Overview panels" })
    .getByRole("button", { name: "Reset layout" })
    .click();
  await goto(page, "/", "Overview");
  await expect(page.locator("#history")).toHaveCount(1);
});
```

- [ ] **Step 2: Run it to see it fail.** Expected: FAIL, no group named Overview panels.
- [ ] **Step 3: Add the block**: label "Overview panels", description "Which panels the Overview draws, in the order you gave them. Change the order on the Overview itself with Arrange. These choices follow your signed-in account, as the bands do.", a `role="group"` list of `Switch` rows in `layout.order` (label `{title} on the Overview`, checked when not hidden), and a `Reset layout` button disabled at the default.
- [ ] **Step 4: Run the spec; `npm run check && npm run lint`.** Expected: pass; clean.
- [ ] **Step 5: Commit.** `git add web/src/lib/settings/AppearancePanel.svelte web/tests/overview-arrange.spec.ts && git commit -m "Offer the Overview's panels on Appearance, a switch each"`

### Task 7: Docs, the whole run and the captures

**Files:**

- Modify: `docs/ui-guidelines.md` (the Overview section's "The body is a list of panels" bullet gains the layout and Arrange mode; Appearance's description gains the Overview panels setting)
- Modify: `docs/screenshots/overview-*.webp`, `appearance-*.webp` via `make screenshots` in both styles

- [ ] **Step 1: `cd web && npm run lint && npm run check && npm run test:unit`; `make lint`.** Expected: clean.
- [ ] **Step 2: Docs; `go test -count=1 ./internal/docs/`.** Expected: ok.
- [ ] **Step 3: `npx playwright test tests/overview-arrange.spec.ts tests/overview.spec.ts tests/calm-overview.spec.ts tests/page-bands.spec.ts tests/settings.spec.ts tests/a11y.spec.ts tests/column-layout.spec.ts --project chromium --project mobile`; `go test -count=1 ./internal/api/`; then the whole Playwright suite in the background to a log.** Expected: all pass.
- [ ] **Step 4: `make screenshots` both styles; `git clean -fq docs/screenshots/mission-control`; photographs of Arrange mode at 1440 and 375 for the pull request.**
- [ ] **Step 5: Commit the docs and captures; audit; push; pull request body with results.** `git commit -m "Say how the Overview is arranged, and recapture it"`
