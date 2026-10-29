import { expect, test, type Page } from '@playwright/test';
import type { Schemas } from '../src/lib/api/types';
import { blockedAt, readiness as makeReadiness, stoppedAt } from './support/repairs';

const repo = 'acme/widgets';
const identity: Schemas['EliIdentity'] = {
  user_id: 'user-1',
  github_user_id: 42,
  github_login: 'octocat',
  confirmed: true,
};
const ready: Schemas['EliReadiness'] = makeReadiness({ head_sha: 'reviewed-head' });
const PAGE = `/repairs/new?repo=${encodeURIComponent(repo)}&run=1234`;

async function mockFailure(page: Page, role: 'admin' | 'operator' = 'admin') {
  page.on('pageerror', (error) => {
    throw error;
  });
  // Keep unrelated shell requests quiet; this suite exercises the repair UI's
  // request contracts, including the writes that must never happen on a read.
  await page.route('**/api/v1/**', (route) => route.fulfill({ json: { items: [] } }));
  await page.route('**/api/v1/assistant/repairs/repositories?*', (route) =>
    route.fulfill({
      json: {
        items: [repo],
        total: 1,
        missing_permissions: [],
        capped: false,
      },
    }),
  );
  await page.route('**/api/v1/meta', (route) =>
    route.fulfill({
      json: {
        auth_disabled: false,
        bootstrap_required: false,
        version: 'dev',
      },
    }),
  );
  await page.route('**/api/v1/auth/session', (route) =>
    route.fulfill({
      json: {
        kind: 'session',
        id: 'user-1',
        name: 'Operator',
        role,
      },
    }),
  );
  await page.route('**/api/v1/installations', (route) =>
    route.fulfill({ json: { items: [{ id: 'inst-1', target: 'acme', target_type: 'org' }] } }),
  );
  await page.route('**/api/v1/assistant/repairs/settings', (route) =>
    route.fulfill({
      json: {
        identities: [identity],
        policies: [],
      },
    }),
  );
  const checks: URLSearchParams[] = [];
  const writes: unknown[] = [];
  let enabled = false;
  await page.route('**/api/v1/assistant/repairs/readiness?*', (route) => {
    checks.push(new URL(route.request().url()).searchParams);
    return route.fulfill({
      json: enabled ? ready : blockedAt('policy', 'Enable repository repairs.'),
    });
  });
  await page.route('**/api/v1/assistant/repairs/policy', async (route) => {
    const body = route.request().postDataJSON();
    writes.push(body);
    enabled = true;
    await route.fulfill({ json: body });
  });
  const repairs: unknown[] = [];
  await page.route(/\/api\/v1\/assistant\/repairs$/, async (route) => {
    if (route.request().method() !== 'POST') return route.fulfill({ json: { items: [] } });
    repairs.push(route.request().postDataJSON());
    await route.fulfill({ json: { id: 'repair-1' } });
  });
  return { checks, writes, repairs };
}

test('failure setup enables only its repository, rechecks the run and repairs the reviewed head', async ({
  page,
}) => {
  const model = await mockFailure(page);
  await page.goto(PAGE);
  const setup = page.getByRole('region', { name: 'Set up repairs for this failure' });
  await expect(setup).toBeVisible();
  await expect(setup.getByLabel('GitHub installation')).toHaveValue('inst-1');
  expect(model.writes).toEqual([]);
  expect(model.repairs).toEqual([]);
  await setup.getByRole('button', { name: 'Save and check this failure' }).click();
  await expect(page.getByRole('button', { name: 'Fix with Eli', exact: true })).toBeVisible();
  expect(model.writes).toEqual([
    {
      repo,
      installation_id: 'inst-1',
      provider_id: '',
      enabled: true,
      automatic: false,
      ask_first: false,
      allow_workflows: false,
      daily_limit: 5,
    },
  ]);
  expect(model.checks.length).toBeGreaterThanOrEqual(2);
  for (const check of model.checks) {
    expect(check.get('repo')).toBe(repo);
    expect(check.get('run_id')).toBe('1234');
  }
  expect(model.repairs).toEqual([]);
  await page.getByRole('button', { name: 'Fix with Eli', exact: true }).click();
  await expect(page).toHaveURL(/\/repairs\/repair-1$/);
  expect(model.repairs).toEqual([
    { repo, pull_number: 17, head_sha: 'reviewed-head', instruction: '', run_id: 1234 },
  ]);
});

