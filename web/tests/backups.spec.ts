/**
 * Settings: the backups.
 *
 * The one place a copy of the whole database leaves the machine and the one
 * place a copy comes back to replace the fleet, so what these tests protect is
 * the sequence: a backup taken from the page appears with what a restore needs
 * to know, it verifies, its download is the archive it says it is, a restore
 * is staged by name and can be cancelled, and deleting demands the name too.
 *
 * The one thing not pressed is "Restart and restore": it stops the controller
 * the whole suite shares. Staging is where the checks live; the restart is a
 * process exit the Go tests cover.
 */
import { expect, test, type Page } from '@playwright/test';
import { browserOverride, goto } from './support/fixtures';

test.use(browserOverride);

const dialog = (page: Page, name: string | RegExp) => page.getByRole('dialog', { name });

async function openBackups(page: Page): Promise<void> {
  await goto(page, '/settings?tab=backups', 'Settings');
  await expect(page.getByRole('heading', { name: 'Backups', exact: true })).toBeVisible();
}

/** Press the button, and return the row the new backup appears in. */
async function takeOne(page: Page) {
  const before = await page.getByRole('row').count();
  await page.getByRole('button', { name: 'Back up now' }).first().click();
  await expect(page.getByText('Backup taken')).toBeVisible();
  await expect
    .poll(async () => page.getByRole('row').count(), { message: 'a row for the new backup' })
    .toBeGreaterThan(before);
  // Newest first: the first body row is the one just taken.
  return page.locator('tbody').getByRole('row').first();
}

/** The id printed in a row, which the confirmations ask to have typed back. */
async function idOf(row: ReturnType<Page['locator']>): Promise<string> {
  const id = await row.locator('.mono').first().textContent();
  expect(id, 'the row names the backup').toMatch(/^zoomies-\d{8}-\d{6}/);
  return id!.trim();
}

test('a backup taken from the page is listed with what a restore needs to know', async ({
  page,
}) => {
  await openBackups(page);
  const row = await takeOne(page);
  const id = await idOf(row);

  // The facts a restore will ask about, in the row rather than in a manifest
  // nobody opens: who took it, the schema it had reached, and whether this
  // host's key is the one that sealed it.
  await expect(row).toContainText('Taken here');
  await expect(row).toContainText('This key');
  await expect(row.getByRole('toolbar', { name: new RegExp(id) })).toBeVisible();

  // And the panel's own facts moved with it.
  await expect(page.getByText('Last backup')).toBeVisible();
  await expect(page.getByText('None yet')).toHaveCount(0);

  // Verify re-reads the file and says so, check by check.
  await row.getByRole('button', { name: `Verify: ${id}` }).click();
  const verify = dialog(page, new RegExp(`Verify ${id}`));
  await expect(verify.getByText('Sound')).toBeVisible();
  await expect(verify.getByText('Unchanged')).toBeVisible();
  await expect(verify.getByText('This backup can be restored here.')).toBeVisible();
  await verify.getByRole('button', { name: 'Done' }).click();
  await expect(verify).toBeHidden();

  // The download is the archive it says it is: a gzip, named for the backup.
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    row.getByRole('button', { name: `Download: ${id}` }).click(),
  ]);
  expect(download.suggestedFilename()).toBe(`${id}.tar.gz`);
});

test('an encrypted download insists on a passphrase worth having', async ({ page }) => {
  await openBackups(page);
  const row = await takeOne(page);
  const id = await idOf(row);

  await row.getByRole('button', { name: `Download encrypted: ${id}` }).click();
  const seal = dialog(page, 'Download encrypted');
  const go = seal.getByRole('button', { name: 'Download' });
  await expect(go, 'dead until a passphrase is typed twice').toBeDisabled();
  await seal.getByLabel('Passphrase', { exact: true }).fill('short');
  await expect(seal.getByText('Use at least eight characters.')).toBeVisible();
  await seal.getByLabel('Passphrase', { exact: true }).fill('correct horse battery staple');
  await seal.getByLabel('Passphrase again').fill('correct horse battery stapl');
  await expect(seal.getByText('The two do not match.')).toBeVisible();
  await expect(go).toBeDisabled();
  await seal.getByLabel('Passphrase again').fill('correct horse battery staple');
  await expect(go).toBeEnabled();
  const [download] = await Promise.all([page.waitForEvent('download'), go.click()]);
  expect(download.suggestedFilename()).toBe(`${id}.tar.gz.enc`);
  await expect(seal).toBeHidden();
});

