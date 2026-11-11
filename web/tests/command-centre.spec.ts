/**
 * The command centre: the fleet on one screen, for a second monitor or a wall.
 *
 * It is an immersive route, which is a promise in two halves: the shell's
 * chrome is gone, because the page is its own, and everything an operator
 * reaches for with the keyboard is still there, because somebody who walks up
 * to a wall display still presses Ctrl+K. Leaving it has to work whichever way
 * the page was reached, including when it was the first page the tab opened.
 */
import { expect, test, type Page } from '@playwright/test';
import {
  FIXTURE,
  goto,
  isFocused,
  nav,
  navEntry,
  openSection,
  pageHeading,
  readThrough,
} from './support/fixtures';
import { busyFleet } from './support/command';

function exitLink(page: Page) {
  return page.getByRole('link', { name: 'Exit', exact: true });
}

test('the entry after Overview opens a page of its own, with no navigation around it', async ({
  page,
}) => {
  test.skip(!!test.info().project.use.isMobile, 'the phone lists it in the menu, not the bar');
  await goto(page, '/', 'Overview');

  // Second in the list, directly after the Overview it sits beside.
  const entries = nav(page).getByRole('listitem').locator('a[href]');
  await expect(entries.nth(1)).toHaveAttribute('href', '/command');

  await navEntry(page, '/command').click();
  await expect(page).toHaveURL(/\/command$/);
  await expect(pageHeading(page, 'Command centre')).toBeVisible();
  await expect(page).toHaveTitle('Command centre · Zoomies');
  // The keyboard lands on the page's own heading, as on every other page.
  expect(await isFocused(pageHeading(page, 'Command centre'))).toBe(true);
  await expect(page.getByRole('navigation')).toHaveCount(0);
  await expect(page.getByRole('contentinfo')).toHaveCount(0);
});

test('g d opens it from the Overview, and Exit goes back there', async ({ page }) => {
  await goto(page, '/', 'Overview');
  await page.keyboard.press('g');
  await page.keyboard.press('d');
  await expect(pageHeading(page, 'Command centre')).toBeVisible();

  await exitLink(page).click();
  await expect(pageHeading(page, 'Overview')).toBeVisible();
  await expect(page).toHaveURL(/127\.0\.0\.1:\d+\/$/);
});

test('Exit returns to the page it was opened from, not always the Overview', async ({ page }) => {
  await goto(page, '/', 'Overview');
  await openSection(page, '/runners');
  await expect(pageHeading(page, 'Runners')).toBeVisible();
  await page.keyboard.press('g');
  await page.keyboard.press('d');
  await expect(pageHeading(page, 'Command centre')).toBeVisible();

  await exitLink(page).click();
  await expect(pageHeading(page, 'Runners')).toBeVisible();
});

test('opened directly, Exit and Esc both lead to the Overview', async ({ page }) => {
  await goto(page, '/command', 'Command centre');
  // A real link, so it can be opened in a new tab like any other.
  await expect(exitLink(page)).toHaveAttribute('href', '/');
  await exitLink(page).click();
  await expect(pageHeading(page, 'Overview')).toBeVisible();

  await goto(page, '/command', 'Command centre');
  await page.keyboard.press('Escape');
  await expect(pageHeading(page, 'Overview')).toBeVisible();
});

test('Esc closes an open overlay before it leaves the page', async ({ page }) => {
  await goto(page, '/command', 'Command centre');
  await page.keyboard.press('?');
  const sheet = page.getByRole('dialog', { name: 'Keyboard shortcuts' });
  await expect(sheet).toBeVisible();

  await page.keyboard.press('Escape');
  await expect(sheet).toBeHidden();
  await expect(pageHeading(page, 'Command centre')).toBeVisible();

  await page.keyboard.press('Escape');
  await expect(pageHeading(page, 'Overview')).toBeVisible();
});

test('a browser that cannot go full screen is not offered the control, and Exit still works', async ({
  page,
}) => {
  await page.addInitScript(() => {
    Object.defineProperty(Document.prototype, 'fullscreenEnabled', {
      configurable: true,
      get: () => false,
    });
  });
  await goto(page, '/command', 'Command centre');
  await expect(page.getByRole('button', { name: 'Full screen' })).toHaveCount(0);

  await exitLink(page).click();
  await expect(pageHeading(page, 'Overview')).toBeVisible();
});

test('a browser that can go full screen is offered the control', async ({ page }) => {
  await goto(page, '/command', 'Command centre');
  const control = page.getByRole('button', { name: 'Full screen' });
  // Headless Chromium reports full screen as available; whether it grants it
  // is the browser's business, so this checks the offer, not the result.
  await expect(control).toBeVisible();
  await expect(control).toHaveAttribute('aria-pressed', 'false');
});

/**
 * The reads a refresh of this page must send again: the fleet (its hosts are
 * the lanes), the two lists in the rail, and the chart's hour of samples.
 */
const COMMAND_READS = ['/api/v1/hosts', '/api/v1/provisioning', '/api/v1/jobs', '/api/v1/samples'];

