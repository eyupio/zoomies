/**
 * A table's focus view: the one table, the whole page.
 *
 * Every grid offers it, from its own toolbar and from `f`, and it is a query
 * parameter on the page's own address rather than a page of its own, so the
 * same table instance carries everything across: the filters, the column
 * layout, an open menu or drawer. The controls that choose a filter do not come
 * with it, only the line saying which are in effect. What this file holds true is the shape of
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
  readThrough,
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

  test('Focus shows the table and the bar, and nothing else', async ({ page, isMobile }) => {
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
    // The card's head chooses what the list shows, and that is done on the
    // whole page: in focus it took half a tablet's screen from the rows.
    await expect(page.getByRole('heading', { name: 'All runners' })).toBeHidden();
    await expect(page.getByRole('searchbox', { name: 'Search runners' })).toBeHidden();
    await expect(page.getByRole('combobox', { name: 'Filter by pool' })).toBeHidden();
    await expect(page.getByRole('group', { name: 'Filter by runner state' })).toBeHidden();
    await expect(page.getByRole('button', { name: 'Columns' })).toBeVisible();
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
    // On a desktop the rows scroll in their frame, so the pagination is at
    // the foot of the window rather than somewhere below it.
    if (!isMobile) {
      const pagination = await page
        .getByRole('navigation', { name: 'Pagination' })
        .evaluate((el) => el.getBoundingClientRect().bottom);
      expect(pagination, 'the pagination is below the window').toBeLessThanOrEqual(
        page.viewportSize()!.height,
      );
    }

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
    // The chips that chose it are gone; the line that says so is not.
    await expect(chips).toBeHidden();
    await expect(page.getByRole('group', { name: 'Filters in effect' })).toContainText('Busy');
    await expect(dataRows(runners(page)).first()).toContainText('Busy');

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

test.describe('what focus keeps, says and does with the keyboard', () => {
  // A pool filter lives outside the card, in the "filters in effect" row. A
  // focus view that hid it showed a narrowed list with no sign of why and no
  // way to widen it.
  test('a filter in effect from outside the card is shown and can be cleared in focus', async ({
    page,
  }) => {
    const pools = (await page.request.get('/api/v1/pools').then((r) => r.json())) as {
      items: { id: string; name: string }[];
    };
    const pool = pools.items.find((p) => p.name === FIXTURE.linuxPool)!;
    await goto(page, `/runners?pool_id=${pool.id}&focus=runners`, 'Runners');
    await expect(focusBar(page)).toBeVisible();
    const inEffect = page.getByRole('group', { name: 'Filters in effect' });
    await expect(inEffect).toBeVisible();
    await inEffect.getByRole('button', { name: 'Clear all' }).click();
    await expect(page).toHaveURL(/\/runners\?focus=runners$/);
    await expect(focusBar(page)).toBeVisible();
  });

  // The job filters are a bar above the card rather than its head, and the
  // same holds for them: the search and the menus go, what they chose stays.
  test('on Jobs the filter controls go in focus and what they chose stays', async ({ page }) => {
    await goto(page, '/jobs?state=completed&q=lint&focus=jobs', 'Jobs');
    await expect(focusBar(page)).toBeVisible();
    await expect(page.getByRole('searchbox', { name: 'Search jobs' })).toBeHidden();
    await expect(page.getByRole('group', { name: 'Filter jobs by status' })).toBeHidden();
    const inEffect = page.getByRole('group', { name: 'Filters in effect' });
    await expect(inEffect).toContainText('lint');
    // The pressed view chip went with the card's head, so the view is said
    // here instead: on the whole page that would be the same thing twice.
    await expect(inEffect).toContainText('Finished');
    await inEffect.getByRole('button', { name: /^Remove the .* filter lint$/ }).click();
    await expect(page).not.toHaveURL(/q=lint/);
    expect(focusParam(page)).toBe('jobs');

    // The view comes off from here too, and with nothing left in effect the
    // bar is not an empty box above the table.
    await inEffect.getByRole('button', { name: 'Remove the Showing filter Finished' }).click();
    await expect(inEffect).toHaveCount(0);
    await expect(page.getByRole('searchbox', { name: 'Search jobs' })).toBeHidden();
    expect(focusParam(page)).toBe('jobs');

    // On the whole page the pressed chip says the view, so no chip repeats it.
    await goto(page, '/jobs?state=completed&focus=jobs', 'Jobs');
    await expect(inEffect).toContainText('Finished');
    await focusBar(page).getByRole('link', { name: 'Exit focus' }).click();
    await expect(page.getByRole('group', { name: 'Filter jobs by status' })).toBeVisible();
    await expect(inEffect).toHaveCount(0);
  });

  test('going in and out is announced as a view, and the keyboard comes back to Focus', async ({
    page,
  }) => {
    await goto(page, '/runners', 'Runners');
    await expect(dataRows(grid(page, 'Runners')).first()).toBeVisible();
    const announcer = page.locator('#route-announcer');
    await enterFocus(page);
    await expect(announcer).toHaveText('Runners table, focus view');
    await expect(page.locator('#focus-heading')).toBeFocused();

    await page.keyboard.press('Escape');
    await expect(announcer).toHaveText('Runners, whole page');
    const focusLink = page.getByRole('link', { name: 'Focus', exact: true });
    await expect(focusLink).toBeFocused();
  });

  // A reload of a focus address must not show the whole shell first and then
  // take it away: watched from the first frame the page draws.
  test('reloading a focus address never shows the navigation', async ({ page }) => {
    await page.addInitScript(() => {
      const w = window as unknown as { navSeen?: boolean };
      w.navSeen = false;
      const look = (): void => {
        const nav = document.querySelector('nav[aria-label="Sections"]');
        if (nav && nav.getClientRects().length > 0) w.navSeen = true;
        requestAnimationFrame(look);
      };
      requestAnimationFrame(look);
    });
    await goto(page, '/runners?focus=runners', 'Runners');
    await expect(focusBar(page)).toBeVisible();
    await expect(dataRows(grid(page, 'Runners')).first()).toBeVisible();
    await reload(page, 'Runners');
    await expect(dataRows(grid(page, 'Runners')).first()).toBeVisible();
    expect(
      await page.evaluate(() => (window as unknown as { navSeen?: boolean }).navSeen),
      'the navigation was drawn before focus took the page',
    ).toBe(false);

    // And an address naming no table here is the ordinary page.
    await goto(page, '/runners?focus=nothing-here', 'Runners');
    await expect(nav(page)).toBeVisible();
    await expect(focusBar(page)).toHaveCount(0);
  });

  // An upgrade waiting under the tab turns the next navigation into a page
  // load, which here would throw away the ticked rows: focus changes only the
  // query string, so it stays in the document.
  test('Focus does not reload the page for a waiting upgrade', async ({ page }) => {
    await goto(page, '/runners', 'Runners');
    await expect(dataRows(grid(page, 'Runners')).first()).toBeVisible();
    await page.evaluate(() => ((window as unknown as { marker?: string }).marker = 'kept'));
    await page.route('**/api/v1/meta', async (route) => {
      const read = await readThrough<Record<string, unknown>>(route);
      if (!read) return;
      await route.fulfill({ response: read.response, json: { ...read.body, version: 'newer' } });
    });
    const asked = page.waitForResponse('**/api/v1/meta');
    await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
    await asked;

    await enterFocus(page);
    expect(
      await page.evaluate(() => (window as unknown as { marker?: string }).marker),
      'pressing Focus reloaded the page',
    ).toBe('kept');
  });
});
