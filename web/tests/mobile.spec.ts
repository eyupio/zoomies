/**
 * Zoomies on a phone.
 *
 * docs/ui-guidelines.md makes read-only monitoring on a phone a requirement,
 * and the phone breakpoint has one rule that matters above the rest: nothing
 * scrolls sideways. An operator woken at 3am reading a dashboard one-handed
 * cannot chase a column that is off the right edge of the screen.
 *
 * These run only in the Pixel 7 project; the desktop project skips them.
 */
import { expect, test, type Page } from '@playwright/test';
import {
  browserOverride,
  dataRows,
  documentWidth,
  FIXTURE,
  goto,
  grid,
  nav,
  menuEntry,
  navEntry,
  navMenu,
  openNavMenu,
  pageHeading,
  PRIMARY_SECTIONS,
  SECTIONS,
  sectionHeading,
} from './support/fixtures';

test.use(browserOverride);

test.skip(({ isMobile }) => !isMobile, 'the phone layout, in the Pixel 7 project');

/** How far the page can be scrolled sideways, which must be nowhere. */
async function expectNoSidewaysScroll(page: Page, where: string): Promise<void> {
  const { scrollWidth, clientWidth } = await documentWidth(page);
  expect(
    scrollWidth,
    `${where} is ${scrollWidth}px wide in a ${clientWidth}px window, so the page scrolls sideways`,
  ).toBeLessThanOrEqual(clientWidth);
}

test('the Overview is readable without scrolling sideways', async ({ page }) => {
  // This caught two real bugs: `.app` kept `align-items: flex-start` where the
  // row becomes a column, so the content column was sized to its content
  // rather than to the window, and the top bar's account trigger
  // ("Authentication disabled" on this instance) was long enough to push the
  // document past the edge on its own.

  await goto(page, '/', 'Overview');
  await expect(page.getByRole('region', { name: 'Recent scaling' })).toBeVisible();

  // The tiles stack rather than shrinking into unreadable columns.
  const tiles = page.getByRole('link', { name: /^(Queued jobs|Running jobs|Live runners)/ });
  await expect(tiles).toHaveCount(3);
  for (let i = 0; i < 3; i++) {
    await expect(tiles.nth(i)).toBeVisible();
  }
  // The problems summary is a line, not a panel, and the count that opens the
  // full list is in the top bar where it is on every other page too.
  await expect(page.getByText(/needs? your attention\.$/).first()).toBeVisible();
  await expect(page.getByRole('button', { name: /^Problems\./ })).toBeVisible();

  await expectNoSidewaysScroll(page, 'the Overview');
});

test('migration labels and job exceptions stay readable', async ({ page }) => {
  await goto(page, '/migrate', 'Migrate repositories');

  // The demo has one installation and preselects repositories that can move.
  // Step through Installation and Repositories to the first wide decision.
  await page.getByRole('button', { name: 'Next' }).click();
  await page.getByRole('button', { name: 'Next' }).click();

  const mapping = page.getByLabel('What replaces ubuntu-latest');
  await expect(mapping).toBeVisible();
  expect((await mapping.boundingBox())?.width).toBeGreaterThan(180);

  await page.getByRole('button', { name: 'Next' }).click();
  const build = page.getByLabel('Where build in .github/workflows/ci.yml runs, in acme/widgets');
  await expect(build).toBeVisible();
  expect((await build.boundingBox())?.width).toBeGreaterThan(180);

  // A blocked row used to be squeezed into a 30px reason column, producing
  // the one-word-per-line stack from the reported phone capture.
  const reason = page.getByText('${{ }} expression').first();
  await expect(reason).toBeVisible();
  expect((await reason.boundingBox())?.width).toBeGreaterThan(180);
  await expectNoSidewaysScroll(page, 'the migration wizard');
});

