import { expect, test } from '@playwright/test';
import { goto, reload } from './support/fixtures';

test('cache storage is visible while the host card is collapsed, and unknown stays unknown', async ({
  page,
}) => {
  await page.route('**/api/v1/events*', (route) => route.abort('connectionfailed'));
  const response = await page.request.get('/api/v1/hosts');
  const body = await response.json();
  const original = body.items[0];
  expect(original?.id).toBeTruthy();
  let known = true;
  const host = {
    ...original,
    name: 'cache-storage-test',
    cache_storage: [
      {
        pool_id: 'pool_one',
        package_bytes: 20 * 1024 ** 3,
        tools_bytes: 8 * 1024 ** 3,
        service_bytes: 4 * 1024 ** 3,
        total_bytes: 32 * 1024 ** 3,
        size_limit: 12 * 1024 ** 3,
        measured_at: new Date().toISOString(),
        deferred: "A container is still using this pool's cache",
      },
    ],
  };
  await page.route(/\/api\/v1\/hosts(?:\?.*)?$/, (route) =>
    route.fulfill({
      json: { ...body, items: [known ? host : { ...host, cache_storage: undefined }], total: 1 },
    }),
  );
  await goto(page, '/hosts', 'Hosts');
  const card = page.getByRole('article', { name: 'cache-storage-test' });
  await expect(card.getByRole('button', { name: /Details/ })).toHaveAttribute(
    'aria-expanded',
    'false',
  );
  const storage = card.getByTestId('host-cache-storage');
  await expect(storage).toContainText('32 GB');
  await expect(storage).toContainText('Over pool limit');
  await expect(storage).toContainText('still using');
  await storage.getByRole('button').hover();
  await expect(page.getByRole('tooltip')).toContainText(
    'Packages 20 GB, tools 8.0 GB, Actions service 4.0 GB',
  );
  known = false;
  await reload(page, 'Hosts');
  await expect(card.getByTestId('host-cache-storage')).toHaveCount(0);
});
