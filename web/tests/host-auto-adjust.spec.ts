import { expect, test } from '@playwright/test';
import { goto, readThrough } from './support/fixtures';

// The Adjust dialog shows the controller's own recommendation, which counts the
// disk as well as the cores and memory, and lets an operator choose whether the
// controller keeps the host there. The recommendation and the save are served by
// the test so the numbers are the test's own and the demo host is not changed.
const recommendation = {
  available: true,
  capacity: 3,
  reserve_cpus: 1,
  reserve_memory_mb: 3072,
  reserve_disk_mb: 4096,
  limited_by: 'disk',
  ask_cpus: 2,
  ask_memory_mb: 4096,
  reason: '3 runners fit after the reserve, and disk is the tightest',
};

test('the dialog uses the controller recommendation, says when the disk sets it, and saves the choice', async ({
  page,
}) => {
  await page.route('**/api/v1/hosts/*/recommendation', (route) =>
    route.fulfill({ json: recommendation }),
  );
  let patched: Record<string, unknown> | undefined;
  await page.route('**/api/v1/hosts/*?*', (route) => route.fallback());
  await page.route(/\/api\/v1\/hosts\/[^/?]+$/, async (route) => {
    if (route.request().method() !== 'PATCH') return route.fallback();
    patched = route.request().postDataJSON();
    const read = await readThrough(route, { method: 'GET' });
    if (!read) return;
    return route.fulfill({ response: read.response });
  });
  await goto(page, '/hosts', 'Hosts');
  const card = page.getByRole('article', { name: 'demo-builder-1', exact: true });
  await card.getByRole('button', { name: 'Adjust', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Adjust demo-builder-1' });

  await expect(dialog.getByTestId('capacity-disk-note')).toContainText('The disk sets this');
  await dialog.getByRole('button', { name: 'Set to recommendations' }).click();
  await expect(dialog.getByRole('slider', { name: 'Runner slots' })).toHaveAttribute(
    'aria-valuetext',
    '3 runners',
  );

  const keep = dialog.getByLabel('Keep at the recommendation');
  await expect(keep).toHaveValue('');
  await keep.selectOption('on');
  await expect(dialog).toContainText('put back within minutes');
  await dialog.getByRole('button', { name: 'Save changes' }).click();
  await expect.poll(() => patched).toBeTruthy();
  expect(patched?.auto_adjust).toBe('on');
  expect(patched?.capacity).toBe(3);
});

test('a host that has not said how big it is gets no disk note and the browser figures stand', async ({
  page,
}) => {
  await page.route('**/api/v1/hosts/*/recommendation', (route) =>
    route.fulfill({ json: { available: false } }),
  );
  await goto(page, '/hosts', 'Hosts');
  const card = page.getByRole('article', { name: 'demo-builder-1', exact: true });
  await card.getByRole('button', { name: 'Adjust', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Adjust demo-builder-1' });
  await expect(dialog.getByTestId('capacity-disk-note')).toHaveCount(0);
  await expect(dialog.getByLabel('Keep at the recommendation')).toBeVisible();
});
