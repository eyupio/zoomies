/**
 * The hub, /repairs: what needs the person, what is running, and what happened.
 *
 * Every test says what the controller answered, through `mockRepairApi`, and
 * checks what the page did with it. It runs on a desktop and on a phone, because
 * looking at what Eli has been doing from a phone is a stated requirement.
 */
import { expect, test, type Page } from '@playwright/test';
import {
  failedJob,
  keptCase,
  linkedAs,
  lintJob,
  mockRepairApi,
  offerRow,
  repairRow,
} from './support/repairs';

const COMMIT = 'b7d3e9150c2a4f8e6d1b0a93c5e7f2a4d6b8c0e1';

/** The history is closed while there is current work; open it before reading rows. */
async function openHistory(page: Page) {
  const region = page.getByRole('region', { name: 'History' });
  const details = region.locator('details').first();
  await details.evaluate((el) => {
    (el as HTMLDetailsElement).open = true;
  });
  return region;
}
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

const running = () =>
  repairRow({
    id: 'rep-run',
    state: 'working',
    created_at: ago(63_000),
    updated_at: ago(2000),
    details: { stage: 'prepare_fix', case: keptCase() },
  });
const passed = () =>
  repairRow({
    id: 'rep-done',
    state: 'succeeded',
    trigger: 'automatic',
    github_login: '',
    commit_sha: COMMIT,
    message: 'Checks passed on Eli’s commit b7d3e91.',
    created_at: ago(26 * 3_600_000),
    updated_at: ago(26 * 3_600_000 - 252_000),
    details: {
      case: keptCase({
        pull_title: 'Add a status line to the repairs page',
        branch: 'feat/repair-status',
        jobs: [{ name: 'Lint', step: 'staticcheck' }],
      }),
    },
  });
const failed = () =>
  repairRow({
    id: 'rep-failed',
    state: 'failed',
    pull_number: 12,
    message: 'Eli could not produce a supported fix.',
    created_at: ago(4 * 86_400_000),
    updated_at: ago(4 * 86_400_000 - 38_000),
    details: {
      case: keptCase({
        pull_title: 'Show the retention window on the Usage page',
        branch: 'feat/usage-retention',
        jobs: [{ name: 'UI (e2e)', step: 'Playwright' }],
      }),
    },
  });

test('the hub lists offers, running repairs and history with the failing job on every row', async ({
  page,
}) => {
  await mockRepairApi(page, {
    settings: linkedAs('dana'),
    jobs: [failedJob(), lintJob()],
    offers: [offerRow()],
    repairs: [running(), passed(), failed()],
  });
  await page.goto('/repairs');
  await expect(page.getByRole('heading', { level: 1, name: 'Eli repairs' })).toBeVisible();

  // Needs you: the pull request, where it is, and the jobs that failed in it.
  const offer = page.getByRole('region', { name: 'Needs you' }).getByRole('article');
  await expect(offer).toHaveCount(1);
  await expect(offer.getByRole('heading', { name: /Retry transient 502s/ })).toBeVisible();
  await expect(offer).toContainText('#17');
  await expect(offer).toContainText('acme/widgets');
  await expect(offer).toContainText('fix/registry-retry');
  await expect(offer).toContainText('Failed 14m ago');
  await expect(offer).toContainText('Go (api-2)');
  await expect(offer).toContainText('step 7, Run go test -race ./internal/...');
  await expect(offer).toContainText('Lint');
  await expect(offer).toContainText('step 5, Run staticcheck ./...');

  // In progress: the stage, who asked and for how long.
  const going = page.getByRole('region', { name: 'In progress' });
  await expect(going).toContainText('Preparing the fix');
  await expect(going.getByRole('heading', { name: /Retry transient 502s/ })).toBeVisible();
  await expect(going).toContainText('Requested by @dana');
  await expect(going).toContainText(/Running for \d+m \d\ds/);
  await expect(going.getByRole('img', { name: 'Step 3 of 5: Preparing the fix' })).toBeVisible();
  await expect(going.getByRole('link', { name: /Follow/ })).toHaveAttribute(
    'href',
    '/repairs/rep-run',
  );

  // History: every row says what it was about and opens the attempt.
  const rows = (await openHistory(page)).getByRole('article');
  await expect(rows).toHaveCount(3);

  const done = rows.filter({ hasText: 'Add a status line to the repairs page' });
  await expect(
    done.getByRole('link', { name: 'Add a status line to the repairs page' }),
  ).toHaveAttribute('href', '/repairs/rep-done');
  await expect(done).toContainText('Checks passed');
  await expect(done).toContainText('acme/widgets #17');
  await expect(done).toContainText('feat/repair-status');
  await expect(done).toContainText('Lint');
  await expect(done).toContainText('staticcheck');
  await expect(done.getByRole('link', { name: /b7d3e91/ })).toHaveAttribute(
    'href',
    `https://github.com/acme/widgets/commit/${COMMIT}`,
  );
  await expect(done).toContainText('Automatic');
  await expect(done).toContainText('4m 12s');

  const lost = rows.filter({ hasText: 'Show the retention window on the Usage page' });
  await expect(lost).toContainText('Could not repair');
  await expect(lost).toContainText('acme/widgets #12');
  await expect(lost).toContainText('UI (e2e)');
  await expect(lost).toContainText('Playwright');
  await expect(lost).toContainText('Eli could not produce a supported fix.');
  await expect(lost).toContainText('@dana');
  await expect(
    lost.getByRole('link', { name: 'Show the retention window on the Usage page' }),
  ).toHaveAttribute('href', '/repairs/rep-failed');
});

