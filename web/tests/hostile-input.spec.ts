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
import { browserOverride, FIXTURE, goto } from './support/fixtures';

test.use(browserOverride);

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
    const response = await route.fetch();
    const body = await response.json();
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
