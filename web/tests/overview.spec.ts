/**
 * The Overview.
 *
 * This is the page an operator leaves open on a second monitor, so what it
 * protects is the promise that looking at it is enough: four numbers with an
 * hour of shape behind them, everything that needs a person listed with the
 * fix beside it, which pools cannot grow, and the scheduler's own words for
 * why runners appeared. The refresh button is here too, but what is protected
 * about it is that it is never load-bearing: the numbers arrive on their own,
 * and pressing it changes nothing about what the page says.
 */
import { expect, test } from '@playwright/test';
import { browserOverride, FIXTURE, goto } from './support/fixtures';

test.use(browserOverride);

test.beforeEach(async ({ page }) => {
  await goto(page, '/', 'Overview');
});

/** A count. The tile's value paragraph is the only text that is only digits. */
const COUNT = /^\d[\d,]*$/;
/** What formatDuration produces: "820ms", "9.4s", "35m 00s", "2h 05m", or "--". */
const DURATION = /^(--|[\d.]+(ms|s)|\d+[mhd] \d{2}[smh])$/;

test('the four metric tiles carry the numbers the fleet is judged on', async ({ page }) => {
  for (const label of ['Queued jobs', 'Running jobs', 'Live runners']) {
    const tile = page.getByRole('link', { name: new RegExp(`^${label}`) });
    await expect(tile, `${label} is on the Overview`).toBeVisible();
    await expect(tile.getByText(COUNT), `${label} shows a number`).toBeVisible();
  }

  const wait = page.getByRole('link', { name: /^Median queue wait/ });
  await expect(wait).toBeVisible();
  await expect(wait.getByText(DURATION)).toBeVisible();
  // The p95 is the reason the median is worth showing: one of them moving
  // without the other is the whole story of a fleet that is nearly coping.
  await expect(wait).toContainText(/p95/);
});

/**
 * The tiles are the first numbers anybody reads, and until now they counted
 * every job GitHub reported.
 *
 * On an organisation that also uses hosted runners -- which the fixture is --
 * that made "Queued jobs" and "Running jobs" partly somebody else's, so the
 * headline answer to "why is my fleet slow?" was a number nobody here could
 * act on. The tiles now default to this fleet's own work, like the panels
 * below them, and the same switch widens all of it.
 */
test("the metric tiles are this fleet's work, and the switch widens them", async ({ page }) => {
  const running = page.getByRole('link', { name: /^Running jobs/ });
  // The page header's own switch, not one of the panels': it governs the whole
  // page, and the tiles are the part of it nothing else controls.
  const header = page.locator('header').filter({ has: page.getByRole('heading', { level: 1 }) });
  const toggle = header.getByRole('switch', { name: 'Other runners' });

  await expect(toggle, 'the Overview carries the toggle its panels have').toBeVisible();
  await expect(toggle).not.toBeChecked();

  const ours = Number((await running.getByText(COUNT).innerText()).replace(/,/g, ''));

  // The fixture has one job running on a hosted-runner vendor. With the switch
  // off it is not in the tile; with it on it is.
  await toggle.click();
  await expect(toggle).toBeChecked();
  await expect
    .poll(async () => Number((await running.getByText(COUNT).innerText()).replace(/,/g, '')), {
      message: "the tile did not widen to include somebody else's running job",
    })
    .toBeGreaterThan(ours);

  // The tile links to the same jobs it just counted, or the number and the
  // list it leads to disagree.
  await expect(running).toHaveAttribute('href', /all=true/);

  // One preference, so the panels below moved with it.
  await expect(
    page
      .getByRole('region', { name: 'Active jobs', exact: true })
      .getByRole('switch', { name: 'Other runners' }),
  ).toBeChecked();

  await toggle.click();
  await expect(toggle).not.toBeChecked();
  await expect(running).toHaveAttribute('href', /^\/jobs\?state=in_progress$/);
  await expect
    .poll(async () => Number((await running.getByText(COUNT).innerText()).replace(/,/g, '')))
    .toBe(ours);
});

