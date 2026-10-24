import { expect, test } from '@playwright/test';
import { goto } from './support/fixtures';

const inspection = {
  installation_id: 'ins_fixture',
  repository: 'acme/repo',
  selection: { run_id: '9007199254740', attempt: 2, github_job_id: '100' },
  head_sha: 'a'.repeat(40),
  source_sha256: 'b'.repeat(64),
  workflow_path: '.github/workflows/ci.yml',
  job_name: 'End to end',
  step_name: 'Browser tests',
  line: 3,
  shards: 4,
  runs: 12,
  test_ms: 360000,
  setup_ms: 42000,
  headroom: 4,
  runner_ms_before: 402000,
  runner_ms_after: 528000,
  wall_ms_before: 402000,
  wall_ms_after: 132000,
  before: '  e2e:\n    runs-on: ubuntu-latest\n',
  after:
    '  e2e-shards:\n    name: "End to end (shard ${{ matrix.shard }}/4)"\n    strategy:\n      fail-fast: false\n      matrix:\n        shard: [1, 2, 3, 4]\n',
  verification: 'inconclusive',
  explanation: 'Split the Browser tests step into 4 Playwright shards.',
  inspected_at: '2026-10-24T09:00:00Z',
};
const retained = {
  id: 'ops_fixture',
  installation_id: 'ins_fixture',
  repository: 'acme/repo',
  reviewed_by: 'usr_operator',
  sha256: 'd'.repeat(64),
  created_at: '2026-10-24T09:01:00Z',
  currentness: 'not_checked',
  selection: inspection.selection,
  head_sha: inspection.head_sha,
  source_sha256: inspection.source_sha256,
  workflow_path: inspection.workflow_path,
  job_name: inspection.job_name,
  step_name: inspection.step_name,
  line: inspection.line,
  shards: inspection.shards,
  runs: inspection.runs,
  test_ms: inspection.test_ms,
  setup_ms: inspection.setup_ms,
  headroom: inspection.headroom,
  runner_ms_before: inspection.runner_ms_before,
  runner_ms_after: inspection.runner_ms_after,
  wall_ms_before: inspection.wall_ms_before,
  wall_ms_after: inspection.wall_ms_after,
  before: inspection.before,
  after: inspection.after,
  verification: 'inconclusive',
  explanation: inspection.explanation,
  approval: null,
  publication: null,
};
const publication = {
  published_by: 'usr_operator',
  published_at: '2026-10-24T09:03:00Z',
  branch: 'zoomies/sharding-ops_fixture',
  base_branch: 'main',
  base_sha: 'a'.repeat(40),
  head_sha: 'a'.repeat(40),
  source_sha256: 'b'.repeat(64),
  preview_sha256: 'd'.repeat(64),
  pull_number: 12,
  pull_url: 'https://github.com/acme/repo/pull/12',
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
  publish: true,
  enabled: true,
  publish_enabled: true,
  can_change: true,
  can_publish: true,
};
const job = {
  id: 'job_1',
  github_job_id: 100,
  github_run_id: 9007199254740,
  repo: 'acme/repo',
  workflow: 'CI',
  job_name: 'End to end',
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

test('pick a job, review the shards and the extra runner time, and keep the preview', async ({
  page,
}) => {
  await mocks(page);
  let inspectBody: Record<string, unknown> | undefined;
  let retainBody: Record<string, unknown> | undefined;
  let approveBody: Record<string, unknown> | undefined;
  let publishBody: Record<string, unknown> | undefined;
  await page.route('**/api/v1/optimisations/sharding/previews/*/publication', async (route) => {
    publishBody = route.request().postDataJSON();
    await route.fulfill({ json: { ...retained, approval, publication } });
  });
  await page.route('**/api/v1/optimisations/sharding/previews/*/approval', async (route) => {
    approveBody = route.request().postDataJSON();
    await route.fulfill({ json: { ...retained, approval } });
  });
  await page.route('**/api/v1/optimisations/sharding/inspections', async (route) => {
    inspectBody = route.request().postDataJSON();
    await route.fulfill({ json: inspection });
  });
  await page.route('**/api/v1/optimisations/sharding/previews', async (route) => {
    retainBody = route.request().postDataJSON();
    await route.fulfill({ status: 201, json: retained });
  });
  await goto(page, '/optimisations/sharding', 'Sharding preview');
  await page.getByRole('combobox', { name: 'Repository' }).selectOption('acme/repo');
  await page
    .getByRole('row', { name: /End to end/ })
    .getByRole('button', { name: 'Review' })
    .click();
  await page.getByRole('button', { name: 'Read this workflow' }).click();
  await expect(page.getByRole('heading', { name: '2. Review the shards' })).toBeVisible();
  // The request names a run and nothing else: no count and no command.
  expect(inspectBody).toEqual({
    installation_id: 'ins_fixture',
    repository: 'acme/repo',
    selection: { run_id: '9007199254740', attempt: 2, github_job_id: '100' },
  });
  await expect(page.getByText('4 shards', { exact: false }).first()).toBeVisible();
  await expect(page.getByText(/rises because every shard repeats the setup/)).toBeVisible();
  await expect(page.getByText(/an upper bound\s+at one moment, not a reservation/)).toBeVisible();
  await expect(page.getByText(/estimate from medians, not a saving/)).toBeVisible();
  const keep = page.getByRole('button', { name: 'Keep a preview for 14 days' });
  await expect(keep).toBeDisabled();
  await page.getByLabel(/I reviewed this job block and the extra runner time/).check();
  await keep.click();
  await expect(page.getByRole('heading', { name: 'Retained sharding preview' })).toBeVisible();
  expect(retainBody).toEqual({
    installation_id: 'ins_fixture',
    repository: 'acme/repo',
    selection: { run_id: '9007199254740', attempt: 2, github_job_id: '100' },
    head_sha: 'a'.repeat(40),
    source_sha256: 'b'.repeat(64),
    shards: 4,
  });
  await expect(page.getByText('Nothing was published.')).toBeVisible();

  const approve = page.getByRole('button', { name: 'Approve preview' });
  await expect(approve).toBeDisabled();
  await page.getByLabel(/I approve this exact sharding preview/).check();
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
  // The request names the reviewed binding and never a shard count.
  expect(publishBody).toEqual({
    installation_id: 'ins_fixture',
    repository: 'acme/repo',
    head_sha: 'a'.repeat(40),
    source_sha256: 'b'.repeat(64),
  });
  await expect(page.getByText(/nothing was merged/i).first()).toBeVisible();
});

test('a kept sharding preview opens from its link', async ({ page }) => {
  await mocks(page);
  await page.route('**/api/v1/optimisations/sharding/previews/*?*', (route) =>
    route.fulfill({ json: retained }),
  );
  await goto(
    page,
    '/optimisations/sharding?id=ops_fixture&installation_id=ins_fixture&repo=acme%2Frepo',
    'Sharding preview',
  );
  await expect(page.getByRole('heading', { name: 'Retained sharding preview' })).toBeVisible();
  await expect(page.getByText('ops_fixture')).toBeVisible();
  await expect(page.getByText(/which together run the whole Playwright suite/)).toBeVisible();
});

test('a kept sharding preview fits a phone', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 700 });
  await mocks(page);
  await page.route('**/api/v1/optimisations/sharding/previews/*?*', (route) =>
    route.fulfill({ json: retained }),
  );
  await goto(
    page,
    '/optimisations/sharding?id=ops_fixture&installation_id=ins_fixture&repo=acme%2Frepo',
    'Sharding preview',
  );
  await expect(page.getByRole('heading', { name: 'Retained sharding preview' })).toBeVisible();
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  );
  expect(overflow).toBe(false);
});

test('a published sharding preview fits a phone', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 700 });
  await mocks(page);
  await page.route('**/api/v1/optimisations/sharding/previews/*?*', (route) =>
    route.fulfill({ json: { ...retained, approval, publication } }),
  );
  await goto(
    page,
    '/optimisations/sharding?id=ops_fixture&installation_id=ins_fixture&repo=acme%2Frepo',
    'Sharding preview',
  );
  await expect(page.getByRole('heading', { name: 'Draft pull request opened' })).toBeVisible();
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  );
  expect(overflow).toBe(false);
});

test('a sharding preview under review fits a phone', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 700 });
  await mocks(page);
  await page.route('**/api/v1/optimisations/sharding/inspections', (route) =>
    route.fulfill({ json: inspection }),
  );
  await goto(page, '/optimisations/sharding', 'Sharding preview');
  await page.getByRole('combobox', { name: 'Repository' }).selectOption('acme/repo');
  await page
    .getByRole('row', { name: /End to end/ })
    .getByRole('button', { name: 'Review' })
    .click();
  await page.getByRole('button', { name: 'Read this workflow' }).click();
  await expect(page.getByRole('heading', { name: '2. Review the shards' })).toBeVisible();
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  );
  expect(overflow).toBe(false);
});
