# The Overview from its mock: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restructure the Overview's body below the band into the mock's card grammar, rendered from a registry of panels a later pull request can reorder and hide.

**Architecture:** A pure module `web/src/lib/overview/panels.ts` lists the body's panels and holds the two caps (worst three problems, six pools) as functions a unit test can hold. `Overview.svelte` renders the list into one grid. `Panel` gains an icon tile so every card has the mock's head without a second card component. Two panels change shape (Needs attention becomes a card of rows, the pools become cards); the rest take the tile and keep their behaviour and tests.

**Tech Stack:** Svelte 5 runes, TypeScript, Node's test runner for units (`npm run test:unit`), Playwright against the built binary (`npx playwright test <spec> --project chromium`, after `cd web && npm run build && cd .. && CGO_ENABLED=0 go build -trimpath -o ./zoomies ./cmd/zoomies`).

**Spec:** `design/webui-refresh-review/SPEC-overview.md`

## Global Constraints

- No raw colour and no raw value that has a token; no new token; no new dependency.
- Status colours keep their fixed mapping; a pill never carries a state by colour alone.
- Both styles in both modes at 1440, 375 and 320 pixels with no horizontal overflow; 4.5:1 for text, 3:1 for a control's edge.
- Three attention rows and a slim band keep the pools above the fold of 1440x900.
- Every existing Overview behaviour and test is kept; a test whose selector changes is re-pointed, never deleted.
- `FleetStatus`, `PageBand`, `BrandStage` and the punch card are not touched.
- British spelling, no em dash, no spaced double hyphen; comments say why; imperative commit messages; every new behaviour arrives with a test watched to fail.
- The gallery imports from `$lib/components` only, so the Overview's own components do not appear in it; the tile does, on `Panel`.

## Review Focus

1. A fleet with more than six enabled pools: six cards and "and N more pools" linking to `/pools`, with the pinned pools among the six. Pinned in Task 1 (unit) and Task 4.
2. More than three active problems: three rows worst first and "and N more" that opens the drawer; exactly three: "Open the drawer"; none: the quiet line, no card. Pinned in Task 1 (unit) and Task 3.
3. A problem whose target has no page (`target()` returns null): the row has no Open link and still reads. Pinned in Task 3.
4. A pool name of 60 unbroken characters inside a card at 375px: wraps, the card does not widen the page. Pinned in Task 4.
5. The first-run fixture (nothing configured): no attention card, no failures, the checklist first, the pools' empty state. Pinned in Task 5.

---

### Task 1: The registry and the two caps

**Files:**

- Create: `web/src/lib/overview/panels.ts`
- Test: `web/unit/overview-panels.test.ts`

**Interfaces:**

- Produces:

  ```ts
  export type OverviewPanelId =
    | "attention"
    | "pools"
    | "failures"
    | "jobs"
    | "events"
    | "history"
    | "diagnostics";
  export interface OverviewPanel {
    id: OverviewPanelId;
    title: string;
    place: "full" | "main" | "side";
    absentWhen?: (fleet: { setupPending: boolean }) => boolean;
  }
  export const OVERVIEW_PANELS: readonly OverviewPanel[]; // attention, pools, failures, jobs (main), events (side), history, diagnostics
  export function visiblePanels(
    panels: readonly OverviewPanel[],
    fleet: { setupPending: boolean },
  ): OverviewPanel[];
  export const ATTENTION_SHOWN = 3;
  export const POOLS_SHOWN = 6;
  export function cap<T>(
    items: readonly T[],
    limit: number,
  ): { shown: T[]; more: number };
  ```

  `attention` and `failures` are absent when `setupPending`. `cap` keeps the first `limit` in the order given; `more` is the count left.

- [ ] **Step 1: Write the failing tests**

