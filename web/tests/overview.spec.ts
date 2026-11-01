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
import { expect, test, type Page } from '@playwright/test';
import { FIXTURE, goto, readThrough, reload, useClassic } from './support/fixtures';

test.beforeEach(async ({ page }) => {
  // These checks exercise the detailed panels. Default disclosure state and
  // the compact monitoring layout have their own Calm operations tests.
  await page.addInitScript(() => {
    for (const id of ['history', 'failures'])
      localStorage.setItem(`zoomies.overview.${id}.open`, 'true');
    localStorage.setItem('zoomies.overview.events.expanded', 'true');
  });
  await goto(page, '/', 'Overview');
});

/**
 * Widen the activity matrix to the year.
 *
 * The matrix opens on today, by the hour, like every other range control; the
 * tests below are about the calendar of days, so they ask for it.
 */
async function yearOfDays(page: Page) {
  const matrix = page.getByRole('region', { name: 'Activity matrix', exact: true });
  await matrix
    .getByRole('group', { name: 'Range' })
    .getByRole('button', { name: 'The last year' })
    .click();
  await expect(matrix.getByRole('grid')).toHaveAccessibleName(/one square per day/);
}

/** A count. The tile's value paragraph is the only text that is only digits. */
const COUNT = /^\d[\d,]*$/;
/** What formatDuration produces: "820ms", "9.4s", "35m 00s", "2h 05m", or "--". */
const DURATION = /^(--|[\d.]+(ms|s)|\d+[mhd] \d{2}[smh])$/;

// A square that has jobs, however they went: "38 jobs finished, 2 failed" or
// "6 jobs queued, none finished". Both halves matter -- for the twenty minutes
// after midnight the newest day is all queue and no completions, because the
// demo fleet's most recent finished job is twenty minutes old and lands on
// yesterday, and a square that says "none finished" is still a square with
// jobs on it.
const WITH_JOBS = /\d[\d,]* jobs? (finished|queued)/;

test('the four metric tiles carry the numbers the fleet is judged on', async ({ page }) => {
  for (const label of ['Queued jobs', 'Running jobs', 'Live runners']) {
    const tile = page.getByRole('link', { name: new RegExp(`^${label}`) });
    await expect(tile, `${label} is on the Overview`).toBeVisible();
    await expect(tile.getByText(COUNT), `${label} shows a number`).toBeVisible();
  }

  const wait = page.getByRole('link', { name: /^Median queue wait/ });
  await expect(wait).toBeVisible();
  await expect(wait.getByText(DURATION)).toBeVisible();

  // Each figure lands on the rows it counted. The live figure is the four live
  // states and not the removed history a bare /runners would include, and a
  // queue wait is only measured on a job that finished, so the wait figure asks
  // for the completed ones in the window the controller reported.
  await expect(page.getByRole('link', { name: /^Live runners/ })).toHaveAttribute(
    'href',
    /^\/runners\?state=provisioning&state=registering&state=idle&state=busy#runners-list$/,
  );
  await expect(wait).toHaveAttribute('href', /^\/jobs\?state=completed&since=/);
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
  const toggle = header.getByRole('combobox', { name: 'Overview scope' });

  await expect(toggle, 'the Overview carries the toggle its panels have').toBeVisible();
  await expect(toggle).toHaveValue('fleet');

  const ours = Number((await running.getByText(COUNT).innerText()).replace(/,/g, ''));

  // The fixture has one job running on a hosted-runner vendor. With the switch
  // off it is not in the tile; with it on it is.
  await toggle.selectOption('all');
  await expect(toggle).toHaveValue('all');
  await expect
    .poll(async () => Number((await running.getByText(COUNT).innerText()).replace(/,/g, '')), {
      message: "the tile did not widen to include somebody else's running job",
    })
    .toBeGreaterThan(ours);

  // Somebody else's running job is not on a busy runner of this fleet, so the
  // wider figure opens the jobs themselves; the fleet's own opens its runners.
  await expect(running).toHaveAttribute('href', '/jobs?state=in_progress&all=true');

  // The single page scope controls the list and the headline together.
  await expect(page.getByRole('region', { name: 'Active jobs', exact: true })).toContainText(
    'Wherever it is running.',
  );

  await toggle.selectOption('fleet');
  await expect(toggle).toHaveValue('fleet');
  await expect(running).toHaveAttribute('href', '/runners?state=busy#runners-list');
  await expect
    .poll(async () => Number((await running.getByText(COUNT).innerText()).replace(/,/g, '')))
    .toBe(ours);
});

/**
 * The queued tile leads to the Queue, not to the Jobs page, and that is
 * deliberate.
 *
 * It counts this fleet's queued work less whatever an operator has removed
 * from the queue, which is exactly the Queue's default view. The Jobs page is
 * history: it has no provisioning filter, so a job removed from the queue sits
 * in it looking like any other queued one, and the tile and the list behind it
 * would disagree the moment somebody cleared a queue. With other runners on
 * the tile is counting queues this fleet does not own, which the Queue has
 * nothing to say about, so that view goes to the Jobs page as the others do.
 */
test('the queued tile leads to the queue it just counted', async ({ page }) => {
  const queued = page.getByRole('link', { name: /^Queued jobs/ });
  await expect(queued).toHaveAttribute('href', '/queue');

  const header = page.locator('header').filter({ has: page.getByRole('heading', { level: 1 }) });
  const toggle = header.getByRole('combobox', { name: 'Overview scope' });
  await toggle.selectOption('all');
  await expect(toggle).toHaveValue('all');
  await expect(queued).toHaveAttribute('href', '/jobs?state=queued&all=true');

  await toggle.selectOption('fleet');
  await expect(toggle).toHaveValue('fleet');
  await expect(queued).toHaveAttribute('href', '/queue');
});

/**
 * The running tile is a shortcut into the rows it counted, not a trip to the
 * top of the Runners page.
 *
 * Landing at the top leaves the reader to scroll past a band of figures and a
 * provisioning strip to reach the busy runners the number was counting, which
 * is the one thing they came for; the anchor means the list arrives already
 * under the eye and holding the keyboard. The Runners band's own Running
 * figure has always worked that way on its own page.
 */
