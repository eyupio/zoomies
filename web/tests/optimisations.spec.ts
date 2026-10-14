import { expect, test, type Page } from '@playwright/test';
import { browserOverride, goto } from './support/fixtures';
test.use(browserOverride);
const inspection = {
  version: 'checkout-web-v1',
  installation_id: 'ins_fixture',
  repository: 'acme/repo',
  id: '',
  created_at: null,
  reviewed_by: null,
  head_sha: 'a'.repeat(40),
  source_sha256: 'c'.repeat(64),
  after_sha256: null,
  binding_basis: 'push_source_unique_literal_names',
  path: '.github/workflows/private.yml',
  mode: '100755',
  content: 'on: push\n# <script>window.bad = true</script>\nfetch-depth: 0\n',
  run_id: '9223372036854775807',
  attempt: 2,
  github_job_id: '9223372036854775807',
  step_number: 42,
  job_index: 0,
  step_index: 0,
  checkout_ms: 4200,
  before: null,
  after: null,
  verification: 'inconclusive',
  currentness: 'not_checked',
  source_funding: 'installation_github_read',
};
const saved = {
  ...inspection,
  id: 'opv_fixture',
  created_at: '2026-10-14T01:00:00Z',
  reviewed_by: 'token_operator',
  after_sha256: 'd'.repeat(64),
  content: inspection.content.replace('depth: 0', 'depth: 1'),
  before: 'fetch-depth: 0\n',
  after: 'fetch-depth: 1\n',
};
async function policy(page: Page, enabled = true) {
  await page.route('**/api/v1/optimisations/policy', (route) =>
    route.fulfill({
      json: { source_enabled: enabled, preview_enabled: enabled, history_enabled: enabled },
    }),
  );
}
async function fill(page: Page) {
  await page.getByLabel('Installation ID', { exact: true }).fill('ins_fixture');
  await page.getByLabel('Repository', { exact: true }).fill('acme/repo');
  await page.getByLabel('Run ID', { exact: true }).fill(inspection.run_id);
  await page.getByLabel('GitHub job ID', { exact: true }).fill(inspection.github_job_id);
  await page.getByLabel('Attempt', { exact: true }).fill('2');
}
test('operator inspects escaped source, declares history needs and saves a bookmarkable inconclusive preview', async ({
  page,
}) => {
  await policy(page);
  let reads = 0,
    writes = 0;
  await page.route('**/api/v1/optimisations/checkout/inspect', (route) => {
    reads++;
    return route.fulfill({ json: inspection });
  });
  await page.route('**/api/v1/optimisations/checkout', (route) => {
    writes++;
    const body = route.request().postDataJSON();
    expect(body.run_id).toBe(inspection.run_id);
    expect(body.github_job_id).toBe(inspection.github_job_id);
    expect(body.reviewed_source_sha256).toBe(inspection.source_sha256);
    expect(body.history_not_required).toBe(true);
    expect(body.reviewed_by).toBeUndefined();
    return route.fulfill({ status: 201, json: saved });
  });
  await goto(page, '/optimisations', 'Optimisation previews');
  await fill(page);
  await page.getByRole('button', { name: 'Inspect checkout', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save checkout preview' })).toBeDisabled();
  await page.getByText('Review inspected workflow source', { exact: true }).click();
  await expect(
    page.getByText('# <script>window.bad = true</script>', { exact: false }),
  ).toBeVisible();
  expect(await page.evaluate(() => 'bad' in window)).toBe(false);
  await page.getByRole('checkbox').check();
  await page.getByRole('button', { name: 'Save checkout preview' }).click();
  await expect(page.getByRole('heading', { name: 'Saved checkout preview' })).toBeVisible();
  await expect(
    page.getByText('Performance verification: Inconclusive.', { exact: false }),
  ).toBeVisible();
  await expect(page).toHaveURL(/id=opv_fixture/);
  expect(reads).toBe(1);
  expect(writes).toBe(1);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
test('bookmarks replay without inspection or capture', async ({ page }) => {
  await policy(page);
  let inspect = 0;
  await page.route('**/api/v1/optimisations/checkout/inspect', (route) => {
    inspect++;
    return route.abort();
  });
  await page.route('**/api/v1/optimisations/opv_fixture?*', (route) =>
    route.fulfill({ json: saved }),
  );
  await goto(
    page,
    '/optimisations?id=opv_fixture&installation_id=ins_fixture&repo=acme%2Frepo',
    'Optimisation previews',
  );
  await expect(page.getByRole('heading', { name: 'Saved checkout preview' })).toBeVisible();
  expect(inspect).toBe(0);
});
test('disabled source controls prevent calls and rate limits require manual retry', async ({
  page,
}) => {
  await policy(page, false);
  let calls = 0;
  await page.route('**/api/v1/optimisations/checkout/inspect', (route) => {
    calls++;
    return route.fulfill({
      status: 429,
      headers: { 'Retry-After': '1' },
      json: { error: { code: 'rate_limited', message: 'Wait for quota.' } },
    });
  });
  await goto(page, '/optimisations', 'Optimisation previews');
  await expect(page.getByRole('button', { name: 'Inspect checkout', exact: true })).toBeDisabled();
  expect(calls).toBe(0);
  await page.unroute('**/api/v1/optimisations/policy');
  await policy(page);
  await page.reload();
  await fill(page);
  await page.getByRole('button', { name: 'Inspect checkout', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Rate-limited' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Inspect checkout', exact: true })).toBeDisabled();
  await page.waitForTimeout(1400);
  expect(calls).toBe(1);
  await expect(page.getByRole('button', { name: 'Inspect checkout', exact: true })).toBeEnabled();
});

test('public visitors make no source or history requests', async ({ page }) => {
  let calls = 0;
  await page.route('**/api/v1/meta', async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    body.auth_disabled = false;
    body.bootstrap_required = false;
    body.public_view = true;
    await route.fulfill({ response, json: body });
  });
  await page.route('**/api/v1/auth/session', async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    body.role = 'viewer';
    body.kind = 'public';
    await route.fulfill({ response, json: body });
  });
  await page.route('**/api/v1/optimisations/**', (route) => {
    calls++;
    return route.abort();
  });
  await goto(
    page,
    '/optimisations?id=opv_fixture&installation_id=ins_fixture&repo=acme%2Frepo',
    'Optimisation previews',
  );
  await expect(
    page.getByText('An identified operator is required', { exact: false }),
  ).toBeVisible();
  expect(calls).toBe(0);
});
