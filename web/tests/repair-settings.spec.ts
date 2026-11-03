import { expect, test, type Page } from '@playwright/test';
import type { Schemas } from '../src/lib/api/types';
import { mockRepairApi } from './support/repairs';

async function repositorySetup(
  page: Page,
  options: {
    repos?: string[];
    failPage?: boolean;
    policies?: Schemas['EliRepairPolicy'][];
    noProvider?: boolean;
  } = {},
) {
  const { state } = await mockRepairApi(page);
  state.settings = { identities: [], policies: options.policies ?? [] };
  const repos = options.repos ?? ['acme/site', 'acme/widgets'];
  const discoveries: URLSearchParams[] = [];
  const writes: Schemas['EliRepairPolicy'][] = [];
  const identities: unknown[] = [];
  let failRepo = '';
  await page.route('**/api/v1/installations', (route) =>
    route.fulfill({ json: { items: [{ id: 'inst-1', target: 'acme', target_type: 'org' }] } }),
  );
  await page.route('**/api/v1/users', (route) => route.fulfill({ json: { items: [] } }));
  await page.route('**/api/v1/assistant/providers*', (route) =>
    route.fulfill({
      json: {
        items: options.noProvider
          ? []
          : [
              {
                id: 'provider-1',
                name: 'Team model',
                model: 'team-model',
                owner_id: '',
                enabled: true,
                is_default: true,
              },
            ],
      },
    }),
  );
  await page.route('**/api/v1/assistant/repairs/repositories?*', (route) => {
    const query = new URL(route.request().url()).searchParams;
    discoveries.push(query);
    const offset = Number(query.get('offset') ?? 0);
    if (options.failPage && offset > 0)
      return route.fulfill({
        status: 503,
        json: { error: { code: 'internal', message: 'GitHub did not answer.' } },
      });
    return route.fulfill({
      json: {
        items: repos.slice(offset, offset + 50),
        total: repos.length,
        missing_permissions: [],
        capped: false,
      },
    });
  });
  await page.route('**/api/v1/assistant/repairs/identity', (route) => {
    identities.push(route.request().postDataJSON());
    return route.fulfill({ status: 500, json: {} });
  });
  await page.route('**/api/v1/assistant/repairs/policy', (route) => {
    const body: Schemas['EliRepairPolicy'] = route.request().postDataJSON();
    writes.push(body);
    if (body.repo === failRepo || failRepo === '*')
      return route.fulfill({
        status: 422,
        json: { error: { code: 'unprocessable', message: 'Installation is unavailable.' } },
      });
    state.settings.policies = [
      ...state.settings.policies.filter((p) => p.repo !== body.repo),
      body,
    ];
    return route.fulfill({ json: body });
  });
  return {
    writes,
    discoveries,
    identities,
    fail: (repo: string) => {
      failRepo = repo;
    },
  };
}

