/**
 * What the pages do with strings somebody else chose.
 *
 * Almost nothing on these pages is written by the operator. Repository and
 * workflow names come from GitHub, runner and job names from the fleet, labels
 * from whoever enrolled the host, and a runner's log is the output of code the
 * job decided to run. Every one of them reaches a page that also holds the
 * operator's session.
 *
 * Svelte escapes interpolated text and a terminal is a terminal rather than a
 * document, so the point of this file is not to discover that they do -- it is
 * to fail if either ever stops. Both halves assert the absence of something,
 * which is worth nothing unless the payload really arrived, so each one checks
 * that it did before asserting what it did not do.
 */
import { expect, test, type Dialog, type Page } from '@playwright/test';
import { expectNoSidewaysScroll, FIXTURE, goto, readThrough } from './support/fixtures';
import {
  automaticPolicy,
  automaticRow,
  failedJob,
  keptCase,
  linkedAs,
  mockRepairWorld,
  offerRow,
  readiness,
  repairRow,
} from './support/repairs';

/** Collected rather than dismissed quietly: a dialog here is the bug. */
function watchForDialogs(page: Page): Dialog[] {
  const seen: Dialog[] = [];
  page.on('dialog', (d) => {
    seen.push(d);
    void d.dismiss();
  });
  return seen;
}

test('a pool named with markup is shown as text, not run as it', async ({ page }) => {
  const dialogs = watchForDialogs(page);

  // The name is put into the response rather than into the database. The
  // controller does accept it -- a pool name is free text under 64 characters,
  // and POST /api/v1/pools answers 201 for this one -- but creating it here
  // would write pool.create and pool.delete rows into a fixture the audit
  // specs count, and a test that breaks its neighbours is not worth the extra
  // yard of realism. What is under test is what the page does with a name it
  // is given, and this gives it one.
  const name = 'zoomies-<img src=x onerror=alert(1)><script>alert(2)</script>';
  await page.route('**/api/v1/pools**', async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    const { response: response, body } = read;
    // Every row rather than the first: another spec may have added a pool
    // before this one runs, and which rows the grid has drawn is then not
    // something this test should depend on.
    if (Array.isArray(body.items)) {
      for (const pool of body.items) pool.name = name;
    }
    await route.fulfill({ response, json: body });
  });

  await goto(page, '/pools', 'Pools');

  // The payload really is on the page. An assertion about what did not happen
  // proves nothing if the string never arrived.
  await expect(page.getByText(name, { exact: true }).first()).toBeVisible();

  // And it arrived as text: had it been parsed there would be an img and a
  // script element in the document instead of those characters.
  await expect(page.locator('img[src="x"]')).toHaveCount(0);
  await expect(page.locator('script:has-text("alert(2)")')).toHaveCount(0);

  expect(dialogs, 'a dialog opened, so something in the name ran').toHaveLength(0);
});

test("a runner's output cannot retitle the page or clear it", async ({ page }) => {
  const dialogs = watchForDialogs(page);

  // The bytes the viewer would receive, delivered as the relay delivers them.
  // What is under test is what the terminal does with the escapes, not that
  // the relay carries them -- that is proven against the real binary in Go, by
  // TestRunnerLogStream -- and a job can print any of these.
  const chunks = [
    // OSC 2: set the window title.
    '\x1b]2;pwned by a build log\x07',
    // OSC 8: hyperlinks whose targets are a script URL and a whole document.
    // Nothing here clicks them -- the terminal draws to a canvas, so there is
    // no element to click -- but if the terminal ever activated one of its own
    // accord, the dialog watcher below is what would notice.
    '\x1b]8;;javascript:alert(1)\x1b\\click me\x1b]8;;\x1b\\\r\n',
    '\x1b]8;;data:text/html,<script>alert(2)</script>\x1b\\or me\x1b]8;;\x1b\\\r\n',
    // Clear the screen, the scrollback, and home the cursor.
    '\x1b[2J\x1b[3J\x1b[H',
    'the line after everything above\r\n',
  ];
  const body = chunks.map((c) => 'event: log\ndata: ' + JSON.stringify(c) + '\n\n').join('');

  await page.route('**/api/v1/runners/*/logs**', async (route) => {
    await route.fulfill({
      status: 200,
      headers: { 'content-type': 'text/event-stream', 'cache-control': 'no-store' },
      body: ': attached\n\n' + body,
    });
  });

  await goto(page, `/runners/${FIXTURE.busyRunnerId}`, FIXTURE.busyRunner);
  // Busy runners open in Build log mode, but these unmarked events belong to
  // the diagnostics view whose line counter reports raw log output.
  await page.getByRole('tab', { name: 'Runner diagnostics' }).click();
  await expect(page.getByRole('switch', { name: 'Follow' })).toBeVisible();
  const titleBefore = await page.title();

  // The payload arrived and was parsed. The counter is the viewer's own tally
  // of lines it took in, and it is the only evidence available in the DOM:
  // the terminal itself draws to a canvas. Without this the assertions below
  // would pass just as well against a viewer that received nothing.
  await expect(page.locator('.lines')).not.toHaveText('0 lines');

  // The title is the application's, not the log's.
  expect(await page.title()).toBe(titleBefore);
  expect(await page.title()).not.toContain('pwned');

  // The page around the terminal was not cleared by the log's clear-screen:
  // the escape belongs to the terminal, and the terminal is not the document.
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
  await expect(page.getByRole('switch', { name: 'Follow' })).toBeVisible();

  expect(dialogs, 'a dialog opened, so something in the output ran').toHaveLength(0);
  expect(page.url(), 'the page went somewhere a log line chose').toContain('/runners/');
});

