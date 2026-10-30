import { expect, test } from '@playwright/test';
import { goto } from './support/fixtures';

for (const style of ['classic', 'mission-control']) {
  for (const mode of ['light', 'dark']) {
    test(`${style} ${mode} keeps attention and current work ahead of collapsed detail`, async ({
      page,
    }, info) => {
      const width = info.project.name === 'mobile' ? 375 : 1440;
      await page.setViewportSize({ width, height: 900 });
      await page.addInitScript(
        ({ style, mode }) => {
          localStorage.setItem('zoomies.ui-style', style);
          localStorage.setItem('zoomies.theme', mode);
        },
        { style, mode },
      );
      const errors: string[] = [];
      page.on('pageerror', (error) => errors.push(error.message));
      await goto(page, '/', 'Overview');
      const pools = page.getByRole('region', { name: 'Pools', exact: true });
      const jobs = page.getByRole('region', { name: 'Active jobs', exact: true });
      await expect(pools.getByRole('listitem').first()).toBeVisible();
      await expect(jobs.getByRole('listitem').first()).toBeVisible();
      await expect(page.getByRole('combobox', { name: 'Overview scope' })).toHaveCount(1);
      await expect(page.getByRole('switch', { name: 'Other runners' })).toHaveCount(0);
      for (const id of ['history', 'capacity', 'startup'])
        await expect(page.locator(`#${id}`)).not.toHaveAttribute('open');
      await expect(page.locator('#history > summary')).toContainText(/finished/);
      expect((await pools.getByRole('listitem').first().boundingBox())!.y).toBeLessThan(
        width === 375 ? 800 : 1000,
      );
      const order = await page
        .locator('#now, #pools, #jobs, #events, #history, #diagnostics')
        .evaluateAll((nodes) => nodes.map((n) => n.id));
      expect(order).toEqual(['now', 'pools', 'jobs', 'events', 'history', 'diagnostics']);
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth - innerWidth),
      ).toBeLessThanOrEqual(1);
      await expect(
        page.getByRole('region', { name: 'Recent events', exact: true }).getByRole('listitem'),
      ).toHaveCount(3);
      const launcher = page.getByRole('button', { name: 'Ask Eli', exact: true });
      const before = (await launcher.boundingBox())!;
      const rightClearance = width - before.x - before.width;
      expect(rightClearance).toBeGreaterThanOrEqual(16);
      expect(rightClearance).toBeLessThan(32);
      const clearance = 900 - before.y - before.height;
      expect(clearance).toBeGreaterThanOrEqual(16);
      expect(clearance).toBeLessThanOrEqual(96);
      if (width === 375) {
        const nav = (await page
          .getByRole('navigation', { name: 'Sections', exact: true })
          .boundingBox())!;
        expect(before.y + before.height).toBeLessThan(nav.y);
      }
      await page.screenshot({ path: info.outputPath(`${style}-${mode}-${width}.png`) });
      await page.evaluate(() => scrollTo(0, document.documentElement.scrollHeight));
      await expect
        .poll(async () => {
          const after = (await launcher.boundingBox())!;
          return Math.abs(after.x - before.x) + Math.abs(after.y - before.y);
        })
        .toBeLessThan(1);
      if (style === 'mission-control' && mode === 'dark') {
        await page.screenshot({ path: info.outputPath(`${style}-${mode}-${width}-scrolled.png`) });
      }
      expect(errors).toEqual([]);
    });
  }
}

test('jump links reveal history, retain its range and remember explicit disclosure choices', async ({
  page,
}, info) => {
  // The jump bar is for screens narrower than 1181px: on a wider one the page does not need it.
  await page.setViewportSize({ width: info.project.name === 'mobile' ? 375 : 1100, height: 900 });
  await goto(page, '/', 'Overview');
  const nav = page.getByRole('navigation', { name: 'Overview sections' });
  if (info.project.name === 'mobile')
    await nav.getByRole('button', { name: 'More overview sections', exact: true }).click();
  await nav.getByRole('link', { name: 'History', exact: true }).filter({ visible: true }).click();
  const history = page.locator('#history');
  await expect(history).toHaveAttribute('open');
  await expect(page).toHaveURL(/#history$/);
  await expect(history.locator('summary')).toBeFocused();
  expect((await history.boundingBox())!.y).toBeGreaterThanOrEqual(44);
  await history
    .getByRole('group', { name: 'Range' })
    .getByRole('button', { name: 'The last 7 days, by the hour' })
    .click();
  await history.locator('summary').click();
  await expect(history).not.toHaveAttribute('open');
  // Clear the fragment: following it on reload intentionally opens its target.
  await page.evaluate(() => window.history.replaceState(null, '', '/'));
  await page.reload();
  await expect(history).not.toHaveAttribute('open');
  await history.locator('summary').click();
  await expect(
    history.getByRole('button', { name: 'The last 7 days, by the hour' }),
  ).toHaveAttribute('aria-pressed', 'true');
  await page.reload();
  await expect(history).toHaveAttribute('open');
});

test('a bookmarked diagnostic opens and a closed history summary remains live', async ({
  page,
}) => {
  await goto(page, '/#startup', 'Overview');
  await expect(page.locator('#startup')).toHaveAttribute('open');
  await expect(page.getByText('Runner startup p50', { exact: true })).toBeVisible();
  await expect(page.locator('#history')).not.toHaveAttribute('open');
  await expect(page.locator('#history > summary')).toContainText('finished');
  await page.getByRole('combobox', { name: 'Overview scope' }).selectOption('all');
  await expect(page.getByRole('link', { name: /^Queued jobs/ })).toHaveAttribute(
    'href',
    /all=true/,
  );
  await expect(page.locator('#history > summary')).toContainText('This fleet');
  // The figures and their scope note are the band's now, so the note is read
  // where the figures are rather than inside the work section under them.
  await expect(page.getByText('Runners and hosts belong to this fleet.')).toBeVisible();
});

test('events expand in place and return to a short preview', async ({ page }) => {
  await goto(page, '/', 'Overview');
  const feed = page.getByRole('region', { name: 'Recent events', exact: true });
  await expect(feed.getByRole('listitem')).toHaveCount(3);
  await feed.getByRole('button', { name: 'Expand events' }).click();
  await expect.poll(() => feed.getByRole('listitem').count()).toBeGreaterThan(3);
  await expect(feed.getByRole('link', { name: 'Choose' })).toBeVisible();
  await feed.getByRole('button', { name: 'Show fewer events' }).click();
  await expect(feed.getByRole('listitem')).toHaveCount(3);
});

test('the page menu has its own name and supports Escape without opening global navigation', async ({
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 900 });
  await goto(page, '/', 'Overview');
  const more = page.getByRole('button', { name: 'More overview sections', exact: true });
  await more.focus();
  await page.keyboard.press('Enter');
  const destinations = page.getByRole('group', { name: 'More overview sections' });
  await expect(destinations).toBeVisible();
  await destinations.getByRole('link', { name: 'History' }).focus();
  await page.keyboard.press('Escape');
  await expect(destinations).toBeHidden();
  await expect(more).toBeFocused();
  await expect(page.getByRole('button', { name: 'More', exact: true })).toHaveCount(1);
  await expect(page.getByRole('dialog', { name: 'All sections' })).toBeHidden();
});