```ts
// web/unit/overview-panels.test.ts
import { test } from "node:test";
import assert from "node:assert/strict";
import {
  ATTENTION_SHOWN,
  OVERVIEW_PANELS,
  POOLS_SHOWN,
  cap,
  visiblePanels,
} from "../src/lib/overview/panels.ts";

test("every panel has a unique id and a side panel follows a main one", () => {
  const ids = OVERVIEW_PANELS.map((p) => p.id);
  assert.equal(new Set(ids).size, ids.length);
  OVERVIEW_PANELS.forEach((p, i) => {
    if (p.place === "side")
      assert.equal(
        OVERVIEW_PANELS[i - 1]?.place,
        "main",
        `${p.id} has no main panel before it`,
      );
  });
  assert.deepEqual(ids, [
    "attention",
    "pools",
    "failures",
    "jobs",
    "events",
    "history",
    "diagnostics",
  ]);
});

test("the attention card and the failures stand down during the first run", () => {
  const ids = visiblePanels(OVERVIEW_PANELS, { setupPending: true }).map(
    (p) => p.id,
  );
  assert.deepEqual(ids, ["pools", "jobs", "events", "history", "diagnostics"]);
  assert.equal(
    visiblePanels(OVERVIEW_PANELS, { setupPending: false }).length,
    OVERVIEW_PANELS.length,
  );
});

test("a cap keeps the first few in the order given and counts the rest", () => {
  assert.deepEqual(cap([1, 2, 3, 4, 5], 3), { shown: [1, 2, 3], more: 2 });
  assert.deepEqual(cap([1, 2, 3], 3), { shown: [1, 2, 3], more: 0 });
  assert.deepEqual(cap([], 3), { shown: [], more: 0 });
  assert.equal(ATTENTION_SHOWN, 3);
  assert.equal(POOLS_SHOWN, 6);
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd web && node --import ./unit/register.mjs --test unit/overview-panels.test.ts`
Expected: FAIL, cannot find module `../src/lib/overview/panels.ts`

- [ ] **Step 3: Write `panels.ts`**

A module comment saying why the body is a list (one grid, one order, and a later pull request reads a saved order over it). `visiblePanels` filters by `absentWhen`.

- [ ] **Step 4: Run them to see them pass**

