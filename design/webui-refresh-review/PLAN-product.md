# The shared look in the product, pull request 1: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Carry what the shared look gained on 31 October 2026 into the product through its components, so every page takes it at once: pill buttons in Runner Blue, status pills, large-radius fields, a terminal window for commands, a facts panel on the dark stage, and the public pages' form on a card.

**Architecture:** Six component changes and two new components in `web/src/lib/components/`, each held by a Playwright assertion in the gallery spec, which runs the real components in all four style and mode combinations against the Vite dev server. The three places that show a command and the one that shows a block of facts swap their own markup for the new components. `SplitPage` frames its column as a card. Documentation and the photographs follow in the last task.

**Tech Stack:** Svelte 5 (runes), Tailwind v4 tokens in `tokens.css`, Vite, Playwright (`web/tests/`), Node 22. No new dependency and no new token.

**Spec:** `design/webui-refresh-review/SPEC-product.md`. Read it first, then `web/AGENTS.md` and `docs/ui-guidelines.md` sections 1.4 and 3.

## Global Constraints

- No raw colour in a component, and no raw value that already has a token (`web/AGENTS.md`). No change to `tokens.css`.
- No new dependency.
- Status colours keep their fixed mapping; a pill never carries a state by colour alone (the dot or the word is always there).
- Both styles (Mission Control and Classic) in both modes; 1440, 375 and 320 pixels with no horizontal overflow; 4.5:1 for text and 3:1 for a control's edge.
- Every command and every sentence the product shows is unchanged.
- Prose: British spelling, no em dash, no spaced double hyphen; commit messages are imperative sentences in plain prose, and end with the two attribution lines below.
- Work on branch `ui/shared-look-kit` in `eyupio/zoomies`; run `npm run lint`, `npm run check` and `npm run test:unit` in `web/` before every commit.
- Commit trailer, on every commit:
  `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_011MNNzpSBoWt2yKP9iPNjbP`

## Review Focus

1. A command longer than the terminal's width (the install command carries a token and an address): it wraps inside the window on a phone and scrolls on a desktop, and never widens the page. Pinned in Task 5.
2. The primary button's text on the stage: Zoomies Black on Runner Blue inside the band, in dark mode, still 7.3:1, since the band remaps `--z-text` and the button must not read that. Pinned in Task 1.
3. A `Badge` of size `sm` next to a `Badge` of size `md` in one grid cell: both pills, both the same height as before, so no row grows. Pinned in Task 3.
4. A facts panel with one item, and with a value that is an unbroken 120-character string: one column on a phone, the value wraps, nothing overflows. Pinned in Task 6.
5. The card beside the stage on a phone, where the stage is a strip: the card keeps its border and radius and the first field is on the first screen at 375 pixels. Pinned in Task 7.

---

### Task 1: The pill button in Runner Blue

**Files:**

- Modify: `web/src/lib/components/Button.svelte` (the `<style>` block: `.btn`, `.primary`, `.primary:hover`, `.primary:active`)
- Test: `web/tests/gallery.spec.ts`

**Interfaces:**

- Produces: the `Button` component unchanged in its props. Every variant has `border-radius: var(--z-radius-full)`; `.primary` has `background: var(--z-brand-runner-blue)` and `color: var(--z-brand-black)`, hover `filter: brightness(1.12)` and active `filter: brightness(0.94)`, with no other colour.

- [ ] **Step 1: Write the failing test**

