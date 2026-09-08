/**
 * Settings: the accounts and the API tokens.
 *
 * These are the two places in the product where a mistake is expensive and
 * irreversible, so the protections are the point: deleting an account demands
 * its name typed out, a minted token is shown once and says so, and a revoked
 * token stops working immediately. None of it had a test.
 *
 * The seeded fixture has neither users nor tokens -- the demo seed refuses to
 * run at all on an instance that already has an account -- so each test makes
 * what it needs and takes it away again.
 */
import { expect, test, type Page } from '@playwright/test';
import { browserOverride, goto } from './support/fixtures';

test.use(browserOverride);

const tab = (page: Page, name: string) => page.getByRole('tab', { name });
const dialog = (page: Page, name: string | RegExp) => page.getByRole('dialog', { name });

/** A name nothing else in the suite will collide with. */
function unique(prefix: string): string {
  return `${prefix}-${Math.random().toString(36).slice(2, 8)}`;
}

/**
 * The panel's own create button.
 *
 * An empty panel carries two of them -- one in its header, one inside the
 * empty state, which is the guidelines' rule that an empty state names the
 * next step with the action inline. The first in document order is the
 * header's, and it is there whether the panel is empty or not.
 */
const create = (page: Page, label: string) => page.getByRole('button', { name: label }).first();

test('an account can be created, given a different role, and deleted by name', async ({ page }) => {
  const username = unique('spec-user');
  await goto(page, '/settings?tab=users', 'Settings');
  await expect(tab(page, 'Users')).toHaveAttribute('aria-selected', 'true');

  await create(page, 'Add an account').click();
  const form = dialog(page, 'Add an account');
  await form.getByRole('textbox', { name: 'Username' }).fill(username);
  await form.getByRole('textbox', { name: 'Display name' }).fill('A Spec Account');
  await form.getByRole('radio', { name: /Viewer/ }).check();
  await form.getByRole('textbox', { name: 'Password' }).fill('a-long-enough-password');
  await form.getByRole('button', { name: 'Add account' }).click();
  await expect(form).toBeHidden();

  const row = page.getByRole('row', { name: new RegExp(username) });
  await expect(row).toBeVisible();
  await expect(row).toContainText('Viewer');

  // Edited in place, and the table shows the label rather than the raw id.
  await row.getByRole('button', { name: new RegExp(`Actions for ${username}`) }).click();
  await page.getByRole('menuitem', { name: 'Edit role and details' }).click();
  const edit = dialog(page, new RegExp(`Edit ${username}`));
  await edit.getByRole('radio', { name: /Operator/ }).check();
  await edit.getByRole('button', { name: 'Save changes' }).click();
  await expect(edit).toBeHidden();
  await expect(row).toContainText('Operator');

  // Deleting demands the name typed out: this is the irreversible one.
  await row.getByRole('button', { name: new RegExp(`Actions for ${username}`) }).click();
  await page.getByRole('menuitem', { name: 'Delete this account' }).click();
  const confirm = dialog(page, 'Delete account');
  const go = confirm.getByRole('button', { name: 'Delete account' });
  await expect(go, 'the button is dead until the name is typed').toBeDisabled();
  await confirm.getByRole('textbox', { name: 'Type the name to confirm' }).fill('not-the-name');
  await expect(go).toBeDisabled();
  await confirm.getByRole('textbox', { name: 'Type the name to confirm' }).fill(username);
  await expect(go).toBeEnabled();
  await go.click();

  await expect(confirm).toBeHidden();
  await expect(row).toHaveCount(0);
});

test('a token is shown once, in plain text, and says so', async ({ page }) => {
  const name = unique('spec-token');
  await goto(page, '/settings?tab=tokens', 'Settings');

  await create(page, 'Create a token').click();
  const form = dialog(page, 'Create an API token');
  await form.getByRole('textbox', { name: 'Name' }).fill(name);
  await form.getByRole('radio', { name: /Operator/ }).check();
  await form.getByRole('button', { name: 'Create token' }).click();

  // The one moment it exists in plain text, and the dialog is explicit that
  // this is the only one.
  await expect(form).toContainText('This is the only time it exists in plain text');
  await expect(form.getByRole('button', { name: 'Copy the token' })).toBeVisible();
  await form.getByRole('button', { name: 'Done' }).click();
  await expect(form).toBeHidden();

  const row = page.getByRole('row', { name: new RegExp(name) });
  await expect(row).toBeVisible();
  await expect(row).toContainText('Operator');

  // Revoking needs a confirmation, but not the name: it is undoable by
  // minting another, unlike deleting the account that owns it.
  await row.getByRole('button', { name: 'Revoke' }).click();
  const confirm = dialog(page, 'Revoke token');
  await expect(confirm).toContainText('stops working immediately');
  await confirm.getByRole('button', { name: 'Revoke' }).click();
  await expect(confirm).toBeHidden();
  await expect(row.getByRole('button', { name: 'Revoke' })).toHaveCount(0);
});

test('cancelling a destructive confirmation changes nothing', async ({ page }) => {
  const name = unique('spec-keep');
  await goto(page, '/settings?tab=tokens', 'Settings');

  await create(page, 'Create a token').click();
  const form = dialog(page, 'Create an API token');
  await form.getByRole('textbox', { name: 'Name' }).fill(name);
  await form.getByRole('button', { name: 'Create token' }).click();
  await form.getByRole('button', { name: 'Done' }).click();

  const row = page.getByRole('row', { name: new RegExp(name) });
  await row.getByRole('button', { name: 'Revoke' }).click();
  await dialog(page, 'Revoke token').getByRole('button', { name: 'Cancel' }).click();

  await expect(dialog(page, 'Revoke token')).toBeHidden();
  await expect(row.getByRole('button', { name: 'Revoke' }), 'still revocable').toBeVisible();

  // Tidy up so the next spec sees the panel it expects.
  await row.getByRole('button', { name: 'Revoke' }).click();
  await dialog(page, 'Revoke token').getByRole('button', { name: 'Revoke' }).click();
  await expect(dialog(page, 'Revoke token')).toBeHidden();
});

test('the chosen tab is in the address bar, so a settings page is a link', async ({ page }) => {
  await goto(page, '/settings', 'Settings');

  await tab(page, 'Appearance').click();
  await expect(page).toHaveURL(/tab=appearance/);
  await expect(page.getByRole('tabpanel')).toContainText('Relative times');

  await page.reload();
  await expect(tab(page, 'Appearance')).toHaveAttribute('aria-selected', 'true');
});

/**
 * The About tab is the product's own identity card, and the two things it says
 * about the product itself are easy to get wrong in opposite directions.
 *
 * The mark: the brand guide ranks the original circular dog above the
 * head/swish, and the head/swish is a restored reconstruction rather than
 * approved source artwork, so serving it here is serving the wrong dog. 128px
 * is the guide's minimum for the circular mark and the reason this is the slot
 * that carries it.
 *
 * The description: most people meet a controller somebody else installed, and
 * the panel header says what the panel is rather than what Zoomies is.
 */
test('the About tab carries the primary mark and says what Zoomies is', async ({ page }) => {
  await goto(page, '/settings?tab=about', 'Settings');

  // The mark is decorative, so nothing in the accessibility tree names it and
  // the served file is the only observable that distinguishes one from another.
  const mark = page.locator('.identity img');
  await expect(mark).toHaveAttribute('src', '/brand/mark-white.png');
  await expect(mark).toHaveJSProperty('naturalWidth', 128);

  await expect(
    page.getByText(/lightweight fleet controller for GitHub Actions runners/),
  ).toBeVisible();
});