/**
 * Resolves once each of the page's reads has answered for the first time.
 * Made before the page is opened, so no answer can slip past it, and awaited
 * before a refresh is recorded, so a first load's request that went out late
 * cannot be counted as the refresh's.
 */
function firstLoad(page: Page): Promise<unknown> {
  return Promise.all(
    COMMAND_READS.map((path) =>
      page.waitForResponse((response) => new URL(response.url()).pathname === path),
    ),
  );
}

function readsSeen(page: Page): Set<string> {
  const seen = new Set<string>();
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname;
    if (COMMAND_READS.includes(path)) seen.add(path);
  });
  return seen;
}

test('Refresh in the bar fetches the fleet, both lists and the chart again, and the page stays', async ({
  page,
}) => {
  // A wall display is the page most likely to have been left open through a
  // laptop lid or a controller restart, so it owes an operator the same way to
  // settle whether it is current as every other page, in the same words.
  const loaded = firstLoad(page);
  await goto(page, '/command', 'Command centre');
  await expect(page.getByTestId('command-lane').first()).toBeVisible();
  const refresh = page.getByRole('button', { name: 'Refresh', exact: true });
  await expect(refresh).toHaveCount(1);
  await expect(refresh).toBeVisible();

  // Ahead of the page's own controls, as PageHeader puts it everywhere else.
  const first = await refresh.evaluate(
    (el, exit) => Boolean(el.compareDocumentPosition(exit!) & Node.DOCUMENT_POSITION_FOLLOWING),
    await exitLink(page).elementHandle(),
  );
  expect(first, 'refresh comes before Exit').toBe(true);

  await loaded;
  const seen = readsSeen(page);
  await refresh.click();
  await expect.poll(() => [...seen].sort()).toEqual([...COMMAND_READS].sort());
  await expect(refresh).not.toHaveAttribute('aria-busy', 'true');
  await expect(pageHeading(page, 'Command centre')).toBeVisible();
  await expect(page.getByTestId('command-lane').first()).toBeVisible();
});

test('R refreshes the page as the button does', async ({ page }) => {
  const loaded = firstLoad(page);
  await goto(page, '/command', 'Command centre');
  await expect(page.getByTestId('command-lane').first()).toBeVisible();
  await loaded;
  const seen = readsSeen(page);
  await page.keyboard.press('r');
  await expect.poll(() => [...seen].sort()).toEqual([...COMMAND_READS].sort());
  await expect(page).toHaveURL(/\/command$/);
});

test('a refresh in which every read failed says so, and does not claim it refreshed', async ({
  page,
}) => {
  // Each band reports its own failure in place, so one read failing leaves a
  // refresh that happened. Every read failing is a refresh that did not, and
  // a control reading "Refreshed" over it would be the page vouching for
  // numbers it could not fetch.
  await page.route('**/api/v1/events*', (route) => route.abort('connectionfailed'));
  const loaded = firstLoad(page);
  await goto(page, '/command', 'Command centre');
  await loaded;
  const refresh = page.getByRole('button', { name: 'Refresh', exact: true });
  await expect(refresh).toHaveAccessibleDescription(/Fetch this page again/);

  await page.route(
    (url) => COMMAND_READS.includes(url.pathname),
    (route) =>
      route.fulfill({
        status: 500,
        json: { error: { code: 'internal', message: 'the database is locked' } },
      }),
  );
  await refresh.click();
  await expect(page.getByText('This page could not be refreshed')).toBeVisible();
  await expect(refresh).not.toHaveAttribute('aria-busy', 'true');
  await expect(refresh).not.toHaveAccessibleDescription(/^Refreshed /);
  // The bands still say what failed, each in its own place.
  await expect(rail(page, 'Waiting').getByRole('alert')).toBeVisible();
});

test('a queue longer than the read sends is counted in full', async ({ page }) => {
  // The waiting read is capped, so a long queue sends its oldest jobs and
  // reports how many there are. The count and "and N more" are the queue's,
  // not the cap's.
  await busyFleet(page, { hosts: 2, slots: 4, waiting: 12, waitingTotal: 640 });
  await goto(page, '/command', 'Command centre');
  const waiting = rail(page, 'Waiting');
  await expect(waiting).toHaveAttribute('aria-busy', 'false');
  await expect(waiting.getByRole('heading', { level: 2 })).toHaveText('Waiting 640');
  await expect(waiting.getByRole('link', { name: 'and 635 more' })).toHaveAttribute(
    'href',
    /^\/queue/,
  );
});

test('Ctrl+K opens the command palette on the page, and it lists the page', async ({ page }) => {
  await goto(page, '/command', 'Command centre');
  await page.keyboard.press('Control+k');
  const palette = page.getByRole('dialog', { name: 'Command palette' });
  await expect(palette).toBeVisible();
  await expect(palette.getByRole('option', { name: /Command centre/ })).toBeVisible();
});