test('an operator gets an actionable setup explanation without repository writes', async ({
  page,
}) => {
  const model = await mockFailure(page, 'operator');
  await page.goto(PAGE);
  const setup = page.getByRole('region', { name: 'Set up repairs for this failure' });
  await expect(
    setup.getByText(`An administrator needs to enable repairs for ${repo}.`),
  ).toBeVisible();
  await expect(setup.getByRole('button', { name: 'Save and check this failure' })).toBeDisabled();
  expect(model.writes).toEqual([]);
});

test('a run without an eligible PR explains the limit and never offers to repair', async ({
  page,
}) => {
  const model = await mockFailure(page);
  await page.route('**/api/v1/assistant/repairs/readiness?*', (route) =>
    route.fulfill({
      json: blockedAt('pull', 'No eligible PR.', { pull: undefined, pull_number: 0 }),
    }),
  );
  await page.goto(PAGE);
  await expect(
    page.getByText('Eli repairs open PR branches in this repository.', { exact: false }),
  ).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Choose a pull request' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Fix with Eli', exact: true })).toHaveCount(0);
  await expect(page.getByRole('region', { name: 'Set up repairs for this failure' })).toHaveCount(
    0,
  );
  expect(model.repairs).toEqual([]);
});

test('setup asks only for a missing GitHub username, then links and confirms it', async ({
  page,
}) => {
  const model = await mockFailure(page);
  await page.route('**/api/v1/assistant/repairs/settings', (route) =>
    route.fulfill({
      json: {
        identities: [],
        policies: [],
      },
    }),
  );
  const links: unknown[] = [];
  const consents: unknown[] = [];
  await page.route('**/api/v1/assistant/repairs/identity', (route) => {
    links.push(route.request().postDataJSON());
    return route.fulfill({ json: { ...identity, confirmed: false } });
  });
  await page.route('**/api/v1/assistant/repairs/consent', (route) => {
    consents.push(route.request().postDataJSON());
    return route.fulfill({ status: 204 });
  });
  await page.goto(PAGE);
  const setup = page.getByRole('region', { name: 'Set up repairs for this failure' });
  const save = setup.getByRole('button', { name: 'Save and check this failure' });
  await expect(save).toBeDisabled();
  await setup.getByLabel('Your GitHub username').fill('octocat');
  await save.click();
  await expect(page.getByRole('button', { name: 'Fix with Eli', exact: true })).toBeVisible();
  expect(links).toEqual([
    { user_id: 'user-1', github_login: 'octocat', installation_id: 'inst-1', repo },
  ]);
  expect(consents).toEqual([{ github_user_id: 42, enabled: true }]);
  expect(model.repairs).toEqual([]);
});

test('a refused setup keeps the failure context and offers a retry without starting repair', async ({
  page,
}) => {
  const model = await mockFailure(page);
  await page.route('**/api/v1/assistant/repairs/policy', (route) =>
    route.fulfill({
      status: 403,
      json: { code: 'forbidden', message: 'Administrator access is required.' },
    }),
  );
  await page.goto(PAGE);
  const setup = page.getByRole('region', { name: 'Set up repairs for this failure' });
  await setup.getByRole('button', { name: 'Save and check this failure' }).click();
  await expect(setup.getByRole('alert')).toContainText('You do not have permission to do that.');
  await setup.getByRole('button', { name: 'Reload setup' }).click();
  await expect(setup.getByRole('button', { name: 'Save and check this failure' })).toBeEnabled();
  await expect(page).toHaveURL(/repo=acme%2Fwidgets&run=1234/);
  expect(model.repairs).toEqual([]);
});

