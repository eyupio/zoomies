/**
 * The four analyses, each a card on the page where its question is already
 * asked: why the queued jobs waited on the Jobs page's Queued view, what got
 * worse beside Usage's table of releases, whether a host's failures came under
 * pressure on the host's own page, and which host or pool keeps failing to
 * start runners when the Runners page is narrowed to failed ones.
 *
 * The binding check is one answer: a card's summary is exactly the `summary`
 * of the same route asked for the same period, which is what an agent's tool
 * and `zoomies analyse` print. The card names the period's end, and its start
 * when the page has a window of its own, so the test asks with the very
 * address the page used rather than with a default that would have moved on
 * by the time it asked.
 *
 * The demo seeds a cause for each analysis between 22 and 20 hours ago (see
 * seedAnalyses in internal/controller/seed_analysis.go), inside the last day
 * that the Jobs, Runners and host pages ask about. Usage asks about its own
 * range, and is opened on thirty days so the demo's two releases are in it.
 */
import { expect, test, type Dialog, type Page } from '@playwright/test';
import { expectNoSidewaysScroll, FIXTURE, goto, readThrough } from './support/fixtures';

/** Every analysis request the page sends, in order, so the last is the one on screen. */
function watchAnalysis(page: Page, route: string): string[] {
  const seen: string[] = [];
  page.on('request', (request) => {
    if (request.url().includes(`/api/v1/analysis/${route}`)) seen.push(request.url());
  });
  return seen;
}

/** The card, by the heading that names its region. */
function card(page: Page, title: string) {
  return page.getByRole('region', { name: title });
}

/**
 * The card's summary against the route's, asked with the page's own period.
 * Returns the route's finding for whatever else a test wants of it.
 */
async function expectOneAnswer(
  page: Page,
  title: string,
  requests: string[],
  windowed = false,
): Promise<{ summary: string; confidence: string }> {
  const summary = card(page, title).getByTestId('analysis-summary');
  await expect(summary).toBeVisible();
  const url = requests.at(-1);
  expect(url, 'the page asked the analysis route').toBeTruthy();
  const query = new URL(url!).searchParams;
  // An explicit end, or the test's own request would read a later period.
  // A page with no window of its own leaves since to the route, whose
  // default before that end is min(24h, limits.job_stats_window).
  expect(query.get('until')).toBeTruthy();
  expect(query.has('since')).toBe(windowed);
  const response = await page.request.get(url!);
  expect(response.status()).toBe(200);
  const finding = (await response.json()) as { summary: string; confidence: string };
  expect((await summary.textContent())?.trim()).toBe(finding.summary);
  return finding;
}

/**
 * The accessibility audit a11y.spec.ts runs on every page in the navigation,
 * on the four pages these cards sit on: the repository has no axe and takes
 * no new library, so these are the checks a machine can make honestly.
 */
async function expectAccessible(page: Page, title: string): Promise<void> {
  // Open, so the evidence's links are name-checked and its rows are laid out
  // at the width the check below measures; folded, the tree skips them.
  const evidence = card(page, title).locator('details');
  await evidence.locator('summary').click();
  await expect(evidence).toHaveAttribute('open', '');
  await expect(page.getByRole('heading', { level: 1 })).toHaveCount(1);
  await expect(page.getByRole('main')).toHaveCount(1);
  await expect(page.getByRole('navigation', { name: 'Sections' })).toHaveCount(1);
  await expect(page.getByRole('button', { name: /^$/ })).toHaveCount(0);
  await expect(page.getByRole('link', { name: /^$/ })).toHaveCount(0);
  const offenders = await page.evaluate(() => {
    const out: string[] = [];
    for (const list of Array.from(document.querySelectorAll('dl'))) {
      for (const child of Array.from(list.children)) {
        const ok =
          ['DT', 'DD', 'SCRIPT', 'TEMPLATE'].includes(child.tagName) ||
          (child.tagName === 'DIV' &&
            Array.from(child.children).every((inner) => ['DT', 'DD'].includes(inner.tagName)));
        if (!ok) out.push(`dl > ${child.tagName.toLowerCase()}`);
      }
    }
    for (const el of Array.from(document.querySelectorAll('[tabindex]'))) {
      if (Number(el.getAttribute('tabindex')) > 0) out.push(`tabindex on ${el.tagName}`);
    }
    for (const image of Array.from(document.querySelectorAll('img'))) {
      if (image.getAttribute('alt') === null && image.getAttribute('aria-hidden') !== 'true') {
        out.push('img without alt');
      }
    }
    return out;
  });
  expect(offenders).toEqual([]);
  // The card's own heading sits under the page's h1, so a screen reader's
  // outline has it in the right place.
  await expect(card(page, title).getByRole('heading', { level: 2, name: title })).toHaveCount(1);
  // On a phone the card reads down the page and never widens it.
  await expectNoSidewaysScroll(page, `The page with the card "${title}"`);
}

test('the Queued view says why jobs waited, as the route does', async ({ page }) => {
  const requests = watchAnalysis(page, 'queue-wait');
  await goto(page, '/jobs?state=queued', 'Jobs');
  const finding = await expectOneAnswer(page, 'Why jobs waited', requests);
  expect(finding.summary).toContain('waited');
  await expectAccessible(page, 'Why jobs waited');
});

