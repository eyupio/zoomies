/**
 * Which failures are offered to Eli at all.
 *
 * Eli pushes one commit to an existing pull request's branch and never raises a
 * pull request, so a failed push, merge or scheduled run has nothing it could
 * repair. The Overview's failures panel and a job's own drawer used to offer
 * "Fix with Eli" on every failure and let the repair page explain, one click
 * later, that there was no pull request. The controller now records what
 * started each run (`run_event`), and the button is simply not there for one
 * that no pull request started.
 */
import { expect, test, type Page } from '@playwright/test';
import { goto, readThrough, signedInAs } from './support/fixtures';

/** The real failures the fixture holds, with the trigger of each chosen by position. */
async function triggeredBy(page: Page, events: (index: number) => string | undefined) {
  await page.route('**/api/v1/jobs?*', async (route) => {
    const read = await readThrough<{ items: Record<string, unknown>[] }>(route);
    if (!read) return;
    const { response: response, body } = read;
    body.items.forEach((job, index) => {
      job.github_run_id = job.github_run_id || 9000 + index;
      job.repo = job.repo || 'acme/site';
      const event = events(index);
      if (event === undefined) delete job.run_event;
      else job.run_event = event;
    });
    return route.fulfill({ response, json: body });
  });
}

test('a failure of a push run is not offered to Eli, and a pull request failure is', async ({
  page,
}) => {
  await signedInAs(page, 'operator');
  await triggeredBy(page, (i) => (i % 2 === 0 ? 'push' : 'pull_request'));
  await goto(page, '/', 'Overview');
  await page.locator('#failures > summary > .title').click();

  const failures = page.getByRole('region', { name: 'Recent failures', exact: true });
  const rows = failures.getByRole('listitem');
  await expect(rows.nth(1)).toBeVisible();
  await expect(rows.nth(0).getByRole('link', { name: 'Fix with Eli' })).toHaveCount(0);
  await expect(rows.nth(1).getByRole('link', { name: 'Fix with Eli' })).toBeVisible();
});

test('a failure whose trigger is not known yet is still offered', async ({ page }) => {
  await signedInAs(page, 'operator');
  await triggeredBy(page, () => undefined);
  await goto(page, '/', 'Overview');
  await page.locator('#failures > summary > .title').click();

  const failures = page.getByRole('region', { name: 'Recent failures', exact: true });
  await expect(failures.getByRole('link', { name: 'Fix with Eli' }).first()).toBeVisible();
});

test("a push failure's own drawer does not offer Eli either, where a pull request failure's does", async ({
  page,
}) => {
  await signedInAs(page, 'operator');
  await triggeredBy(page, (i) => (i % 2 === 0 ? 'push' : 'pull_request'));
  await goto(page, '/', 'Overview');
  await page.locator('#failures > summary > .title').click();

  const rows = page
    .getByRole('region', { name: 'Recent failures', exact: true })
    .getByRole('listitem');
  await expect(rows.nth(1)).toBeVisible();
  const open = async (index: number) => {
    await rows
      .nth(index)
      .getByRole('button', { name: /^Inspect/ })
      .click();
    const drawer = page.getByRole('dialog');
    await expect(drawer).toBeVisible();
    return drawer;
  };

  const pr = await open(1);
  await expect(pr.getByRole('link', { name: 'Fix with Eli' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(pr).toBeHidden();

  const push = await open(0);
  await expect(push.getByRole('link', { name: 'Fix with Eli' })).toHaveCount(0);
});