test('the ring says what is running and opens the running jobs', async ({ page }) => {
  await goto(page, '/command', 'Command centre');
  const ring = page.getByRole('link', { name: /jobs? running, \d+ of \d+ slots? in use/ });
  await expect(ring).toHaveAttribute('href', '/jobs?state=in_progress');
  await ring.click();
  await expect(page).toHaveURL(/\/jobs\?state=in_progress$/);
});

test('the verdict is a heading of its own, with its sentence beside it', async ({ page }) => {
  await goto(page, '/command', 'Command centre');
  const verdict = page.getByRole('heading', {
    level: 2,
    name: /^(Not live|No hosts yet|Needs a look|Busy, with a queue|Busy and healthy|Quiet)\.$/,
  });
  await expect(verdict).toBeVisible();
});

// The sentence carries waits counted by the second and, when the stream is
// down, how long ago the page last heard. A live region over it would have a
// screen reader read the verdict aloud every second; only the headline, which
// changes when the fleet's state does, is announced.
test('the verdict announces its headline only, never the sentence that counts by the second', async ({
  page,
}) => {
  await goto(page, '/command', 'Command centre');
  const hero = page.locator('section[aria-labelledby="command-verdict"]');
  const regions = hero.locator('[aria-live]');
  const headline = hero.getByRole('heading', { level: 2 });
  await expect(regions).toHaveCount(1);
  await expect(regions).toHaveText((await headline.textContent()) ?? '');
  await expect(hero.locator('.sentence[aria-live]')).toHaveCount(0);

  await page.route('**/api/v1/events*', (route) => route.abort('connectionfailed'));
  await page.evaluate(() => window.dispatchEvent(new Event('offline')));
  await expect(headline).toHaveText('Not live.');
  await expect(regions).toHaveCount(1);
  await expect(regions).toHaveText('Not live.');
});

test('a host lane is named by its host, and its header opens that host', async ({ page }) => {
  await goto(page, '/command', 'Command centre');
  const lane = page.getByRole('region', { name: FIXTURE.busyRunnerHost, exact: true });
  await expect(lane).toBeVisible();
  const head = lane.getByRole('link', { name: new RegExp(`^${FIXTURE.busyRunnerHost}`) });
  await expect(head).toHaveAttribute('href', /^\/hosts\/[^/]+$/);
  await head.click();
  await expect(page).toHaveURL(/\/hosts\/[^/]+$/);
});

test('a runner tile opens its runner', async ({ page }) => {
  await goto(page, '/command', 'Command centre');
  const tile = page.locator(`a[href="/runners/${FIXTURE.busyRunnerId}"]`);
  await expect(tile).toHaveCount(1);
  await expect(tile).toHaveAttribute('data-state', 'busy');
  await tile.click();
  await expect(page).toHaveURL(new RegExp(`/runners/${FIXTURE.busyRunnerId}$`));

  // A wall display is visited, not left: Back puts it where it was.
  await page.goBack();
  await expect(page).toHaveURL(/\/command$/);
  await expect(pageHeading(page, 'Command centre')).toBeVisible();
});

test('a free cell counts the free slots on its host and opens the host', async ({ page }) => {
  await goto(page, '/command', 'Command centre');
  const cell = page.getByTestId('command-free').first();
  await expect(cell).toHaveAttribute('href', /^\/hosts\/[^/]+$/);
  await expect(cell).toHaveAccessibleName(/^\d+ free slots? on /);
  await cell.click();
  await expect(page).toHaveURL(/\/hosts\/[^/]+$/);
});

test('the wait and start figures open the queue and the runners', async ({ page }) => {
  for (const [label, path] of [
    ['Median wait', '/queue'],
    ['Runner start', '/runners'],
  ] as const) {
    await goto(page, '/command', 'Command centre');
    const figure = page.getByRole('link', { name: new RegExp(`^${label}`) });
    await expect(figure).toHaveAttribute('href', path);
    await figure.click();
    await expect(page, `${label} leads to ${path}`).toHaveURL(new RegExp(`${path}$`));
  }
});

test('the success share names its window and opens the jobs finished in it', async ({ page }) => {
  await goto(page, '/command', 'Command centre');
  const figure = page.getByRole('link', { name: /^Succeeded, last / });
  await expect(figure).toHaveAttribute('href', /^\/jobs\?state=completed&since=/);
  // The share is over the stats window, which is not a calendar day, so the
  // figure never calls it one. Scoped to the figure: a job or a feed entry
  // elsewhere on the page may say "today" about something else entirely.
  await expect(figure).not.toContainText(/today/i);
});

function rail(page: Page, name: string) {
  return page.getByRole('region', { name: new RegExp(`^${name}`) });
}

test('a waiting job opens the job drawer in place', async ({ page }) => {
  await goto(page, '/command', 'Command centre');
  // The fixture keeps jobs queued for its pools, so the list has rows.
  const row = rail(page, 'Waiting').getByRole('button').first();
  await expect(row).toBeVisible();
  await row.click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await expect(page).toHaveURL(/\/command$/);
});

