import { expect, test, type Page } from '@playwright/test';

// These tests are about the list, so the section is opened as a person would.
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('zoomies.repairs.history.open', 'true');
    localStorage.setItem('zoomies.repairs.filters.open', 'true');
  });
});
import { goto, reload } from './support/fixtures';

/*
  The history is paged and filtered by the server, so these tests stand in for
  the server: a small model of 120 repairs that answers `group`, `repo`,
  `trigger`, `limit` and `offset` the way the real list does, and writes down
  every request it was asked. What is under test is what the page asks for and
  how it reads the answer, not the arithmetic of the controller's filter.

  The list, the selection and the bulk route are answered here, each by its
  exact path. The page also reads /assistant/repairs/settings, /readiness and
  /opportunities, which stay real, so a match on the prefix
  /assistant/repairs would be too wide.
*/

const STATES = [
  'queued',
  'working',
  'checking',
  'succeeded',
  'checks_failed',
  'interrupted',
  'unverified',
  'superseded',
  'blocked',
  'failed',
];
const ACTIVE = ['queued', 'working', 'checking'];
const REPOS = ['acme/site', 'acme/widgets', 'acme/api'];
const NEWEST = Date.UTC(2026, 9, 12, 12, 0, 0);

/** Newest first, so row 0 is #1 and the states, repositories and triggers cycle. */
function repair(index: number) {
  const created = new Date(NEWEST - index * 60_000).toISOString();
  return {
    id: `rep-${String(index + 1).padStart(3, '0')}`,
    installation_id: 'inst-1',
    repo: REPOS[index % REPOS.length]!,
    head_sha: `head${index}`,
    commit_sha: '',
    user_id: 'user-1',
    github_login: 'octocat',
    provider_id: 'prov-1',
    // The server stores a person's request as "mention", whatever way they asked;
    // "requested" is only ever the filter's name for everything that is not
    // automatic.
    trigger: index % 2 === 0 ? 'automatic' : 'mention',
    state: STATES[index % STATES.length]!,
    message: `Repair number ${index + 1}`,
    pull_number: index + 1,
    job_id: index + 1,
    run_id: index + 1,
    github_user_id: 1,
    comment_id: 0,
    created_at: created,
    updated_at: created,
  };
}

const inGroup = (group: string | null, state: string) =>
  group === 'attention'
    ? !ACTIVE.includes(state) && state !== 'succeeded'
    : group === 'active'
      ? ACTIVE.includes(state)
      : group === 'passed'
        ? state === 'succeeded'
        : true;

type Row = ReturnType<typeof repair>;
type Answer = { items: Row[]; total: number };

/** What the real selection route will not send in one answer. */
const SELECTION_CAP = 500;

async function mockHistory(page: Page, count = 120) {
  const seen: URLSearchParams[] = [];
  const model = {
    rows: Array.from({ length: count }, (_, i) => repair(i)),
    seen,
    /** Every selection read, with the filters it carried. */
    selections: [] as URLSearchParams[],
    /** Every bulk request, as the page sent it. */
    bulk: [] as { action: string; ids: string[] }[],
    cap: SELECTION_CAP,
    /** While set, the next answers wait for it, so a test can look at a read in flight. */
    gate: null as Promise<void> | null,
    /** The same for the selection route, which the page waits on separately. */
    selectionGate: null as Promise<void> | null,
    /** Replaces the model's own answer, for a server that says something odd. */
    answer: null as ((query: URLSearchParams) => Answer) | null,
    /**
     * While set, a bulk request fails with the message and clears nothing, once
     * `after` requests have gone through: 0 fails the first, 1 lets the first
     * succeed and fails the second.
     */
    bulkFails: null as { message: string; after: number } | null,
  };
  /** What the same filters would list, which is also what they would select from. */
  const matching = (query: URLSearchParams) => {
    const repo = (query.get('repo') ?? '').trim().toLowerCase();
    const trigger = query.get('trigger');
    return model.rows.filter(
      (r) =>
        inGroup(query.get('group'), r.state) &&
        r.repo.toLowerCase().includes(repo) &&
        (trigger === 'automatic'
          ? r.trigger === 'automatic'
          : trigger === 'requested'
            ? r.trigger !== 'automatic'
            : true),
    );
  };
  await page.route(
    (url) => url.pathname === '/api/v1/assistant/repairs',
    async (route) => {
      const request = route.request();
      // Asking for a repair is a POST to this same path and stays real.
      if (request.method() !== 'GET') return route.fallback();
      const query = new URL(request.url()).searchParams;
      // The hub's own list of the repairs that are running is a separate read, with
      // a limit no page size uses. It is answered from the same rows and not
      // counted: these tests are about what the history asks for.
      if (query.get('group') === 'active' && query.get('limit') === '20') {
        const going = model.rows.filter((r) => ACTIVE.includes(r.state));
        return route.fulfill({ json: { items: going.slice(0, 20), total: going.length } });
      }
      seen.push(query);
      if (model.gate) await model.gate;
      if (model.answer) return route.fulfill({ json: model.answer(query) });
      const matches = matching(query);
      const limit = Number(query.get('limit') ?? 50);
      const offset = Number(query.get('offset') ?? 0);
      return route.fulfill({
        json: { items: matches.slice(offset, offset + limit), total: matches.length },
      });
    },
  );
  // Only what can be cleared, newest first and no more than the cap, as the real
  // route answers; `total` is how many could be cleared in all.
  await page.route(
    (url) => url.pathname === '/api/v1/assistant/repairs/selection',
    async (route) => {
      const query = new URL(route.request().url()).searchParams;
      model.selections.push(query);
      if (model.selectionGate) await model.selectionGate;
      const clearable = matching(query).filter((r) => !ACTIVE.includes(r.state));
      return route.fulfill({
        json: {
          ids: clearable.slice(0, model.cap).map((r) => r.id),
          total: clearable.length,
          truncated: clearable.length > model.cap,
        },
      });
    },
  );
  // One result for each id, in the order asked. A repair that has started running
  // since the page last read it is refused, as the real route refuses it.
  await page.route(
    (url) => url.pathname === '/api/v1/assistant/repairs/bulk',
    async (route) => {
      const body = route.request().postDataJSON() as { action: string; ids: string[] };
      model.bulk.push(body);
      if (model.bulkFails && model.bulk.length > model.bulkFails.after)
        return route.fulfill({
          status: 500,
          json: { error: { code: 'internal', message: model.bulkFails.message } },
        });
      const cleared = new Set<string>();
      const results = body.ids.map((id) => {
        const row = model.rows.find((r) => r.id === id);
        if (!row) return { id, ok: false, error: 'not found' };
        if (ACTIVE.includes(row.state)) return { id, ok: false, error: 'still running' };
        cleared.add(id);
        return { id, ok: true };
      });
      model.rows = model.rows.filter((r) => !cleared.has(r.id));
      return route.fulfill({ json: { results } });
    },
  );
  return model;
}

