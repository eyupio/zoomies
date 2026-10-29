/**
 * Automatic repairs on the Eli repairs pages: the hub's panel and the stop that
 * goes with it, the attempt page's card, and the repair page's note.
 *
 * Where nothing is automatic the pages say nothing about it, and neither do they
 * to anyone who cannot read the policies, so each test says what the controller
 * answered and checks what the page did with it. Every one runs on a desktop and
 * on a phone, because stopping the automation from a phone is the case that
 * matters when something is going wrong away from a desk.
 */
import { expect, test } from '@playwright/test';

// Supporting detail is closed while work is in front of it; these tests open it.
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('zoomies.repairs.automatic.open', 'true');
    localStorage.setItem('zoomies.repairs.history.open', 'true');
  });
});
import {
  automaticPolicy,
  automaticRow,
  failedJob,
  keptCase,
  linkedAs,
  mockRepairApi,
  readiness,
  repairRow,
  settingsWithAutomatic,
  stoppedAt,
} from './support/repairs';

const ago = (ms: number) => new Date(Date.now() - ms).toISOString();
const inMinutes = (minutes: number) => new Date(Date.now() + minutes * 60_000).toISOString();

const NO_PROVIDER =
  'No installation provider is set for this repository, so automatic repairs cannot start. Choose one in setup.';

test.describe('the hub', () => {
  test('says nothing about automatic repairs where none is on', async ({ page }) => {
    const { calls } = await mockRepairApi(page, {
      settings: {
        ...linkedAs('dana'),
        policies: [automaticPolicy('acme/widgets', { automatic: false, ask_first: true })],
      },
      automatic: [],
    });
    await page.goto('/repairs');
    await expect(page.getByRole('heading', { level: 1, name: 'Eli repairs' })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Repair settings' })).toHaveCount(0);
    await expect(page.getByRole('region', { name: 'Automatic repairs' })).toHaveCount(0);
    expect(calls.automaticReads).toBeGreaterThan(0);
  });

  test('says nothing to someone who cannot read the policies, and does not ask', async ({
    page,
  }) => {
    const { calls } = await mockRepairApi(page, {
      role: 'viewer',
      settings: linkedAs('dana'),
      automatic: { status: 403, message: 'your account is not allowed to do that' },
    });
    await page.goto('/repairs');
    await expect(page.getByRole('heading', { level: 1, name: 'Eli repairs' })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Automatic repairs' })).toHaveCount(0);
    await expect(page.getByRole('alert')).toHaveCount(0);
    expect(calls.automaticReads).toBe(0);
  });

  test('lists each automatic repository with who pays, its allowance, its health and its latest repair', async ({
    page,
  }) => {
    await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets', 'acme/site'),
      automatic: [
        automaticRow({
          repo: 'acme/site',
          provider_id: '',
          provider_name: '',
          model: '',
          problem: NO_PROVIDER,
          used: 0,
        }),
        automaticRow({
          repo: 'acme/widgets',
          used: 3,
          running: 1,
          last: {
            id: 'rep-done',
            state: 'succeeded',
            pull_number: 1127,
            updated_at: ago(2 * 3_600_000),
          },
        }),
      ],
    });
    await page.goto('/repairs');
    const panel = page.getByRole('region', { name: 'Automatic repairs' });
    await expect(panel).toBeVisible();
    await expect(panel).toContainText('without asking');
    await expect(panel).toContainText('never merges');
    const rows = panel.getByRole('article');
    await expect(rows).toHaveCount(2);

    // What cannot run is first, with what to do about it.
    const broken = rows.nth(0);
    await expect(broken.getByRole('heading', { name: 'acme/site' })).toBeVisible();
    await expect(broken).toContainText('Needs attention');
    await expect(broken).toContainText(NO_PROVIDER);
    await expect(broken).toContainText('No installation provider is set');
    await expect(broken).toContainText('None yet');
    await expect(broken.getByRole('link', { name: 'Fix in setup' })).toHaveAttribute(
      'href',
      '/repairs/setup?repo=acme%2Fsite',
    );

    const healthy = rows.nth(1);
    await expect(healthy.getByRole('heading', { name: 'acme/widgets' })).toBeVisible();
    await expect(healthy).toContainText('Ready');
    await expect(healthy).toContainText('Paid by Team provider (team-model)');
    await expect(healthy).toContainText('3 of 5 attempts used in the last 24 hours');
    await expect(
      healthy.getByRole('meter', { name: 'Allowance used on acme/widgets' }),
    ).toHaveAttribute('aria-valuenow', '3');
    await expect(healthy).toContainText('1 running now');
    await expect(healthy.getByRole('link', { name: /Checks passed on #1127/ })).toHaveAttribute(
      'href',
      '/repairs/rep-done',
    );
  });

  test('says when a spent allowance has a place again', async ({ page }) => {
    await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      automatic: [automaticRow({ used: 5, daily_limit: 5, next_free_at: inMinutes(40) })],
    });
    await page.goto('/repairs');
    const row = page.getByRole('region', { name: 'Automatic repairs' }).getByRole('article');
    await expect(row).toContainText('Allowance used');
    await expect(row).toContainText('All 5 attempts used in the last 24 hours');
    await expect(row).toContainText('Automatic repairs here wait until');
    await expect(row).toContainText(/\(in (39|40)m\)/);
    await expect(row.getByRole('meter')).toHaveAttribute('aria-valuenow', '5');
  });

  test('is counted in the section navigation', async ({ page }) => {
    await mockRepairApi(page, {
      settings: {
        ...linkedAs('dana'),
        policies: [
          automaticPolicy('acme/a'),
          automaticPolicy('acme/b'),
          automaticPolicy('acme/c', { automatic: false, ask_first: true }),
          automaticPolicy('acme/off', { enabled: false }),
        ],
      },
      automatic: [automaticRow({ repo: 'acme/a' }), automaticRow({ repo: 'acme/b' })],
    });
    await page.goto('/repairs');
    const nav = page.getByRole('navigation', { name: 'On this page' });
    await expect(nav.getByRole('link', { name: 'Automatic 2' })).toBeVisible();
  });

  test('keeps what it last read and says so when it cannot read again', async ({ page }) => {
    const { state } = await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      automatic: [automaticRow()],
    });
    await page.goto('/repairs');
    const panel = page.getByRole('region', { name: 'Automatic repairs' });
    await expect(panel.getByRole('article')).toHaveCount(1);
    state.automatic = { status: 500, message: 'The overview could not be read.' };
    await page.getByRole('button', { name: 'Refresh' }).click();
    await expect(panel).toContainText('Could not read automatic repairs');
    await expect(panel.getByRole('article')).toHaveCount(1);
  });
});

