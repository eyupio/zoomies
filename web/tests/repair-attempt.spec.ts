/**
 * The attempt page, /repairs/:id: one repair, followed from the moment it is
 * queued to the sentence that says how it ended.
 *
 * Every test says what the controller answered, through `mockRepairApi`, and
 * checks what the page did with it. The page asks again every few seconds while
 * a repair is going, so the running test drives that with the browser's fake
 * clock and moves the repair on between two reads; the others are repairs that
 * had already finished, which the page reads once and leaves alone.
 */
import { expect, test } from '@playwright/test';
import { keptCase, mockRepairApi, repairRow } from './support/repairs';

const NOW = new Date('2026-10-18T09:21:03Z');
const COMMIT = 'b7d3e9150c2a4f8e6d1b0a93c5e7f2a4d6b8c0e1';

const running = (over = {}) =>
  repairRow({
    id: 'rep-run',
    state: 'working',
    updated_at: '2026-10-18T09:21:00Z',
    details: { stage: 'prepare_fix', model: 'my-model', source_bytes: 38_912, case: keptCase() },
    ...over,
  });

test('a running repair shows where it is, follows itself to the end and then stops asking', async ({
  page,
}) => {
  await page.clock.install({ time: NOW });
  const { state, calls } = await mockRepairApi(page, { attempts: { 'rep-run': running() } });
  await page.goto('/repairs/rep-run');

  await expect(page.getByRole('heading', { level: 1, name: 'Retry transient 502s' })).toBeVisible();
  const head = page.locator('.page-header');
  await expect(head.getByText('Investigating', { exact: true })).toBeVisible();
  await expect(head.getByText('Requested by @dana')).toBeVisible();
  await expect(head.getByRole('link', { name: /Open PR/ })).toHaveAttribute(
    'href',
    'https://github.com/acme/widgets/pull/17',
  );
  await expect(head.getByRole('link', { name: /Open run/ })).toHaveAttribute(
    'href',
    'https://github.com/acme/widgets/actions/runs/482',
  );

  const progress = page.getByRole('region', { name: 'Progress' });
  await expect(progress).toContainText('Preparing the fix');
  await progress.getByText('Show the steps').click();
  const steps = progress.getByRole('list', { name: 'What Eli is doing' }).getByRole('listitem');
  await expect(steps).toHaveText([
    /Queued/,
    /Reading the failed checks/,
    /Preparing the fix/,
    /Pushing the commit/,
    /Watching CI/,
  ]);
  await expect(steps.nth(2)).toHaveAttribute('aria-current', 'step');
  await expect(steps.nth(1)).toContainText('Done');
  await expect(steps.nth(2)).toContainText('In progress');
  await expect(steps.nth(3)).toContainText('Not started');
  await expect(progress).toContainText(/Elapsed 1m 0\ds/);
  await expect(progress).toContainText('You can leave this page');
  // Nothing has ended, so there is no outcome and nothing to try next yet.
  await expect(page.getByRole('region', { name: 'What to try next' })).toHaveCount(0);

  const facts = page.getByRole('complementary', { name: 'This attempt' });
  await expect(facts).toContainText('rep-run');
  await expect(facts).toContainText('Personal provider');
  await expect(facts).toContainText('fix/registry-retry');
  await expect(facts.getByRole('link')).toHaveCount(0);

  // Eli pushes a commit and starts watching CI.
  state.attempts['rep-run'] = running({
    state: 'checking',
    commit_sha: COMMIT,
    details: { stage: 'watch_ci', model: 'my-model', source_bytes: 38_912, case: keptCase() },
  });
  await page.clock.runFor(4500);
  await expect(steps.nth(4)).toHaveAttribute('aria-current', 'step');
  await expect(head.getByText('Waiting for CI', { exact: true })).toBeVisible();
  await expect(facts.getByRole('link', { name: /b7d3e91/ })).toHaveAttribute(
    'href',
    `https://github.com/acme/widgets/commit/${COMMIT}`,
  );

  // The checks pass: the track gives way to the outcome.
  state.attempts['rep-run'] = running({
    state: 'succeeded',
    commit_sha: COMMIT,
    message: 'Checks passed on Eli’s commit b7d3e91.',
    updated_at: '2026-10-18T09:30:00Z',
    details: { stage: 'watch_ci', model: 'my-model', source_bytes: 38_912, case: keptCase() },
  });
  await page.clock.runFor(4500);
  const outcome = page.getByRole('region', { name: 'Checks passed' });
  await expect(outcome).toContainText('Checks passed on Eli’s commit b7d3e91.');
  await expect(progress).toHaveCount(0);
  await expect(page.locator('output.sr-only', { hasText: 'Repair finished' })).toContainText(
    'Checks passed',
  );
  const timeline = page.locator('details.timeline-fold');
  await timeline.locator('summary').click();
  await expect(timeline).toContainText('Queued');

  // A repair that has finished is not asked about again.
  const asked = calls.attempts.length;
  await page.clock.runFor(60_000);
  expect(calls.attempts.length).toBe(asked);
});

