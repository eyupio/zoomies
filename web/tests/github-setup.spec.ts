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

// Device emulation tests the actual form requests, not Android's OS intent
// dispatcher. Physical Android validation is still required for the chooser.
test('GitHub creation and installation use direct native forms and survive a reload', async ({
  page,
}) => {
  await page.route('**/api/v1/meta', async (route) => {
    const response = await route.fetch();
    const meta = (await response.json()) as Record<string, unknown>;
    // The shared fixture is loopback-only, which is the right truth for most of
    // the suite. This path is about the browser handoff itself, so it needs the
    // public URL the dialog now insists on before it will build a manifest.
    meta.external_url = 'https://zoomies.example.test';
    meta.webhook_url = 'https://zoomies.example.test/webhooks/github';
    await route.fulfill({ response, json: meta });
  });
  await page.route('**/api/v1/installations/manifest', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        post_url: 'https://github.com/settings/apps/new',
        manifest: '{"name":"zoomies-acme"}',
        state: 'same-tab-state',
      }),
    });
  });
  await goto(page, '/installations', 'Installations');

  await page.getByRole('button', { name: 'Connect GitHub' }).first().click();
  const dialog = page.getByRole('dialog', { name: 'Connect GitHub' });
  await dialog.getByLabel('Organisation login').fill('acme');
  await dialog.getByRole('button', { name: 'Continue to GitHub' }).click();

  const handoff = page.locator('form#github-manifest');
  await expect(handoff).toHaveAttribute('target', '_self');
  await expect(handoff).toHaveAttribute(
    'action',
    'https://github.com/settings/apps/new?state=same-tab-state',
  );
  await expect(handoff.locator('input[name="manifest"]')).toHaveValue('{"name":"zoomies-acme"}');
  await expect(dialog.getByText(/in this tab/i)).toBeVisible();

  await page.route('https://github.com/**', (route) =>
    route.fulfill({ contentType: 'text/html', body: '<h1>GitHub test destination</h1>' }),
  );
  const creation = page.waitForRequest('https://github.com/settings/apps/new?state=same-tab-state');
  await dialog.getByRole('button', { name: 'Create the App on GitHub' }).click();
  const request = await creation;
  expect(request.method()).toBe('POST');
  expect(request.redirectedFrom()).toBeNull();
  expect(new URLSearchParams(request.postData() ?? '').get('manifest')).toBe(
    '{"name":"zoomies-acme"}',
  );
  await expect(page).toHaveURL('https://github.com/settings/apps/new?state=same-tab-state');
  expect(page.context().pages()).toHaveLength(1);

  await page.route('**/api/v1/installations/manifest/exchange', (route) =>
    route.fulfill({
      json: {
        app_id: 123,
        slug: 'zoomies-acme',
        target: 'acme',
        target_type: 'org',
        install_url:
          'https://github.com/apps/zoomies-acme/installations/new?state=install-state&x=one&x=two',
      },
    }),
  );
  await goto(
    page,
    '/settings/github/setup?code=returned-code&state=same-tab-state',
    'Installations',
  );
  await expect(dialog.getByRole('button', { name: 'Install it on acme' })).toBeVisible();
  await page.reload();
  await page.getByRole('button', { name: 'Connect GitHub' }).first().click();
  const installation = page.waitForRequest(
    'https://github.com/apps/zoomies-acme/installations/new?**',
  );
  await dialog.getByRole('button', { name: 'Install it on acme' }).click();
  const installRequest = await installation;
  expect(installRequest.method()).toBe('GET');
  expect(installRequest.redirectedFrom()).toBeNull();
  const destination = new URL(installRequest.url());
  expect(destination.searchParams.get('state')).toBe('install-state');
  expect(destination.searchParams.getAll('x')).toEqual(['one', 'two']);
  await expect(page).toHaveURL(installRequest.url());
  expect(page.context().pages()).toHaveLength(1);
});

test('a return URL from another browser can be exchanged without local setup storage', async ({
  page,
}) => {
  await page.route('**/api/v1/installations/manifest/exchange', (route) =>
    route.fulfill({
      json: { app_id: 123, slug: 'zoomies-acme', target: 'acme', target_type: 'org' },
    }),
  );
  // Fresh context, no saved handshake. A callback without state exposes the
  // manual exchange; the pasted full URL supplies the state from the other tab.
  await goto(page, '/settings/github/setup?code=unused', 'Installations');
  const dialog = page.getByRole('dialog', { name: 'Connect GitHub' });
  await dialog
    .getByLabel('Code from GitHub')
    .fill(
      'https://zoomies.example.test/settings/github/setup?code=recovered-code&state=recovered-state',
    );
  const exchange = page.waitForRequest('**/api/v1/installations/manifest/exchange');
  await dialog.getByRole('button', { name: 'Exchange the code' }).click();
  expect((await exchange).postDataJSON()).toMatchObject({
    code: 'recovered-code',
    state: 'recovered-state',
  });
  await expect(dialog).toContainText('zoomies-acme exists');
});

/**
 * Verify, driven from the browser, with no fake in the way.
 *
 * The Playwright suite drives the real binary, but its GitHub is an in-process
 * fake the browser cannot reach -- so the connect and verify paths were tested
 * at the API and never through the dialog an operator actually uses. The demo
 * installation closes that gap without any credentials: its probe answers
 * deterministically, with `workflow_job` subscribed and one permission still
 * short of what Zoomies needs, so the dialog exercises the operator-facing
 * verdict rather than a raw GitHub error.
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
  await expect(dialog).toContainText(/credentials work|the App is reachable but is missing/i);
  await expect(dialog).toContainText(/self.hosted runners|organization_self_hosted_runners/i);
  await expect(dialog).toContainText('workflow_job');
  // And which repositories these credentials reach. An App with every
  // permission right, installed on "only select repositories" and not on the
  // one somebody pushes to, is a fleet where nothing queues and no page says
  // why -- so the dialog names the scope and the repositories themselves.
  await expect(dialog).toContainText(/Repositories this installation can see/i);
  await expect(dialog).toContainText(/Every repository in/i);
  await expect(dialog.getByText(/^acme\//).first()).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
});