Run: the same command. Expected: `# pass 3`.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/overview/panels.ts web/unit/overview-panels.test.ts
git commit -m "List the Overview's panels as data, with the caps the cards keep"
```

### Task 2: The tile on a Panel

**Files:**

- Modify: `web/src/lib/components/Panel.svelte` (props and header)
- Modify: `web/src/gallery/sections/Surfaces.svelte` (one Panel with an icon in the existing "Panel" examples)
- Test: `web/tests/gallery.spec.ts`

**Interfaces:**

- Produces: `Panel` prop `icon?: typeof import('@lucide/svelte').Boxes` (the Lucide component type, `LucideIcon` from `$lib/feed/entries` is the same type: use `import type { Component } from 'svelte'` as `Panel`'s `icon?: Component<{ size?: number | string; 'aria-hidden'?: boolean | 'true' }>` only if `@lucide/svelte` exports no `Icon` type; check `grep -n "export type Icon\b" web/node_modules/@lucide/svelte/dist/*.d.ts` and prefer its export). Drawn as `<span class="tile" aria-hidden="true">` before `.titles`: `--z-space-10` square, `--z-surface-sunken` ground, hairline `--z-border-strong`, `--z-radius-md`, icon at 18.

- [ ] **Step 1: Write the failing test** in `gallery.spec.ts`, after the terminal tests:

```ts
// A panel with an icon draws it in a tile beside the title: the Overview's
// cards share one head, so no page draws its own.
test("a panel with an icon draws it in a tile beside the title", async ({
  page,
}) => {
  await open(page, "mission-control", "light");
  const panel = page
    .locator(".panel", { hasText: "Panel with a tile" })
    .first();
  const tile = panel.locator("header .tile");
  await expect(tile).toBeVisible();
  const [w, h, radius] = await tile.evaluate((el) => {
    const r = el.getBoundingClientRect();
    return [r.width, r.height, parseFloat(getComputedStyle(el).borderRadius)];
  });
  expect(w).toBe(h);
  expect(radius).toBeGreaterThan(0);
  await expect(tile).toHaveAttribute("aria-hidden", "true");
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `cd web && npx playwright test tests/gallery.spec.ts --project gallery -g "tile beside the title"`
Expected: FAIL, no element with text "Panel with a tile".

- [ ] **Step 3: Add the prop and the tile to `Panel.svelte`; add `<Panel title="Panel with a tile" icon={Boxes} description="The Overview's cards carry one.">` to the Surfaces section's Panel examples**

The header's `.tile` sits first in the flex row, `align-self: flex-start`; the `.titles` keep `min-width: 0`.

- [ ] **Step 4: Run the gallery spec whole**

Run: `cd web && npx playwright test tests/gallery.spec.ts --project gallery`
Expected: all pass (24).

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/components/Panel.svelte web/src/gallery/sections/Surfaces.svelte web/tests/gallery.spec.ts
git commit -m "Let a panel carry an icon in a tile beside its title"
```

### Task 3: Needs attention as a card of rows

**Files:**

- Create: `web/src/lib/overview/NeedsAttention.svelte`
- Modify: `web/src/lib/problems/targets.ts` (move `target()` out of `ProblemItem.svelte` as `problemTarget`)
- Modify: `web/src/lib/problems/ProblemItem.svelte` (call `problemTarget`)
- Test: `web/tests/overview.spec.ts` (re-point one test, add two)

**Interfaces:**

- Consumes: `ATTENTION_SHOWN`, `cap` from Task 1; `notifications.groups` (worst group first), `notifications.active`, `notifications.open`; `severityStatus` from `$lib/status`; `RemedyText` as `ProblemItem` uses it; `Panel` with `icon={TriangleAlert}` from Task 2.
- Produces: `problemTarget(p: Problem): { href: string; label: string } | null` in `targets.ts` (the body of `ProblemItem`'s `target`, unchanged, with `hostTarget`, `controllerTarget` and `kennelListHref` imported there). `NeedsAttention` props `{ loading?: boolean; setupPending?: boolean; class?: string }`; it renders `ProblemsSummary` (unchanged) when `loading` or when `notifications.active.length === 0`, and otherwise a `Panel` titled "Needs attention" with `description="Worst first, with the fix the controller suggests."`, rows `<ul class="rows"><li>` for `cap(flattened groups, ATTENTION_SHOWN).shown`: `<Badge status={severityStatus(p.severity)} size="sm" />`, `<p class="title">{p.title}</p>`, `{#if p.fix}<p class="fix"><RemedyText text={p.fix} /></p>{/if}`, and `{#if problemTarget(p)}<a class="open" href={t.href}>Open</a>{/if}` with `aria-label="{t.label}: {p.title}"`. The actions snippet: a `<button type="button" class="more">` reading `and {more} more` when `more > 0`, else `Open the drawer`, `onclick={() => (notifications.open = true)}`.

- [ ] **Step 1: Re-point and add the tests**

Replace the body of `the Overview says how much needs a person without spending the page on it` so it holds the new rule: the card lists at most three, fixes included, and the band's Review button is still on the first screen:

```ts
test("the Overview says what needs a person in three rows at most, and the rest is one click away", async ({
  page,
}) => {
  const card = page.getByRole("region", {
    name: "Needs attention",
    exact: true,
  });
  await expect(card).toBeVisible();
  const rows = card.getByRole("listitem");
  await expect(rows.first()).toBeVisible();
  expect(await rows.count()).toBeLessThanOrEqual(3);
  // The worst first: the fixture seeds errors, so the first row is one.
  await expect(rows.first()).toContainText("Error");
  // The fix is on the row now, where the drawer had it alone.
  await expect(rows.first().locator(".fix")).not.toBeEmpty();
  // The rest are behind the drawer, which the card's own action opens.
  const problems = await page.request
    .get("/api/v1/problems")
    .then((r) => r.json());
  const active = (problems.items ?? []).length;
  const action = card.getByRole("button", {
    name: active > 3 ? `and ${active - 3} more` : "Open the drawer",
  });
  await expect(action).toBeVisible();
  await action.click();
  await expect(page.getByRole("dialog", { name: "Problems" })).toBeVisible();
  await page.keyboard.press("Escape");
  const viewport = page.viewportSize();
  if ((viewport?.width ?? 0) >= 1180) {
    const box = await page
      .getByRole("button", { name: "Review" })
      .boundingBox();
    expect(
      box?.y ?? Infinity,
      "the way in is within the first screen",
    ).toBeLessThan(viewport?.height ?? 0);
  }
});

test("a problem row opens the page that holds the thing", async ({ page }) => {
  const card = page.getByRole("region", {
    name: "Needs attention",
    exact: true,
  });
  const open = card.getByRole("link", { name: /^Open/ }).first();
  await expect(open).toBeVisible();
  await expect(open).toHaveAttribute("href", /^\//);
});
```

Keep the drawer tests that follow as they are: they open the drawer from the top bar.

- [ ] **Step 2: Run them to see them fail**

Run: `cd web && npm run build && cd .. && CGO_ENABLED=0 go build -trimpath -o ./zoomies ./cmd/zoomies && cd web && npx playwright test tests/overview.spec.ts --project chromium -g "three rows at most|holds the thing"`
Expected: FAIL, no region "Needs attention".

- [ ] **Step 3: Move `target()` to `targets.ts` as `problemTarget`; write `NeedsAttention.svelte`; in `Overview.svelte` replace `<ProblemsSummary {loading} {setupPending} />` (inside `.problems`) with `<NeedsAttention {loading} {setupPending} />` and drop the `.problems.in-hero` display rule, since the card is the mock's and stands whether or not the band says the count**

The row: a grid `auto minmax(0, 1fr) auto`, `gap: var(--z-space-3)`, `padding: var(--z-space-3) var(--z-space-5)`, hairline between rows; `.fix` in `--z-text-muted`, `--z-text-sm`; `.open` a `Button variant="secondary" size="sm"` rendered as a link (`href`), so it is a pill like every other button. The card's edge carries the worst severity: `data-severity` on the Panel's class via a wrapper `div.attention[data-severity]` with `border-left: var(--z-border-width-rail) solid var(--z-danger | --z-pending)` on the panel, as the summary line draws today.

- [ ] **Step 4: Run the two tests, then the whole overview spec on the rebuilt binary**

Run: the build commands, then `npx playwright test tests/overview.spec.ts --project chromium`
Expected: all pass. The drawer tests still pass since the top bar's bell is unchanged.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/overview/NeedsAttention.svelte web/src/lib/problems/targets.ts web/src/lib/problems/ProblemItem.svelte web/src/routes/Overview.svelte web/tests/overview.spec.ts
git commit -m "Show the worst three problems on the Overview as rows with their fix"
```

### Task 4: The pools as cards

**Files:**

- Modify: `web/src/lib/overview/PoolUtilisation.svelte`
- Test: `web/tests/overview.spec.ts` (re-point `per-pool utilisation`, add one), `web/unit/overview-panels.test.ts` (none; the cap is Task 1's)

**Interfaces:**

- Consumes: `POOLS_SHOWN`, `cap` from Task 1; `Panel` with `icon={Boxes}`.
- Produces: the same `region` named "Pools" (the `Panel`'s heading), each pool a `listitem` holding the pool's name link, `1–8 runners`, the badges as today, a facts pair `Runners {live} of {max}` and `Waiting {queued}` (`{queued} and nowhere to run` when pinned), the `UtilisationBar` and capacity line as today; after six, `<p class="more"><a href="/pools">and N more pools</a></p>`. The header keeps `View all` and the `at the ceiling` badge; the description reads `{n} enabled` (`pluralise(enabled, 'pool')` + ' enabled').

- [ ] **Step 1: Re-point and add the tests**

In `per-pool utilisation shows both pools and marks the one at its ceiling` the `row` locator stays `pools.getByRole('listitem').filter({ hasText: FIXTURE.linuxPool })`; add after the ceiling branch:

```ts
// A card says its runners and its waiting work as a pair of facts, so the
// one at its ceiling can say where the waiting work stands.
await expect(row).toContainText(/Runners\s*\d+ of \d+/);
await expect(row).toContainText(
  atCeiling ? /Waiting\s*\d+ and nowhere to run/ : /Waiting\s*\d+/,
);
```

And a new test:

```ts
test("the pool cards fit a phone with a long name and never widen the page", async ({
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 800 });
  const pools = page.getByRole("region", { name: "Pools", exact: true });
  await expect(pools.getByRole("listitem").first()).toBeVisible();
  const columns = await pools
    .locator("ul")
    .first()
    .evaluate(
      (el) => getComputedStyle(el).gridTemplateColumns.split(" ").length,
    );
  expect(columns).toBe(1);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth - innerWidth,
    ),
  ).toBeLessThanOrEqual(0);
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `npx playwright test tests/overview.spec.ts --project chromium -g "per-pool|fit a phone"` (rebuilt binary)
Expected: FAIL on `Runners 8 of 8` (the row says `8 / 8 live / max`) and on the column count (the rows are a list, not a grid).

- [ ] **Step 3: Rewrite the markup under the `{:else}` branch of `PoolUtilisation.svelte` as `<ul class="cards">` with a card per row, and the styles**

`.cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(18rem, 100%), 1fr)); gap: var(--z-space-3); padding: var(--z-space-4) var(--z-space-5); }`, `.card { display: grid; gap: var(--z-space-3); padding: var(--z-space-4); border: var(--z-border-width) solid var(--z-border); border-radius: var(--z-radius-lg); background: var(--z-surface); }`, `.card.pinned { border-color: var(--z-pending-border); background: var(--z-pending-subtle); }`, the head as today, `.facts` a `dl` of two `div`s (`dt` in `--z-text-xs` muted, `dd` tabular), the bar and capacity line as today. The name is `overflow-wrap: anywhere`. The skeleton branch draws three cards. The cap: `const { shown, more } = $derived(cap(rows, POOLS_SHOWN))`.

- [ ] **Step 4: Run the tests, then `overview.spec.ts`, `calm-overview.spec.ts` and `overview-columns.spec.ts` whole on the rebuilt binary**

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/overview/PoolUtilisation.svelte web/tests/overview.spec.ts
git commit -m "Draw each pool on the Overview as a card that keeps its ceiling"
```

### Task 5: The body as the registry's grid

**Files:**

- Modify: `web/src/routes/Overview.svelte`
- Modify: `web/src/lib/overview/RecentFailures.svelte`, `ActiveJobs.svelte`, `EventsFeed.svelte` (the `Panel` icon: `Zap`, `Workflow`, `Activity`; the failures' one-line form gets the card's edge and radius)
- Modify: `web/src/lib/overview/OverviewDisclosure.svelte` (an `icon` prop drawn as the same tile before the title, `Clock` for History, `HardDrive` and `Gauge` for the diagnostics)
- Test: `web/tests/overview.spec.ts` (one new test), `web/tests/calm-overview.spec.ts` (keep; its order assertion holds)

**Interfaces:**

- Consumes: `OVERVIEW_PANELS`, `visiblePanels` from Task 1; `NeedsAttention` from Task 3.
- Produces: `Overview.svelte` renders `<div class="body">` with `{#each visiblePanels(OVERVIEW_PANELS, { setupPending }) as panel (panel.id)}` and an `{#if panel.id === ...}` chain drawing each panel in a `<div class="slot {panel.place}" data-panel={panel.id}>`; the ids `pools`, `jobs`, `events`, `history`, `diagnostics` stay on the elements the jump bar and the tests reach today; `#now` stays above the body with the first-run checklist, the stale line and `RepairOffers`. Grid: `.body { display: grid; grid-template-columns: minmax(0, 2fr) minmax(0, 1fr); gap: var(--z-space-4); align-items: stretch; } .slot.full { grid-column: 1 / -1; }`; under 1180px one column. The `.events` flex rules move onto `.slot.side`.

- [ ] **Step 1: Write the failing test**

```ts
test("the body is drawn from the registry, in its order, and one column on a phone", async ({
  page,
}) => {
  const order = await page
    .locator("[data-panel]")
    .evaluateAll((nodes) => nodes.map((n) => n.getAttribute("data-panel")));
  expect(order).toEqual([
    "attention",
    "pools",
    "failures",
    "jobs",
    "events",
    "history",
    "diagnostics",
  ]);
  await page.setViewportSize({ width: 375, height: 800 });
  const columns = await page
    .locator(".body")
    .evaluate(
      (el) => getComputedStyle(el).gridTemplateColumns.split(" ").length,
    );
  expect(columns).toBe(1);
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `npx playwright test tests/overview.spec.ts --project chromium -g "drawn from the registry"`
Expected: FAIL, order is `[]`.

- [ ] **Step 3: Rewrite the body of `Overview.svelte` from `<div class="stack">` down; add the icons; give `OverviewDisclosure` its `icon` prop and the one-line failures its card edge**

The `.work`, `.primary` and `.stack` rules go; `.destination` keeps its scroll margin; `#events > .panel { flex: 1 }` stays under `@media (min-width: 1181px)` on `.slot.side`.

- [ ] **Step 4: Run `overview.spec.ts`, `calm-overview.spec.ts`, `overview-columns.spec.ts` and `page-bands.spec.ts` on the rebuilt binary, chromium then mobile**

Expected: all pass. The columns spec (events as tall as jobs) holds because the side slot stretches to the main slot's row.

- [ ] **Step 5: Commit**

```bash
git add web/src/routes/Overview.svelte web/src/lib/overview/RecentFailures.svelte web/src/lib/overview/ActiveJobs.svelte web/src/lib/overview/EventsFeed.svelte web/src/lib/overview/OverviewDisclosure.svelte web/tests/overview.spec.ts
git commit -m "Render the Overview's body from its list of panels"
```

### Task 6: The events rows' mark as a pill

**Files:**

- Modify: `web/src/lib/overview/EventsFeed.svelte`
- Test: `web/tests/overview.spec.ts`

**Interfaces:** none new. The `.mark` becomes a pill: `border-radius: var(--z-radius-full)`, `min-width: var(--z-space-6)`, `height: var(--z-space-5)`, `padding-inline: var(--z-space-2)`, the tone's subtle ground and colour as today, the icon inside, and `border: var(--z-border-width) solid` the tone's border token from `toneTokens`.

- [ ] **Step 1: Write the failing test**

```ts
test("an event's mark is a pill in its tone, the icon inside it", async ({
  page,
}) => {
  const feed = page.getByRole("region", { name: "Recent events", exact: true });
  const mark = feed.getByRole("listitem").first().locator(".mark");
  await expect(mark).toBeVisible();
  const [radius, height, border] = await mark.evaluate((el) => {
    const s = getComputedStyle(el);
    return [
      parseFloat(s.borderTopLeftRadius),
      el.getBoundingClientRect().height,
      s.borderTopWidth,
    ];
  });
  expect(radius).toBeGreaterThanOrEqual(height / 2);
  expect(border).toBe("1px");
  await expect(mark.locator("svg")).toHaveCount(1);
});
```

- [ ] **Step 2: Run it to see it fail** (`-g "mark is a pill"`). Expected: FAIL on the border width `0px`.

- [ ] **Step 3: Change the `.mark` rule and its inline style to include the border colour.**

- [ ] **Step 4: Run `overview.spec.ts` whole.** Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/overview/EventsFeed.svelte web/tests/overview.spec.ts
git commit -m "Draw an event's mark as a pill in its tone"
```

### Task 7: Lint, docs and the whole run

**Files:**

- Modify: `docs/ui-guidelines.md` (the `Panel` row gains "an optional icon in a tile beside the title"; the Overview section gains a paragraph on the body: the registry, the attention card's three rows, the pool cards and their cap, the grid)
- Modify: `docs/screenshots/overview-*.webp` and `docs/screenshots/mission-control/overview-*.webp` via `make screenshots` (both styles)

- [ ] **Step 1: `cd web && npm run lint && npm run check && npm run test:unit`** Expected: clean; `# fail 0`.
- [ ] **Step 2: Docs; `go test -count=1 ./internal/docs/`.** Expected: ok.
- [ ] **Step 3: `cd web && npx playwright test tests/overview.spec.ts tests/calm-overview.spec.ts tests/overview-columns.spec.ts tests/page-bands.spec.ts tests/gallery.spec.ts tests/a11y.spec.ts`** (all projects). Expected: all pass. Then the whole suite in the background to a log, read its tail.
- [ ] **Step 4: `make screenshots` and `ZOOMIES_SCREENSHOT_STYLE=mission-control make screenshots`; `git clean -fq docs/screenshots/mission-control` (only the tracked captures are committed); photographs of the Overview at 1440 and 375 before and after for the pull request.**
- [ ] **Step 5: Commit the docs and captures; audit; push; pull request body with results.**

```bash
git add docs/ui-guidelines.md docs/screenshots
git commit -m "Say what the Overview's body is now, and recapture it"
```
