import { expect, test } from '@playwright/test';
import { goto } from './support/fixtures';

/*
 * The figures used to sit inside the loading skeleton, so an operator saw four
 * zeros for as long as the request took and then no figures at all -- and the
 * quota figure, which is fed by readings that arrive after the list, could
 * never show one. They belong in the band, where they are computed from what
 * loaded.
 */
test('the band figures show the loaded connections and stay once the page has loaded', async ({
  page,
}) => {
  // An App nearly out of quota, which is the reading the fourth tile exists for.
  await page.route(/\/api\/v1\/installations\/[^/]+\/rate-limit$/, (route) =>
    route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ limit: 5000, remaining: 100, reset_at: '2030-01-01T00:00:00Z' }),
    }),
  );
  await goto(page, '/installations', 'Installations');

  // A band figure has no role of its own: it is a term and its definition.
  const tile = (label: string) =>
    page.getByTestId('band-installations').locator('.stat').filter({ hasText: label });
  await expect(tile('GitHub connection').locator('.value')).toHaveText('1');
  await expect(tile('Low API quota').locator('.value')).toHaveText('1');
  // The quota reading is the figure's help, where a screen reader reaches it.
  await expect(
    page.getByTestId('band-installations').getByRole('link', { name: /^Low API quota/ }),
  ).toHaveAccessibleDescription('1 quota readings. Below 10% remaining.');
});
