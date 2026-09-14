/**
 * Every table fits the window it is shown in.
 *
 * A table that scrolls sideways hides the column somebody is looking for behind
 * a gesture they have no reason to try, and takes the row's name off the left
 * edge the moment they do -- so the value they scrolled to belongs to a row they
 * can no longer name. The grids therefore divide whatever width there is and
 * truncate, and on a phone each row becomes a card and reads down instead.
 *
 * This is the file that holds that to be true, at the widths the product claims
 * to support: the reference desktop, the two documented breakpoints and either
 * side of them, and the two phone widths the mobile project uses.
 */
import { expect, test, type Page } from '@playwright/test';
import { browserOverride, dataRows, goto, grid, waitForRows } from './support/fixtures';

test.use(browserOverride);

/** Every page whose main content is a grid, with the name its table carries. */
const GRIDS = [
  { path: '/runners', heading: 'Runners', label: 'Runners' },
  { path: '/jobs?state=completed', heading: 'Jobs', label: 'Jobs' },
  { path: '/pools', heading: 'Pools', label: 'Pools' },
  { path: '/queue', heading: 'Queue', label: 'Provisioning queue' },
  { path: '/audit', heading: 'Audit', label: 'Audit log' },
] as const;

/**
 * The widths that matter: the reference desktop the screenshots are taken at,
 * `--z-bp-lg` and just below it, `--z-bp-md`, and the two phone widths.
 */
const WIDTHS = [1440, 1180, 1179, 768, 412, 360] as const;

/**
 * How far the table overflows the frame around it, and how far the document
 * overflows the window.
 *
 * Both, because they fail separately: a table can stay inside a frame that is
 * itself too wide for the page, and a page can hold together while the table in
 * it is quietly cut off.
 */
async function overflow(page: Page, label: string): Promise<{ table: number; document: number }> {
  return grid(page, label).evaluate((table) => {
    const frame = table.parentElement as HTMLElement;
    return {
      table: table.scrollWidth - frame.clientWidth,
      document: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    };
  });
}

/** Every table on the page, against the box each one is drawn in. */
async function expectTablesFit(page: Page, where: string): Promise<void> {
  const measured = await page.evaluate(() =>
    [...document.querySelectorAll('table')].map((table) => ({
      caption: (table.caption?.textContent ?? table.getAttribute('aria-label') ?? '?').trim(),
      over: table.scrollWidth - (table.parentElement as HTMLElement).clientWidth,
    })),
  );
  expect(measured.length, `${where} has a table to measure`).toBeGreaterThan(0);
  for (const { caption, over } of measured) {
    expect(over, `${caption} is wider than its frame, in ${where}`).toBeLessThanOrEqual(1);
  }
}

for (const width of WIDTHS) {
  test(`no grid scrolls sideways at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });

    for (const { path, heading, label } of GRIDS) {
      await goto(page, path, heading);
      await waitForRows(grid(page, label));

      // A pixel of slack: the columns are shared out by arithmetic, and a
      // fractional width rounded up is not a table anybody can scroll.
      const measured = await overflow(page, label);
      expect(measured.table, `the ${heading} table is wider than its frame`).toBeLessThanOrEqual(1);
      expect(measured.document, `the ${heading} page scrolls sideways`).toBeLessThanOrEqual(1);
    }
  });
}

/*
 * The phone does not merely fit: it keeps everything. A grid that fitted by
 * dropping columns would answer the wrong question -- an operator woken at 3am
 * is looking at a phone, and the column they need is the one that was dropped.
 */
test('every column is still there when the rows become cards', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 780 });
  await goto(page, '/runners', 'Runners');
  const runners = grid(page, 'Runners');
  await waitForRows(runners);

  // The heading row is out of the way, so each cell says what it is instead.
  const labels = await dataRows(runners)
    .first()
    .locator('td')
    .evaluateAll((cells) => cells.map((cell) => cell.getAttribute('data-label')));
  for (const heading of ['State', 'Name', 'Pool', 'Host', 'Current job', 'CPU', 'Memory']) {
    expect(labels, `a card names its ${heading}`).toContain(heading);
  }

  // And the two things a heading row does that a card cannot are kept: the
  // sort, and the tick that takes every row on the page.
  await expect(runners.getByRole('checkbox', { name: /^Select every/ })).toBeVisible();
  await runners.getByRole('columnheader', { name: 'Age' }).getByRole('button').click();
  await expect(page).toHaveURL(/sort=created_at/);
});

/*
 * The tables that are not grids: the usage report, and the two lists in
 * Settings. They are hand-written markup rather than `DataGrid`, so they get
 * the same treatment by hand -- and the same guarantee here, since a rule that
 * holds only where it was easy to apply is not a rule.
 *
 * Settings starts with neither an account beyond the signed-in one nor a token,
 * so each is given a row. Unique names, so a retry does not trip over a
 * leftover from a failed run.
 */
test('the tables that are not grids fit the window too', async ({ page, request }) => {
  const stamp = `${Date.now()}`;
  let userId = '';
  let tokenId = '';
  try {
    const user = await request.post('/api/v1/users', {
      data: {
        username: `e2e-tables-${stamp}`,
        password: 'correct horse battery staple',
        display_name: 'Someone With A Long Display Name',
        email: `e2e-tables-${stamp}@example.com`,
        role: 'viewer',
      },
    });
    expect(user.ok(), 'the account was created').toBeTruthy();
    userId = ((await user.json()) as { id: string }).id;
    const token = await request.post('/api/v1/tokens', {
      data: {
        name: `e2e-tables-${stamp}`,
        role: 'operator',
        scopes: ['pools:read', 'runners:read', 'jobs:read'],
        expires_in: '1h',
      },
    });
    expect(token.ok(), 'the token was created').toBeTruthy();
    tokenId = ((await token.json()) as { id: string }).id;

    for (const width of WIDTHS) {
      await page.setViewportSize({ width, height: 900 });

      await goto(page, '/usage', 'Usage');
      await expect(page.getByRole('table')).toBeVisible();
      await expectTablesFit(page, `the usage report at ${width}px`);

      await goto(page, '/settings', 'Settings');
      await expect(page.getByRole('table', { name: 'Accounts' })).toBeVisible();
      await expectTablesFit(page, `the accounts list at ${width}px`);

      await page.getByRole('tab', { name: 'API tokens' }).click();
      await expect(page.getByRole('table', { name: 'API tokens' })).toBeVisible();
      await expectTablesFit(page, `the API tokens list at ${width}px`);
    }
  } finally {
    // A token cannot be deleted, only revoked: its row stays, marked revoked,
    // so the audit trail keeps pointing at something.
    if (tokenId) await request.delete(`/api/v1/tokens/${tokenId}`);
    if (userId) await request.delete(`/api/v1/users/${userId}`);
  }
});