test('the running tile lands on the busy runners themselves', async ({ page }) => {
  await page.getByRole('link', { name: /^Running jobs/ }).click();
  await expect(page).toHaveURL(/\/runners\?state=busy#runners-list$/);
  await expect(page.getByRole('heading', { name: 'Runners', level: 1 })).toBeVisible();
  await expect(page.locator('#runners-list')).toBeFocused();
});

/*
 * The four figures are the band's own figures, as on every other page that
 * opens on one, and not a second row of numbers under the hero. Moving them
 * there gives the page back a row of height, and a figure in a band is a way
 * into the records it counts rather than a card that happens to be a link. A
 * phone never draws the band, so it gets the same figures as a plain panel.
 */
test('the Overview carries the four figures in one strip, each a way into its records', async ({
  page,
}) => {
  const figures = page.getByRole('list', { name: 'Key figures' });
  for (const label of ['Queued jobs', 'Running jobs', 'Live runners', 'Median queue wait']) {
    await expect(
      figures.getByRole('link', { name: new RegExp(`^${label}`) }),
      `${label} is in the Overview figures`,
    ).toBeVisible();
  }
  // The p95 and the capacity are context the figure cannot carry in its number,
  // and they are on the page rather than only in the tooltip.
  await expect(figures).toContainText(/p95/);
  await expect(figures).toContainText(/slots on/);
});

/** Open the problems drawer the way an operator does: from the top bar. */
async function openProblems(page: import('@playwright/test').Page) {
  await page.getByRole('button', { name: /^Problems\./ }).click();
  const drawer = page.getByRole('dialog', { name: 'Problems' });
  await expect(drawer).toBeVisible();
  return drawer;
}

test('the Overview says what needs a person in three rows at most, and the rest is one click away', async ({
  page,
}) => {
  // The card lists the worst three with the fix beside each, as the mock
  // draws it; everything past three is the drawer's, so a settled
  // configuration warning never pushes the fleet below the fold.
  // A phone has no band and keeps the pools on its first screen, so it keeps
  // the one-line summary; the card is for the screens with room for it.
  test.skip((page.viewportSize()?.width ?? 0) < 769, 'a phone keeps the summary line');
  const card = page.getByRole('region', { name: 'Needs attention', exact: true });
  await expect(card).toBeVisible();
  const rows = card.getByRole('listitem');
  await expect(rows.first()).toBeVisible();
  expect(await rows.count()).toBeLessThanOrEqual(3);
  // The worst first: the fixture seeds errors, so the first row is one.
  await expect(rows.first()).toContainText('Error');
  // The fix is on the row now, where the drawer had it alone.
  await expect(rows.first().locator('.fix')).not.toBeEmpty();
  // The rest are behind the drawer, which the card's own action opens.
  const problems = await page.request.get('/api/v1/problems').then((r) => r.json());
  const active = (problems.items ?? []).length;
  const action = card.getByRole('button', {
    name: active > 3 ? `and ${active - 3} more` : 'Open the drawer',
  });
  await expect(action).toBeVisible();
  await action.click();
  await expect(page.getByRole('dialog', { name: 'Problems' })).toBeVisible();
  await page.keyboard.press('Escape');
  const viewport = page.viewportSize();
  if ((viewport?.width ?? 0) >= 1180) {
    const box = await page.getByRole('button', { name: 'Review' }).boundingBox();
    expect(box?.y ?? Infinity, 'the way in is within the first screen').toBeLessThan(
      viewport?.height ?? 0,
    );
  }
});

test('a problem row opens the page that holds the thing', async ({ page }) => {
  test.skip((page.viewportSize()?.width ?? 0) < 769, 'a phone keeps the summary line');
  const card = page.getByRole('region', { name: 'Needs attention', exact: true });
  const open = card.getByRole('link', { name: /^Open/ }).first();
  await expect(open).toBeVisible();
  await expect(open).toHaveAttribute('href', /^\//);
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
  await entry.getByRole('button', { name: /^Dismiss or snooze:/ }).click();
  await page.getByRole('menuitem', { name: 'Dismiss until resolved' }).click();

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

  // The controller is untouched by any of this: what an operator has read belongs
  // to their account, never to the fleet, so an alerting rule still sees it.
  const api = await page.request.get('/api/v1/problems').then((r) => r.json());
  expect(
    (api.items ?? []).some((p: { code?: string }) => p.code === 'auth.disabled'),
    'the API still reports everything',
  ).toBe(true);
});

test('a problem can be snoozed for a fixed while instead of dismissed outright', async ({
  page,
}) => {
  const problems = await openProblems(page);
  const before = await problems.getByRole('listitem').count();

  const entry = problems.getByRole('listitem').filter({ hasText: 'authentication is disabled' });
  await entry.getByRole('button', { name: /^Dismiss or snooze:/ }).click();
  // Several durations are offered, not just one: an operator who knows this
  // will resolve itself in an hour should not have to pick "until resolved".
  await expect(page.getByRole('menuitem', { name: 'Snooze for 15 minutes' })).toBeVisible();
  await expect(page.getByRole('menuitem', { name: 'Snooze for 1 hour' })).toBeVisible();
  await expect(page.getByRole('menuitem', { name: 'Snooze for 4 hours' })).toBeVisible();
  await page.getByRole('menuitem', { name: 'Snooze for 24 hours' }).click();

  // Gone from the active list, same as an outright dismissal.
  await expect(problems.getByRole('listitem')).toHaveCount(before - 1);
  await expect(problems).not.toContainText('authentication is disabled');

  // Filed under dismissed, but it says when it comes back rather than just
  // when it was put away -- a snooze is a dated decision with a deadline.
  await problems.getByRole('button', { name: /Show 1 dismissed problem/ }).click();
  const snoozed = problems.getByRole('listitem').filter({ hasText: 'authentication is disabled' });
  await expect(snoozed).toContainText('dismissed');
  await expect(snoozed).toContainText(/back in/i);

  await snoozed.getByRole('button', { name: /^Restore:/ }).click();
  await expect(problems.getByRole('listitem')).toHaveCount(before);
});

test('a snooze can cover every problem of the same kind, not just the one', async ({ page }) => {
  const problems = await openProblems(page);
  const before = await problems.getByRole('listitem').count();

  const entry = problems.getByRole('listitem').filter({ hasText: 'authentication is disabled' });
  await entry.getByRole('button', { name: /^Dismiss or snooze:/ }).click();
  // Both scopes, for every duration: a planned outage that makes every pool
  // short of capacity should be put away once, not once per pool.
  await expect(page.getByRole('menuitem', { name: 'Snooze for 1 hour' })).toBeVisible();
  await page.getByRole('menuitem', { name: 'Snooze all like this for 1 hour' }).click();

  await expect(problems.getByRole('listitem')).toHaveCount(before - 1);
  await problems.getByRole('button', { name: /Show 1 dismissed problem/ }).click();
  const snoozed = problems.getByRole('listitem').filter({ hasText: 'authentication is disabled' });
  await expect(snoozed).toContainText('snoozed with all like it');
  await expect(snoozed).toContainText(/back in/i);

  await snoozed.getByRole('button', { name: /^Restore:/ }).click();
  await expect(problems.getByRole('listitem')).toHaveCount(before);
});

test('dismissing the last problem closes the drawer', async ({ page }) => {
  const problems = await openProblems(page);

  // "Dismiss all" clears every active entry in one action, so there is
  // nothing left the drawer needs to keep the operator looking at.
  await problems.getByRole('button', { name: 'Dismiss all' }).click();
  await expect(page.getByRole('dialog', { name: 'Problems' })).not.toBeVisible();
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
  // A card says its runners and its waiting work as a pair of facts, so the
  // one at its ceiling can say where the waiting work stands.
  await expect(row).toContainText(/Runners\s*\d+ of \d+/);
  await expect(row).toContainText(atCeiling ? /Waiting\s*\d+ and nowhere to run/ : /Waiting\s*\d+/);
});

test('the pool cards fit a phone with a long name and never widen the page', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 800 });
  const pools = page.getByRole('region', { name: 'Pools', exact: true });
  await expect(pools.getByRole('listitem').first()).toBeVisible();
  const [display, columns] = await pools
    .locator('ul')
    .first()
    .evaluate((el) => {
      const s = getComputedStyle(el);
      return [s.display, s.gridTemplateColumns.split(' ').length];
    });
  expect(display).toBe('grid');
  expect(columns).toBe(1);
  expect(
    await page.evaluate(() => document.documentElement.scrollWidth - innerWidth),
  ).toBeLessThanOrEqual(0);
});