test('the navigation is a bar at the bottom, aligned and reaching every page', async ({ page }) => {
  await goto(page, '/', 'Overview');

  const bar = nav(page);
  await expect(bar).toBeVisible();
  const box = await bar.boundingBox();
  const viewport = page.viewportSize();
  expect(box, 'the navigation is on screen').not.toBeNull();
  expect(
    (box?.y ?? 0) + (box?.height ?? 0) / 2,
    'the navigation sits at the bottom, under the thumb',
  ).toBeGreaterThan((viewport?.height ?? 0) / 2);
  // The desktop collapse control is gone, because there is nothing to collapse.
  await expect(page.getByRole('button', { name: /the navigation/ })).toHaveCount(0);

  // Four sections and the menu button, each an equal share of the width and
  // each under its own word.
  //
  // This is the regression the whole test exists for. `.nav.collapsed` is two
  // classes and the phone rules were one, a media query adds no specificity of
  // its own, and the collapse pref defaulted to on at any width under 1180px --
  // so on a phone the bar was 56px wide with all ten entries piled into the
  // corner and the masthead sitting on top of them.
  const entries = bar.getByRole('listitem');
  await expect(entries).toHaveCount(5);
  const widths: number[] = [];
  for (let i = 0; i < 5; i++) {
    const entry = await entries.nth(i).boundingBox();
    expect(entry, 'every entry has a box').not.toBeNull();
    expect(entry?.x ?? -1, 'no entry starts off the left edge').toBeGreaterThanOrEqual(0);
    expect(
      (entry?.x ?? 0) + (entry?.width ?? 0),
      'no entry runs off the right edge',
    ).toBeLessThanOrEqual((viewport?.width ?? 0) + 1);
    widths.push(entry?.width ?? 0);
  }
  expect(
    Math.max(...widths) - Math.min(...widths),
    'the entries share the width evenly rather than bunching up',
  ).toBeLessThanOrEqual(1);
  expect(
    Math.min(...widths),
    'each entry is wide enough to be a thumb target',
  ).toBeGreaterThanOrEqual(44);
  for (const label of ['Overview', 'Pools', 'Runners', 'Jobs', 'More']) {
    await expect(bar.getByText(label, { exact: true })).toBeVisible();
  }

  // Every section is one press away from the Overview: the four in the bar
  // directly, the other six through the menu the fifth entry opens.
  //
  // Each hop starts from the Overview on purpose: a press from a page that has
  // been scrolled tells you less than a press from a known one.
  for (const section of SECTIONS) {
    await goto(page, '/', 'Overview');
    if (PRIMARY_SECTIONS.includes(section.path)) {
      await navEntry(page, section.path).click();
      await expect(pageHeading(page, sectionHeading(section))).toBeVisible();
      await expect(navEntry(page, section.path)).toHaveAttribute('aria-current', 'page');
      continue;
    }
    await openNavMenu(page);
    await menuEntry(page, section.path).click();
    await expect(pageHeading(page, sectionHeading(section))).toBeVisible();
    // Choosing a section closes the menu: nobody wants to dismiss a menu they
    // have already used.
    await expect(navMenu(page)).toHaveCount(0);
    // And the menu says where you are when it is opened again.
    await openNavMenu(page);
    await expect(menuEntry(page, section.path)).toHaveAttribute('aria-current', 'page');
    await page.keyboard.press('Escape');
    await expect(navMenu(page)).toHaveCount(0);
  }
});

test('the side menu closes on Escape and on the scrim, and lists every section', async ({
  page,
}) => {
  await goto(page, '/', 'Overview');

  const menu = await openNavMenu(page);
  for (const section of SECTIONS) {
    await expect(menuEntry(page, section.path)).toBeVisible();
  }
  // The masthead a phone otherwise never sees, since the bottom bar has no room
  // for one.
  await expect(menu.getByText('Zoomies', { exact: true })).toBeVisible();

  await page.keyboard.press('Escape');
  await expect(navMenu(page)).toHaveCount(0);

  await openNavMenu(page);
  // The scrim is the part of the overlay outside the panel; pressing the far
  // right edge is pressing it.
  const size = page.viewportSize();
  await page.mouse.click((size?.width ?? 400) - 8, (size?.height ?? 800) / 2);
  await expect(navMenu(page)).toHaveCount(0);
});