test('each trend tile carries a described sparkline', async ({ page }) => {
  // The wait tile is deliberately excluded: its series is built from live
  // `stats` frames rather than from GET /samples, so on a freshly started
  // controller it has one point and correctly draws no line.
  for (const label of ['Queued jobs', 'Running jobs', 'Live runners']) {
    const tile = page.getByRole('link', { name: new RegExp(`^${label}`) });
    const sparkline = tile.getByRole('img');
    await expect(sparkline, `${label} has a sparkline`).toBeVisible();
    // The description is the whole point: a line nobody can read is not data.
    await expect(sparkline).toHaveAttribute(
      'aria-label',
      new RegExp(`^${label}: .+ now, .+ at the start of the window, peaking at .+`),
    );
  }
});

/** Open the problems drawer the way an operator does: from the top bar. */
async function openProblems(page: import('@playwright/test').Page) {
  await page.getByRole('button', { name: /^Problems\./ }).click();
  const drawer = page.getByRole('dialog', { name: 'Problems' });
  await expect(drawer).toBeVisible();
  return drawer;
}

test('the Overview says how much needs a person without spending the page on it', async ({
  page,
}) => {
  // The whole point of the summary: the fleet's own panels are what the page
  // is for, and a settled configuration warning must not push them under the
  // fold. So what is on the page is a sentence and a way in -- never the list.
  await expect(page.getByText(/needs? your attention\.$/).first()).toBeVisible();
  await expect(page.getByRole('button', { name: 'Review' })).toBeVisible();

  // Nothing in the page's own content lists a problem's fix; that lives in the
  // drawer, which is not open yet.
  await expect(page.getByRole('main')).not.toContainText('no webhook has ever arrived');

  // And on a desktop the panels an operator actually watches are above the fold
  // with it, which is exactly what the full-height list used to cost. A phone
  // stacks everything and cannot promise this, so it is not asserted there.
  const viewport = page.viewportSize();
  if ((viewport?.width ?? 0) >= 1180) {
    const box = await page.getByRole('region', { name: 'Recent scaling' }).boundingBox();
    expect(box, 'the scaling feed is laid out').not.toBeNull();
    expect(box?.y ?? Infinity, 'the scaling feed starts within the first screen').toBeLessThan(
      viewport?.height ?? 0,
    );
  }
});

test('the top bar carries the count on every page and opens the list', async ({ page }) => {
  const bell = page.getByRole('button', { name: /^Problems\./ });
  // The label spells the counts out, because colour and a badge alone are not
  // information anyone can read aloud.
  await expect(bell).toHaveAccessibleName(/(error|warning|note)s? .*need/);

  await page.goto('/runners');
  await expect(bell, 'the count follows the operator off the Overview').toBeVisible();
  await openProblems(page);
});

test('the problems drawer names the seeded faults, with a fix for each', async ({ page }) => {
  const problems = await openProblems(page);

  // Four the seeded fleet always has: a controller that cannot be told about
  // queued work, a pool that weakens isolation two different ways, and a job
  // no pool will ever claim. Host heartbeats are deliberately not asserted on:
  // the fixture hosts have no agent behind them, so they fall silent 90s after
  // the controller starts and that entry appears part-way through a run.
  await expect(problems).toContainText('no webhook has ever arrived');
  await expect(problems).toContainText('authentication is disabled');
  await expect(problems).toContainText(
    `pool ${FIXTURE.armPool}: docker-in-docker sidecar: runners get a privileged container`,
  );
  await expect(problems).toContainText(
    `pool ${FIXTURE.armPool}: persistent runners: job state and credentials leak between workflow runs`,
  );
  await expect(problems).toContainText(/no enabled pool here claims \d+ queued jobs?/);

  // Errors are listed before warnings, so the worst thing is the first thing.
  await expect(problems.getByRole('heading', { level: 3 }).first()).toContainText(/error/);

  // Every entry says what is true, why it matters and what to change. Those
  // three lines have no roles that tell them apart, so they are read by class.
  const entries = problems.getByRole('listitem');
  await expect(entries.first()).toBeVisible();
  const lines = await entries.evaluateAll((items) =>
    items.map((item) => ({
      title: (item.querySelector('.title') as HTMLElement | null)?.innerText.trim() ?? '',
      detail: (item.querySelector('.detail') as HTMLElement | null)?.innerText.trim() ?? '',
      fix: (item.querySelector('.fix') as HTMLElement | null)?.innerText.trim() ?? '',
    })),
  );
  expect(lines.length).toBeGreaterThanOrEqual(5);
  for (const line of lines) {
    expect(line.title, 'every problem says what is wrong').not.toBe('');
    expect(line.detail, `"${line.title}" says why it matters`).not.toBe('');
    expect(line.fix, `"${line.title}" says what to change`).not.toBe('');
  }
});