test('the events feed quotes the scheduler verbatim', async ({ page }) => {
  const feed = page.getByRole('region', { name: 'Recent events' });
  await expect(feed).toBeVisible();
  // Paraphrasing the one sentence that explains why a runner exists is how a
  // dashboard stops being trustworthy, so the reason string is matched as the
  // scheduler writes it.
  //
  // The newest three rather than the panel's whole length: the feed carries
  // every kind of news now, so a decision and a runner that failed compete for
  // the same dozen lines, and the seeded decisions older than an hour are
  // legitimately below the fold.
  const response = await page.request.get('/api/v1/scaling-events?limit=3');
  expect(response.ok()).toBe(true);
  const events = (await response.json()).items as Array<{ reason?: string }>;
  expect(events.length).toBeGreaterThan(0);
  for (const event of events) {
    expect(event.reason, "every scaling event carries the scheduler's sentence").toBeTruthy();
    await expect(feed).toContainText(event.reason!);
  }
  await expect(feed.getByRole('listitem').first()).toBeVisible();
});

test('the events feed carries the fleet’s other news, not only the scheduler’s', async ({
  page,
}) => {
  // The panel used to be the scheduler's decisions and nothing else, with how
  // the last jobs ended in a second list across the bottom of the page. Both
  // are this one list now: a job ending is news like anything else.
  //
  // Asserted with the decisions switched off, because a fleet that is scaling
  // writes a line every few minutes and the newest fifteen lines are the
  // panel's whole length: what is being tested is that the other kinds are
  // there at all, not where an hour-old job sits in a list that keeps moving.
  // That the switch works is the Events page's own test.
  await goto(page, '/settings/events', 'Events');
  await page.getByRole('switch', { name: 'Scaling decisions' }).click();
  await goto(page, '/', 'Overview');

  const feed = page.getByRole('region', { name: 'Recent events', exact: true });
  await expect(feed.getByRole('listitem').first()).toBeVisible();
  await expect(feed).not.toContainText('scaled zoomies-demo-');

  // The recent past is where "is CI broken?" gets answered, and it has to be
  // answered without a click: how the job ended, where it went wrong, and
  // whether the fleet or the workflow is to blame.
  await expect(feed, 'the jobs that went well are here too').toContainText('A job succeeded');
  await expect(feed).toContainText('A job failed');
  await expect(feed.getByText(/^at .+ · /).first(), 'a step failure names its step').toBeVisible();
  // And the line the outcomes panel carried: where it came from, and how long
  // it took. The shape rather than a particular repository and branch: the
  // seed hands those out by run, and which runs are among the newest fifteen
  // lines is the seed's business rather than this test's.
  await expect(feed.getByText(/ · acme\/[a-z]+ · [\w./-]+ · \d/).first()).toBeVisible();
  // The fleet owning up to its own failure: the runner died under the job, so
  // the failure is not the workflow's, and GitHub records both the same way.
  await expect(feed).toContainText(/A job.s runner stopped under it/);
});

test('an empty feed says when it is the operator’s own choices that emptied it', async ({
  page,
}) => {
  // The other half of the first-run spec's "blames no filter": here the fleet
  // has plenty to say and every kind is switched off, so the panel is empty
  // for exactly the reason it gives, and says how many lines it is holding
  // back so that "empty" and "silenced" cannot be mistaken for each other.
  await goto(page, '/settings/events', 'Events');
  const switches = page.getByRole('switch');
  const count = await switches.count();
  expect(count, 'the Events page lists the kinds').toBeGreaterThan(0);
  for (let i = 0; i < count; i++) {
    const kind = switches.nth(i);
    if (await kind.isChecked()) await kind.click();
  }
  await goto(page, '/', 'Overview');

  const feed = page.getByRole('region', { name: 'Recent events', exact: true });
  await expect(feed).toContainText('Nothing in the kinds you are watching');
  await expect(feed.getByRole('link', { name: 'Choose' })).toBeVisible();
  // Every kind, counted from the page rather than written down: how many a
  // release ships is not what this is about.
  await expect(feed).toContainText(
    new RegExp(
      `\\d+ events? (is|are) not shown, because ${count} kinds of event are switched off for this browser`,
    ),
  );
  await expect(feed).not.toContainText('Nothing has happened yet');
});