/** Every list request made after `from`, as plain objects. */
const asked = (seen: URLSearchParams[], from = 0) =>
  seen.slice(from).map((q) => Object.fromEntries(q));

const historyOf = (page: Page) => page.getByRole('region', { name: 'History' });
const paginationOf = (page: Page) => page.getByRole('navigation', { name: 'Pagination' });
const barOf = (page: Page) => historyOf(page).getByRole('group', { name: 'Repair selection' });
/** A row's own checkbox, named by the repair it belongs to. */
const tickOf = (page: Page, r: Row) =>
  historyOf(page).getByRole('checkbox', {
    name: `Select repair ${r.repo} #${r.pull_number}`,
    exact: true,
  });
const dialogOf = (page: Page) => page.getByRole('dialog', { name: 'Clear from history' });
/**
 * Choosing repairs is behind a button, so the history reads as a record until
 * somebody means to edit it.
 */
const selecting = (page: Page) =>
  historyOf(page).getByRole('button', { name: 'Select', exact: true }).click();
/**
 * What a clear left on the card for a person to read. Not a live region: what a
 * screen reader is told is written into one the card keeps mounted.
 */
const outcomeOf = (page: Page) =>
  historyOf(page).getByRole('group', { name: 'Result of the last clear' });

async function noSidewaysScroll(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
}

test('the history is paged by the server and says how many there are', async ({ page }) => {
  const { seen } = await mockHistory(page);
  await goto(page, '/repairs', 'Eli repairs');
  const history = historyOf(page);
  const pagination = paginationOf(page);

  await expect(pagination).toContainText('1–50 of 120 repairs');
  await expect(history.getByRole('article')).toHaveCount(50);
  await expect(history.getByRole('article').first()).toContainText('acme/site #1');

  let from = seen.length;
  await page.getByRole('button', { name: 'Next page' }).click();
  await expect.poll(() => asked(seen, from)).toContainEqual({ offset: '50', limit: '50' });
  await expect(pagination).toContainText('51–100 of 120 repairs');
  await expect(history.getByRole('article').first()).toContainText('acme/api #51');

  from = seen.length;
  await pagination.getByLabel('Rows').selectOption('25');
  // A different page size starts again from the top; the offset it was at
  // would land somewhere unrelated in the new pages.
  await expect.poll(() => asked(seen, from)).toContainEqual({ limit: '25', offset: '0' });
  await expect(pagination).toContainText('1–25 of 120 repairs');
  await expect(history.getByRole('article')).toHaveCount(25);
  await noSidewaysScroll(page);
});

test('a filter reaches repairs beyond the first page and resets the page', async ({ page }) => {
  const { seen } = await mockHistory(page);
  await goto(page, '/repairs', 'Eli repairs');
  const history = historyOf(page);
  const pagination = paginationOf(page);

  await page.getByRole('button', { name: 'Next page' }).click();
  await expect(pagination).toContainText('51–100 of 120 repairs');

  let from = seen.length;
  await history.getByLabel('Show', { exact: true }).selectOption('attention');
  await expect
    .poll(() => asked(seen, from))
    .toContainEqual({
      group: 'attention',
      limit: '50',
      offset: '0',
    });
  // Six of every ten repairs are neither running nor passed.
  await expect(pagination).toContainText('1–50 of 72 repairs');

  from = seen.length;
  await history.getByLabel('Repository', { exact: true }).pressSequentially('widgets');
  await expect
    .poll(() => asked(seen, from), { message: 'a request for the finished text' })
    .toContainEqual({ group: 'attention', repo: 'widgets', limit: '50', offset: '0' });
  // One request for the whole word, not one for every letter on the way to it.
  expect(asked(seen, from).filter((q) => q.repo !== undefined)).toHaveLength(1);
  await expect(pagination).toContainText('1–24 of 24 repairs');

  from = seen.length;
  await history.getByLabel('Triggered by', { exact: true }).selectOption('automatic');
  await expect
    .poll(() => asked(seen, from))
    .toContainEqual({
      group: 'attention',
      repo: 'widgets',
      trigger: 'automatic',
      limit: '50',
      offset: '0',
    });
  await noSidewaysScroll(page);
});

