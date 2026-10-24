import { expect, test } from '@playwright/test';
import { goto } from './support/fixtures';

const inspection = {
  installation_id: 'ins_fixture',
  repository: 'acme/repo',
  selection: { run_id: '9007199254740', attempt: 2, github_job_id: '100' },
  condition: 'curl -fsS http://127.0.0.1:8080/health',
  head_sha: 'a'.repeat(40),
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
  await page.route('**/api/v1/optimisations/readiness/inspections', async (route) => {
    body = route.request().postDataJSON();
    await route.fulfill({ json: inspection });
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
  await expect(page.getByText(/Nothing was retained, approved or published/)).toBeVisible();
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
