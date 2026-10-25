import { expect, test } from '@playwright/test';
import { goto, readThrough, useClassic } from './support/fixtures';

async function mission(page: import('@playwright/test').Page, prefs?: Record<string, unknown>) {
  await page.setViewportSize({ width: 375, height: 900 });
  await page.addInitScript((prefs) => {
    if (sessionStorage.getItem('layout-seeded')) return;
    localStorage.setItem('zoomies.ui-style', 'mission-control');
    if (prefs) localStorage.setItem('zoomies.prefs', JSON.stringify(prefs));
    sessionStorage.setItem('layout-seeded', '1');
  }, prefs);
}

test('Mission Control shows current work before activity history and retains timing detail', async ({
  page,
}) => {
  await mission(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await goto(page, '/', 'Overview');
  const pools = page.getByRole('heading', { name: 'Pools', exact: true });
  const history = page.locator('#history > summary');
  expect(await pools.evaluate((node) => node.getBoundingClientRect().top)).toBeLessThan(1000);
  expect(await history.evaluate((node) => node.getBoundingClientRect().top)).toBeGreaterThan(
    await pools.evaluate((node) => node.getBoundingClientRect().top),
  );
  const timing = page.locator('details.performance');
  await expect(timing).not.toHaveAttribute('open');
  await timing.locator('summary').click();
  await expect(page.getByText('Runner startup p50', { exact: true })).toBeVisible();
  await expect(page.getByText('Registration p50', { exact: true })).toBeVisible();
  await page.setViewportSize({ width: 375, height: 900 });
  const active = page.getByRole('heading', { name: 'Active jobs', exact: true });
  const events = page.getByRole('heading', { name: 'Recent events', exact: true });
  expect(await active.evaluate((node) => node.getBoundingClientRect().top)).toBeLessThan(
    await events.evaluate((node) => node.getBoundingClientRect().top),
  );
});

test('a fresh Mission Control phone shows job identity and Inspect before supplementary analytics', async ({
  page,
}) => {
  await mission(page);
  await goto(page, '/jobs', 'Jobs');
  await expect(page.getByRole('button', { name: /^Cards,/ })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  const identity = page
    .locator('table[aria-label="Jobs"] tbody tr')
    .first()
    .locator('[data-label="Job"]');
  await expect(identity).toBeVisible();
  expect(await identity.evaluate((node) => node.getBoundingClientRect().bottom)).toBeLessThan(900);
  await expect(page.locator('details.fleet-context')).not.toHaveAttribute('open');
  await expect(page.locator('details.advanced')).not.toHaveAttribute('open');
  await identity.getByRole('button', { name: /^Inspect/ }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
});

for (const [label, prefs, chosen] of [
  ['global Rows', { gridView: 'rows' }, 'Rows'],
  [
    'per-grid Rows over global Cards',
    { gridView: 'cards', grids: { jobs: { view: 'rows' } } },
    'Rows',
  ],
  [
    'per-grid Cards over global Rows',
    { gridView: 'rows', grids: { jobs: { view: 'cards' } } },
    'Cards',
  ],
] as const) {
  test(`Mission Control preserves ${label}`, async ({ page }) => {
    await mission(page, prefs);
    await goto(page, '/jobs', 'Jobs');
    await expect(page.getByRole('button', { name: new RegExp(`^${chosen},`) })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    await page.reload();
    await expect(page.getByRole('button', { name: new RegExp(`^${chosen},`) })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
  });
}

test('opening and closing Filters preserves a query and its active chip', async ({ page }) => {
  await mission(page);
  await goto(page, '/jobs?repo=acme%2Fapi', 'Jobs');
  const disclosure = page.locator('details.advanced');
  await expect(disclosure).not.toHaveAttribute('open');
  await expect(disclosure.locator('summary')).toContainText('1');
  await expect(page.getByRole('group', { name: 'Filters in effect' })).toContainText('acme/api');
  await disclosure.locator('summary').click();
  await expect(disclosure).toHaveAttribute('open');
  await disclosure.locator('summary').click();
  await expect(disclosure).not.toHaveAttribute('open');
  await expect(page).toHaveURL(/repo=acme%2Fapi/);
  await expect(page.getByRole('group', { name: 'Filters in effect' })).toContainText('acme/api');
});

test('saving vocabulary does not turn a fresh style default into an explicit global table choice', async ({
  page,
}) => {
  await mission(page);
  await goto(page, '/settings/appearance', 'Appearance');
  await page
    .getByRole('group', { name: 'Zoomies vocabulary', exact: true })
    .getByRole('button', { name: /^Cute/ })
    .click();
  expect(
    await page.evaluate(() => JSON.parse(localStorage.getItem('zoomies.prefs') || '{}').gridView),
  ).toBeUndefined();
  await goto(page, '/jobs', 'Jobs');
  await expect(page.getByRole('button', { name: /^Cards,/ })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
});

test('Classic shares the current-work-first overview and retains its Rows fallback', async ({
  page,
}) => {
  await useClassic(page);
  await page.setViewportSize({ width: 375, height: 900 });
  await goto(page, '/jobs', 'Jobs');
  await expect(page.getByRole('button', { name: /^Rows,/ })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await expect(page.locator('details.advanced')).toHaveAttribute('open');
  await goto(page, '/', 'Overview');
  const history = page.locator('#history > summary');
  const pools = page.getByRole('heading', { name: 'Pools', exact: true });
  expect(await history.evaluate((node) => node.getBoundingClientRect().top)).toBeGreaterThan(
    await pools.evaluate((node) => node.getBoundingClientRect().top),
  );
});

test('saved column order and widths survive the Mission Control defaults and a style change', async ({
  page,
}) => {
  await mission(page, {
    grids: {
      jobs: { view: 'rows', order: ['state', 'job_name', 'repo'], widths: { job_name: 240 } },
    },
  });
  await goto(page, '/jobs', 'Jobs');
  const order = () =>
    page
      .locator('table[aria-label="Jobs"] thead th[data-table-column]')
      .evaluateAll((nodes) =>
        nodes.slice(0, 3).map((node) => node.getAttribute('data-table-column')),
      );
  await expect.poll(order).toEqual(['state', 'job_name', 'repo']);
  await goto(page, '/settings/appearance', 'Appearance');
  await page.getByRole('radio', { name: /^Classic/ }).check();
  await goto(page, '/jobs', 'Jobs');
  await expect.poll(order).toEqual(['state', 'job_name', 'repo']);
  expect(
    await page.evaluate(
      () => JSON.parse(localStorage.getItem('zoomies.prefs') || '{}').grids.jobs.widths.job_name,
    ),
  ).toBe(240);
});

test('Mission Control keeps first-run guidance first when this fleet has not run a job', async ({
  page,
}) => {
  await mission(page);
  await page.route('**/api/v1/events*', (route) => route.abort());
  await page.route('**/api/v1/stats*', async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    const { response, body: stats } = read;
    await route.fulfill({
      response,
      json: { ...stats, fleet: { ...stats.fleet, running_jobs: 0, completed: 0 } },
    });
  });
  await goto(page, '/', 'Overview');
  const setup = page.getByRole('heading', { name: 'Finish setting up', exact: true });
  await expect(setup).toBeVisible();
  const metrics = page.getByText('Queued jobs', { exact: true }).first();
  expect(await setup.evaluate((node) => node.getBoundingClientRect().top)).toBeLessThan(
    await metrics.evaluate((node) => node.getBoundingClientRect().top),
  );
});

test.describe('touch phone', () => {
  test.use({ viewport: { width: 375, height: 900 }, hasTouch: true, isMobile: true });
  test('the first Inspect action is visible with a full-size touch target', async ({ page }) => {
    await mission(page);
    await goto(page, '/jobs', 'Jobs');
    const inspect = page
      .locator('table[aria-label="Jobs"] tbody tr')
      .first()
      .getByRole('button', { name: /^Inspect/ });
    await expect(inspect).toBeVisible();
    const box = await inspect.boundingBox();
    expect(box!.height).toBeGreaterThanOrEqual(44);
    expect(box!.width).toBeGreaterThanOrEqual(44);
    expect(box!.y + box!.height).toBeLessThan(900);
  });
});
