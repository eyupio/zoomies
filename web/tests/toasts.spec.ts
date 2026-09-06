/**
 * The notification queue, and the one promise it makes.
 *
 * "Successes dismiss themselves, errors do not" is the whole contract: an
 * operator who looked away should still find out that something was refused.
 * The queue is capped, though, so the interesting case is a refusal followed by
 * a run of successes -- which is exactly what a bad afternoon looks like.
 */
import { expect, test, type Page } from '@playwright/test';
import { browserOverride, goto, grid } from './support/fixtures';

test.use(browserOverride);

const errorToasts = (page: Page) => page.locator('.toast[data-tone="error"]');
const successToasts = (page: Page) => page.locator('.toast[data-tone="success"]');

/** Tick every pool on the page and press one of the bulk buttons. */
async function bulkAction(page: Page, label: 'Enable' | 'Disable'): Promise<void> {
  await grid(page, 'Pools')
    .getByRole('checkbox', { name: /Select every pool/ })
    .check();
  await page
    .getByRole('group', { name: /Actions for the selected/ })
    .getByRole('button', { name: label })
    .click();
}

test('a refusal is not pushed off the screen by the successes that follow it', async ({ page }) => {
  await goto(page, '/pools', 'Pools');

  try {
    // One refusal, from the controller's point of view: the request is made and
    // comes back 500, which is the path an operator meets when a pool's host has
    // gone away underneath it.
    await page.route('**/api/v1/pools/*/disable', (route) =>
      route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ code: 'internal', message: 'The pool could not be reached.' }),
      }),
    );
    await bulkAction(page, 'Disable');
    await expect(errorToasts(page)).toHaveCount(1);
    await page.unroute('**/api/v1/pools/*/disable');

    // Four things that went fine. The queue shows four at a time, so under the
    // old rule -- evict the oldest, whatever it is -- the fourth success pushed
    // the refusal off the screen before anybody had read it.
    for (const label of ['Disable', 'Enable', 'Disable', 'Enable'] as const) {
      await bulkAction(page, label);
    }
    await expect(successToasts(page).first()).toBeVisible();
    await expect(errorToasts(page)).toHaveCount(1);
    await expect(errorToasts(page)).toContainText('could not be disabled');

    // And it goes when it is dismissed, not before.
    await errorToasts(page).getByRole('button', { name: 'Dismiss' }).click();
    await expect(errorToasts(page)).toHaveCount(0);
  } finally {
    // Put the fleet back. The suite shares one controller, so a pool left
    // disabled here is a pool that cannot place a runner in somebody else's
    // spec -- which is a failure a long way from its cause.
    const pools = await page.request.get('/api/v1/pools');
    for (const pool of ((await pools.json()).items ?? []) as { id?: string }[]) {
      if (pool.id) await page.request.post(`/api/v1/pools/${pool.id}/enable`);
    }
  }
});