test('the Queued card says it covers every pool when the list is narrowed further', async ({
  page,
}) => {
  await goto(page, `/jobs?state=queued&repo=${encodeURIComponent(FIXTURE.repos[0])}`, 'Jobs');
  const found = card(page, 'Why jobs waited');
  await expect(found.getByTestId('analysis-summary')).toBeVisible();
  // The route reads one pool or none, so the list's repository filter is not
  // the card's, and the card says what it covers instead of borrowing it.
  await expect(found.getByText(/^Every pool, /)).toBeVisible();
  await expect(found.getByText("The list's repository filter is not applied here")).toBeVisible();
});

test('the Queued card names the unmatched filter it does not apply', async ({ page }) => {
  // An address that keeps the list queued with unmatched on narrows it to the
  // jobs no pool claims, while the card still reads every pool.
  await goto(page, '/jobs?state=queued&unmatched=true', 'Jobs');
  const found = card(page, 'Why jobs waited');
  await expect(found.getByTestId('analysis-summary')).toBeVisible();
  await expect(found.getByText("The list's unmatched filter is not applied here")).toBeVisible();
});

// GitHub reports every job in an installed repository, and the list leaves out
// the ones another vendor's runners ran unless "Include other runners" is on:
// they are that vendor's to run, never time a pool should have claimed them in.
// The demo has three on a hosted-runner vendor, one of them still queued, so a
// card that read them would count more jobs than the list lists, and say a
// pool had not claimed a job no pool here ever could.
test('the Queued card counts the jobs the list lists, and none of another vendor’s', async ({
  page,
}) => {
  const requests = watchAnalysis(page, 'queue-wait');
  await goto(page, '/jobs?state=queued', 'Jobs');
  await expectOneAnswer(page, 'Why jobs waited', requests);
  // The card's own period, which the list is asked about in the same words.
  const asked = new URL(requests.at(-1)!).searchParams;
  const until = asked.get('until')!;
  const since = new Date(Date.parse(until) - 24 * 3600 * 1000).toISOString();
  const total = async (narrowing: string): Promise<number> => {
    const url = `/api/v1/jobs?since=${encodeURIComponent(since)}&until=${encodeURIComponent(until)}&limit=1${narrowing}`;
    return ((await page.request.get(url).then((r) => r.json())) as { total: number }).total;
  };
  const listed = await total('&managed=true');
  expect(await total(''), 'the demo has jobs on a vendor’s runners to leave out').toBeGreaterThan(
    listed,
  );

  const found = card(page, 'Why jobs waited');
  await found.getByText('Evidence', { exact: true }).click();
  const line = (await found.getByText(/^[\d,]+ jobs? queued, /).textContent()) ?? '';
  const counted = /^([\d,]+) jobs?/.exec(line.trim())?.[1] ?? '';
  expect(Number(counted.replace(/,/g, '')), `the job count in "${line}"`).toBe(listed);
  // The switch is off, so the card has nothing to say about it.
  await expect(found.getByText('Include other runners')).toHaveCount(0);
});

test('the Queued card names the Include other runners switch when it is on', async ({ page }) => {
  await goto(page, '/jobs?state=queued&all=true', 'Jobs');
  const found = card(page, 'Why jobs waited');
  await expect(found.getByTestId('analysis-summary')).toBeVisible();
  // With the switch on the list holds jobs the card does not read.
  await expect(
    found.getByText(
      "The list's Include other runners switch is not applied here: this analysis leaves out the jobs another vendor's runners ran.",
    ),
  ).toBeVisible();
});

test("the Queued card reads the list's own dates", async ({ page }) => {
  const requests = watchAnalysis(page, 'queue-wait');
  const day = (back: number) =>
    page.evaluate((n) => {
      const d = new Date();
      d.setDate(d.getDate() - n);
      const pad = (v: number) => String(v).padStart(2, '0');
      return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
    }, back);
  await goto(page, '/jobs', 'Jobs');
  const since = await day(3);
  const until = await day(2);
  await goto(page, `/jobs?state=queued&since=${since}&until=${until}`, 'Jobs');
  await expectOneAnswer(page, 'Why jobs waited', requests, true);
  const query = new URL(requests.at(-1)!).searchParams;
  // The whole of each day on the operator's clock, as the list reads them.
  const expected = await page.evaluate(
    ([s, u]) => [
      new Date(`${s}T00:00:00`).toISOString(),
      new Date(`${u}T23:59:59.999`).toISOString(),
    ],
    [since, until],
  );
  expect([query.get('since'), query.get('until')]).toEqual(expected);
});