test('Esc closes an open job drawer and stays, and a second Esc leaves', async ({ page }) => {
  await goto(page, '/command', 'Command centre');
  await rail(page, 'Waiting').getByRole('button').first().click();
  const drawer = page.getByRole('dialog');
  await expect(drawer).toBeVisible();

  await page.keyboard.press('Escape');
  await expect(drawer).toBeHidden();
  await expect(page).toHaveURL(/\/command$/);

  await page.keyboard.press('Escape');
  await expect(pageHeading(page, 'Overview')).toBeVisible();
});

test('a finished job opens the job drawer in place, and its outcome is named', async ({ page }) => {
  // The fixture's jobs finished long before the ten-minute window, so one of
  // them is moved into it. The stream goes first, or a frame could replace it.
  await page.route('**/api/v1/events*', (route) => route.abort('connectionfailed'));
  const finishedRead = (url: URL) =>
    url.pathname === '/api/v1/jobs' && url.searchParams.has('completed_since');
  await page.route(finishedRead, async (route) => {
    const url = new URL(route.request().url());
    url.searchParams.delete('completed_since');
    const read = await readThrough<{ items?: Record<string, unknown>[]; total?: number }>(route, {
      url: url.toString(),
    });
    if (!read) return;
    const job = {
      ...read.body.items![0]!,
      conclusion: 'success',
      completed_at: new Date().toISOString(),
    };
    return route.fulfill({
      response: read.response,
      json: { ...read.body, items: [job], total: 1 },
    });
  });
  await goto(page, '/command', 'Command centre');
  const section = rail(page, 'Just finished');
  const row = section.getByRole('button').first();
  await expect(row).toBeVisible();
  await expect(row.getByRole('img', { name: 'Succeeded' })).toBeVisible();
  await row.click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await expect(page).toHaveURL(/\/command$/);
});

test('Needs attention is absent, not empty, when no problem is open', async ({ page }) => {
  await page.route('**/api/v1/events*', (route) => route.abort('connectionfailed'));
  await page.route('**/api/v1/problems', async (route) => {
    const read = await readThrough<{ items?: unknown[] }>(route);
    if (!read) return;
    return route.fulfill({ response: read.response, json: { ...read.body, items: [] } });
  });
  await goto(page, '/command', 'Command centre');
  await expect(rail(page, 'Waiting')).toBeVisible();
  await expect(rail(page, 'Needs attention')).toHaveCount(0);
});

test('the ticker’s Pause button holds the events still', async ({ page }) => {
  await goto(page, '/command', 'Command centre');
  const ticker = page.getByRole('region', { name: 'Recent events' });
  const pause = ticker.getByRole('button', { name: 'Pause' });
  await expect(pause).toHaveAttribute('aria-pressed', 'false');
  const track = ticker.locator('.track');
  const playState = () => track.evaluate((el) => getComputedStyle(el).animationPlayState);
  // The pointer is moved off the strip first: hovering it pauses it too, and
  // this is about the button.
  await page.mouse.move(0, 0);
  expect(await playState()).toBe('running');

  await pause.click();
  await page.mouse.move(0, 0);
  await expect(pause).toHaveAttribute('aria-pressed', 'true');
  expect(await playState()).toBe('paused');

  await pause.click();
  await page.mouse.move(0, 0);
  await expect(pause).toHaveAttribute('aria-pressed', 'false');
  expect(await playState()).toBe('running');
});

test('the page says it is not live when the stream goes away', async ({ page }) => {
  await goto(page, '/command', 'Command centre');
  await expect(page.getByText('Live', { exact: true })).toBeVisible();
  await page.route('**/api/v1/events*', (route) => route.abort('connectionfailed'));
  await page.evaluate(() => window.dispatchEvent(new Event('offline')));
  await expect(page.getByRole('heading', { name: 'Not live.' })).toBeVisible();
  await expect(page.getByText('Reconnecting')).toBeVisible();
  // Nothing on the page goes on moving as if the work were still vouched for,
  // the ticker included: its events have stopped arriving too.
  await expect(page.locator('[data-live="false"]')).toHaveCount(1);
  await page.mouse.move(0, 0);
  const track = page.getByRole('region', { name: 'Recent events' }).locator('.track');
  await expect(track).toHaveCount(1);
  expect(await track.evaluate((el) => getComputedStyle(el).animationPlayState)).toBe('paused');
});

test('when the stream drops, the page counts from the last thing it heard, not from noticing', async ({
  page,
}) => {
  // A stream can fall silent long before anything notices: a proxy holding a
  // dead socket is only found out by the watchdog. Counting "last heard" from
  // the moment the drop was noticed would understate how stale the page is by
  // exactly that silence. The clock is moved on while nothing arrives, so the
  // two moments are ten minutes apart.
  await page.clock.install();
  await goto(page, '/command', 'Command centre');
  await expect(page.getByText('Live', { exact: true })).toBeVisible();
  await page.clock.setSystemTime(Date.now() + 10 * 60_000);
  await page.route('**/api/v1/events*', (route) => route.abort('connectionfailed'));
  await page.evaluate(() => window.dispatchEvent(new Event('offline')));
  await expect(page.getByRole('heading', { name: 'Not live.' })).toBeVisible();
  // The page's ten-second clock is what the sentence counts on.
  await page.clock.runFor(11_000);
  const hero = page.locator('section[aria-labelledby="command-verdict"]');
  await expect(hero.locator('.sentence')).toContainText(/Last heard from the controller 10m/);
});