test('a filter that matches nothing says so and can be cleared', async ({ page }) => {
  const model = await mockHistory(page);
  await goto(page, '/repairs', 'Eli repairs');
  const history = historyOf(page);

  await history.getByLabel('Repository', { exact: true }).fill('none');
  await expect(history.getByText('No repairs match these filters')).toBeVisible();
  await expect(history.getByText('No repairs yet')).toHaveCount(0);
  // The controls stay, or there would be no way to widen the search.
  await expect(history.getByLabel('Repository', { exact: true })).toHaveValue('none');
  await history.getByRole('button', { name: 'Clear filters' }).click();
  await expect(history.getByRole('article')).toHaveCount(50);
  await expect(history.getByLabel('Repository', { exact: true })).toHaveValue('');
  await expect(history.getByLabel('Show', { exact: true })).toHaveValue('all');
  await expect(history.getByText('No repairs match these filters')).toHaveCount(0);

  // A history with nothing in it at all is a different fact, and offers nothing
  // to filter.
  model.rows.length = 0;
  await reload(page, 'Eli repairs');
  await expect(history.getByText('No repairs yet')).toBeVisible();
  await expect(history.getByText('No repairs match these filters')).toHaveCount(0);
  await expect(history.getByRole('button', { name: 'Select', exact: true })).toHaveCount(0);
  await expect(history.getByLabel('Show', { exact: true })).toHaveCount(0);
  await expect(history.getByLabel('Repository', { exact: true })).toHaveCount(0);
  await expect(history.getByLabel('Triggered by', { exact: true })).toHaveCount(0);
  await expect(paginationOf(page)).toHaveCount(0);
  await noSidewaysScroll(page);
});

test('the history keeps its page and filters through the refresh', async ({ page }) => {
  // The page refreshes itself every ten seconds; its clock is moved rather than
  // waited for.
  await page.clock.install();
  const { seen } = await mockHistory(page);
  await goto(page, '/repairs', 'Eli repairs');
  const history = historyOf(page);
  const pagination = paginationOf(page);

  await history.getByLabel('Show', { exact: true }).selectOption('attention');
  await expect(pagination).toContainText('1–50 of 72 repairs');
  await page.getByRole('button', { name: 'Next page' }).click();
  await expect(pagination).toContainText('51–72 of 72 repairs');

  const from = seen.length;
  await page.clock.fastForward(10_000);
  await expect
    .poll(() => asked(seen, from), { message: 'the refresh asked again' })
    .toContainEqual({ group: 'attention', limit: '50', offset: '50' });
  // None of the refresh's reads fell back to the first page or dropped the filter.
  expect(asked(seen, from).filter((q) => q.group === undefined || q.offset === '0')).toEqual([]);
  await expect(pagination).toContainText('51–72 of 72 repairs');
  await expect(history.getByLabel('Show', { exact: true })).toHaveValue('attention');
  await noSidewaysScroll(page);
});

// The guidelines' rule for a page past the end: the history is not empty, so
// the list steps back to the last page that has rows rather than saying so.
test('a page emptied under the operator steps back instead of showing an empty list', async ({
  page,
}) => {
  await page.clock.install();
  const model = await mockHistory(page);
  await goto(page, '/repairs', 'Eli repairs');
  const pagination = paginationOf(page);

  await page.getByRole('button', { name: 'Next page' }).click();
  await expect(pagination).toContainText('51–100 of 120 repairs');

  model.rows.length = 50;
  const from = model.seen.length;
  await page.clock.fastForward(10_000);
  await expect.poll(() => asked(model.seen, from)).toContainEqual({ limit: '50', offset: '0' });
  await expect(pagination).toContainText('1–50 of 50 repairs');
  await expect(historyOf(page).getByRole('article')).toHaveCount(50);
  await expect(historyOf(page).getByText('No repairs')).toHaveCount(0);
  await noSidewaysScroll(page);
});

