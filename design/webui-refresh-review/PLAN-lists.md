# The list card, Runners first: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the list card from the kit's parts (a `Panel` eyebrow and toolbar, a bare `DataGrid`, a new `FilterChips`) and draw the Runners page as band, card and a folded Fleet context, with the primary filter as chips and the default columns trimmed.

**Architecture:** Three small kit changes held by gallery tests, one pure module for the chips' options and selection held by unit tests, then the Runners route rebuilt around them with its Playwright spec re-pointed and extended. `RunnerInsights` keeps serving the pool page and gains a `folded` form for the Runners page's disclosure.

**Tech Stack:** Svelte 5 (runes), Tailwind v4 tokens, Vite, Playwright (`web/tests/`), Node 22 unit tests (`web/unit/`). No new dependency and no new token.

**Spec:** `design/webui-refresh-review/SPEC-lists.md`. Read it first, then `web/AGENTS.md`.

## Global Constraints

- No raw colour and no raw value that has a token; no change to `tokens.css`; no new dependency.
- Both styles in both modes at 1440, 375 and 320 pixels with no horizontal overflow; 4.5:1 for text, 3:1 for a control's edge; a chip's state is never colour alone.
- Nothing reachable from the UI that the REST API does not expose: the counts are `fleet.stats.runners`.
- Every existing Runners behaviour and test is kept; a test whose selector changes is re-pointed, never deleted.
- The only new words on the page: the eyebrow `{total} known, {live} live`, the title `All runners`, the summary `Fleet context`.
- British spelling, no em dash, no spaced double hyphen; imperative commit messages; every new behaviour arrives with a test watched to fail.
- Branch `ui/list-card` in `eyupio/zoomies`, cut from `ui/overview-cards`; run `npm run lint`, `npm run check` and `npm run test:unit` in `web/` before every commit.
- Commit trailer on every commit: `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_011MNNzpSBoWt2yKP9iPNjbP`.

## Review Focus

1. The stats have not arrived (first paint, or `/api/v1/stats` failing): no eyebrow, chips without counts, and never the word `undefined` or `NaN` on the page. Pinned in Task 2 (unit) and Task 3 (Playwright with the stats route failed).
2. A pool or host filter in force: the stats are the fleet's, so a count beside a chip would disagree with the rows under it. The chips drop their counts while either filter is set. Pinned in Task 2 (unit) and Task 3.
3. `state=bogus` in the address: ignored as today, All pressed, rows unfiltered. Pinned in Task 3.
4. The quirky-status setting off: chips read Provisioning, Registering, Idle, Busy, Draining, Failed, as the filter did. Pinned in Task 2 (unit).
5. An operator who showed Age before this change: a saved layout with Age visible stays visible after Age becomes hidden by default. Pinned in Task 4.

---

### Task 1: The Panel's eyebrow and toolbar, and the bare grid

**Files:**

- Modify: `web/src/lib/components/Panel.svelte` (props, markup, styles)
- Modify: `web/src/lib/components/DataGrid.svelte` (props, `.grid` style, the `bare` class)
- Modify: `web/src/gallery/sections/Data.svelte` (the grid block becomes the list card)
- Test: `web/tests/gallery.spec.ts`

**Interfaces:**

- Produces: `Panel` props `eyebrow?: string` and `toolbar?: Snippet`; `DataGrid` prop `bare?: boolean`. A bare grid's root `.grid` carries class `bare`.

- [ ] **Step 1: Write the failing gallery test**

In `web/tests/gallery.spec.ts`, after the panel-with-a-tile test:

```ts
test("the list card is one frame: an eyebrow over the title, chips in a toolbar, and a bare grid inside", async ({
  page,
}) => {
  await open(page, "mission-control", "light");
  const card = page.getByRole("region", { name: "Data grid" });
  const eyebrow = card.locator("header .eyebrow");
  await expect(eyebrow).toHaveText("11 known, 9 live");
  await expect(
    eyebrow.evaluate((el) => getComputedStyle(el).textTransform),
  ).resolves.toBe("uppercase");
  await expect(
    card
      .locator(".toolbar")
      .getByRole("group", { name: "Filter by runner state" }),
  ).toBeVisible();
  const grid = card.locator(".grid");
  await expect(grid).toHaveClass(/bare/);
  const [gridBorder, cardBorder] = await Promise.all([
    grid.evaluate((el) => getComputedStyle(el).borderTopWidth),
    card.evaluate((el) => getComputedStyle(el).borderTopWidth),
  ]);
  expect(gridBorder).toBe("0px");
  expect(parseFloat(cardBorder)).toBeGreaterThan(0);
});
```

