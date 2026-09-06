/**
 * The log viewer.
 *
 * The most intricate component in the product and, until this file, the one
 * with no test at all: a terminal, an event stream, a search over what has
 * arrived, and a follow mode that has to know the difference between "the
 * operator scrolled up to read something" and "there is nothing new".
 *
 * The seeded fixture has no agent behind it, so no line is ever relayed. That
 * turns out to be the state most worth protecting: the viewer has to say it is
 * waiting rather than claim the runner produced nothing, has to offer the whole
 * log on the host rather than only the lines on screen, and has to refuse
 * honestly for a runner whose host can no longer be asked. Feeding synthetic
 * lines through the stream is deliberately not done here -- a mocked
 * text/event-stream is a different object from the relayed one, and a test that
 * passes against the mock proves nothing about the relay.
 */
import { expect, test, type Page } from '@playwright/test';
import { browserOverride, FIXTURE, goto } from './support/fixtures';

test.use(browserOverride);

const follow = (page: Page) => page.getByRole('switch', { name: 'Follow' });
const wrap = (page: Page) => page.getByRole('switch', { name: 'Wrap' });
const search = (page: Page) => page.getByRole('searchbox', { name: 'Search the log' });

/** Wait for the dynamic xterm import to land and the toolbar to exist. */
async function booted(page: Page): Promise<void> {
  await expect(follow(page)).toBeVisible();
}

async function openLog(page: Page): Promise<void> {
  await goto(page, `/runners/${FIXTURE.busyRunnerId}`, FIXTURE.busyRunner);
  await expect(page.getByRole('heading', { name: 'Log', exact: true })).toBeVisible();
  await booted(page);
}

test('with nothing relayed it says it is waiting, not that there was nothing', async ({ page }) => {
  await openLog(page);

  // The difference matters: "nothing has been written yet" is a runner that is
  // still starting, "this runner produced no output" is a finished job that
  // said nothing, and the viewer must not claim the second while waiting for
  // the first.
  await expect(page.getByText('Waiting for output. Nothing has been written yet.')).toBeVisible();
  await expect(page.getByText('This runner produced no output.')).toHaveCount(0);

  // The terminal is a named region rather than an anonymous box of text.
  await expect(
    page.getByRole('group', { name: `Log output for ${FIXTURE.busyRunner}` }),
  ).toBeVisible();
});

test('follow and wrap are switches, and turning follow off offers the way back', async ({
  page,
}) => {
  await openLog(page);

  // Both on by default: an operator opening a log wants the newest line, and
  // does not want to lose the right-hand end of it.
  await expect(follow(page)).toBeChecked();
  await expect(wrap(page)).toBeChecked();

  await follow(page).click();
  await expect(follow(page)).not.toBeChecked();
  // Turning follow off is what puts the jump control on screen. Without it,
  // scrolling up to read something is a trap.
  // It is asserted rather than pressed: with no lines relayed the "waiting for
  // output" overlay is on top of it, which is right -- there is nothing to
  // jump to -- and pressing it here would be testing the overlay.
  const jump = page.getByRole('button', { name: /^Jump to latest/ });
  await expect(jump).toBeVisible();

  await follow(page).click();
  await expect(follow(page)).toBeChecked();
  await expect(jump).toBeHidden();

  await wrap(page).click();
  await expect(wrap(page)).not.toBeChecked();
});

test('search answers honestly when there is nothing to search', async ({ page }) => {
  await openLog(page);

  // Both step buttons are dead until there is a query, and the count says "no
  // matches" rather than staying blank -- "is this error in here?" needs an
  // answer, and no is an answer.
  await expect(page.getByRole('button', { name: 'Next match' })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Previous match' })).toBeDisabled();

  await search(page).fill('npm ERR!');
  await expect(page.getByText('No matches')).toBeVisible();
});

test('clearing empties the screen and promises the host still has the log', async ({ page }) => {
  await openLog(page);

  await page.getByRole('button', { name: 'Clear' }).click();
  await expect(
    page.getByText('Cleared. Anything the runner writes from now on appears here.'),
  ).toBeVisible();

  // The wording is the promise, and the download beside it is what makes the
  // promise checkable: it fetches the log on the host, not the lines that were
  // on screen.
  await expect(page.getByRole('link', { name: 'Download' })).toHaveAttribute(
    'href',
    new RegExp(`/runners/${FIXTURE.busyRunnerId}/logs/download$`),
  );
});

test('a runner that is gone says the log cannot be read, rather than waiting for ever', async ({
  page,
}) => {
  // run_demo10 is seeded as removed, so its host can no longer be asked.
  await page.goto('/runners/run_demo10', { waitUntil: 'domcontentloaded' });
  await expect(page.getByText('The log cannot be read right now')).toBeVisible();
  await expect(follow(page), 'no toolbar for a stream that cannot open').toHaveCount(0);
});