// The controls are what the operator is using to widen the search, and the
// history is not empty, so neither may go while the wider answer is on its way:
// the filters were reset a moment before the answer to the narrow ones was
// replaced, and the page must go by what it last heard, not what it has asked.
test('widening an empty filtered result keeps the controls and never calls the history empty', async ({
  page,
}) => {
  const model = await mockHistory(page);
  // Every repair has passed, so "Needs attention" matches nothing.
  model.rows = model.rows.filter((r) => r.state === 'succeeded');
  await goto(page, '/repairs', 'Eli repairs');
  const history = historyOf(page);
  const show = history.getByLabel('Show', { exact: true });
  const repository = history.getByLabel('Repository', { exact: true });
  await expect(history.getByRole('article')).toHaveCount(12);

  // Widening by the Show control.
  await show.selectOption('attention');
  await expect(history.getByText('No repairs match these filters')).toBeVisible();
  const control = await show.elementHandle();
  let release!: () => void;
  model.gate = new Promise<void>((resolve) => (release = resolve));
  let from = model.seen.length;
  await show.selectOption('all');
  await expect.poll(() => asked(model.seen, from)).toContainEqual({ limit: '50', offset: '0' });
  await expect(show).toBeVisible();
  await expect(show).toHaveValue('all');
  await expect(history.getByText('No repairs yet')).toHaveCount(0);
  expect(await control!.evaluate((el) => el.isConnected)).toBe(true);
  release();
  model.gate = null;
  await expect(history.getByRole('article')).toHaveCount(12);

  // Widening by Clear filters.
  await repository.fill('none');
  await expect(history.getByText('No repairs match these filters')).toBeVisible();
  const box = await repository.elementHandle();
  model.gate = new Promise<void>((resolve) => (release = resolve));
  from = model.seen.length;
  await history.getByRole('button', { name: 'Clear filters' }).click();
  await expect.poll(() => asked(model.seen, from)).toContainEqual({ limit: '50', offset: '0' });
  await expect(repository).toBeVisible();
  await expect(repository).toHaveValue('');
  await expect(history.getByText('No repairs yet')).toHaveCount(0);
  expect(await box!.evaluate((el) => el.isConnected)).toBe(true);
  release();
  model.gate = null;
  await expect(history.getByRole('article')).toHaveCount(12);
  await noSidewaysScroll(page);
});

// A server that answers an in-range page with no rows would send a page that
// steps back to the same offset round and round, one read after another. The
// step must go somewhere lower or stop.
test('a page that comes back empty at an offset that is in range is not read again in a loop', async ({
  page,
}) => {
  await page.clock.install();
  const model = await mockHistory(page);
  await goto(page, '/repairs', 'Eli repairs');
  const pagination = paginationOf(page);
  await page.getByRole('button', { name: 'Next page' }).click();
  await expect(pagination).toContainText('51–100 of 120 repairs');

  model.answer = () => ({ items: [], total: 120 });
  const from = model.seen.length;
  await page.clock.fastForward(10_000);
  await expect.poll(() => asked(model.seen, from).length).toBeGreaterThan(0);
  // Long enough for a loop to have made hundreds of reads.
  await page.waitForTimeout(500);
  expect(asked(model.seen, from).length).toBeLessThanOrEqual(2);
  await expect(pagination).toContainText('of 120 repairs');
  await noSidewaysScroll(page);
});

// Choosing is how a record is edited, so the list reads as a record until somebody
// asks to edit it, and goes back to one when they are done.
test('rows are not selectable until Select is pressed', async ({ page }) => {
  await mockHistory(page);
  await goto(page, '/repairs', 'Eli repairs');
  const history = historyOf(page);
  await expect(history.getByRole('article')).toHaveCount(50);
  await expect(history.getByRole('checkbox')).toHaveCount(0);
  await expect(barOf(page)).toHaveCount(0);

  await history.getByRole('button', { name: 'Select', exact: true }).click();
  await expect(
    history.getByRole('checkbox', { name: 'Select the 50 repairs shown' }),
  ).toBeVisible();
  await expect(tickOf(page, repair(3))).toBeVisible();
  await expect(barOf(page)).toContainText('Select repairs to clear them from the history');

  // Done drops the boxes and whatever was chosen, and the next Select starts clean.
  await tickOf(page, repair(3)).check();
  await expect(barOf(page).getByText('1 selected')).toBeVisible();
  await history.getByRole('button', { name: 'Done', exact: true }).click();
  await expect(history.getByRole('checkbox')).toHaveCount(0);
  await expect(barOf(page)).toHaveCount(0);
  await history.getByRole('button', { name: 'Select', exact: true }).click();
  await expect(barOf(page)).toContainText('Select repairs to clear them from the history');
  await expect(tickOf(page, repair(3))).not.toBeChecked();
  await noSidewaysScroll(page);
});