test('the hub puts current work first and keeps the history behind a summary', async ({ page }) => {
  await mockRepairApi(page, {
    settings: linkedAs('dana'),
    jobs: [failedJob(), lintJob()],
    offers: [offerRow()],
    repairs: [running(), passed(), failed()],
  });
  await page.goto('/repairs');

  const nav = page.getByRole('navigation', { name: 'On this page' });
  await expect(nav.getByRole('link', { name: 'Needs you 1' })).toBeVisible();
  await expect(nav.getByRole('link', { name: 'In progress 1' })).toBeVisible();
  if ((page.viewportSize()?.width ?? 0) > 768) {
    await expect(nav.getByRole('link', { name: 'History' })).toBeVisible();
  } else {
    await expect(nav.getByText('More')).toBeVisible();
  }

  await expect(
    page.getByRole('region', { name: 'Needs you' }).locator('details').first(),
  ).toHaveAttribute('open', '');
  await expect(
    page.getByRole('region', { name: 'In progress' }).locator('details').first(),
  ).toHaveAttribute('open', '');

  const history = page.getByRole('region', { name: 'History' });
  const details = history.locator('details').first();
  await expect(details).not.toHaveAttribute('open', '');
  await expect(history).toContainText('3 repairs');
  await details.locator('summary').first().click();
  await expect(details).toHaveAttribute('open', '');
  await expect(history.getByRole('article')).toHaveCount(3);
});

test('Review fix links to the repair page for that offer', async ({ page }) => {
  const { calls } = await mockRepairApi(page, {
    settings: linkedAs('dana'),
    jobs: [failedJob()],
    offers: [offerRow()],
  });
  await page.goto('/repairs');
  const review = page.getByRole('link', { name: 'Review fix for acme/widgets #17' });
  await expect(review).toHaveAttribute(
    'href',
    '/repairs/new?repo=acme%2Fwidgets&pull=17&run=482&offer=opp-1',
  );

  // Reviewing an offer reads; it starts nothing and spends nothing.
  await review.click();
  await expect(page).toHaveURL(/\/repairs\/new\?/);
  await expect(
    page.getByRole('heading', { level: 1, name: 'Repair a failed check' }),
  ).toBeVisible();
  expect(calls.starts).toEqual([]);
  expect(calls.offerActions).toEqual([]);
});

test('Dismiss hides the offer', async ({ page }) => {
  const { calls } = await mockRepairApi(page, {
    settings: linkedAs('dana'),
    jobs: [failedJob()],
    offers: [offerRow(), offerRow({ id: 'opp-2', pull_number: 20, title: 'Pin the base image' })],
  });
  await page.goto('/repairs');
  const needs = page.getByRole('region', { name: 'Needs you' });
  await expect(needs.getByRole('article')).toHaveCount(2);

  await needs.getByRole('button', { name: 'Dismiss the offer for acme/widgets #17' }).click();
  await expect(needs.getByRole('article')).toHaveCount(1);
  await expect(needs).not.toContainText('Retry transient 502s');
  await expect(needs).toContainText('Pin the base image');
  expect(calls.offerActions).toEqual([{ id: 'opp-1', body: { action: 'dismiss' } }]);
});

