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
    page.getByText('Saved repair settings in 2 repositories, watching 2', { exact: true }),
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
  await page.getByText('When Eli acts, and what it may edit', { exact: true }).click();
  await page.locator('#repair-mode').selectOption('manual');
  await page.getByRole('button', { name: 'Retry 1 failed repository' }).click();
  await expect(
    page.getByText('Saved repair settings in one repository, watching 1', { exact: true }),
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

test('configured repositories can be filtered without changing their policies', async ({
  page,
}) => {
  const policy = {
    installation_id: 'inst-1',
    provider_id: 'provider-1',
    automatic: true,
    ask_first: false,
    allow_workflows: false,
  };
  const model = await repositorySetup(page, {
    policies: [
      { ...policy, repo: 'acme/site', enabled: true },
      { ...policy, repo: 'acme/widgets', enabled: false },
    ],
  });
  await page.goto('/repairs/setup');
  await expect(page.getByRole('status').filter({ hasText: 'Showing 2 of 2' })).toContainText(
    '1 enabled, 1 paused.',
  );
  await page.getByRole('searchbox', { name: 'Filter configured repositories' }).fill(' WIDGETS ');
  await expect(page.getByRole('button', { name: 'Resume repairs on acme/widgets' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Pause repairs on acme/site' })).toHaveCount(0);
  await expect(page.getByRole('status').filter({ hasText: 'Showing 1 of 2' })).toBeVisible();
  await page.getByRole('searchbox', { name: 'Filter configured repositories' }).fill('missing');
  await expect(page.getByRole('status').filter({ hasText: 'Showing 0 of 2' })).toBeVisible();
  expect(model.writes).toEqual([]);
});

test('editing stays on one repository and adding repositories starts with safe defaults', async ({
  page,
}) => {
  const policy: Schemas['EliRepairPolicy'] = {
    repo: 'acme/widgets',
    installation_id: 'inst-1',
    provider_id: 'provider-1',
    enabled: false,
    automatic: true,
    ask_first: false,
    allow_workflows: true,
  };
  const model = await repositorySetup(page, { policies: [policy] });
  await page.goto('/repairs/setup');
  await page.getByRole('button', { name: 'Edit repair settings for acme/widgets' }).click();
  await expect(page.getByRole('heading', { name: 'Editing acme/widgets' })).toBeFocused();
  await expect(page.getByRole('group', { name: 'Select repositories', exact: true })).toHaveCount(
    0,
  );
  await page.getByRole('button', { name: 'Save in 1 repository' }).click();
  await expect.poll(() => model.writes.length).toBe(1);
  expect(model.writes[0]).toEqual(policy);
  await page.getByRole('button', { name: 'Add repositories instead' }).click();
  await expect(page.getByRole('heading', { name: 'Add repository repairs' })).toBeFocused();
  await expect(page.getByRole('button', { name: 'Save in 0 repositories' })).toBeDisabled();
  await page
    .getByRole('group', { name: 'Select repositories', exact: true })
    .getByRole('checkbox', { name: 'acme/site', exact: true })
    .check();
  await page.getByRole('button', { name: 'Save in 1 repository' }).click();
  await expect.poll(() => model.writes.length).toBe(2);
  expect(model.writes[1]).toEqual({
    ...policy,
    repo: 'acme/site',
    enabled: true,
    allow_workflows: false,
  });
});

test('editing another repository brings the form back on screen when it already has focus', async ({
  page,
}) => {
  // Safari, and Firefox on macOS, do not focus a button when it is clicked, so
  // after one edit the heading keeps focus while the operator scrolls back to
  // the list. Focusing it again cannot scroll: it already has focus. The form
  // is above the list, so without an explicit scroll a second Edit changes a
  // form nobody can see and reads as a button that does nothing.
  const policy: Schemas['EliRepairPolicy'] = {
    repo: 'acme/site',
    installation_id: 'inst-1',
    provider_id: '',
    enabled: true,
    automatic: false,
    ask_first: true,
    allow_workflows: false,
  };
  const policies = Array.from({ length: 15 }, (_, i) => ({
    ...policy,
    repo: i === 0 ? 'acme/site' : `acme/repo-${i}`,
  }));
  await repositorySetup(page, { policies });
  await page.goto('/repairs/setup');
  await page.getByRole('button', { name: 'Edit repair settings for acme/site' }).click();
  const heading = page.getByRole('heading', { name: 'Editing acme/site' });
  await expect(heading).toBeFocused();

  const second = page.getByRole('button', { name: 'Edit repair settings for acme/repo-14' });
  await second.scrollIntoViewIfNeeded();
  await expect(heading).not.toBeInViewport();
  // A DOM click, which does not move focus, as those browsers' own would not.
  await second.evaluate((el) => (el as HTMLElement).click());
  await expect(page.getByRole('heading', { name: 'Editing acme/repo-14' })).toBeInViewport();
});