test('a new window does not leave the last answer standing while it loads', async ({ page }) => {
  let held = 0;
  await page.route('**/api/v1/analysis/queue-wait**', async (route) => {
    // The first answer arrives; every later one is held, so what the card
    // shows meanwhile is what it shows while a new window loads.
    if (held++ === 0) await route.continue();
  });
  await goto(page, '/jobs?state=queued', 'Jobs');
  const found = card(page, 'Why jobs waited');
  await expect(found.getByTestId('analysis-summary')).toBeVisible();
  await page.evaluate(() => {
    const d = new Date(Date.now() - 2 * 24 * 3600 * 1000);
    const pad = (v: number) => String(v).padStart(2, '0');
    const day = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
    history.pushState({}, '', `/jobs?state=queued&since=${day}`);
    dispatchEvent(new PopStateEvent('popstate'));
  });
  await expect.poll(() => held).toBeGreaterThan(1);
  await expect(found.getByTestId('analysis-summary')).toHaveCount(0);
  await expect(found.getByText('Working it out')).toBeVisible();
});

/** Another address without a reload, as one of the page's own controls moves it. */
async function moveTo(page: Page, path: string): Promise<void> {
  await page.evaluate((to) => {
    history.pushState({}, '', to);
    dispatchEvent(new PopStateEvent('popstate'));
  }, path);
}

// What the Jobs card does for a new window holds for the other pages that
// narrow a card with a control. The pool moves at once, with the chips and the
// grid, and a card that went on saying the last answer until the next landed
// read as this pool's: on the demo, a host blamed for failures the pool does
// not have.
test('a new pool does not leave the last answer standing on the failed runners', async ({
  page,
}) => {
  const pools = (await page.request.get('/api/v1/pools').then((r) => r.json())) as {
    items: { id: string }[];
  };
  let held = 0;
  await page.route('**/api/v1/analysis/startup-failures**', async (route) => {
    if (held++ === 0) await route.continue();
  });
  await goto(page, '/runners?state=failed', 'Runners');
  const found = card(page, 'What keeps failing to start');
  await expect(found.getByTestId('analysis-summary')).toBeVisible();
  await moveTo(page, `/runners?state=failed&pool_id=${pools.items[0]!.id}`);
  await expect.poll(() => held).toBeGreaterThan(1);
  await expect(found.getByTestId('analysis-summary')).toHaveCount(0);
  await expect(found.getByText('Working it out')).toBeVisible();
});

test('a new range does not leave the last comparison standing on Usage', async ({ page }) => {
  let held = 0;
  await page.route('**/api/v1/analysis/compare**', async (route) => {
    if (held++ === 0) await route.continue();
  });
  await goto(page, '/usage?range=30d', 'Usage');
  const found = card(page, 'What got worse and what got better');
  await expect(found.getByTestId('analysis-summary')).toBeVisible();
  await moveTo(page, '/usage?range=7d');
  await expect.poll(() => held).toBeGreaterThan(1);
  await expect(found.getByTestId('analysis-summary')).toHaveCount(0);
  await expect(found.getByText('Working it out')).toBeVisible();
});

