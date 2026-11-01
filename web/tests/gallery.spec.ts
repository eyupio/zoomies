/**
 * The component gallery, in every style and mode combination.
 *
 * The gallery is development-only (web/gallery.html) and the suite's other
 * specs drive a built binary, which does not contain it. This project drives
 * the Vite dev server instead; see tests/support/serve-gallery.mjs.
 *
 * The point of the spec is not that the page loads. N-T4 asks for the states a
 * healthy fleet never shows to be reviewable across both styles and both modes,
 * so every test below either visits all four combinations or is about the
 * behaviour a combination must not change: focus rings, readable refusal
 * reasons, live contrast, and the phone width with no page-level overflow.
 */
import { expect, test, type Page } from '@playwright/test';
const COMBINATIONS = [
  { style: 'classic', mode: 'light' },
  { style: 'classic', mode: 'dark' },
  { style: 'mission-control', mode: 'light' },
  { style: 'mission-control', mode: 'dark' },
] as const;

/** The regions a reviewer should find on the page, whatever the combination. */
const SECTIONS = [
  'Controls',
  'Form controls',
  'Status vocabulary',
  'Loading',
  'Empty states',
  'Errors',
  'Loading boundary',
  'Toasts',
  'Surfaces and text',
  'Status tones',
  'Type and density',
  'Overlays',
  'Notices',
  'Data grid',
  'Eli repairs',
  'Public pages',
  'Terminal and facts',
];

function open(page: Page, style: string, mode: string): Promise<unknown> {
  return page.goto(`/gallery.html?style=${style}&mode=${mode}`);
}

function parseColour(value: string): [number, number, number] {
  const parts =
    /rgba?\(([^)]+)\)/
      .exec(value)?.[1]
      ?.split(/[,\s/]+/)
      .map(Number) ?? [];
  return [parts[0] ?? 0, parts[1] ?? 0, parts[2] ?? 0];
}

/** A token's hex value as the channels getComputedStyle reports. */
function hexToRgb(hex: string): [number, number, number] {
  const h = hex.replace('#', '');
  return [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16)) as [number, number, number];
}

