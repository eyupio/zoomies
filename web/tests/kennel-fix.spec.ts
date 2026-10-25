import { expect, test } from '@playwright/test';
import { documentWidth, goto } from './support/fixtures';

const blob = 'c'.repeat(40);
const head = 'a'.repeat(40);
const source = 'b'.repeat(64);
const before = '  build:\n    name: Build\n    runs-on: ubuntu-latest\n';
const after = before + '    timeout-minutes: 30\n';
const inspection = {
  kennel_repository_id: 'kcr_fixture',
  installation_id: 'ins_fixture',
  repository: 'acme/repo',
  check_code: 'ci.no_timeout',
  blob_sha: blob,
  job_index: 0,
  job_name: 'Build',
  minutes: 30,
  head_sha: head,
  branch: 'main',
  source_sha256: source,
  workflow_path: '.github/workflows/ci.yml',
  line: 5,
  before,
  after,
  verification: 'recheck',
  explanation: 'Give Build a timeout of 30 minutes.',
  inspected_at: '2026-10-25T09:00:00Z',
};
const retained = {
  id: 'kfp_fixture',
  installation_id: 'ins_fixture',
  repository: 'acme/repo',
  kennel_repository_id: 'kcr_fixture',
  reviewed_by: 'usr_operator',
  sha256: 'd'.repeat(64),
  created_at: '2026-10-25T09:01:00Z',
  currentness: 'not_checked',
  check_code: 'ci.no_timeout',
  blob_sha: blob,
  job_index: 0,
  job_name: 'Build',
  minutes: 30,
  head_sha: head,
  branch: 'main',
  source_sha256: source,
  workflow_path: '.github/workflows/ci.yml',
  line: 5,
  before,
  after,
  verification: 'recheck',
  explanation: 'Give Build a timeout of 30 minutes.',
  approval: null,
  publication: null,
};
const approval = {
  approved_by: 'usr_operator',
  approved_at: '2026-10-25T09:02:00Z',
  head_sha: head,
  source_sha256: source,
  preview_sha256: 'd'.repeat(64),
};
const publication = {
  published_by: 'usr_operator',
  published_at: '2026-10-25T09:03:00Z',
  branch: 'zoomies/kennel-fix-kfp_fixture',
  base_branch: 'main',
  base_sha: head,
  head_sha: head,
  source_sha256: source,
  preview_sha256: 'd'.repeat(64),
  pull_number: 31,
  pull_url: 'https://github.com/acme/repo/pull/31',
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
const address = `/kennel/fixes/timeout?repository=kcr_fixture&blob=${blob}&job=0`;

async function mocks(page: import('@playwright/test').Page) {
  await page.route('**/api/v1/optimisations/enablement', (route) =>
    route.fulfill({ json: enablement }),
  );
}

test('choose the minutes, review the change, keep it, approve it and publish a draft', async ({
  page,
}) => {
  await mocks(page);
  let inspectBody: Record<string, unknown> | undefined;
  let retainBody: Record<string, unknown> | undefined;
  let approveBody: Record<string, unknown> | undefined;
  let publishBody: Record<string, unknown> | undefined;
  await page.route('**/api/v1/kennel/fixes/timeout/previews/*/publication', async (route) => {
    publishBody = route.request().postDataJSON();
    await route.fulfill({ json: { ...retained, approval, publication } });
  });
  await page.route('**/api/v1/kennel/fixes/timeout/previews/*/approval', async (route) => {
    approveBody = route.request().postDataJSON();
    await route.fulfill({ json: { ...retained, approval } });
  });
  await page.route('**/api/v1/kennel/fixes/timeout/inspections', async (route) => {
    inspectBody = route.request().postDataJSON();
    await route.fulfill({ json: inspection });
  });
  await page.route('**/api/v1/kennel/fixes/timeout/previews', async (route) => {
    retainBody = route.request().postDataJSON();
    await route.fulfill({ status: 201, json: retained });
  });

  await goto(page, address, 'Timeout fix');
  await page.getByLabel('Minutes the job may run').fill('30');
  await page.getByRole('button', { name: 'Read this workflow' }).click();
  await expect(page.getByRole('heading', { name: '2. Review the change' })).toBeVisible();
  expect(inspectBody).toEqual({
    kennel_repository_id: 'kcr_fixture',
    blob_sha: blob,
    job_index: 0,
    minutes: 30,
  });
  // The change is drawn as a change: the lines that stay, and the one that is added.
  const diff = page.getByRole('group', { name: 'The change to this job, untrusted text' });
  await expect(diff.getByText('Added: ')).toHaveCount(1);
  await expect(diff).toContainText('timeout-minutes: 30');
  await expect(diff).toContainText('runs-on: ubuntu-latest');
  await expect(diff.getByText('Unchanged: ')).toHaveCount(3);

  const keep = page.getByRole('button', { name: 'Keep a fix for 14 days' });
  await expect(keep).toBeDisabled();
  await page.getByLabel(/I reviewed this change and 30 minutes is enough/).check();
  await keep.click();
  await expect(page.getByRole('heading', { name: 'Kept Kennel fix' })).toBeVisible();
  expect(retainBody).toEqual({
    kennel_repository_id: 'kcr_fixture',
    blob_sha: blob,
    job_index: 0,
    minutes: 30,
    head_sha: head,
    source_sha256: source,
  });
  await expect(page).toHaveURL(/id=kfp_fixture/);

  const approve = page.getByRole('button', { name: 'Approve fix' });
  await expect(approve).toBeDisabled();
  await page.getByLabel(/I approve this exact fix, including the timeout of 30 minutes/).check();
  await approve.click();
  await expect(page.getByRole('heading', { name: 'Approved' })).toBeVisible();
  expect(approveBody).toEqual({
    installation_id: 'ins_fixture',
    repository: 'acme/repo',
    head_sha: head,
    source_sha256: source,
  });

  await page.getByRole('button', { name: 'Open a draft pull request' }).click();
  await expect(page.getByRole('heading', { name: 'Draft pull request opened' })).toBeVisible();
  expect(publishBody).toEqual(approveBody);
  await expect(page.getByRole('link', { name: '#31' })).toHaveAttribute(
    'href',
    'https://github.com/acme/repo/pull/31',
  );
  // Opening a pull request does not close the finding, and the page says who does.
  await expect(page.getByText('the finding is not closed yet')).toBeVisible();
});

test('minutes that are not a whole number of minutes are said, not rounded', async ({ page }) => {
  await mocks(page);
  let inspected = false;
  await page.route('**/api/v1/kennel/fixes/timeout/inspections', async (route) => {
    inspected = true;
    await route.fulfill({ json: inspection });
  });
  await goto(page, address, 'Timeout fix');
  const read = page.getByRole('button', { name: 'Read this workflow' });
  const field = page.getByLabel('Minutes the job may run');
  await expect(read).toBeDisabled();
  for (const bad of ['1.5', '30m', '0', '1441', '-3']) {
    await field.fill(bad);
    await expect(read, bad).toBeDisabled();
    // Said twice on purpose: as the field's hint and as the disabled button's reason.
    await expect(
      page.getByText('Use a whole number of minutes from 1 to 1440.').first(),
    ).toBeVisible();
  }
  await field.fill('45');
  await expect(read).toBeEnabled();
  expect(inspected, 'nothing was read for a value that was refused').toBe(false);
});

test('the reading stays off while the workflow switches are off', async ({ page }) => {
  await page.route('**/api/v1/optimisations/enablement', (route) =>
    route.fulfill({
      json: { ...enablement, source_reads: false, enabled: false, publish_enabled: false },
    }),
  );
  await goto(page, address, 'Timeout fix');
  await page.getByLabel('Minutes the job may run').fill('30');
  await expect(page.getByRole('button', { name: 'Read this workflow' })).toBeDisabled();
});

test('a kept fix opens from its address', async ({ page }) => {
  await mocks(page);
  await page.route('**/api/v1/kennel/fixes/timeout/previews/kfp_fixture?*', (route) =>
    route.fulfill({ json: { ...retained, approval, publication } }),
  );
  await goto(
    page,
    '/kennel/fixes/timeout?id=kfp_fixture&installation_id=ins_fixture&repo=acme/repo',
    'Timeout fix',
  );
  await expect(page.getByRole('heading', { name: 'Draft pull request opened' })).toBeVisible();
  await expect(
    page.getByText('Nothing was merged, and the finding is not closed yet.'),
  ).toBeVisible();
});

test('the page fits a phone with the change and the kept fix on it, and every control has a name', async ({
  page,
  isMobile,
}) => {
  test.skip(!isMobile, 'the phone project checks the narrow widths');
  await mocks(page);
  await page.route('**/api/v1/kennel/fixes/timeout/inspections', (route) =>
    route.fulfill({ json: inspection }),
  );
  await page.route('**/api/v1/kennel/fixes/timeout/previews', (route) =>
    route.fulfill({ status: 201, json: { ...retained, approval, publication } }),
  );
  for (const width of [360, 412]) {
    await page.setViewportSize({ width, height: 780 });
    await goto(page, address, 'Timeout fix');
    await page.getByLabel('Minutes the job may run').fill('30');
    await page.getByRole('button', { name: 'Read this workflow' }).click();
    await expect(page.getByRole('heading', { name: '2. Review the change' })).toBeVisible();
    for (const where of ['the change under review']) {
      const { scrollWidth, clientWidth } = await documentWidth(page);
      expect(scrollWidth, `${where} at ${width}px scrolls sideways`).toBeLessThanOrEqual(
        clientWidth,
      );
    }
    await page.getByLabel(/I reviewed this change/).check();
    await page.getByRole('button', { name: 'Keep a fix for 14 days' }).click();
    await expect(page.getByRole('heading', { name: 'Draft pull request opened' })).toBeVisible();
    const { scrollWidth, clientWidth } = await documentWidth(page);
    expect(scrollWidth, `the kept fix at ${width}px scrolls sideways`).toBeLessThanOrEqual(
      clientWidth,
    );
    await expect(page.getByRole('button', { name: /^$/ })).toHaveCount(0);
    await expect(page.getByRole('link', { name: /^$/ })).toHaveCount(0);
  }
});