test('what a pull request, a job and its log say stays text on every Eli repair page', async ({
  page,
}) => {
  const dialogs = watchForDialogs(page);

  // Everything here was written by whoever can open a pull request or edit a
  // workflow: the pull request's title and branch, the job's name and step, the
  // addresses GitHub reported, and the job's log. Each is shaped to run if it is
  // ever parsed, or to be followed if it is ever made a link, and the log line is
  // ten thousand characters with nowhere to break.
  const title = '<img src=x onerror=alert(1)>';
  const step = '<script>alert(2)</script>';
  const address = 'javascript:alert(3)';
  const branch = '"><svg onload=alert(5)>';
  const line = `<svg onload=alert(4)>${'x'.repeat(10_000)}`;
  const kept = keptCase({
    pull_title: title,
    pull_url: address,
    run_url: address,
    author: '<b>dana</b>',
    branch,
    // The job's own address is a real one, because its id is what the log is asked for by;
    // an address that is not one has no id and the page says its log cannot be read.
    jobs: [{ name: title, step, url: 'https://github.com/acme/widgets/actions/runs/482/job/9001' }],
  });
  const attempt = repairRow({
    id: 'rep-x',
    state: 'failed',
    message: '<b>bold</b> message',
    github_login: '<i>mallory</i>',
    details: { stage: 'prepare_fix', failure_code: 'unsupported_fix', case: kept },
  });
  await mockRepairWorld(page, {
    jobs: [
      failedJob({
        job_name: title,
        head_branch: branch,
        html_url: address,
        failed_step: { number: 7, name: step, status: 'completed', conclusion: 'failure' },
      }),
    ],
    offers: [offerRow({ title })],
    readiness: readiness({
      pull: {
        number: 17,
        title,
        author: '<b>dana</b>',
        branch,
        base: '<i>main</i>',
        url: address,
        open: true,
      },
    }),
    repairs: [attempt],
    attempts: { 'rep-x': attempt },
    errorOutput: { '9001': { lines: [line] } },
  });

  /** Nothing was parsed or made a link, and the page still fits the screen. */
  const asText = async (where: string) => {
    await expect(page.locator('img[src="x"]'), `${where}: an img was parsed`).toHaveCount(0);
    await expect(page.locator('main svg[onload]'), `${where}: an svg was parsed`).toHaveCount(0);
    await expect(page.locator('main b, main i'), `${where}: markup was parsed`).toHaveCount(0);
    await expect(
      page.locator('script:has-text("alert(")'),
      `${where}: a script was parsed`,
    ).toHaveCount(0);
    await expect(
      page.locator('a[href^="javascript:" i]'),
      `${where}: a script address became a link`,
    ).toHaveCount(0);
    await expectNoSidewaysScroll(page, where);
  };
  /** The job opened, unless it is the only one and open already, and its log read. */
  const readTheLog = async () => {
    const asked = page.getByRole('region', { name: 'What Eli was asked to fix' });
    if (await asked.count()) {
      const outer = asked.locator('details').first();
      if (!(await outer.evaluate((el) => (el as HTMLDetailsElement).open))) {
        await outer.locator('summary').first().click();
      }
    }
    const job = page.locator('details.job').first();
    if (!(await job.evaluate((el) => (el as HTMLDetailsElement).open)))
      await job.locator('summary').first().click();
    await expect(page.locator('pre').getByText(/^<svg onload=alert\(4\)>x+$/)).toBeVisible();
  };

  // The hub: an offer, and a row of the history. The payloads really arrived.
  await goto(page, '/repairs', 'Eli repairs');
  await expect(
    page.getByRole('region', { name: 'Needs you' }).getByRole('heading', { name: `${title} #17` }),
  ).toBeVisible();
  const history = page.getByRole('region', { name: 'History' });
  const historyDetails = history.locator('details').first();
  await historyDetails.evaluate((el) => {
    (el as HTMLDetailsElement).open = true;
  });
  await expect(history.getByRole('link', { name: title })).toBeVisible();
  await asText('the hub');

  // The repair page, with the log of the job read.
  await goto(page, '/repairs/new?repo=acme%2Fsite&run=9', 'Repair a failed check');
  await expect(page.getByRole('heading', { name: title }).first()).toBeVisible();
  await readTheLog();
  await asText('the repair page');

  // The attempt, with the same job opened.
  await goto(page, '/repairs/rep-x', title);
  await expect(page.getByRole('region', { name: 'Could not repair' })).toContainText(
    '<b>bold</b> message',
  );
  await readTheLog();
  await asText('the attempt');
  // The pull request's and the run's addresses were not web addresses, so the page
  // sends nobody to them. It builds the pull request's link on the job's own address,
  // which was one, and has no run link to build, so it shows none.
  const head = page.locator('.page-header');
  await expect(head.getByRole('link', { name: /Open PR/ })).toHaveAttribute(
    'href',
    'https://github.com/acme/widgets/pull/17',
  );
  await expect(head.getByRole('link', { name: /Open run/ })).toHaveCount(0);

  expect(dialogs, 'a dialog opened, so something a pull request or a job wrote ran').toHaveLength(
    0,
  );
});