function luminance(value: string): number {
  const [r, g, b] = parseColour(value).map((channel) => {
    const c = channel / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  }) as [number, number, number];
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(foreground: string, background: string): number {
  const [hi, lo] = [luminance(foreground), luminance(background)].sort((a, b) => b - a) as [
    number,
    number,
  ];
  return (hi + 0.05) / (lo + 0.05);
}

/**
 * Read the colours the browser actually resolved, not the strings the CSS
 * wrote. A component that hard-codes a colour passes a token lint and fails
 * here, which is the test the plan's contrast contract needs.
 *
 * Badges and state cells are presentation rather than controls, so there is no
 * role to reach for; the class each carries is the only thing that says which
 * element is which.
 */
async function assertReadableText(page: Page): Promise<void> {
  const surface = await page.evaluate(() => {
    const panel = document.querySelector('.panel');
    return panel ? getComputedStyle(panel).backgroundColor : 'rgb(255, 255, 255)';
  });

  const badges = await page.locator('.badge').evaluateAll((els) =>
    els.map((el) => {
      const style = getComputedStyle(el);
      return { text: el.textContent?.trim() ?? '', fg: style.color, bg: style.backgroundColor };
    }),
  );
  expect(badges.length).toBeGreaterThan(20);
  for (const badge of badges) {
    if (badge.bg === 'rgba(0, 0, 0, 0)' || badge.bg === 'transparent') continue;
    expect(
      contrast(badge.fg, badge.bg),
      `${badge.text || 'a badge'}: ${badge.fg} on ${badge.bg}`,
    ).toBeGreaterThanOrEqual(4.5);
  }

  const cells = await page
    .locator('[data-gallery="runner-states"] .cell')
    .evaluateAll((els) => els.map((el) => getComputedStyle(el).color));
  expect(cells.length).toBeGreaterThan(5);
  for (const colour of cells) {
    expect(contrast(colour, surface), `${colour} on the panel surface`).toBeGreaterThanOrEqual(4.5);
  }
}

test.describe('every style and mode combination', () => {
  for (const { style, mode } of COMBINATIONS) {
    test(`${style} ${mode} carries its own root attributes and renders every section`, async ({
      page,
    }) => {
      await open(page, style, mode);

      const root = page.locator('html');
      if (style === 'mission-control') {
        await expect(root).toHaveAttribute('data-ui-style', 'mission-control');
      } else {
        expect(await root.getAttribute('data-ui-style')).toBeNull();
      }
      await expect(root).toHaveAttribute('data-theme', mode);

      for (const name of SECTIONS) {
        await expect(page.getByRole('region', { name, exact: true })).toBeVisible();
      }

      // The refusal keeps its reason in the accessible description, and stays
      // a button: `aria-disabled`, never the native attribute.
      const refused = page.locator('button[aria-disabled="true"]', { hasText: 'Delete pool' });
      await expect(refused).toHaveCount(1);
      expect(await refused.getAttribute('disabled')).toBeNull();
      const describedBy = await refused.getAttribute('aria-describedby');
      expect(describedBy).toBeTruthy();
      await expect(page.locator(`#${describedBy}`)).toContainText('Only administrators can delete');

      // A plain disabled control is the other half: it is really disabled and
      // carries no reason.
      await expect(page.getByRole('button', { name: 'Publish' })).toBeDisabled();

      // The keyboard reaches the refused control and draws a ring on it.
      await refused.focus();
      const ring = await refused.evaluate((el) => {
        const style = getComputedStyle(el);
        return {
          visible: el.matches(':focus-visible'),
          style: style.outlineStyle,
          width: Number.parseFloat(style.outlineWidth),
        };
      });
      expect(ring.visible).toBe(true);
      expect(ring.style).not.toBe('none');
      expect(ring.width).toBeGreaterThan(0);

      // Retry actions are present, enabled and named.
      await expect(page.getByRole('button', { name: 'Try again' }).first()).toBeEnabled();

      await assertReadableText(page);
    });

    test(`${style} ${mode} does not overflow at 1440 or 375 pixels`, async ({ page }) => {
      await open(page, style, mode);
      for (const width of [1440, 375]) {
        await page.setViewportSize({ width, height: 900 });
        // A frame for the grid's own measurements to settle after the reflow.
        await page.waitForTimeout(150);
        const { scrollWidth, inner } = await page.evaluate(() => ({
          scrollWidth: document.documentElement.scrollWidth,
          inner: window.innerWidth,
        }));
        expect(scrollWidth, `page width at ${width}px`).toBeLessThanOrEqual(inner);
      }
    });
  }
});

test('an unknown style and mode fall back to Mission Control and System', async ({ page }) => {
  await open(page, 'neon', 'twilight');
  const root = page.locator('html');
  await expect(root).toHaveAttribute('data-ui-style', 'mission-control');
  expect(await root.getAttribute('data-theme')).toBeNull();
});

test('a fresh phone grid follows the style fallback: Cards in Mission Control, Rows in Classic', async ({
  page,
}) => {
  // The gallery sets the root attribute without saving a preference, so this
  // is also the check that the store the grid reads is hearing the same
  // combination the tokens are.
  for (const [style, view] of [
    ['mission-control', 'Cards'],
    ['classic', 'Rows'],
  ] as const) {
    await open(page, style, 'light');
    await page.setViewportSize({ width: 375, height: 900 });
    const region = page.getByRole('region', { name: 'Data grid' });
    // The layout choice lives inside the column panel, which is where the rest
    // of "how this grid is drawn" is: on a phone it was a second control beside
    // the Columns button, answering the same question twice.
    await region.getByRole('button', { name: 'Columns', exact: true }).click();
    await expect(
      region.getByRole('button', { name: new RegExp(`^${view}`) }),
      `${style} starts a fresh phone grid as ${view}`,
    ).toHaveAttribute('aria-pressed', 'true');
  }
});

test('System follows the operating system without changing the chosen style', async ({ page }) => {
  await open(page, 'mission-control', 'system');
  const root = page.locator('html');
  await expect(root).toHaveAttribute('data-ui-style', 'mission-control');
  expect(await root.getAttribute('data-theme')).toBeNull();

  await page.emulateMedia({ colorScheme: 'dark' });
  const dark = await page.evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue('--z-bg').trim(),
  );
  await page.emulateMedia({ colorScheme: 'light' });
  const light = await page.evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue('--z-bg').trim(),
  );
  expect(dark).not.toBe(light);
  await expect(root).toHaveAttribute('data-ui-style', 'mission-control');
});