/** A calendar day on the operator's clock as an address writes it, `ahead` days from today. */
function dayAt(ahead: number): string {
  const d = new Date();
  d.setDate(d.getDate() + ahead);
  const pad = (v: number) => String(v).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

const NOT_STARTED =
  'This period has not started, and an analysis reads only what has already happened; choose a start in the past.';
const BACKWARDS = 'The end is before the start, so there is no period to analyse.';

// A window the page does not ask about used to leave its card a titled frame
// with nothing under it. The date picker points out a range that runs
// backwards, in an alert of its own, and nothing points out one that has not
// started: the list is merely empty. So the card says why it asked nothing,
// and the route is still not asked.
test('a Queued window that has not started, or runs backwards, says why there is no answer', async ({
  page,
}) => {
  const requests = watchAnalysis(page, 'queue-wait');
  const found = card(page, 'Why jobs waited');
  await goto(page, `/jobs?state=queued&since=${dayAt(3)}`, 'Jobs');
  await expect(found.getByRole('status')).toHaveText(NOT_STARTED);
  await goto(page, `/jobs?state=queued&since=${dayAt(-2)}&until=${dayAt(-5)}`, 'Jobs');
  await expect(found.getByRole('status')).toHaveText(BACKWARDS);
  expect(requests, 'the route was asked nothing').toEqual([]);
});

test('a Usage range that has not started, or runs backwards, says why there is no comparison', async ({
  page,
}) => {
  const requests = watchAnalysis(page, 'compare');
  const found = card(page, 'What got worse and what got better');
  await goto(page, `/usage?since=${dayAt(3)}&until=${dayAt(4)}`, 'Usage');
  await expect(found.getByRole('status')).toHaveText(NOT_STARTED);
  await goto(page, `/usage?since=${dayAt(-2)}&until=${dayAt(-5)}`, 'Usage');
  await expect(found.getByRole('status')).toHaveText(BACKWARDS);
  expect(requests, 'the route was asked nothing').toEqual([]);
});

/**
 * What a press on the card's Ask Eli sends the model. The demo's built-in model
 * is the default provider, so the prompt the page builds is the first message
 * of the chat request.
 */
async function askedByTheCard(page: Page, title: string): Promise<string> {
  const asked: string[] = [];
  await page.route('**/api/v1/assistant/personal/chat', async (route) => {
    const body = route.request().postDataJSON() as { messages: { content: string }[] };
    asked.push(body.messages[0]!.content);
    await route.continue();
  });
  const ask = card(page, title).getByRole('button', { name: 'Ask Eli' });
  await expect(ask, `the card "${title}" offers Ask Eli`).toBeVisible();
  await ask.click();
  await expect.poll(() => asked.length, 'the prompt Eli was sent').toBeGreaterThan(0);
  return asked[0]!;
}

// The prompt tells Eli to check what the page showed with its tools, which
// read the last day of the whole fleet unless they are passed what the card
// was narrowed to. A finding for one pool says nothing of the pool ("No start
// failed in this period"), so without the id Eli's check answered the
// fleet-wide question and contradicted the card it had been asked about.
test('Ask Eli is sent the pool the failed runners were narrowed to, and told to pass it', async ({
  page,
}) => {
  const pools = (await page.request.get('/api/v1/pools').then((r) => r.json())) as {
    items: { id: string; name: string }[];
  };
  const pool = pools.items[0]!;
  await goto(page, `/runners?state=failed&pool_id=${pool.id}`, 'Runners');
  const title = 'What keeps failing to start';
  await expect(card(page, title).getByTestId('analysis-summary')).toBeVisible();
  const prompt = await askedByTheCard(page, title);
  expect(prompt).toContain(`Pool: ${pool.name} (${pool.id})`);
  expect(prompt).toContain(`pool_id ${pool.id}`);
  expect(prompt).toMatch(/since \S+, until \S+/);
});

test('Ask Eli is sent the pool the Queued card was narrowed to, and told to pass it', async ({
  page,
}) => {
  const pools = (await page.request.get('/api/v1/pools').then((r) => r.json())) as {
    items: { id: string; name: string }[];
  };
  const pool = pools.items[0]!;
  await goto(page, `/jobs?state=queued&pool_id=${pool.id}`, 'Jobs');
  const title = 'Why jobs waited';
  await expect(card(page, title).getByTestId('analysis-summary')).toBeVisible();
  const prompt = await askedByTheCard(page, title);
  expect(prompt).toContain(`Pool: ${pool.name} (${pool.id})`);
  expect(prompt).toContain(`pool_id ${pool.id}`);
});

test('Ask Eli is sent the host the page is about, and told to pass it', async ({ page }) => {
  const hosts = (await page.request.get('/api/v1/hosts').then((r) => r.json())) as {
    items: { id: string; name: string }[];
  };
  const host = hosts.items.find((h) => h.name === FIXTURE.machineHost)!;
  await goto(page, `/hosts/${host.id}`, FIXTURE.machineHost);
  const title = 'Whether its failures came under pressure';
  await expect(card(page, title).getByTestId('analysis-summary')).toBeVisible();
  const prompt = await askedByTheCard(page, title);
  expect(prompt).toContain(`Host: ${host.name} (${host.id})`);
  expect(prompt).toContain(`host_id ${host.id}`);
});

test('Ask Eli on a card asked about the whole fleet passes no pool or host', async ({ page }) => {
  await goto(page, '/jobs?state=queued', 'Jobs');
  const title = 'Why jobs waited';
  await expect(card(page, title).getByTestId('analysis-summary')).toBeVisible();
  const prompt = await askedByTheCard(page, title);
  expect(prompt).toContain('for this fleet');
  expect(prompt).not.toMatch(/pool_id|host_id/);
});

/** Mission Control at a phone's width, set up as operational-layout.spec.ts does. */
async function missionControl(page: Page): Promise<void> {
  await page.setViewportSize({ width: 375, height: 900 });
  await page.addInitScript(() => {
    if (sessionStorage.getItem('layout-seeded')) return;
    localStorage.setItem('zoomies.ui-style', 'mission-control');
    sessionStorage.setItem('layout-seeded', '1');
  });
}

const top = (locator: import('@playwright/test').Locator) =>
  locator.evaluate((node) => node.getBoundingClientRect().top);

test('on a Mission Control phone the queued jobs come before the card', async ({ page }) => {
  // docs/ui-guidelines.md: on a Mission Control phone, records come before
  // supplementary analytics; the card is one.
  await missionControl(page);
  await goto(page, '/jobs?state=queued', 'Jobs');
  const found = card(page, 'Why jobs waited');
  const grid = page.locator('table[aria-label="Jobs"]');
  await expect(found.getByTestId('analysis-summary')).toBeVisible();
  await expect(grid).toBeVisible();
  expect(await top(grid)).toBeLessThan(await top(found));
  // A wide window keeps the card beside the filters it reads, above the list.
  await page.setViewportSize({ width: 1440, height: 900 });
  await expect(found.getByTestId('analysis-summary')).toBeVisible();
  expect(await top(found)).toBeLessThan(await top(grid));
});

test('the card is the Queued view’s alone', async ({ page }) => {
  await goto(page, '/jobs', 'Jobs');
  await expect(page.locator('tbody tr[data-row]').first()).toBeVisible();
  await expect(card(page, 'Why jobs waited')).toHaveCount(0);
});

test('Usage compares releases beside its table, as the route does', async ({ page }) => {
  const requests = watchAnalysis(page, 'compare');
  await goto(page, '/usage?range=30d', 'Usage');
  const title = 'What got worse and what got better';
  const finding = await expectOneAnswer(page, title, requests, true);
  // The demo's two releases ran fewer than thirty jobs each.
  expect(finding.summary).toContain('not ranked');
  // The page's own range: thirty days, today included, from local midnight.
  const start = await page.evaluate(() => {
    const d = new Date();
    return new Date(d.getFullYear(), d.getMonth(), d.getDate() - 29).toISOString();
  });
  expect(new URL(requests.at(-1)!).searchParams.get('since')).toBe(start);
  // With the evidence folded, each side's own dates and what was left out
  // are still on the card: they are what the figures mean.
  const found = card(page, title);
  await expect(found.locator('details')).not.toHaveAttribute('open', '');
  await expect(
    found.getByText(
      /^v1\.3\.2 from .+; v1\.3\.3 from .+\. Cancelled and skipped jobs are left out of every figure read from the job rows\.$/,
    ),
  ).toBeVisible();
  await expectAccessible(page, title);
});

test('Usage says too few releases in terms of its own range', async ({ page }) => {
  // The route's refusal for a range in which fewer than two releases ran a job
  // (the demo gives it for the last hour), byte for byte as it sends it today.
  // Its sentence sends a caller to the a and b parameters and to comparing by
  // period, and the card has neither; on a day with one release it is the
  // ordinary answer, so a reader who follows the wrong remedy has nowhere to go.
  await page.route('**/api/v1/analysis/compare**', (route) =>
    route.fulfill({
      status: 409,
      json: {
        error: {
          code: 'analysis.too_few_releases',
          message:
            'fewer than two releases completed a job in this period, so there is no pair to compare; name the releases with a and b, widen the period, or compare by period',
        },
      },
    }),
  );
  await goto(page, '/usage?range=1h', 'Usage');
  const found = card(page, 'What got worse and what got better');
  await expect(found.getByRole('alert')).toHaveCount(0);
  await expect(found.getByRole('status')).toHaveText(
    'Fewer than two releases completed a job in this range, so there is no pair to compare; widen the range.',
  );
});

test('Refresh on Usage asks for the comparison again, once, whatever the range', async ({
  page,
}) => {
  // A day's bounds are midnights, so Refresh moving "now" along changes
  // neither of them, and a range typed by hand does not move at all; the card,
  // which followed the bounds alone, was not asked again: the table was read
  // anew and the card kept the old moment. An hour's bounds do move, and were
  // asked again, which is how it went unseen. The hour also guards the other
  // way: moving the bounds and pressing the button are one refresh, not two
  // requests.
  const requests = watchAnalysis(page, 'compare');
  const refresh = page.getByRole('button', { name: 'Refresh', exact: true });
  for (const address of ['/usage', '/usage?range=1h', '/usage?since=2026-10-01&until=2026-10-08']) {
    requests.length = 0;
    await goto(page, address, 'Usage');
    await expect.poll(() => requests.length, address).toBe(1);
    await expect(
      card(page, 'What got worse and what got better').locator('[aria-busy]'),
    ).toHaveCount(0);
    await refresh.click();
    await expect.poll(() => requests.length, `${address} after Refresh`).toBe(2);
    // Not a third, from the bounds and the button each asking.
    await page.waitForTimeout(600);
    expect(requests.length, `${address} after Refresh, once`).toBe(2);
  }
});

/** The demo's pools and hosts, for the tests that narrow a card to one. */
async function fleetOf(page: Page) {
  const get = async <T>(path: string) =>
    ((await page.request.get(path).then((r) => r.json())) as { items: T[] }).items;
  return {
    pools: await get<{ id: string; name: string }>('/api/v1/pools'),
    hosts: await get<{ id: string; name: string }>('/api/v1/hosts'),
  };
}

// What a card asks of the route is not what its summary shows: the tests that
// read a summary bind it to the route by asking again with the request the page
// sent, so a parameter that went missing from both was invisible, and the page
// went on showing an answer, for the whole fleet, under a heading that named
// one pool. These read the query string the page sent.
test('the Queued card asks about the one pool the list is narrowed to, and none for several', async ({
  page,
}) => {
  const { pools } = await fleetOf(page);
  const a = pools.find((p) => p.name === FIXTURE.linuxPool)!;
  const b = pools.find((p) => p.name === FIXTURE.armPool)!;
  const requests = watchAnalysis(page, 'queue-wait');
  await goto(page, `/jobs?state=queued&pool_id=${a.id}`, 'Jobs');
  await expectOneAnswer(page, 'Why jobs waited', requests);
  expect(new URL(requests.at(-1)!).searchParams.getAll('pool_id')).toEqual([a.id]);
  // The route reads one pool or none, so two pools are the fleet, which the
  // card says in its scope, and nothing is sent.
  requests.length = 0;
  await goto(page, `/jobs?state=queued&pool_id=${a.id}&pool_id=${b.id}`, 'Jobs');
  await expectOneAnswer(page, 'Why jobs waited', requests);
  expect(new URL(requests.at(-1)!).searchParams.has('pool_id')).toBe(false);
});

test('the failed runners card asks about the pool and the host the list is narrowed to', async ({
  page,
}) => {
  const { pools, hosts } = await fleetOf(page);
  const pool = pools.find((p) => p.name === FIXTURE.linuxPool)!;
  const host = hosts.find((h) => h.name === FIXTURE.machineHost)!;
  const requests = watchAnalysis(page, 'startup-failures');
  await goto(page, `/runners?state=failed&pool_id=${pool.id}&host_id=${host.id}`, 'Runners');
  await expectOneAnswer(page, 'What keeps failing to start', requests);
  const query = new URL(requests.at(-1)!).searchParams;
  expect(query.get('pool_id')).toBe(pool.id);
  expect(query.get('host_id')).toBe(host.id);
  // And nothing it was not narrowed to.
  requests.length = 0;
  await goto(page, '/runners?state=failed', 'Runners');
  await expectOneAnswer(page, 'What keeps failing to start', requests);
  const fleetWide = new URL(requests.at(-1)!).searchParams;
  expect([fleetWide.has('pool_id'), fleetWide.has('host_id')]).toEqual([false, false]);
});

/**
 * The page's one Refresh asks a card again, once. Pressed after the card has
 * its first answer, so the extra request is the press and not the opening.
 */
async function expectRefreshAsksOnce(
  page: Page,
  address: string,
  heading: string,
  route: string,
  title: string,
): Promise<void> {
  const requests = watchAnalysis(page, route);
  await goto(page, address, heading);
  await expect.poll(() => requests.length, `${address} on opening`).toBe(1);
  await expect(card(page, title).locator('[aria-busy]')).toHaveCount(0);
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect.poll(() => requests.length, `${address} after Refresh`).toBe(2);
  // Not a third, from the key and a second trigger each asking.
  await page.waitForTimeout(600);
  expect(requests.length, `${address} after Refresh, once`).toBe(2);
}

// Refresh is one gesture for the page, and the grids and tables it re-reads
// move on to a new moment while a card that is not wired to it keeps the one
// the page was opened at. Nothing failed when it was not: the card only grew
// older than what was beside it.
test('Refresh on the Queued view asks why jobs waited again, once', async ({ page }) => {
  await expectRefreshAsksOnce(page, '/jobs?state=queued', 'Jobs', 'queue-wait', 'Why jobs waited');
});

test('Refresh on the failed runners asks what keeps failing to start again, once', async ({
  page,
}) => {
  await expectRefreshAsksOnce(
    page,
    '/runners?state=failed',
    'Runners',
    'startup-failures',
    'What keeps failing to start',
  );
});

test("Refresh on a host's page asks whether its failures came under pressure again, once", async ({
  page,
}) => {
  const { hosts } = await fleetOf(page);
  const host = hosts.find((h) => h.name === FIXTURE.machineHost)!;
  await expectRefreshAsksOnce(
    page,
    `/hosts/${host.id}`,
    FIXTURE.machineHost,
    'host-pressure',
    'Whether its failures came under pressure',
  );
});

test("a host's page says whether its failures came under pressure", async ({ page }) => {
  const hosts = (await page.request.get('/api/v1/hosts').then((r) => r.json())) as {
    items: { id: string; name: string }[];
  };
  const host = hosts.items.find((h) => h.name === FIXTURE.machineHost)!;
  const requests = watchAnalysis(page, 'host-pressure');
  await goto(page, `/hosts/${host.id}`, FIXTURE.machineHost);
  const title = 'Whether its failures came under pressure';
  const finding = await expectOneAnswer(page, title, requests);
  expect(new URL(requests.at(-1)!).searchParams.get('host_id')).toBe(host.id);
  expect(finding.summary).toContain(FIXTURE.machineHost);
  await expectAccessible(page, title);
});

test('the failed runners say what keeps failing to start, as the route does', async ({ page }) => {
  const requests = watchAnalysis(page, 'startup-failures');
  await goto(page, '/runners?state=failed', 'Runners');
  const title = 'What keeps failing to start';
  const finding = await expectOneAnswer(page, title, requests);
  expect(finding.summary).toContain(FIXTURE.machineHost);
  await expectAccessible(page, title);
});

test('the card is the failed filter’s alone', async ({ page }) => {
  await goto(page, '/runners', 'Runners');
  await expect(page.locator('tbody tr[data-row]').first()).toBeVisible();
  await expect(card(page, 'What keeps failing to start')).toHaveCount(0);
});

// The Queued card names every filter of the list that it leaves out, because
// the fleet's answer under a list narrowed to one thing reads as that thing's.
// The failed runners are the same: searched down to a name that matches
// nothing, the list says "No runners match these filters" while the card above
// it went on blaming a host without a word of what it covered.
test('the failed runners card says what it covers, and that the list’s search is not applied', async ({
  page,
}) => {
  const pools = (await page.request.get('/api/v1/pools').then((r) => r.json())) as {
    items: { id: string; name: string }[];
  };
  const pool = pools.items[0]!;
  const found = card(page, 'What keeps failing to start');

  await goto(page, '/runners?state=failed', 'Runners');
  await expect(found.getByTestId('analysis-summary')).toBeVisible();
  await expect(found.getByText(/^Every pool and host, /)).toBeVisible();
  await expect(found.getByText("The list's search filter is not applied here")).toHaveCount(0);

  await goto(page, '/runners?state=failed&q=no-such-runner-name', 'Runners');
  await expect(found.getByTestId('analysis-summary')).toBeVisible();
  await expect(
    found.getByText(
      "The list's search filter is not applied here: this analysis reads every start in the period, narrowed only by pool and host.",
    ),
  ).toBeVisible();

  // Narrowed by the page's own controls, the card says which pool it covers.
  await goto(page, `/runners?state=failed&pool_id=${pool.id}`, 'Runners');
  await expect(found.getByTestId('analysis-summary')).toBeVisible();
  await expect(found.getByText(`Pool ${pool.name}, `)).toBeVisible();
});

// When a pool fails on one host while it starts on others, the route reports the
// pool and, beside it, that host counting the pool's starts alone (pool_id set):
// the host's trouble, not the pool's. Drawn as a bare host row, "6 of 8 starts
// failed" reads as the host's own tally across every pool, which is the one
// thing the route says it is not, and the two rows look like two findings. The
// demo's seed does not produce this case, so the answer is the demo's own with
// its groups replaced by the pair the schema describes.
test('a host group that counts one pool’s starts alone says which pool', async ({ page }) => {
  const pools = (await page.request.get('/api/v1/pools').then((r) => r.json())) as {
    items: { id: string; name: string }[];
  };
  const pool = pools.items[0]!;
  await page.route('**/api/v1/analysis/startup-failures**', async (route) => {
    const read = await readThrough<{
      evidence: { groups: Record<string, unknown>[] };
    }>(route);
    if (!read) return;
    const { response, body } = read;
    const host = body.evidence.groups[0]!;
    body.evidence.groups = [
      { ...host, scope: 'pool', id: pool.id, name: pool.name, pool_id: '' },
      { ...host, scope: 'host', pool_id: pool.id, starts: 8, failures: 6 },
    ];
    await route.fulfill({ response, json: body });
  });
  await goto(page, '/runners?state=failed', 'Runners');
  const found = card(page, 'What keeps failing to start');
  await found.getByText('Evidence', { exact: true }).click();
  const rows = found.getByRole('list', { name: 'Where they fail' }).getByRole('listitem');
  await expect(rows).toHaveCount(2);
  // The pool's own row is the pool's tally, and says nothing more.
  await expect(rows.nth(0)).toContainText(`Pool ${pool.name}`);
  await expect(rows.nth(0)).not.toContainText('alone');
  // The host's row says whose starts it counts, and links the pool.
  await expect(rows.nth(1)).toContainText(`(starts of pool ${pool.name} alone)`);
  await expect(rows.nth(1).getByRole('link', { name: pool.name })).toHaveAttribute(
    'href',
    `/pools/${pool.id}`,
  );
});

test("a refusal is the route's own sentence, and the page's facts stand", async ({ page }) => {
  // A refusal the route gives, with its code; the card shows the sentence
  // rather than a generic failure, because the sentence says what to change.
  await page.route('**/api/v1/analysis/startup-failures**', (route) =>
    route.fulfill({
      status: 400,
      json: {
        error: {
          code: 'bad_request',
          field: 'since',
          message:
            'the period from since to until is 91 days, and an analysis covers at most 90 days; narrow it, or raise limits.job_stats_window',
        },
      },
    }),
  );
  await goto(page, '/runners?state=failed', 'Runners');
  const found = card(page, 'What keeps failing to start');
  // A refusal is the route declining a question it understood: a status,
  // which a screen reader reads when it is next idle, not an alert.
  await expect(found.getByRole('alert')).toHaveCount(0);
  await expect(found.getByRole('status')).toHaveText(
    'The period from since to until is 91 days, and an analysis covers at most 90 days; narrow it, or raise limits.job_stats_window.',
  );
  // The list of runners is still the page's to show. Its grid, not a row:
  // whether any runner is still in the failed state by the time this runs is
  // not this test's question.
  await expect(page.getByRole('grid', { name: 'Runners' })).toBeVisible();
  await expect(page.getByRole('heading', { level: 1, name: 'Runners' })).toBeVisible();
});

test('a failed request is an alert, and names the request to quote', async ({ page }) => {
  await page.route('**/api/v1/analysis/startup-failures**', (route) =>
    route.fulfill({
      status: 500,
      json: {
        error: {
          code: 'internal',
          message:
            'something went wrong while analysing startup failures. Quote the request ID to whoever operates this instance; the cause is in its log.',
          detail: 'request req_test01',
        },
      },
    }),
  );
  await goto(page, '/runners?state=failed', 'Runners');
  await expect(card(page, 'What keeps failing to start').getByRole('alert')).toHaveText(
    'Something went wrong while analysing startup failures. Quote req_test01 to whoever operates this instance; the cause is in its log.',
  );
});

/** Collected rather than dismissed quietly: a dialog here is the bug. */
function watchForDialogs(page: Page): Dialog[] {
  const seen: Dialog[] = [];
  page.on('dialog', (d) => {
    seen.push(d);
    void d.dismiss();
  });
  return seen;
}

test('text a host or a workflow wrote is shown as text', async ({ page }) => {
  const dialogs = watchForDialogs(page);
  const message = 'pull failed <img src=x onerror=alert(1)><script>alert(2)</script>';
  await page.route('**/api/v1/analysis/startup-failures**', async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    const { response: response, body } = read;
    for (const group of body.evidence.groups) {
      group.last_message = message;
      group.name = '<b>loud</b>';
    }
    await route.fulfill({ response, json: body });
  });
  await goto(page, '/runners?state=failed', 'Runners');
  const found = card(page, 'What keeps failing to start');
  await found.getByText('Evidence', { exact: true }).click();
  // The payload really arrived, as the characters it is.
  await expect(found.getByText(message).first()).toBeVisible();
  await expect(found.getByText('<b>loud</b>').first()).toBeVisible();
  await expect(page.locator('img[src="x"]')).toHaveCount(0);
  await expect(page.locator('script:has-text("alert(2)")')).toHaveCount(0);
  await expect(found.locator('b')).toHaveCount(0);
  expect(dialogs, 'a dialog opened, so something in the text ran').toHaveLength(0);
});