test('the grids stay inside the screen instead of overflowing it', async ({ page }) => {
  // This one caught the subtler half of the same problem. Even once the column
  // stretched, a visually-hidden `.sr-only` span in a cell 500px along a
  // 1200px table was positioned against the page rather than against the
  // grid's own scroll frame, escaped its clipping, and made the document that
  // wide -- so the page scrolled sideways instead of the grid scrolling inside
  // its frame, and Chrome zoomed out to fit until the toolbar and the row
  // actions could not be pressed.

  for (const [path, heading, label] of [
    ['/runners', 'Runners', 'Runners'],
    ['/jobs', 'Jobs', 'Jobs'],
    ['/pools', 'Pools', 'Pools'],
  ] as const) {
    await goto(page, path, heading);
    await expect(dataRows(grid(page, label)).first()).toBeVisible();

    // A wide table is fine -- it may scroll inside its own frame -- but the
    // page around it must not.
    await expectNoSidewaysScroll(page, `the ${heading} grid`);
  }
});

/*
 * The grids above were the list pages. A detail page is the other half of the
 * phone story and had no test at all, which is how this shipped: a pool page
 * on a 412px phone was 500-odd pixels wide, so the name was cut off mid-word,
 * the runner cards were clipped on the left, and Delete sat off the right edge
 * where it could not be pressed.
 *
 * The cause was one row shared by every detail page. PageHeader's actions row
 * is a flex row that never wrapped, and the buttons in it do not wrap their
 * own labels, so five of them -- refresh, prewarm, disable, edit, delete --
 * demanded their full width and pushed the document out. Giving it `width:
 * 100%` on a phone made the row wide without letting it wrap, which is why the
 * breakpoint that was already there did not save it.
 *
 * Every detail page with an actions row is walked, not just the pool one that
 * was reported, because the row is the same row on all of them.
 */
test('a detail page stays inside the screen, actions and all', async ({ page }) => {
  for (const [path, heading] of [
    [`/pools`, 'Pools'],
    [`/runners/${FIXTURE.busyRunnerId}`, FIXTURE.busyRunner],
  ] as const) {
    await goto(page, path, heading);
    await expectNoSidewaysScroll(page, `the ${heading} page`);
  }

  // The pool page is reached the way an operator reaches it, through the row,
  // since its id is not fixed by the seed the way the busy runner's is.
  await goto(page, '/pools', 'Pools');
  await page.getByRole('link', { name: FIXTURE.linuxPool }).first().click();
  await expect(pageHeading(page, FIXTURE.linuxPool)).toBeVisible();
  await expectNoSidewaysScroll(page, `the ${FIXTURE.linuxPool} page`);

  // The document fitting is necessary but not sufficient: a button can still
  // sit past the right edge of a page that does not scroll, and one that does
  // is one an operator on a phone cannot press. It was Delete that ended up
  // there -- last in the row, and the one whose being half off the screen is
  // worst in both directions. Every visible control is checked rather than
  // those four by name, since the row is built from whatever the page passes.
  const width = (await documentWidth(page)).clientWidth;
  const offscreen = await page.evaluate((limit) => {
    const out: string[] = [];
    for (const el of document.querySelectorAll('button, a[href]')) {
      const box = el.getBoundingClientRect();
      if (box.width === 0 && box.height === 0) continue;
      if (box.right > limit + 0.5 || box.left < -0.5) {
        out.push(
          `${(el.textContent ?? '').trim() || el.getAttribute('aria-label')} at ${Math.round(box.left)}..${Math.round(box.right)}`,
        );
      }
    }
    return out;
  }, width);
  expect(offscreen, `these controls are outside a ${width}px window`).toEqual([]);
});

