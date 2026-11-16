import { expect, test } from '@playwright/test';
import { goto, reload } from './support/fixtures';

test('a disk-blocked host is red in its collapsed row and recovers without a cordon', async ({
  page,
}) => {
  await page.route('**/api/v1/events*', (route) => route.abort('connectionfailed'));
  const response = await page.request.get('/api/v1/hosts');
  const body = await response.json();
  const original = body.items[0];
  expect(original?.id).toBeTruthy();
  const host = {
    ...original,
    name: 'disk-admission-test',
    healthy: true,
    cordoned: false,
    disk_total_mb: 144 * 1024,
    disk_free_mb: 34 * 1024,
  };
  let blocked = true;
  const problem = {
    code: 'host.low_disk',
    severity: 'error',
    audience: 'fleet',
    title: 'host disk-admission-test is short of disk for new runners',
    detail:
      '34 GiB free; 16 GiB reserve; 18 GiB allocatable; 10 GiB promised; 8 GiB left. One runner needs 10 GiB.',
    fix: 'Inspect disk use and reclaim unused build caches.',
    target_kind: 'host',
    target_id: host.id,
  };
  await page.route(/\/api\/v1\/hosts(?:\?.*)?$/, (route) =>
    route.fulfill({ json: { ...body, items: [host], total: 1 } }),
  );
  await page.route(/\/api\/v1\/problems(?:\?.*)?$/, (route) =>
    route.fulfill({ json: { ok: true, items: blocked ? [problem] : [] } }),
  );
  await goto(page, '/hosts?health=attention', 'Hosts');
  const card = page.getByRole('article', { name: 'disk-admission-test' });
  await expect(card.getByText('Disk blocked', { exact: true })).toBeVisible();
  await expect(card.getByTestId('host-disk-blocked')).toContainText('One runner needs 10 GiB');
  await expect(card.getByRole('button', { name: /Details/ })).toHaveAttribute('aria-expanded', 'false');
  blocked = false;
  await reload(page, 'Hosts');
  await expect(page.getByRole('heading', { name: 'No host needs attention' })).toBeVisible();
  await goto(page, '/hosts', 'Hosts');
  await expect(card.getByText('Disk blocked', { exact: true })).toHaveCount(0);
  await expect(card.getByText('Connected', { exact: true })).toBeVisible();
});