- [ ] **Step 2: Run it.** `cd web && npx playwright test tests/gallery.spec.ts --project gallery -g "list card"`. Expected: FAIL, no `.eyebrow`.

- [ ] **Step 3: `Panel`.** Add `eyebrow?: string` and `toolbar?: Snippet` to `Props`. In the markup, inside `.titles` before the `h2`: `{#if eyebrow}<p class="eyebrow">{eyebrow}</p>{/if}`; after `</header>`: `{#if toolbar}<div class="toolbar">{@render toolbar()}</div>{/if}`. Styles: `.eyebrow` is `margin: 0 0 var(--z-space-1)`, `font-size: var(--z-text-2xs)`, `font-weight: var(--z-weight-semibold)`, `letter-spacing: var(--z-tracking-wider)`, `text-transform: uppercase`, `color: var(--z-text-muted)`; `.toolbar` is `padding: var(--z-space-3) var(--z-space-5)` with `border-bottom: var(--z-border-width) solid var(--z-border)`. The existing `.titles p` rule must not restyle the eyebrow: scope it to `.titles .description` by giving the description `<p>` that class.

- [ ] **Step 4: `DataGrid`.** Add `bare?: boolean` (default false) to `Props`; add `class:bare` on the root `div.grid`; add `.grid.bare { border: 0; border-radius: 0; background: transparent; }` after `.grid`.

- [ ] **Step 5: The gallery.** In `Data.svelte`, give the Panel `icon={Server}` (from `@lucide/svelte`), `eyebrow="11 known, 9 live"`, `flush`, and a `toolbar` snippet holding a `FilterChips` placeholder: until Task 2 lands, a `div role="group" aria-label="Filter by runner state"` with one `Button` "All". Pass `bare` to the `DataGrid`. Move the grid-state `Segmented` into the Panel's `actions`.

- [ ] **Step 6: Run it, and the existing grid and panel tests.** `npx playwright test tests/gallery.spec.ts --project gallery -g "list card|ready, empty|panel with an icon"`. Expected: all pass. Then `npm run lint && npm run check`.

- [ ] **Step 7: Commit.** `Give a panel an eyebrow and a toolbar, and let a grid sit bare inside one`

### Task 2: FilterChips and the runner state chips

**Files:**

- Create: `web/src/lib/components/FilterChips.svelte`
- Create: `web/src/lib/runners/state-chips.ts`
- Create: `web/unit/state-chips.test.ts`
- Modify: `web/src/gallery/sections/Controls.svelte` (a FilterChips block), `web/src/gallery/sections/Data.svelte` (the placeholder becomes `FilterChips`)
- Test: `web/tests/gallery.spec.ts`

**Interfaces:**

- Produces, in `state-chips.ts`:

```ts
import type { RunnerState } from "$lib/status";
export interface StateChip {
  value: RunnerState;
  label: string;
  count?: number;
}
/** The chips in the product's order, removed left out; counts only when `counted`. */
export function runnerStateChips(
  stats:
    | {
        provisioning?: number;
        registering?: number;
        idle?: number;
        busy?: number;
        draining?: number;
        failed?: number;
      }
    | undefined,
  quirky: boolean,
  counted: boolean,
): StateChip[];
/** provisioning + registering + idle + busy + draining, or undefined with no stats. */
export function liveRunners(
  stats: Parameters<typeof runnerStateChips>[0],
): number | undefined;
/** The eyebrow, or undefined until the stats say. */
export function runnersEyebrow(
  stats:
    (Parameters<typeof runnerStateChips>[0] & { total?: number }) | undefined,
): string | undefined;
/** Toggle a value in a selection; an empty selection is "All". */
export function toggleChip<T extends string>(
  selected: readonly T[],
  value: T,
): T[];
```

