import { expect, test } from '@playwright/test';
import { goto } from './support/fixtures';

const inspection = {
  installation_id: 'ins_fixture',
  repository: 'acme/repo',
  selection: { run_id: '9007199254740', attempt: 2, github_job_id: '100' },
  condition: 'curl -fsS http://127.0.0.1:8080/health',
  head_sha: 'a'.repeat(40),
  source_sha256: 'b'.repeat(64),
  workflow_path: '.github/workflows/ci.yml',
  job_name: 'Build',
  step_name: 'Wait for the API',
  line: 12,
  wait_seconds: 4,
  runs: 4,
  wait_ms: 4200,
  before: '        run: sleep 4\n',
  after:
    "        run: |\n          timeout 4 sh -c 'until curl -fsS http://127.0.0.1:8080/health; do sleep 1; done'\n",
  verification: 'inconclusive',
  explanation: 'Replace a fixed 4s sleep with a bounded poll of the operator command.',
  inspected_at: '2026-10-24T09:00:00Z',
};
const retained = {
  id: 'opr_fixture',
  installation_id: 'ins_fixture',
  repository: 'acme/repo',
  reviewed_by: 'usr_operator',
  sha256: 'd'.repeat(64),
  created_at: '2026-10-24T09:01:00Z',
  currentness: 'not_checked',
  selection: { run_id: '9007199254740', attempt: 2, github_job_id: '100' },
  condition: inspection.condition,
  head_sha: 'a'.repeat(40),
  source_sha256: 'b'.repeat(64),
  workflow_path: '.github/workflows/ci.yml',
  job_name: 'Build',
  step_name: 'Wait for the API',
  line: 12,
  wait_seconds: 4,
  runs: 4,
  wait_ms: 4200,
  before: '        run: sleep 4\n',
  after:
    "        run: |\n          timeout 4 sh -c 'until curl -fsS http://127.0.0.1:8080/health; do sleep 1; done'\n",
  verification: 'inconclusive',
  explanation: 'Replace a fixed 4s sleep with a bounded poll of the operator command.',
  approval: null,
};
const approval = {
  approved_by: 'usr_operator',
  approved_at: '2026-10-24T09:02:00Z',
  head_sha: 'a'.repeat(40),
  source_sha256: 'b'.repeat(64),
  preview_sha256: 'd'.repeat(64),
};
const enablement = {
  source_reads: true,
  workflow_edits: true,
  publish: false,
  enabled: true,
  publish_enabled: false,
  can_change: true,
  can_publish: true,
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
  completed_at: '2026-10-24T09:00:00Z',
  matched: true,
  hosted: false,
  provisioning: '',
  provision_now: false,
};
async function mocks(page: import('@playwright/test').Page) {
  await page.route('**/api/v1/optimisations/enablement', (route) =>
    route.fulfill({ json: enablement }),
  );
  await page.route('**/api/v1/jobs/facets', (route) =>
    route.fulfill({ json: { repos: ['acme/repo'], workflows: [], pools: [], branches: [] } }),
  );
  await page.route('**/api/v1/jobs?*', (route) =>
    route.fulfill({ json: { items: [job], total: 1 } }),
  );
}