test.describe('stopping', () => {
  test('one repository asks what should happen instead, defaults to ask me first, and the panel drops it', async ({
    page,
  }) => {
    const { calls } = await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets', 'acme/site'),
      automatic: [automaticRow({ repo: 'acme/site' }), automaticRow({ repo: 'acme/widgets' })],
    });
    await page.goto('/repairs');
    await page.getByRole('button', { name: 'Stop automatic repairs on acme/widgets' }).click();

    const dialog = page.getByRole('dialog', { name: 'Stop automatic repairs' });
    await expect(dialog).toContainText('Eli will stop repairing acme/widgets by itself.');
    await expect(dialog.getByRole('radio', { name: /Ask me first/ })).toBeChecked();
    await expect(dialog).toContainText(
      'When checks fail, Eli offers a repair under Needs you and waits for you.',
    );
    await expect(dialog).toContainText('turned on again in setup');
    await expect(dialog).toContainText(
      'A repair that has not pushed yet stops before it changes the branch, unless it is already publishing.',
    );
    await dialog.getByRole('button', { name: 'Stop automatic repairs' }).click();

    await expect(page.getByText('Stopped automatic repairs on acme/widgets')).toBeVisible();
    await expect(page.getByText('It asks first now.')).toBeVisible();
    expect(calls.stops).toEqual([{ repos: ['acme/widgets'], then: 'ask_first' }]);
    const panel = page.getByRole('region', { name: 'Automatic repairs' });
    await expect(panel.getByRole('article')).toHaveCount(1);
    await expect(panel.getByRole('heading', { name: 'acme/site' })).toBeVisible();
    const nav = page.getByRole('navigation', { name: 'On this page' });
    await expect(nav.getByRole('link', { name: 'Automatic 1' })).toBeVisible();
  });

  test('choosing to turn repairs off sends that', async ({ page }) => {
    const { calls } = await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      automatic: [automaticRow()],
    });
    await page.goto('/repairs');
    await page.getByRole('button', { name: 'Stop automatic repairs on acme/widgets' }).click();
    const dialog = page.getByRole('dialog', { name: 'Stop automatic repairs' });
    await dialog.getByRole('radio', { name: /Turn repairs off/ }).check();
    await dialog.getByRole('button', { name: 'Stop automatic repairs' }).click();
    await expect(page.getByText('Repairs are off there now.')).toBeVisible();
    expect(calls.stops).toEqual([{ repos: ['acme/widgets'], then: 'off' }]);
  });

  test('cancelling sends nothing and puts keyboard focus back on the button', async ({ page }) => {
    const { calls } = await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      automatic: [automaticRow()],
    });
    await page.goto('/repairs');
    const open = page.getByRole('button', { name: 'Stop automatic repairs on acme/widgets' });
    await open.click();
    await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(open).toBeFocused();
    expect(calls.stops).toEqual([]);
  });

  test('all of them in one go, with keyboard focus left on the page heading when the panel goes', async ({
    page,
  }) => {
    const { calls } = await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/a', 'acme/b', 'acme/c'),
      automatic: [
        automaticRow({ repo: 'acme/a' }),
        automaticRow({ repo: 'acme/b' }),
        automaticRow({ repo: 'acme/c' }),
      ],
    });
    await page.goto('/repairs');
    await page.getByRole('button', { name: 'Stop all' }).click();
    const dialog = page.getByRole('dialog', { name: 'Stop automatic repairs on 3 repositories' });
    await expect(dialog).toContainText('acme/a, acme/b and acme/c');
    await dialog.getByRole('button', { name: 'Stop automatic repairs' }).click();

    await expect(page.getByText('Stopped automatic repairs on 3 repositories')).toBeVisible();
    expect(calls.stops).toEqual([{ repos: ['acme/a', 'acme/b', 'acme/c'], then: 'ask_first' }]);
    await expect(page.getByRole('region', { name: 'Automatic repairs' })).toHaveCount(0);
    // The button that opened the dialog is gone with the panel; focus must not be lost with it.
    await expect(page.getByRole('heading', { level: 1, name: 'Eli repairs' })).toBeFocused();
  });

  test('there is no stop-all for a single repository', async ({ page }) => {
    await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      automatic: [automaticRow()],
    });
    await page.goto('/repairs');
    await expect(page.getByRole('region', { name: 'Automatic repairs' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Stop all' })).toHaveCount(0);
  });

  test('a stop that fails keeps the dialog and the panel and says which repository', async ({
    page,
  }) => {
    const { state, calls } = await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      automatic: [automaticRow()],
    });
    state.stop = (body) => ({
      status: 200,
      body: {
        results: body.repos.map((repo) => ({
          repo,
          changed: false,
          error: 'Could not save this repository’s policy. Try again.',
        })),
      },
    });
    await page.goto('/repairs');
    await page.getByRole('button', { name: 'Stop automatic repairs on acme/widgets' }).click();
    const dialog = page.getByRole('dialog', { name: 'Stop automatic repairs' });
    await dialog.getByRole('button', { name: 'Stop automatic repairs' }).click();
    await expect(dialog.getByRole('alert')).toContainText(
      'acme/widgets: Could not save this repository’s policy. Try again.',
    );
    await expect(page.getByRole('region', { name: 'Automatic repairs' })).toBeVisible();
    // Trying again is the same press.
    state.stop = null;
    await dialog.getByRole('button', { name: 'Stop automatic repairs' }).click();
    await expect(page.getByText('Stopped automatic repairs on acme/widgets')).toBeVisible();
    expect(calls.stops).toHaveLength(2);
  });

  test('a repository somebody else already stopped reads as already stopped, not as an error', async ({
    page,
  }) => {
    const { state } = await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      automatic: [automaticRow()],
    });
    state.stop = (body) => {
      state.automatic = [];
      return {
        status: 200,
        body: { results: body.repos.map((repo) => ({ repo, changed: false })) },
      };
    };
    await page.goto('/repairs');
    await page.getByRole('button', { name: 'Stop automatic repairs on acme/widgets' }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Stop automatic repairs' }).click();
    await expect(page.getByText('Already stopped')).toBeVisible();
    await expect(page.getByRole('alert')).toHaveCount(0);
    await expect(page.getByRole('region', { name: 'Automatic repairs' })).toHaveCount(0);
  });
});

