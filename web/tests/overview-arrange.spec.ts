/**
 * Arranging the Overview.
 *
 * The body is a list of panels, and an operator may put them in their own
 * order and hide the ones they never read. What is protected: the controls
 * exist only while arranging, every move is reversible by keyboard, a hidden
 * panel can always be brought back, the layout outlives the visit, and the
 * grid never breaks whatever order it is given.
 */
import { expect, test, type Page } from '@playwright/test';
import { goto, reload } from './support/fixtures';
import { centre, touchDrag } from './support/gestures';

const ORDER = ['attention', 'pools', 'failures', 'jobs', 'events', 'history', 'diagnostics'];
const TITLES = [
  'Needs attention',
  'Pools',
  'Recent failures',
  'Active jobs',
  'Recent events',
  'History',
  'Diagnostics',
];

const order = (page: Page) =>
  page
    .locator('[data-panel]')
    .evaluateAll((nodes) => nodes.map((n) => n.getAttribute('data-panel')));

test.beforeEach(async ({ page }) => {
  // Each test starts from the default layout: the browser's copy is what a
  // signed-out fixture keeps, and a test must not inherit the last one's.
  await page.addInitScript(() => {
    // Once per test, not on every load: a reload inside a test has to keep
    // what the test arranged.
    if (sessionStorage.getItem('arrange-spec-reset')) return;
    sessionStorage.setItem('arrange-spec-reset', '1');
    const raw = localStorage.getItem('zoomies.prefs');
    if (!raw) return;
    try {
      const prefs = JSON.parse(raw) as Record<string, unknown>;
      delete prefs.panelLayouts;
      localStorage.setItem('zoomies.prefs', JSON.stringify(prefs));
    } catch {
      /* a copy nobody can read is a copy the page ignores anyway */
    }
  });
  await goto(page, '/', 'Overview');
});

async function arrange(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Arrange' }).click();
  await expect(page.getByRole('region', { name: 'Arrange the Overview' })).toBeVisible();
}

test('Arrange shows the controls on every card and Done puts them away', async ({ page }) => {
  await expect(page.getByRole('toolbar', { name: /^Arrange / })).toHaveCount(0);
  await arrange(page);
  await expect(page.getByRole('toolbar', { name: /^Arrange / })).toHaveCount(7);
  await expect(page.getByRole('button', { name: 'Move Needs attention up' })).toHaveAttribute(
    'aria-disabled',
    'true',
  );
  await expect(page.getByRole('button', { name: 'Move Diagnostics down' })).toHaveAttribute(
    'aria-disabled',
    'true',
  );
  await page.getByRole('button', { name: 'Done' }).click();
  await expect(page.getByRole('toolbar', { name: /^Arrange / })).toHaveCount(0);
});

test('Move down then Move up restores the order, with focus staying on the button', async ({
  page,
}) => {
  await arrange(page);
  await page.getByRole('button', { name: 'Move Pools down' }).click();
  expect(await order(page)).toEqual([
    'attention',
    'failures',
    'pools',
    'jobs',
    'events',
    'history',
    'diagnostics',
  ]);
  await expect(page.getByRole('button', { name: 'Move Pools down' })).toBeFocused();
  await page.getByRole('button', { name: 'Move Pools up' }).click();
  expect(await order(page)).toEqual(ORDER);
});

test('a hidden panel leaves a Show row while arranging and nothing when done, and the layout survives a reload', async ({
  page,
}) => {
  await arrange(page);
  await page.getByRole('button', { name: 'Hide History' }).click();
  await expect(page.getByText('History is hidden.')).toBeVisible();
  await expect(page.locator('#history')).toHaveCount(0);
  await expect(page.getByRole('region', { name: 'Arrange the Overview' })).toContainText(
    '1 panel hidden',
  );
  await page.getByRole('button', { name: 'Done' }).click();
  await expect(page.getByText('History is hidden.')).toHaveCount(0);
  await reload(page, 'Overview');
  await expect(page.locator('#pools')).toBeVisible();
  await expect(page.locator('#history')).toHaveCount(0);
  await arrange(page);
  await page.getByRole('button', { name: 'Show History' }).click();
  await expect(page.locator('#history')).toHaveCount(1);
});

test('Reset layout puts the default back and is disabled at the default', async ({ page }) => {
  await arrange(page);
  await expect(page.getByRole('button', { name: 'Reset layout' })).toHaveAttribute(
    'aria-disabled',
    'true',
  );
  await page.getByRole('button', { name: 'Move Pools down' }).click();
  await page.getByRole('button', { name: 'Hide History' }).click();
  await page.getByRole('button', { name: 'Reset layout' }).click();
  expect(await order(page)).toEqual(ORDER);
  await expect(page.getByRole('button', { name: 'Reset layout' })).toHaveAttribute(
    'aria-disabled',
    'true',
  );
});

test('the events panel moved above the jobs stands alone, with no horizontal overflow', async ({
  page,
}) => {
  await arrange(page);
  await page.getByRole('button', { name: 'Move Recent events up' }).click();
  expect((await order(page)).slice(3, 5)).toEqual(['events', 'jobs']);
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth - innerWidth),
  ).toBeLessThanOrEqual(0);
});

test('hiding every panel leaves Show rows, and Reset brings them all back', async ({ page }) => {
  await arrange(page);
  for (const title of TITLES) await page.getByRole('button', { name: `Hide ${title}` }).click();
  await expect(page.getByRole('button', { name: /^Show / })).toHaveCount(7);
  await expect(page.locator('[data-panel]')).toHaveCount(0);
  await page.getByRole('button', { name: 'Reset layout' }).click();
  expect(await order(page)).toEqual(ORDER);
});

test('a card can be dragged by its grip to a new place', async ({ page }) => {
  await arrange(page);
  const grip = page.getByRole('button', { name: 'Move Pools', exact: true });
  const target = page.locator('[data-panel="attention"]');
  const from = await centre(grip);
  const to = await centre(target);
  await touchDrag(grip, [from, { x: from.x, y: from.y - 40 }, to]);
  expect((await order(page)).slice(0, 2)).toEqual(['pools', 'attention']);
});

test("Appearance lists the Overview's panels as switches, and one switched off is hidden on the page", async ({
  page,
}) => {
  await goto(page, '/settings/appearance', 'Appearance');
  const group = page.getByRole('group', { name: 'Overview panels' });
  await expect(group.getByRole('switch')).toHaveCount(7);
  await group.getByRole('switch', { name: 'History on the Overview' }).click();
  await goto(page, '/', 'Overview');
  await expect(page.locator('#pools')).toBeVisible();
  await expect(page.locator('#history')).toHaveCount(0);
  await goto(page, '/settings/appearance', 'Appearance');
  await page
    .getByRole('group', { name: 'Overview panels' })
    .getByRole('button', { name: 'Reset layout' })
    .click();
  await goto(page, '/', 'Overview');
  await expect(page.locator('#history')).toHaveCount(1);
});

// The jobs and the events share a row whose slots stretch their card to the
// row's height; the toolbar above each must not stretch with it.
test('the toolbar over a card in the shared row stays one line tall', async ({ page }) => {
  test.skip((page.viewportSize()?.width ?? 0) < 1181, 'a narrow screen stacks the row');
  await arrange(page);
  for (const id of ['jobs', 'events']) {
    const bar = page.locator(`[data-panel="${id}"] [role="toolbar"]`);
    const box = (await bar.boundingBox())!;
    expect(box.height, `${id} toolbar height`).toBeLessThan(56);
    expect(box.width, `${id} toolbar width`).toBeGreaterThan(box.height);
  }
});
