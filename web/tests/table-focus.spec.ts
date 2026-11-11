/**
 * A table's focus view: the one table, the whole page.
 *
 * Every grid offers it, from its own toolbar and from `f`, and it is a query
 * parameter on the page's own address rather than a page of its own, so the
 * same table instance carries everything across: the filters, the column
 * layout, an open menu or drawer. What this file holds true is the shape of
 * that promise on Runners in detail, and the way in, out and back on two pages
 * whose surroundings differ most from it: Jobs, with its filter bar, analysis
 * cards and folded fleet context, and Pools, with its automatic pools panel,
 * its demand fold and a header menu of its own.
 */
import { expect, test, type Locator, type Page } from '@playwright/test';
import {
  FIXTURE,
  dataRows,
  goto,
  grid,
  nav,
  openColumnPanel,
  closeColumnPanel,
  reload,
} from './support/fixtures';

const focusBar = (page: Page): Locator => page.getByTestId('focus-bar');

/** Press the grid's own Focus control, as an operator does. */
async function enterFocus(page: Page): Promise<void> {
  await page.getByRole('link', { name: 'Focus', exact: true }).click();
  await expect(focusBar(page)).toBeVisible();
}

/** Everything that is not the table is gone: the shell's chrome and the page's own. */
async function expectOnlyTheTable(page: Page, band: string): Promise<void> {
  await expect(nav(page), 'the navigation landmark is hidden').toHaveCount(0);
  await expect(page.locator('#page-heading'), 'the page header is hidden').toBeHidden();
  await expect(page.getByTestId(band), 'the band is hidden').toBeHidden();
  await expect(page.getByRole('contentinfo'), 'the footer is hidden').toHaveCount(0);
}

/** And back again: the whole page, at the address it had before focus. */
async function expectWholePage(page: Page, band: string): Promise<void> {
  await expect(focusBar(page)).toHaveCount(0);
  await expect(nav(page)).toBeVisible();
  await expect(page.locator('#page-heading')).toBeVisible();
  await expect(page.getByTestId(band)).toBeVisible();
}

function focusParam(page: Page): string | null {
  return new URL(page.url()).searchParams.get('focus');
}