test('with nothing to review or running, the hub says where offers come from', async ({ page }) => {
  await mockRepairApi(page, { settings: linkedAs('dana') });
  await page.goto('/repairs');

  const needs = page.getByRole('region', { name: 'Needs you' });
  await expect(needs.getByText('No failed pull requests need you')).toBeVisible();
  await expect(needs.getByText('Fix a failed check')).toBeVisible();
  await expect(page.getByRole('region', { name: 'In progress' })).toHaveCount(0);
  const history = page.getByRole('region', { name: 'History' });
  await expect(history.locator('details').first()).not.toHaveAttribute('open', '');
  await expect(await openHistory(page)).toContainText('No repairs yet');
});

/**
 * Presses Refresh once the last one has settled: a press while the icon is still
 * turning is the same refresh, by design, and would read nothing new.
 */
async function refresh(page: Page) {
  const button = page.getByRole('button', { name: 'Refresh' });
  await expect(button).not.toHaveAttribute('aria-busy', 'true');
  await button.click();
}

test('the setup bar appears only when the person cannot repair yet', async ({ page }) => {
  const { state } = await mockRepairApi(page, { settings: linkedAs('dana') });
  await page.goto('/repairs');
  await expect(page.getByRole('region', { name: 'Repair settings' })).toHaveCount(0);

  // Linked, but the person has not said repairs may use their provider.
  state.settings = linkedAs('dana', false);
  await refresh(page);
  const bar = page.getByRole('region', { name: 'Repair settings' });
  await expect(bar).toContainText('Setup needed');
  await expect(bar).toContainText('GitHub @dana linked, repairs not allowed yet');
  await expect(bar.getByRole('link', { name: /Open settings/ })).toHaveAttribute(
    'href',
    '/repairs/setup',
  );

  // No link at all.
  state.settings = { identities: [], policies: [] };
  await refresh(page);
  await expect(bar).toContainText('Setup needed');
  await expect(bar).toContainText('No GitHub account linked');
});

test('a person who is ready is not shown a setup status line', async ({ page }) => {
  await mockRepairApi(page, {
    settings: {
      ...linkedAs('dana'),
      policies: [
        {
          repo: 'acme/widgets',
          installation_id: 'ins_1',
          provider_id: 'prv_1',
          enabled: true,
          automatic: false,
          ask_first: true,
          allow_workflows: false,
          daily_limit: 5,
        },
      ],
    },
  });
  await page.goto('/repairs');
  await expect(page.getByRole('region', { name: 'Repair settings' })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Repair settings' })).toHaveAttribute(
    'href',
    '/repairs/setup',
  );
});

test('a pull request link can be pasted directly on the hub without opening a dialog', async ({
  page,
}) => {
  await mockRepairApi(page, { settings: linkedAs('dana') });
  await page.goto('/repairs');

  const input = page.getByLabel('Pull request');
  await expect(input).toBeVisible();
  await input.fill('not-a-pr-url');
  await page.getByRole('button', { name: 'Check this pull request' }).click();
  await expect(page.getByText('Paste a GitHub pull request URL')).toBeVisible();

  await input.fill('https://github.com/acme/widgets/pull/17/checks');
  await page.getByRole('button', { name: 'Check this pull request' }).click();
  await expect(page).toHaveURL(/\/repairs\/new\?repo=acme%2Fwidgets&pull=17$/);
});

