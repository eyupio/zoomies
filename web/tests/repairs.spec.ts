import { expect, test } from '@playwright/test';
import { goto } from './support/fixtures';

test('repair settings discovers repositories and saves a manual policy without a shared payer', async ({
  page,
}) => {
  await goto(page, '/repairs/setup', 'Repair settings');
  const policies = page
    .locator('details')
    .filter({ has: page.locator('#policy-installation') })
    .last();
  if (!(await page.locator('#policy-installation').isVisible()))
    await policies.locator('summary').first().click();
  await page.locator('#repair-mode').selectOption('manual');
  const repositories = page.getByRole('group', { name: 'Select repositories', exact: true });
  await repositories.getByRole('checkbox', { name: 'acme/site', exact: true }).check();
  await repositories.getByRole('checkbox', { name: 'acme/widgets', exact: true }).check();
  await expect(page.locator('#policy-provider')).toHaveCount(0);
  await page.getByRole('button', { name: 'Save in 2 repositories' }).click();
  await expect(page.getByText('acme/site: saved', { exact: true })).toBeVisible();
  await expect(page.getByText('acme/widgets: saved', { exact: true })).toBeVisible();
  const response = await page.request.get('/api/v1/assistant/repairs/settings');
  const settings = await response.json();
  for (const repo of ['acme/site', 'acme/widgets']) {
    const policy = settings.policies.find((p: { repo: string }) => p.repo === repo);
    expect(policy.provider_id).toBe('');
    expect(policy.automatic).toBe(false);
    expect(policy.ask_first).toBe(false);
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

// Setup is a page of its own, listed ahead of the page that opens one repair by its
// id, or "setup" would be looked up as a repair that does not exist.
test('the setup page holds the account card, the repository policies and the link form', async ({
  page,
}) => {
  await goto(page, '/repairs/setup', 'Repair settings');
  await expect(page.getByRole('heading', { name: 'Your GitHub account' })).toBeVisible();
  await expect(page.getByText('Repairs in these repositories')).toBeVisible();
  await expect(page.getByText('Link a GitHub account', { exact: true })).toBeVisible();
  await expect(page.getByText('This repair is not in your history')).toHaveCount(0);
  // The way back to the hub is the breadcrumb, and the hub's own list is not here.
  await expect(
    page.locator('.page-header').getByRole('link', { name: 'Eli repairs' }),
  ).toHaveAttribute('href', '/repairs');
  await expect(page.getByRole('region', { name: 'History' })).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('the setup page has an in-page navigation for its sections', async ({ page }) => {
  await goto(page, '/repairs/setup', 'Repair settings');
  const nav = page.getByRole('navigation', { name: 'On this page' });
  await expect(nav.getByRole('link', { name: 'Account', exact: true })).toHaveAttribute(
    'href',
    '#access',
  );
  await expect(nav.getByRole('link', { name: 'Repositories', exact: true })).toHaveAttribute(
    'href',
    '#repositories',
  );
});

// Settings keeps what is about the model, and sends repairs to the pages that are about them.
test('Settings, Eli AI Assistant links to the hub and to setup and no longer lists repairs', async ({
  page,
}) => {
  await goto(page, '/settings/assistant', 'Eli AI Assistant');
  const card = page.getByRole('region', { name: 'PR repairs' });
  await expect(card.getByRole('link', { name: 'Open Eli repairs' })).toHaveAttribute(
    'href',
    '/repairs',
  );
  await expect(card.getByRole('link', { name: 'Repair settings' })).toHaveAttribute(
    'href',
    '/repairs/setup',
  );
  await expect(page.getByRole('region', { name: 'History' })).toHaveCount(0);
  await expect(page.getByText('Repairs in these repositories')).toHaveCount(0);
  await expect(page.getByText('Link a GitHub account', { exact: true })).toHaveCount(0);
});

// Arriving from a failure names a repository, and the forms start with it chosen, so
// the person fixes that one and does not have to find it again.
test('a repository named in the address is ready in the policy and link forms', async ({
  page,
}) => {
  await goto(page, '/repairs/setup?repo=acme%2Fsite', 'Repair settings');
  const repositories = page.getByRole('group', { name: 'Select repositories', exact: true });
  await expect(
    repositories.getByRole('checkbox', { name: 'acme/site', exact: true }),
  ).toBeChecked();
  await expect(
    repositories.getByRole('checkbox', { name: 'acme/widgets', exact: true }),
  ).not.toBeChecked();
  await expect(page.getByRole('button', { name: 'Save in 1 repository' })).toBeVisible();
});
