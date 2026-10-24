import { expect, test } from '@playwright/test';
import { goto } from './support/fixtures';

const inspection = {
  installation_id: 'ins_fixture',
  repository: 'acme/repo',
  selection: { run_id: '9007199254740', attempt: 2, github_job_id: '100' },
  head_sha: 'a'.repeat(40),
  workflow_path: '.github/workflows/ci.yml',
  lockfile_path: 'package-lock.json',
  lockfile_sha256: 'c'.repeat(64),
  source_sha256: 'b'.repeat(64),
  package_manager: 'npm',
  job_name: 'Build',
  step_name: 'Node',
  line: 12,
  runs: 4,
  setup_ms: 4200,
  before: '        with:\n          node-version: 20\n',
  after: '        with:\n          cache: npm\n          node-version: 20\n',
  verification: 'inconclusive',
  explanation: 'Enable the setup action npm cache. The saving is unverified.',
  inspected_at: '2026-10-23T09:00:00Z',
};
const retained = {
  id: 'opc_fixture',
  installation_id: 'ins_fixture',
  repository: 'acme/repo',
  reviewed_by: 'usr_operator',
  sha256: 'd'.repeat(64),
  created_at: '2026-10-23T09:01:00Z',
  currentness: 'not_checked',
  head_sha: 'a'.repeat(40),
  workflow_path: '.github/workflows/ci.yml',
  lockfile_path: 'package-lock.json',
  lockfile_sha256: 'c'.repeat(64),
  source_sha256: 'b'.repeat(64),
  after_sha256: 'e'.repeat(64),
  package_manager: 'npm',
  job_name: 'Build',
  step_name: 'Node',
  line: 12,
  runs: 4,
  setup_ms: 4200,
  before: '        with:\n          node-version: 20\n',
  after: '        with:\n          cache: npm\n          node-version: 20\n',
  verification: 'inconclusive',
  explanation: 'Enable the setup action npm cache. The saving is unverified.',
  approval: null,
  publication: null,
};
const approval = {
  approved_by: 'usr_operator',
  approved_at: '2026-10-23T09:02:00Z',
  head_sha: 'a'.repeat(40),
  source_sha256: 'b'.repeat(64),
  preview_sha256: 'd'.repeat(64),
};
const enablement = {
  source_reads: true,
  workflow_edits: true,
  publish: true,
  enabled: true,
  publish_enabled: true,
  can_change: true,
  can_publish: true,
};
const publication = {
  published_by: 'usr_operator',
  published_at: '2026-10-23T09:03:00Z',
  branch: 'zoomies/cache-opc_fixture',
  base_branch: 'main',
  base_sha: 'a'.repeat(40),
  head_sha: 'a'.repeat(40),
  source_sha256: 'b'.repeat(64),
  preview_sha256: 'd'.repeat(64),
  pull_number: 12,
  pull_url: 'https://github.com/acme/repo/pull/12',
};
const job = {
  id: 'job_1',
  github_job_id: 100,
  github_run_id: 9007199254740,
  repo: 'acme/repo',
  workflow: 'CI',
  job_name: 'Build',
  labels: [],
  state: 'completed',
  conclusion: 'success',
  installation_id: 'ins_fixture',
  head_branch: 'main',
  run_attempt: 2,
  run_event: 'push',
  completed_at: '2026-10-23T09:00:00Z',
  matched: true,
  hosted: false,
  provisioning: '',
  provision_now: false,
};
async function previewsOn(page: import('@playwright/test').Page) {
  await page.route('**/api/v1/optimisations/enablement', (route) =>
    route.fulfill({ json: enablement }),
  );
}
async function listRuns(page: import('@playwright/test').Page) {
  await page.route('**/api/v1/jobs/facets', (route) =>
    route.fulfill({ json: { repos: ['acme/repo'], workflows: [], pools: [], branches: [] } }),
  );
  await page.route('**/api/v1/jobs?*', (route) =>
    route.fulfill({
      json: { items: [job, { ...job, id: 'job_2', run_event: 'pull_request' }], total: 2 },
    }),
  );
}

