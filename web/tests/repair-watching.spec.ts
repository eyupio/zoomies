/**
 * Watching on the Eli repairs pages: the repositories Eli follows, the pull
 * requests it is on, what it last saw, and the three controls that stop it.
 *
 * Every test says what the controller answered and checks what the page did with
 * it, because the section has one rule above all others: it never says all clear
 * about something it could not read. Each runs on a desktop and on a phone, since
 * pausing a repository from a phone is the case that matters when something is
 * going wrong away from a desk.
 */
import { expect, test, type Page } from '@playwright/test';
import {
  automaticPolicy,
  linkedAs,
  mockRepairApi,
  repairRow,
  watchRepositoryRow,
  watchRow,
} from './support/repairs';

// The section is supporting detail while work is in front of it; these tests open it.
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('zoomies.repairs.watching.open', 'true');
    localStorage.setItem('zoomies.repairs.automatic.open', 'true');
  });
});

const BEHAVIOUR = 'Baby sit PRs · checks and merge conflicts';

test.describe('the hub', () => {
  test('says nothing about watching where nothing is watched', async ({ page }) => {
    const { calls } = await mockRepairApi(page, {
      settings: { ...linkedAs('dana'), policies: [automaticPolicy('acme/widgets')] },
      automatic: [],
    });
    await page.goto('/repairs');
    await expect(page.getByRole('heading', { level: 1, name: 'Eli repairs' })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Watching' })).toHaveCount(0);
    // The read happened: the section is absent because there is nothing, not
    // because nobody asked.
    expect(calls.watchReads).toBeGreaterThan(0);
    expect(calls.watchRepositoryReads).toBeGreaterThan(0);
  });

  test('does not say all clear when the watches could not be read', async ({ page }) => {
    await mockRepairApi(page, {
      settings: { ...linkedAs('dana'), policies: [automaticPolicy('acme/widgets')] },
      automatic: [],
      watchRepositories: [watchRepositoryRow()],
    });
    // Registered after the stub's catch-all, so it is the one that answers. The
    // glob carries the query string, because the page pages the list.
    await page.route(/\/api\/v1\/assistant\/repairs\/watches(\?.*)?$/, (route) =>
      route.fulfill({
        status: 500,
        json: { error: { code: 'internal', message: 'the database is busy' } },
      }),
    );
    await page.goto('/repairs');
    const section = page.getByRole('region', { name: 'Watching' });
    await expect(section).toBeVisible();
    await expect(section).toContainText(
      'Nothing here says whether a pull request is being followed',
    );
    await expect(section).not.toContainText('Nothing is being watched');
  });

  test('lists a repository with its behaviour, count, activity and what needs a person', async ({
    page,
  }) => {
    await mockRepairApi(page, {
      settings: {
        ...linkedAs('dana'),
        policies: [automaticPolicy('acme/widgets'), automaticPolicy('acme/site')],
      },
      automatic: [],
      watchRepositories: [
        watchRepositoryRow(),
        watchRepositoryRow({ id: 'd2', repo: 'acme/site' }),
      ],
      watches: [
        watchRow(),
        watchRow({
          id: 'w2',
          pull_number: 18,
          check_state: 'passed',
          check_evidence: JSON.stringify({ state: 'passed', total: 1, checks: [] }),
        }),
        watchRow({
          id: 'w3',
          repo: 'acme/site',
          pull_number: 21,
          check_state: 'failed',
          chain_attempts: 3,
          chain_stopped_reason: 'Eli has made three repair attempts here.',
        }),
      ],
    });
    await page.goto('/repairs');
    const section = page.getByRole('region', { name: 'Watching' });
    await expect(section).toBeVisible();
    const widgets = section.getByRole('article', { name: 'Watched repository acme/widgets' });
    await expect(widgets.getByRole('heading', { name: 'acme/widgets' })).toBeVisible();
    await expect(widgets).toContainText(BEHAVIOUR);
    await expect(widgets).toContainText('Watched pull requests');
    await expect(widgets).toContainText('2 pull requests');
    await expect(widgets.getByText('Last successful observation')).toBeVisible();
    // One of the two is being repaired, which is what the row says is happening.
    await expect(widgets).toContainText('Repairing 1 pull request');
    await expect(widgets).toContainText('Nothing');

    // What needs a person is first, and says why.
    const site = section.getByRole('article', { name: 'Watched repository acme/site' });
    await expect(site).toContainText('1 pull request needs a person');
    await expect(site).toContainText('Eli has made three repair attempts here.');
    await expect(section.getByRole('article').first()).toHaveAccessibleName(
      'Watched repository acme/site',
    );
  });
});

test.describe('one pull request', () => {
  test('shows its intention, evidence, first failure and what changed', async ({ page }) => {
    const done = repairRow({
      id: 'rep-watch',
      repo: 'acme/widgets',
      pull_number: 17,
      trigger: 'automatic',
      state: 'checks_failed',
      commit_sha: 'fe1d2c3b4a5968778695a4b3c2d1e0f9a8b7c6d5',
      details: {
        summary: 'Retried the registry call and raised its timeout.',
        changed_files: ['internal/registry/client.go', 'internal/registry/client_test.go'],
      },
    });
    await mockRepairApi(page, {
      settings: { ...linkedAs('dana'), policies: [automaticPolicy('acme/widgets')] },
      automatic: [],
      watchRepositories: [watchRepositoryRow()],
      watches: [watchRow()],
      repairs: [done],
      attempts: { 'rep-watch': done },
    });
    await page.goto('/repairs');
    const pull = page.getByRole('article', { name: 'Watched pull request acme/widgets #17' });
    await expect(pull).toContainText('Repair the failed checks at');
    await expect(pull.getByText('Intended action')).toBeVisible();
    await expect(pull.getByText('Next', { exact: true })).toBeVisible();

    await pull.getByText('Evidence, what failed and what changed').click();
    await expect(pull).toContainText('2 check results on');
    await expect(pull).toContainText('lint');
    await expect(pull).toContainText('failure');
    await expect(pull).toContainText('What Eli changed');
    await expect(pull).toContainText('internal/registry/client.go');
    await expect(pull).toContainText('Retried the registry call and raised its timeout.');
    // The attempt is a link out: raw model output and logs stay on its own page.
    await expect(pull.getByRole('link', { name: 'Open this attempt' })).toHaveAttribute(
      'href',
      '/repairs/rep-watch',
    );
  });

  test('lists the attempts on this pull request beside the current one', async ({ page }) => {
    const current = repairRow({
      id: 'rep-current',
      repo: 'acme/widgets',
      pull_number: 17,
      state: 'checks_failed',
      trigger: 'automatic',
      commit_sha: 'fe1d2c3b4a5968778695a4b3c2d1e0f9a8b7c6d5',
      created_at: '2026-10-18T10:20:00Z',
      updated_at: '2026-10-18T10:30:00Z',
    });
    const earlier = repairRow({
      id: 'rep-earlier',
      repo: 'acme/widgets',
      pull_number: 17,
      state: 'failed',
      created_at: '2026-10-18T09:20:00Z',
      updated_at: '2026-10-18T09:21:08Z',
    });
    const otherPull = repairRow({
      id: 'rep-other',
      repo: 'acme/widgets',
      pull_number: 18,
      state: 'succeeded',
      created_at: '2026-10-18T11:20:00Z',
      updated_at: '2026-10-18T11:21:08Z',
    });
    await mockRepairApi(page, {
      settings: { ...linkedAs('dana'), policies: [automaticPolicy('acme/widgets')] },
      automatic: [],
      watchRepositories: [watchRepositoryRow()],
      watches: [watchRow()],
      repairs: [earlier, current, otherPull],
    });
    await page.goto('/repairs');
    const pull = page.getByRole('article', { name: 'Watched pull request acme/widgets #17' });
    await pull.getByText('Evidence, what failed and what changed').click();
    const history = pull.getByRole('region', { name: 'Attempts on this pull request' });
    await expect(history).toBeVisible();
    await expect(history.getByRole('listitem')).toHaveCount(2);
    await expect(history).toContainText('Current');
    // The newest first, and only this pull request's attempts.
    await expect(history.getByRole('listitem').first()).toContainText('Checks failed');
    await expect(history).toContainText('Could not repair');
    await expect(history).not.toContainText('Checks passed');
    await expect(history.getByRole('link').first()).toHaveAttribute('href', '/repairs/rep-current');
  });

  test('says an observation failed instead of passing the pull request', async ({ page }) => {
    await mockRepairApi(page, {
      settings: { ...linkedAs('dana'), policies: [automaticPolicy('acme/widgets')] },
      automatic: [],
      watchRepositories: [watchRepositoryRow()],
      watches: [watchRow({ check_state: 'unknown', check_evidence: '[]' })],
    });
    await page.goto('/repairs');
    const pull = page.getByRole('article', { name: 'Watched pull request acme/widgets #17' });
    await expect(pull).toContainText('The last check read failed');
    await expect(pull).toContainText('could not be read');
    await expect(pull).not.toContainText('Checks passed');
  });
});

test.describe('the controls', () => {
  test('pausing a pull request says what happens to a repair already under way', async ({
    page,
  }) => {
    const { calls } = await mockRepairApi(page, {
      settings: { ...linkedAs('dana'), policies: [automaticPolicy('acme/widgets')] },
      automatic: [],
      watchRepositories: [watchRepositoryRow()],
      watches: [watchRow()],
    });
    await page.goto('/repairs');
    await page.getByRole('button', { name: 'Pause watching pull request 17' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText('acme/widgets #17');
    await expect(dialog).toContainText('stops before it changes the branch');
    await expect(dialog).toContainText('already publishing finishes');
    await dialog.getByRole('button', { name: 'Pause watching' }).click();
    await expect(dialog).toBeHidden();
    expect(calls.watchChanges).toEqual([
      {
        id: 'watch-1',
        action: 'pause',
        body: { reason: 'Watching was paused from the Eli repairs page.' },
      },
    ]);
    await expect(
      page.getByRole('article', { name: 'Watched pull request acme/widgets #17' }),
    ).toContainText('Paused');
  });

  test('resuming a pull request goes back through admission', async ({ page }) => {
    const { calls } = await mockRepairApi(page, {
      settings: { ...linkedAs('dana'), policies: [automaticPolicy('acme/widgets')] },
      automatic: [],
      watchRepositories: [watchRepositoryRow()],
      watches: [watchRow({ enabled: false, paused_reason: 'Paused yesterday.' })],
    });
    await page.goto('/repairs');
    await page.getByRole('button', { name: 'Resume watching pull request 17' }).click();
    await expect.poll(() => calls.watchChanges.length).toBe(1);
    expect(calls.watchChanges[0]).toMatchObject({ id: 'watch-1', action: 'resume' });
  });

  test('stops every watched pull request, naming how many', async ({ page }) => {
    const { calls } = await mockRepairApi(page, {
      settings: { ...linkedAs('dana'), policies: [automaticPolicy('acme/widgets')] },
      automatic: [],
      watchRepositories: [watchRepositoryRow()],
      watches: [watchRow(), watchRow({ id: 'w2', pull_number: 18 })],
    });
    await page.goto('/repairs');
    await page.getByRole('button', { name: 'Stop all' }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Stop watching 2 pull requests');
    await expect(dialog).toContainText('keep their settings');
    await dialog.getByRole('button', { name: 'Stop 2 watches' }).click();
    await expect.poll(() => calls.watchChanges.length).toBe(2);
    expect(calls.watchChanges.map((change) => change.action)).toEqual(['stop', 'stop']);
  });

  test('pauses new pull requests for a repository without touching the ones it follows', async ({
    page,
  }) => {
    const { calls } = await mockRepairApi(page, {
      settings: { ...linkedAs('dana'), policies: [automaticPolicy('acme/widgets')] },
      automatic: [],
      watchRepositories: [watchRepositoryRow()],
      watches: [watchRow()],
    });
    await page.goto('/repairs');
    const repo = page.getByRole('article', { name: 'Watched repository acme/widgets' });
    await expect(repo).toContainText('1 pull request already being followed carries on');
    await repo.getByRole('button', { name: 'Pause new pull requests' }).click();
    await expect.poll(() => calls.repositoryChanges.length).toBe(1);
    expect(calls.repositoryChanges[0]).toEqual({ id: 'decrepo-1', action: 'pause' });
    await expect(repo.getByRole('button', { name: 'Resume new pull requests' })).toBeVisible();
  });

  test('a viewer is offered nothing and the watches are never asked for', async ({ page }) => {
    const { calls } = await mockRepairApi(page, {
      role: 'viewer',
      settings: linkedAs('dana'),
      automatic: [],
    });
    await page.goto('/repairs');
    await expect(page.getByRole('heading', { level: 1, name: 'Eli repairs' })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Watching' })).toHaveCount(0);
    expect(calls.watchReads).toBe(0);
    expect(calls.watchRepositoryReads).toBe(0);
  });
});

test.describe('repair settings', () => {
  /** The reads the picker and the provider field need, beside the repairs stub. */
  async function settingsPage(page: Page) {
    const harness = await mockRepairApi(page, {
      settings: { ...linkedAs('dana'), policies: [] },
      automatic: [],
    });
    const policies: unknown[] = [];
    // Registered after the stub's catch-all, so these are the ones that answer.
    await page.route('**/api/v1/assistant/repairs/repositories?*', (route) =>
      route.fulfill({
        json: { items: ['acme/widgets'], total: 1, missing_permissions: [], capped: false },
      }),
    );
    await page.route('**/api/v1/installations', (route) =>
      route.fulfill({ json: { items: [{ id: 'ins_1', target: 'acme', target_type: 'org' }] } }),
    );
    await page.route('**/api/v1/users*', (route) => route.fulfill({ json: { items: [] } }));
    await page.route('**/api/v1/assistant/providers*', (route) =>
      route.fulfill({
        json: {
          items: [{ id: 'prv_1', name: 'Team provider', model: 'team-model', enabled: true }],
        },
      }),
    );
    await page.route('**/api/v1/assistant/repairs/policy', async (route) => {
      policies.push(route.request().postDataJSON());
      await route.fulfill({ json: route.request().postDataJSON() });
    });
    return { ...harness, policies };
  }

  test('offers the three behaviours and admits the repository as the one chosen', async ({
    page,
  }) => {
    const { calls, policies } = await settingsPage(page);
    await page.goto('/repairs/setup');
    await expect(page.getByRole('heading', { name: 'Choose repositories' })).toBeVisible();
    const behaviours = page.getByRole('group', {
      name: 'What should Eli do in these repositories?',
    });
    await expect(behaviours.getByRole('radio')).toHaveCount(3);
    await expect(behaviours).toContainText('Baby sit PRs');
    await expect(behaviours).toContainText('Fix failed checks only');
    await expect(behaviours).toContainText('Fix merge conflicts only');

    await page.getByRole('checkbox', { name: 'acme/widgets' }).check();
    await behaviours.getByRole('radio', { name: /Fix merge conflicts only/ }).check();
    await page.getByRole('button', { name: 'Save in 1 repository' }).click();

    await expect.poll(() => calls.admissions.length).toBe(1);
    // The policy is what repairs are allowed to be; the admission is the
    // separate, explicit decision to follow the repository's pull requests.
    expect(policies).toHaveLength(1);
    expect(policies[0]).toMatchObject({
      repo: 'acme/widgets',
      automatic: true,
      enabled: true,
      provider_id: 'prv_1',
    });
    expect(calls.admissions[0]).toEqual({
      repo: 'acme/widgets',
      installation_id: 'ins_1',
      capabilities: ['conflicts'],
    });
    await expect(
      page.getByText('acme/widgets: saved · watching 1 open pull request'),
    ).toBeVisible();
  });

  test('a refused admission leaves the settings saved and says why watching did not start', async ({
    page,
  }) => {
    await settingsPage(page);
    const refused: unknown[] = [];
    await page.route('**/api/v1/assistant/repairs/watches/repositories', (route) => {
      // The same path answers the list as a GET; only the admission is refused.
      if (route.request().method() !== 'POST') return route.fallback();
      refused.push(route.request().postDataJSON());
      return route.fulfill({
        status: 422,
        json: {
          error: {
            code: 'unprocessable',
            message:
              'continuous watching needs automatic repairs for this repository; ask me first and manual only keep their one-attempt behaviour',
          },
        },
      });
    });
    await page.goto('/repairs/setup');
    await page.getByRole('checkbox', { name: 'acme/widgets' }).check();
    await page.getByRole('button', { name: 'Save in 1 repository' }).click();
    await expect.poll(() => refused.length).toBe(1);
    expect(refused[0]).toMatchObject({
      repo: 'acme/widgets',
      capabilities: ['checks', 'conflicts'],
    });
    // The policy saved and the admission did not, and the line says both: a bare
    // "saved" would be a half-truth about a repository nothing follows.
    await expect(
      page.getByText('acme/widgets: saved · Saved, but Eli is not watching this repository'),
    ).toBeVisible();
  });

  test('lists what is being watched, with its pause, on the settings page too', async ({
    page,
  }) => {
    const { calls } = await settingsPage(page);
    await page.goto('/repairs/setup');
    const section = page.getByRole('region', { name: 'What Eli is watching now' });
    await expect(section).toBeVisible();
    await expect(section).toContainText('Nothing is being watched');
    expect(calls.watchReads).toBeGreaterThan(0);
  });
});
