/**
 * The audit log.
 *
 * This is the page somebody opens when the question is "who did that, and
 * when?" -- after an incident, or during a review. Two things make it worth
 * having: every filter is in the address bar, so an answer can be sent to
 * somebody else rather than described; and it names the actor as a person, a
 * token or the controller itself, because "the pool was disabled" means
 * different things depending on which.
 *
 * The whole page was untested until this file.
 */
import { expect, test, type Page } from '@playwright/test';
import { browserOverride, dataRows, goto, grid } from './support/fixtures';

test.use(browserOverride);

const log = (page: Page) => grid(page, 'Audit log');
const rows = (page: Page) => dataRows(log(page));

/**
 * What the seed writes, and therefore what an assertion may rely on.
 *
 * Counts are deliberately not asserted: the audit log is the one table every
 * other spec writes to just by doing its job, so "exactly five rows" would be
 * a test of what ran before it rather than of this page.
 */
const SEEDED = [
  { actor: 'alice', action: 'pool.create' },
  { actor: 'alice', action: 'installation.create' },
  { actor: 'bob', action: 'runner.drain' },
  { actor: 'ci-bot', action: 'pool.update' },
  { actor: 'zoomies', action: 'host.join' },
] as const;

test('the log lists what was done, by whom, newest first', async ({ page }) => {
  await goto(page, '/audit', 'Audit');
  await expect(rows(page).first()).toBeVisible();

  for (const entry of SEEDED) {
    await expect(
      rows(page).filter({ hasText: entry.action }).filter({ hasText: entry.actor }),
      `${entry.actor} did ${entry.action}`,
    ).toHaveCount(1);
  }

  // An actor is a person, a token or the controller, and the page says which.
  await expect(rows(page).filter({ hasText: 'ci-bot' })).toContainText('token');
  await expect(rows(page).filter({ hasText: 'zoomies' })).toContainText('system');
});

test('the newest change is at the top, and the order can be turned round', async ({ page }) => {
  await goto(page, '/audit', 'Audit');
  await expect(rows(page).first()).toBeVisible();

  // Newest first is the default: the question this page answers is usually
  // "what just happened?".
  const header = log(page).getByRole('columnheader', { name: 'When' });
  const when = header.getByRole('button');
  await expect(header).toHaveAttribute('aria-sort', 'descending');

  await when.click();
  await expect(log(page).getByRole('columnheader', { name: 'When' })).toHaveAttribute(
    'aria-sort',
    'ascending',
  );
  // The oldest seeded event is over four hours old and nothing in the suite
  // writes anything older, so it is the first row whichever way round it goes.
  await expect(rows(page).first()).toContainText('installation.create');
});

test('filtering by action narrows the log and the address bar carries it', async ({ page }) => {
  await goto(page, '/audit', 'Audit');
  // Counted after the grid has settled, not before: `count()` takes one look
  // and a skeleton has no rows.
  await expect(rows(page).first()).toBeVisible();
  const before = await rows(page).count();
  expect(before).toBeGreaterThanOrEqual(SEEDED.length);

  await page.getByRole('combobox', { name: 'Filter by action' }).selectOption('pool.create');
  await expect(rows(page)).toHaveCount(1);
  await expect(rows(page).first()).toContainText('alice');
  await expect(page).toHaveURL(/action=pool\.create/);

  // The filter is a chip, and removing it puts everything back.
  const chips = page.getByRole('group', { name: 'Filters in effect' });
  await chips.getByRole('button', { name: /Remove the Action filter/ }).click();
  await expect(rows(page)).toHaveCount(before);
  await expect(page).not.toHaveURL(/action=/);
});

test('a filtered log is a link somebody else can open', async ({ page }) => {
  // Straight to the address rather than through the controls: this is the case
  // that matters, a filtered view pasted into a chat window.
  await goto(page, '/audit?target_kind=pool', 'Audit');

  // What is in it rather than how many: every spec that creates a pool adds a
  // row here, so a count would be a test of what ran first.
  await expect(rows(page).filter({ hasText: 'pool.create' }).first()).toBeVisible();
  await expect(rows(page).filter({ hasText: 'pool.update' }).first()).toBeVisible();
  await expect(rows(page).filter({ hasText: 'host.join' }), 'a host is not a pool').toHaveCount(0);
  await expect(
    rows(page).filter({ hasText: 'installation.create' }),
    'nor is an installation',
  ).toHaveCount(0);
  await expect(
    page.getByRole('combobox', { name: 'Filter by the kind of thing acted on' }),
  ).toHaveValue('pool');
});

test('a search that matches nothing settles on an empty state that offers a way out', async ({
  page,
}) => {
  await goto(page, '/audit', 'Audit');
  await page.getByRole('searchbox', { name: 'Search the audit log' }).fill('nothing-did-this');

  await expect(rows(page)).toHaveCount(0);
  // The empty state, not a skeleton that never resolves.
  await expect(page.getByRole('button', { name: 'Clear filters' })).toBeVisible();
  await page.getByRole('button', { name: 'Clear filters' }).click();
  await expect(rows(page).first()).toBeVisible();
});

test('an entry opens and says what changed', async ({ page }) => {
  await goto(page, '/audit', 'Audit');
  await rows(page).filter({ hasText: 'pool.create' }).click();

  const drawer = page.getByRole('dialog');
  await expect(drawer).toBeVisible();
  await expect(drawer).toContainText('pool.create');
  await expect(drawer).toContainText('alice');
  await expect(drawer.getByRole('heading', { name: 'What changed' })).toBeVisible();

  await page.keyboard.press('Escape');
  await expect(drawer).toBeHidden();
});