test('pick a run, review the cache line, declare, keep: no IDs typed', async ({ page }) => {
  await previewsOn(page);
  await listRuns(page);
  let inspectBody: { selection: Record<string, unknown> } | undefined;
  let retainBody: Record<string, unknown> | undefined;
  let approveBody: Record<string, unknown> | undefined;
  let publishBody: Record<string, unknown> | undefined;
  await page.route('**/api/v1/optimisations/cache/inspections', async (route) => {
    inspectBody = route.request().postDataJSON();
    await route.fulfill({ json: inspection });
  });
  await page.route('**/api/v1/optimisations/cache/previews', async (route) => {
    retainBody = route.request().postDataJSON();
    await route.fulfill({ status: 201, json: retained });
  });
  await page.route('**/api/v1/optimisations/cache/previews/*/approval', async (route) => {
    approveBody = route.request().postDataJSON();
    await route.fulfill({ json: { ...retained, approval } });
  });
  await page.route('**/api/v1/optimisations/cache/previews/*/publication', async (route) => {
    publishBody = route.request().postDataJSON();
    await route.fulfill({ json: { ...retained, approval, publication } });
  });
  await goto(page, '/optimisations/cache', 'Cache preview');
  await page.getByRole('combobox', { name: 'Repository' }).selectOption('acme/repo');
  await expect(page.getByRole('row', { name: /Build/ })).toHaveCount(1);
  await page.getByRole('row', { name: /Build/ }).getByRole('button', { name: 'Review' }).click();
  await expect(page.getByRole('heading', { name: '2. Review the cache change' })).toBeVisible();
  expect(inspectBody?.selection).toEqual({
    run_id: '9007199254740',
    attempt: 2,
    github_job_id: '100',
  });
  await expect(page.getByText('cache: npm')).toBeVisible();
  await expect(page.getByText('package-lock.json')).toBeVisible();

  const keep = page.getByRole('button', { name: 'Keep a preview for 14 days' });
  await expect(keep).toBeDisabled();
  await page.getByLabel(/I reviewed this setup block/).check();
  await keep.click();
  await expect(page.getByRole('heading', { name: 'Retained cache preview' })).toBeVisible();
  expect(retainBody?.selection).toEqual(inspection.selection);
  expect(retainBody?.head_sha).toBe('a'.repeat(40));
  expect(retainBody?.source_sha256).toBe('b'.repeat(64));
  expect(retainBody?.lockfile_sha256).toBe('c'.repeat(64));
  await expect(page.getByText('Nothing was published.')).toBeVisible();
  await expect(page.getByText(/no measurement shows this change helps/)).toBeVisible();

  const approve = page.getByRole('button', { name: 'Approve preview' });
  await expect(approve).toBeDisabled();
  await page.getByLabel(/I approve this exact cache preview/).check();
  await approve.click();
  await expect(page.getByRole('heading', { name: 'Approved' })).toBeVisible();
  expect(approveBody).toEqual({
    installation_id: 'ins_fixture',
    repository: 'acme/repo',
    head_sha: 'a'.repeat(40),
    source_sha256: 'b'.repeat(64),
  });
  await expect(page.getByText(/did not publish anything/)).toBeVisible();
  await page.getByRole('button', { name: 'Open a draft pull request' }).click();
  await expect(page.getByRole('heading', { name: 'Draft pull request opened' })).toBeVisible();
  await expect(page.getByRole('link', { name: '#12' })).toHaveAttribute(
    'href',
    'https://github.com/acme/repo/pull/12',
  );
  expect(publishBody).toEqual({
    installation_id: 'ins_fixture',
    repository: 'acme/repo',
    head_sha: 'a'.repeat(40),
    source_sha256: 'b'.repeat(64),
  });
});

test('a kept cache preview opens from its link and the page fits a phone', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 700 });
  await previewsOn(page);
  await listRuns(page);
  await page.route('**/api/v1/optimisations/cache/previews/*?*', (route) =>
    route.fulfill({ json: retained }),
  );
  await goto(
    page,
    '/optimisations/cache?id=opc_fixture&installation_id=ins_fixture&repo=acme%2Frepo',
    'Cache preview',
  );
  await expect(page.getByRole('heading', { name: 'Retained cache preview' })).toBeVisible();
  await expect(page.getByText('cache: npm')).toBeVisible();
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  );
  expect(overflow).toBe(false);
});