test('a dismissed problem stops asking, and can be brought back', async ({ page }) => {
  const problems = await openProblems(page);
  const before = await problems.getByRole('listitem').count();

  const entry = problems.getByRole('listitem').filter({ hasText: 'authentication is disabled' });
  await entry.getByRole('button', { name: /^Dismiss:/ }).click();

  // Gone from the list, and off the count in the top bar.
  await expect(problems.getByRole('listitem')).toHaveCount(before - 1);
  await expect(problems).not.toContainText('authentication is disabled');

  // Never deleted, though: a dismissal is a decision, and a decision can be
  // undone. It is dated where it is listed.
  await problems.getByRole('button', { name: /Show 1 dismissed problem/ }).click();
  const dismissed = problems
    .getByRole('listitem')
    .filter({ hasText: 'authentication is disabled' });
  await expect(dismissed).toContainText('dismissed');
  await dismissed.getByRole('button', { name: /^Restore:/ }).click();
  await expect(problems.getByRole('listitem')).toHaveCount(before);

  // The controller is untouched by any of this: what an operator has read is a
  // browser preference, never fleet state, so an alerting rule still sees it.
  const api = await page.request.get('/api/v1/problems').then((r) => r.json());
  expect(
    (api.items ?? []).some((p: { code?: string }) => p.code === 'auth.disabled'),
    'the API still reports everything',
  ).toBe(true);
});

test('per-pool utilisation shows both pools and marks the one at its ceiling', async ({ page }) => {
  const pools = page.getByRole('region', { name: 'Pools' });
  await expect(pools).toBeVisible();
  await expect(pools.getByRole('link', { name: FIXTURE.linuxPool })).toBeVisible();
  await expect(pools.getByRole('link', { name: FIXTURE.armPool })).toBeVisible();

  const row = pools.getByRole('listitem').filter({ hasText: FIXTURE.linuxPool });
  // The floor and the ceiling are always on the row, so a full pool can be
  // told from a pool with room.
  await expect(row).toContainText('1–8 runners');

  // zoomies-demo-linux-x64 is seeded with eight live runners against a maximum of
  // eight and a job still queued for it: it cannot grow, and saying so is the
  // one thing this section exists for. It is asserted against the controller's
  // own numbers rather than against the fixture, because after about five
  // minutes the controller gives up on the two runners no agent ever collected
  // and the pool is then genuinely no longer at its ceiling. Either way the
  // page must agree with the server.
  const stats = await page.request.get('/api/v1/stats').then((response) => response.json());
  const pool = (stats.pools ?? []).find(
    (entry: { pool_name?: string }) => entry.pool_name === FIXTURE.linuxPool,
  );
  expect(pool, 'the controller knows this pool').toBeTruthy();
  const atCeiling = pool.max > 0 && pool.live >= pool.max && pool.queued > 0;

  if (atCeiling) {
    await expect(row).toContainText('At its ceiling');
    // Said again in the panel's own header, so it survives a glance.
    await expect(pools).toContainText('at the ceiling');
  } else {
    await expect(row).not.toContainText('At its ceiling');
  }
});

test('the scaling feed quotes the scheduler verbatim', async ({ page }) => {
  const feed = page.getByRole('region', { name: 'Recent scaling' });
  await expect(feed).toBeVisible();
  // Paraphrasing the one sentence that explains why a runner exists is how a
  // dashboard stops being trustworthy, so the reason string is matched as the
  // scheduler writes it.
  await expect(feed).toContainText(/scaled zoomies-demo-linux-x64 \d+ -> \d+: /);
  await expect(feed).toContainText('scaled zoomies-demo-linux-x64 1 -> 4: 3 jobs queued > 30s');
  await expect(feed).toContainText('scaled zoomies-demo-linux-arm64 0 -> 1: 1 job queued > 30s');
  await expect(feed.getByRole('listitem').first()).toBeVisible();
});

