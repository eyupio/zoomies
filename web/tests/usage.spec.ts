/**
 * The Usage page.
 *
 * Runner-hours belong to pools and installations: a runner idles on behalf of
 * a pool, never on behalf of a repository or a workflow. This protects that
 * the page drops those columns and says why when asked to group by repository,
 * rather than printing a column of zeros that read as "this repository used
 * nothing", and that the three job counts are there in its place.
 *
 * It also protects the thing that makes a report worth having: the range and
 * the grouping are in the address bar, so the page an operator is looking at is
 * the page they can send to somebody else.
 */
import { expect, test, type Page } from '@playwright/test';
import { browserOverride, FIXTURE, goto } from './support/fixtures';

test.use(browserOverride);

const table = (page: Page) => page.getByRole('table');
const header = (page: Page, name: string) => table(page).getByRole('columnheader', { name });

test('runner-hours are given by pool and dropped, with the reason, by repository', async ({
  page,
}) => {
  await goto(page, '/usage', 'Usage');

  // The default grouping is by pool, and the seeded fleet has had runners, so
  // the attributed columns are there.
  await expect(header(page, 'Runner-hours')).toBeVisible();
  await expect(header(page, 'Estimated cost')).toBeVisible();
  // Strings rather than regular expressions throughout: Playwright normalises
  // whitespace for strings, and the template wraps these sentences.
  await expect(page.getByText('cannot be attributed at this grouping')).toBeHidden();

  // Runner-hours is the eighth column: the key, then the three job counts,
  // peak, wait and executing time before it.
  const first = table(page).getByRole('row').nth(1);
  await expect(first.getByRole('cell').nth(6)).toHaveText(/^\d+\.\d\d$/);

  await page.getByLabel('Group by').selectOption('repository');

  await expect(page.getByText('never on behalf of a repository')).toBeVisible();
  await expect(header(page, 'Runner-hours')).toBeHidden();
  await expect(header(page, 'Estimated cost')).toBeHidden();

  // What is left is honest: a row per seeded repository, with the queued,
  // started and completed counts as whole numbers.
  const api = table(page).getByRole('row', { name: new RegExp(FIXTURE.repos[0]) });
  await expect(api).toBeVisible();
  for (const column of [0, 1, 2]) {
    await expect(api.getByRole('cell').nth(column)).toHaveText(/^\d+$/);
  }
});

test('the report is a link: the range and the grouping live in the address bar', async ({
  page,
}) => {
  await goto(page, '/usage?since=2026-08-01&until=2026-08-31&group_by=workflow', 'Usage');

  await expect(page.getByLabel('Group by')).toHaveValue('workflow');
  // Exact: the range's second leg is labelled "to", which is a substring of
  // half the accessible names on the page.
  await expect(page.getByLabel('From', { exact: true })).toHaveValue('2026-08-01');
  await expect(page.getByLabel('to', { exact: true })).toHaveValue('2026-08-31');

  // Changing the grouping rewrites the address rather than needing an Apply
  // button, so a reload lands on the same report.
  await page.getByLabel('Group by').selectOption('pool');
  await expect(page).toHaveURL(/since=2026-08-01/);
  await expect(page).toHaveURL(/until=2026-08-31/);
  // Pool is the default, so it drops out rather than being spelled out.
  await expect(page).not.toHaveURL(/group_by=/);

  // And the CSV carries the same range, so the file is the report on screen.
  const csv = page.getByRole('link', { name: 'Export CSV' });
  await expect(csv).toHaveAttribute('href', /from=2026-08-01T/);
  await expect(csv).toHaveAttribute('href', /to=2026-08-31T/);
});