test('withheld job names are skipped, and the step that says so is shown', async ({ page }) => {
  const hosts = (await page.request.get('/api/v1/hosts').then((r) => r.json())) as {
    items: { id: string; name: string }[];
  };
  const host = hosts.items.find((h) => h.name === FIXTURE.machineHost)!;
  const withheld =
    'The repository, workflow and job name of each event were left out, because this token cannot read jobs; use a token that also has the jobs:read scope to see which jobs they were.';
  // What a token without jobs:read is sent: the route blanks the names and
  // adds a step with neither a setting nor a code.
  await page.route('**/api/v1/analysis/host-pressure**', async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    const { response: response, body } = read;
    for (const e of body.evidence.events) {
      e.repo = '';
      e.workflow = '';
      e.job_name = '';
    }
    body.evidence.job_names_withheld = true;
    body.next_steps.push({ text: withheld });
    await route.fulfill({ response, json: body });
  });
  await goto(page, `/hosts/${host.id}`, FIXTURE.machineHost);
  const found = card(page, 'Whether its failures came under pressure');
  await expect(found.getByText(withheld)).toHaveCount(1);
  await found.getByText('Evidence', { exact: true }).click();
  const events = found.getByRole('list', { name: 'What went wrong' }).getByRole('listitem');
  await expect(events.first()).toBeVisible();
  // No row of separators standing where names would be.
  for (const text of await events.allTextContents()) expect(text).not.toMatch(/·\s*·/);
});