for (const [code, check, heading, message] of [
  [
    'fixed',
    'checks',
    'Already fixed',
    'The latest checks on this PR have passed. No repair is needed.',
  ],
  [
    'closed',
    'pull',
    'This PR is closed',
    'PR #17 is closed or merged. This failure no longer needs a PR repair.',
  ],
  [
    'superseded',
    'pull',
    'A newer commit replaced this failure',
    'PR #17 has newer commits than this failed run.',
  ],
] as const) {
  test(`a ${code} failure explains the change without sending the user into setup`, async ({
    page,
  }) => {
    const model = await mockFailure(page);
    await page.route('**/api/v1/assistant/repairs/readiness?*', (route) =>
      route.fulfill({ json: blockedAt(check, message, { blockers: [{ code, message }] }) }),
    );
    await page.goto(PAGE);
    const card = page.getByRole('complementary', { name: 'Next step' });
    await expect(card.getByRole('heading', { name: heading })).toBeVisible();
    await expect(card).toContainText(message);
    await expect(
      card.getByRole('link', { name: /View the original run on GitHub/ }),
    ).toHaveAttribute('href', `https://github.com/${repo}/actions/runs/1234`);
    await expect(page.getByRole('button', { name: 'Fix with Eli', exact: true })).toHaveCount(0);
    await expect(page.getByRole('region', { name: 'Set up repairs for this failure' })).toHaveCount(
      0,
    );
    expect(model.repairs).toEqual([]);
  });
}

// A failure reached by its run is checked against the commit that run ran on, so asking
// for it again by run gets the same answer for ever: the newer commit is still newer. The
// way forward is the pull request as it stands now, which is asked for by its number.
test('a superseded failure is checked again by the pull request it belongs to', async ({
  page,
}) => {
  const model = await mockFailure(page);
  const message = 'PR #17 has newer commits than this failed run.';
  const asked: URLSearchParams[] = [];
  await page.route('**/api/v1/assistant/repairs/readiness?*', (route) => {
    const query = new URL(route.request().url()).searchParams;
    asked.push(query);
    return route.fulfill({
      json: query.get('pull_number')
        ? ready
        : blockedAt('pull', message, { blockers: [{ code: 'superseded', message }] }),
    });
  });
  await page.goto(PAGE);
  const card = page.getByRole('complementary', { name: 'Next step' });
  await expect(
    card.getByRole('heading', { name: 'A newer commit replaced this failure' }),
  ).toBeVisible();
  await card.getByRole('button', { name: 'Check the current PR' }).click();

  await expect(page).toHaveURL(/\/repairs\/new\?repo=acme%2Fwidgets&pull=17$/);
  await expect(card.getByRole('button', { name: 'Fix with Eli', exact: true })).toBeVisible();
  expect(asked.map((query) => [query.get('run_id'), query.get('pull_number')])).toEqual([
    ['1234', null],
    [null, '17'],
  ]);
  expect(model.repairs).toEqual([]);
});

test('a failure fixed between review and repair replaces the stale offer with its outcome', async ({
  page,
}) => {
  const model = await mockFailure(page);
  let fixed = false;
  await page.route('**/api/v1/assistant/repairs/readiness?*', (route) =>
    route.fulfill({
      json: fixed
        ? blockedAt('checks', 'The latest checks have passed. No repair is needed.', {
            blockers: [
              { code: 'fixed', message: 'The latest checks have passed. No repair is needed.' },
            ],
          })
        : ready,
    }),
  );
  // Once the checks have passed the controller refuses every repair. Playwright
  // runs only the latest route that matches, so the refused attempt is recorded
  // here: the recorder in mockFailure sits behind this route and never sees it,
  // and an assertion on that recorder alone cannot fail. A request made before
  // the checks pass falls through to it, and would be queued.
  const refused: unknown[] = [];
  await page.route(/\/api\/v1\/assistant\/repairs$/, (route) => {
    if (route.request().method() !== 'POST' || !fixed) return route.fallback();
    refused.push(route.request().postDataJSON());
    return route.fulfill({
      status: 409,
      json: { code: 'conflict', message: 'Checks passed since review. No repair was started.' },
    });
  });
  await page.goto(PAGE);
  const fix = page.getByRole('button', { name: 'Fix with Eli', exact: true });
  await expect(fix).toBeVisible();
  // The checks pass after the repair was reviewed and before it is asked for.
  fixed = true;
  await fix.click();
  await expect(page.getByRole('heading', { name: 'Already fixed' })).toBeVisible();
  await expect(fix).toHaveCount(0);
  // One attempt, for the head that was reviewed and the run it was reached by, and its
  // refusal ended it.
  expect(refused).toEqual([
    { repo, pull_number: 17, head_sha: 'reviewed-head', instruction: '', run_id: 1234 },
  ]);
  expect(model.repairs).toEqual([]);
});