// The page finds out by itself, on a timer, that the repair has moved. A stage that
// arrives that way has no business taking keyboard focus from wherever the person has
// gone in the meantime.
test('a repair that finishes while the page is open leaves keyboard focus where it was', async ({
  page,
}) => {
  await page.clock.install({ time: NOW });
  const { state } = await mockRepairApi(page, { attempts: { 'rep-run': running() } });
  await page.goto('/repairs/rep-run');
  const openPr = page.locator('.page-header').getByRole('link', { name: /Open PR/ });
  await expect(openPr).toBeVisible();
  await openPr.focus();
  await expect(openPr).toBeFocused();

  state.attempts['rep-run'] = running({
    state: 'succeeded',
    commit_sha: COMMIT,
    message: 'Checks passed on Eli’s commit b7d3e91.',
    details: { stage: 'watch_ci', model: 'my-model', source_bytes: 38_912, case: keptCase() },
  });
  await page.clock.runFor(4500);
  await expect(page.getByRole('region', { name: 'Checks passed' })).toBeVisible();
  await expect(openPr).toBeFocused();
});

test('a repair that could not fix the failure says why and what to try next', async ({ page }) => {
  await mockRepairApi(page, {
    attempts: {
      'rep-failed': repairRow({
        id: 'rep-failed',
        state: 'failed',
        message: 'Eli could not produce a supported fix.',
        details: {
          stage: 'prepare_fix',
          failure_code: 'unsupported_fix',
          model: 'my-model',
          source_bytes: 38_912,
          case: keptCase(),
          steps: [
            {
              input_tokens: 21_400,
              output_tokens: 1800,
              usage_reported: true,
              request_bytes: 80_312,
              elapsed_ms: 41_000,
              response_recorded: true,
              response:
                '{"read":["a.go"],"files":[...]}\n<script>throw new Error("untrusted")</script>',
              validation_error: 'Do not combine source requests with changes',
              response_truncated: true,
            },
            {
              input_tokens: 0,
              output_tokens: 0,
              usage_reported: false,
              request_bytes: 91_234,
              elapsed_ms: 9000,
            },
          ],
        },
      }),
    },
  });
  await page.goto('/repairs/rep-failed');

  const outcome = page.getByRole('region', { name: 'Could not repair' });
  await expect(outcome).toContainText('Eli could not produce a supported fix.');
  await expect(page.getByRole('region', { name: 'Progress' })).toHaveCount(0);
  const asked = page.getByRole('region', { name: 'What Eli was asked to fix' });
  await expect(asked).toContainText('acme/widgets #17');
  await asked.locator('summary').first().click();
  await expect(asked.getByRole('heading', { name: 'Retry transient 502s' })).toBeVisible();

  const next = page.getByRole('region', { name: 'What to try next' });
  await expect(next.getByRole('listitem').first()).toContainText(
    'Add a note naming the file or the test, then try again.',
  );
  await expect(next.getByRole('link', { name: 'Try another attempt' })).toHaveAttribute(
    'href',
    '/repairs/new?repo=acme%2Fwidgets&pull=17',
  );
  await expect(next.getByRole('link', { name: 'Repair settings' })).toHaveAttribute(
    'href',
    '/repairs/setup',
  );
  await expect(next.getByRole('link', { name: /Open pull request/ })).toHaveAttribute(
    'href',
    'https://github.com/acme/widgets/pull/17',
  );

  // The evidence is one press away: the outcome and the next action are the page.
  const tech = page.locator('details.tech');
  await expect(tech).not.toHaveAttribute('open', '');
  await tech.locator('summary').first().click();
  const rows = tech.getByRole('row');
  await expect(rows.nth(1).getByRole('cell')).toHaveText(['21,400', '1,800', '78 KB', '41s']);
  // Usage the provider did not report is unknown, never zero.
  await expect(rows.nth(2).getByRole('cell')).toHaveText(['Unknown', 'Unknown', '89 KB', '9.0s']);
  await expect(tech).toContainText('my-model');
  await expect(tech).toContainText('38 KB');
  await expect(tech).toContainText('Preparing the fix');
  await expect(tech).toContainText('unsupported_fix');
  await expect(tech.getByRole('button', { name: 'Copy investigation details' })).toBeVisible();
  const response = tech.locator('details.response').first();
  await response.locator('summary').click();
  await expect(response).toContainText('Do not combine source requests with changes');
  await expect(response).toContainText('shortened to the 96 KiB retention limit');
  await expect(response.locator('pre')).toContainText(
    '<script>throw new Error("untrusted")</script>',
  );
  await page.evaluate(() => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: async (value: string) => {
          document.documentElement.dataset.copiedResponse = value;
        },
      },
    });
  });
  await response.getByRole('button', { name: 'Copy model response 1', exact: true }).click();
  expect(await page.evaluate(() => document.documentElement.dataset.copiedResponse)).toBe(
    '{"read":["a.go"],"files":[...]}\n<script>throw new Error("untrusted")</script>',
  );
  const oldResponse = tech.locator('details.response').nth(1);
  await oldResponse.locator('summary').click();
  await expect(oldResponse).toContainText('Response not retained for this older call');

  const facts = page.getByRole('complementary', { name: 'This attempt' });
  await expect(facts).toContainText('Took');
  await expect(facts).toContainText('1m 08s');
  const timeline = page.locator('details.timeline-fold');
  await timeline.locator('summary').click();
  await expect(timeline).toContainText('Queued');
  await expect(timeline).toContainText('Could not repair');
  // The sentence is the outcome card's; the timeline does not repeat it.
  await expect(timeline).not.toContainText('supported fix');
});