test('Fix a failed check lists recent failures and Settings goes to settings', async ({ page }) => {
  await mockRepairApi(page, {
    settings: linkedAs('dana'),
    jobs: [failedJob(), lintJob()],
  });
  await page.goto('/repairs');
  const head = page.locator('.page-header');

  await expect(head.getByRole('link', { name: 'Repair settings' })).toHaveAttribute(
    'href',
    '/repairs/setup',
  );
  await head.getByRole('button', { name: 'Fix a failed check' }).click();
  const dialog = page.getByRole('dialog', { name: 'Fix a failed check' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('list', { name: 'Recent failures' })).toBeVisible();
  await dialog.getByRole('button', { name: /Go \(api-2\)/ }).click();
  await expect(page).toHaveURL(/\/repairs\/new\?repo=acme%2Fwidgets&run=482&job=job_unit$/);
});

test('the floating Eli launcher stays off the repair pages', async ({ page }) => {
  await mockRepairApi(page, { settings: linkedAs('dana') });
  await page.goto('/repairs');
  await expect(page.getByRole('button', { name: 'Ask Eli' })).toHaveCount(0);
});

test('offers are read at most once a minute, and Refresh reads them at once', async ({ page }) => {
  await page.clock.install({ time: new Date() });
  const { calls } = await mockRepairApi(page, {
    settings: linkedAs('dana'),
    jobs: [failedJob()],
    offers: [offerRow()],
  });
  await page.goto('/repairs');
  await expect(
    page
      .getByRole('region', { name: 'Needs you' })
      .getByRole('heading', { name: /Retry transient/ }),
  ).toBeVisible();
  const polls = () => calls.lists.filter((query) => query.get('group') === 'active').length;
  expect(calls.offerReads).toBe(1);

  // Three ten-second polls have run, and none of them asked for the offers again.
  const before = polls();
  await page.clock.runFor(30_000);
  await expect.poll(polls).toBeGreaterThanOrEqual(before + 3);
  expect(calls.offerReads).toBe(1);

  // Past a minute they are read again, and Refresh does not wait for one.
  await page.clock.runFor(35_000);
  await expect.poll(() => calls.offerReads).toBe(2);
  await page.getByRole('button', { name: 'Refresh' }).click();
  await expect.poll(() => calls.offerReads).toBe(3);
});

test('everything a pull request or a job wrote is text, and none of it is a link', async ({
  page,
}) => {
  page.on('dialog', async (dialog) => {
    await dialog.dismiss();
    throw new Error(`a dialog opened: ${dialog.message()}`);
  });
  await mockRepairApi(page, {
    settings: linkedAs('<b>dana</b>'),
    jobs: [
      failedJob({
        job_name: '<script>alert(2)</script>',
        head_branch: '"><img src=x onerror=alert(3)>',
        html_url: 'javascript:alert(4)',
        failed_step: {
          number: 7,
          name: '<svg onload=alert(5)>',
          status: 'completed',
          conclusion: 'failure',
        },
      }),
    ],
    offers: [offerRow({ title: '<img src=x onerror=alert(1)>' })],
    repairs: [
      repairRow({
        id: 'rep-x',
        state: 'failed',
        message: '<b>bold</b> message',
        github_login: '<i>mallory</i>',
        details: {
          case: keptCase({
            pull_title: '<img src=x onerror=alert(6)>',
            pull_url: 'javascript:alert(7)',
            run_url: 'javascript:alert(8)',
            branch: '"><script>alert(9)</script>',
            jobs: [
              {
                name: '<script>alert(10)</script>',
                step: '<b>step</b>',
                url: 'javascript:alert(11)',
              },
            ],
          }),
        },
      }),
    ],
  });
  await page.goto('/repairs');

  const needs = page.getByRole('region', { name: 'Needs you' });
  await expect(
    needs.getByRole('heading', { name: '<img src=x onerror=alert(1)> #17' }),
  ).toBeVisible();
  await expect(needs).toContainText('<script>alert(2)</script>');
  const history = await openHistory(page);
  await expect(history.getByRole('link', { name: '<img src=x onerror=alert(6)>' })).toBeVisible();
  await expect(history).toContainText('<b>bold</b> message');
  await expect(history).toContainText('@<i>mallory</i>');

  await expect(page.locator('img[src="x"]')).toHaveCount(0);
  await expect(page.locator('main b, main i, main svg[onload]')).toHaveCount(0);
  await expect(page.locator('a[href^="javascript:" i]')).toHaveCount(0);
});

test('no horizontal scroll on a phone with very long names, and the sections stack', async ({
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 800 });
  // Hyphens and slashes are places a browser may break a line, so this has none.
  const long = `Name${'x'.repeat(170)}End`;
  await mockRepairApi(page, {
    settings: linkedAs('dana'),
    jobs: [failedJob({ job_name: long, head_branch: long })],
    offers: [offerRow({ title: long })],
    repairs: [
      running(),
      repairRow({
        id: 'rep-long',
        state: 'failed',
        message: `Error: ${'x'.repeat(300)}`,
        details: {
          case: keptCase({
            pull_title: long,
            branch: long,
            jobs: [{ name: long, step: long }],
          }),
        },
      }),
    ],
  });
  await page.goto('/repairs');
  const history = await openHistory(page);
  await expect(history.getByRole('article')).toHaveCount(2);

  const { scrollWidth, inner } = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    inner: window.innerWidth,
  }));
  expect(scrollWidth).toBeLessThanOrEqual(inner);

  // The three sections follow one another down the screen.
  const tops = await Promise.all(
    ['Needs you', 'In progress', 'History'].map(async (name) =>
      page.getByRole('region', { name }).evaluate((node) => node.getBoundingClientRect().top),
    ),
  );
  expect(tops[1]!).toBeGreaterThan(tops[0]!);
  expect(tops[2]!).toBeGreaterThan(tops[1]!);
});