test('loading figures are unknown rather than zero or an unfinished setup claim', async ({
  page,
}) => {
  await page.route('**/api/v1/events*', (route) => route.abort());
  await page.route('**/api/v1/stats*', () => {});
  await goto(page, '/', 'Overview');
  await expect(page.getByRole('link', { name: /^Queued jobs/ }).getByText(/^0$/)).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Finish setting up' })).toBeHidden();
  await expect(page.locator('#startup > summary')).toContainText('Loading timings');
});

test('a failed first fetch offers a retry instead of presenting current figures', async ({
  page,
}) => {
  await page.route('**/api/v1/events*', (route) => route.abort());
  await page.route('**/api/v1/stats*', (route) =>
    route.fulfill({
      status: 500,
      json: { error: { code: 'internal_error', message: 'the fleet could not be read' } },
    }),
  );
  await goto(page, '/', 'Overview');
  await expect(
    page.getByRole('alert').getByText('The fleet could not be loaded', { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('button', { name: 'Try again', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: /^Queued jobs/ })).toHaveCount(0);
});

test('a failed history fetch is visible even while its chart is closed', async ({ page }) => {
  await page.route('**/api/v1/usage*', (route) =>
    route.fulfill({
      status: 500,
      json: { error: { code: 'internal_error', message: 'history unavailable' } },
    }),
  );
  await goto(page, '/', 'Overview');
  await expect(page.locator('#history')).not.toHaveAttribute('open');
  await expect(page.locator('#history > summary')).toContainText('History unavailable');
  await page.locator('#history > summary').click();
  await expect(
    page.locator('#history').getByRole('button', { name: 'Try again', exact: true }),
  ).toBeVisible();
});

test('page navigation follows the section reached by a jump or scrolling', async ({ page }) => {
  // Narrower than the width at which the jump bar stands down.
  await page.setViewportSize({ width: 1100, height: 900 });
  await goto(page, '/', 'Overview');
  const nav = page.getByRole('navigation', { name: 'Overview sections' });
  for (const label of ['Pools', 'Jobs']) {
    const link = nav.getByRole('link', { name: label, exact: true });
    await link.click();
    await expect(link).toHaveAttribute('aria-current', 'location');
  }
  await page.evaluate(() => scrollTo(0, 0));
  await expect(nav.getByRole('link', { name: 'Now', exact: true })).toHaveAttribute(
    'aria-current',
    'location',
  );
});

test('a silenced preview keeps category settings reachable', async ({ page }) => {
  await goto(page, '/settings/events', 'Events');
  const categories = page.getByRole('switch');
  expect(await categories.count()).toBeGreaterThan(0);
  for (const category of await categories.all()) {
    if (await category.isChecked()) await category.click();
  }
  await goto(page, '/', 'Overview');
  const preview = page.getByRole('region', { name: 'Recent events', exact: true });
  await expect(preview).toContainText('Nothing in the kinds you are watching');
  await preview.getByRole('link', { name: 'Choose', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Events', exact: true })).toBeVisible();
});

// The default must not override an explicit side chosen in this browser.
test('Eli retains an explicitly saved side', async ({ page }) => {
  await page.addInitScript(() =>
    localStorage.setItem('zoomies.eli.layout', JSON.stringify({ side: 'left' })),
  );
  await goto(page, '/', 'Overview');
  const launcher = page.getByRole('button', { name: 'Ask Eli', exact: true });
  await expect.poll(async () => (await launcher.boundingBox())!.x).toBeLessThan(32);
  await page.reload();
  await expect.poll(async () => (await launcher.boundingBox())!.x).toBeLessThan(32);
});
