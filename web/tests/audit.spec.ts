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
import { dataRows, goto, grid } from './support/fixtures';

const log = (page: Page) => grid(page, 'Audit log');
const rows = (page: Page) => dataRows(log(page));
const kindChips = (page: Page) =>
  page.getByRole('group', { name: 'Filter by the kind of thing acted on' });

/**
 * What the seed writes, and therefore what an assertion may rely on.
 *
 * Counts are deliberately not asserted: the audit log is the one table every
 * other spec writes to just by doing its job, so "exactly five rows" would be
 * a test of what ran before it rather than of this page.
 */
const SEEDED = [
  { actor: 'alice', action: 'pool.create', kind: 'user' },
  { actor: 'alice', action: 'installation.create', kind: 'user' },
  { actor: 'bob', action: 'runner.drain', kind: 'user' },
  { actor: 'ci-bot', action: 'pool.update', kind: 'token' },
  { actor: 'zoomies', action: 'host.join', kind: 'system' },
] as const;

test('the log lists what was done, by whom, newest first', async ({ page }) => {
  await goto(page, '/audit', 'Audit');
  await expect(rows(page).first()).toBeVisible();

  // Each seeded row is read through the page's own action filter rather than
  // off the first page of the whole log. The seed dates these rows hours in
  // the past, the log is newest first and one page long, and every other spec
  // writes to it just by doing its job -- so whether a seeded row is on page
  // one is a fact about how many specs ran before this one, not about the page.
  for (const entry of SEEDED) {
    await goto(page, `/audit?action=${entry.action}`, 'Audit');
    const seeded = rows(page).filter({ hasText: entry.action }).filter({ hasText: entry.actor });
    if (entry.kind === 'system') {
      // The controller's rows are taken one at a time: the hosts specs leave
      // system rows behind -- a host throttled after the pressure they
      // simulate is audited as the fleet's own decision -- and the claim here
      // is that the seeded one is there, not that the fleet decided exactly
      // one thing.
      await expect(seeded.first(), `${entry.actor} did ${entry.action}`).toBeVisible();
    } else {
      await expect(seeded, `${entry.actor} did ${entry.action}`).toHaveCount(1);
    }
    // An actor is a person, a token or the controller, and the page says which.
    await expect(seeded.first()).toContainText(entry.kind);
  }
});