/*
 * The usage report is ten columns wide, which is not a table on a 412px phone
 * however it scrolls: the heading is cut off mid-word, every pool reads
 * "zoomies-demo-lin..." and is indistinguishable from the next, and the figure
 * you scrolled to belongs to a row you can no longer name. On a phone each row
 * is a card instead, so this holds the three things that makes it readable --
 * the page does not scroll sideways, no row name is truncated, and every
 * figure carries its own heading.
 */
test('the usage report reads as cards on a phone, with nothing cut off', async ({ page }) => {
  for (const grouping of ['pool', 'repository', 'workflow', 'installation'] as const) {
    await goto(page, `/usage?group_by=${grouping}`, 'Usage');
    await expect(page.getByRole('table')).toBeVisible();
    await expectNoSidewaysScroll(page, `the usage report grouped by ${grouping}`);
  }

  await goto(page, '/usage', 'Usage');
  const names = page.getByRole('rowheader');
  await expect(names.first()).toBeVisible();

  // Every row's name is rendered in full. Truncation here is what made two
  // different pools read as the same row.
  //
  // A name longer than the card wraps instead of being cut. Asserted on the
  // rule rather than on the rendering, because every name the demo fleet has
  // fits either way -- a measured check here would pass whatever the CSS said,
  // and the pools that read alike on a real fleet have longer names than these.
  const nowrap = await names.evaluateAll((els) =>
    els
      .flatMap((el) => [el, ...Array.from(el.querySelectorAll<HTMLElement>('*'))])
      .filter((el) => {
        const style = getComputedStyle(el);
        return style.whiteSpace === 'nowrap' && style.overflow === 'hidden';
      })
      .map((el) => el.textContent ?? ''),
  );
  expect(nowrap, 'a row name is still held to one clipped line').toEqual([]);

  // And every figure says what it is, because the column headings are gone.
  // The rendered pseudo-element is what is checked, not the attribute it is
  // drawn from: a rule that stopped drawing it would leave the attribute in
  // place and a column of unexplained numbers on screen.
  const unlabelled = await page.getByRole('cell').evaluateAll((els) =>
    els
      .filter((el) => {
        const label = getComputedStyle(el, '::before').content;
        return !label || label === 'none' || label === '""';
      })
      .map((el) => el.textContent ?? ''),
  );
  expect(unlabelled, 'a figure has no heading of its own on a phone').toEqual([]);

  // And the figures stack rather than running across, which is the whole of
  // what makes ten columns fit: two cells of one row share a left edge and sit
  // at different heights. Laid out as a table row they would do the opposite.
  const first = page.getByRole('row').nth(1).getByRole('cell');
  const a = await first.nth(0).boundingBox();
  const b = await first.nth(1).boundingBox();
  expect(a && b, 'the first row has no figures to lay out').toBeTruthy();
  expect(b!.y, 'the figures are laid out across the row, not down the card').toBeGreaterThan(a!.y);
  expect(Math.abs(b!.x - a!.x), 'the figures do not share a left edge').toBeLessThan(2);

  // The card holds its contents off its own edge by more than the hairline of
  // its border -- a row whose text starts on its own boundary reads as a wall
  // of figures rather than as a card.
  const card = await page.getByRole('row').nth(1).boundingBox();
  expect(a!.x - card!.x, 'the card gives its contents no room off its own edge').toBeGreaterThan(4);
});

test('a grid is still usable at this width', async ({ page }) => {
  await goto(page, '/runners', 'Runners');
  const rows = dataRows(grid(page, 'Runners'));
  await expect(rows.first()).toBeVisible();

  // Whatever the layout does, the rows are there and one can be opened. Six
  // of the seeded runners survive indefinitely; the rest depend on how long
  // the controller has been up. See runners.spec.ts.
  expect(await rows.count()).toBeGreaterThanOrEqual(6);
  const name = (await rows.first().getByRole('link').first().innerText()).trim();
  await rows.first().getByRole('link', { name }).click();
  await expect(pageHeading(page, name)).toBeVisible();
  await expect(page.getByRole('region', { name: 'Timeline' })).toBeVisible();
});

