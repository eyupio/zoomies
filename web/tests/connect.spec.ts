/**
 * Connecting a GitHub App, from the browser, against a GitHub the browser can
 * reach.
 *
 * The suite drives the real binary, but its fake GitHub is an in-process test
 * server -- so the connect and verify pages, which are the first two an
 * operator meets after creating an account, were exercised at every layer
 * except the one they live on. This project runs that same fake as a program on
 * a loopback port beside an empty controller, so the whole path is real: the
 * form, the seal, the probe, the verdict.
 *
 * It is also the suite's only fail-then-recover journey outside a wrong
 * password. Setting a fleet up is mostly a sequence of things that do not work
 * yet, and a page is only trustworthy if it says which one.
 */
import { readFileSync } from 'node:fs';

import { expect, test } from '@playwright/test';
import { browserOverride, goto } from './support/fixtures';

test.use(browserOverride);
test.describe.configure({ mode: 'serial' });

interface Fake {
  url: string;
  appId: string;
  installationId: string;
  privateKey: string;
}

/** What the fixture left behind when it started the fake. */
function fake(): Fake {
  return JSON.parse(readFileSync('test-results/fakegithub.json', 'utf8')) as Fake;
}

/** Fill the "use an App you already have" form and submit it. */
async function connectWith(page: import('@playwright/test').Page, key: string): Promise<void> {
  const details = fake();
  await page.getByRole('button', { name: 'Connect GitHub' }).first().click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await dialog.getByRole('tab', { name: 'Use an App you already have' }).click();

  await dialog.getByLabel('Organisation login').fill('acme');
  await dialog.getByLabel('App ID').fill(details.appId);
  await dialog.getByLabel('Installation ID').fill(details.installationId);
  await dialog.getByLabel('API base URL').fill(details.url);
  await dialog.getByLabel('Private key').fill(key);
  await dialog.getByRole('button', { name: 'Connect', exact: true }).click();
}

test('an App connected with an unusable key says so, and works once it is fixed', async ({
  page,
}) => {
  await goto(page, '/installations', 'Installations');

  // A key that is not a key: the commonest paste error there is, because the
  // .pem file GitHub hands over is downloaded once and easily confused with
  // the App's public identifiers.
  await connectWith(
    page,
    '-----BEGIN RSA PRIVATE KEY-----\nnot actually a key\n-----END RSA PRIVATE KEY-----\n',
  );

  const dialog = page.getByRole('dialog');
  // The dialog does not end at a recorded row: it ends when the operator knows
  // whether the credentials work.
  await expect(dialog).toContainText(/something is missing|did not work|private key/i, {
    timeout: 15_000,
  });
  // The footer's Close, not the dialog's corner icon: both are named Close.
  await dialog.getByRole('button', { name: 'Close', exact: true }).last().click();

  // And the page agrees: an installation that cannot authenticate is not shown
  // as healthy just because the row exists.
  const card = page.getByRole('article').filter({ hasText: 'acme' }).first();
  await expect(card).toBeVisible();
  await card.getByRole('button', { name: /Verify/ }).click();
  const verify = page.getByRole('dialog');
  await expect(verify).toContainText(/private key|could not|not a PEM/i, { timeout: 15_000 });
  await page.keyboard.press('Escape');

  // Now the real key, without taking the fleet apart to do it. Until this,
  // replacing a key meant disconnecting the installation -- which takes its
  // pools and their runner rows with it -- because the route existed and
  // nothing surfaced it.
  await card.getByRole('button', { name: 'Replace key' }).click();
  const replace = page.getByRole('dialog');
  await expect(replace).toBeVisible();
  await replace.getByLabel('Private key').fill(fake().privateKey);
  await replace.getByRole('button', { name: 'Replace the key' }).click();

  // Replacing it verifies straight away: "it is stored" is not the answer
  // somebody replacing a broken credential came for.
  const ok = page.getByRole('dialog');
  await expect(ok).toContainText(/credentials work/i, { timeout: 15_000 });
  await expect(ok).toContainText(/Repositories this installation can see/i);
  await expect(ok).toContainText('acme/widgets');
});