// A repair made before cases were kept has its row and nothing about the failure.
// It is still a repair somebody may want to read the outcome of.
test('a repair from before cases were kept still opens, and says what it does not have', async ({
  page,
}) => {
  await mockRepairApi(page, {
    attempts: {
      'rep-old': repairRow({
        id: 'rep-old',
        state: 'failed',
        message: 'Eli could not produce a supported fix.',
      }),
    },
  });
  await page.goto('/repairs/rep-old');

  await expect(
    page.getByRole('heading', { level: 1, name: 'Pull request #17 in acme/widgets' }),
  ).toBeVisible();
  const asked = page.getByRole('region', { name: 'What Eli was asked to fix' });
  await asked.locator('summary').first().click();
  await expect(asked.getByText('did not keep a record of the jobs')).toBeVisible();
  await expect(asked.getByText('Pull request details were not kept')).toBeVisible();
  await expect(page.getByText('Pull request not looked up yet')).toHaveCount(0);
  // Without a job's address there is no host to build a link on.
  await expect(page.locator('.page-header').getByRole('link', { name: /Open PR/ })).toHaveCount(0);
  await expect(page.getByRole('region', { name: 'Could not repair' })).toBeVisible();
});

test('a repair that is not in your history says so and does not guess', async ({ page }) => {
  const { calls } = await mockRepairApi(page);
  await page.goto('/repairs/rep-missing');

  await expect(
    page.getByRole('heading', { name: 'This repair is not in your history' }),
  ).toBeVisible();
  await expect(page.getByRole('link', { name: 'Back to Eli repairs' })).toHaveAttribute(
    'href',
    '/repairs',
  );
  expect(calls.attempts).toEqual(['rep-missing']);
});