/**
 * A public view's visitor, as comparisons.spec.ts makes one: the controller
 * answers as it does with authentication off, and the page is told the view is
 * public and the session is a visitor's.
 */
async function asVisitor(page: Page): Promise<void> {
  await page.route('**/api/v1/meta', async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    const { response: response, body } = read;
    body.auth_disabled = false;
    body.bootstrap_required = false;
    body.public_view = true;
    await route.fulfill({ response, json: body });
  });
  await page.route('**/api/v1/auth/session', async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    const { response: response, body } = read;
    body.role = 'viewer';
    body.kind = 'public';
    await route.fulfill({ response, json: body });
  });
}

// Every analysis route refuses a public view's visitor before it reads anything
// (internal/api/router.go), and the product's rule for what the API refuses a
// visitor is to neither offer it nor ask for it (sections.ts, RepairOffers).
// Without that, every page a visitor may open drew a card whose only content was
// "sign in to do this", after a request certain to be refused.
test("a public view's visitor gets no analysis card and no analysis request", async ({ page }) => {
  const hosts = (await page.request.get('/api/v1/hosts').then((r) => r.json())) as {
    items: { id: string; name: string }[];
  };
  const host = hosts.items.find((h) => h.name === FIXTURE.machineHost)!;
  await asVisitor(page);
  const asked: string[] = [];
  page.on('request', (request) => {
    if (request.url().includes('/api/v1/analysis/')) asked.push(request.url());
  });
  // Each page's own read is the proof the page has mounted and run its effects:
  // a card's request, were it made, is issued in the same pass and before it.
  const pages = [
    {
      path: '/jobs?state=queued',
      heading: 'Jobs',
      title: 'Why jobs waited',
      read: '/api/v1/jobs?',
    },
    {
      path: '/usage',
      heading: 'Usage',
      title: 'What got worse and what got better',
      read: '/api/v1/jobs/stats',
    },
    {
      path: `/hosts/${host.id}`,
      heading: FIXTURE.machineHost,
      title: 'Whether its failures came under pressure',
      read: `/api/v1/hosts/${host.id}`,
    },
    {
      path: '/runners?state=failed',
      heading: 'Runners',
      title: 'What keeps failing to start',
      read: '/api/v1/runners?',
    },
  ];
  for (const p of pages) {
    const read = page.waitForResponse((response) => response.url().includes(p.read));
    await goto(page, p.path, p.heading);
    await read;
    await expect(card(page, p.title), `${p.path} for a visitor`).toHaveCount(0);
  }
  expect(asked, 'the analysis routes a visitor asked').toEqual([]);
});
