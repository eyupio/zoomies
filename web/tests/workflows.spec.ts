/**
 * The Workflows page: one row per workflow run, opening to the jobs inside.
 *
 * What GitHub's Actions tab lists, in this fleet's terms. The rows are runs
 * rather than jobs, each summing up the jobs GitHub reported under it, and
 * a run opens in place to those jobs and how each is getting on -- so an
 * operator who arrives with a run number from a pull request finds it, and
 * then finds what this fleet did with it, without leaving the page.
 */
import { expect, test, type Page } from '@playwright/test';
import {
  browserOverride,
  dataRows,
  FIXTURE,
  goto,
  grid,
  pageHeading,
  rowCount,
} from './support/fixtures';

test.use(browserOverride);

const runs = (page: Page) => grid(page, 'Workflow runs');

/** The row of status buttons above the grid, by the view it names. */
function view(page: Page, name: string) {
  return page.getByRole('group', { name: 'Filter jobs by status' }).getByRole('button', { name });
}

test('the page lists one row per run and opens on what is running', async ({ page }) => {
  await goto(page, '/workflows', 'Workflows');
  await expect(dataRows(runs(page)).first()).toBeVisible();

  // Three jobs are running on this fleet's runners, in two runs: the page
  // counts runs, not jobs.
  await expect(rowCount(page)).toContainText(`of ${FIXTURE.runningRuns} runs`);
  await expect(view(page, 'Running')).toHaveAttribute('aria-pressed', 'true');

  // A row is a run: it says how many jobs it holds and how they are getting on.
  const first = dataRows(runs(page)).first();
  await expect(first).toContainText('Running');
  await expect(first).toContainText(/\d+ running/);

  // Every run this fleet has a hand in, then everything GitHub reported.
  await view(page, 'All').click();
  await expect(rowCount(page)).toContainText(`of ${FIXTURE.managedRuns} runs`);
  await page.getByRole('switch', { name: 'Include other runners' }).click();
  await expect(rowCount(page)).toContainText(`of ${FIXTURE.totalRuns} runs`);
  await expect(page).toHaveURL(/[?&]all=true/);
});

test('a run opens in place to the jobs inside it, and closes again from the keyboard', async ({
  page,
}) => {
  // The one run with a failure this fleet caused: its row says so, and so
  // does the job under it.
  await goto(page, '/workflows?faulted=true', 'Workflows');
  const row = dataRows(runs(page)).first();
  await expect(row).toBeVisible();
  await expect(dataRows(runs(page))).toHaveCount(1);
  await expect(row).toContainText(`#${FIXTURE.faultedRun}`);
  await expect(row).toContainText('Runner lost');
  await expect(row).toContainText('1 succeeded · 1 failed');

  await row.getByRole('button', { name: 'Show jobs' }).click();
  const jobs = page.getByRole('table', { name: `Jobs in run #${FIXTURE.faultedRun}` });
  await expect(jobs).toBeVisible();
  await expect(jobs.locator('tbody tr')).toHaveCount(2);
  // Each job with its state and what went wrong, in the same words the Jobs
  // page uses: the fleet's own category rather than "failure" twice over.
  await expect(jobs).toContainText('Success');
  await expect(jobs).toContainText('Failure');
  await expect(jobs).toContainText('Out of memory');
  await expect(jobs).toContainText(FIXTURE.linuxPool);

  // A job opens the same drawer the Jobs page opens.
  await jobs
    .getByRole('button', { name: /^(build|test|lint|package)$/ })
    .first()
    .click();
  const drawer = page.getByRole('dialog');
  await expect(drawer).toBeVisible();
  await expect(drawer.getByRole('list', { name: 'Timeline' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(drawer).toBeHidden();

  // The arrows a tree uses close and open the run again.
  await row.focus();
  await page.keyboard.press('ArrowLeft');
  await expect(jobs).toBeHidden();
  await expect(row).toHaveAttribute('aria-expanded', 'false');
  await page.keyboard.press('ArrowRight');
  await expect(jobs).toBeVisible();
  await expect(row).toHaveAttribute('aria-expanded', 'true');
});

test('a re-run counts its latest attempt and still lists the earlier one', async ({ page }) => {
  await goto(page, '/workflows?state=completed&repo=acme%2Fwidgets', 'Workflows');
  const row = dataRows(runs(page))
    .filter({ hasText: `#${FIXTURE.rerunRun}` })
    .first();
  await expect(row).toBeVisible();
  await expect(row).toContainText('attempt 2');

  await row.getByRole('button', { name: 'Show jobs' }).click();
  const jobs = page.getByRole('table', { name: `Jobs in run #${FIXTURE.rerunRun}` });
  await expect(jobs).toContainText('attempt 2');
  await expect(jobs).toContainText('attempt 1');
});

test('a run links to the same run on GitHub, and the filters travel to the Jobs page', async ({
  page,
}) => {
  await goto(page, '/workflows?state=completed&repo=acme%2Fapi', 'Workflows');
  const row = dataRows(runs(page)).first();
  await expect(row).toBeVisible();
  await expect(row.getByRole('link', { name: /^Open run \d+ of/ })).toHaveAttribute(
    'href',
    /github\.com\/acme\/api\/actions\/runs\/\d+$/,
  );

  // The same history one step down, narrowed the same way.
  await page.getByRole('link', { name: 'Every job' }).click();
  await expect(pageHeading(page, 'Jobs')).toBeVisible();
  await expect(page).toHaveURL(/\/jobs\?/);
  await expect(page).toHaveURL(/state=completed/);
  await expect(page).toHaveURL(/repo=acme%2Fapi/);
  await expect(rowCount(page)).toContainText('jobs');

  // And back up.
  await page.getByRole('link', { name: 'Workflow runs' }).click();
  await expect(pageHeading(page, 'Workflows')).toBeVisible();
  await expect(page).toHaveURL(/\/workflows\?/);
  await expect(page).toHaveURL(/repo=acme%2Fapi/);
});