test('while the jobs are still loading the rail shows their shape and the deck is drawn', async ({
  page,
}) => {
  // Held, never answered: the page has to stand on what it has.
  await page.route('**/api/v1/jobs*', () => undefined);
  await goto(page, '/command', 'Command centre');
  await expect(page.getByTestId('command-lane').first()).toBeVisible();
  await expect(page.getByTestId('command-rail-skeleton')).toHaveCount(2);
  await expect(rail(page, 'Waiting')).toHaveAttribute('aria-busy', 'true');
  // A list nobody has read yet is not an empty one.
  await expect(page.getByText('Nothing is waiting.')).toHaveCount(0);
});

test('while the fleet is still loading the hero and the deck show their shape, not a verdict', async ({
  page,
}) => {
  await page.route('**/api/v1/events*', (route) => route.abort('connectionfailed'));
  await page.route('**/api/v1/hosts*', () => undefined);
  await goto(page, '/command', 'Command centre');
  await expect(page.getByTestId('command-deck')).toHaveAttribute('aria-busy', 'true');
  await expect(page.getByTestId('command-lane')).toHaveCount(0);
  // "No hosts yet." about a fleet nobody has read would be a claim.
  await expect(page.getByRole('heading', { level: 2, name: 'Loading the fleet.' })).toBeAttached();
  await expect(page.getByRole('heading', { name: 'No hosts yet.' })).toHaveCount(0);
});

test('a fleet that does not load says so where the hosts go, and tries again', async ({ page }) => {
  await page.route('**/api/v1/events*', (route) => route.abort('connectionfailed'));
  let failing = true;
  await page.route('**/api/v1/hosts*', (route) =>
    failing
      ? route.fulfill({
          status: 500,
          json: { error: { code: 'internal', message: 'the database is locked' } },
        })
      : route.fallback(),
  );
  await goto(page, '/command', 'Command centre');
  const deck = page.getByTestId('command-deck');
  await expect(deck.getByText('The hosts did not load.')).toBeVisible();
  // The rail reads the jobs, which answered.
  await expect(rail(page, 'Waiting').getByRole('button').first()).toBeVisible();

  failing = false;
  await deck.getByRole('button', { name: 'Try again' }).click();
  await expect(page.getByTestId('command-lane').first()).toBeVisible();
});

test('a read that fails says so in its band, offers to try again, and leaves the rest', async ({
  page,
}) => {
  await page.route('**/api/v1/events*', (route) => route.abort('connectionfailed'));
  let failing = true;
  await page.route('**/api/v1/jobs*', (route) =>
    failing
      ? route.fulfill({
          status: 500,
          json: { error: { code: 'internal', message: 'the database is locked' } },
        })
      : route.fallback(),
  );
  await goto(page, '/command', 'Command centre');
  const waiting = rail(page, 'Waiting');
  await expect(waiting.getByRole('alert')).toBeVisible();
  const retry = waiting.getByRole('button', { name: 'Try again' });
  await expect(retry).toBeVisible();
  // The hero and the deck read the fleet, which answered.
  await expect(page.getByRole('heading', { level: 2, name: /\.$/ }).first()).toBeVisible();
  await expect(page.getByTestId('command-lane').first()).toBeVisible();

  failing = false;
  await retry.click();
  await expect(waiting.getByRole('alert')).toHaveCount(0);
  await expect(waiting.getByRole('button').first()).toBeVisible();
});

