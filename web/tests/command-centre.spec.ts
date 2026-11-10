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
});

test('the success share names its window and opens the jobs finished in it', async ({ page }) => {
  await goto(page, '/command', 'Command centre');
  const figure = page.getByRole('link', { name: /^Succeeded, last / });
  await expect(figure).toHaveAttribute('href', /^\/jobs\?state=completed&since=/);
  await expect(page.getByText(/today/i)).toHaveCount(0);
});