test('a failed job’s error output is read when the job is opened and not before', async ({
  page,
}) => {
  const { calls } = await mockRepairApi(page, {
    attempts: { 'rep-failed': repairRow({ id: 'rep-failed', details: { case: keptCase() } }) },
    errorOutput: {
      '9001': {
        lines: ['--- FAIL: TestRegistryRetries (0.31s)', 'client_test.go:88: got 1, want 3'],
      },
      '9002': {
        status: 409,
        message:
          'Your linked GitHub account needs write access to this repository to read a job’s error output. Link and confirm it in Eli repair settings.',
      },
    },
  });
  await page.goto('/repairs/rep-failed');

  const asked = page.getByRole('region', { name: 'What Eli was asked to fix' });
  await asked.locator('summary').first().click();
  const failed = asked;
  await expect(failed.locator('details.job')).toHaveCount(2);
  expect(calls.errorOutput).toHaveLength(0);
  // A kept case has the step's name but not its number, and does not guess one.
  await expect(failed.locator('details.job', { hasText: 'Go (api-2)' })).toContainText(
    'Failed at step: Run go test -race ./internal/...',
  );
  await expect(failed).not.toContainText('step ?');

  await failed.locator('details.job', { hasText: 'Go (api-2)' }).locator('summary').click();
  await expect(failed.getByText('client_test.go:88: got 1, want 3')).toBeVisible();
  expect(calls.errorOutput).toHaveLength(1);
  expect(calls.errorOutput[0]?.get('job_id')).toBe('9001');
  expect(calls.errorOutput[0]?.get('repo')).toBe('acme/widgets');

  // The controller decides who may read a log; the page shows its answer.
  await failed.locator('details.job', { hasText: 'Lint' }).locator('summary').click();
  await expect(failed.getByText(/needs write access to this repository/)).toBeVisible();
  // A stored case does not say whose fault a failure was, so it makes no claim.
  await expect(failed.getByText('Workflow failure')).toHaveCount(0);
});

test('everything a workflow or a pull request wrote is text, and none of it is a link', async ({
  page,
}) => {
  page.on('dialog', async (dialog) => {
    await dialog.dismiss();
    throw new Error(`a dialog opened: ${dialog.message()}`);
  });
  await mockRepairApi(page, {
    attempts: {
      'rep-x': repairRow({
        id: 'rep-x',
        message: '<b>bold</b> message',
        github_login: '<i>mallory</i>',
        details: {
          case: keptCase({
            pull_title: '<img src=x onerror=alert(1)>',
            pull_url: 'javascript:alert(3)',
            run_url: 'javascript:alert(4)',
            author: '<b>dana</b>',
            branch: '"><script>alert(5)</script>',
            jobs: [
              {
                name: '<script>alert(2)</script>',
                step: '<svg onload=alert(6)>',
                url: 'javascript:alert(7)',
              },
            ],
          }),
        },
      }),
    },
  });
  await page.goto('/repairs/rep-x');

  await expect(
    page.getByRole('heading', { level: 1, name: '<img src=x onerror=alert(1)>' }),
  ).toBeVisible();
  await expect(page.getByRole('region', { name: 'Could not repair' })).toContainText(
    '<b>bold</b> message',
  );
  await expect(page.getByText('Requested by @<i>mallory</i>')).toBeVisible();
  await expect(page.locator('img[src="x"]')).toHaveCount(0);
  await expect(page.locator('main b, main i, main svg[onload]')).toHaveCount(0);
  await expect(page.locator('a[href^="javascript:" i]')).toHaveCount(0);
  await expect(
    page.locator('.page-header').getByRole('link', { name: /Open (PR|run)/ }),
  ).toHaveCount(0);
  await expect(
    page
      .getByRole('region', { name: 'What Eli was asked to fix' })
      .getByRole('link', { name: /Open job on GitHub/ }),
  ).toHaveCount(0);
});

