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

test('a free cell opens its host', async ({ page }) => {
  await goto(page, '/command', 'Command centre');
  const cell = page.getByTestId('command-free').first();
  await expect(cell).toHaveAttribute('href', /^\/hosts\/[^/]+$/);
  await expect(cell).toHaveAccessibleName(/^Free slot on /);
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

test('forty hosts are forty lanes, and the page grows to hold them', async ({ page }) => {
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
  await page.route('**/api/v1/runners*', async (route) => {
    const read = await readThrough<{ items?: unknown[] }>(route);
    if (!read) return;
    return route.fulfill({
      response: read.response,
      json: { ...read.body, items: [], total: 0 },
    });
  });
  await goto(page, '/command', 'Command centre');
  const lanes = page.getByTestId('command-lane');
  await expect(lanes).toHaveCount(40);
  await expect(page.getByTestId('command-free')).toHaveCount(200);
  const sizes = await page.evaluate(() => ({
    scroll: document.documentElement.scrollHeight,
    viewport: innerHeight,
    flat: Array.from(document.querySelectorAll('[data-testid="command-lane"]')).filter(
      (lane) => lane.getBoundingClientRect().height === 0,
    ).length,
  }));
  expect(sizes.scroll, 'the deck grows downwards rather than being cut off').toBeGreaterThan(
    sizes.viewport,
  );
  expect(sizes.flat, 'a lane drawn with no height').toBe(0);
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
  await expect(note.getByRole('button', { name: 'Sign in' })).toBeVisible();
});
