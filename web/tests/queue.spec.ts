import { expect, test } from '@playwright/test';
import { browserOverride, goto } from './support/fixtures';

test.use(browserOverride);

test('queue controls persist, filter and restore demand without changing GitHub state', async ({
  page,
  request,
}) => {
  const response = await request.get('/api/v1/provisioning?limit=1&sort=queued_at&order=asc');
  const { items } = await response.json();
  expect(items.length).toBeGreaterThan(0);
  const job = items[0];
  try {
    await goto(page, '/queue', 'Queue');
    const errors: string[] = [];
    page.on('pageerror', (error) => errors.push(error.message));
    await expect(
      page.getByRole('button', { name: 'Select all matching', exact: true }),
    ).toBeEnabled();
    const menu = page
      .getByRole('button', { name: `Provisioning actions for ${job.job_name}`, exact: true })
      .first();
    await menu.click();
    await page.getByRole('menuitem', { name: 'Pause', exact: true }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Pause', exact: true }).click();
    await expect(page.getByRole('status').filter({ hasText: '1 updated' })).toBeVisible();
    await page.reload();
    await page.getByRole('button', { name: /Paused.*Demand on hold/ }).click();
    await expect(page.getByRole('grid', { name: 'Provisioning queue' })).toContainText(
      job.job_name,
    );
    await page.getByRole('button', { name: 'Select all matching', exact: true }).click();
    await expect(page.getByText('Snapshot across all pages')).toBeVisible();
    await page.getByRole('button', { name: 'Run now', exact: true }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Run now', exact: true }).click();
    const updated = await (await request.get(`/api/v1/jobs/${job.id}`)).json();
    expect(updated.state).toBe('queued');
    expect(updated.provisioning).toBe('');
    expect(updated.provision_now).toBe(true);
    expect(errors).toEqual([]);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
  } finally {
    await request.post('/api/v1/provisioning/bulk', { data: { ids: [job.id], action: 'resume' } });
  }
});

test('queue views preserve combined filters and cancel leaves demand unchanged', async ({
  page,
}) => {
  await goto(page, '/queue?repo=acme%2Fapi&provisioning=ready', 'Queue');
  await page.getByRole('button', { name: 'Save view', exact: true }).click();
  await page.getByRole('textbox', { name: 'View name' }).fill('API ready');
  await page.getByRole('dialog').getByRole('button', { name: 'Save view', exact: true }).click();
  await page.getByRole('button', { name: 'Reset filters', exact: true }).first().click();
  await page
    .getByRole('combobox', { name: 'Saved queue views' })
    .selectOption({ label: 'API ready' });
  await expect(page).toHaveURL(/repo=acme%2Fapi/);
  await expect(page).toHaveURL(/provisioning=ready/);
  await expect(
    page.getByRole('button', { name: 'Select all matching', exact: true }),
  ).toBeEnabled();
  await page.getByRole('button', { name: 'Select all matching', exact: true }).click();
  await page.getByRole('button', { name: 'Delete from queue', exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByText('Snapshot across all pages')).toBeVisible();
});