/** A repair Eli started by itself on acme/widgets #17, which has since failed its checks. */
const automaticAttempt = (over: Parameters<typeof repairRow>[0] = {}) =>
  repairRow({
    id: 'rep-auto',
    trigger: 'automatic',
    github_login: '',
    user_id: '',
    state: 'checks_failed',
    message: 'Checks failed on Eli’s commit b7d3e91.',
    details: { case: keptCase() },
    ...over,
  });

test.describe('the attempt page', () => {
  const card = (page: import('@playwright/test').Page) =>
    page.getByRole('region', { name: 'Automatic repairs on acme/widgets' });

  test('says an automatic repair was started by the repository’s policy', async ({ page }) => {
    await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      attempts: { 'rep-auto': automaticAttempt() },
      automatic: [automaticRow({ used: 3 })],
    });
    await page.goto('/repairs/rep-auto');
    const facts = page.getByRole('complementary', { name: 'This attempt' });
    await expect(facts).toContainText('Started by');
    await expect(facts).toContainText('Automatic repairs, by the repository’s policy');
  });

  test('shows an administrator where the policy stands, and stops it from the repair', async ({
    page,
  }) => {
    const { calls } = await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      attempts: { 'rep-auto': automaticAttempt() },
      automatic: [automaticRow({ used: 3 })],
    });
    await page.goto('/repairs/rep-auto');
    await expect(card(page)).toContainText('Automatic repairs are on');
    await expect(card(page)).toContainText('Paid by Team provider (team-model)');
    await expect(card(page)).toContainText('3 of 5 attempts used in the last 24 hours');
    await expect(card(page).getByRole('link', { name: 'Policy in setup' })).toHaveAttribute(
      'href',
      '/repairs/setup?repo=acme%2Fwidgets',
    );

    await card(page).getByRole('button', { name: 'Stop automatic repairs' }).click();
    const dialog = page.getByRole('dialog', { name: 'Stop automatic repairs' });
    await dialog.getByRole('button', { name: 'Stop automatic repairs' }).click();
    await expect(page.getByText('Stopped automatic repairs on acme/widgets')).toBeVisible();
    expect(calls.stops).toEqual([{ repos: ['acme/widgets'], then: 'ask_first' }]);
    await expect(card(page)).toContainText('no longer set to repair automatically');
    await expect(card(page).getByRole('button', { name: 'Stop automatic repairs' })).toHaveCount(0);
  });

  test('puts keyboard focus on the card when the button that was pressed has left it', async ({
    page,
  }) => {
    await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      attempts: { 'rep-auto': automaticAttempt() },
      automatic: [automaticRow()],
    });
    await page.goto('/repairs/rep-auto');
    await card(page).getByRole('button', { name: 'Stop automatic repairs' }).click();
    await page
      .getByRole('dialog', { name: 'Stop automatic repairs' })
      .getByRole('button', { name: 'Stop automatic repairs' })
      .click();
    await expect(card(page)).toContainText('no longer set to repair automatically');
    await expect(
      card(page).getByRole('heading', { name: 'Automatic repairs on acme/widgets' }),
    ).toBeFocused();
  });

  test('says when stopping would also stop this repair, if it is still going', async ({ page }) => {
    await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      attempts: {
        'rep-auto': automaticAttempt({
          state: 'working',
          message: '',
          details: { stage: 'prepare_fix', case: keptCase() },
        }),
      },
      automatic: [automaticRow({ running: 1 })],
    });
    await page.goto('/repairs/rep-auto');
    await expect(card(page)).toContainText(
      'Stopping now also stops this repair if it has not pushed yet.',
    );
  });

  test('says a repository that stopped being automatic is no longer, and links to setup', async ({
    page,
  }) => {
    await mockRepairApi(page, {
      settings: {
        ...linkedAs('dana'),
        policies: [automaticPolicy('acme/widgets', { automatic: false, ask_first: true })],
      },
      attempts: { 'rep-auto': automaticAttempt() },
      automatic: [],
    });
    await page.goto('/repairs/rep-auto');
    await expect(card(page)).toContainText('no longer set to repair automatically');
    await expect(card(page).getByRole('link', { name: 'Policy in setup' })).toBeVisible();
  });

  test('leaves the rest of the page standing when the status cannot be read', async ({ page }) => {
    await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      attempts: { 'rep-auto': automaticAttempt() },
      automatic: { status: 500, message: 'The overview could not be read.' },
    });
    await page.goto('/repairs/rep-auto');
    await expect(card(page)).toContainText(
      'Could not read the repository’s automatic repair status',
    );
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    await expect(page.getByRole('complementary', { name: 'This attempt' })).toContainText(
      'Started by',
    );
  });

  test('shows a repair somebody asked for no card and reads nothing about automation', async ({
    page,
  }) => {
    const { calls } = await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      attempts: { 'rep-req': repairRow({ id: 'rep-req', trigger: 'mention', state: 'failed' }) },
      automatic: [automaticRow()],
    });
    await page.goto('/repairs/rep-req');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    await expect(card(page)).toHaveCount(0);
    await expect(page.getByRole('complementary', { name: 'This attempt' })).not.toContainText(
      'Started by',
    );
    expect(calls.automaticReads).toBe(0);
  });

  test('does not read the overview for someone who is not an administrator', async ({ page }) => {
    const { calls } = await mockRepairApi(page, {
      role: 'viewer',
      attempts: { 'rep-auto': automaticAttempt() },
    });
    await page.goto('/repairs/rep-auto');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    await expect(card(page)).toHaveCount(0);
    expect(calls.automaticReads).toBe(0);
  });
});