// The controller folds GitHub not answering into the check it was making, so an outage while
// the pull request is looked up arrives as the pull request check, blocked, with the reason
// in `blocker`. Read by the check alone it sends the person to choose another pull request,
// which is not what is wrong; the card says that GitHub did not answer and offers to ask again.
test('a GitHub outage while the pull request is looked up says so and offers to ask again', async ({
  page,
}) => {
  const model = await mockFailure(page);
  const message = 'GitHub could not resolve this repair target. Try checking readiness again.';
  await page.route('**/api/v1/assistant/repairs/readiness?*', (route) =>
    route.fulfill({
      json: makeReadiness({
        ready: false,
        pull: undefined,
        pull_number: 0,
        head_sha: '',
        blockers: [{ code: 'github', message }],
        checks: stoppedAt('pull', message, 'github'),
      }),
    }),
  );
  await page.goto(PAGE);
  const card = page.getByRole('complementary', { name: 'Next step' });
  await expect(card.getByRole('heading', { name: 'GitHub did not answer' })).toBeVisible();
  await expect(card).toContainText(message);
  await expect(card.getByRole('button', { name: 'Check again' })).toBeVisible();
  await expect(card.getByRole('button', { name: 'Choose a pull request' })).toHaveCount(0);
  await expect(card).not.toContainText('Eli repairs open PR branches');
  expect(model.repairs).toEqual([]);
});

test('a GitHub outage offers a status retry and never claims the failure is fixed', async ({
  page,
}) => {
  const model = await mockFailure(page);
  // A controller that does not name the check says only that GitHub did not answer.
  await page.route('**/api/v1/assistant/repairs/readiness?*', (route) =>
    route.fulfill({
      json: makeReadiness({
        ready: false,
        checks: [],
        blockers: [
          {
            code: 'github',
            message:
              'GitHub could not check whether this failure still needs fixing. Try checking readiness again.',
          },
        ],
      }),
    }),
  );
  await page.goto(PAGE);
  const card = page.getByRole('complementary', { name: 'Next step' });
  await expect(card).toContainText(
    'GitHub could not check whether this failure still needs fixing.',
  );
  await expect(card.getByRole('heading', { name: 'GitHub did not answer' })).toBeVisible();
  await expect(card.getByRole('button', { name: 'Check again' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Already fixed' })).toHaveCount(0);
  expect(model.repairs).toEqual([]);
});

for (const ticked of [false, true]) {
  test(`workflow previews are offered during setup and turned on only when ${ticked ? 'ticked' : 'left unticked'}`, async ({
    page,
  }) => {
    await mockFailure(page);
    await page.route('**/api/v1/assistant/repairs/settings', (route) =>
      route.fulfill({ json: { identities: [], policies: [] } }),
    );
    await page.route('**/api/v1/assistant/repairs/identity', (route) =>
      route.fulfill({ json: { ...identity, confirmed: false } }),
    );
    await page.route('**/api/v1/assistant/repairs/consent', (route) =>
      route.fulfill({ status: 204 }),
    );
    const puts: unknown[] = [];
    await page.route('**/api/v1/optimisations/enablement', (route) => {
      if (route.request().method() === 'PUT') puts.push(route.request().postDataJSON());
      return route.fulfill({
        json: {
          source_reads: false,
          workflow_edits: false,
          enabled: false,
          can_change: true,
        },
      });
    });
    await page.goto(PAGE);
    const setup = page.getByRole('region', { name: 'Set up repairs for this failure' });
    const offer = setup.getByLabel('Also let Zoomies preview workflow speed-ups');
    await expect(offer).not.toBeChecked();
    await setup.getByLabel('Your GitHub username').fill('octocat');
    if (ticked) await offer.check();
    await setup.getByRole('button', { name: 'Save and check this failure' }).click();
    await expect(page.getByRole('button', { name: 'Fix with Eli', exact: true })).toBeVisible();
    expect(puts).toEqual(ticked ? [{ enabled: true }] : []);
  });
}