// Rows are chosen three ways, and they must agree about what is chosen: the
// count in the bar is the one number an operator reads before clearing.
test('rows are selected one at a time, by the page, and by everything that matches', async ({
  page,
}) => {
  const model = await mockHistory(page);
  await goto(page, '/repairs', 'Eli repairs');
  await selecting(page);
  const bar = barOf(page);
  const header = historyOf(page).getByRole('checkbox', { name: 'Select the 50 repairs shown' });
  const selectAll = bar.getByRole('button', { name: /^Select all .* matching$/ });

  await expect(bar).toContainText('Select repairs to clear them from the history');
  await expect(bar.getByRole('button', { name: 'Clear from history' })).toHaveCount(0);

  // One at a time. The header then stands between all and none.
  await tickOf(page, repair(3)).check();
  await expect(bar.getByText('1 selected')).toBeVisible();
  await expect(header).toHaveJSProperty('indeterminate', true);
  await expect(header).not.toBeChecked();
  // Not every shown row is chosen, so there is no offer to take the rest yet.
  await expect(selectAll).toHaveCount(0);

  // The page: 50 rows are shown, and 15 of them are still running.
  await header.check();
  await expect(bar.getByText('35 selected')).toBeVisible();
  await expect(header).toBeChecked();
  await expect(header).toHaveJSProperty('indeterminate', false);
  await expect(tickOf(page, repair(3))).toBeChecked();
  await expect(tickOf(page, repair(1))).not.toBeChecked();

  // Everything that matches: 120 repairs, of which 84 can be cleared.
  await expect(selectAll).toHaveText('Select all 120 matching');
  // While it is being read the button is off, so a second press cannot start a
  // second read.
  let release!: () => void;
  model.selectionGate = new Promise<void>((resolve) => (release = resolve));
  await selectAll.click();
  // A busy button is named for what it is doing, not for its label.
  await expect(bar.getByRole('button', { name: 'Working' })).toBeDisabled();
  await expect(bar.getByText('35 selected')).toBeVisible();
  release();
  model.selectionGate = null;
  await expect.poll(() => model.selections.map((q) => Object.fromEntries(q))).toEqual([{}]);
  await expect(bar.getByText('84 selected')).toBeVisible();
  // There is nothing left to offer once everything has been taken.
  await expect(selectAll).toHaveCount(0);

  await bar.getByRole('button', { name: 'Clear selection' }).click();
  await expect(bar).toContainText('Select repairs to clear them from the history');
  await expect(header).not.toBeChecked();

  // The selection read carries the filters, and none of the paging.
  await historyOf(page).getByLabel('Triggered by', { exact: true }).selectOption('requested');
  await expect(paginationOf(page)).toContainText('1–50 of 60 repairs');
  await header.check();
  await expect(bar.getByText('40 selected')).toBeVisible();
  await expect(selectAll).toHaveText('Select all 60 matching');
  await selectAll.click();
  await expect(bar.getByText('48 selected')).toBeVisible();
  expect(model.selections.map((q) => Object.fromEntries(q)).at(-1)).toEqual({
    trigger: 'requested',
  });

  // A history longer than the server will select in one go says how much of it
  // was taken, so nobody clears 500 believing they have cleared 700.
  model.rows = Array.from({ length: 1000 }, (_, i) => repair(i));
  await reload(page, 'Eli repairs');
  await selecting(page);
  await header.check();
  await selectAll.click();
  await expect(bar.getByText('500 of 700 selected')).toBeVisible();
  await expect(selectAll).toHaveCount(0);
  await noSidewaysScroll(page);
});

// A repair Eli is still working on cannot be cleared, and a control that
// refuses without saying why is reported as broken.
test('an in-flight repair cannot be selected', async ({ page }) => {
  await mockHistory(page);
  await goto(page, '/repairs', 'Eli repairs');
  await selecting(page);
  const bar = barOf(page);

  // The reason is shown to a pointer, and is also the box's description.
  await tickOf(page, repair(1)).hover();
  await expect(page.locator('.bubble:popover-open')).toContainText('Still running');

  // Rows 1, 2 and 3 are queued, working and waiting on checks.
  for (const index of [0, 1, 2]) {
    const tick = tickOf(page, repair(index));
    await expect(tick).toBeDisabled();
    await expect(tick).toHaveAccessibleDescription('Still running');
    // Clicking is refused as well as styled so: aria-disabled stops nothing alone.
    await tick.click({ force: true });
    await expect(tick).not.toBeChecked();
  }
  await expect(bar).toContainText('Select repairs to clear them from the history');

  // A finished repair beside them is not affected.
  await expect(tickOf(page, repair(3))).toBeEnabled();
  await noSidewaysScroll(page);
});

// Clearing hides a repair and nothing else, and the dialog is the only place an
// operator is told so before it happens.
test('clearing says what it does, then clears and reports', async ({ page }) => {
  const model = await mockHistory(page);
  await goto(page, '/repairs', 'Eli repairs');
  await selecting(page);
  const history = historyOf(page);
  const bar = barOf(page);
  const pagination = paginationOf(page);
  const [first, second, third] = [repair(3), repair(4), repair(5)] as const;

  for (const r of [first, second, third]) await tickOf(page, r).check();
  await expect(bar.getByText('3 selected')).toBeVisible();

  // Asking changes nothing, and neither does changing one's mind.
  await bar.getByRole('button', { name: 'Clear from history' }).click();
  const dialog = dialogOf(page);
  await expect(dialog.getByText('The repairs stop showing in this list.')).toBeVisible();
  // How many, and that this page cannot bring them back: three here, but after a
  // select-all it is up to 500, most of them on pages the operator never saw.
  await expect(
    dialog.getByText(
      '3 repairs will stop showing in this list, and this cannot be undone from this page.',
    ),
  ).toBeVisible();
  await expect(
    dialog.getByText('Eli will not repair its own commits from them again.'),
  ).toBeVisible();
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(bar.getByText('3 selected')).toBeVisible();
  expect(model.bulk).toEqual([]);

  // One of the three starts running between the page reading it and the click.
  model.rows[4]!.state = 'working';
  await bar.getByRole('button', { name: 'Clear from history' }).click();
  await dialog.getByRole('button', { name: 'Clear from history' }).click();
  await expect
    .poll(() => model.bulk)
    .toEqual([{ action: 'clear', ids: [first.id, second.id, third.id] }]);
  await expect(dialog).toHaveCount(0);

  // The selection is spent, the report says what happened, and the list is the
  // server's again.
  await expect(bar).toContainText('Select repairs to clear them from the history');
  const report = outcomeOf(page);
  await expect(report).toContainText('2 repairs cleared from the history');
  await expect(report).toContainText('1 skipped');
  await report.getByText('Review skipped repairs').click();
  await expect(
    report.getByText(`${second.repo} #${second.pull_number}: still running`),
  ).toBeVisible();
  await expect(pagination).toContainText('1–50 of 118 repairs');
  await expect(history.getByText('Repair number 4', { exact: true })).toHaveCount(0);
  await expect(history.getByText('Repair number 5', { exact: true })).toBeVisible();

  await report.getByRole('button', { name: 'Dismiss' }).click();
  await expect(report).toHaveCount(0);
  await noSidewaysScroll(page);
});