test('refresh asks at once, a hidden tab does not poll, and leaving stops it for good', async ({
  page,
}) => {
  await page.clock.install({ time: NOW });
  const { calls } = await mockRepairApi(page, { attempts: { 'rep-run': running() } });
  await page.goto('/repairs/rep-run');
  await expect(page.getByRole('heading', { level: 1, name: 'Retry transient 502s' })).toBeVisible();

  const first = calls.attempts.length;
  await page.getByRole('button', { name: 'Refresh' }).click();
  await expect.poll(() => calls.attempts.length).toBeGreaterThan(first);

  const setVisibility = (value: 'hidden' | 'visible') =>
    page.evaluate((state) => {
      Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state });
      document.dispatchEvent(new Event('visibilitychange'));
    }, value);

  await setVisibility('hidden');
  const hidden = calls.attempts.length;
  await page.clock.runFor(20_000);
  expect(calls.attempts.length).toBe(hidden);

  await setVisibility('visible');
  await expect.poll(() => calls.attempts.length).toBeGreaterThan(hidden);

  await page.locator('.page-header').getByRole('link', { name: 'Eli repairs' }).click();
  await expect(page).toHaveURL(/\/repairs$/);
  const left = calls.attempts.length;
  await page.clock.runFor(30_000);
  expect(calls.attempts.length).toBe(left);
});

test('a read that fails keeps what was on screen, says so, and recovers', async ({ page }) => {
  await page.clock.install({ time: NOW });
  const { state } = await mockRepairApi(page, { attempts: { 'rep-run': running() } });
  await page.goto('/repairs/rep-run');
  await expect(page.getByRole('heading', { level: 1, name: 'Retry transient 502s' })).toBeVisible();

  state.attempts['rep-run'] = { status: 500, message: 'the controller could not read the repair' };
  await page.clock.runFor(4500);
  await expect(page.getByText(/Could not refresh just now/)).toBeVisible();
  // The last answer is still there, and the repair is still followed.
  await expect(page.getByRole('region', { name: 'Progress' })).toBeVisible();

  state.attempts['rep-run'] = running();
  await page.clock.runFor(4500);
  await expect(page.getByText(/Could not refresh just now/)).toHaveCount(0);
});

test('no horizontal scroll on a phone, and the track stacks', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  const branch = `fix/${'a-very-long-branch-name-'.repeat(5)}end`;
  const long = `Error: ${'x'.repeat(300)}`;
  const steps = Array.from({ length: 6 }, (_, i) => ({
    input_tokens: 21_400 + i,
    output_tokens: 1800,
    usage_reported: true,
    request_bytes: 80_312,
    elapsed_ms: 41_000,
  }));
  const { state } = await mockRepairApi(page, {
    attempts: {
      'rep-run': running({ details: { stage: 'prepare_fix', case: keptCase({ branch }) } }),
    },
  });
  await page.goto('/repairs/rep-run');
  const progress = page.getByRole('region', { name: 'Progress' });
  await progress.getByText('Show the steps').click();
  const items = progress.getByRole('list', { name: 'What Eli is doing' }).getByRole('listitem');
  await expect(items).toHaveCount(5);
  const tops = await items.evaluateAll((nodes) => nodes.map((n) => n.getBoundingClientRect().top));
  expect(tops[1]!).toBeGreaterThan(tops[0]!);
  expect(tops[4]!).toBeGreaterThan(tops[3]!);

  const overflow = () =>
    page.evaluate(() => ({
      scrollWidth: document.documentElement.scrollWidth,
      inner: window.innerWidth,
    }));
  let { scrollWidth, inner } = await overflow();
  expect(scrollWidth).toBeLessThanOrEqual(inner);

  state.attempts['rep-run'] = repairRow({
    id: 'rep-run',
    state: 'failed',
    message: long,
    details: {
      stage: 'prepare_fix',
      failure_code: 'unsupported_fix',
      model: `a-model-with-a-${'long-'.repeat(20)}name`,
      source_bytes: 38_912,
      case: keptCase({ branch }),
      steps,
    },
  });
  await page.reload();
  await expect(page.getByRole('region', { name: 'Could not repair' })).toContainText(long);
  ({ scrollWidth, inner } = await overflow());
  expect(scrollWidth).toBeLessThanOrEqual(inner);
});