- Produces: `FilterChips` with the props the spec gives (`options`, `selected`, `label`, `allLabel = 'All'`, `total`, `onchange`).

- [ ] **Step 1: Write the failing unit tests** in `web/unit/state-chips.test.ts` (node:test, as `unit/overview-panels.test.ts`):

```ts
test('the chips are the six states in order, removed left out, with counts when counted', ...)
  // stats {provisioning:1, registering:1, idle:3, busy:3, draining:1, failed:1}, quirky false, counted true
  // → values ['provisioning','registering','idle','busy','draining','failed'], labels ['Provisioning', …, 'Failed'], counts [1,1,3,3,1,1]
test('a pool or host filter takes the counts away, since the stats are the fleet\'s', ...)
  // counted false → every count undefined
test('the quirky setting on gives the kennel\'s words', ...)
  // quirky true → first label 'Walkies!'
test('no stats means no eyebrow and chips with no counts, never undefined on the page', ...)
  // runnersEyebrow(undefined) === undefined; liveRunners(undefined) === undefined; chips have no count key set to anything
test('the eyebrow counts known and live', ...)
  // {total: 11, provisioning:1, registering:1, idle:3, busy:3, draining:1, failed:1} → '11 known, 9 live'
test('toggling a chip adds it, toggling again removes it, and All is the empty selection', ...)
  // toggleChip([], 'busy') → ['busy']; toggleChip(['busy'], 'idle') → ['busy','idle']; toggleChip(['busy','idle'], 'busy') → ['idle']
```

- [ ] **Step 2: Run them.** `node --import ./unit/register.mjs --test unit/state-chips.test.ts`. Expected: FAIL, module not found.

- [ ] **Step 3: Write `state-chips.ts`** to the signatures above. Labels come from `runnerStatus(state, quirky).label`; the order is `RUNNER_STATES` without `removed`.

- [ ] **Step 4: Run them.** Expected: 6 pass.

- [ ] **Step 5: Write the failing gallery test:**

```ts
test("filter chips say which are in force with a tick and aria-pressed, in every combination", async ({
  page,
}) => {
  for (const { style, mode } of COMBINATIONS) {
    await open(page, style, mode);
    const group = page
      .getByRole("group", { name: "Filter by runner state" })
      .first();
    const busy = group.getByRole("button", { name: /^Busy/ });
    await expect(busy).toHaveAttribute("aria-pressed", "true");
    await expect(busy.locator("svg")).toHaveCount(1);
    const idle = group.getByRole("button", { name: /^Idle/ });
    await expect(idle).toHaveAttribute("aria-pressed", "false");
    await expect(idle.locator("svg")).toHaveCount(0);
    await expect(group.getByRole("button", { name: /^All/ })).toHaveAttribute(
      "aria-pressed",
      "false",
    );
    expect(
      parseFloat(
        await busy.evaluate((el) => getComputedStyle(el).borderRadius),
      ),
    ).toBeGreaterThan(20);
  }
});
```

- [ ] **Step 6: Run it.** `npx playwright test tests/gallery.spec.ts --project gallery -g "filter chips"`. Expected: FAIL, no group.

- [ ] **Step 7: `FilterChips.svelte`.** The markup and styles the spec gives: `div[role=group][aria-label]`, a `button` per chip with `aria-pressed`, `Check` (12px, `aria-hidden="true"`) only when pressed, `<span class="count">` only when `count !== undefined`; All first with `total` as its count. `onchange(toggleChip(selected, value))` for a state, `onchange([])` for All. Styles per the spec; `@media (max-width: 768px) { .chip { min-height: var(--z-control-touch); } }`.

- [ ] **Step 8: The gallery.** `Controls.svelte` gains a block `FilterChips` after Segmented: one `FilterChips` with `label="Filter by runner state"`, `total={11}`, options from `runnerStateChips({...the Step 1 stats}, false, true)`, `selected={['busy','draining']}`, `onchange` writing a local state. `Data.svelte` swaps the Task 1 placeholder for the same component with `selected={[]}`.