// A clear that fails has not happened: the rows stay chosen and the dialog stays
// open, so the operator can try again without choosing them over.
test('a clear that fails keeps the selection and the dialog', async ({ page }) => {
  const model = await mockHistory(page);
  await goto(page, '/repairs', 'Eli repairs');
  await selecting(page);
  const bar = barOf(page);

  await tickOf(page, repair(3)).check();
  await bar.getByRole('button', { name: 'Clear from history' }).click();
  model.bulkFails = { message: 'the database is busy', after: 0 };
  await dialogOf(page).getByRole('button', { name: 'Clear from history' }).click();
  await expect(page.getByText('the database is busy')).toBeVisible();
  await expect(dialogOf(page)).toBeVisible();
  await expect(bar.getByText('1 selected')).toBeVisible();

  model.bulkFails = null;
  await dialogOf(page).getByRole('button', { name: 'Clear from history' }).click();
  await expect(outcomeOf(page)).toContainText('1 repair cleared from the history');
  await expect(paginationOf(page)).toContainText('1–50 of 119 repairs');
  expect(model.bulk).toHaveLength(2);
});

// Choosing is slow work across pages, and the history refreshes under it every
// ten seconds; only a change to what is listed may lose it.
test('a selection survives the refresh and a page change, and a filter change drops it', async ({
  page,
}) => {
  await page.clock.install();
  const { seen } = await mockHistory(page);
  await goto(page, '/repairs', 'Eli repairs');
  await selecting(page);
  const history = historyOf(page);
  const bar = barOf(page);
  const pagination = paginationOf(page);

  await tickOf(page, repair(3)).check();
  await page.getByRole('button', { name: 'Next page' }).click();
  await expect(pagination).toContainText('51–100 of 120 repairs');
  await tickOf(page, repair(54)).check();
  await expect(bar.getByText('2 selected')).toBeVisible();

  const from = seen.length;
  await page.clock.fastForward(10_000);
  await expect.poll(() => asked(seen, from)).toContainEqual({ limit: '50', offset: '50' });
  await expect(bar.getByText('2 selected')).toBeVisible();
  await expect(tickOf(page, repair(54))).toBeChecked();

  // The row chosen on the first page is still chosen when it comes back.
  await page.getByRole('button', { name: 'Previous page' }).click();
  await expect(pagination).toContainText('1–50 of 120 repairs');
  await expect(tickOf(page, repair(3))).toBeChecked();
  await expect(bar.getByText('2 selected')).toBeVisible();

  // What is listed changing drops it: the rows chosen may not be in the new list
  // at all, and clearing them unseen is the mistake to design out.
  await history.getByLabel('Show', { exact: true }).selectOption('attention');
  await expect(pagination).toContainText('1–50 of 72 repairs');
  await expect(bar).toContainText('Select repairs to clear them from the history');
  await expect(history.getByRole('checkbox', { checked: true })).toHaveCount(0);

  await tickOf(page, repair(4)).check();
  await expect(bar.getByText('1 selected')).toBeVisible();
  await history.getByLabel('Repository', { exact: true }).fill('widgets');
  await expect(pagination).toContainText('1–24 of 24 repairs');
  await expect(bar).toContainText('Select repairs to clear them from the history');
  await expect(history.getByRole('checkbox', { checked: true })).toHaveCount(0);
  await noSidewaysScroll(page);
});

// After a clear the page the operator was on may no longer exist, and the list
// should go straight to the last one that does rather than ask for the empty
// one first.
test('clearing the last row on the last page steps back a page', async ({ page }) => {
  const model = await mockHistory(page, 51);
  // The 51st would be queued, which cannot be cleared.
  model.rows[50]!.state = 'failed';
  await goto(page, '/repairs', 'Eli repairs');
  await selecting(page);
  const history = historyOf(page);
  const pagination = paginationOf(page);

  await page.getByRole('button', { name: 'Next page' }).click();
  await expect(pagination).toContainText('51–51 of 51 repairs');
  await expect(history.getByRole('article')).toHaveCount(1);
  await tickOf(page, model.rows[50]!).check();

  const from = model.seen.length;
  await barOf(page).getByRole('button', { name: 'Clear from history' }).click();
  await dialogOf(page).getByRole('button', { name: 'Clear from history' }).click();

  await expect(pagination).toContainText('1–50 of 50 repairs');
  await expect(history.getByRole('article')).toHaveCount(50);
  await expect(outcomeOf(page)).toContainText('1 repair cleared from the history');
  // Every read after the clear asked for the page that exists.
  const after = asked(model.seen, from);
  expect(after.length).toBeGreaterThan(0);
  expect(after.filter((q) => q.offset !== '0')).toEqual([]);
  await noSidewaysScroll(page);
});