test('forty hosts are forty lanes, and the deck scrolls to hold them', async ({ page }) => {
  await page.route('**/api/v1/events*', (route) => route.abort('connectionfailed'));
  await page.route('**/api/v1/hosts*', async (route) => {
    const read = await readThrough<{ items?: Record<string, unknown>[] }>(route);
    if (!read) return;
    const template = read.body.items![0]!;
    const items = Array.from({ length: 40 }, (_, i) => ({
      ...template,
      id: `hst_crowd${String(i).padStart(2, '0')}`,
      name: `crowd-${String(i).padStart(2, '0')}`,
      slots: 5,
      capacity: 5,
      healthy: true,
      retired: false,
      cordoned: false,
    }));
    return route.fulfill({ response: read.response, json: { ...read.body, items } });
  });
  // Two busy runners on every host, so the deck at this size is tiles as well
  // as free cells: the case where a long name and a narrow cell meet forty times.
  await page.route('**/api/v1/runners*', async (route) => {
    const read = await readThrough<{ items?: Record<string, unknown>[] }>(route);
    if (!read) return;
    const busy = read.body.items!.find((runner) => runner.state === 'busy' && runner.current_job)!;
    const items = Array.from({ length: 40 }, (_, i) => i).flatMap((i) =>
      ['a', 'b'].map((slot) => ({
        ...busy,
        id: `run_crowd${String(i).padStart(2, '0')}${slot}`,
        name: `crowd-${String(i).padStart(2, '0')}-${slot}`,
        host_id: `hst_crowd${String(i).padStart(2, '0')}`,
        state: 'busy',
      })),
    );
    return route.fulfill({
      response: read.response,
      json: { ...read.body, items, total: items.length },
    });
  });
  await goto(page, '/command', 'Command centre');
  const lanes = page.getByTestId('command-lane');
  await expect(lanes).toHaveCount(40);
  await expect(page.getByTestId('command-tile')).toHaveCount(80);
  // One free cell for each host, counting its three free slots, rather than a
  // dashed box for every one of them.
  const free = page.getByTestId('command-free');
  await expect(free).toHaveCount(40);
  for (const text of await free.locator('.title').allTextContents()) {
    expect(text.trim()).toBe('3 free slots');
  }
  const phone = !!test.info().project.use.isMobile;
  const sizes = await page.evaluate(() => {
    const deck = document.querySelector<HTMLElement>('[data-testid="command-deck"]')!;
    return {
      scroll: document.documentElement.scrollHeight,
      viewport: document.documentElement.clientHeight,
      deckScroll: deck.scrollHeight,
      deckClient: deck.clientHeight,
      flat: Array.from(document.querySelectorAll('[data-testid="command-lane"]')).filter(
        (lane) => lane.getBoundingClientRect().height === 0,
      ).length,
      // A tile wider than the cell the lane's grid gave it, or than its own box.
      wide: Array.from(document.querySelectorAll('[data-testid="command-tile"]')).filter((tile) => {
        // The lane's grid, not the parent: the tile's tooltip wrapper takes no box.
        const cells = tile.closest('.cells')!.getBoundingClientRect();
        const box = tile.getBoundingClientRect();
        return tile.scrollWidth > tile.clientWidth || box.right > cells.right + 0.5;
      }).length,
    };
  });
  if (phone) {
    // Stacked, the page is a document and grows downwards as it always did.
    expect(sizes.scroll, 'the phone grows downwards rather than cutting off').toBeGreaterThan(
      sizes.viewport,
    );
  } else {
    // The page is the window; the deck takes the fleet's height inside itself.
    expect(sizes.scroll, 'the document grew with the fleet').toBe(sizes.viewport);
    expect(sizes.deckScroll, 'the deck holds more than it shows').toBeGreaterThan(sizes.deckClient);
  }
  expect(sizes.flat, 'a lane drawn with no height').toBe(0);
  expect(sizes.wide, 'a tile wider than its cell').toBe(0);
  // Nothing past the fold is out of reach: the last lane scrolls into view.
  await lanes.last().scrollIntoViewIfNeeded();
  await expect(lanes.last()).toBeInViewport();
});

/*
 * A wall display that scrolls is not on one screen. The owner's report: a
 * fleet of five large hosts and a queue of twelve, on a 1920 by 1080 screen,
 * pushed the ticker off the bottom and scrolled the hero away. The page is
 * the window: the bar and the hero at the top, the ticker at the foot, and
 * the deck and the rail sharing what is left, each scrolling in itself.
 */
for (const [width, height] of [
  [1920, 1080],
  [1440, 900],
] as const) {
  test(`at ${width} by ${height} a busy fleet fits the window, hero and ticker included`, async ({
    page,
  }) => {
    test.skip(!!test.info().project.use.isMobile, 'a phone stacks the page and scrolls it');
    await page.setViewportSize({ width, height });
    await busyFleet(page, { hosts: 5, slots: 11, waiting: 12 });
    await goto(page, '/command', 'Command centre');
    await expect(page.getByTestId('command-lane')).toHaveCount(5);
    await expect(rail(page, 'Waiting')).toHaveAttribute('aria-busy', 'false');
    await expect(rail(page, 'Needs attention')).toBeVisible();

    const fit = await page.evaluate(() => {
      const box = (el: Element | null | undefined) => {
        const r = el!.getBoundingClientRect();
        return { top: r.top, bottom: r.bottom, height: r.height };
      };
      return {
        scroll: document.documentElement.scrollHeight,
        client: document.documentElement.clientHeight,
        hero: box(document.getElementById('command-verdict')?.closest('section')),
        ticker: box(document.getElementById('command-ticker')?.closest('section')),
      };
    });
    expect(fit.scroll, 'the document is taller than the window').toBe(fit.client);
    for (const [name, box] of [
      ['the hero', fit.hero],
      ['the ticker', fit.ticker],
    ] as const) {
      expect(box.height, `${name} has a height`).toBeGreaterThan(0);
      expect(box.top, `${name} starts inside the window`).toBeGreaterThanOrEqual(0);
      expect(box.bottom, `${name} ends inside the window`).toBeLessThanOrEqual(fit.client);
    }

    // At 1440 by 900 this fleet's rail is longer than its share of the window,
    // so it scrolls in itself, and its last list can be reached.
    if (width === 1440) {
      const finished = rail(page, 'Just finished');
      const scroller = await finished.evaluate((el) => {
        let node = el.parentElement;
        while (node && getComputedStyle(node).overflowY === 'visible') node = node.parentElement;
        return node ? { scroll: node.scrollHeight, client: node.clientHeight } : null;
      });
      expect(scroller, 'the rail scrolls in a container of its own').not.toBeNull();
      expect(scroller!.scroll, 'the rail holds more than it shows').toBeGreaterThan(
        scroller!.client,
      );
      const heading = finished.getByRole('heading', { level: 2 });
      await heading.scrollIntoViewIfNeeded();
      await expect(heading).toBeInViewport();
    }
  });
}