test.describe('the repair page', () => {
  const FAILURE = '/repairs/new?repo=acme%2Fwidgets&run=482';
  const note = (page: import('@playwright/test').Page) =>
    page.getByRole('complementary', { name: 'Next step' });
  const automatic = () => readiness({ policy_mode: 'automatic' });

  test('tells an administrator Eli already tried this failure on its own, and shows the attempt', async ({
    page,
  }) => {
    await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      jobs: [failedJob()],
      readiness: automatic(),
      repairs: [automaticAttempt()],
      automatic: [automaticRow()],
    });
    await page.goto(FAILURE);
    const aside = note(page);
    await expect(
      aside.getByRole('heading', { name: 'Eli already tried this on its own' }),
    ).toBeVisible();
    await expect(aside).toContainText('Checks failed.');
    await expect(aside).toContainText('starting another attempt here is the retry');
    await expect(aside.getByRole('link', { name: 'View the attempt' })).toHaveAttribute(
      'href',
      '/repairs/rep-auto',
    );
    // The attempt does not take the button away: starting again is the retry.
    await expect(aside.getByRole('button', { name: 'Fix with Eli' })).toBeVisible();
  });

  test('tells an administrator nothing started, and why, when the overview knows', async ({
    page,
  }) => {
    await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      jobs: [failedJob()],
      readiness: automatic(),
      automatic: [automaticRow({ used: 1 })],
    });
    await page.goto(FAILURE);
    const aside = note(page);
    await expect(
      aside.getByRole('heading', { name: 'Nothing has started on its own for this failure' }),
    ).toBeVisible();
    await expect(aside).toContainText('GitHub’s report of a failed job');
    await expect(aside.getByRole('button', { name: 'Fix with Eli' })).toBeVisible();
  });

  test('names a spent allowance as the reason, with when it frees', async ({ page }) => {
    await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      jobs: [failedJob()],
      readiness: readiness({
        policy_mode: 'automatic',
        ready: false,
        remaining: 0,
        blockers: [{ code: 'budget', message: 'The repair allowance is exhausted.' }],
        checks: stoppedAt('budget', 'The repair allowance is exhausted.', 'budget'),
      }),
      automatic: [
        automaticRow({
          used: 5,
          daily_limit: 5,
          next_free_at: new Date(Date.now() + 40 * 60_000).toISOString(),
        }),
      ],
    });
    await page.goto(FAILURE);
    const aside = note(page);
    await expect(aside).toContainText('allowance is used up');
    await expect(aside).toContainText(/wait until .+ \(in (39|40)m\)/);
    await expect(aside).not.toContainText('GitHub’s report of a failed job');
  });

  test('names a provider that cannot pay as the reason', async ({ page }) => {
    await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      jobs: [failedJob()],
      readiness: automatic(),
      automatic: [
        automaticRow({
          provider_id: '',
          provider_name: '',
          model: '',
          problem:
            'No installation provider is set for this repository, so automatic repairs cannot start.',
        }),
      ],
    });
    await page.goto(FAILURE);
    await expect(note(page)).toContainText('No installation provider is set for this repository');
    await expect(note(page)).toContainText('That is why nothing has started');
  });

  test('gives anyone else one neutral sentence and reads nothing about what Eli has done', async ({
    page,
  }) => {
    const { calls } = await mockRepairApi(page, {
      role: 'viewer',
      settings: linkedAs('dana'),
      jobs: [failedJob()],
      readiness: automatic(),
      repairs: [automaticAttempt()],
      automatic: [automaticRow()],
    });
    await page.goto(FAILURE);
    const aside = note(page);
    await expect(
      aside.getByRole('heading', { name: 'This repository repairs itself' }),
    ).toBeVisible();
    await expect(aside).toContainText('An administrator can see what it has done.');
    await expect(aside).not.toContainText('Eli already tried');
    await expect(aside).not.toContainText('Nothing has started');
    expect(calls.automaticReads).toBe(0);
    expect(calls.lists.some((query) => query.get('trigger') === 'automatic')).toBe(false);
  });

  test('says nothing for a repository that asks first, or is manual, or whose failure has been fixed', async ({
    page,
  }) => {
    for (const answer of [
      readiness({ policy_mode: 'ask_first' }),
      readiness({ policy_mode: 'manual' }),
      readiness({
        policy_mode: 'automatic',
        ready: false,
        blockers: [
          {
            code: 'fixed',
            message:
              'The latest checks on this PR have passed. This failure has already been fixed; no repair is needed.',
          },
        ],
        checks: stoppedAt('checks', 'The latest checks have passed.', 'fixed'),
      }),
    ]) {
      const { calls } = await mockRepairApi(page, {
        settings: settingsWithAutomatic('acme/widgets'),
        jobs: [failedJob()],
        readiness: answer,
        automatic: [automaticRow()],
      });
      await page.goto(FAILURE);
      await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
      await expect(note(page)).toBeVisible();
      await expect(note(page)).not.toContainText('repairs itself');
      await expect(note(page)).not.toContainText('on its own');
      expect(calls.automaticReads).toBe(0);
      await page.unroute('**/api/v1/**');
    }
  });

  test('still opens the repair that is running, with no note', async ({ page }) => {
    await mockRepairApi(page, {
      settings: settingsWithAutomatic('acme/widgets'),
      jobs: [failedJob()],
      readiness: automatic(),
      repairs: [
        automaticAttempt({
          state: 'working',
          message: '',
          details: { stage: 'prepare_fix', case: keptCase() },
        }),
      ],
      attempts: {
        'rep-auto': automaticAttempt({
          state: 'working',
          message: '',
          details: { stage: 'prepare_fix', case: keptCase() },
        }),
      },
      automatic: [automaticRow({ running: 1 })],
    });
    await page.goto(FAILURE);
    await expect(page).toHaveURL(/\/repairs\/rep-auto$/);
  });
});