test('the scaling feed sits beside the pools and the running jobs, never under them', async ({
  page,
}) => {
  // A fleet with one pool and ten decisions used to show the pools, then a
  // screen of blank space, then the running jobs: the feed set the height of
  // the row it shared with the pools. Now the jobs share the pools' column
  // and the feed is cut to that column's height, scrolling inside itself for
  // the rest. A phone stacks the panels, so there is nothing to assert there.
  const viewport = page.viewportSize();
  test.skip((viewport?.width ?? 0) < 1180, 'the panels stack on a narrow screen');

  const pools = page.getByRole('region', { name: 'Pools', exact: true });
  const jobs = page.getByRole('region', { name: 'Active jobs', exact: true });
  const feed = page.getByRole('region', { name: 'Recent scaling', exact: true });
  // Both lists load after the page does; measure them full, not as skeletons.
  await expect(jobs.getByRole('listitem').first()).toBeVisible();
  await expect(feed.getByRole('listitem').first()).toBeVisible();

  const poolsBox = await pools.boundingBox();
  const jobsBox = await jobs.boundingBox();
  const feedBox = await feed.boundingBox();
  expect(poolsBox).not.toBeNull();
  expect(jobsBox).not.toBeNull();
  expect(feedBox).not.toBeNull();
  if (!poolsBox || !jobsBox || !feedBox) return;

  // The jobs are under the pools and beside the feed.
  expect(jobsBox.y, 'the jobs come after the pools').toBeGreaterThan(poolsBox.y + poolsBox.height);
  expect(jobsBox.x + jobsBox.width, "the jobs share the pools' column").toBeLessThanOrEqual(
    feedBox.x,
  );
  // The feed ends where that column ends. A pixel of slack for rounding.
  expect(feedBox.y + feedBox.height, 'the feed is no taller than its column').toBeLessThanOrEqual(
    jobsBox.y + jobsBox.height + 1,
  );
  // And the fixture's ten decisions do not all fit in that height, so the
  // ones off the end are reachable by scrolling the panel, not gone.
  const list = feed.getByRole('list');
  const hidden = await list.evaluate(
    (el) => (el.parentElement?.scrollHeight ?? 0) - (el.parentElement?.clientHeight ?? 0),
  );
  expect(hidden, 'the decisions that do not fit are a scroll away').toBeGreaterThan(0);
  await feed.getByRole('listitem').last().scrollIntoViewIfNeeded();
  const scrolled = await list.evaluate((el) => el.parentElement?.scrollTop ?? 0);
  expect(scrolled, 'the panel itself scrolls to reach the oldest').toBeGreaterThan(0);
});

test('refreshing the Overview leaves it saying exactly what it said', async ({ page }) => {
  // The gesture exists for the operator who wants to be sure, so the thing
  // worth protecting is that it is not how the page keeps up: a press must not
  // blank the tiles, re-run their skeletons, or change a single number that
  // the stream had already put there.
  const live = page.getByRole('link', { name: /^Live runners/ });
  await expect(live).toBeVisible();
  const before = await live.textContent();

  const refresh = page.getByRole('button', { name: 'Refresh' });
  await expect(refresh).toBeVisible();
  await refresh.click();

  // Nothing goes behind a skeleton on the way through: the last known truth
  // stays on screen while the reconcile is in flight.
  await expect(live).toBeVisible();
  await expect(refresh).not.toHaveAttribute('aria-busy', 'true');
  await expect(live).toHaveText(before ?? '');
});

/*
 * The recent past is where "is CI broken?" gets answered, and the panel has to
 * do it without a click: the outcome, where it went wrong, and whether the
 * fleet or the workflow is to blame.
 */
