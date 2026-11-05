import { expect, test } from '@playwright/test';
import { goto } from './support/fixtures';

test('one click prepares an instance and its encrypted archive verifies before cutover', async ({
  page,
  request,
}) => {
  // This fixture has its own empty controller. Reset only its fence between
  // desktop and phone runs; no other fleet's work is paused by this spec.
  const reset = await request.post('/api/v1/recovery/unfence');
  expect(reset.ok()).toBeTruthy();
  await goto(page, '/settings/backups', 'Backups');
  await page
    .locator('summary')
    .getByText('Move this instance to another controller', { exact: true })
    .click();
  await page.getByRole('button', { name: 'Prepare for transfer', exact: true }).click();
  await expect(
    page.getByText('Ready to export. The instance is fenced.', { exact: true }),
  ).toBeVisible();
  const downloadButton = page.getByRole('button', {
    name: 'Download complete instance',
    exact: true,
  });
  await expect(downloadButton).toBeDisabled();
  await page
    .getByLabel('Archive passphrase', { exact: true })
    .first()
    .fill('a strong archive passphrase');
  await page.getByLabel('Passphrase again', { exact: true }).fill('a strong archive passphrase');
  const waitDownload = page.waitForEvent('download');
  await downloadButton.click();
  const downloaded = await waitDownload;
  expect(downloaded.suggestedFilename()).toBe('zoomies-instance.zbk');
  const path = await downloaded.path();
  expect(path).not.toBeNull();
  await page
    .locator('summary')
    .getByText('Bring an instance to this controller', { exact: true })
    .click();
  await page.getByLabel('Encrypted instance archive', { exact: true }).setInputFiles(path!);
  await page
    .getByLabel('Archive passphrase', { exact: true })
    .last()
    .fill('a strong archive passphrase');
  await page.getByRole('button', { name: 'Verify archive', exact: true }).click();
  await expect(page.getByText(/Archive verified: 0 installations, 0 hosts, 0 jobs/)).toBeVisible();
  await expect(
    page.getByRole('button', { name: 'Import and restart', exact: true }),
  ).toBeDisabled();
  await expect(
    page.getByLabel(
      'The source controller is stopped and will stay stopped while this instance runs.',
    ),
  ).not.toBeChecked();
  // Do not activate another copy in a browser test. The real restart, key
  // conversion and operator handover are covered by the Go round-trip tests.
  const fence = await request.get('/api/v1/recovery');
  expect((await fence.json()).fenced).toBe(true);
});

test('a large instance archive stays below a proxy request limit and failed verification cannot import it', async ({
  page,
}) => {
  // A sparse file exercises the browser's file path without allocating the
  // complete archive in the test runner. Its contents are deliberately invalid.
  const { mkdtempSync, openSync, ftruncateSync, closeSync, rmSync } = await import('node:fs');
  const { tmpdir } = await import('node:os');
  const { join } = await import('node:path');
  const dir = mkdtempSync(join(tmpdir(), 'zoomies-large-transfer-'));
  const path = join(dir, 'large-instance.zbk');
  const size = 106910 * 1024;
  const fd = openSync(path, 'w');
  ftruncateSync(fd, size);
  closeSync(fd);
  const chunks: number[] = [];
  try {
    await page.route('**/api/v1/transfers/**', async (route) => {
      const request = route.request();
      const length = request.postDataBuffer()?.byteLength ?? 0;
      if (request.url().includes('/chunks?')) chunks.push(length);
      if (length > 8 * 1024 * 1024)
        await route.fulfill({ status: 413, body: 'Proxy request limit exceeded' });
      else await route.continue();
    });
    await goto(page, '/settings/backups', 'Backups');
    await page
      .locator('summary')
      .getByText('Bring an instance to this controller', { exact: true })
      .click();
    await page.getByLabel('Encrypted instance archive', { exact: true }).setInputFiles(path);
    await page
      .getByLabel('Archive passphrase', { exact: true })
      .last()
      .fill('large archive passphrase');
    await page.getByRole('button', { name: 'Verify archive', exact: true }).click();
    await expect(
      page.getByText(/That file is not an instance archive this controller can use/),
    ).toBeVisible({ timeout: 30000 });
    expect(chunks).toHaveLength(14);
    expect(chunks.every((n) => n > 0 && n <= 8 * 1024 * 1024)).toBeTruthy();
    expect(chunks.reduce((sum, n) => sum + n, 0)).toBe(size);
    await expect(page.getByRole('button', { name: 'Import and restart', exact: true })).toHaveCount(
      0,
    );
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