/*
 * A window too short to fit the bar, the hero, a usable deck and the ticker
 * gives up fitting and scrolls, and the ticker stays below the deck rather
 * than under it: the deck is placed from its frame, so a frame that spilled
 * past its share of the page would paint the lanes over the ticker.
 */
test('in a short window the deck stops above the ticker instead of covering it', async ({
  page,
}) => {
  test.skip(!!test.info().project.use.isMobile, 'a phone stacks the page and scrolls it');
  await page.setViewportSize({ width: 1024, height: 560 });
  await busyFleet(page, { hosts: 5, slots: 11, waiting: 12 });
  await goto(page, '/command', 'Command centre');
  await expect(page.getByTestId('command-lane')).toHaveCount(5);
  const boxes = await page.evaluate(() => {
    const box = (el: Element | null | undefined) => {
      const r = el!.getBoundingClientRect();
      return { top: r.top, bottom: r.bottom, left: r.left, right: r.right };
    };
    return {
      deck: box(document.querySelector('[data-testid="command-deck"]')),
      ticker: box(document.getElementById('command-ticker')?.closest('section')),
    };
  });
  const { deck, ticker } = boxes;
  const overlap =
    deck.left < ticker.right &&
    ticker.left < deck.right &&
    deck.top < ticker.bottom &&
    ticker.top < deck.bottom;
  expect(
    overlap,
    `the deck at ${JSON.stringify(deck)} and the ticker at ${JSON.stringify(ticker)}`,
  ).toBe(false);
});

test('on a phone the page still scrolls as a document, and never sideways', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 });
  await busyFleet(page, { hosts: 5, slots: 11, waiting: 12 });
  await goto(page, '/command', 'Command centre');
  await expect(page.getByTestId('command-lane')).toHaveCount(5);
  const sizes = await page.evaluate(() => ({
    scrollHeight: document.documentElement.scrollHeight,
    clientHeight: document.documentElement.clientHeight,
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
  expect(sizes.scrollHeight, 'the stacked page scrolls').toBeGreaterThan(sizes.clientHeight);
  expect(sizes.scrollWidth, 'the page scrolls sideways').toBeLessThanOrEqual(sizes.clientWidth);
});

for (const style of ['classic', 'mission-control']) {
  for (const mode of ['light', 'dark']) {
    test(`${style} ${mode} draws the page with no error`, async ({ page }) => {
      await page.addInitScript(
        ({ style, mode }) => {
          localStorage.setItem('zoomies.ui-style', style);
          localStorage.setItem('zoomies.theme', mode);
        },
        { style, mode },
      );
      const errors: string[] = [];
      page.on('pageerror', (error) => errors.push(error.message));
      await goto(page, '/command', 'Command centre');
      await expect(page.getByTestId('command-ring')).toBeVisible();
      await expect(page.getByTestId('command-lane').first()).toBeVisible();
      await expect(rail(page, 'Waiting')).toHaveAttribute('aria-busy', 'false');
      expect(errors).toEqual([]);
    });
  }
}

test("a public view's visitor is told what the page is, without the shell's notice", async ({
  page,
}) => {
  await page.route('**/api/v1/meta', async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    read.body.auth_disabled = false;
    read.body.bootstrap_required = false;
    read.body.public_view = true;
    await route.fulfill({ response: read.response, json: read.body });
  });
  await page.route('**/api/v1/auth/session', async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    read.body.role = 'viewer';
    read.body.kind = 'public';
    await route.fulfill({ response: read.response, json: read.body });
  });
  await goto(page, '/command', 'Command centre');
  const note = page.getByRole('note');
  await expect(note).toContainText('Public, read-only view');
  const signIn = note.getByRole('link', { name: 'Sign in' });
  await expect(signIn).toHaveAttribute('href', '/login');
  await signIn.click();
  await expect(page).toHaveURL(/\/login$/);
});

/*
 * Every item on the page says more about itself on hover and on focus, in the
 * shared tooltip's structured card. The card is for the eye: the item stays
 * one link or one button, a finger's tap still does what it did, and what a
 * workflow names is text in the card, never markup.
 */
const MARKUP = '<img src=x onerror=window.__x=1>';

function card(page: Page) {
  return page.locator('.bubble:popover-open');
}

async function busyPage(page: Page): Promise<void> {
  await busyFleet(page, { hosts: 2, slots: 4, busy: 2, waiting: 6, jobName: MARKUP });
  await goto(page, '/command', 'Command centre');
  await expect(rail(page, 'Waiting')).toHaveAttribute('aria-busy', 'false');
  await expect(page.getByTestId('command-tile').first()).toBeVisible();
}

