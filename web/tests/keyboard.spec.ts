/**
 * The shortcuts the guidelines promise, from the keyboard alone.
 *
 * These are the ones an operator learns and then relies on: `g` then a letter
 * to reach any page, `/` to get into the search on whatever page they are on,
 * `?` to be reminded of the rest. They are also the ones most easily broken by
 * a change somewhere else, because they are global listeners on a window that
 * every page shares -- and, until this file, nothing tested them at all.
 *
 * The chord's refusals matter as much as the chord: `g r` typed into an open
 * confirmation must not navigate away from the thing being confirmed, and `g`
 * typed into a search box is a letter, not a shortcut.
 */
import { expect, test, type Page } from '@playwright/test';
import { browserOverride, FIXTURE, goto, isFocused, pageHeading } from './support/fixtures';

test.use(browserOverride);

/** `g` then a letter, with the pause an operator actually leaves between them. */
async function chord(page: Page, letter: string): Promise<void> {
  await page.keyboard.press('g');
  await page.keyboard.press(letter);
}

// The whole list, from docs/ui-guidelines.md section 4. Each is checked from
// the Overview so a failure names the destination rather than the starting
// point, and the headings are the pages' own <h1>s.
const JUMPS = [
  { key: 'p', path: '/pools', heading: 'Pools' },
  { key: 'r', path: '/runners', heading: 'Runners' },
  { key: 'j', path: '/jobs', heading: 'Jobs' },
  { key: 'u', path: '/usage', heading: 'Usage' },
  { key: 'h', path: '/hosts', heading: 'Hosts' },
  { key: 'i', path: '/installations', heading: 'Installations' },
  { key: 'm', path: '/migrate', heading: 'Migrate repositories' },
  { key: 'a', path: '/audit', heading: 'Audit' },
  { key: 's', path: '/settings', heading: 'Settings' },
] as const;

test('g then a letter reaches every page in the navigation', async ({ page }) => {
  await goto(page, '/', 'Overview');

  for (const jump of JUMPS) {
    await chord(page, jump.key);
    await expect(
      pageHeading(page, jump.heading),
      `g ${jump.key} reaches ${jump.path}`,
    ).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`${jump.path}$`));

    // Back to the Overview by its own chord, which checks `g o` nine times
    // over without a test of its own.
    await chord(page, 'o');
    await expect(pageHeading(page, 'Overview')).toBeVisible();
  }
});

test('a chord typed into a field is text, and one typed under a dialog goes nowhere', async ({
  page,
}) => {
  await goto(page, '/runners', 'Runners');
  const search = page.getByRole('searchbox', { name: 'Search runners' });

  // Into a text field, `g` is the letter g: the chord never fires, so the page
  // does not change under somebody in the middle of typing a runner's name.
  //
  // And the field keeps the keyboard while it does. The search writes itself
  // into the query string, which used to re-run the shell's post-navigation
  // focus move and hand the keyboard to the page heading -- so the second
  // character of every search was swallowed, on every page that has one.
  await search.click();
  await chord(page, 'r');
  await expect(page).toHaveURL(/\/runners/);
  await expect(search).toHaveValue('gr');
  expect(await isFocused(search), 'the search still has the keyboard').toBe(true);
  await search.fill('');

  // Under an open dialog the keyboard belongs to the dialog: a jump typed
  // there must not navigate away from the thing waiting for an answer.
  await goto(page, `/runners/${FIXTURE.busyRunnerId}`, FIXTURE.busyRunner);
  await page.getByRole('button', { name: 'Drain', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();

  await chord(page, 'p');
  await expect(dialog, 'the dialog is still waiting').toBeVisible();
  await expect(page).not.toHaveURL(/\/pools/);

  // Escape is the one shortcut an overlay still lets through.
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
});

test('slash focuses the search on a page that has one, and the palette on a page that does not', async ({
  page,
}) => {
  await goto(page, '/runners', 'Runners');
  const search = page.getByRole('searchbox', { name: 'Search runners' });
  await expect(search).not.toBeFocused();

  await page.keyboard.press('/');
  expect(await isFocused(search), 'slash lands in the page search').toBe(true);

  // Typed again from inside the field it is a slash, not a shortcut -- a
  // runner name can contain one.
  await page.keyboard.press('/');
  await expect(search).toHaveValue('/');
  await search.fill('');

  // The Overview has nothing of its own to search, so it falls back to the
  // palette, which can search everything.
  await goto(page, '/', 'Overview');
  await page.keyboard.press('/');
  await expect(page.getByRole('dialog', { name: 'Command palette' })).toBeVisible();
});

test('question mark opens the shortcut list, and it agrees with the keys themselves', async ({
  page,
}) => {
  await goto(page, '/', 'Overview');
  await page.keyboard.press('?');

  const sheet = page.getByRole('dialog', { name: 'Keyboard shortcuts' });
  await expect(sheet).toBeVisible();

  // Lower case, because that is the key an operator presses and what the
  // navigation shows beside each entry.
  await expect(sheet.getByText('Focus the search on this page')).toBeVisible();
  for (const jump of JUMPS) {
    const row = sheet.locator('.row', { hasText: jump.heading }).first();
    await expect(row.locator('kbd').first(), `${jump.heading} is reached with g`).toHaveText('g');
    await expect(row.locator('kbd').nth(1)).toHaveText(jump.key);
  }

  await page.keyboard.press('Escape');
  await expect(sheet).toBeHidden();
});

test('Home and End move tab focus together with the selected panel', async ({ page }) => {
  await goto(page, '/settings?tab=appearance', 'Settings');
  const tabs = page.getByRole('tablist', { name: 'Settings sections' });
  await tabs.getByRole('tab', { name: 'Appearance' }).focus();
  await page.keyboard.press('End');
  await expect(tabs.getByRole('tab', { name: 'About' })).toBeFocused();
  await expect(tabs.getByRole('tab', { name: 'About' })).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('Home');
  await expect(tabs.getByRole('tab', { name: 'Users', exact: true })).toBeFocused();
  await expect(tabs.getByRole('tab', { name: 'Users', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
});
