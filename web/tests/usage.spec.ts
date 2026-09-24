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
// Located by element rather than by role: on a phone the heading row is hidden
// and each figure carries its own heading instead, so a columnheader role does
// not resolve there. Whether the column exists at all is the question, and that
// is the same question at both widths.
const header = (page: Page, name: string) =>
  table(page).locator('thead th').filter({ hasText: name });

test('runner-hours are given by pool and dropped, with the reason, by repository', async ({
  page,
}) => {
  await goto(page, '/usage', 'Usage');

  // The default grouping is by pool, and the seeded fleet has had runners, so
  // the attributed columns are there.
  await expect(header(page, 'Runner-hours')).toHaveCount(1);
  await expect(header(page, 'Estimated cost')).toHaveCount(1);
  // Strings rather than regular expressions throughout: Playwright normalises
  // whitespace for strings, and the template wraps these sentences.
  await expect(page.getByText('cannot be attributed at this grouping')).toBeHidden();

  // Runner-hours is the eighth column: the key, then the three job counts,
  // peak, wait and executing time before it.
  const first = table(page).getByRole('row').nth(1);
  await expect(first.getByRole('cell').nth(6)).toHaveText(/^\d+\.\d\d$/);

  await page.getByLabel('Group by').selectOption('repository');

  await expect(page.getByText('never on behalf of a repository')).toBeVisible();
  await expect(header(page, 'Runner-hours')).toHaveCount(0);
  await expect(header(page, 'Estimated cost')).toHaveCount(0);

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

test('a backwards range switches the CSV link off for the keyboard too', async ({ page }) => {
  await goto(page, '/usage?since=2026-08-31&until=2026-08-01', 'Usage');

  // No link role at all, which is what an <a> without an href is. The styling
  // says it is off; this is what makes it true for somebody tabbing to it, who
  // a `pointer-events: none` rule does not reach.
  await expect(page.getByRole('link', { name: 'Export CSV' })).toHaveCount(0);
  const off = page.getByText('Export CSV');
  await expect(off).toBeVisible();
  await expect(off.locator('xpath=ancestor::a[1]')).toHaveAttribute('aria-disabled', 'true');
  await expect(off.locator('xpath=ancestor::a[1]')).not.toHaveAttribute('href', /./);

  // And it comes back the moment the range makes sense again.
  await page.getByLabel('From', { exact: true }).fill('2026-08-01');
  await page.getByLabel('From', { exact: true }).blur();
  await expect(page.getByRole('link', { name: 'Export CSV' })).toHaveCount(1);
});

test('changing grouping cancels a pending manual refresh', async ({ page }) => {
  await goto(page, '/usage', 'Usage');
  await expect(header(page, 'Runner-hours')).toHaveCount(1);
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route('**/api/v1/usage?**', async (route) => {
    if (new URL(route.request().url()).searchParams.get('group_by') !== 'pool') {
      await route.continue();
      return;
    }
    await gate;
    await route.abort().catch(() => {});
  });
  const started = page.waitForRequest((request) => request.url().includes('/api/v1/usage?'));
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  const old = await started;
  const cancelled = page.waitForEvent('requestfailed', { predicate: (request) => request === old });
  try {
    await page.getByLabel('Group by').selectOption('repository');
    await cancelled;
    await expect(
      table(page).getByRole('row', { name: new RegExp(FIXTURE.repos[0]) }),
    ).toBeVisible();
    await expect(header(page, 'Runner-hours')).toHaveCount(0);
  } finally {
    release();
  }
});

for (const theme of ['light', 'dark'] as const) {
  test(`date controls follow the explicit ${theme} theme instead of the system preference`, async ({
    page,
  }) => {
    await page.emulateMedia({ colorScheme: theme === 'light' ? 'dark' : 'light' });
    await page.addInitScript((choice) => localStorage.setItem('zoomies.theme', choice), theme);
    await goto(page, '/usage', 'Usage');
    await expect(page.getByLabel('From', { exact: true })).toHaveCSS('color-scheme', theme);
    await expect(page.getByLabel('to', { exact: true })).toHaveCSS('color-scheme', theme);
  });
}

test('analytics supports keyboard inspection, group focus and matching CSV exports', async ({
  page,
}) => {
  await goto(page, '/usage', 'Usage');
  const chart = page.getByRole('region', { name: 'Usage over time', exact: true });
  await expect(chart).toBeVisible();

  // Every figure is a chip and every chip is a line, so the two measures are
  // not two fixed pictures: the chart draws whichever figures are pressed,
  // and says which those are in its accessible name.
  const measures = chart.getByRole('group', { name: 'Measure' });
  await measures.getByRole('button', { name: 'Runner time' }).click();
  await expect(chart.getByRole('img', { name: /executing, allocated/ })).toBeVisible();
  const figures = chart.getByRole('group', { name: 'Figures shown' });
  await figures.getByRole('button', { name: 'Executing' }).click();
  await expect(chart.getByRole('img', { name: /Usage over time: allocated/ })).toBeVisible();
  // The rows below are the same switchboard, and the reading: a figure taken
  // off the chart leaves them too.
  const rows = chart.getByRole('group', { name: 'Figures read at this interval' });
  await expect(rows.getByRole('button', { name: /Executing/ })).toHaveCount(0);
  await expect(rows.getByRole('button', { name: /Allocated/ })).toHaveCount(1);
  await measures.getByRole('button', { name: 'Jobs' }).click();

  const matrix = page.getByRole('region', { name: 'Activity matrix', exact: true });
  // The grid is one tab stop: the square with tabindex 0 is the newest one
  // with anything in it. Focusing it opens the tooltip; Enter selects it and
  // opens the detail, which carries the same figures as text.
  const square = matrix.locator('[role="gridcell"][tabindex="0"]');
  await square.focus();
  await expect(square).toBeFocused();
  await expect(page.locator('.tip:popover-open')).toContainText('Queued');
  await square.press('Enter');
  // The report opens on a day, so a square is an hour and the detail is one.
  await expect(matrix.getByRole('region', { name: 'Selected hour' })).toContainText('Queued');
  // Choosing a square there brings its interval under the chart's crosshair,
  // which is the whole point of the two panels sharing a moment. Letting it
  // go lets go of both.
  const clear = chart.getByRole('button', { name: 'Back to the latest' });
  await expect(clear).toBeVisible();
  await clear.click();
  await expect(matrix.getByRole('region', { name: 'Selected hour' })).toHaveCount(0);
  const ranking = page.getByRole('region', { name: 'Execution by group', exact: true });
  await ranking.getByRole('button').first().click();
  await expect(page).toHaveURL(/entity=/);
  const key = new URL(page.url()).searchParams.get('entity');
  const href = await page.getByRole('link', { name: 'Export CSV' }).getAttribute('href');
  expect(new URL(href!, page.url()).searchParams.get('key')).toBe(key);
  await expect(table(page).locator('tbody tr')).toHaveCount(1);
  await page.getByLabel('Group by').selectOption('host');
  await expect(page).not.toHaveURL(/entity=/);
  await expect(header(page, 'Runner-hours')).toHaveCount(1);
  await expect(
    matrix.getByText('Capacity observations belong to pools.', { exact: false }),
  ).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

/**
 * The Usage page's matrix is drawn the way the Overview draws a window as
 * long, not in the report's own days: a month of days was five columns in
 * the corner of the panel. Its squares are then narrower than the chart's, so
 * choosing one has to find the chart's interval it falls in -- the day, not
 * the square's index -- or the crosshair would land a month out.
 */
test('a month is drawn in four-hour squares, and a square moves the crosshair to its day', async ({
  page,
}) => {
  const day = (d: Date) =>
    `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
  const since = day(new Date(Date.now() - 29 * 86_400_000));
  await goto(page, `/usage?since=${since}`, 'Usage');
  const matrix = page.getByRole('region', { name: 'Activity matrix', exact: true });
  const grid = matrix.getByRole('grid');
  await expect(grid).toHaveAccessibleName(/^\d weeks of every pool, one square per 4 hours$/);
  await expect(grid.getByRole('gridcell')).toHaveCount(30 * 6);
  const fits = await matrix
    .locator('.frame')
    .evaluate((el) => el.scrollWidth <= el.clientWidth + 1);
  expect(fits, 'the whole month fits the panel').toBe(true);

  // A square three days back, in the afternoon: the chart is a day to a
  // point, so its crosshair goes to the 27th of the month's 30 days.
  const chart = page.getByRole('region', { name: 'Usage over time', exact: true });
  const square = grid.locator(`[role="gridcell"][data-index="${27 * 6 + 3}"]`);
  await square.click();
  await expect(matrix.getByRole('region', { name: 'Selected hours' })).toBeVisible();
  await expect(chart.getByRole('slider', { name: 'Inspect an interval' })).toHaveValue('27');
  await chart.getByRole('button', { name: 'Back to the latest' }).click();
  await expect(matrix.getByRole('region', { name: 'Selected hours' })).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('one installation opens its report, with every figure defined in the docs', async ({
  page,
}) => {
  await goto(page, '/usage?group_by=installation', 'Usage');
  // Grouped by installation with none chosen, the page says where the report is.
  await expect(page.getByTestId('installation-report-hint')).toBeVisible();

  await goto(page, `/usage?group_by=installation&entity=${FIXTURE.installationId}`, 'Usage');
  const report = page.getByRole('region', { name: 'Installation report', exact: true });
  await expect(report).toBeVisible();
  for (const figure of ['Observed', 'Eligible', 'Created for', 'Ran here', 'Fleet fault']) {
    await expect(report.getByText(figure, { exact: true })).toBeVisible();
  }
  await expect(
    report.getByRole('rowheader', { name: 'Eligible to first create task' }),
  ).toBeVisible();
  await expect(report.getByRole('link', { name: 'How each figure is defined' })).toHaveAttribute(
    'href',
    /metrics\/#per-installation-report$/,
  );
  // It is per installation, so no repository appears in it.
  for (const repo of FIXTURE.repos) {
    await expect(report.getByText(repo)).toHaveCount(0);
  }

  // Its window is its own, and changing it asks again.
  const asked = page.waitForRequest((r) => r.url().includes('/report?window=168h'));
  await report.getByLabel('Report window').selectOption('168h');
  await asked;
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