test('recent outcomes name the failures and blame the right party', async ({ page }) => {
  const outcomes = page.getByRole('region', { name: 'Recent outcomes' });
  await expect(outcomes).toBeVisible();
  const rows = outcomes.getByRole('listitem');
  await expect(rows.first()).toBeVisible();

  // A step failure says the step; a lost runner says so, and is badged.
  await expect(outcomes.getByText(/^at /).first()).toBeVisible();
  await expect(outcomes).toContainText('Success');
  await expect(outcomes.getByRole('link', { name: 'Every failed job' })).toHaveAttribute(
    'href',
    '/jobs?failed=true',
  );

  // Running jobs stay in their own panel beside it.
  await expect(page.getByRole('region', { name: 'Active jobs' })).toBeVisible();
});

/*
 * Whose jobs these are.
 *
 * GitHub reports every job in the repositories an installation covers, so an
 * organisation part-way through a migration -- or one that keeps a hosted or
 * vendor runner for a couple of workflows -- has jobs on this page that no
 * runner here has ever seen. The panels used to list them under "what the
 * fleet is running", which is a claim about somebody else's machine.
 */
test("the job panels are this fleet's work, and say so when they are not", async ({ page }) => {
  const active = page.getByRole('region', { name: 'Active jobs', exact: true });
  const outcomes = page.getByRole('region', { name: 'Recent outcomes', exact: true });
  await expect(active.getByRole('listitem').first()).toBeVisible();

  // By default both panels are the fleet's own work: the fixture's two vendor
  // jobs, one running and one finished, are not on the page at all.
  await expect(active).toContainText('What this fleet is running at this moment.');
  await expect(active).not.toContainText('blacksmith');
  await expect(outcomes).not.toContainText('blacksmith');
  await expect(active.getByText('Elsewhere')).toHaveCount(0);

  // One switch, shared: flipping it on either panel widens both.
  await active.getByRole('switch', { name: 'Other runners' }).click();

  await expect(active).toContainText('wherever it is running');
  const vendor = active.getByRole('listitem').filter({ hasText: 'blacksmith' });
  await expect(vendor).toHaveCount(1);
  // And it is marked, because the row above it is this fleet's and looks the
  // same otherwise.
  await expect(vendor.getByText('Elsewhere')).toBeVisible();
  await expect(outcomes.getByRole('switch', { name: 'Other runners' })).toBeChecked();

  // The choice is the operator's, and it survives a reload.
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
  await expect(
    page
      .getByRole('region', { name: 'Active jobs', exact: true })
      .getByRole('switch', { name: 'Other runners' }),
  ).toBeChecked();

  // Off again, and the page is the fleet's own once more.
  await page
    .getByRole('region', { name: 'Active jobs', exact: true })
    .getByRole('switch', { name: 'Other runners' })
    .click();
  await expect(page.getByRole('region', { name: 'Active jobs', exact: true })).not.toContainText(
    'blacksmith',
  );
});

test('the subtitle names the window the figures actually cover', async ({ page }) => {
  // The page used to promise that "trends and waits cover the last hour". The
  // trends do; the waits and the outcomes are computed over the controller's
  // stats window, a day by default, so the sentence described a figure the page
  // was not showing. It comes from the payload now, which is the only way it
  // stays true when the window is not the default.
  const stats = (await page.request.get('/api/v1/stats').then((r) => r.json())) as Record<
    string,
    unknown
  >;
  await page.route('**/api/v1/stats*', (route) =>
    route.fulfill({
      status: 200,
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ ...stats, window: '6h0m0s' }),
    }),
  );
  // And no stream, so the controller's own window cannot arrive and overwrite
  // the one under test a second later.
  await page.route('**/api/v1/events*', (route) =>
    route.fulfill({
      status: 200,
      headers: { 'content-type': 'text/event-stream', 'cache-control': 'no-store' },
      body: '',
    }),
  );

  await goto(page, '/', 'Overview');
  const subtitle = page.locator('.subtitle').first();
  await expect(subtitle).toContainText('the last 6 hours');
  await expect(subtitle, 'the sparklines are still an hour, and say so separately').toContainText(
    'Trends cover the last hour',
  );
});
