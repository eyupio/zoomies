import { expect, test } from '@playwright/test';
import { browserOverride, goto } from './support/fixtures';

test.use(browserOverride);

test('runner queue context uses fleet totals and links to provisioning controls', async ({
  page,
}, testInfo) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await goto(page, '/runners', 'Runners');
  const stats = await (await page.request.get('/api/v1/stats')).json();
  const context = page.getByRole('region', { name: 'Runner operational context' });
  await expect(
    context.getByRole('link', {
      name: new RegExp(`Job queue depth: ${stats.fleet.queued_jobs}\\.`),
    }),
  ).toBeVisible();
  await expect(context.getByRole('link', { name: 'Manage queue ↗' })).toHaveAttribute(
    'href',
    '/queue',
  );
  await page.getByText('Explore fleet activity over the last hour', { exact: true }).click();
  await page.getByLabel('Fleet trend metric').selectOption('idle');
  await expect(page.getByRole('slider', { name: 'Inspect idle runners' })).toBeVisible();
  await page.getByRole('slider', { name: 'Inspect idle runners' }).press('Home');
  await expect(page.getByText(/minutes observed · gaps mean no sample/)).toBeVisible();

  // A wider window folds the minutes into intervals that carry their peak,
  // and says so, so a day of samples is ninety-six points rather than a smear.
  const trend = page.getByRole('region', { name: 'Fleet activity', exact: true });
  await trend.getByRole('button', { name: 'The last 6 hours, in 5-minute peaks' }).click();
  await expect(trend.getByText(/5-minute intervals observed/)).toBeVisible();
  await expect(
    trend.getByRole('img', { name: /Idle runners; \d+ observed 5-minute intervals/ }),
  ).toBeVisible();
  await trend.getByRole('button', { name: 'The last hour, minute by minute' }).click();
  await expect(trend.getByText(/minutes observed · gaps mean no sample/)).toBeVisible();

  await page.screenshot({ path: testInfo.outputPath('fleet-history.png'), fullPage: true });
  expect(errors).toEqual([]);
});

/*
 * The runner lifecycle is the store's own state machine, drawn: the five
 * steps in the order the controller allows, each with its count and a way in,
 * and the segments of the bar under it are ways in too.
 */
test('the runner lifecycle draws the state machine with live counts and links', async ({
  page,
}) => {
  await goto(page, '/runners', 'Runners');
  const lifecycle = page.getByRole('region', { name: 'Runner lifecycle', exact: true });
  const steps = lifecycle.getByRole('list', { name: 'Runner lifecycle, in order' });
  await expect(steps.getByRole('listitem')).toHaveCount(5);
  await expect(steps.getByRole('link')).toHaveText([
    /^\s*Provisioning\s*\d+$/,
    /^\s*Registering\s*\d+$/,
    /^\s*Idle\s*\d+$/,
    /^\s*Busy\s*\d+$/,
    /^\s*Draining\s*\d+$/,
  ]);
  await expect(steps.getByRole('link', { name: 'Busy runners' })).toHaveAttribute(
    'href',
    '/runners?state=busy',
  );

  // The counts are the controller's, not a partial page of runners.
  const stats = await page.request.get('/api/v1/stats').then((r) => r.json());
  await expect(steps.getByRole('link', { name: 'Busy runners' })).toContainText(
    String(stats.runners.busy),
  );

  // Every segment of the bar is a link with a name, and hovering one says
  // the share out loud for sighted readers.
  const bar = lifecycle.locator('.bar');
  const busy = bar.getByRole('link', { name: 'Busy live runners' });
  await expect(busy).toBeVisible();
  await busy.hover();
  await expect(page.locator('.bubble:popover-open')).toContainText(/Busy.*of \d+ · \d+%/);
  await expect(lifecycle).toContainText(/\d+ live runners? in the flow/);
});

for (const route of [
  { path: '/runners', heading: 'Runners', panel: 'Runner lifecycle' },
  { path: '/pools', heading: 'Pools', panel: 'Pool demand and headroom' },
  { path: '/jobs', heading: 'Jobs', panel: 'Job outcomes' },
  { path: '/hosts', heading: 'Hosts', panel: 'Host capacity map' },
  { path: '/queue', heading: 'Queue', panel: 'Provisioning demand' },
])
  for (const theme of ['light', 'dark'] as const) {
    test(`${route.path} operational visuals fit the ${theme} layout`, async ({
      page,
    }, testInfo) => {
      const errors: string[] = [];
      page.on('pageerror', (error) => errors.push(error.message));
      await page.addInitScript((value) => localStorage.setItem('zoomies.theme', value), theme);
      await goto(page, route.path, route.heading);
      await expect(page.getByRole('region', { name: route.panel, exact: true })).toBeVisible();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      );
      await page.screenshot({
        path: testInfo.outputPath(`${route.path.slice(1)}-${theme}.png`),
        fullPage: true,
      });
      expect(errors).toEqual([]);
    });
  }
