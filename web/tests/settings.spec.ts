/**
 * Settings: the accounts and the API tokens, and the section they live in.
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
import { browserOverride, goto, openAccountMenu, pageHeading } from './support/fixtures';

test.use(browserOverride);

const dialog = (page: Page, name: string | RegExp) => page.getByRole('dialog', { name });

/** A name nothing else in the suite will collide with. */
function unique(prefix: string): string {
  return `${prefix}-${Math.random().toString(36).slice(2, 8)}`;
}

/**
 * The page's own create button.
 *
 * An empty page carries two of them -- one in its header, one inside the
 * empty state, which is the guidelines' rule that an empty state names the
 * next step with the action inline. The first in document order is the
 * header's, and it is there whether the page is empty or not.
 */
const create = (page: Page, label: string) => page.getByRole('button', { name: label }).first();

/**
 * The section's own navigation: a rail beside the page on a desktop, a strip
 * above it on a tablet, and on a phone the list `/settings` itself shows.
 * Either way, every page is a link in a landmark named for the section.
 */
const settingsNav = (page: Page) => page.getByRole('navigation', { name: 'Settings' });

test('an account can be created, given a different role, and deleted by name', async ({ page }) => {
  const username = unique('spec-user');
  await goto(page, '/settings/users', 'Users');

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
  await confirm.getByRole('textbox', { name: `Type ${username} to confirm` }).fill('not-the-name');
  await expect(go).toBeDisabled();
  await confirm.getByRole('textbox', { name: `Type ${username} to confirm` }).fill(username);
  await expect(go).toBeEnabled();
  await go.click();

  await expect(confirm).toBeHidden();
  await expect(row).toHaveCount(0);
});

test('a token is shown once, in plain text, and says so', async ({ page }) => {
  const name = unique('spec-token');
  await goto(page, '/settings/tokens', 'API tokens');

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
  await goto(page, '/settings/tokens', 'API tokens');

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

  // Tidy up so the next spec sees the page it expects.
  await row.getByRole('button', { name: 'Revoke' }).click();
  await dialog(page, 'Revoke token').getByRole('button', { name: 'Revoke' }).click();
  await expect(dialog(page, 'Revoke token')).toBeHidden();
});

/*
 * Settings is a section of pages rather than one page of tabs: each has an
 * address, the section's own navigation lists them all, and the one being
 * read is marked. On a desktop `/settings` alone goes straight to the first
 * page; on a phone it is the list of them.
 */
test('every settings page has an address of its own, and the section lists them', async ({
  page,
}) => {
  await goto(page, '/settings', /^(Account|Settings)$/);
  const phone = !!test.info().project.use.isMobile;
  if (phone) {
    await expect(page).toHaveURL(/\/settings$/);
  } else {
    await expect(page).toHaveURL(/\/settings\/account$/);
  }

  const pages = [
    ['account', 'Account'],
    ['appearance', 'Appearance'],
    ['users', 'Users'],
    ['tokens', 'API tokens'],
    ['configuration', 'Configuration'],
    ['backups', 'Backups'],
    ['about', 'About'],
  ] as const;

  for (const [id, label] of pages) {
    // On a phone the section's list is the page `/settings` shows, and each
    // page carries a way back to it; on a desktop the rail is beside the page.
    // A row in the list is named by its label and its description, a rail
    // entry by its label alone.
    if (phone) await goto(page, '/settings', 'Settings');
    await page
      .getByRole('link', phone ? { name: new RegExp(`^${label} `) } : { name: label, exact: true })
      .first()
      .click();
    await expect(page).toHaveURL(new RegExp(`/settings/${id}$`));
    await expect(pageHeading(page, label)).toBeVisible();
    if (!phone) {
      const current = settingsNav(page).locator('[aria-current="page"]');
      await expect(current).toHaveCount(1);
      await expect(current).toHaveText(label);
    }
  }

  // A page survives a reload, which is what an address is for.
  await goto(page, '/settings/appearance', 'Appearance');
  await page.reload();
  await expect(pageHeading(page, 'Appearance')).toBeVisible();
  await expect(page).toHaveURL(/\/settings\/appearance$/);
});