// The bulk route refuses more than 500 ids, and a selection can pass that: the
// newest 500 from select-all, and a row from a page beyond them.
test('a selection past the bulk limit is cleared in requests the route accepts', async ({
  page,
}) => {
  const model = await mockHistory(page, 1000);
  await goto(page, '/repairs', 'Eli repairs');
  await selecting(page);
  const bar = barOf(page);
  const pagination = paginationOf(page);

  await historyOf(page).getByRole('checkbox', { name: 'Select the 50 repairs shown' }).check();
  await bar.getByRole('button', { name: 'Select all 1,000 matching' }).click();
  await expect(bar.getByText('500 of 700 selected')).toBeVisible();

  await page.getByRole('button', { name: 'Last page' }).click();
  await expect(pagination).toContainText('951–1,000 of 1,000 repairs');
  // Row 954 is a finished one, older than the 500 newest finished ones.
  await tickOf(page, repair(953)).check();
  await expect(bar.getByText('501 selected')).toBeVisible();

  await bar.getByRole('button', { name: 'Clear from history' }).click();
  await dialogOf(page).getByRole('button', { name: 'Clear from history' }).click();
  await expect(outcomeOf(page)).toContainText('501 repairs cleared from the history');
  expect(model.bulk.map((b) => b.ids.length)).toEqual([500, 1]);
  expect(model.bulk.every((b) => b.action === 'clear')).toBe(true);
  // 499 are left, so the page it was on has gone and the list went to the last.
  await expect(pagination).toContainText('451–499 of 499 repairs');
  await noSidewaysScroll(page);
});

// The count in the bar is what the operator agreed to clear. A select-all that
// is still being read has not settled it, and the answer replaces the set when
// it lands, so a clear started in that gap would clear more than they saw.
test('a clear cannot start while a select-all is being read, and clears what the dialog counted', async ({
  page,
}) => {
  const model = await mockHistory(page);
  await goto(page, '/repairs', 'Eli repairs');
  await selecting(page);
  const bar = barOf(page);
  const clearing = bar.getByRole('button', { name: 'Clear from history' });

  await historyOf(page).getByRole('checkbox', { name: 'Select the 50 repairs shown' }).check();
  await expect(bar.getByText('35 selected')).toBeVisible();

  let release!: () => void;
  model.selectionGate = new Promise<void>((resolve) => (release = resolve));
  await bar.getByRole('button', { name: 'Select all 120 matching' }).click();
  // Off while the read is out, and a press does not open the dialog.
  await expect(clearing).toBeDisabled();
  await clearing.click({ force: true });
  await expect(dialogOf(page)).toHaveCount(0);

  release();
  model.selectionGate = null;
  await expect(bar.getByText('84 selected')).toBeVisible();
  await expect(clearing).toBeEnabled();
  await clearing.click();
  await expect(
    dialogOf(page).getByText(
      '84 repairs will stop showing in this list, and this cannot be undone from this page.',
    ),
  ).toBeVisible();
  // Exactly the repairs the dialog counted, newest first as the route sent them.
  // Worked out before the confirm, which removes them from the model.
  const clearable = model.rows.filter((r) => !ACTIVE.includes(r.state)).map((r) => r.id);
  expect(clearable).toHaveLength(84);
  await dialogOf(page).getByRole('button', { name: 'Clear from history' }).click();
  await expect.poll(() => model.bulk).toEqual([{ action: 'clear', ids: clearable }]);
});

// The rows on show after a filter change are still the old filter's until the
// answer lands, and a box ticked among them would sit in the set under filters
// it does not belong to, with the bar counting one and nothing visibly ticked.
test('the boxes are off while the list is re-read for new filters', async ({ page }) => {
  const model = await mockHistory(page);
  await goto(page, '/repairs', 'Eli repairs');
  await selecting(page);
  const history = historyOf(page);
  const bar = barOf(page);
  const header = history.getByRole('checkbox', { name: 'Select the 50 repairs shown' });

  let release!: () => void;
  model.gate = new Promise<void>((resolve) => (release = resolve));
  await history.getByLabel('Show', { exact: true }).selectOption('attention');
  // Row 4 passed, so it is not in "Needs attention", but it is still on show.
  const stale = tickOf(page, repair(3));
  await expect(stale).toBeVisible();
  await expect(stale).toBeDisabled();
  await expect(header).toBeDisabled();
  await stale.click({ force: true });
  await expect(bar).toContainText('Select repairs to clear them from the history');

  release();
  model.gate = null;
  await expect(paginationOf(page)).toContainText('1–50 of 72 repairs');
  await expect(history.getByRole('article').first()).toContainText('Repair number 5');
  await expect(header).toBeEnabled();
  await expect(tickOf(page, repair(4))).toBeEnabled();
  await tickOf(page, repair(4)).check();
  await expect(bar.getByText('1 selected')).toBeVisible();
  await noSidewaysScroll(page);
});

