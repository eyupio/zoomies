/**
 * The pages keep themselves current.
 *
 * Refreshing is a thing an operator may do, never a thing they must, and this
 * protects the second half: a change made somewhere else -- another operator's
 * tab, the CLI, an automation -- appears on the page that is already open,
 * without anyone pressing anything. Each test makes the change over the API,
 * the way any other client would, and watches the page it is looking at.
 *
 * Each test also plants a value on `window` before the change and checks it
 * is still there afterwards, because a page that got its new numbers by
 * reloading itself would pass a weaker test and still be the bug.
 */
import { expect, test, type APIRequestContext, type Locator, type Page } from '@playwright/test';
import {
  browserOverride,
  dataRows,
  expectNoReload,
  FIXTURE,
  goto,
  grid,
  plantMarker,
} from './support/fixtures';

test.use(browserOverride);

interface Named {
  id: string;
  name: string;
}

/** A fixture, found by name the way the tests find it on screen. */
async function findByName(request: APIRequestContext, path: string, name: string): Promise<Named> {
  const body = (await request.get(path).then((r) => r.json())) as { items: Named[] };
  const found = body.items.find((item) => item.name === name);
  expect(found, `${name} is listed by ${path}`).toBeTruthy();
  return found as Named;
}

/** The top bar's problems bell, whose label carries the count. */
function bell(page: Page): Locator {
  return page.getByRole('button', { name: /^Problems\./ });
}

/** "1 error and 2 warnings need your attention." -> 3 */
function countIn(label: string | null): number {
  let total = 0;
  for (const match of (label ?? '').matchAll(/(\d+) (error|warning|note)s?/g)) {
    total += Number(match[1]);
  }
  return total;
}

test('a host cordoned from elsewhere says so on the Hosts page', async ({ page }) => {
  const host = await findByName(page.request, '/api/v1/hosts', 'demo-builder-2');
  await goto(page, '/hosts', 'Hosts');
  const card = page.getByRole('article', { name: host.name });
  await expect(card).toBeVisible();
  await expect(card).not.toContainText('Cordoned.');
  await plantMarker(page);

  try {
    const response = await page.request.post(`/api/v1/hosts/${host.id}/cordon`, {
      data: { cordoned: true },
    });
    expect(response.ok()).toBeTruthy();
    await expect(card).toContainText('Cordoned.');
    await expectNoReload(page);
  } finally {
    await page.request.post(`/api/v1/hosts/${host.id}/cordon`, { data: { cordoned: false } });
  }
  // And back again, the same way.
  await expect(card).not.toContainText('Cordoned.');
});

test('a pool edited from elsewhere changes in the Pools grid', async ({ page }) => {
  const pool = await findByName(page.request, '/api/v1/pools', FIXTURE.linuxPool);
  await goto(page, '/pools', 'Pools');
  const row = dataRows(grid(page, 'Pools')).filter({ hasText: FIXTURE.linuxPool });
  await expect(row).toContainText('5m 00s');
  await plantMarker(page);

  try {
    const response = await page.request.patch(`/api/v1/pools/${pool.id}`, {
      data: { idle_timeout: '6m' },
    });
    expect(response.ok()).toBeTruthy();
    await expect(row).toContainText('6m 00s');
    await expectNoReload(page);
  } finally {
    await page.request.patch(`/api/v1/pools/${pool.id}`, { data: { idle_timeout: '5m' } });
  }
  await expect(row).toContainText('5m 00s');
});

test('a new risk on a pool reaches the problems bell on whatever page is open', async ({
  page,
}) => {
  const pool = await findByName(page.request, '/api/v1/pools', FIXTURE.armPool);
  // The Jobs page has nothing to do with pools, which is the point: the bell
  // follows the operator everywhere.
  await goto(page, '/jobs', 'Jobs');
  await expect(bell(page)).toBeVisible();
  const before = countIn(await bell(page).getAttribute('aria-label'));
  await plantMarker(page);

  try {
    const response = await page.request.patch(`/api/v1/pools/${pool.id}`, {
      data: { run_as_root: true },
    });
    expect(response.ok()).toBeTruthy();
    await expect
      .poll(async () => countIn(await bell(page).getAttribute('aria-label')), {
        message: 'the bell counts the new warning',
      })
      .toBe(before + 1);
    await expectNoReload(page);
  } finally {
    await page.request.patch(`/api/v1/pools/${pool.id}`, { data: { run_as_root: false } });
  }
  await expect
    .poll(async () => countIn(await bell(page).getAttribute('aria-label')), {
      message: 'the bell forgets the warning once the setting is undone',
    })
    .toBe(before);
});

test('a dropped stream says so, and the page catches up by itself when it returns', async ({
  page,
}) => {
  // A page which cannot hear the controller has to say so rather than quietly
  // showing stale numbers, and has to reconcile once it can hear again. The
  // refresh button is not the answer to either: nobody watching a dashboard is
  // there to press it. Nothing tested either half.
  await goto(page, '/runners', 'Runners');
  const rows = dataRows(grid(page, 'Runners'));
  await expect(rows.first()).toBeVisible();

  const connection = page.locator('.connection');
  await expect(connection).toHaveAttribute('data-state', 'live');

  // Cut the stream. Aborting is what a proxy, a laptop lid or a controller
  // restart looks like from here.
  let cut = true;
  await page.route('**/api/v1/events*', async (route) => {
    if (cut) return route.abort('connectionfailed');
    return route.fallback();
  });
  await page.evaluate(() => window.dispatchEvent(new Event('offline')));

  await expect(connection, 'the top bar stops claiming to be live').not.toHaveAttribute(
    'data-state',
    'live',
  );
  await expect(page.locator('.connection-text')).toHaveText(/Reconnecting|Offline|Connecting/);

  // Let it back through. The stream reopens on its own and the page returns to
  // live without anybody pressing anything.
  cut = false;
  await page.evaluate(() => window.dispatchEvent(new Event('online')));
  await expect(connection, 'and comes back by itself').toHaveAttribute('data-state', 'live', {
    timeout: 20_000,
  });
  // And the rows are still the rows: catching up is a reconcile, not a reload.
  await expect(rows.first()).toBeVisible();
});

test('an optimistic change that the controller refuses is put back', async ({ page }) => {
  // Optimism is what makes the grid feel immediate, and a refusal that left the
  // wrong answer on screen would be worse than no optimism at all: the operator
  // would believe the pool was disabled and walk away.
  await goto(page, '/pools', 'Pools');
  const rows = dataRows(grid(page, 'Pools'));
  const linux = rows.filter({ hasText: FIXTURE.linuxPool });
  await expect(linux).toContainText('Enabled');

  await page.route('**/api/v1/pools/*/disable', (route) =>
    route.fulfill({
      status: 409,
      contentType: 'application/json',
      body: JSON.stringify({
        error: { code: 'conflict', message: 'That pool changed underneath you.' },
      }),
    }),
  );

  await linux.getByRole('button', { name: new RegExp(`Actions for ${FIXTURE.linuxPool}`) }).click();
  await page.getByRole('menuitem', { name: 'Disable' }).click();

  // The refusal is reported, and the row is what the controller says it is
  // rather than what the click hoped for.
  await expect(page.locator('.toast[data-tone="error"]')).toBeVisible();
  await expect(linux, 'the row rolled back').toContainText('Enabled');
});
