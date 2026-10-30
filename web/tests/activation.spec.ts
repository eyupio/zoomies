import { expect, test } from '@playwright/test';
import { goto, readThrough } from './support/fixtures';

for (const [width, style] of [
  [1440, 'classic'],
  [375, 'classic'],
  [1440, 'mission-control'],
  [375, 'mission-control'],
] as const) {
  test(`first-job setup respects pool choice and gives an honest GitHub handoff at ${width}px in ${style}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.addInitScript((style) => localStorage.setItem('zoomies.ui-style', style), style);
    await page.route('**/api/v1/events**', (route) => route.abort());
    await page.route('**/api/v1/stats**', async (route) => {
      const read = await readThrough(route);
      if (!read) return;
      const { response, body } = read;
      body.fleet = { ...body.fleet, running_jobs: 0, completed: 0 };
      // The Overview's running figure is this fleet's busy runners.
      body.runners = { ...body.runners, busy: 0 };
      await route.fulfill({ response, json: body });
    });
    await page.route('**/api/v1/installations', (route) =>
      route.fulfill({
        json: {
          items: [{ id: 'activation-app', target: 'acme', target_type: 'org', healthy: true }],
        },
      }),
    );
    await page.route('**/api/v1/pools', (route) =>
      route.fulfill({
        json: {
          items: [
            {
              id: 'first',
              name: 'First pool',
              installation_id: 'activation-app',
              labels: ['zoomies-first'],
              enabled: true,
              max_runners: 2,
              priority: 0,
            },
            {
              id: 'second',
              name: 'Paused pool',
              installation_id: 'activation-app',
              labels: ['zoomies-second'],
              enabled: false,
              max_runners: 2,
              priority: 0,
            },
          ],
        },
      }),
    );
    await goto(page, '/', 'Overview');
    await expect(page.getByRole('heading', { name: 'Finish setting up' })).toBeVisible();
    const repository = page.getByLabel('Repository (owner/name)');
    const link = page.getByRole('link', { name: 'Open the test workflow on GitHub' });
    await repository.fill('other/widgets');
    await expect(repository).toHaveAttribute('aria-invalid', 'true');
    await expect(link).toHaveCount(0);
    await repository.fill('acme/widgets');
    await expect(link).toHaveAttribute(
      'href',
      'https://github.com/acme/widgets/actions/workflows/zoomies-test.yml',
    );
    await expect(page.getByText(/If the default branch is protected/)).toBeVisible();
    await expect(page.getByRole('heading', { name: /^Your first job/ })).toHaveCount(0);
    if (process.env.ZOOMIES_ACTIVATION_CAPTURES) {
      await page.screenshot({
        fullPage: true,
        path: `../docs/screenshots/activation/setup-${style}-${width}.png`,
      });
    }
    await page.getByLabel('Destination pool').selectOption('second');
    await expect(repository).toHaveValue('');
    await expect(page.getByRole('status').filter({ hasText: 'This pool is paused' })).toBeVisible();
    await expect(page.getByRole('group', { name: 'The test workflow' })).toContainText(
      'runs-on: zoomies-second',
    );
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
  });
}
