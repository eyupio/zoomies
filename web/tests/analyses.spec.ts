/**
 * The four analyses, each a card on the page where its question is already
 * asked: why the queued jobs waited on the Jobs page's Queued view, what got
 * worse beside Usage's table of releases, whether a host's failures came under
 * pressure on the host's own page, and which host or pool keeps failing to
 * start runners when the Runners page is narrowed to failed ones.
 *
 * The binding check is one answer: a card's summary is exactly the `summary`
 * of the same route asked for the same period, which is what an agent's tool
 * and `zoomies analyse` print. The card asks for an explicit period, so the
 * test asks with the very address the page used rather than with a default
 * that would have moved on by the time it asked.
 *
 * The demo seeds a cause for each analysis between 22 and 20 hours ago (see
 * seedAnalyses in internal/controller/seed_analysis.go), inside the last day
 * that the Jobs, Runners and host pages ask about. Usage asks about its own
 * range, and is opened on thirty days so the demo's two releases are in it.
 */
import { expect, test, type Dialog, type Page } from '@playwright/test';
import { expectNoSidewaysScroll, FIXTURE, goto } from './support/fixtures';

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
): Promise<{ summary: string; confidence: string }> {
  const summary = card(page, title).getByTestId('analysis-summary');
  await expect(summary).toBeVisible();
  const url = requests.at(-1);
  expect(url, 'the page asked the analysis route').toBeTruthy();
  const query = new URL(url!).searchParams;
  // An explicit period, or the test's own request would read a later one.
  expect(query.get('since')).toBeTruthy();
  expect(query.get('until')).toBeTruthy();
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
  await expectOneAnswer(page, 'Why jobs waited', requests);
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
  const finding = await expectOneAnswer(page, title, requests);
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
      /^v1\.3\.2 from .+; v1\.3\.3 from .+\. Cancelled and skipped jobs are left out\.$/,
    ),
  ).toBeVisible();
  await expectAccessible(page, title);
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
    const response = await route.fetch();
    const body = await response.json();
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
    const response = await route.fetch();
    const body = await response.json();
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