test('the events feed sits beside the pools and the running jobs, never under them', async ({
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
  const feed = page.getByRole('region', { name: 'Recent events', exact: true });
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
  await feed.getByRole('button', { name: 'Show fewer events' }).click();
  await expect(feed.getByRole('listitem')).toHaveCount(3);
  await feed.getByRole('button', { name: 'Expand events' }).click();
  await expect.poll(() => feed.getByRole('listitem').count()).toBeGreaterThan(3);
  await feed.getByRole('listitem').last().scrollIntoViewIfNeeded();
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
  const feed = page.getByRole('region', { name: 'Recent events', exact: true });
  await expect(active.getByRole('listitem').first()).toBeVisible();

  // By default both panels are the fleet's own work: the fixture's two vendor
  // jobs, one running and one finished, are not on the page at all.
  await expect(page.getByRole('combobox', { name: 'Overview scope' })).toHaveValue('fleet');
  await expect(active).not.toContainText('blacksmith');
  await expect(
    feed,
    'the feed carries the finished ones, and holds to the same rule',
  ).not.toContainText('blacksmith');
  await expect(active.getByText('Elsewhere')).toHaveCount(0);

  // One switch, shared: flipping it on either panel widens both.
  await page.getByRole('combobox', { name: 'Overview scope' }).selectOption('all');

  await expect(active).toContainText('Wherever it is running');
  const vendor = active.getByRole('listitem').filter({ hasText: 'blacksmith' });
  await expect(vendor).toHaveCount(1);
  // And it is marked, because the row above it is this fleet's and looks the
  // same otherwise.
  await expect(vendor.getByText('Elsewhere')).toBeVisible();

  // The choice is the operator's, and it survives a reload.
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Overview scope' })).toHaveValue('all');
  await page.getByRole('combobox', { name: 'Overview scope' }).selectOption('fleet');
  await expect(page.getByRole('region', { name: 'Active jobs', exact: true })).not.toContainText(
    'blacksmith',
  );
});

test('the subtitle names the window the figures actually cover', async ({ page }) => {
  // The page used to promise that "trends and waits cover the last hour". The
  // waits and the outcomes are computed over the controller's stats window, a
  // day by default, so the sentence described a figure the page was not
  // showing. It comes from the payload now, which is the only way it stays true
  // when the window is not the default.
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
  const subtitle = page.locator('.coverage');
  await expect(subtitle).toContainText('Waits and outcomes cover the last 6 hours');
});

/*
 * The activity matrix: a year of the fleet's days as one band of squares at
 * the top of the page. What it protects is that the band is data and not
 * decoration -- every square says its figures in words, the keyboard can
 * walk it, and selecting a day leads to that day's jobs -- and that it sits
 * above the numbers, where the operator asked for it.
 */
test('the activity matrix follows current work, and every square says what it holds', async ({
  page,
}) => {
  // Classic and Mission Control both put current work before history.
  await useClassic(page);
  await goto(page, '/', 'Overview');
  await yearOfDays(page);
  const matrix = page.getByRole('region', { name: 'Activity matrix', exact: true });
  await expect(matrix).toBeVisible();
  const grid = matrix.getByRole('grid');
  await expect(grid).toHaveAccessibleName(/weeks of this fleet's jobs, one square per day/);

  // Above the metric tiles, on every width.
  const tile = page.getByRole('link', { name: /^Queued jobs/ });
  const matrixBox = await matrix.boundingBox();
  const tileBox = await tile.boundingBox();
  expect(matrixBox).not.toBeNull();
  expect(tileBox).not.toBeNull();
  expect(matrixBox!.y, 'current figures come before history').toBeGreaterThan(tileBox!.y);

  // The seed's morning of jobs is in the newest squares. A square's
  // accessible name is the whole sentence the tooltip shows, so nothing
  // lives only in the tooltip.
  const squares = grid.getByRole('gridcell');
  expect(await squares.count()).toBeGreaterThan(7);
  const busy = grid.locator(
    '[role="gridcell"][data-kind="failing"], [role="gridcell"][data-kind="healthy"]',
  );
  await expect(busy.first()).toBeVisible();
  await expect(busy.last()).toHaveAttribute('aria-label', /\d+ jobs? finished.*succeeded.*failed/);

  // The header sums the squares on screen, and the seed has failures.
  await expect(matrix).toContainText(/\d+ jobs? finished/);
  await expect(matrix.locator('.badge')).toHaveText(/^\s*\d+ failed$/);

  // The band never widens the page: it is cut to the width it has.
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('the matrix is one tab stop, walked with the arrow keys, with a tooltip on every square', async ({
  page,
}) => {
  await yearOfDays(page);
  const matrix = page.getByRole('region', { name: 'Activity matrix', exact: true });
  const grid = matrix.getByRole('grid');
  await expect(grid).toBeVisible();

  // Exactly one square is in the tab order, and it is the newest with jobs.
  const stop = grid.locator('[role="gridcell"][tabindex="0"]');
  await expect(stop).toHaveCount(1);
  await expect(stop).toHaveAttribute('aria-label', WITH_JOBS);

  // Reached the way the keyboard reaches it: a script's focus after a click is not
  // keyboard focus, and the card opens for the keyboard only.
  await stop.focus();
  await page.keyboard.press('Shift+Tab');
  await page.keyboard.press('Tab');
  await expect(stop).toBeFocused();
  const tip = page.locator('.bubble:popover-open');
  await expect(tip, 'focus opens the tooltip').toBeVisible();
  await expect(tip).toContainText(WITH_JOBS);
  await expect(tip).toContainText('Queued');
  await expect(tip).toContainText('Executing');

  // Left is the same weekday a week earlier; the tooltip follows the focus.
  const before = await stop.getAttribute('aria-label');
  await page.keyboard.press('ArrowLeft');
  const focused = grid.locator('[role="gridcell"]:focus');
  await expect(focused).toHaveCount(1);
  expect(await focused.getAttribute('aria-label')).not.toBe(before);
  await expect(page.locator('.bubble:popover-open')).toContainText(
    (await focused.getAttribute('aria-label'))!.split('.')[0]!,
  );
  // And back, so the rest of the test is about today.
  await page.keyboard.press('ArrowRight');
  await expect(grid.locator('[role="gridcell"]:focus')).toHaveAttribute('aria-label', before!);

  // Escape lets go of the tooltip and nothing else: the page is still here.
  await page.keyboard.press('Escape');
  await expect(page.locator('.bubble:popover-open')).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
});

test('selecting a day opens its hours and leads to its jobs', async ({ page }) => {
  await yearOfDays(page);
  const matrix = page.getByRole('region', { name: 'Activity matrix', exact: true });
  const grid = matrix.getByRole('grid');
  // The newest day that finished something and failed some of it, which is
  // what every assertion below needs: the outcome split, the hour-by-hour
  // summary and the "Failed jobs" link are all rendered from completions. It
  // is the tab stop itself for all but the twenty minutes after midnight --
  // in those the newest day is all queue, the demo fleet's most recent
  // finished job having landed on yesterday -- so a day picked by the clock
  // rather than by its contents would make this test a nightly flake.
  const failing = grid.locator('[role="gridcell"][data-kind="failing"]');
  await expect(failing.first()).toBeAttached();
  const newest = await failing.evaluateAll((cells) =>
    Math.max(...cells.map((c) => Number(c.getAttribute('data-index')))),
  );
  const day = grid.locator(`[role="gridcell"][data-index="${newest}"]`);
  await expect(day).toHaveAttribute('aria-label', WITH_JOBS);

  await day.click();
  await expect(day).toHaveAttribute('aria-selected', 'true');
  const detail = matrix.getByRole('region', { name: 'Selected day' });
  await expect(detail).toBeVisible();
  await expect(detail).toContainText(WITH_JOBS);
  await expect(detail).toContainText('Succeeded');
  await expect(detail).toContainText('Failed');

  // The hour-by-hour breakdown is fetched on demand and described in words.
  const hours = detail.getByRole('img', { name: /^Hour by hour:/ });
  await expect(hours).toBeVisible();
  // The clock is the browser's own, so it may carry a meridiem.
  await expect(hours).toHaveAccessibleName(/busiest at \d\d:\d\d( [AP]M)? with \d+ finished/);

  // The links are cut to the day, and the Jobs page honours them.
  const jobs = detail.getByRole('link', { name: 'Jobs that day' });
  await expect(jobs).toHaveAttribute(
    'href',
    /^\/jobs\?since=\d{4}-\d\d-\d\d&until=\d{4}-\d\d-\d\d$/,
  );
  await expect(detail.getByRole('link', { name: 'Failed jobs' })).toHaveAttribute(
    'href',
    /^\/jobs\?failed=true&since=/,
  );
  await expect(detail.getByRole('link', { name: 'Usage report for the day' })).toHaveAttribute(
    'href',
    /^\/usage\?since=\d{4}-\d\d-\d\d&until=\d{4}-\d\d-\d\d$/,
  );

  // Selecting the same square again lets go of it.
  await day.click();
  await expect(day).toHaveAttribute('aria-selected', 'false');
  await expect(matrix.getByRole('region', { name: 'Selected day' })).toHaveCount(0);

  await day.click();
  await jobs.click();
  await expect(page.getByRole('heading', { name: 'Jobs' })).toBeVisible();
  await expect(page.locator('tbody tr[data-row]').first()).toBeVisible();
  await expect(page).toHaveURL(/since=/);
});

test('the matrix can be coloured by a different figure', async ({ page }) => {
  const matrix = page.getByRole('region', { name: 'Activity matrix', exact: true });
  // The demo fleet's jobs are written once, when the controller starts, over
  // the six hours before it. A suite that starts before midnight and gets here
  // after it finds today's squares empty of anything queued, so this asks for
  // a window that holds those hours whatever the clock says. The test is about
  // colouring, not about today.
  const week = matrix
    .getByRole('group', { name: 'Range' })
    .getByRole('button', { name: 'The last 7 days, by the hour' });
  await week.click();
  await expect(week).toHaveAttribute('aria-pressed', 'true');
  await expect(matrix.getByRole('grid').getByRole('gridcell')).toHaveCount(7 * 24);
  await expect(matrix).toContainText('Failures, few');

  const colour = matrix.getByLabel('Colour the matrix by');
  await colour.selectOption('queue');
  await expect(matrix.locator('.matrix')).toHaveAttribute('data-mode', 'queue');
  await expect(matrix).toContainText('Fewer queued');
  await expect(matrix).not.toContainText('Failures, few');
  // The squares are the same squares, now saying how many were queued.
  await expect(
    matrix.locator('[role="gridcell"][data-tone="pending"][data-kind="active"]').first(),
  ).toBeVisible();

  await colour.selectOption('runtime');
  await expect(matrix).toContainText('runner time');
  await expect(
    matrix.locator('[role="gridcell"][data-tone="busy"][data-kind="active"]').first(),
  ).toBeVisible();
});

test('the matrix has quick ranges, and remembers the one chosen', async ({ page }) => {
  const matrix = page.getByRole('region', { name: 'Activity matrix', exact: true });
  const ranges = matrix.getByRole('group', { name: 'Range' });
  // Today alone, until an operator asks for more: one row of twenty-four.
  await expect(ranges.getByRole('button', { name: 'Today, by the hour' })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  const grid = matrix.getByRole('grid');
  await expect(grid.getByRole('gridcell')).toHaveCount(24);

  // A week by the hour: seven rows of twenty-four squares, the newest one
  // with jobs being the tab stop, and a square is an hour.
  await ranges.getByRole('button', { name: 'The last 7 days, by the hour' }).click();
  await expect(grid).toHaveAccessibleName(/^7 days of this fleet's jobs, one square per hour$/);
  await expect(grid.getByRole('gridcell')).toHaveCount(7 * 24);
  // The newest hour with anything in it may be one where jobs are still
  // queued and nothing has finished yet; either way it is an hour.
  await expect(grid.locator('[role="gridcell"][tabindex="0"]')).toHaveAttribute(
    'aria-label',
    /, \d\d?:\d\d( [AP]M)? to \d\d?:\d\d( [AP]M)?\. \d+ jobs? (finished|queued)/,
  );

  // The choice is the operator's, and survives a reload.
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
  const again = page.getByRole('region', { name: 'Activity matrix', exact: true });
  await expect(again.getByRole('button', { name: 'The last 7 days, by the hour' })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await expect(again.getByRole('grid').getByRole('gridcell')).toHaveCount(7 * 24);
});

/**
 * The band spends its width.
 *
 * What this protects is the thing an operator sees before they read anything:
 * a band with a stub of squares at the left, a field of white, and the key
 * pushed against the right edge. The grid takes the width it can use and the
 * figures take the rest, so there is no gap between them to explain -- and on
 * a phone that holds too, which is the half of it that was once missing: a
 * week of hours squeezed into ten-pixel squares filled half the screen and
 * left the other half white, whatever the phone was.
 */
test('the matrix spends the width on squares and figures, with the key beneath', async ({
  page,
}) => {
  const matrix = page.getByRole('region', { name: 'Activity matrix', exact: true });
  const band = matrix.locator('.band');
  // The grid itself, not the frame around it: the frame filled the band before
  // any of this, and the squares inside it were the stub.
  const grid = matrix.getByRole('grid');
  const aside = matrix.locator('.aside');
  const key = matrix.locator('.legend');

  // The figures are what the squares on screen come to, so an operator reads
  // the window without hovering every square in it. Read off the week rather
  // than today: the demo fleet's most recent finished job landed twenty
  // minutes before it was seeded, so a suite started in the twenty minutes
  // after midnight has a today with nothing finished in it, and the aside
  // says so instead -- correctly, and for the rest of the run, because no
  // demo job ever completes. The week always has the morning's work in it.
  await matrix
    .getByRole('group', { name: 'Range' })
    .getByRole('button', { name: 'The last 7 days, by the hour' })
    .click();
  await expect(grid).toBeVisible();
  await expect(aside).toContainText('Failure rate');
  await expect(aside).toContainText('Busiest hour');
  await expect(aside).toContainText(/\d[\d,]* jobs? finished/);

  const viewport = page.viewportSize();
  if ((viewport?.width ?? 0) < 900) {
    /*
      A phone is too narrow for both, so one of the two gives way -- and which
      one is the grid's to decide. A week of hours would use every pixel of the
      band, so it takes it and the figures go underneath, and so does a month,
      now that its days are six squares each rather than five week columns of
      one. A few weeks of whole days -- the Usage page's shape -- would stop
      short, and there the figures stay beside it rather than leaving the
      band's own width white. Either way the page never widens.
    */
    for (const [range, spends] of [
      ['The last 7 days, by the hour', true],
      ['The last 30 days', true],
    ] as const) {
      await matrix
        .getByRole('group', { name: 'Range' })
        .getByRole('button', { name: range })
        .click();
      await expect(grid).toBeVisible();
      const [bandBox, gridBox, asideBox] = await Promise.all([
        band.boundingBox(),
        grid.boundingBox(),
        aside.boundingBox(),
      ]);
      if (spends) {
        expect(gridBox!.width, `${range}: the squares take the band`).toBeGreaterThan(
          bandBox!.width - 16,
        );
        expect(asideBox!.y, `${range}: the figures are under them`).toBeGreaterThan(gridBox!.y);
      } else {
        expect(asideBox!.x, `${range}: the figures are beside the squares`).toBeGreaterThan(
          gridBox!.x + gridBox!.width - 1,
        );
        expect(
          gridBox!.width + asideBox!.width,
          `${range}: and the two of them are the band`,
        ).toBeGreaterThan(bandBox!.width - 32);
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      );
    }
    return;
  }

  // A day of hours has more columns than rows, so its squares grow into the
  // room, and so does a month of days cut into squares. Nothing is left over:
  // the two of them, and the one gap between, are the band.
  for (const range of ['Today, by the hour', 'The last 30 days', 'The last 90 days']) {
    await matrix.getByRole('group', { name: 'Range' }).getByRole('button', { name: range }).click();
    await expect(grid).toBeVisible();
    const [bandBox, gridBox, asideBox, keyBox] = await Promise.all([
      band.boundingBox(),
      grid.boundingBox(),
      aside.boundingBox(),
      key.boundingBox(),
    ]);
    const spent = gridBox!.width + asideBox!.width;
    expect(spent, `${range}: the squares and the figures fill the band`).toBeGreaterThan(
      bandBox!.width - 48,
    );
    // The key explains the squares, so it runs under them rather than sitting
    // in a column of its own with the white space in front of it.
    expect(keyBox!.y, `${range}: the key is beneath`).toBeGreaterThanOrEqual(
      gridBox!.y + gridBox!.height - 1,
    );
    expect(keyBox!.x, `${range}: the key starts under the grid`).toBeLessThan(gridBox!.x + 8);
  }
});

/**
 * The longer ranges fill the band with squares.
 *
 * A month of whole days was five week columns in the corner of a band with
 * the rest of it white, and a year was cut to fit a phone -- six months of it,
 * under a button that says a year. A month and a quarter now cut every day
 * into squares, and every range is drawn whole with its square sized to the
 * width, so what the button names is what is on screen.
 */
test('a month and a quarter cut their days into squares, and a year is twelve months', async ({
  page,
}) => {
  const matrix = page.getByRole('region', { name: 'Activity matrix', exact: true });
  const ranges = matrix.getByRole('group', { name: 'Range' });
  const grid = matrix.getByRole('grid');
  const frame = matrix.locator('.frame');

  for (const [range, name, squares] of [
    [
      'The last 30 days, in four-hour squares',
      /^\d weeks of this fleet's jobs, one square per 4 hours$/,
      30 * 6,
    ],
    [
      'The last 90 days, in eight-hour squares',
      /^\d+ weeks of this fleet's jobs, one square per 8 hours$/,
      90 * 3,
    ],
    ['The last year', /^52 weeks of this fleet's jobs, one square per day$/, null],
  ] as const) {
    await ranges.getByRole('button', { name: range }).click();
    await expect(grid).toHaveAccessibleName(name);
    if (squares !== null) await expect(grid.getByRole('gridcell')).toHaveCount(squares);
    // Every square of it on screen, on every width: the frame does not
    // scroll to reach the newest, and the page does not widen.
    const fits = await frame.evaluate((el) => el.scrollWidth <= el.clientWidth + 1);
    expect(fits, `${range}: the whole window fits the band`).toBe(true);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
  }

  // Selecting a slice of a day opens that day's hours, and says what it is.
  await ranges.getByRole('button', { name: 'The last 30 days, in four-hour squares' }).click();
  const stop = grid.locator('[role="gridcell"][tabindex="0"]');
  await expect(stop).toHaveAttribute(
    'aria-label',
    /^\w+,? .+, \d\d?:\d\d( [AP]M)? to \d\d?:\d\d( [AP]M)?\. \d+ jobs? (finished|queued)/,
  );
  await stop.click();
  const detail = matrix.getByRole('region', { name: 'Selected hours' });
  await expect(detail).toBeVisible();
  await expect(detail.getByRole('img', { name: /^Hour by hour:/ })).toBeVisible();
  await expect(detail.getByRole('link', { name: 'Jobs that day' })).toBeVisible();
});

// The setup checklist decides whether this fleet has run a job from the
// fleet's own counters. Until those arrive "no jobs yet" is unknown, not true:
// shown in that gap, the checklist counted as seen, and a fleet with a day of
// finished jobs was then congratulated on its first one -- a banner that also
// pushed the problems line off the first screen.
test('a fleet that has run jobs is never announced its first one while its counters load', async ({
  page,
}) => {
  await page.evaluate(() => localStorage.clear());
  await page.route('**/api/v1/stats*', async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 2_000));
    await route.continue();
  });
  const counters = page.waitForResponse((response) => response.url().includes('/api/v1/stats'));
  await page.reload();
  await expect(page.getByRole('heading', { level: 1, name: 'Overview' })).toBeVisible();
  await counters;
  // Once the counters have landed, whatever they would trigger has a moment
  // to render; the banner fetches its job before it shows.
  await page.waitForTimeout(1_500);
  await expect(page.getByRole('heading', { name: /^Your first job/ })).toHaveCount(0);
  // The checklist itself, by what it is. This used to look for the words its
  // opening line starts with ("No ", "One step "), and "No " is also how the
  // activity matrix opens when no job finished today. That is true of this fleet
  // when the suite starts within twenty minutes of midnight UTC: the newest job
  // it is seeded with finished twenty minutes before the start, which is then
  // yesterday. CI failed on exactly that, on the test and on its retry.
  await expect(page.getByRole('region', { name: 'Finish setting up' })).toHaveCount(0);
});

/*
 * The question GitHub cannot answer, on the page an operator leaves open:
 * which failures has anybody read. The panel lists the failures from the last
 * week that nobody has acknowledged and no later attempt has replaced, its
 * header carries the whole count rather than the two rows it shows (SHOWN in
 * RecentFailures.svelte), and a row opens the job's own drawer where the same
 * decision can be taken and taken back.
 */
test('recent failures says which failures nobody has read, and links to the rest', async ({
  page,
}) => {
  const failures = page.getByRole('region', { name: 'Recent failures', exact: true });
  await expect(failures).toBeVisible();
  // The badge is the whole list, not the rows: it appears once the fetch has
  // landed and the panel is not empty.
  const count = failures.getByText(/\d+ to look at/);
  await expect(count).toBeVisible();
  const rows = failures.getByRole('listitem');
  await expect(rows.first()).toBeVisible();

  // A row is a job, not a page away: it opens the failure's own drawer.
  await rows
    .first()
    .getByRole('button', { name: /^Inspect/ })
    .click();
  const drawer = page.getByRole('dialog');
  await expect(drawer).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(drawer).toBeHidden();

  // The panel's own action and the toast's undo: the fastest way to clear a
  // failure that has been read, from the page the operator is already on.
  const before = await rows.count();
  await rows.first().getByRole('button', { name: 'Mark this failure as known' }).click();
  await expect(rows).toHaveCount(before - 1);
  await page.getByRole('button', { name: 'Undo' }).click();
  await expect(rows).toHaveCount(before);

  // What does not fit is one link away, carrying the same question with it.
  const total = Number((await count.textContent())?.match(/\d+/)?.[0] ?? '0');
  const more = failures.getByRole('link', { name: /more on the jobs page/ });
  if (total > (await rows.count())) {
    await expect(more).toBeVisible();
    await more.click();
    await expect(page).toHaveURL(/[?&]attention=true/);
    await expect(
      page.getByRole('button', { name: 'Needs attention', exact: true }),
    ).toHaveAttribute('aria-pressed', 'true');
  } else {
    await expect(more).toHaveCount(0);
  }
});

/*
 * The link under the panel is the rest of what the panel counted, so it has to
 * lead to the same scope. The Jobs page lists this fleet's own jobs unless it is
 * told otherwise, and the panel's switch is the same preference the tiles use:
 * with other runners on, the count includes failures elsewhere and a bare link
 * lands on a list that is shorter than the number it was reached from.
 */
test("the failures panel's link to the rest keeps the scope the panel counted", async ({
  page,
}) => {
  const failures = page.getByRole('region', { name: 'Recent failures', exact: true });
  const more = failures.getByRole('link', { name: /more on the jobs page/ });
  await expect(more, 'the fixture has more failures than the panel shows').toBeVisible();
  await expect(more).toHaveAttribute('href', '/jobs?attention=true');

  const header = page.locator('header').filter({ has: page.getByRole('heading', { level: 1 }) });
  const toggle = header.getByRole('combobox', { name: 'Overview scope' });
  await toggle.selectOption('all');
  await expect(toggle).toHaveValue('all');
  await expect(more).toHaveAttribute('href', '/jobs?attention=true&all=true');

  await more.click();
  await expect(page).toHaveURL(/[?&]attention=true/);
  await expect(page).toHaveURL(/[?&]all=true/);
});

// The line shown when nothing is owed links to the history, and it says nothing
// is waiting "wherever they ran" only when the switch is on. A bare link would
// land on the fleet's own failed jobs, a shorter list than the claim it was
// reached from, the same way the link under a full panel would.
test("the one-line failures panel's link to the failed view keeps the scope", async ({ page }) => {
  await page.route(/\/api\/v1\/jobs\?.*attention=true/, async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    const { response, body } = read;
    await route.fulfill({ response, json: { ...body, items: [], total: 0 } });
  });
  await reload(page, 'Overview');

  const failures = page.getByRole('region', { name: 'Recent failures', exact: true });
  const history = failures.getByRole('link', { name: 'Failed view' });
  await expect(history).toHaveAttribute('href', '/jobs?failed=true');

  const header = page.locator('header').filter({ has: page.getByRole('heading', { level: 1 }) });
  const toggle = header.getByRole('combobox', { name: 'Overview scope' });
  await toggle.selectOption('all');
  await expect(toggle).toHaveValue('all');
  await expect(history).toHaveAttribute('href', '/jobs?failed=true&all=true');
});

// Marking a failure as known takes its row off the list, and the button that was
// pressed goes with it. Keyboard focus fell to the top of the document, so
// working down the list meant tabbing back to where you had been after every row.
test('marking a failure as known moves focus to the failure that takes its place', async ({
  page,
}) => {
  const failures = page.getByRole('region', { name: 'Recent failures', exact: true });
  const rows = failures.getByRole('listitem');
  const count = failures.getByText(/\d+ to look at/);
  await expect(rows).toHaveCount(2);
  const before = await count.textContent();
  const next = await rows
    .nth(1)
    .getByRole('button', { name: /^Inspect/ })
    .getAttribute('aria-label');

  await rows.first().getByRole('button', { name: 'Mark this failure as known' }).click();

  const inspect = rows.first().getByRole('button', { name: /^Inspect/ });
  await expect(inspect).toHaveAttribute('aria-label', next ?? '');
  await expect(inspect).toBeFocused();

  // Put it back, so the fleet the other specs share is as this one found it.
  await page.getByRole('button', { name: 'Undo' }).click();
  await expect(count).toHaveText(before ?? '');
});

// With no failure left to take the row's place, focus has nowhere on the panel to
// go, and the page heading is where the app puts it after any other change of
// place. The fleet is given one failure and the requests that mark it as known
// are answered here, so the fixture the other specs share is not touched.
test('marking the only failure as known moves focus to the page heading', async ({ page }) => {
  let acknowledged = false;
  await page.route(/\/api\/v1\/jobs\?.*attention=true/, async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    const { response, body } = read;
    const items = acknowledged ? [] : (body.items ?? []).slice(0, 1);
    await route.fulfill({ response, json: { ...body, items, total: items.length } });
  });
  await page.route(/\/api\/v1\/jobs\/[^/]+\/acknowledge$/, (route) => {
    acknowledged = route.request().method() === 'POST';
    return route.fulfill({ json: {} });
  });
  await reload(page, 'Overview');

  const failures = page.getByRole('region', { name: 'Recent failures', exact: true });
  const rows = failures.getByRole('listitem');
  await expect(rows).toHaveCount(1);
  await rows.first().getByRole('button', { name: 'Mark this failure as known' }).click();

  await expect(failures.getByText('No failure is waiting on you')).toBeVisible();
  await expect(page.getByRole('heading', { level: 1, name: 'Overview' })).toBeFocused();
});

// A failure can need a person even when Eli cannot repair its trigger.
test('the overview keeps failed push and scheduled runs in its attention inbox', async ({
  page,
}) => {
  await page.route('**/api/v1/events*', (route) => route.abort('connectionfailed'));
  await page.route(/\/api\/v1\/jobs\?.*attention=true/, async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    const { response, body } = read;
    const seed = body.items?.[0];
    expect(seed, 'the fixture supplies a failed job').toBeTruthy();
    const repairOnly = new URL(route.request().url()).searchParams.get('eli_repairable') === 'true';
    const items = repairOnly
      ? []
      : [
          { ...seed, id: 'job_push_failure', job_name: 'Failed push build', run_event: 'push' },
          {
            ...seed,
            id: 'job_schedule_failure',
            job_name: 'Failed scheduled build',
            run_event: 'schedule',
          },
        ];
    await route.fulfill({ response, json: { ...body, items, total: items.length } });
  });
  await reload(page, 'Overview');
  const failures = page.getByRole('region', { name: 'Recent failures', exact: true });
  await expect(failures.getByRole('button', { name: 'Inspect Failed push build' })).toBeVisible();
  await expect(
    failures.getByRole('button', { name: 'Inspect Failed scheduled build' }),
  ).toBeVisible();
  await expect(failures.getByText('No failure is waiting on you.')).toHaveCount(0);
  await expect(failures.getByRole('link', { name: 'Fix with Eli' })).toHaveCount(0);
});

// The body under the band is one grid drawn from the list of panels, in its
// order, so a later change can save another order and hide some; on a phone
// it is one column in that same order.
test('the body is drawn from the registry, in its order, and one column on a phone', async ({
  page,
}) => {
  const order = await page
    .locator('[data-panel]')
    .evaluateAll((nodes) => nodes.map((n) => n.getAttribute('data-panel')));
  expect(order).toEqual([
    'attention',
    'pools',
    'failures',
    'jobs',
    'events',
    'history',
    'diagnostics',
  ]);
  await page.setViewportSize({ width: 375, height: 800 });
  const columns = await page
    .locator('.panels')
    .evaluate((el) => getComputedStyle(el).gridTemplateColumns.split(' ').length);
  expect(columns).toBe(1);
});

// The mark before an event is a pill in the tone of the news, with the icon
// inside it: the shape and the icon both say what kind it is, never the
// colour alone.
test("an event's mark is a pill in its tone, the icon inside it", async ({ page }) => {
  const feed = page.getByRole('region', { name: 'Recent events', exact: true });
  const mark = feed.getByRole('listitem').first().locator('.mark');
  await expect(mark).toBeVisible();
  const [radius, height, border] = await mark.evaluate((el) => {
    const s = getComputedStyle(el);
    return [parseFloat(s.borderTopLeftRadius), el.getBoundingClientRect().height, s.borderTopWidth];
  });
  expect(radius).toBeGreaterThanOrEqual(height / 2);
  expect(border).toBe('1px');
  await expect(mark.locator('svg')).toHaveCount(1);
});