- [ ] **Step 9: Run the gallery spec whole.** `npx playwright test tests/gallery.spec.ts --project gallery`. Expected: all pass. `npm run lint && npm run check && npm run test:unit`.

- [ ] **Step 10: Commit.** `Add filter chips, and the runner states as chips with their counts`

### Task 3: The Runners page as a card

**Files:**

- Modify: `web/src/routes/Runners.svelte`
- Delete: `web/src/lib/runners/RunnerStateFilter.svelte` (after `grep -rn RunnerStateFilter web/src` shows no other importer)
- Test: `web/tests/runners.spec.ts`

**Interfaces:**

- Consumes: Task 1's `Panel` props and `bare`; Task 2's `FilterChips`, `runnerStateChips`, `runnersEyebrow`.

- [ ] **Step 1: Re-point the state-filter test and write the new ones** in `runners.spec.ts`. In `'filtering by state narrows the rows and puts the filter in the URL'`, replace the popover steps with `await page.getByRole('group', { name: 'Filter by runner state' }).getByRole('button', { name: /^Busy/ }).click();` and the last assertion with `await expect(page.getByRole('group', { name: 'Filter by runner state' }).getByRole('button', { name: /^Busy/ })).toHaveAttribute('aria-pressed', 'true');`. Add:

```ts
test("the card's eyebrow counts the runners the stats know and the ones that are live", ...)
  // read /api/v1/stats via page.request; expect the eyebrow text `${total} known, ${live} live`
test('two chips are two states in the address, and All clears them', ...)
  // click Busy then Draining → URL matches /state=busy/ and /state=draining/; click All → no `state=` in URL, All aria-pressed true
test('a state the product does not have is ignored, with All pressed', ...)
  // goto '/runners?state=bogus' → All aria-pressed true, rows count equals the unfiltered count
test('the chips drop their counts while a pool filter is in force', ...)
  // chips have .count before; select a pool via 'Filter by pool' → no .count in the group
test('with the stats failed the card still draws, without an eyebrow or counts', ...)
  // route '**/api/v1/stats*' → 500 before goto; expect header .eyebrow count 0; group visible; page text has no 'undefined'
test('every chip fits on a phone with no horizontal overflow', ...)
  // test.skip unless viewport width < 500; expect scrollWidth - innerWidth <= 0
```

- [ ] **Step 2: Run the file.** `npx playwright test tests/runners.spec.ts --project chromium -g "eyebrow|two chips|does not have|drop their counts|stats failed|filtering by state"`. Expected: the six FAIL (no group / no eyebrow).

- [ ] **Step 3: Rebuild the page's middle.** Replace the `div.filters > FilterBar` and the `div#runners-list > DataGrid` with one `Panel` carrying `title="All runners"`, `icon={Server}`, `eyebrow={runnersEyebrow(fleet.stats?.runners)}`, `flush`, `class="list-card"`, wrapped in the existing `div#runners-list.jump-target` so the band's figure still lands on it. `actions` snippet: the search `Input`, both `Select`s and the Include removed `Switch` as they are. `toolbar` snippet: `<FilterChips label="Filter by runner state" total={fleet.stats?.runners?.total} options={runnerStateChips(fleet.stats?.runners, prefs.quirkyStatus, poolId === '' && hostId === '')} selected={states} onchange={(next) => router.setQuery({ state: next.length > 0 ? next : null })} />`. Keep the `FilterBar` for its chips row only: `<FilterBar {chips} onclear={clearFilters} />` directly above the Panel (it renders nothing with no chips). The `DataGrid` gains `bare`. Remove the `RunnerStateFilter` import and file.

- [ ] **Step 4: Run the whole Runners spec on both projects.** `npx playwright test tests/runners.spec.ts tests/tables.spec.ts --project chromium --project mobile`. Expected: all pass. `npm run lint && npm run check`.

- [ ] **Step 5: Commit.** `Draw the Runners list as one card, with the states as chips`

### Task 4: Fleet context folded, and the default columns trimmed

**Files:**