test('the Settings tables stay inside the screen once they have rows', async ({ page }) => {
  // The join-tokens table caught this one: a table wider than a phone scrolls
  // inside its own box, yet mobile Chrome still counted the clipped part
  // towards the page's width, grew the layout viewport to fit, and the fixed
  // bottom navigation grew with it -- so the whole page scrolled sideways. The
  // accounts and API-token tables are the same shape, and the test server
  // starts with neither, so each is given a row here. Unique names, so a retry
  // does not trip over a leftover from a failed run.
  const stamp = Date.now();
  let userId = '';
  let tokenId = '';
  try {
    const user = await page.request.post('/api/v1/users', {
      data: {
        username: `e2e-user-${stamp}`,
        password: 'correct horse battery staple',
        display_name: 'Someone With A Long Display Name',
        email: `e2e-user-${stamp}@example.com`,
        role: 'viewer',
      },
    });
    expect(user.ok(), 'the account was created').toBeTruthy();
    userId = ((await user.json()) as { id: string }).id;
    const token = await page.request.post('/api/v1/tokens', {
      data: {
        name: `e2e-token-${stamp}`,
        role: 'operator',
        scopes: ['pools:read', 'runners:read', 'jobs:read'],
        expires_in: '1h',
      },
    });
    expect(token.ok(), 'the token was created').toBeTruthy();
    tokenId = ((await token.json()) as { id: string }).id;

    await goto(page, '/settings', 'Settings');
    const accounts = page.getByRole('table', { name: 'Accounts' });
    await expect(accounts.getByRole('row').filter({ hasText: `e2e-user-${stamp}` })).toBeVisible();
    await expectNoSidewaysScroll(page, 'the Settings page listing an account');

    await page.getByRole('tab', { name: 'API tokens' }).click();
    const tokens = page.getByRole('table', { name: 'API tokens' });
    await expect(tokens.getByRole('row').filter({ hasText: `e2e-token-${stamp}` })).toBeVisible();
    await expectNoSidewaysScroll(page, 'the Settings page listing an API token');
  } finally {
    // A token cannot be deleted, only revoked: its row stays, marked revoked,
    // so the audit trail keeps pointing at something.
    if (tokenId) await page.request.delete(`/api/v1/tokens/${tokenId}`);
    if (userId) await page.request.delete(`/api/v1/users/${userId}`);
  }
});

test('every text control is at least 16px, so tapping one does not zoom the page', async ({
  page,
}) => {
  // The rule the guidelines give in section 1.2, checked where it applies.
  // Mobile Safari zooms the whole viewport whenever a focused control's text
  // is under 16px, and the viewport meta deliberately sets no maximum-scale --
  // so a field a pixel under the line jumps a 360px page to roughly 410px of
  // effective width and runs the card off both edges, once per tap. Three
  // controls were written outside the primitives and missed it entirely: the
  // page-size select, the date range, and the pool wizard's label field.
  const PAGES = [
    { path: '/runners', heading: 'Runners' },
    { path: '/jobs', heading: 'Jobs' },
    { path: '/usage', heading: 'Usage' },
    { path: '/audit', heading: 'Audit' },
    { path: '/pools/new', heading: 'Create a pool' },
  ] as const;

  for (const { path, heading } of PAGES) {
    await goto(page, path, heading);
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();

    const small = await page.evaluate(() =>
      Array.from(document.querySelectorAll('input, select, textarea'))
        .filter((el) => {
          const type = el.getAttribute('type');
          // Checkboxes, radios and range sliders have no editable text to zoom to.
          if (type === 'checkbox' || type === 'radio' || type === 'range' || type === 'hidden')
            return false;
          if (!(el as HTMLElement).offsetParent && el.getClientRects().length === 0) return false;
          return Number.parseFloat(getComputedStyle(el).fontSize) < 16;
        })
        .map((el) => `${el.tagName.toLowerCase()}[${el.getAttribute('aria-label') ?? el.id}]`),
    );
    expect(small, `${path}: every text control is 16px or more on a phone`).toEqual([]);
  }
});