test('the old tab addresses still land on their page', async ({ page }) => {
  // Bookmarks and the links in problem entries were written as `?tab=`, and a
  // setting a link named is still the row it lands on.
  await page.goto('/settings?tab=configuration&setting=scheduler.provision_timeout');
  await expect(pageHeading(page, 'Configuration')).toBeVisible();
  await expect(page).toHaveURL(/\/settings\/configuration\?setting=scheduler\.provision_timeout$/);
  await expect(page.locator('.row.sought')).toBeVisible();

  await page.goto('/settings?tab=about');
  await expect(pageHeading(page, 'About')).toBeVisible();
  await expect(page).toHaveURL(/\/settings\/about$/);
});

test('the account menu leads to the pages that are about the person', async ({ page }) => {
  await goto(page, '/', 'Overview');

  const menu = await openAccountMenu(page);
  // The identity first, then the theme as a choice with the one in force
  // marked, then the pages, then the way out.
  await expect(menu.getByRole('menuitemradio', { name: 'System' })).toHaveAttribute(
    'aria-checked',
    'true',
  );
  await expect(menu.getByRole('menuitem', { name: 'Sign out' })).toBeVisible();

  await menu.getByRole('menuitem', { name: 'Your account' }).click();
  await expect(pageHeading(page, 'Account')).toBeVisible();
  await expect(page).toHaveURL(/\/settings\/account$/);
  await expect(page.getByText(/^Signed in as /)).toBeVisible();
});

/**
 * The About page is the product's own identity card, and the two things it
 * says about the product itself are easy to get wrong in opposite directions.
 *
 * The mark: the brand guide ranks the original circular dog above the
 * head/swish, and the head/swish is a restored reconstruction rather than
 * approved source artwork, so serving it here is serving the wrong dog. 128px
 * is the guide's minimum for the circular mark and the reason this is the slot
 * that carries it.
 *
 * The description: most people meet a controller somebody else installed, and
 * the page header says what the page is rather than what Zoomies is.
 */
test('the About page carries the primary mark and says what Zoomies is', async ({ page }) => {
  await goto(page, '/settings/about', 'About');

  // The mark is decorative, so nothing in the accessibility tree names it and
  // the served file is the only observable that distinguishes one from another.
  const mark = page.locator('.identity img');
  await expect(mark).toHaveAttribute('src', '/brand/mark-white.png');
  await expect(mark).toHaveJSProperty('naturalWidth', 128);

  await expect(
    page.getByText(/lightweight fleet controller for GitHub Actions runners/),
  ).toBeVisible();
});

/**
 * The Events page is the one setting that changes another page, so what it
 * protects is that the switch means what it says: the Overview's feed carries
 * exactly the kinds left on, it says how many are off rather than quietly
 * omitting them, and the choice survives leaving the page.
 */
test('the Events page decides what the Overview’s feed carries', async ({ page }) => {
  await goto(page, '/settings/events', 'Events');
  const scaling = page.getByRole('switch', { name: 'Scaling decisions' });
  await expect(scaling, 'the scheduler’s decisions are on by default').toHaveAttribute(
    'aria-checked',
    'true',
  );
  await scaling.click();
  await expect(scaling).toHaveAttribute('aria-checked', 'false');

  await goto(page, '/', 'Overview');
  const feed = page.getByRole('region', { name: 'Recent events', exact: true });
  await expect(feed.getByRole('listitem').first()).toBeVisible();
  await expect(feed, 'the decisions are gone').not.toContainText('scaled zoomies-demo-');
  // Counted out loud rather than omitted silently: a panel that quietly left
  // things out would be worse than one that shows too much.
  await expect(feed).toContainText(/6 of \d+ kinds/);
  // What was not switched off is still there -- and it is not a scaling
  // decision, which is the whole point of the feed being a feed.
  await expect(feed).toContainText(/A job/);

  // And the choice is this browser's, remembered across the navigation back.
  await goto(page, '/settings/events', 'Events');
  const again = page.getByRole('switch', { name: 'Scaling decisions' });
  await expect(again).toHaveAttribute('aria-checked', 'false');
  await again.click();

  await goto(page, '/', 'Overview');
  await expect(feed).toContainText(/scaled zoomies-demo-/);
  await expect(feed).toContainText(/7 of \d+ kinds/);
});