test('pick a job, supply the readiness command and review the bounded wait', async ({ page }) => {
  await mocks(page);
  let body: Record<string, unknown> | undefined;
  let retainBody: Record<string, unknown> | undefined;
  let approveBody: Record<string, unknown> | undefined;
  await page.route('**/api/v1/optimisations/readiness/previews/*/approval', async (route) => {
    approveBody = route.request().postDataJSON();
    await route.fulfill({ json: { ...retained, approval } });
  });
  await page.route('**/api/v1/optimisations/readiness/inspections', async (route) => {
    body = route.request().postDataJSON();
    await route.fulfill({ json: inspection });
  });
  await page.route('**/api/v1/optimisations/readiness/previews', async (route) => {
    retainBody = route.request().postDataJSON();
    await route.fulfill({ status: 201, json: retained });
  });
  await goto(page, '/optimisations/readiness', 'Readiness preview');
  await page.getByRole('combobox', { name: 'Repository' }).selectOption('acme/repo');
  await page.getByRole('row', { name: /Build/ }).getByRole('button', { name: 'Review' }).click();
  await page.getByLabel('Readiness command').fill(inspection.condition);
  await page.getByRole('button', { name: 'Read this workflow' }).click();
  await expect(page.getByRole('heading', { name: '2. Review the bounded wait' })).toBeVisible();
  expect(body).toEqual({
    installation_id: 'ins_fixture',
    repository: 'acme/repo',
    selection: { run_id: '9007199254740', attempt: 2, github_job_id: '100' },
    condition: inspection.condition,
  });
  await expect(page.getByText('timeout 4 sh -c')).toBeVisible();
  await expect(page.getByText('The old fixed wait was')).toBeVisible();
  const keep = page.getByRole('button', { name: 'Keep a preview for 14 days' });
  await expect(keep).toBeDisabled();
  await page.getByLabel(/I reviewed this run block/).check();
  await keep.click();
  await expect(page.getByRole('heading', { name: 'Retained readiness preview' })).toBeVisible();
  expect(retainBody).toEqual({
    installation_id: 'ins_fixture',
    repository: 'acme/repo',
    selection: { run_id: '9007199254740', attempt: 2, github_job_id: '100' },
    condition: inspection.condition,
    head_sha: 'a'.repeat(40),
    source_sha256: 'b'.repeat(64),
  });
  await expect(page.getByText('Nothing was published.')).toBeVisible();

  const approve = page.getByRole('button', { name: 'Approve preview' });
  await expect(approve).toBeDisabled();
  await page.getByLabel(/I approve this exact readiness preview/).check();
  await approve.click();
  await expect(page.getByRole('heading', { name: 'Approved' })).toBeVisible();
  expect(approveBody).toEqual({
    installation_id: 'ins_fixture',
    repository: 'acme/repo',
    head_sha: 'a'.repeat(40),
    source_sha256: 'b'.repeat(64),
  });
  await expect(page.getByText(/did not publish anything/)).toBeVisible();
});

test('a kept readiness preview opens from its link', async ({ page }) => {
  await mocks(page);
  await page.route('**/api/v1/optimisations/readiness/previews/*?*', (route) =>
    route.fulfill({ json: retained }),
  );
  await goto(
    page,
    '/optimisations/readiness?id=opr_fixture&installation_id=ins_fixture&repo=acme%2Frepo',
    'Readiness preview',
  );
  await expect(page.getByRole('heading', { name: 'Retained readiness preview' })).toBeVisible();
  await expect(page.getByText('opr_fixture')).toBeVisible();
  await expect(page.getByText('timeout 4 sh -c')).toBeVisible();
});

test('an approved readiness preview fits a phone', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 700 });
  await mocks(page);
  await page.route('**/api/v1/optimisations/readiness/previews/*?*', (route) =>
    route.fulfill({ json: { ...retained, approval } }),
  );
  await goto(
    page,
    '/optimisations/readiness?id=opr_fixture&installation_id=ins_fixture&repo=acme%2Frepo',
    'Readiness preview',
  );
  await expect(page.getByRole('heading', { name: 'Approved' })).toBeVisible();
  const approvedOverflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  );
  expect(approvedOverflow).toBe(false);
});

test('a readiness preview fits a phone', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 700 });
  await mocks(page);
  await page.route('**/api/v1/optimisations/readiness/inspections', (route) =>
    route.fulfill({ json: inspection }),
  );
  await goto(page, '/optimisations/readiness', 'Readiness preview');
  await page.getByRole('combobox', { name: 'Repository' }).selectOption('acme/repo');
  await page.getByRole('row', { name: /Build/ }).getByRole('button', { name: 'Review' }).click();
  await page.getByLabel('Readiness command').fill(inspection.condition);
  await page.getByRole('button', { name: 'Read this workflow' }).click();
  await expect(page.getByRole('heading', { name: '2. Review the bounded wait' })).toBeVisible();
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  );
  expect(overflow).toBe(false);
});
