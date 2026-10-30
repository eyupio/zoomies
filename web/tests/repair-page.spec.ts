/**
 * The repair page, /repairs/new: one failure, what is in the way of repairing it,
 * and the one button that starts a repair.
 *
 * It runs on a desktop and a phone, because looking at a failure from a phone is
 * a stated requirement. Every test says what the controller answered, through
 * `mockRepairApi`, and checks what the page did with it, including the writes
 * that must never happen on a read: opening the page checks readiness and starts
 * nothing, and only the button that says so spends an attempt.
 */
import { expect, test, type Page } from '@playwright/test';
import type { ReadinessJob } from './support/repairs';
import {
  blockedAt,
  failedJob,
  lintJob,
  mockRepairApi,
  readiness,
  repairRow,
  SHA,
} from './support/repairs';

const PAGE = '/repairs/new?repo=acme%2Fwidgets&run=482';
const ACCESS = 'Your linked GitHub account needs write access to this repository.';

test('blocked on access shows the failure, names the jobs and keeps the rest unchecked', async ({
  page,
}) => {
  const { calls } = await mockRepairApi(page, {
    jobs: [failedJob(), lintJob()],
    readiness: blockedAt('access', ACCESS),
  });
  await page.goto(`${PAGE}&job=job_unit`);

  const nav = page.getByRole('navigation', { name: 'On this page' });
  await expect(nav.getByRole('link', { name: 'Next step' })).toBeVisible();
  await expect(nav.getByRole('link', { name: /What failed/ })).toBeVisible();
  await expect(nav.getByRole('link', { name: /Checks/ })).toBeVisible();

  const failure = page.getByRole('region', { name: 'CI #482 failed on fix/registry-retry' });
  await expect(failure).toBeVisible();
  await expect(failure.getByRole('link', { name: /acme\/widgets/ })).toHaveAttribute(
    'href',
    'https://github.com/acme/widgets',
  );
  await expect(failure.getByText('fix/registry-retry', { exact: true })).toBeVisible();
  await expect(failure.getByRole('link', { name: /a41c9e2/ })).toHaveAttribute(
    'href',
    `https://github.com/acme/widgets/commit/${SHA}`,
  );
  await expect(failure.getByRole('link', { name: /CI #482/ })).toHaveAttribute(
    'href',
    'https://github.com/acme/widgets/actions/runs/482',
  );
  await expect(failure.getByText('Pull request not looked up yet')).toBeVisible();

  const failed = page.getByRole('region', { name: 'What failed' });
  await expect(failed.locator('details.job', { hasText: 'Go (api-2)' })).toContainText(
    'Failed at step 7: Run go test -race ./internal/...',
  );
  await expect(failed.locator('details.job', { hasText: 'Lint' })).toContainText(
    'Failed at step 5: Run staticcheck ./...',
  );
  await expect(failed.getByText('You opened this one')).toBeVisible();

  const checks = page.getByRole('region', { name: 'Can Eli act on this?' });
  await expect(checks.getByRole('listitem').filter({ hasText: 'Write access' })).toContainText(
    ACCESS,
  );
  // Only the blocked rows are on show; the rest are one press away.
  await checks.getByText('Show all 9 checks').click();
  await expect(checks.getByRole('listitem').filter({ hasText: 'Repository policy' })).toContainText(
    'Not checked yet.',
  );
  await expect(page.getByRole('heading', { name: 'Fix write access first' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Fix with Eli', exact: true })).toHaveCount(0);

  expect(calls.readiness).toHaveLength(1);
  expect(calls.readiness[0]?.get('run_id')).toBe('482');
  expect(calls.starts).toEqual([]);
});

test('ready shows the pull request, loads error output when the job opens and starts one repair', async ({
  page,
}) => {
  const { calls } = await mockRepairApi(page, {
    jobs: [failedJob(), lintJob()],
    readiness: readiness(),
    errorOutput: {
      '9001': {
        lines: [
          '--- FAIL: TestRegistryRetries (0.31s)',
          'client_test.go:88: got 1 attempt, want 3',
        ],
      },
    },
  });
  await page.goto(PAGE);

  await expect(page.getByRole('heading', { name: 'Retry transient 502s' })).toBeVisible();
  await expect(page.getByRole('link', { name: /acme\/widgets #17/ })).toHaveAttribute(
    'href',
    'https://github.com/acme/widgets/pull/17',
  );
  await expect(page.getByText('Opened by @dana, merging into main')).toBeVisible();

  // A log is read when somebody opens a job, and not before.
  expect(calls.errorOutput).toHaveLength(0);
  await page.locator('details.job', { hasText: 'Go (api-2)' }).locator('summary').first().click();
  await expect(page.getByText('client_test.go:88: got 1 attempt, want 3')).toBeVisible();
  await expect(page.getByText('nothing here is stored', { exact: false })).toBeVisible();
  expect(calls.errorOutput).toHaveLength(1);
  expect(calls.errorOutput[0]?.get('repo')).toBe('acme/widgets');
  expect(calls.errorOutput[0]?.get('job_id')).toBe('9001');

  expect(calls.starts).toEqual([]);
  await page.getByLabel('Add a note for Eli (optional)').fill('Look at the retry loop');
  await page.getByRole('button', { name: 'Fix with Eli', exact: true }).click();
  await expect(page).toHaveURL(/\/repairs\/rep-1$/);
  expect(calls.starts).toEqual([
    {
      repo: 'acme/widgets',
      pull_number: 17,
      head_sha: SHA,
      instruction: 'Look at the retry loop',
      run_id: 482,
    },
  ]);
});

test('a fleet fault says a repair is unlikely to help', async ({ page }) => {
  await mockRepairApi(page, {
    jobs: [
      failedJob({
        fault_domain: 'fleet',
        fault_kind: 'out_of_memory',
        fault_fix: 'Give this pool runners with more memory, then re-run the job.',
      }),
    ],
    readiness: readiness(),
  });
  await page.goto(PAGE);

  const job = page.locator('details.job', { hasText: 'Go (api-2)' });
  await expect(job).toContainText('Fleet fault: Out of memory');
  // A job that is the only one there is starts open.
  await expect(job.getByText('A repair is unlikely to help', { exact: false })).toBeVisible();
  await expect(job.getByText('Give this pool runners with more memory')).toBeVisible();
});

test('a closed pull request replaces the card and offers no start', async ({ page }) => {
  const message = 'PR #17 is closed or merged. This failure no longer needs a PR repair.';
  const { calls } = await mockRepairApi(page, {
    jobs: [failedJob()],
    readiness: blockedAt('pull', message, { blockers: [{ code: 'closed', message }] }),
  });
  await page.goto(PAGE);

  await expect(page.getByRole('heading', { name: 'This PR is closed' })).toBeVisible();
  // The words are on the card, in the checklist and in the live region.
  await expect(page.getByRole('complementary', { name: 'Next step' })).toContainText(message);
  await expect(page.getByRole('link', { name: /original run on GitHub/ })).toHaveAttribute(
    'href',
    'https://github.com/acme/widgets/actions/runs/482',
  );
  await expect(page.getByRole('button', { name: 'Fix with Eli', exact: true })).toHaveCount(0);
  expect(calls.starts).toEqual([]);
});

test('a run with no recorded jobs still explains itself', async ({ page }) => {
  await mockRepairApi(page, {
    jobs: [],
    readiness: blockedAt('access', ACCESS),
  });
  await page.goto(PAGE);

  const failure = page.getByRole('region', { name: 'A repair in acme/widgets' });
  await expect(failure.getByText('acme/widgets', { exact: true })).toBeVisible();
  await expect(failure.getByText('Run 482')).toBeVisible();
  await expect(page.getByRole('region', { name: 'What failed' })).toContainText(
    'Zoomies has no record of the jobs',
  );
  // Nothing is known to have failed, so the card does not count zero of it in red.
  await expect(page.getByRole('region', { name: 'What failed' })).not.toContainText(
    '0 failed jobs',
  );
  await expect(page.getByRole('heading', { name: 'Fix write access first' })).toBeVisible();
});

// A failure this fleet never ran (GitHub's own runners, or records since gone)
// is named from what readiness read on GitHub: the jobs, the step each stopped
// at, and the error lines of the one the person opens.
test('a run the fleet never ran still names what failed, from GitHub', async ({ page }) => {
  const job = (id: number, name: string, step: string | undefined): ReadinessJob => ({
    id,
    name,
    conclusion: 'failure',
    ...(step ? { step, step_number: 8 } : {}),
    url: `https://github.com/acme/widgets/actions/runs/482/job/${id}`,
  });
  const { calls } = await mockRepairApi(page, {
    jobs: [],
    readiness: readiness({
      failed_jobs: [
        {
          ...job(7001, 'Publish Latest Image / build-and-push', 'Install dependencies and build'),
          started_at: new Date(Date.now() - 9 * 60_000).toISOString(),
          completed_at: new Date(Date.now() - 4 * 60_000).toISOString(),
          steps: [
            { number: 1, name: 'Set up job', conclusion: 'success' },
            { number: 8, name: 'Install dependencies and build', conclusion: 'failure' },
          ],
        },
        job(7002, 'Publish Latest Image / smoke', undefined),
      ],
    }),
    errorOutput: { '7001': { lines: ['npm error Missing script: build'] } },
  });
  await page.goto(PAGE);

  const failed = page.getByRole('region', { name: 'What failed' });
  await expect(failed).toContainText('a run this fleet did not run');
  await expect(failed.locator('details.job', { hasText: 'build-and-push' })).toContainText(
    'Failed at step 8: Install dependencies and build',
  );
  // A log is read when somebody opens a job, and not before, by GitHub's id
  // for it, which the fleet never kept.
  expect(calls.errorOutput).toHaveLength(0);
  await failed
    .locator('details.job', { hasText: 'build-and-push' })
    .locator('summary')
    .first()
    .click();
  await expect(page.getByText('npm error Missing script: build')).toBeVisible();
  expect(calls.errorOutput).toHaveLength(1);
  expect(calls.errorOutput[0]?.get('job_id')).toBe('7001');
});

test('the old address still works', async ({ page }) => {
  await mockRepairApi(page, { jobs: [failedJob()] });
  await page.goto('/repairs?repo=acme/site&run=9');
  await expect(page).toHaveURL(/\/repairs\/new\?repo=acme%2Fsite&run=9$/);
  await expect(page.getByRole('heading', { name: 'Repair a failed check' })).toBeVisible();
  // A pasted pull request takes the same road.
  await page.goto('/repairs?repo=acme/site&pull=3');
  await expect(page).toHaveURL(/\/repairs\/new\?repo=acme%2Fsite&pull=3$/);
  await page.goto('/repairs?url=https://github.com/acme/site/pull/3');
  await expect(page).toHaveURL(/\/repairs\/new\?repo=acme%2Fsite&pull=3$/);
});

// Everything on the page was written by somebody who can open a pull request or
// edit a workflow. A title with markup in it is text, and a job address that is
// not a web address is not a link.
test('hostile text renders as text', async ({ page }) => {
  page.on('dialog', (dialog) => {
    throw new Error(`the page ran script: ${dialog.message()}`);
  });
  await mockRepairApi(page, {
    jobs: [
      failedJob({
        job_name: '<img src=x onerror=alert(2)>',
        html_url: 'javascript:alert(1)',
      }),
    ],
    readiness: readiness({
      pull: {
        number: 17,
        title: '<img src=x onerror=alert(1)>',
        author: 'dana',
        branch: 'fix/registry-retry',
        base: 'main',
        url: 'javascript:alert(3)',
        open: true,
      },
    }),
  });
  await page.goto(PAGE);

  await expect(page.getByRole('heading', { name: '<img src=x onerror=alert(1)>' })).toBeVisible();
  await expect(page.locator('img[src="x"]')).toHaveCount(0);
  const job = page.locator('details.job').first();
  await expect(job).toContainText('<img src=x onerror=alert(2)>');
  await expect(job.getByRole('link', { name: /Open job on GitHub/ })).toHaveCount(0);
  await expect(page.getByRole('link', { name: /acme\/widgets #17/ })).toHaveCount(0);
});

test('no horizontal scroll with a very long branch name and error line', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  const branch = `fix/${'a-very-long-branch-name-'.repeat(5)}end`;
  await mockRepairApi(page, {
    jobs: [failedJob({ head_branch: branch })],
    readiness: readiness({
      pull: {
        number: 17,
        title: 'Retry transient 502s',
        author: 'dana',
        branch,
        base: 'main',
        url: 'https://github.com/acme/widgets/pull/17',
        open: true,
      },
    }),
    errorOutput: { '9001': { lines: [`Error: ${'x'.repeat(300)}`] } },
  });
  await page.goto(PAGE);
  await expect(page.getByText(`Error: ${'x'.repeat(300)}`)).toBeVisible();
  const { scrollWidth, inner } = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    inner: window.innerWidth,
  }));
  expect(scrollWidth).toBeLessThanOrEqual(inner);
});

// A repair already running for a failure is the process itself, so the page that
// would offer to queue a second one takes the person to the one that exists.
test('a repair already running for this failure opens its own page', async ({ page }) => {
  await mockRepairApi(page, {
    jobs: [failedJob()],
    repairs: [
      {
        id: 'rep-active',
        installation_id: 'ins_1',
        repo: 'acme/widgets',
        head_sha: SHA,
        commit_sha: '',
        user_id: 'user-1',
        github_login: 'dana',
        provider_id: 'prv_1',
        trigger: 'mention',
        state: 'working',
        message: '',
        pull_number: 17,
        job_id: 0,
        run_id: 482,
        github_user_id: 42,
        comment_id: 0,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ],
  });
  await page.goto(PAGE);
  await expect(page).toHaveURL(/\/repairs\/rep-active$/);
});

// A failure is reached by its run, but only a repair Eli queued itself records a
// run: one queued for a pasted pull request, or before runs were recorded, is known
// by its pull request and commit. Looked for by the run alone it is never found, and
// whoever came back to the failure was offered it a second time. Once readiness has
// said which pull request and commit the run is, the page asks again of those.
test('arriving by run follows a repair queued for that pull request and commit', async ({
  page,
}) => {
  const queued = repairRow({ id: 'rep-queued', state: 'working', run_id: 0 });
  const { calls } = await mockRepairApi(page, {
    jobs: [failedJob()],
    repairs: [queued],
    attempts: { 'rep-queued': queued },
  });
  await page.goto(PAGE);

  await expect(page).toHaveURL(/\/repairs\/rep-queued$/);
  await expect(page.getByRole('region', { name: 'Progress' })).toContainText('Preparing the fix');
  await expect(page.getByRole('button', { name: 'Fix with Eli', exact: true })).toHaveCount(0);
  // Only the repairs that are running are asked for, so a repository with a long
  // history cannot push the one under way off the page that is read.
  const asked = calls.lists.map((query) => [query.get('repo'), query.get('group')]);
  expect(asked).toEqual([['acme/widgets', 'active']]);
});

test('arriving by run still offers a repair when the one running is for another commit', async ({
  page,
}) => {
  const older = repairRow({
    id: 'rep-older',
    state: 'working',
    run_id: 0,
    head_sha: 'an-older-head',
  });
  await mockRepairApi(page, {
    jobs: [failedJob()],
    repairs: [older],
    attempts: { 'rep-older': older },
  });
  await page.goto(PAGE);

  await expect(page.getByRole('button', { name: 'Fix with Eli', exact: true })).toBeVisible();
  await expect(page).toHaveURL(/\/repairs\/new\?/);
});

test('arriving by pull request follows a repair already running for that pull request', async ({
  page,
}) => {
  const running = repairRow({ id: 'rep-running', state: 'checking', run_id: 0 });
  await mockRepairApi(page, { repairs: [running], attempts: { 'rep-running': running } });
  await page.goto('/repairs/new?repo=acme%2Fwidgets&pull=17');

  await expect(page).toHaveURL(/\/repairs\/rep-running$/);
});

test('an offer is accepted as the offer, with its own run and no note', async ({ page }) => {
  const { calls } = await mockRepairApi(page, {
    jobs: [failedJob()],
    offers: [
      {
        id: 'opp-1',
        repo: 'acme/widgets',
        installation_id: 'ins_1',
        pull_number: 17,
        head_sha: SHA,
        run_id: 482,
        title: 'Retry transient 502s',
        state: 'open',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ],
  });
  await page.goto('/repairs/new?offer=opp-1');
  await expect(page.getByRole('heading', { name: 'Retry transient 502s' })).toBeVisible();
  expect(calls.readiness[0]?.get('pull_number')).toBe('17');
  await expect(page.getByLabel('Add a note for Eli (optional)')).toHaveCount(0);
  await page.getByRole('button', { name: 'Fix with Eli', exact: true }).click();
  await expect(page).toHaveURL(/\/repairs\/rep-offer$/);
  expect(calls.offerActions).toEqual([{ id: 'opp-1', body: { action: 'accept' } }]);
  expect(calls.starts).toEqual([]);
});

test('an offer that is gone says so and sends the person back', async ({ page }) => {
  await mockRepairApi(page, { offers: [] });
  await page.goto('/repairs/new?offer=opp-missing');
  await expect(
    page.getByRole('heading', { name: 'This offer is no longer available' }),
  ).toBeVisible();
  await expect(page.getByRole('link', { name: 'Back to Eli repairs' })).toHaveAttribute(
    'href',
    '/repairs',
  );
});

test('a refused start because the target moved re-reads readiness and explains', async ({
  page,
}) => {
  const message = 'PR #17 has newer commits than this failed run.';
  // The controller refuses the start and, from then on, says the pull request moved.
  const { state, calls } = await mockRepairApi(page, {
    jobs: [failedJob()],
    readiness: readiness(),
    start: () => {
      state.readiness = blockedAt('pull', message, { blockers: [{ code: 'superseded', message }] });
      return {
        status: 409,
        body: {
          error: {
            code: 'conflict',
            message: 'The PR has a newer commit since you reviewed this failure.',
          },
        },
      };
    },
  });
  await page.goto(PAGE);
  await page.getByRole('button', { name: 'Fix with Eli', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'A newer commit replaced this failure' }),
  ).toBeVisible();
  await expect(page.getByRole('button', { name: 'Fix with Eli', exact: true })).toHaveCount(0);
  expect(calls.starts).toHaveLength(1);
  expect(calls.readiness.length).toBeGreaterThanOrEqual(2);
});

test('opening /repairs/new without a target presents the inline pull request field without a modal', async ({
  page,
}) => {
  await mockRepairApi(page, { jobs: [failedJob()], readiness: readiness() });
  await page.goto('/repairs/new');
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await page
    .getByLabel('Pull request', { exact: true })
    .fill('https://github.com/acme/gadgets/pull/9');
  await page.getByRole('button', { name: 'Check this pull request' }).click();
  await expect(page).toHaveURL(/\/repairs\/new\?repo=acme%2Fgadgets&pull=9$/);
});

test('Choose another pull request parses a pasted address and moves the page to it', async ({
  page,
}) => {
  const { calls } = await mockRepairApi(page, { jobs: [failedJob()], readiness: readiness() });
  await page.goto(PAGE);
  await page.getByRole('button', { name: 'Choose another pull request' }).click();
  const dialog = page.getByRole('dialog', { name: 'Fix a failed check' });
  await dialog.getByText('Paste a pull request link', { exact: true }).click();
  await dialog.getByLabel('Pull request').fill('not an address');
  await dialog.getByRole('button', { name: 'Check this pull request' }).click();
  await expect(dialog.getByText('Paste a GitHub pull request URL')).toBeVisible();
  await dialog.getByLabel('Pull request').fill('https://github.com/acme/gadgets/pull/9');
  await dialog.getByRole('button', { name: 'Check this pull request' }).click();
  await expect(page).toHaveURL(/\/repairs\/new\?repo=acme%2Fgadgets&pull=9$/);
  await expect(page.getByRole('dialog')).toHaveCount(0);
  // One question about the pasted pull request, asked by the page it moved to. A
  // second one is a page that was rebuilt under the first, with its check in flight.
  await expect(page.getByRole('heading', { name: 'Ready to fix' })).toBeVisible();
  expect(calls.readiness.filter((query) => query.get('pull_number') === '9')).toHaveLength(1);
});

/** Where keyboard focus is, named by the region it sits in (or by the element, outside any). */
const focusedIn = (page: Page) =>
  page.evaluate(() => {
    const active = document.activeElement;
    return active?.closest('aside')?.getAttribute('aria-label') ?? active?.tagName ?? 'nothing';
  });

// Checking again replaces what the card says, and the button that was pressed goes
// with it when the answer is that nothing is in the way. Keyboard focus went with
// it, to the top of the document, and whoever was working by keyboard had to find the
// card again.
test('the card keeps keyboard focus when checking again replaces the button that was pressed', async ({
  page,
}) => {
  const { state } = await mockRepairApi(page, {
    jobs: [failedJob()],
    readiness: blockedAt('provider', 'Add an enabled personal model provider in Eli settings.'),
  });
  await page.goto(PAGE);
  const card = page.getByRole('complementary', { name: 'Next step' });
  const again = card.getByRole('button', { name: 'Check again' });
  await expect(again).toBeVisible();
  await again.focus();

  // The provider is added in the meantime, so the check finds nothing in the way.
  state.readiness = readiness();
  await page.keyboard.press('Enter');
  await expect(card.getByRole('button', { name: 'Fix with Eli', exact: true })).toBeVisible();
  await expect.poll(() => focusedIn(page)).toBe('Next step');
});