test('repository automation uses the saved team provider without personal account setup', async ({
  page,
}) => {
  const model = await repositorySetup(page);
  await page.goto('/repairs/setup');
  await expect(page.getByRole('heading', { name: 'Choose repositories' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Your GitHub account' })).not.toBeVisible();
  await expect(page.getByText('Team model', { exact: true })).toBeVisible();
  await expect(page.locator('#policy-installation')).toHaveCount(0);
  await page.getByRole('button', { name: 'Select all matching repositories' }).click();
  await page.getByRole('button', { name: 'Save in 2 repositories' }).click();
  await expect(
    page.getByText('Saved repair settings in 2 repositories', { exact: true }),
  ).toBeVisible();
  expect(model.identities).toEqual([]);
  expect(model.writes.map((p) => [p.repo, p.provider_id, p.automatic, p.allow_workflows])).toEqual([
    ['acme/site', 'provider-1', true, false],
    ['acme/widgets', 'provider-1', true, false],
  ]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('select all spans discovery pages and does not enable anything before saving', async ({
  page,
}) => {
  const repos = Array.from({ length: 51 }, (_, i) => `acme/repo-${i}`);
  const model = await repositorySetup(page, { repos });
  await page.goto('/repairs/setup');
  await page.getByRole('button', { name: 'Select all matching repositories' }).click();
  await expect(page.getByRole('button', { name: 'Save in 51 repositories' })).toBeVisible();
  expect(model.discoveries.some((query) => query.get('offset') === '50')).toBe(true);
  expect(model.writes).toEqual([]);
});

test('a failed discovery page does not leave a partial select-all selection', async ({ page }) => {
  const repos = Array.from({ length: 51 }, (_, i) => `acme/repo-${i}`);
  await repositorySetup(page, { repos, failPage: true });
  await page.goto('/repairs/setup');
  await page.getByRole('button', { name: 'Select all matching repositories' }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'GitHub did not answer.' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save in 0 repositories' })).toBeDisabled();
});

test('changing the search cancels cross-page selection and saving waits for selection to finish', async ({
  page,
}) => {
  const repos = Array.from({ length: 51 }, (_, i) => `acme/repo-${i}`);
  await repositorySetup(page, { repos });
  let release!: () => void;
  const held = new Promise<void>((resolve) => {
    release = resolve;
  });
  let secondPage = false;
  await page.route('**/api/v1/assistant/repairs/repositories?*', async (route) => {
    if (new URL(route.request().url()).searchParams.get('offset') !== '50') return route.fallback();
    secondPage = true;
    await held;
    await route
      .fulfill({ json: { items: [repos[50]], total: 51, missing_permissions: [], capped: false } })
      .catch(() => {});
  });
  await page.goto('/repairs/setup');
  await page
    .getByRole('group', { name: 'Select repositories', exact: true })
    .getByRole('checkbox', { name: 'acme/repo-0', exact: true })
    .check();
  await page.getByRole('button', { name: 'Select all matching repositories' }).click();
  await expect.poll(() => secondPage).toBe(true);
  await expect(page.getByRole('button', { name: 'Save in 1 repository' })).toBeDisabled();
  await page.locator('#repair-repository-search').fill('repo-0');
  release();
  await expect(page.getByRole('button', { name: 'Save in 1 repository' })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Save in 51 repositories' })).toHaveCount(0);
});

test('partial saves report failure and retry only failed repositories with the reviewed settings', async ({
  page,
}) => {
  const model = await repositorySetup(page);
  model.fail('acme/widgets');
  await page.goto('/repairs/setup');
  await page.getByRole('button', { name: 'Select all matching repositories' }).click();
  await page.getByRole('button', { name: 'Save in 2 repositories' }).click();
  await expect(page.getByText('Saved 1 of 2 repositories', { exact: true })).toBeVisible();
  await expect(
    page.getByText('acme/widgets: Installation is unavailable.', { exact: true }),
  ).toBeVisible();
  model.fail('');
  await page.locator('#repair-mode').selectOption('manual');
  await page.getByRole('button', { name: 'Retry 1 failed repository' }).click();
  await expect(
    page.getByText('Saved repair settings in 1 repository', { exact: true }),
  ).toBeVisible();
  expect(model.writes.map((p) => p.repo)).toEqual(['acme/site', 'acme/widgets', 'acme/widgets']);
  expect(model.writes[2]?.automatic).toBe(true);
  expect(model.writes[2]?.provider_id).toBe('provider-1');
});

test('all failed saves never show a success toast', async ({ page }) => {
  const model = await repositorySetup(page);
  model.fail('*');
  await page.goto('/repairs/setup');
  await page.getByRole('button', { name: 'Select all matching repositories' }).click();
  await page.getByRole('button', { name: 'Save in 2 repositories' }).click();
  await expect(page.getByText('No repository settings saved', { exact: true })).toBeVisible();
  await expect(page.getByText('Repository policies processed', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Retry 2 failed repositories' })).toBeVisible();
});

test('a broken automatic policy can be paused without choosing a replacement provider', async ({
  page,
}) => {
  const model = await repositorySetup(page, {
    noProvider: true,
    policies: [
      {
        repo: 'acme/widgets',
        installation_id: 'inst-1',
        provider_id: '',
        enabled: true,
        automatic: true,
        ask_first: false,
        allow_workflows: false,
      },
    ],
  });
  await page.goto('/repairs/setup');
  await page.getByRole('button', { name: 'Pause repairs on acme/widgets' }).click();
  await expect(page.getByRole('button', { name: 'Resume repairs on acme/widgets' })).toBeVisible();
  expect(model.writes).toHaveLength(1);
  expect(model.writes[0]?.enabled).toBe(false);
  expect(model.writes[0]?.automatic).toBe(true);
  expect(model.writes[0]?.provider_id).toBe('');
});