test('hovering a runner tile shows its card, and the keyboard opens and Esc closes it', async ({
  page,
}) => {
  test.skip(!!test.info().project.use.isMobile, 'a phone has no hover; see the tap test');
  await busyPage(page);
  const tile = page.getByTestId('command-tile').nth(1);
  await tile.hover();
  await expect(card(page)).toBeVisible();
  await expect(card(page)).toContainText('busy-00-01');
  await expect(card(page)).toContainText('Pool');
  await expect(card(page).locator('dl')).toBeVisible();

  // Away and back by the keyboard: focus opens it at once, Esc puts it away
  // and leaves focus where it was.
  await page.mouse.move(0, 0);
  await expect(card(page)).toHaveCount(0);
  await tile.focus();
  await page.keyboard.press('Shift+Tab');
  await page.keyboard.press('Tab');
  await expect(tile).toBeFocused();
  await expect(card(page)).toContainText('busy-00-01');
  await page.keyboard.press('Escape');
  await expect(card(page)).toHaveCount(0);
  await expect(tile).toBeFocused();
  await expect(page).toHaveURL(/\/command$/);
});

test('the free cell says what limits it, and a waiting row says why it waits', async ({ page }) => {
  test.skip(!!test.info().project.use.isMobile, 'a phone has no hover');
  await busyPage(page);
  await page.getByTestId('command-free').first().hover();
  await expect(card(page)).toContainText('2 free slots');
  await expect(card(page)).toContainText('Limited by');

  await rail(page, 'Waiting').getByRole('button').nth(1).hover();
  await expect(card(page)).toContainText('waiting job 01');
  await expect(card(page)).toContainText('Why');
});

test('a job named with markup is text in its card, and runs nothing', async ({ page }) => {
  test.skip(!!test.info().project.use.isMobile, 'a phone has no hover');
  await busyPage(page);
  for (const item of [
    page.getByTestId('command-tile').first(),
    rail(page, 'Waiting').getByRole('button').first(),
  ]) {
    await item.hover();
    await expect(card(page).getByRole('heading', { includeHidden: true })).toHaveText(MARKUP);
    await expect(card(page).locator('img')).toHaveCount(0);
    await page.mouse.move(0, 0);
    await expect(card(page)).toHaveCount(0);
  }
  expect(await page.evaluate(() => (window as { __x?: unknown }).__x)).toBeUndefined();
});

for (const [width, height] of [
  [1440, 900],
  [375, 812],
] as const) {
  test(`at ${width} wide a card at the end of a lane or the rail stays inside the window`, async ({
    page,
  }) => {
    test.skip(!!test.info().project.use.isMobile, 'a phone has no hover');
    await page.setViewportSize({ width, height });
    await busyPage(page);
    const lane = page.getByTestId('command-lane').last();
    for (const item of [
      lane.getByTestId('command-tile').last(),
      rail(page, 'Waiting').getByRole('button').last(),
      rail(page, 'Just finished').getByRole('button').last(),
    ]) {
      if ((await item.count()) === 0) continue;
      await item.scrollIntoViewIfNeeded();
      await item.hover();
      await expect(card(page)).toBeVisible();
      const box = (await card(page).boundingBox())!;
      expect(box.x, 'left edge').toBeGreaterThanOrEqual(0);
      expect(box.y, 'top edge').toBeGreaterThanOrEqual(0);
      expect(box.x + box.width, 'right edge').toBeLessThanOrEqual(width);
      expect(box.y + box.height, 'bottom edge').toBeLessThanOrEqual(height);
      const wider = await page.evaluate(
        () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
      );
      expect(wider, 'the card widened the page').toBe(false);
      await page.mouse.move(0, 0);
      await expect(card(page)).toHaveCount(0);
    }
  });
}

test('on a touch screen a tap on a tile follows its link with no card in the way', async ({
  page,
}) => {
  test.skip(!test.info().project.use.isMobile, 'the tap is the phone');
  await busyPage(page);
  const tile = page.getByTestId('command-tile').first();
  await tile.scrollIntoViewIfNeeded();
  await tile.tap();
  await expect(page).toHaveURL(/\/runners\/[^/]+$/);
});

// A card that ticks is worked out only while it is open. What a screen reader
// is given is the steady part, so with no bubble open a second passing writes
// nothing into the page for any tile.
test('a tile description is short and does not change while the clock runs', async ({ page }) => {
  await busyPage(page);
  const tile = page.getByTestId('command-tile').first();
  const described = () =>
    tile.evaluate((el) => {
      const ids = (el.getAttribute('aria-describedby') ?? '').split(/\s+/).filter(Boolean);
      return ids.map((id) => document.getElementById(id)?.textContent ?? '').join(' ');
    });
  const before = await described();
  expect(before).toContain('Runner: busy-00-00');
  expect(before).toContain('Pool:');
  expect(before).not.toMatch(/Running for|Started|Queued|When|Image/);
  await page.waitForTimeout(2200);
  expect(await described()).toBe(before);
});