In `web/tests/gallery.spec.ts`, after the existing tests, add one test that visits every combination in `COMBINATIONS` and, in the "Controls" section, reads the first primary button (`page.locator('section', { hasText: 'Button variants' }).getByRole('button', { name: 'Primary' })` or the gallery's own label) and asserts:

```ts
test("the primary button is a pill in Runner Blue with Zoomies Black text, in every combination", async ({
  page,
}) => {
  for (const { style, mode } of COMBINATIONS) {
    await open(page, style, mode);
    const button = page.getByRole("button", { name: "Primary" }).first();
    const root = page.locator(":root");
    const [radius, background, colour, runnerBlue, black] = await Promise.all([
      button.evaluate((el) => getComputedStyle(el).borderRadius),
      button.evaluate((el) => getComputedStyle(el).backgroundColor),
      button.evaluate((el) => getComputedStyle(el).color),
      root.evaluate((el) =>
        getComputedStyle(el).getPropertyValue("--z-brand-runner-blue").trim(),
      ),
      root.evaluate((el) =>
        getComputedStyle(el).getPropertyValue("--z-brand-black").trim(),
      ),
    ]);
    expect(parseFloat(radius), `${style} ${mode}`).toBeGreaterThanOrEqual(999);
    expect(parseColour(background), `${style} ${mode}`).toEqual(
      hexToRgb(runnerBlue),
    );
    expect(parseColour(colour), `${style} ${mode}`).toEqual(hexToRgb(black));
  }
});
```

`parseColour` exists in the spec; add `hexToRgb(hex: string): [number, number, number]` beside it. If the gallery's primary button carries another accessible name, use that name: read `web/src/gallery/sections/Controls.svelte` lines 28 to 46 first.

- [ ] **Step 2: Run the test to verify it fails**

Run, in `web/`: `npx playwright test --project gallery -g "primary button is a pill"`
Expected: FAIL on the radius assertion (the button is `8px`).

- [ ] **Step 3: Change the styles in `Button.svelte`**

In `.btn`, `border-radius: var(--z-radius-full)`. In `.primary`, `background: var(--z-brand-runner-blue); color: var(--z-brand-black);` and, so the band's remap of `--z-text` cannot reach it, nothing that reads `--z-text`. Replace the hover and active backgrounds (`--z-accent-hover`, `--z-accent-active`) with `filter: brightness(1.12)` and `filter: brightness(0.94)` respectively, keeping the same selectors. Add a comment saying why the brand blue carries dark text where the accent carried white (the 7.3:1 figure, `docs/brand.md`).

- [ ] **Step 4: Run the test to verify it passes**

Run: `npx playwright test --project gallery -g "primary button is a pill"`
Expected: PASS for all four combinations.

- [ ] **Step 5: Run the gallery's other tests, lint and check**

Run: `npx playwright test --project gallery && npm run lint && npm run check`
Expected: all pass. The gallery's contrast test reads live colours; if it names the primary button, the new pair passes at 7.3:1.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/components/Button.svelte web/tests/gallery.spec.ts
git commit -m "Draw every button as a pill, the primary one in Runner Blue"
```

---

### Task 2: The icon button and the fields

**Files:**

- Modify: `web/src/lib/components/IconButton.svelte` (`.icon-btn` at the `<style>` block, line 131: `border-radius`)
- Modify: `web/src/lib/components/Input.svelte` (line 164), `web/src/lib/components/Select.svelte` (line 86), `web/src/lib/components/Textarea.svelte` (line 69): `border-radius`
- Test: `web/tests/gallery.spec.ts`

**Interfaces:**

- Produces: `.icon-btn` with `border-radius: var(--z-radius-full)`; the three fields' controls with `border-radius: var(--z-radius-lg)`. Props unchanged.

- [ ] **Step 1: Write the failing test**

```ts
test("icon buttons are pills and text fields take the large radius", async ({
  page,
}) => {
  await open(page, "mission-control", "dark");
  const large = await page
    .locator(":root")
    .evaluate((el) =>
      getComputedStyle(el).getPropertyValue("--z-radius-lg").trim(),
    );
  const icon = page.locator(".icon-btn").first();
  expect(
    parseFloat(await icon.evaluate((el) => getComputedStyle(el).borderRadius)),
  ).toBeGreaterThanOrEqual(999);
  for (const selector of [".field input", ".field select", ".field textarea"]) {
    const control = page.locator(selector).first();
    expect(
      await control.evaluate((el) => getComputedStyle(el).borderRadius),
      selector,
    ).toBe(large);
  }
});
```

If the gallery's form controls are not under `.field`, read `web/src/gallery/sections/Forms.svelte` and use the selector it gives the first `Input`, `Select` and `Textarea`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `npx playwright test --project gallery -g "icon buttons are pills"`
Expected: FAIL on the icon button's radius.

- [ ] **Step 3: Change the four radii**

`IconButton.svelte` `.icon-btn`: `border-radius: var(--z-radius-full)`. `Input.svelte`, `Select.svelte`, `Textarea.svelte`: the control's `border-radius: var(--z-radius-lg)`. Checkbox and radio boxes are untouched.

- [ ] **Step 4: Run the test to verify it passes**

Run: `npx playwright test --project gallery -g "icon buttons are pills"`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/components/IconButton.svelte web/src/lib/components/Input.svelte web/src/lib/components/Select.svelte web/src/lib/components/Textarea.svelte web/tests/gallery.spec.ts
git commit -m "Round the icon buttons to pills and the fields to the large radius"
```

---

### Task 3: The status pill

**Files:**

- Modify: `web/src/lib/components/Badge.svelte` (`.badge` at line 69: `border-radius`)
- Test: `web/tests/gallery.spec.ts`

**Interfaces:**

- Produces: `.badge` with `border-radius: var(--z-radius-full)`; `.md` and `.sm` heights unchanged (`--z-space-5` and `--z-space-4`). Review Focus 3 is the height assertion.

- [ ] **Step 1: Write the failing test**

```ts
test("a status badge is a pill and keeps its height in both sizes", async ({
  page,
}) => {
  await open(page, "classic", "light");
  const badges = page.locator(".badge");
  expect(await badges.count()).toBeGreaterThan(1);
  const first = badges.first();
  expect(
    parseFloat(await first.evaluate((el) => getComputedStyle(el).borderRadius)),
  ).toBeGreaterThanOrEqual(999);
  const heights = await badges.evaluateAll((els) =>
    els.map((el) => el.getBoundingClientRect().height),
  );
  const [md, sm] = await page.locator(":root").evaluate((el) => {
    const s = getComputedStyle(el);
    return [
      parseFloat(s.getPropertyValue("--z-space-5")) * 4,
      parseFloat(s.getPropertyValue("--z-space-4")) * 4,
    ];
  });
  for (const h of heights) expect([md, sm]).toContain(Math.round(h));
});
```

The spacing tokens are in rem; `* 4` assumes the root is 16px as the gallery sets it. If `--z-space-5` is written in px, drop the multiplication.

- [ ] **Step 2: Run the test to verify it fails**

Run: `npx playwright test --project gallery -g "status badge is a pill"`
Expected: FAIL on the radius.

- [ ] **Step 3: Change `.badge` to `border-radius: var(--z-radius-full)`** and adjust `.sm`'s left padding to `var(--z-space-2)` so the dot is not cut by the curve.

- [ ] **Step 4: Run the test to verify it passes**

Expected: PASS, every height still one of the two.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/components/Badge.svelte web/tests/gallery.spec.ts
git commit -m "Draw a status badge as a pill"
```

---

### Task 4: The terminal window component

**Files:**

- Create: `web/src/lib/components/Terminal.svelte`
- Modify: `web/src/gallery/sections/Surfaces.svelte` (a new block, "Terminal", under the panel)
- Test: `web/tests/gallery.spec.ts`

**Interfaces:**

- Produces: `Terminal.svelte` with props
  `{ title: string; command: string | readonly string[]; copyLabel?: string; size?: 'sm' | 'md'; class?: string; children?: Snippet }`.
  It draws a window bar (three dots, `aria-hidden`, and `title` in the sans face, muted), a body in `var(--z-font-mono)` with one `<pre><code>` per line of `command` (a string is split on `\n`), a `CopyButton` with `showLabel` and `size="md"` for the whole command joined by `\n` when `copyLabel` is given, and `children` beneath for fine print. Root element `<div class="terminal">`, body `.terminal-body`, bar `.terminal-bar`. The root remaps `--z-text`, `--z-text-muted`, `--z-text-subtle`, `--z-border`, `--z-surface`, `--z-surface-hover`, `--z-surface-raised`, `--z-surface-sunken` and `--z-accent` to the `--z-panel-*` tokens as `PageBand.svelte` does, sets `background: var(--z-panel-bg)`, `border: var(--z-border-width) solid var(--z-panel-border)`, `border-radius: var(--z-radius-lg)` and `color: var(--z-panel-text)`, and names no colour of its own. `size="sm"` halves the padding. The `<pre>` has `white-space: pre-wrap; overflow-wrap: anywhere` (Review Focus 1).

- [ ] **Step 1: Write the failing test**

```ts
test("the terminal is a window on the dark stage that wraps a long command", async ({
  page,
}) => {
  for (const { style, mode } of COMBINATIONS) {
    await open(page, style, mode);
    const terminal = page.locator(".terminal").first();
    const [background, panel, radius] = await Promise.all([
      terminal.evaluate((el) => getComputedStyle(el).backgroundColor),
      page
        .locator(":root")
        .evaluate((el) =>
          getComputedStyle(el).getPropertyValue("--z-panel-bg").trim(),
        ),
      terminal.evaluate((el) => getComputedStyle(el).borderRadius),
    ]);
    expect(parseColour(background), `${style} ${mode}`).toEqual(
      hexToRgb(panel),
    );
    expect(radius).not.toBe("0px");
    await expect(terminal.locator(".terminal-bar")).toContainText(
      "sam@lab-vm-2",
    );
    await expect(
      terminal.getByRole("button", { name: "Copy the command" }),
    ).toBeVisible();
  }
  await page.setViewportSize({ width: 375, height: 800 });
  await open(page, "mission-control", "dark");
  const width = await page
    .locator(".terminal")
    .first()
    .evaluate((el) => el.scrollWidth - el.clientWidth);
  expect(width).toBe(0);
});
```

`--z-panel-bg` resolves to `var(--z-brand-black)`; if `getPropertyValue` returns the unresolved `var(...)`, read `--z-brand-black` instead.

- [ ] **Step 2: Run the test to verify it fails**

Run: `npx playwright test --project gallery -g "terminal is a window"`
Expected: FAIL, no `.terminal` on the page.

- [ ] **Step 3: Create `Terminal.svelte`** with the interface above, and add a "Terminal" block to `Surfaces.svelte`:

```svelte
<Terminal
  title="sam@lab-vm-2"
  command={['sudo sh join.sh --controller https://controller.example --token 0123456789012345678901234567890123456789012345678901234567890123456789', 'zoomies agent join https://controller.example --token …']}
  copyLabel="Copy the command"
>
  <p class="muted">The token in it is shown once, only its hash is stored, and works once.</p>
</Terminal>
```

Add "Terminal" to the gallery spec's `SECTIONS` only if the block is its own region; as a block inside "Surfaces and text" it needs no entry.

- [ ] **Step 4: Run the test to verify it passes**

Expected: PASS in all four combinations and no overflow at 375.

- [ ] **Step 5: Lint, check, commit**

```bash
npm run lint && npm run check
git add web/src/lib/components/Terminal.svelte web/src/gallery/sections/Surfaces.svelte web/tests/gallery.spec.ts
git commit -m "Add the terminal window that shows a command on the dark stage"
```

---

### Task 5: Commands in the terminal

**Files:**

- Modify: `web/src/lib/hosts/AddHostFlow.svelte` (lines 596 to 660: the `.command` block and the `.command.small` block; the `.command*` and `.version-match*` rules in its `<style>`)
- Modify: `web/src/lib/hosts/HostDoctorCommand.svelte` (the `<pre class="mono">` and `.actions` block; its `.terminal` class, which clashes with the component's, is renamed `.doctor`)
- Modify: `web/src/lib/pools/RunsOnPreview.svelte` (the `<figure class="preview">`)
- Test: `web/tests/connect.spec.ts` or `web/tests/host-card.spec.ts`, whichever already opens Add a host to the command step; read both first and add to the one that does. If neither reaches the command, add the assertion to `web/tests/first-run.spec.ts`'s Add a host coverage.

**Interfaces:**

- Consumes: `Terminal` from Task 4.
- Produces: the install command, the join command, the doctor command and the `runs-on` snippet drawn by `Terminal`, with their existing strings, copy labels and fine print. `AddHostFlow`'s `version-match` facts move to Task 6.

- [ ] **Step 1: Write the failing test**

In the spec that reaches the command step, after the command is minted:

```ts
const terminal = page.locator(".terminal").first();
await expect(terminal).toBeVisible();
await expect(terminal.locator("pre")).toContainText("zoomies");
await expect(
  terminal.getByRole("button", { name: "Copy the install command" }),
).toBeVisible();
```

- [ ] **Step 2: Run it to verify it fails**

Run: `npx playwright test --project <that project> -g "<that test's name>"`
Expected: FAIL, no `.terminal`.

- [ ] **Step 3: Replace the three command blocks**

In `AddHostFlow.svelte`: the install command becomes `<Terminal title="Run this on the new host" command={installCommand} copyLabel={windowsHost ? 'Copy the PowerShell command' : 'Copy the install command'}>` with the existing fine-print sentence and `versionNote` as children; the Windows fallback paragraph stays outside it under the same `{#if installCommand}` branch. The join command under the disclosure becomes `<Terminal size="sm" title="With the binary installed" command={joinCommand} copyLabel="Copy the join command" />`. Remove the `.command`, `.command.small`, `.command pre` and `.command-actions` rules. Leave `.version-match` for Task 6.
In `HostDoctorCommand.svelte`: the `<pre>` and `.actions` become `<Terminal title="On the host" command={command} copyLabel="Copy the command">` with the expiry fine print as children; rename the wrapper's `terminal` class to `doctor`.
In `RunsOnPreview.svelte`: the figure becomes `<Terminal size="sm" title="What a workflow writes" command={snippet} copyLabel="Copy the runs-on snippet">` with the hint paragraph as children; keep `figure` semantics by wrapping the `Terminal` in `<figure class="preview">` with no styles of its own beyond margin.

- [ ] **Step 4: Run the test and the suites that touch these pages**

Run: `npx playwright test --project chromium -g "host|pool|runs-on" && npx playwright test --project connect`
Expected: PASS.

- [ ] **Step 5: Lint, check, commit**

```bash
npm run lint && npm run check
git add web/src/lib/hosts/AddHostFlow.svelte web/src/lib/hosts/HostDoctorCommand.svelte web/src/lib/pools/RunsOnPreview.svelte web/tests/
git commit -m "Show every command the operator runs in the terminal window"
```

---

### Task 6: The facts panel

**Files:**

- Create: `web/src/lib/components/Facts.svelte`
- Modify: `web/src/lib/hosts/AddHostFlow.svelte` (the `version-match` list, lines 602 to 613, and its rules)
- Modify: `web/src/gallery/sections/Surfaces.svelte` (a "Facts" block)
- Test: `web/tests/gallery.spec.ts`

**Interfaces:**

- Produces: `Facts.svelte` with props
  `{ label: string; items: readonly { term: string; value: string; mono?: boolean; status?: StatusMeta }[]; class?: string }`.
  A `<dl class="facts" aria-label={label}>` with one `<div>` per item holding `<dt>` and `<dd>`; `mono` draws the value in `var(--z-font-mono)`, `status` draws a `Badge` after the value. Two columns (`grid-template-columns: repeat(2, minmax(0, 1fr))`) and one under 480 pixels. The same `--z-panel-*` remap as `Terminal`, `border-radius: var(--z-radius-lg)`, `padding: var(--z-space-4) var(--z-space-5)`; `dd` has `overflow-wrap: anywhere` (Review Focus 4).

- [ ] **Step 1: Write the failing test**

```ts
test("the facts panel is on the dark stage and holds an unbroken value", async ({
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 800 });
  await open(page, "mission-control", "light");
  const facts = page.locator(".facts").first();
  const [background, panel] = await Promise.all([
    facts.evaluate((el) => getComputedStyle(el).backgroundColor),
    page
      .locator(":root")
      .evaluate((el) =>
        getComputedStyle(el).getPropertyValue("--z-brand-black").trim(),
      ),
  ]);
  expect(parseColour(background)).toEqual(hexToRgb(panel));
  expect(await facts.evaluate((el) => el.scrollWidth - el.clientWidth)).toBe(0);
  expect(
    await facts.evaluate(
      (el) => getComputedStyle(el).gridTemplateColumns.split(" ").length,
    ),
  ).toBe(1);
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `npx playwright test --project gallery -g "facts panel"`
Expected: FAIL, no `.facts`.

- [ ] **Step 3: Create `Facts.svelte`**, add a block to `Surfaces.svelte` with two items, one of them a 120-character unbroken `mono` value and one with a `status`, and replace `AddHostFlow`'s `version-match` list with `<Facts label="Version selected for this host" items={[{ term: 'Controller build', value: minted.controller_version || NO_VALUE, mono: true }, { term: 'Host install channel', value: `:${minted.install_tag}`, mono: true }]} />` inside the install `Terminal`'s children, above the fine print. Remove the `.version-match*` rules.

- [ ] **Step 4: Run the test and the hosts suites**

Run: `npx playwright test --project gallery -g "facts panel" && npx playwright test --project chromium -g "host"`
Expected: PASS.

- [ ] **Step 5: Lint, check, commit**

```bash
npm run lint && npm run check
git add web/src/lib/components/Facts.svelte web/src/lib/hosts/AddHostFlow.svelte web/src/gallery/sections/Surfaces.svelte web/tests/gallery.spec.ts
git commit -m "Add the facts panel and draw a new host's build and channel on it"
```

---

### Task 7: The public pages' form on a card

**Files:**

- Modify: `web/src/lib/public/SplitPage.svelte` (`.side` in the `<style>` block)
- Test: `web/tests/first-run.spec.ts`

**Interfaces:**

- Produces: `.side` with `border: var(--z-border-width) solid var(--z-border)`, `border-radius: var(--z-radius-xl)`, `background: var(--z-surface)`, `padding: var(--z-space-12)` matching the stage's, and under 800 pixels `padding: var(--z-space-6) var(--z-space-5)` with the border kept (Review Focus 5).

- [ ] **Step 1: Write the failing test**

In `first-run.spec.ts`, in the first test (the first screen), after the heading assertion:

```ts
const card = page.locator(".side");
expect(await card.evaluate((el) => getComputedStyle(el).borderStyle)).toBe(
  "solid",
);
expect(
  parseFloat(await card.evaluate((el) => getComputedStyle(el).borderRadius)),
).toBeGreaterThan(0);
```

and in the project's mobile run (or by `page.setViewportSize({ width: 375, height: 812 })` inside the test), assert the setup token field is within the first 812 pixels: `expect((await page.getByLabel('Setup token').boundingBox())!.y).toBeLessThan(812)`.

- [ ] **Step 2: Run it to verify it fails**

Run: `npx playwright test --project first-run -g "first screen"`
Expected: FAIL on `borderStyle` (`none`).

- [ ] **Step 3: Change `.side`** as the interface says, and keep the inner `.form-column` widths the pages set.

- [ ] **Step 4: Run the first-run project**

Run: `npx playwright test --project first-run`
Expected: PASS, including the consent screen's tests.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/public/SplitPage.svelte web/tests/first-run.spec.ts
git commit -m "Put the form beside the stage on a card with the stage's radius"
```

---

### Task 8: Documentation, photographs and the whole suite

**Files:**

- Modify: `docs/ui-guidelines.md` (section 1.4, the radii line at line 515; section 3, the `Button`, `IconButton`, `Badge`, `Input` rows; a `Terminal` and a `Facts` row after `CopyButton`)
- Modify: `docs/brand.md` (lines 46 to 50, the sentence on the accent and buttons)
- Modify: `docs/screenshots/*.webp` by `make screenshots`
- Test: `internal/docs` (holds the documentation), `web/tests/a11y.spec.ts`

- [ ] **Step 1: Rewrite the radii line in section 1.4**

`--z-radius-sm` 4 (checkboxes, the radio box) · `--z-radius-md` 8 (cards' inner boxes) · `--z-radius-lg` 12 (dialogs, panels, fields, the terminal and the facts panel) · `--z-radius-xl` 28 (the stage, and the card beside it) · `--z-radius-full` 9999 (dots, pills, every button).

- [ ] **Step 2: Update section 3 and `docs/brand.md`**

`Button`: add "every variant is a pill; `primary` is Runner Blue under Zoomies Black (7.3:1), so the darkened accent is for links, the active entry and focus". `IconButton`: "a pill". `Badge`: "status pill, a pill in shape as well". `Input`: "the large radius". New rows: `Terminal` ("a command the operator runs: a window bar with a title, the command in mono on the dark stage, a copy control and a line of fine print; `AddHostFlow`, `HostDoctorCommand` and `RunsOnPreview` draw it") and `Facts` ("a block of facts about one thing on the dark stage, two columns and one on a phone, a value in mono or with a status pill"). In `docs/brand.md`, the sentence "and it fails for white text on a button" gains: "which is why a filled button carries Zoomies Black on the pure blue (7.3:1) and the darkened blue is kept for links and text".

- [ ] **Step 3: Run the documentation tests and the whole UI suite**

Run, in the repository root: `make build-nogui && go test ./internal/docs/`
Run, in `web/`: `npm run lint && npm run check && npm run test:unit && npx playwright test`
Expected: all pass. A failure in `a11y.spec.ts`'s contrast pass names the pair; fix the component, never the test.

- [ ] **Step 4: Retake the photographs**

Run, in the repository root: `make build && make screenshots && ZOOMIES_SCREENSHOT_STYLE=mission-control make screenshots`
Expected: `docs/screenshots/*.webp` and `docs/screenshots/mission-control/*.webp` rewritten; `git status` shows only files that already existed.

- [ ] **Step 5: Commit**

```bash
git add docs/ui-guidelines.md docs/brand.md docs/screenshots
git commit -m "Say what the components now draw, and retake the photographs"
```

- [ ] **Step 6: Push and open the pull request**

`git fetch origin main && git rebase origin/main` if main moved, then `git push -u origin ui/shared-look-kit`. Open a draft pull request whose body gives the problem, the approach, the checks run, before-and-after photographs of the Overview, Pools, Add a host at the command step and the first account at 1440 and 375, and the follow-up (the Overview from its mock) left out.