- Modify: `web/src/lib/insights/RunnerInsights.svelte` (`folded` prop), `web/src/routes/Runners.svelte` (the disclosure; `hiddenByDefault` on `created_at`, `cpu`, `memory`)
- Test: `web/tests/runners.spec.ts`, `web/tests/mobile.spec.ts`

**Interfaces:**

- Produces: `RunnerInsights` prop `folded?: boolean`: with it, no `section.context` chrome and no inner `details`; the scope row, the lifecycle `ChartPanel` and the note are drawn as plain content.

- [ ] **Step 1: Write the failing tests.** In `runners.spec.ts`:

```ts
test('the lifecycle is behind Fleet context, under the list', async ({ page }) => {
  await goto(page, '/runners', 'Runners');
  await expect(page.getByRole('list', { name: 'Runner lifecycle, in order' })).toHaveCount(0);
  const context = page.locator('details.fleet-context');
  const card = page.getByRole('region', { name: 'All runners' });
  expect((await context.boundingBox())!.y).toBeGreaterThan((await card.boundingBox())!.y);
  await context.getByText('Fleet context', { exact: true }).click();
  await expect(page.getByRole('list', { name: 'Runner lifecycle, in order' })).toBeVisible();
  await expect(context.getByText('Explore fleet activity over the last 24 hours')).toBeVisible();
});
test('Age, CPU and Memory are a click away rather than on by default, and a saved choice to show Age survives a reload', ...)
  // columnheaders exclude Age, CPU, Memory; open Columns, check Age; reload; Age header visible
```

In `mobile.spec.ts`, the lifecycle test opens `Fleet context` first: `await page.getByText('Fleet context', { exact: true }).click();` before the existing click, and the existing `'How runners move through their states'` click is removed (the content is open inside).

- [ ] **Step 2: Run them.** `npx playwright test tests/runners.spec.ts --project chromium -g "Fleet context|a click away"; npx playwright test tests/mobile.spec.ts --project mobile -g "lifecycle keeps its steps"`. Expected: FAIL.

- [ ] **Step 3: `RunnerInsights` `folded`.** With `folded`, render the scope row, then the `ChartPanel` and the note directly, with no `<section>` and no `<details>`; without it, exactly as today.

- [ ] **Step 4: The Runners page.** Remove `<RunnerInsights {poolId} pulse={false} />` from above. After the card, one `<details class="fleet-context"><summary>Fleet context</summary><RunnerInsights {poolId} pulse={false} folded /><details class="activity-history">…existing…</details></details>`, styled as `.activity-history` is (move those rules to a shared `.fold` class on both). Set `hiddenByDefault: true` on the `created_at`, `cpu` and `memory` columns.

- [ ] **Step 5: Run.** The two commands from Step 2, then `npx playwright test tests/runners.spec.ts tests/mobile.spec.ts tests/tables.spec.ts tests/column-layout.spec.ts --project chromium --project mobile`. Expected: all pass. `npm run lint && npm run check`.

- [ ] **Step 6: Commit.** `Fold the Runners page's context under its list, and trim its default columns`

### Task 5: Docs, the whole run, the captures and the pull request

**Files:**

- Modify: `docs/ui-guidelines.md` (component inventory rows for `Panel`, `DataGrid`, new `FilterChips`; the Runners item in the information architecture says band, card, chips, Fleet context)
- Modify: `docs/screenshots/runners-*.webp` and `mission-control/runners-*.webp` via `make screenshots`

- [ ] **Step 1: Docs; `go test -count=1 ./internal/docs/`.** Expected: ok.
- [ ] **Step 2: `go test -count=1 ./internal/api/`** (the embedded UI placeholder test) and `make lint`. Expected: ok.
- [ ] **Step 3: Rebuild the binary and run the whole Playwright suite in the background to a log**; rerun any failure alone. Expected: every failure a load flake that passes alone, in a file this branch does not touch.
- [ ] **Step 4: `make screenshots` both styles**, keep only the Runners captures, `git clean -fq docs/screenshots/mission-control`; photographs of the page at 1440 and 375 in both styles for the pull request.
- [ ] **Step 5: Commit, audit, push, draft pull request with results.** `Say what the Runners page is now, and recapture it`