test('a restore is staged by name, shown as waiting, and can be cancelled', async ({ page }) => {
  await openBackups(page);
  const row = await takeOne(page);
  const id = await idOf(row);

  await row.getByRole('button', { name: `Restore: ${id}` }).click();
  const confirm = dialog(page, 'Restore backup');
  const stage = confirm.getByRole('button', { name: 'Stage the restore' });
  await expect(stage, 'the button is dead until the name is typed').toBeDisabled();
  await expect(confirm).toContainText('Everyone is signed out');
  await confirm.getByRole('textbox', { name: 'Type the name to confirm' }).fill(id);
  await expect(stage).toBeEnabled();
  await stage.click();
  await expect(confirm).toBeHidden();

  // Nothing has changed: the page says a restore is waiting, names it, and
  // offers the two ways out. The row it names cannot be deleted meanwhile.
  const banner = page.getByRole('region', { name: /waiting for a restart/i });
  await expect(banner).toBeVisible();
  await expect(banner).toContainText(id);
  await expect(page.getByRole('button', { name: 'Restart and restore' })).toBeVisible();
  const remove = row.getByRole('button', { name: `Delete: ${id}` });
  await expect(remove).toHaveAttribute('aria-disabled', 'true');

  await page.getByRole('button', { name: 'Cancel the restore', exact: true }).click();
  await expect(banner).toBeHidden();
  await expect(remove).not.toHaveAttribute('aria-disabled', 'true');
});

test('deleting a backup demands its name and then it is gone', async ({ page }) => {
  await openBackups(page);
  const row = await takeOne(page);
  const id = await idOf(row);

  await row.getByRole('button', { name: `Delete: ${id}` }).click();
  const confirm = dialog(page, 'Delete backup');
  const go = confirm.getByRole('button', { name: 'Delete backup' });
  await expect(go).toBeDisabled();
  await confirm.getByRole('textbox', { name: 'Type the name to confirm' }).fill(id);
  await go.click();
  await expect(confirm).toBeHidden();
  await expect(page.getByText(`${id} deleted`)).toBeVisible();
  await expect(page.locator('tbody').getByRole('row').filter({ hasText: id })).toHaveCount(0);
});

test('the configuration can be exported and an import is previewed before it is applied', async ({
  page,
}) => {
  await goto(page, '/settings?tab=configuration', 'Settings');
  await expect(page.getByRole('heading', { name: 'Configuration', exact: true })).toBeVisible();

  // The export is a download in the shape the file takes.
  await page.getByRole('button', { name: 'Export the settings' }).click();
  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.getByRole('menuitem', { name: 'As zoomies.yaml' }).click(),
  ]);
  expect(download.suggestedFilename()).toMatch(/^zoomies-settings-\d{8}-\d{6}\.yaml$/);

  // The import says what it would do, key by key, and refuses the key it
  // cannot take -- and nothing is written until Apply.
  await page.getByRole('button', { name: 'Import' }).click();
  const importing = dialog(page, 'Import settings');
  await importing
    .getByRole('textbox', { name: 'Document' })
    .fill('retention:\n  webhooks: 72h\nscheduler:\n  interval: soon\n');
  await importing.getByRole('button', { name: 'Check the document' }).click();
  const webhooks = importing.getByRole('row').filter({ hasText: 'retention.webhooks' });
  await expect(webhooks).toContainText('Will change');
  const interval = importing.getByRole('row').filter({ hasText: 'scheduler.interval' });
  await expect(interval).toContainText('Refused');
  await expect(interval).toContainText('is not a duration');
  await expect(importing.getByRole('button', { name: 'Apply 1 change' })).toBeEnabled();
  await importing.getByRole('button', { name: 'Apply 1 change' }).click();
  await expect(importing).toBeHidden();
  await expect(page.getByText('1 setting imported')).toBeVisible();

  // Applied, and shown on the page as a value stored here -- then put back,
  // so the next spec sees the fixture it expects.
  const row = page.locator('.row').filter({ has: page.getByText('Keep webhook deliveries for') });
  await expect(row).toContainText('72h');
  await row.getByRole('button', { name: 'Reset' }).click();
  await expect(row.getByText('Saved here')).toBeHidden();
});