// The repository, provider and model named on the automatic repair pages are written by
// people (an installation names a provider, a provider names a model), and the sentence
// that says why a provider cannot pay is built from one of them. Each is text.
test('a repository, provider and model named with markup read as text on the automatic repair pages', async ({
  page,
}) => {
  const dialogs = watchForDialogs(page);
  const repo = 'acme/<img src=x onerror=alert(1)>';
  const provider = '<script>alert(2)</script>';
  const model = '"><svg onload=alert(3)>';
  const problem = `The installation provider ${provider} is disabled <b>so</b> automatic repairs cannot start.`;
  const attempt = repairRow({
    id: 'rep-auto',
    repo,
    // An earlier commit, so the repair page has no attempt of Eli's to point to and says
    // why nothing started, which is the sentence built from the provider's name.
    head_sha: 'f'.repeat(40),
    trigger: 'automatic',
    github_login: '',
    user_id: '',
    state: 'failed',
    details: { stage: 'prepare_fix', case: keptCase() },
  });
  await mockRepairWorld(page, {
    settings: { ...linkedAs('dana'), policies: [automaticPolicy(repo)] },
    readiness: readiness({ repo, policy_mode: 'automatic' }),
    repairs: [attempt],
    attempts: { 'rep-auto': attempt },
    automatic: [
      automaticRow({
        repo,
        provider_name: provider,
        model,
        problem,
        last: { id: 'rep-auto', state: 'failed', pull_number: 17, updated_at: attempt.updated_at },
      }),
    ],
  });

  const asText = async (where: string) => {
    await expect(page.locator('img[src="x"]'), `${where}: an img was parsed`).toHaveCount(0);
    await expect(page.locator('main svg[onload]'), `${where}: an svg was parsed`).toHaveCount(0);
    await expect(page.locator('main b'), `${where}: markup was parsed`).toHaveCount(0);
    await expect(
      page.locator('script:has-text("alert(")'),
      `${where}: a script was parsed`,
    ).toHaveCount(0);
    await expectNoSidewaysScroll(page, where);
  };

  // The hub's panel: the payloads really arrived, as words.
  await goto(page, '/repairs', 'Eli repairs');
  const panel = page.getByRole('region', { name: 'Automatic repairs' });
  await expect(panel).toContainText(provider);
  await expect(panel).toContainText(model);
  await expect(panel).toContainText('<b>so</b>');
  await expect(panel.getByRole('heading', { level: 3 })).toContainText(repo);
  await asText('the automatic repairs panel');

  // The attempt's card.
  await goto(page, '/repairs/rep-auto', /./);
  await expect(page.getByRole('region', { name: `Automatic repairs on ${repo}` })).toBeVisible();
  await asText('the automatic repair card');

  // The repair page's note, which repeats the sentence about the provider.
  await goto(page, `/repairs/new?repo=${encodeURIComponent(repo)}&run=9`, 'Repair a failed check');
  await expect(page.getByRole('complementary', { name: 'Next step' })).toContainText('<b>so</b>');
  await asText('the repair page note');

  expect(dialogs, 'a dialog opened, so something an installation wrote ran').toHaveLength(0);
});
