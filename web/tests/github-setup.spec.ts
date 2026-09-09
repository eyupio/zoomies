/**
 * The address GitHub sends an operator back to.
 *
 * Every GitHub App this controller builds names `/settings/github/setup` as its
 * redirect and setup URL, so the address outlives the release that created the
 * App: it is not a page anybody navigates to, it is a promise made to GitHub.
 * When the router did not know it, the operator's reward for confirming the App
 * on GitHub was "Page not found" with the single-use code in the address bar.
 */
import { expect, test } from '@playwright/test';
import { browserOverride, goto, pageHeading } from './support/fixtures';

test.use(browserOverride);

test('the App creation callback lands on Installations with the code in hand', async ({ page }) => {
  await goto(page, '/settings/github/setup?code=abc123&state=xyz789', 'Installations');

  // Forwarded, not merely rendered: the flow lives on the Installations page.
  await expect(page).toHaveURL(/\/installations\?/);
  await expect(page).toHaveURL(/code=abc123/);
  await expect(page).toHaveURL(/state=xyz789/);

  // The connect flow is open at the step that spends the code, with the code
  // already in it -- nobody should have to copy it out of the address bar.
  const dialog = page.getByRole('dialog', { name: 'Connect GitHub' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel('Code from GitHub')).toHaveValue('abc123');

  // Going back must not land on the callback again and re-spend the code.
  await page.goBack();
  await expect(pageHeading(page, 'Page not found')).toBeHidden();
});

test('the callback with nothing on it is still a page, not a dead end', async ({ page }) => {
  await goto(page, '/settings/github/setup', 'Installations');
  await expect(page.getByRole('dialog', { name: 'Connect GitHub' })).toBeHidden();
});

/**
 * Verify, driven from the browser, with no fake in the way.
 *
 * The Playwright suite drives the real binary, but its GitHub is an in-process
 * fake the browser cannot reach -- so the connect and verify paths were tested
 * at the API and never through the dialog an operator actually uses. The demo
 * installation closes that gap without any credentials: its probe answers
 * deterministically, with every permission granted and `workflow_job`
 * subscribed, so the dialog has something real to render.
 */
test('the verify dialog says what the credentials can do', async ({ page }) => {
  await goto(page, '/installations', 'Installations');

  const card = page.getByRole('article').first();
  await expect(card).toBeVisible();
  await card.getByRole('button', { name: /Verify/ }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  // The verdict in a sentence, then the permissions by name: "403" is what the
  // dialog exists to avoid showing.
  await expect(dialog).toContainText(/credentials work/i);
  await expect(dialog).toContainText(/self.hosted runners|organization_self_hosted_runners/i);
  await expect(dialog).toContainText('workflow_job');
  // And nothing is reported missing on an installation that has everything --
  // a dialog that lists a gap on a healthy App teaches an operator to ignore it.
  await expect(dialog).not.toContainText(/is not granted/i);

  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
});