test('choosing a style applies it in place and writes no preference', async ({ page }) => {
  await open(page, 'classic', 'light');
  const styleGroup = page.getByRole('group', { name: 'Visual style', exact: true });

  await styleGroup.getByRole('button', { name: /^Mission Control/ }).click();
  await expect(page.locator('html')).toHaveAttribute('data-ui-style', 'mission-control');
  expect(new URL(page.url()).searchParams.get('style')).toBe('mission-control');

  await page
    .getByRole('group', { name: 'Colour mode', exact: true })
    .getByRole('button', { name: 'Dark' })
    .click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');

  // The gallery is a review tool: it must not touch the preferences a real
  // Appearance change owns.
  const stored = await page.evaluate(() => ({
    style: localStorage.getItem('zoomies.ui-style'),
    theme: localStorage.getItem('zoomies.theme'),
  }));
  expect(stored).toEqual({ style: null, theme: null });

  await styleGroup.getByRole('button', { name: 'Classic' }).click();
  expect(await page.locator('html').getAttribute('data-ui-style')).toBeNull();
  expect(new URL(page.url()).searchParams.get('style')).toBe('classic');
});

test('the drawer traps focus, closes on Escape and returns focus to its trigger', async ({
  page,
}) => {
  await open(page, 'mission-control', 'dark');
  const trigger = page.getByRole('button', { name: 'Open the runner drawer' });
  await trigger.click();

  const drawer = page.getByRole('dialog', { name: 'run_demo00' });
  await expect(drawer).toBeVisible();
  await expect(drawer.locator('header').getByRole('button', { name: 'Close' })).toBeVisible();

  // Tab never leaves the open drawer, however many times it is pressed.
  for (let i = 0; i < 8; i += 1) {
    await page.keyboard.press('Tab');
    const inside = await drawer.evaluate((el) => el.contains(document.activeElement));
    expect(inside, `focus after Tab ${i + 1}`).toBe(true);
  }

  // Exactly one layer closes per Escape. The last Tab may have landed on the
  // refused control, whose reason opens the shared tooltip, so the presses
  // unwind whatever the loop left open and then the drawer.
  await page.keyboard.press('Escape');
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog', { name: 'run_demo00' })).toHaveCount(0);
  await expect(trigger).toBeFocused();
});

test('the destructive confirmation names the thing and gates the confirm', async ({ page }) => {
  await open(page, 'classic', 'light');
  await page.getByRole('button', { name: 'Delete a pool' }).click();

  const dialog = page.getByRole('dialog', { name: 'Delete pool' });
  await expect(dialog).toBeVisible();
  const confirm = dialog.getByRole('button', { name: 'Delete', exact: true });
  await expect(confirm).toBeDisabled();

  await dialog.getByLabel('Type linux-x64 to confirm').fill('linux-x64');
  await expect(confirm).toBeEnabled();
  await confirm.click();
  await expect(dialog).toHaveCount(0);
});

test('the grid shows ready, empty, loading and failed states', async ({ page }) => {
  await open(page, 'mission-control', 'light');
  const region = page.getByRole('region', { name: 'Data grid' });
  const table = region.getByRole('grid', { name: 'Gallery runners' });

  await expect(table).toBeVisible();
  await expect(region.getByRole('row', { name: /run_demo00/ })).toBeVisible();

  await region.getByRole('button', { name: /^Empty/ }).click();
  await expect(region.getByText('No runners match these filters')).toBeVisible();

  await region.getByRole('button', { name: /^Error/ }).click();
  await expect(region.getByRole('alert')).toContainText('The controller did not answer');

  await region.getByRole('button', { name: /^Loading/ }).click();
  // Nothing in the accessibility tree distinguishes a skeleton row from a data
  // row, so this one reaches for the class the grid gives it.
  await expect(region.locator('tr.skeleton-row').first()).toBeVisible();
});