// A clear that stops part-way has cleared some and not tried the rest. It says
// so in the same words in the toast and the report, and the rest stay chosen so
// that trying again is one press.
test('a clear that fails on a later request reports what went and keeps the rest chosen', async ({
  page,
}) => {
  const model = await mockHistory(page, 1000);
  await goto(page, '/repairs', 'Eli repairs');
  await selecting(page);
  const history = historyOf(page);
  const bar = barOf(page);
  const late = repair(953);

  await history.getByRole('checkbox', { name: 'Select the 50 repairs shown' }).check();
  await bar.getByRole('button', { name: 'Select all 1,000 matching' }).click();
  await expect(bar.getByText('500 of 700 selected')).toBeVisible();
  await page.getByRole('button', { name: 'Last page' }).click();
  await tickOf(page, late).check();
  await expect(bar.getByText('501 selected')).toBeVisible();

  // The first request of 500 goes through and the second, of one, does not.
  model.bulkFails = { message: 'the database is busy', after: 1 };
  await bar.getByRole('button', { name: 'Clear from history' }).click();
  await dialogOf(page).getByRole('button', { name: 'Clear from history' }).click();

  const report = outcomeOf(page);
  await expect(report).toContainText('500 repairs cleared from the history');
  await expect(report).toContainText('1 skipped');
  await report.getByText('Review skipped repairs').click();
  await expect(report.getByText(`${late.repo} #${late.pull_number}: not attempted`)).toBeVisible();
  await expect(page.getByText('Cleared 500 of 501 repairs before a request failed')).toBeVisible();
  await expect(page.getByText('the database is busy')).toBeVisible();
  // What was not tried is still chosen, and the dialog has gone.
  await expect(dialogOf(page)).toHaveCount(0);
  await expect(bar.getByText('1 selected')).toBeVisible();
  expect(model.bulk.map((b) => b.ids.length)).toEqual([500, 1]);

  // Another go sends only what was left.
  model.bulkFails = null;
  await bar.getByRole('button', { name: 'Clear from history' }).click();
  await expect(
    dialogOf(page).getByText(
      '1 repair will stop showing in this list, and this cannot be undone from this page.',
    ),
  ).toBeVisible();
  await dialogOf(page).getByRole('button', { name: 'Clear from history' }).click();
  await expect(report).toContainText('1 repair cleared from the history');
  expect(model.bulk.at(-1)).toEqual({ action: 'clear', ids: [late.id] });
  await noSidewaysScroll(page);
});

// A box or a button inside a live region is announced by name whenever it
// appears, so the card announces words and nothing a person presses.
test('no box or button on the history card sits inside a live region', async ({ page }) => {
  await mockHistory(page);
  await goto(page, '/repairs', 'Eli repairs');
  await selecting(page);
  const history = historyOf(page);
  const live = history.locator('[aria-live]');

  await tickOf(page, repair(3)).check();
  await expect(barOf(page).getByRole('button', { name: 'Clear from history' })).toBeVisible();
  await expect(live.getByRole('checkbox')).toHaveCount(0);
  await expect(live.getByRole('button')).toHaveCount(0);
  await expect(barOf(page)).not.toHaveAttribute('aria-live', /.*/);
});

// The outcome of a clear is the most consequential thing this page says, and it
// arrives after the button that caused it has gone, because the selection is
// spent. Some screen readers announce nothing for a live region that is inserted
// already holding its words, so the words are written into regions that were
// mounted, and empty, before anything happened.
test('the count and the outcome are announced through live regions that were already mounted', async ({
  page,
}) => {
  await mockHistory(page);
  await goto(page, '/repairs', 'Eli repairs');
  await selecting(page);
  const history = historyOf(page);
  const live = history.locator('[aria-live]');
  const [selection, outcome] = [live.nth(0), live.nth(1)];

  await expect(live).toHaveCount(2);
  await expect(live).toHaveText(['', '']);

  await tickOf(page, repair(3)).check();
  await expect(selection).toHaveText('1 selected');
  await expect(outcome).toHaveText('');

  await barOf(page).getByRole('button', { name: 'Clear from history' }).click();
  await dialogOf(page).getByRole('button', { name: 'Clear from history' }).click();
  await expect(outcome).toHaveText('1 repair cleared from the history');
  // The selection is spent, and the count says nothing about it.
  await expect(selection).toHaveText('');

  // What is written is the words alone. The outcome a person reads and acts on,
  // with its Dismiss button, is not itself a live region.
  const visible = outcomeOf(page);
  await expect(visible).toContainText('1 repair cleared from the history');
  await expect(visible.getByRole('button', { name: 'Dismiss' })).toBeVisible();
  await expect(visible).not.toHaveAttribute('aria-live', /.*/);
  await expect(visible).not.toHaveAttribute('role', 'status');
});

// A live region announces a change. Two clears that end in the same words would
// leave the text as it was, and the second would be said by nobody.
test('a second clear that ends in the same words is announced again', async ({ page }) => {
  await mockHistory(page);
  await goto(page, '/repairs', 'Eli repairs');
  await selecting(page);
  const outcome = historyOf(page).locator('[aria-live]').nth(1);

  const clearOne = async (r: Row) => {
    await tickOf(page, r).check();
    await barOf(page).getByRole('button', { name: 'Clear from history' }).click();
    await dialogOf(page).getByRole('button', { name: 'Clear from history' }).click();
    await expect(dialogOf(page)).toHaveCount(0);
  };

  await clearOne(repair(3));
  await expect(outcome).toHaveText('1 repair cleared from the history');
  await outcome.evaluate((el) => {
    const w = window as unknown as { writes: number };
    w.writes = 0;
    new MutationObserver((records) => (w.writes += records.length)).observe(el, {
      childList: true,
      characterData: true,
      subtree: true,
    });
  });

  await clearOne(repair(4));
  // Emptied, then written: two changes, where an unchanged string would be none.
  await expect
    .poll(() => page.evaluate(() => (window as unknown as { writes: number }).writes))
    .toBeGreaterThanOrEqual(2);
  await expect(outcome).toHaveText('1 repair cleared from the history');
});