test.describe('on Runners', () => {
  const runners = (page: Page) => grid(page, 'Runners');

  test('Focus shows the table, its chips and the bar, and nothing else', async ({ page }) => {
    // A grid's bookkeeping runs in effects, and one that fed itself would show
    // only here: as an error the page swallowed while it went on drawing.
    const errors: string[] = [];
    page.on('pageerror', (error) => errors.push(error.message));
    await goto(page, '/runners', 'Runners');
    await expect(dataRows(runners(page)).first()).toBeVisible();
    await enterFocus(page);

    expect(focusParam(page)).toBe('runners');
    await expectOnlyTheTable(page, 'band-runners');
    await expect(page.getByText('Fleet context', { exact: true })).toBeHidden();

    await expect(runners(page)).toBeVisible();
    await expect(page.getByRole('group', { name: 'Filter by runner state' })).toBeVisible();
    await expect(focusBar(page).getByRole('heading', { level: 1, name: 'Runners' })).toBeVisible();
    await expect(focusBar(page).getByRole('button', { name: 'Refresh' })).toBeVisible();
    await expect(focusBar(page).getByRole('link', { name: 'Exit focus' })).toBeVisible();
    await expect(page.getByRole('navigation', { name: 'Pagination' })).toBeVisible();

    // The heading row stays while the rows scroll: the frame scrolls, not the page.
    const scrolls = await runners(page).evaluate((table) => {
      const frame = table.parentElement as HTMLElement;
      const style = getComputedStyle(frame);
      return {
        frame: style.overflowY,
        document: document.documentElement.scrollHeight - document.documentElement.clientHeight,
      };
    });
    expect(scrolls.frame).toBe('auto');
    expect(scrolls.document, 'the page itself scrolls in focus').toBeLessThanOrEqual(1);

    await focusBar(page).getByRole('link', { name: 'Exit focus' }).click();
    await expectWholePage(page, 'band-runners');
    expect(errors).toEqual([]);
  });

  test('a filter chosen before focus holds in focus and after it', async ({ page }) => {
    await goto(page, '/runners', 'Runners');
    const chips = page.getByRole('group', { name: 'Filter by runner state' });
    await chips.getByRole('button', { name: /^Busy/ }).click();
    await expect(page).toHaveURL(/state=busy/);
    await expect(dataRows(runners(page)).first()).toContainText('Busy');

    await enterFocus(page);
    await expect(page).toHaveURL(/state=busy/);
    await expect(chips.getByRole('button', { name: /^Busy/ })).toHaveAttribute(
      'aria-pressed',
      'true',
    );

    await focusBar(page).getByRole('link', { name: 'Exit focus' }).click();
    await expect(page).toHaveURL(/\/runners\?state=busy$/);
    await expect(chips.getByRole('button', { name: /^Busy/ })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
  });

  test('a column hidden in focus stays hidden after it', async ({ page }) => {
    await goto(page, '/runners', 'Runners');
    await enterFocus(page);
    const pool = runners(page).locator('thead th[data-table-column="pool"]');
    await expect(pool).toHaveCount(1);

    const panel = await openColumnPanel(page);
    await panel.getByRole('checkbox', { name: 'Pool', exact: true }).uncheck();
    await closeColumnPanel(page);
    await expect(pool).toHaveCount(0);

    await page.keyboard.press('Escape');
    await expect(focusBar(page)).toHaveCount(0);
    await expect(pool).toHaveCount(0);
  });

  test('Exit focus and Esc both return to the same address without the parameter', async ({
    page,
  }) => {
    await goto(page, '/runners?sort=name&order=asc', 'Runners');
    await enterFocus(page);
    await expect(page).toHaveURL(/\/runners\?sort=name&order=asc&focus=runners$/);

    const exit = focusBar(page).getByRole('link', { name: 'Exit focus' });
    // A real link to where it goes, so it can be opened elsewhere or copied.
    await expect(exit).toHaveAttribute('href', '/runners?sort=name&order=asc');
    await exit.click();
    await expect(page).toHaveURL(/\/runners\?sort=name&order=asc$/);
    await expectWholePage(page, 'band-runners');

    // Back and Forward go through it, since it is an address.
    await page.goBack();
    await expect(focusBar(page)).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page).toHaveURL(/\/runners\?sort=name&order=asc$/);
    await expectWholePage(page, 'band-runners');
  });

  test('reloading a focus address opens in focus, and f comes back from it', async ({ page }) => {
    await goto(page, '/runners?focus=runners', 'Runners');
    await expect(focusBar(page)).toBeVisible();
    await expectOnlyTheTable(page, 'band-runners');
    await reload(page, 'Runners');
    await expect(focusBar(page)).toBeVisible();

    await page.keyboard.press('f');
    await expectWholePage(page, 'band-runners');
    expect(focusParam(page)).toBeNull();
    await page.keyboard.press('f');
    await expect(focusBar(page)).toBeVisible();
    expect(focusParam(page)).toBe('runners');
  });

  test('R refreshes the page from focus', async ({ page }) => {
    await goto(page, '/runners?focus=runners', 'Runners');
    await expect(dataRows(runners(page)).first()).toBeVisible();
    const refresh = focusBar(page).getByRole('button', { name: 'Refresh' });
    await page.keyboard.press('r');
    await expect(refresh).toHaveAttribute('aria-busy', 'true');
    await expect(refresh).not.toHaveAttribute('aria-busy', 'true');
  });

  test('Full screen is offered where the browser allows it, and absent where not', async ({
    page,
  }) => {
    await goto(page, '/runners?focus=runners', 'Runners');
    const supported = await page.evaluate(() => document.fullscreenEnabled);
    await expect(focusBar(page).getByRole('button', { name: 'Full screen' })).toHaveCount(
      supported ? 1 : 0,
    );

    await page.addInitScript(() => {
      Object.defineProperty(Document.prototype, 'fullscreenEnabled', { get: () => false });
    });
    await reload(page, 'Runners');
    await expect(focusBar(page)).toBeVisible();
    await expect(focusBar(page).getByRole('button', { name: 'Full screen' })).toHaveCount(0);
  });

  test('a row menu and the confirmation it opens still work in focus', async ({ page }) => {
    await goto(page, '/runners?focus=runners', 'Runners');
    const row = dataRows(runners(page)).filter({ hasText: FIXTURE.busyRunner }).first();
    await expect(row).toBeVisible();

    const trigger = page.getByRole('button', { name: `Actions for ${FIXTURE.busyRunner}` });
    await trigger.click();
    await expect(page.getByRole('menu')).toBeVisible();
    // The first Esc belongs to the menu, not to focus.
    await page.keyboard.press('Escape');
    await expect(page.getByRole('menu')).toHaveCount(0);
    expect(focusParam(page)).toBe('runners');

    await trigger.click();
    await page.getByRole('menuitem', { name: 'Drain', exact: true }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText(FIXTURE.busyRunner);
    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await expect(dialog).toBeHidden();
    await expect(focusBar(page)).toBeVisible();
  });

  test('on a phone every control in the bar is big enough for a finger', async ({
    page,
    isMobile,
  }) => {
    test.skip(!isMobile, 'the touch sizes, in the Pixel 7 project');
    await goto(page, '/runners?focus=runners', 'Runners');
    await expect(dataRows(runners(page)).first()).toBeVisible();
    for (const control of await focusBar(page).locator('a, button').all()) {
      const box = await control.boundingBox();
      expect(box?.height ?? 0, `${await control.innerText()} is too short`).toBeGreaterThanOrEqual(
        44,
      );
    }
    const sideways = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(sideways, 'the page scrolls sideways in focus').toBeLessThanOrEqual(1);
  });
});

test.describe('on the other grid pages', () => {
  test('Jobs goes into focus, opens a job, and comes back to the same address', async ({
    page,
  }) => {
    await goto(page, '/jobs?state=completed', 'Jobs');
    const jobs = grid(page, 'Jobs');
    await expect(dataRows(jobs).first()).toBeVisible();
    await enterFocus(page);
    await expect(page).toHaveURL(/\/jobs\?state=completed&focus=jobs$/);
    await expectOnlyTheTable(page, 'band-jobs');
    await expect(page.getByText('Fleet context and activity')).toBeHidden();

    // The job drawer is the page's, outside the table, and still opens over it.
    await dataRows(jobs).first().click();
    const drawer = page.getByRole('dialog');
    await expect(drawer).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(drawer).toHaveCount(0);
    expect(focusParam(page)).toBe('jobs');

    await reload(page, 'Jobs');
    await expect(focusBar(page)).toBeVisible();
    await focusBar(page).getByRole('link', { name: 'Exit focus' }).click();
    await expect(page).toHaveURL(/\/jobs\?state=completed$/);
    await expectWholePage(page, 'band-jobs');
  });

  test('Pools goes into focus, reloads in it, and comes back with Esc', async ({ page }) => {
    await goto(page, '/pools', 'Pools');
    await expect(dataRows(grid(page, 'Pools')).first()).toBeVisible();
    await enterFocus(page);
    await expect(page).toHaveURL(/\/pools\?focus=pools$/);
    await expectOnlyTheTable(page, 'band-pools');
    await expect(grid(page, 'Pools')).toBeVisible();

    await reload(page, 'Pools');
    await expect(focusBar(page)).toBeVisible();
    await expect(grid(page, 'Pools')).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page).toHaveURL(/\/pools$/);
    await expectWholePage(page, 'band-pools');
  });
});