test('the newest change is at the top, and the order can be turned round', async ({ page }) => {
  await goto(page, '/audit', 'Audit');
  await expect(rows(page).first()).toBeVisible();

  // Newest first is the default: the question this page answers is usually
  // "what just happened?".
  const header = log(page).getByRole('columnheader', { name: 'When' });
  // Named, because a heading carries three buttons now: the sort, and the
  // grips that reposition and resize the column.
  const when = header.getByRole('button', { name: 'When', exact: true });
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
  const filtered = rows(page);
  // Not asserted as exactly one: pools.spec.ts's elastic-CPU edit test creates
  // and deletes a pool of its own through the API, and deleting a pool does not
  // erase the pool.create row it wrote on the way in. What this proves is that
  // alice's seeded row survived the filter and the total narrowed.
  await expect(filtered.filter({ hasText: 'alice' })).toHaveCount(1);
  await expect(filtered).not.toHaveCount(before);
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
  // The pressed chip is there although the rows are all pools and have never
  // shown another kind: a filter in force has to be visible to be undone.
  await expect(kindChips(page).getByRole('button', { name: 'pool', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
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
  // Through the action filter, for the reason the listing test gives: the
  // seeded row is hours old and the first page belongs to whatever ran last.
  await goto(page, '/audit?action=pool.create', 'Audit');
  await rows(page).filter({ hasText: 'pool.create' }).filter({ hasText: 'alice' }).click();

  const drawer = page.getByRole('dialog');
  await expect(drawer).toBeVisible();
  await expect(drawer).toContainText('pool.create');
  await expect(drawer).toContainText('alice');
  await expect(drawer.getByRole('heading', { name: 'What changed' })).toBeVisible();

  await page.keyboard.press('Escape');
  await expect(drawer).toBeHidden();
});

/*
 * The list card. The Audit page has no band and nothing between its header
 * and its list, so it is header and card: the log's tile, the count over the
 * title, the search, the action and actor menus and the dates opposite, the
 * kinds of thing acted on as chips in place of the menu, and the grid bare
 * inside. The chips read and write the `target_kind` parameter as the menu
 * did, one kind at a time.
 */
test('the eyebrow counts the events the filters leave, as the API counts them', async ({
  page,
  request,
}) => {
  const { total } = (await (await request.get('/api/v1/audit?limit=1')).json()) as {
    total: number;
  };
  await goto(page, '/audit', 'Audit');
  const card = page.locator('section.list-card');
  await expect(card.getByRole('heading', { name: 'All events' })).toBeVisible();
  // Every spec that changes something writes a row here, so the figure is read
  // again rather than pinned: what is held is that the card says the API's total.
  await expect
    .poll(async () => {
      const said = await card.locator('.eyebrow').textContent();
      const now = (await (await request.get('/api/v1/audit?limit=1')).json()) as {
        total: number;
      };
      return said === `${now.total} events` || said === `${total} events`;
    })
    .toBe(true);
  await expect(card.locator('.grid.bare')).toHaveCount(1);
});

test('a figure is not left standing over an error or over nothing', async ({ page }) => {
  await goto(page, '/audit', 'Audit');
  const eyebrow = page.locator('section.list-card .eyebrow');
  await expect(eyebrow).toContainText(/\d+ events?/);
  // The next page fails: the card must not keep saying the last filter's count
  // beside the grid's error.
  await page.route('**/api/v1/audit?*', (route) => route.abort('connectionfailed'));
  await page.getByRole('combobox', { name: 'Filter by action' }).selectOption({ index: 1 });
  await expect(page.getByRole('grid', { name: 'Audit log' }).getByRole('alert')).toBeVisible();
  await expect(eyebrow).toHaveCount(0);
  await page.unroute('**/api/v1/audit?*');
  // A search nothing matches is the empty state's to explain, not the eyebrow's.
  await goto(page, '/audit?q=nothing-ever-did-this-zzz', 'Audit');
  await expect(page.getByText('Nothing matches those filters')).toBeVisible();
  await expect(eyebrow).toHaveCount(0);
});

test('a kind chip narrows the log to that kind, a second replaces it, and the same again clears it', async ({
  page,
}) => {
  // Starting from a pasted address rather than a cold list: whether a host
  // row is on page one of an unfiltered log depends on how many specs ran
  // before this one, and the pressed kind's chip exists by construction.
  await goto(page, '/audit?target_kind=host', 'Audit');
  await expect(rows(page).filter({ hasText: 'host.join' }).first()).toBeVisible();
  const chips = kindChips(page);
  await expect(chips.getByRole('button', { name: 'host', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await expect(page.getByRole('button', { name: /Remove the Thing filter host/ })).toBeVisible();

  // The same chip again is no filter.
  await chips.getByRole('button', { name: 'host', exact: true }).click();
  await expect(page).not.toHaveURL(/target_kind=/);
  await expect(chips.getByRole('button', { name: /^All/ })).toHaveAttribute('aria-pressed', 'true');
  await expect(rows(page).first()).toBeVisible();

  // One kind at a time: whichever other kind the rows have shown replaces
  // host when pressed, and host is no longer pressed.
  const other = chips
    .getByRole('button')
    .filter({ hasNotText: /^All/ })
    .filter({ hasNotText: /^host$/ })
    .first();
  const kind = (await other.textContent())?.trim() ?? '';
  expect(kind, 'another kind of thing has been shown').not.toBe('');
  await other.click();
  await expect(page).toHaveURL(new RegExp(`[?&]target_kind=${kind}(?:&|#|$)`));
  await expect(chips.getByRole('button', { name: 'host', exact: true })).toHaveAttribute(
    'aria-pressed',
    'false',
  );
  await expect(rows(page).filter({ hasText: 'host.join' })).toHaveCount(0);
  for (const row of await rows(page).all()) await expect(row.locator('.kind')).toHaveText(kind);
});