test('a toast lands in the region with the tone it was raised with', async ({ page }) => {
  await open(page, 'classic', 'dark');
  await page.getByRole('button', { name: 'Success' }).click();
  await expect(page.getByText('Pool created')).toBeVisible();

  await page.getByRole('button', { name: 'Error with an action' }).click();
  const refusal = page.getByText('The drain was refused');
  await expect(refusal).toBeVisible();
  await expect(page.getByRole('button', { name: 'Open your account' })).toBeVisible();
});

test('the Eli repair components show every state they can be in', async ({ page }) => {
  await open(page, 'mission-control', 'dark');
  const region = page.getByRole('region', { name: 'Eli repairs', exact: true });
  await expect(region).toBeVisible();

  // The case, with the pull request looked up and without it.
  const withPull = region.locator('[data-gallery="case-pull"]');
  await expect(
    withPull.getByRole('heading', { name: 'Retry transient 502s in the registry client' }),
  ).toBeVisible();
  await expect(withPull.getByRole('link', { name: /eyupio\/zoomies #1131/ })).toHaveAttribute(
    'href',
    'https://github.com/eyupio/zoomies/pull/1131',
  );
  await expect(withPull.getByRole('link', { name: 'a41c9e2' })).toHaveAttribute(
    'href',
    /\/commit\/a41c9e2f0b7d/,
  );
  await expect(region.locator('[data-gallery="case-no-pull"]')).toContainText(
    'Pull request not looked up yet',
  );

  // The job the person opened is open and its error output has been read.
  const loaded = region.locator('[data-gallery="jobs-loaded"]');
  await expect(loaded.getByText('You opened this one')).toBeVisible();
  await expect(loaded.getByText('Error output, 4 lines')).toBeVisible();
  await expect(loaded.getByText('client_test.go:88: got 1 attempt, want 3')).toBeVisible();
  await expect(loaded.getByText('Nothing here is stored', { exact: false })).toBeVisible();
  // A fleet fault says a repair is unlikely to help, and says what to do.
  await loaded.locator('details', { hasText: 'Integration (shard 3)' }).locator('summary').click();
  await expect(loaded.getByText('A repair is unlikely to help')).toBeVisible();
  await expect(loaded.getByText('Out of memory').first()).toBeVisible();

  // Without verified write access the log is not read, and the page says why.
  const locked = region.locator('[data-gallery="jobs-locked"]');
  await expect(locked.getByText('Error output needs write access')).toBeVisible();
  await expect(locked.getByText('Error output, ')).toHaveCount(0);

  // A log that is gone is an answer, and a very long line wraps.
  await expect(region.locator('[data-gallery="jobs-gone"]')).toContainText(
    'GitHub no longer has this log',
  );
  const long = region.locator('[data-gallery="jobs-long"] pre');
  await expect(long).toContainText('<script>alert("an error line is text")</script>');
  expect(await long.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);

  // The checklist names what blocked and what was never reached.
  const blocked = region.locator('[data-gallery="checks-blocked"]');
  await expect(blocked.getByRole('listitem').filter({ hasText: 'Write access' })).toContainText(
    'Your linked GitHub account needs write access',
  );
  await expect(blocked.getByText('Not checked yet.')).toHaveCount(4);
  await expect(blocked.getByRole('link', { name: /Manage access on GitHub/ })).toHaveAttribute(
    'href',
    'https://github.com/eyupio/zoomies/settings/access',
  );
  await expect(region.locator('[data-gallery="checks-passed"]')).toContainText(
    'All 8 checks passed',
  );

  // The decision card at each stage.
  await expect(
    region
      .locator('[data-gallery="decision-blocked"]')
      .getByRole('heading', { name: 'Fix write access first' }),
  ).toBeVisible();
  await expect(
    region
      .locator('[data-gallery="decision-blocked"]')
      .getByRole('button', { name: 'Check access again' }),
  ).toBeVisible();
  await expect(
    region.locator('[data-gallery="decision-ready"]').getByRole('button', { name: 'Fix with Eli' }),
  ).toBeEnabled();
  await expect(region.locator('[data-gallery="decision-ready"]')).toContainText('never merges');
  const changed = region.locator('[data-gallery="decision-changed"]');
  await expect(
    changed.getByRole('heading', { name: 'A newer commit replaced this failure' }),
  ).toBeVisible();
  await expect(changed.getByRole('button', { name: 'Check the current PR' })).toBeVisible();
  await expect(changed.getByRole('link', { name: /original run/i })).toBeVisible();
  await expect(region.locator('[data-gallery="decision-checking"]')).toContainText('Checking');
  await expect(region.locator('[data-gallery="decision-error"]').getByRole('alert')).toContainText(
    'The controller did not answer',
  );

  // The progress track puts its mark on the step the worker is on, and the steps
  // either side say what is done and what is to come.
  for (const name of ['progress-queued', 'progress-preparing', 'progress-watching']) {
    await region.locator(`[data-gallery="${name}"]`).getByText('Show the steps').click();
  }
  const track = (name: string) =>
    region
      .locator(`[data-gallery="${name}"]`)
      .getByRole('list', { name: 'What Eli is doing' })
      .getByRole('listitem');
  await expect(track('progress-queued').nth(0)).toHaveAttribute('aria-current', 'step');
  await expect(track('progress-preparing').nth(2)).toHaveAttribute('aria-current', 'step');
  await expect(track('progress-preparing').nth(1)).toContainText('Done');
  await expect(track('progress-preparing').nth(3)).toContainText('Not started');
  await expect(track('progress-watching').nth(4)).toHaveAttribute('aria-current', 'step');
  await expect(region.locator('[data-gallery="progress-preparing"]')).toContainText(
    'Elapsed 1m 03s',
  );
  await expect(region.locator('[data-gallery="progress-preparing"]')).toContainText(
    'You can leave this page',
  );

  // Each kind of ending is named by its state, and a state from a newer controller keeps its own name.
  const outcomes = region.locator('[data-gallery="outcomes"]');
  for (const heading of [
    'Could not repair',
    'Checks passed',
    'Checks failed',
    'Needs attention',
    'a-state-from-a-newer-controller',
  ]) {
    await expect(outcomes.getByRole('heading', { name: heading })).toBeVisible();
  }

  // Technical details: open once finished, shut while going, and a call whose usage
  // was never reported is unknown rather than zero.
  const finished = region.locator('[data-gallery="tech-finished"]');
  await expect(finished.locator('details.tech')).not.toHaveAttribute('open', '');
  await finished.locator('details.tech summary').first().click();
  await expect(finished.getByRole('row').nth(2).getByRole('cell')).toHaveText([
    'Unknown',
    'Unknown',
    '89 KB',
    '9.0s',
  ]);
  await expect(region.locator('[data-gallery="tech-running"] details.tech')).not.toHaveAttribute(
    'open',
    '',
  );
  await expect(region.locator('[data-gallery="tech-none"]')).toContainText(
    'No model call was recorded',
  );

  // What to try next: the advice for the code, a short list for a success, and the generic advice otherwise.
  await expect(region.locator('[data-gallery="next-failed"]')).toContainText(
    'Add a note naming the file or the test',
  );
  await expect(
    region
      .locator('[data-gallery="next-failed"]')
      .getByRole('link', { name: 'Try another attempt' }),
  ).toBeVisible();
  const passedNext = region.locator('[data-gallery="next-passed"]');
  await expect(passedNext.getByRole('heading', { name: 'What to do next' })).toBeVisible();
  await expect(passedNext.getByRole('link', { name: 'Try another attempt' })).toHaveCount(0);
  await expect(region.locator('[data-gallery="next-unknown"]')).toContainText(
    'Add a note naming the file or the test',
  );
  await expect(
    region
      .locator('[data-gallery="next-unknown"]')
      .getByRole('link', { name: /Open pull request/ }),
  ).toHaveCount(0);

  // The facts leave out what the row does not have, and the timeline holds the moments it recorded.
  const running = region.locator('[data-gallery="facts-running"]');
  await expect(running).toContainText('Personal provider');
  await expect(running).not.toContainText('Commit pushed');
  const done = region.locator('[data-gallery="facts-finished"]');
  await expect(done.getByRole('link', { name: /b7d3e91/ })).toHaveAttribute(
    'href',
    /\/commit\/b7d3e9150c2a/,
  );
  await expect(done).toContainText('Took');
  await expect(done.getByRole('region', { name: 'Timeline' })).toContainText(
    'Pushed commit b7d3e91',
  );

  // The hub's setup bar says whether the person can repair, in each state it can be in.
  const bars = region
    .locator('[data-gallery="setup-bars"]')
    .getByRole('region', { name: 'Repair settings' });
  await expect(bars.nth(0)).toContainText('Ready to repair');
  await expect(bars.nth(0)).toContainText('GitHub @dana linked');
  await expect(bars.nth(0)).toContainText('Ask me first on 3 repositories');
  await expect(bars.nth(1)).toContainText('Setup needed');
  await expect(bars.nth(1)).toContainText('repairs not allowed yet');
  await expect(bars.nth(1)).not.toContainText('Ask me first');
  await expect(bars.nth(2)).toContainText('No GitHub account linked');
  await expect(bars.nth(3)).toContainText('Sign-in is off');
  await expect(bars.nth(4)).toContainText('Checking setup');
  await expect(bars.nth(0).getByRole('link', { name: /Open settings/ })).toHaveAttribute(
    'href',
    '/repairs/setup',
  );

  // Offers say what failed and where, and a title or a job name with no break points wraps.
  const offers = region.locator('[data-gallery="offers"]');
  await expect(offers.getByRole('article')).toHaveCount(3);
  await expect(offers.getByRole('article').first()).toContainText('UI (e2e)');
  await expect(offers.getByRole('article').first()).toContainText('step 4, Install browsers');
  await expect(offers.getByRole('article').nth(1)).toContainText('step 5, Run the unit tests');
  await expect(
    offers.getByRole('link', { name: 'Review fix for eyupio/zoomies #1142' }),
  ).toHaveAttribute('href', /\/repairs\/new\?.*offer=opp_1/);
  expect(await offers.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);

  // Running repairs say which step they are on, and Follow opens the attempt.
  const going = region.locator('[data-gallery="active"]');
  await expect(going).toContainText('Preparing the fix');
  await expect(going).toContainText('Queued');
  await expect(going.getByRole('img', { name: 'Step 3 of 5: Preparing the fix' })).toBeVisible();
  await expect(
    going.getByRole('link', { name: /Follow the repair for eyupio\/zoomies #1131/ }),
  ).toHaveAttribute('href', '/repairs/eli_run1');

  // History rows open the attempt, name the failed jobs, and keep their boxes for Select mode.
  const rows = region.locator('[data-gallery="rows"]');
  await expect(rows.getByRole('checkbox')).toHaveCount(0);
  await expect(
    rows.getByRole('link', { name: 'Add a status line to the repairs page' }),
  ).toHaveAttribute('href', '/repairs/eli_pass');
  await expect(rows.getByRole('article').nth(3)).toContainText('+1 more');
  await expect(rows.getByRole('article').nth(1)).toContainText('Fix commit');
  // A repair from before cases were kept says less and does not guess.
  await expect(rows.getByRole('article').nth(4)).toContainText(
    'Pull request #1002 in eyupio/zoomies',
  );
  expect(await rows.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
  const picking = region.locator('[data-gallery="rows-selecting"]');
  await expect(picking.getByRole('checkbox')).toHaveCount(3);
  await expect(picking.getByRole('checkbox').nth(1)).toBeChecked();
  // The running repair's box says why it cannot be ticked.
  await expect(picking.getByRole('checkbox').nth(0)).toBeDisabled();

  // Every state a repair can end in has its word, and one this build does not know keeps its own.
  const badges = region.locator('[data-gallery="repair-status"]');
  for (const label of [
    'Queued',
    'Investigating',
    'Waiting for CI',
    'Checks passed',
    'Checks failed',
    'Could not repair',
    'Needs attention',
    'Interrupted',
    'Newer commit',
    'Unverified',
    'a-state-from-a-newer-controller',
  ]) {
    await expect(badges.getByText(label, { exact: true })).toBeVisible();
  }
});

// A panel with an icon draws it in a tile beside the title: the Overview's
// cards share one head, so no page draws its own.
test('a panel with an icon draws it in a tile beside the title', async ({ page }) => {
  await open(page, 'mission-control', 'light');
  const panel = page.locator('.panel', { hasText: 'Panel with a tile' }).first();
  const tile = panel.locator('header .tile');
  await expect(tile).toBeVisible();
  const [w, h, radius] = await tile.evaluate((el) => {
    const r = el.getBoundingClientRect();
    return [r.width, r.height, parseFloat(getComputedStyle(el).borderRadius)];
  });
  expect(w).toBe(h);
  expect(radius).toBeGreaterThan(0);
  await expect(tile).toHaveAttribute('aria-hidden', 'true');
});

// The shared look's one button: a pill in the brand's Runner Blue under Zoomies
// Black, which is 7.3:1 in every combination, where the darkened accent was
// needed only to carry white text. Held here so a later component cannot drift
// back to a rectangle or to the accent.
test('the primary button is a pill in Runner Blue with Zoomies Black text, in every combination', async ({
  page,
}) => {
  for (const { style, mode } of COMBINATIONS) {
    await open(page, style, mode);
    const button = page.getByRole('button', { name: 'Create a pool' }).first();
    const root = page.locator(':root');
    const [radius, background, colour, runnerBlue, black] = await Promise.all([
      button.evaluate((el) => getComputedStyle(el).borderRadius),
      button.evaluate((el) => getComputedStyle(el).backgroundColor),
      button.evaluate((el) => getComputedStyle(el).color),
      root.evaluate((el) => getComputedStyle(el).getPropertyValue('--z-brand-runner-blue').trim()),
      root.evaluate((el) => getComputedStyle(el).getPropertyValue('--z-brand-black').trim()),
    ]);
    expect(parseFloat(radius), `${style} ${mode}`).toBeGreaterThanOrEqual(999);
    expect(parseColour(background), `${style} ${mode}`).toEqual(hexToRgb(runnerBlue));
    expect(parseColour(colour), `${style} ${mode}`).toEqual(hexToRgb(black));
  }
});

// The icon buttons share the pill, and a text field takes the large radius so
// it reads as the card's child rather than a box inside it; the checkbox and
// the radio box keep the small one.
test('icon buttons are pills and text fields take the large radius', async ({ page }) => {
  await open(page, 'mission-control', 'dark');
  const large = await page
    .locator(':root')
    .evaluate((el) => getComputedStyle(el).getPropertyValue('--z-radius-lg').trim());
  const icon = page.locator('.icon-btn').first();
  expect(
    parseFloat(await icon.evaluate((el) => getComputedStyle(el).borderRadius)),
  ).toBeGreaterThanOrEqual(999);
  for (const selector of ['.field input', '.field select', '.field textarea']) {
    const control = page.locator(selector).first();
    expect(await control.evaluate((el) => getComputedStyle(el).borderRadius), selector).toBe(large);
  }
});

// A status badge is a pill in shape as well as in name. Its two heights do not
// move, so no grid row grows where a small badge sits beside a medium one.
test('a status badge is a pill and keeps its height in both sizes', async ({ page }) => {
  await open(page, 'classic', 'light');
  const badges = page.locator('.badge');
  expect(await badges.count()).toBeGreaterThan(1);
  const first = badges.first();
  expect(
    parseFloat(await first.evaluate((el) => getComputedStyle(el).borderRadius)),
  ).toBeGreaterThanOrEqual(999);
  const heights = await badges.evaluateAll((els) =>
    els.map((el) => el.getBoundingClientRect().height),
  );
  const [md, sm] = await page.locator(':root').evaluate((el) => {
    const s = getComputedStyle(el);
    const rem = parseFloat(s.fontSize);
    return [
      parseFloat(s.getPropertyValue('--z-space-5')) * rem,
      parseFloat(s.getPropertyValue('--z-space-4')) * rem,
    ];
  });
  for (const h of heights) expect([Math.round(md), Math.round(sm)]).toContain(Math.round(h));
});

// A command the operator runs is shown in a window on the dark stage, the
// same black in every combination, and a long one wraps inside it on a phone
// rather than widening the page.
test('the terminal is a window on the dark stage that wraps a long command', async ({ page }) => {
  for (const { style, mode } of COMBINATIONS) {
    await open(page, style, mode);
    const terminal = page.locator('.terminal').first();
    const [background, black, radius] = await Promise.all([
      terminal.evaluate((el) => getComputedStyle(el).backgroundColor),
      page
        .locator(':root')
        .evaluate((el) => getComputedStyle(el).getPropertyValue('--z-brand-black').trim()),
      terminal.evaluate((el) => getComputedStyle(el).borderRadius),
    ]);
    expect(parseColour(background), `${style} ${mode}`).toEqual(hexToRgb(black));
    expect(radius).not.toBe('0px');
    await expect(terminal.locator('.terminal-bar')).toContainText('sam@lab-vm-2');
    await expect(terminal.getByRole('button', { name: 'Copy the command' })).toBeVisible();
  }
  await page.setViewportSize({ width: 375, height: 800 });
  await open(page, 'mission-control', 'dark');
  const overflow = await page
    .locator('.terminal')
    .first()
    .evaluate((el) => el.scrollWidth - el.clientWidth);
  expect(overflow).toBe(0);
});

// The copy control inside the window is the one control on the stage, so its
// edge has to read against the black (the 3:1 bar for a control's edge), and
// it is a pill like every other button, not the one rectangle left.
test('the copy control inside the terminal is a pill whose edge reads on the stage', async ({
  page,
}) => {
  for (const { style, mode } of COMBINATIONS) {
    await open(page, style, mode);
    const terminal = page.locator('.terminal').first();
    const control = terminal.getByRole('button', { name: 'Copy the command' });
    const [background, edge, radius, height] = await Promise.all([
      terminal.evaluate((el) => getComputedStyle(el).backgroundColor),
      control.evaluate((el) => getComputedStyle(el).borderTopColor),
      control.evaluate((el) => parseFloat(getComputedStyle(el).borderTopLeftRadius)),
      control.evaluate((el) => el.getBoundingClientRect().height),
    ]);
    expect(contrast(edge, background), `${style} ${mode} edge`).toBeGreaterThanOrEqual(3);
    expect(radius, `${style} ${mode} radius`).toBeGreaterThanOrEqual(height / 2);
  }
});

// A block of facts about one thing stands on the dark stage as the terminal
// does, and on a phone it is one column in which an unbroken value wraps.
test('the facts panel is on the dark stage and holds an unbroken value', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  await open(page, 'mission-control', 'light');
  const facts = page.locator('.facts-panel').first();
  const [background, black] = await Promise.all([
    facts.evaluate((el) => getComputedStyle(el).backgroundColor),
    page
      .locator(':root')
      .evaluate((el) => getComputedStyle(el).getPropertyValue('--z-brand-black').trim()),
  ]);
  expect(parseColour(background)).toEqual(hexToRgb(black));
  expect(await facts.evaluate((el) => el.scrollWidth - el.clientWidth)).toBe(0);
  expect(
    await facts.evaluate((el) => getComputedStyle(el).gridTemplateColumns.split(' ').length),
  ).toBe(1);
});
