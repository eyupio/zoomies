/**
 * Moving a repository's CI onto this fleet.
 *
 * The demo installation has no GitHub App behind it, which makes it exactly the
 * right fixture for this: the reads answer, so the wizard can be walked to the
 * screen that matters, and the App has none of the three permissions a pull
 * request needs -- so the last step must refuse rather than get halfway.
 */
import { expect, test } from '@playwright/test';
import { FIXTURE, browserOverride, goto, navEntry, pageHeading } from './support/fixtures';

test.use(browserOverride);

test.skip(({ isMobile }) => isMobile, 'the wizard is a desktop task');

/** Walk to a step, leaving the wizard on it. */
async function walkTo(page: import('@playwright/test').Page, step: number): Promise<void> {
  await goto(page, '/migrate', 'Migrate repositories');
  // One installation, so it is already chosen.
  await expect(page.getByRole('radio', { name: 'acme', exact: false })).toBeChecked();
  for (let i = 0; i < step; i++) {
    await page.getByRole('button', { name: 'Next' }).click();
  }
}

test('the navigation reaches the wizard', async ({ page }) => {
  await goto(page, '/', 'Overview');
  await navEntry(page, '/migrate').click();
  await expect(pageHeading(page, 'Migrate repositories')).toBeVisible();
});

test('the scan lists every repository and says what each one would get', async ({ page }) => {
  await walkTo(page, 1);

  await expect(page.getByRole('heading', { level: 2, name: 'Repositories' })).toBeVisible();
  for (const repo of FIXTURE.repos) {
    // Each is ticked, because each has a job that would move.
    await expect(page.getByRole('checkbox', { name: repo, exact: false })).toBeChecked();
  }
  // The count is the scan's own, not a guess.
  await expect(page.getByText('1 job in 1 file').first()).toBeVisible();
});

test('a repository with nothing to move is hidden, and can be shown', async ({ page }) => {
  await walkTo(page, 1);

  // The default: the repositories that would change, and a count of what that
  // hides -- not a list of an organisation's documentation repositories.
  const quiet = page.getByRole('checkbox', { name: FIXTURE.quietRepo, exact: false });
  await expect(quiet).toBeHidden();
  await expect(page.getByText('with nothing to move')).toBeVisible();

  await page.getByRole('switch', { name: 'Only repositories with something to move' }).click();
  await expect(quiet).toBeVisible();
  // It is listed so the operator can see it was looked at, and it cannot be
  // chosen, because there is nothing in it to choose.
  await expect(quiet).toBeDisabled();
  await expect(page.getByText('No workflows')).toBeVisible();
});

test('a repository with several workflows is chosen file by file', async ({ page }) => {
  await walkTo(page, 1);

  const repo = page.getByRole('checkbox', { name: FIXTURE.multiWorkflowRepo, exact: false });
  await expect(repo).toBeChecked();

  await page.getByRole('button', { name: '2 files' }).click();
  const release = page.getByRole('checkbox', { name: '.github/workflows/release.yml' });
  await expect(release).toBeChecked();
  await release.click();

  // The repository is now partly chosen, and says so.
  await expect(repo).not.toBeChecked();
  await expect(page.getByText('1 chosen')).toBeVisible();

  // And the review only offers to change the file that is still ticked.
  await page.getByRole('button', { name: 'Next' }).click();
  await page.getByRole('button', { name: 'Next' }).click();
  await expect(page.getByText('.github/workflows/release.yml')).toBeHidden();
  await expect(page.getByText('.github/workflows/ci.yml').first()).toBeVisible();
});

test('the mapping step proposes the pool that matches the hosted label', async ({ page }) => {
  await walkTo(page, 2);

  await expect(page.getByRole('heading', { level: 2, name: 'Labels' })).toBeVisible();
  await expect(page.getByText('ubuntu-latest', { exact: true })).toBeVisible();

  // ubuntu-latest is x64 Linux, and so is exactly one of the two demo pools.
  const select = page.getByLabel('What replaces ubuntu-latest');
  await expect(select).toHaveValue(FIXTURE.linuxPool);
  // Leaving it alone is always an option, and it is a real one.
  await expect(select.getByRole('option', { name: /Leave it alone/ })).toHaveCount(1);
});

test('the review step shows the diff and the jobs it will not touch', async ({ page }) => {
  await walkTo(page, 3);

  await expect(page.getByRole('heading', { level: 2, name: 'Review' })).toBeVisible();

  // The exact change, as a diff -- not a sentence describing one.
  const diff = page.getByRole('group', { name: 'The change to this file' }).first();
  await expect(diff).toContainText('-    runs-on: ubuntu-latest');
  await expect(diff).toContainText(`+    runs-on: ${FIXTURE.linuxPool}`);

  // And the job it deliberately leaves behind, with the reason.
  await page
    .getByRole('group', { name: 'The change to this file' })
    .first()
    .scrollIntoViewIfNeeded();
  const skips = page.getByText('left alone in this repository').first();
  await expect(skips).toBeVisible();
  await skips.click();
  await expect(page.getByText('${{ }} expression').first()).toBeVisible();
});

test('an App without the permissions is stopped here, not halfway through', async ({ page }) => {
  await walkTo(page, 3);

  const blocker = page.getByRole('alert');
  await expect(blocker).toContainText('cannot open a pull request yet');
  // Each missing permission is named the way GitHub's own settings page names
  // it, so the fix is a search on that page rather than a guess.
  for (const name of ['Contents', 'Pull requests', 'Workflows']) {
    await expect(blocker).toContainText(name);
  }
  await expect(blocker.getByRole('link', { name: /Open the App's permissions/ })).toHaveAttribute(
    'href',
    /settings\/apps\//,
  );

  // Nothing can be opened while that is true.
  await expect(page.getByRole('button', { name: 'Open the pull requests' })).toBeDisabled();
});
