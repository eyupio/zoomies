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
  await page.screenshot({ path: testInfo.outputPath('fleet-history.png'), fullPage: true });
  expect(errors).toEqual([]);
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
