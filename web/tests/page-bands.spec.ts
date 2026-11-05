import { expect, test } from '@playwright/test';
import { FIXTURE, goto } from './support/fixtures';
import type { Job } from '../src/lib/api/types';

// A band is the dark stage at the top of a page that says its answer in a sentence. Each
// is on by default and each can be switched off in Appearance, per page, in this browser.
test.use({ viewport: { width: 1440, height: 900 } });

const PAGES = [
  { path: '/', heading: 'Overview', label: 'Overview', band: 'fleet-status' },
  { path: '/runners', heading: 'Runners', label: 'Runners', band: 'band-runners' },
  { path: '/pools', heading: 'Pools', label: 'Pools', band: 'band-pools' },
  { path: '/queue', heading: 'Queue', label: 'Queue', band: 'band-queue' },
  { path: '/jobs', heading: 'Jobs', label: 'Jobs', band: 'band-jobs' },
  { path: '/workflows', heading: 'Workflows', label: 'Workflows', band: 'band-workflows' },
  { path: '/usage', heading: 'Usage', label: 'Usage', band: 'band-usage' },
  { path: '/hosts', heading: 'Hosts', label: 'Hosts', band: 'band-hosts' },
  { path: '/providers', heading: 'Providers', label: 'Providers', band: 'band-providers' },
  {
    path: '/installations',
    heading: 'Installations',
    label: 'Installations',
    band: 'band-installations',
  },
  {
    path: '/migrate',
    heading: 'Migrate repositories',
    label: 'Migrate',
    band: 'band-migrate',
  },
  { path: '/kennel', heading: 'Kennel Club', label: 'Kennel Club', band: 'band-kennel' },
] as const;

for (const item of PAGES) {
  test(`the ${item.label} page opens on its band`, async ({ page }) => {
    await goto(page, item.path, item.heading);
    await expect(page.getByTestId(item.band)).toBeVisible();
  });
}

test('Appearance has a switch for each page, all on, and switching one off hides only that band', async ({
  page,
}) => {
  await goto(page, '/settings/appearance', 'Appearance');
  const group = page.getByRole('group', { name: 'Page bands' });
  for (const item of PAGES) {
    await expect(group.getByRole('switch', { name: item.label, exact: true })).toBeChecked();
  }
  await group.getByRole('switch', { name: 'Pools', exact: true }).click();

  await goto(page, '/pools', 'Pools');
  await expect(page.getByTestId('band-pools')).toHaveCount(0);
  // The others are untouched.
  await goto(page, '/queue', 'Queue');
  await expect(page.getByTestId('band-queue')).toBeVisible();

  // The choice is remembered, and the switch is where it was left.
  await page.reload();
  await goto(page, '/settings/appearance', 'Appearance');
  await expect(
    page
      .getByRole('group', { name: 'Page bands' })
      .getByRole('switch', { name: 'Pools', exact: true }),
  ).not.toBeChecked();
});

test("switching the Overview's band off puts its problems line back", async ({ page }) => {
  await page.addInitScript(() =>
    localStorage.setItem('zoomies.prefs', JSON.stringify({ hiddenBands: ['overview'] })),
  );
  await goto(page, '/', 'Overview');
  await expect(page.getByTestId('fleet-status')).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
});

// A phone draws a band only to carry its picture, slim: the sentence, then the
// picture at the full width. A band with nothing to carry, or with its picture
// switched off, stays hidden so the rows are on the first screen as they were.
test('a phone draws a band to carry its picture, and only then', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 800 });
  await goto(page, '/runners', 'Runners');
  const band = page.getByTestId('band-runners');
  await expect(band).toBeVisible();
  await expect(band.getByTestId('band-slot-map')).toBeVisible();
  // The figures are in their own panel under it, not in the band as well.
  await expect(band.getByRole('list', { name: 'Key figures' })).toBeHidden();
  await expect(page.getByRole('list', { name: 'Key figures' })).toBeVisible();
  // The cut and the dismiss are for a screen with the room.
  await expect(band.getByRole('button', { name: /^Hide the .* band/ })).toBeHidden();

  // Nothing to carry: no band, as before.
  await goto(page, '/migrate', 'Migrate repositories');
  await expect(page.getByTestId('band-migrate')).toBeHidden();

  // The picture switched off in Appearance takes the band with it on a phone.
  await goto(page, '/settings/appearance', 'Appearance');
  await page.getByRole('switch', { name: 'Runners band picture' }).click();
  await goto(page, '/runners', 'Runners');
  await expect(page.getByTestId('band-runners')).toBeHidden();
  await expect(page.getByRole('list', { name: 'Key figures' })).toBeVisible();
});

// The cut is a desktop preference that syncs to every screen, so an Expanded
// choice must not draw an Expanded band on a phone: it takes the slim one.
test('a phone draws the slim band whatever cut the operator chose', async ({ page }) => {
  await goto(page, '/settings/appearance', 'Appearance');
  const size = page.getByRole('group', { name: 'Runners band size' });
  const slim = async () => {
    await goto(page, '/runners', 'Runners');
    return page
      .getByTestId('band-runners')
      .locator('.headline')
      .evaluate((el) => getComputedStyle(el).fontSize);
  };
  await page.setViewportSize({ width: 375, height: 812 });
  const before = await slim();
  await goto(page, '/settings/appearance', 'Appearance');
  await size.getByRole('button', { name: 'Expanded' }).click();
  expect(await slim(), 'an Expanded cut reads as slim on a phone').toBe(before);
});

// Each row of a picture has the width of the band and no more: its name and its
// note share a line and the picture has the next. Before that the name's column
// was squeezed to nothing and a row was a picture and a number with no one to
// say whose.
test('on a phone every picture fits the width and every row names what it is about', async ({
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 812 });
  for (const [path, heading, picture] of [
    ['/', 'Overview', 'fleet-punch-card'],
    ['/hosts', 'Hosts', 'band-slot-map'],
    ['/runners', 'Runners', 'band-slot-map'],
    ['/pools', 'Pools', 'band-pool-bars'],
    ['/queue', 'Queue', 'band-queue-trend'],
    ['/usage', 'Usage', 'band-usage-columns'],
    ['/jobs', 'Jobs', 'fleet-punch-card'],
    ['/workflows', 'Workflows', 'band-in-flight'],
  ] as const) {
    await goto(page, path, heading);
    const pic = page.getByTestId(picture).first();
    await expect(pic, `${heading} draws its picture`).toBeVisible();
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth - innerWidth),
      `${heading} has no sideways scroll`,
    ).toBeLessThanOrEqual(0);
    const box = await pic.boundingBox();
    expect(box?.width ?? 0, `${heading}'s picture has the band's width`).toBeGreaterThan(250);
    // A named row shows its name: a link with a width, not an empty column.
    for (const link of await pic.locator('a.name').all()) {
      expect((await link.boundingBox())?.width ?? 0, `${heading}: a row's name`).toBeGreaterThan(
        20,
      );
    }
  }
});

// Below 1000px the picture does not fit beside the words and still read, so it
// takes a row of its own under them: a tablet and a phone are one rule.
test('a tablet stacks the picture under the sentence and a laptop keeps it beside', async ({
  page,
}) => {
  await page.setViewportSize({ width: 820, height: 1000 });
  await goto(page, '/hosts', 'Hosts');
  let band = page.getByTestId('band-hosts');
  let headline = await band.locator('.headline').boundingBox();
  let picture = await band.getByTestId('band-slot-map').boundingBox();
  expect(picture?.y ?? 0, 'the picture is under the sentence').toBeGreaterThan(
    (headline?.y ?? 0) + (headline?.height ?? 0),
  );
  expect(picture?.width ?? 0, 'and has the band to itself').toBeGreaterThan(500);

  await page.setViewportSize({ width: 1280, height: 900 });
  await goto(page, '/hosts', 'Hosts');
  band = page.getByTestId('band-hosts');
  headline = await band.locator('.headline').boundingBox();
  picture = await band.getByTestId('band-slot-map').boundingBox();
  expect(picture?.x ?? 0, 'the picture is beside the sentence').toBeGreaterThan(
    (headline?.x ?? 0) + (headline?.width ?? 0) - 1,
  );
});

/**
 * The time a test that visits every page in turn is given. A visit takes about a
 * second on a quiet machine and several on a busy runner, and the list gains a page
 * every few weeks, so a fixed thirty seconds is a test that fails on the day the
 * runners are busy. The budget grows with the list, as the tooltips spec's does.
 */
const everyPageBudget = () => 30_000 + PAGES.length * 6_000;

test('every page opens on a slim band', async ({ page }) => {
  test.setTimeout(everyPageBudget());
  for (const item of PAGES) {
    await goto(page, item.path, item.heading);
    await expect(page.getByTestId(item.band)).toHaveAttribute('data-size', 'slim');
  }
});

test('a page can be moved between every size in Appearance, and stays there', async ({ page }) => {
  await goto(page, '/settings/appearance', 'Appearance');
  const size = page.getByRole('group', { name: 'Runners band size' });
  for (const label of ['Slim', 'Compact', 'Standard', 'Expanded']) {
    await expect(size.getByRole('button', { name: label, exact: true })).toBeVisible();
  }
  await expect(size.getByRole('button', { name: 'Slim' })).toHaveAttribute('aria-pressed', 'true');
  await size.getByRole('button', { name: 'Expanded' }).click();

  await goto(page, '/runners', 'Runners');
  await expect(page.getByTestId('band-runners')).toHaveAttribute('data-size', 'expanded');
  // Only that page moved.
  await goto(page, '/pools', 'Pools');
  await expect(page.getByTestId('band-pools')).toHaveAttribute('data-size', 'slim');

  await page.reload();
  await goto(page, '/runners', 'Runners');
  await expect(page.getByTestId('band-runners')).toHaveAttribute('data-size', 'expanded');
});

test('a band offers the same sizes on the band itself, and the choice is shared', async ({
  page,
}) => {
  await goto(page, '/runners', 'Runners');
  const band = page.getByTestId('band-runners');
  const size = band.getByRole('group', { name: 'Runners band size' });
  for (const label of ['Slim', 'Compact', 'Standard', 'Expanded']) {
    await expect(size.getByRole('button', { name: label, exact: true })).toBeVisible();
  }
  await size.getByRole('button', { name: 'Slim', exact: true }).click();
  // A size is a change, and a change asks where it lands before it is made.
  const panel = page.getByRole('dialog', { name: 'Slim band size, where to apply' });
  await expect(panel).toBeVisible();
  await panel.getByRole('button', { name: 'Apply to Runners' }).click();
  await expect(band).toHaveAttribute('data-size', 'slim');
  await expect(panel).toHaveCount(0);

  // Appearance holds the same choice, rather than a second one of its own.
  await goto(page, '/settings/appearance', 'Appearance');
  await expect(
    page
      .getByRole('group', { name: 'Runners band size' })
      .getByRole('button', { name: 'Slim', exact: true }),
  ).toHaveAttribute('aria-pressed', 'true');
});

test('a cut can be given to the pages picked one at a time', async ({ page }) => {
  await goto(page, '/runners', 'Runners');
  await page
    .getByTestId('band-runners')
    .getByRole('group', { name: 'Runners band size' })
    .getByRole('button', { name: 'Expanded', exact: true })
    .click();

  const panel = page.getByRole('dialog', { name: 'Expanded band size, where to apply' });
  await panel.getByRole('radio', { name: 'Choose pages' }).check();
  // The page the panel was opened from is chosen already; two more are ticked.
  await expect(panel.getByRole('checkbox', { name: 'Runners (this page)' })).toBeChecked();
  await panel.getByRole('checkbox', { name: 'Pools', exact: true }).check();
  await panel.getByRole('checkbox', { name: 'Jobs', exact: true }).check();
  await expect(panel.getByRole('button', { name: 'Apply to 3 pages' })).toBeEnabled();
  await panel.getByRole('button', { name: 'Apply to 3 pages' }).click();

  for (const item of [
    { path: '/runners', heading: 'Runners', band: 'band-runners' },
    { path: '/pools', heading: 'Pools', band: 'band-pools' },
    { path: '/jobs', heading: 'Jobs', band: 'band-jobs' },
  ]) {
    await goto(page, item.path, item.heading);
    await expect(page.getByTestId(item.band)).toHaveAttribute('data-size', 'expanded');
  }
  // A page that was not picked keeps its own cut.
  await goto(page, '/hosts', 'Hosts');
  await expect(page.getByTestId('band-hosts')).toHaveAttribute('data-size', 'slim');

  // Appearance reflects the three, and the all-pages control shows no answer
  // while the pages disagree.
  await goto(page, '/settings/appearance', 'Appearance');
  for (const label of ['Runners', 'Pools', 'Jobs']) {
    await expect(
      page
        .getByRole('group', { name: `${label} band size` })
        .getByRole('button', { name: 'Expanded', exact: true }),
    ).toHaveAttribute('aria-pressed', 'true');
  }
  for (const label of ['Slim', 'Compact', 'Standard', 'Expanded']) {
    await expect(
      page
        .getByRole('group', { name: "Every page's band size" })
        .getByRole('button', { name: label, exact: true }),
    ).toHaveAttribute('aria-pressed', 'false');
  }
});

test('one answer can give every band the same cut', async ({ page }) => {
  await goto(page, '/runners', 'Runners');
  await page
    .getByTestId('band-runners')
    .getByRole('group', { name: 'Runners band size' })
    .getByRole('button', { name: 'Slim', exact: true })
    .click();

  const panel = page.getByRole('dialog', { name: 'Slim band size, where to apply' });
  await panel.getByRole('radio', { name: 'Every page' }).check();
  await expect(panel.getByRole('button', { name: 'Apply to 12 pages' })).toBeVisible();
  await panel.getByRole('button', { name: 'Apply to 12 pages' }).click();

  await expect(page.getByTestId('band-runners')).toHaveAttribute('data-size', 'slim');
  await goto(page, '/hosts', 'Hosts');
  await expect(page.getByTestId('band-hosts')).toHaveAttribute('data-size', 'slim');
  // Every page means every page, the Overview's hero included.
  await goto(page, '/', 'Overview');
  await expect(page.getByTestId('fleet-status')).toHaveAttribute('data-size', 'slim');

  await goto(page, '/settings/appearance', 'Appearance');
  await expect(
    page
      .getByRole('group', { name: "Every page's band size" })
      .getByRole('button', { name: 'Slim', exact: true }),
  ).toHaveAttribute('aria-pressed', 'true');
});

test('a band offers no logo, even to a browser that once asked for one', async ({ page }) => {
  // Older builds let a band draw the dog, and a browser may still hold that choice.
  await page.addInitScript(() =>
    localStorage.setItem('zoomies.prefs', JSON.stringify({ bandLogo: true })),
  );
  await goto(page, '/runners', 'Runners');
  await expect(page.getByTestId('band-runners').locator('.art')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Runners band logo settings' })).toHaveCount(0);

  await goto(page, '/settings/appearance', 'Appearance');
  await expect(page.getByRole('switch', { name: 'Runners band logo' })).toHaveCount(0);
  await expect(page.getByRole('combobox', { name: 'Runners logo size' })).toHaveCount(0);
  await expect(page.getByRole('switch', { name: 'Show the logo in the band' })).toHaveCount(0);
});

test('a size is only offered for a band that is on', async ({ page }) => {
  await page.addInitScript(() =>
    localStorage.setItem('zoomies.prefs', JSON.stringify({ hiddenBands: ['jobs'] })),
  );
  await goto(page, '/settings/appearance', 'Appearance');
  const off = page.getByRole('group', { name: 'Jobs band size' });
  await expect(off.getByRole('button', { name: 'Standard' })).toBeDisabled();
  const on = page.getByRole('group', { name: 'Hosts band size' });
  await expect(on.getByRole('button', { name: 'Standard' })).toBeEnabled();
});

test('the cross asks first, says how to bring the band back, and only then hides it', async ({
  page,
}) => {
  await goto(page, '/pools', 'Pools');
  await page.getByRole('button', { name: 'Hide the Pools band' }).click();
  const dialog = page.getByRole('dialog', { name: 'Hide the Pools band?' });
  await expect(dialog).toContainText('Settings, then Appearance');
  await expect(dialog).toContainText('Pools under Page bands');

  // Cancelling leaves it exactly as it was.
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(page.getByTestId('band-pools')).toBeVisible();

  await page.getByRole('button', { name: 'Hide the Pools band' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Hide the band' }).click();
  await expect(page.getByTestId('band-pools')).toHaveCount(0);

  // The switch in Appearance is where the dialog said it would be.
  await goto(page, '/settings/appearance', 'Appearance');
  await expect(
    page
      .getByRole('group', { name: 'Page bands' })
      .getByRole('switch', { name: 'Pools', exact: true }),
  ).not.toBeChecked();
});

test('the Runners band carries the provisioning figures, and they stay when it is hidden', async ({
  page,
}) => {
  await goto(page, '/runners', 'Runners');
  const band = page.getByTestId('band-runners');
  await expect(band.getByRole('list', { name: 'Key figures' })).toBeVisible();
  await expect(band.getByRole('link', { name: /^Manage queue/ })).toBeVisible();

  await page.addInitScript(() =>
    localStorage.setItem('zoomies.prefs', JSON.stringify({ hiddenBands: ['runners'] })),
  );
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Runners', level: 1 })).toBeVisible();
  await expect(page.getByTestId('band-runners')).toHaveCount(0);
  // The same figures, on their own.
  const alone = page.getByRole('list', { name: 'Key figures' });
  await expect(alone).toBeVisible();
  await expect(alone.getByRole('link', { name: /^Jobs? queued/ })).toBeVisible();
  await expect(alone.getByRole('link', { name: /^Manage queue/ })).toBeVisible();
});

test('the Queue status filters live in the band, filter the list and take the reader to it', async ({
  page,
}) => {
  await goto(page, '/queue', 'Queue');
  const filter = page.getByTestId('band-queue').getByRole('button', { name: /^Paused/ });
  await expect(filter).toHaveAttribute('aria-pressed', 'false');
  await filter.click();
  await expect(filter).toHaveAttribute('aria-pressed', 'true');
  await expect(page).toHaveURL(/#queue-list$/);
  await expect(page.locator('#queue-list')).toBeFocused();
});

test('a figure that counts a list on the page narrows it and jumps to it', async ({ page }) => {
  await goto(page, '/runners', 'Runners');
  await page
    .getByTestId('band-runners')
    .getByRole('button', { name: /^Running/ })
    .click();
  // The figure is a shortcut into the evidence, not a trip to another page.
  await expect(page).toHaveURL(/[?&]state=busy/);
  await expect(page).toHaveURL(/#runners-list$/);
  await expect(page.locator('#runners-list')).toBeFocused();
});

test('the figures that count a section jump to it on their own page', async ({ page }) => {
  const figures = [
    {
      path: '/usage',
      heading: 'Usage',
      band: 'band-usage',
      figure: /^Failed jobs/,
      section: 'usage-detail',
    },
    {
      path: '/providers',
      heading: 'Providers',
      band: 'band-providers',
      figure: /^Machines/,
      section: 'machines',
    },
    {
      path: '/installations',
      heading: 'Installations',
      band: 'band-installations',
      figure: /^GitHub connection/,
      section: 'installations-list',
    },
  ];
  for (const item of figures) {
    await goto(page, item.path, item.heading);
    await page.getByTestId(item.band).getByRole('link', { name: item.figure }).click();
    await expect(page).toHaveURL(new RegExp(`#${item.section}$`));
    await expect(page.locator(`#${item.section}`)).toBeFocused();
  }
});

test('a figure whose list is on another page opens that page at the list', async ({ page }) => {
  await goto(page, '/runners', 'Runners');
  await page
    .getByTestId('band-runners')
    .getByRole('link', { name: /^Jobs? queued/ })
    .click();
  // The queue shows the work that is waiting, and the address names its list.
  await expect(page).toHaveURL(/\/queue#queue-list$/);
  await expect(page.getByRole('heading', { name: 'Queue', level: 1 })).toBeVisible();
  await expect(page.locator('#queue-list')).toBeFocused();

  await page.goBack();
  await page
    .getByTestId('band-runners')
    .getByRole('link', { name: /^Ready/ })
    .click();
  await expect(page).toHaveURL(/\/queue\?provisioning=ready#queue-list$/);
  await expect(page.getByRole('heading', { name: 'Queue', level: 1 })).toBeVisible();
  await expect(page.locator('#queue-list')).toBeFocused();
});

test('every page names itself, underlines it and says what it is', async ({ page }) => {
  test.setTimeout(everyPageBudget());
  for (const item of PAGES) {
    await goto(page, item.path, item.heading);
    const header = page.locator('.page-header');
    await expect(header.getByRole('heading', { name: item.heading, level: 1 })).toBeVisible();
    await expect(header.locator('.rule')).toBeVisible();
    await expect(header.locator('p.subtitle')).toBeVisible();
  }
});

test('a phone gets its figures in their own panel, not in the band', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 800 });
  await goto(page, '/queue', 'Queue');
  await expect(
    page.getByTestId('band-queue').getByRole('list', { name: 'Key figures' }),
  ).toBeHidden();
  await expect(page.getByRole('list', { name: 'Key figures' })).toBeVisible();
});

test('on a phone a figure still opens the page and the list it counts', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 800 });
  await goto(page, '/runners', 'Runners');
  await page
    .getByRole('list', { name: 'Key figures' })
    .getByRole('link', { name: /^Jobs? queued/ })
    .click();
  await expect(page).toHaveURL(/\/queue#queue-list$/);
  await expect(page.locator('#queue-list')).toBeFocused();
});

/**
 * The Overview's band carries the week as a punch card beside its sentence,
 * where the dog stands when it is shown. What this protects: it is data and
 * not decoration (every hour says its figures in words), it keeps out of the
 * Slim cut, and an hour of it is a way into History with that hour selected.
 */
test('an hour of the punch card says what it holds on hover, as the matrix does', async ({
  page,
}) => {
  test.skip(Boolean(test.info().project.use.isMobile), 'a hover has no finger');
  await goto(page, '/', 'Overview');
  const card = page.getByTestId('fleet-status').getByTestId('fleet-punch-card');
  await expect(card).toBeVisible();
  const grid = card.getByRole('grid');
  await expect(grid.getByRole('gridcell')).toHaveCount(7 * 24);
  const busy = grid.locator('[role="gridcell"]:not([data-level="0"])').last();
  await expect(busy).toHaveAttribute('aria-label', /jobs? finished/);
  const label = (await busy.getAttribute('aria-label')) ?? '';
  const [when = '', ...rest] = label.split('. ');
  await busy.hover();
  // The bubble is the square's own sentence in two parts: the hour as its
  // heading, the figures under it, so a reader scrubbing across the week
  // need not open History to read one square.
  const bubble = page.locator('.bubble:popover-open');
  await expect(bubble).toContainText(when);
  await expect(bubble).toContainText(rest.join('. ').replace(/\.$/, ''));
  // The bubble sits over the row above the square it describes. The shared
  // tooltip hit-tests through its own bubble, so the covered square opens its
  // own bubble when the reader moves up a row and takes the click that opens
  // History; a bubble that caught the pointer would make the top of the week
  // unreachable from the bottom.
  const busyRowIndex = await busy.evaluate((el) =>
    Array.from(el.closest('[role="grid"]')!.querySelectorAll('[role="row"]')).indexOf(
      el.closest('[role="row"]')!,
    ),
  );
  if (busyRowIndex > 0) {
    // The square straight above: the one in the previous row at the same
    // left edge, which the bubble over the busy square covers.
    const busyBox = (await busy.boundingBox())!;
    const candidates = grid
      .getByRole('row')
      .nth(busyRowIndex - 1)
      .locator('[role="gridcell"]');
    let above = candidates.first();
    for (const cell of await candidates.all()) {
      const box = await cell.boundingBox();
      if (box && Math.abs(box.x - busyBox.x) < 2) above = cell;
    }
    const aboveLabel = (await above.getAttribute('aria-label')) ?? '';
    await above.hover();
    await expect(bubble).toContainText(aboveLabel.split('. ')[0] ?? '');
    await above.click({ timeout: 5_000 });
    await expect(page).toHaveURL(/#history$/);
  }
  // An hour still to come says so and nothing else.
  const hour = await page.evaluate(() => new Date().getHours());
  if (hour < 23) {
    await grid.locator('[role="gridcell"][data-future]').first().hover();
    await expect(bubble).toContainText('Not yet');
    await expect(bubble).not.toContainText('succeeded');
  }
});

test('the Overview band shows the week by the hour, and an hour of it opens History on that hour', async ({
  page,
}) => {
  await goto(page, '/', 'Overview');
  const band = page.getByTestId('fleet-status');
  const card = band.getByTestId('fleet-punch-card');
  await expect(card).toBeVisible();
  await expect(card).toContainText('The last 7 days, by the hour');
  const grid = card.getByRole('grid');
  await expect(grid.getByRole('gridcell')).toHaveCount(7 * 24);
  // Hours that have not come yet are drawn, so a row is a day, but say so. In
  // the day's last hour there are none to say it, so the check follows the
  // browser's clock rather than failing every night between eleven and twelve.
  const hour = await page.evaluate(() => new Date().getHours());
  const future = grid.locator('[role="gridcell"][data-future]');
  if (hour < 23) {
    await expect(future.first()).toHaveAttribute('aria-label', /Not yet\.$/);
    await expect(future).toHaveCount(23 - hour);
  } else {
    await expect(future).toHaveCount(0);
  }
  // The seed's jobs ran in the hours before the controller started, so a square
  // with completions is in the last day and says what it holds.
  const busy = grid.locator('[role="gridcell"]:not([data-level="0"])').last();
  await expect(busy).toHaveAttribute('aria-label', /jobs? finished/);
  // Only one square is a tab stop, and the arrows walk from it.
  await expect(grid.locator('[role="gridcell"][tabindex="0"]')).toHaveCount(1);

  // An hour's click opens History on the 7d range with that hour selected.
  const label = await busy.getAttribute('aria-label');
  await busy.click();
  await expect(page).toHaveURL(/#history$/);
  const history = page.locator('details#history');
  await expect(history).toHaveJSProperty('open', true);
  const matrix = page.getByRole('region', { name: 'Activity matrix', exact: true });
  await expect(
    matrix
      .getByRole('group', { name: 'Range' })
      .getByRole('button', { name: 'The last 7 days, by the hour' }),
  ).toHaveAttribute('aria-pressed', 'true');
  await expect(
    matrix.getByRole('grid').locator('[role="gridcell"][aria-selected="true"]'),
  ).toHaveAttribute('aria-label', label!);

  // Every cut carries it, the default Slim included: the figures give even the
  // shortest cut the height for seven rows.
  await expect(band).toHaveAttribute('data-size', 'slim');
  await band
    .getByRole('group', { name: 'Overview band size' })
    .getByRole('button', { name: 'Expanded', exact: true })
    .click();
  await page
    .getByRole('dialog', { name: 'Expanded band size, where to apply' })
    .getByRole('button', { name: 'Apply to Overview' })
    .click();
  await expect(band).toHaveAttribute('data-size', 'expanded');
  await expect(card).toBeVisible();
  await expect(grid.getByRole('gridcell')).toHaveCount(7 * 24);
});

/**
 * The Hosts and Runners bands carry the fleet's slots as a map beside their
 * sentence. What this protects: the map agrees with the sentence (the same
 * hosts, the same slots), each host is a way in, and the Runners page colours
 * the same squares by state rather than drawing a second picture.
 */
test('the Hosts and Runners bands draw the same slots, by host, and each row opens its host', async ({
  page,
}) => {
  await goto(page, '/hosts', 'Hosts');
  const hostsMap = page.getByTestId('band-hosts').getByTestId('band-slot-map');
  await expect(hostsMap).toBeVisible();
  await expect(hostsMap).toHaveAttribute('data-detail', 'occupancy');
  await expect(hostsMap).toContainText('Slots right now');
  // The fixture's three hosts, and the band's own count of slots in use.
  const rows = hostsMap.getByRole('listitem');
  await expect(rows).toHaveCount(3);
  await expect(hostsMap).toContainText(/\d+ of 12 in use/);
  await expect(page.getByTestId('band-hosts')).toContainText('of 12 slots in use');
  // A row names its host and says what it holds, in one accessible name.
  const first = hostsMap.getByRole('link').first();
  await expect(first).toHaveAccessibleName(/^demo-.*: \d+ of \d+ in use/);

  await goto(page, '/runners', 'Runners');
  const runnersMap = page.getByTestId('band-runners').getByTestId('band-slot-map');
  await expect(runnersMap).toBeVisible();
  await expect(runnersMap).toHaveAttribute('data-detail', 'state');
  await expect(runnersMap).toContainText('Runners by host');
  await expect(runnersMap.getByRole('listitem')).toHaveCount(3);
  await expect(runnersMap).toContainText(/\d+ busy · \d+ idle/);

  // A row is a way into its host.
  await runnersMap.getByRole('link', { name: new RegExp(`^${FIXTURE.busyRunnerHost}:`) }).click();
  await expect(page).toHaveURL(/\/hosts\/[^/]+$/);
});

/**
 * A band's picture is a choice, as the dog is: off for one page under Page
 * bands, or off for every page at once, and the band obeys without a reload.
 */
test('a band picture can be switched off in Appearance, per page or for every page', async ({
  page,
}) => {
  await goto(page, '/settings/appearance', 'Appearance');
  // Only the pages that draw one offer the switch.
  await expect(page.getByRole('switch', { name: 'Overview band picture' })).toBeChecked();
  await expect(page.getByRole('switch', { name: 'Hosts band picture' })).toBeChecked();
  await expect(page.getByRole('switch', { name: 'Migrate band picture' })).toHaveCount(0);

  await page.getByRole('switch', { name: 'Overview band picture' }).click();
  await goto(page, '/', 'Overview');
  await expect(page.getByTestId('fleet-status')).toBeVisible();
  await expect(page.getByTestId('fleet-punch-card')).toHaveCount(0);
  // Only that page: Hosts keeps its map.
  await goto(page, '/hosts', 'Hosts');
  await expect(page.getByTestId('band-slot-map')).toBeVisible();

  // Every page at once, which also puts the Overview back in step.
  await goto(page, '/settings/appearance', 'Appearance');
  await expect(page.getByText('One page has its own picture setting')).toBeVisible();
  await page.getByRole('switch', { name: 'Show the picture in the band' }).click();
  await expect(page.getByText('One page has its own picture setting')).toHaveCount(0);
  await goto(page, '/hosts', 'Hosts');
  await expect(page.getByTestId('band-hosts')).toBeVisible();
  await expect(page.getByTestId('band-slot-map')).toHaveCount(0);

  await goto(page, '/settings/appearance', 'Appearance');
  await page.getByRole('switch', { name: 'Show the picture in the band' }).click();
  await goto(page, '/', 'Overview');
  await expect(page.getByTestId('fleet-punch-card')).toBeVisible();
});

/**
 * The Pools, Queue and Usage bands carry a picture each: the pools against
 * their ceilings, the last hour, and the range's hours. What this protects:
 * each agrees with its band's own sentence, each is data (a row names its pool
 * and what it holds, a sparkline says its figure), and Appearance offers the
 * switch for each.
 */
test('the Pools, Queue and Usage bands draw their pictures, and each agrees with its sentence', async ({
  page,
}) => {
  await goto(page, '/pools', 'Pools');
  const bars = page.getByTestId('band-pools').getByTestId('band-pool-bars');
  await expect(bars).toBeVisible();
  await expect(bars.getByRole('listitem')).toHaveCount(2);
  // The x64 pool is the fuller of the two, so it comes first whatever the
  // moment; whether it is at its ceiling depends on how long the fixture has
  // been running (idle runners drain), so the picture is held to the sentence
  // rather than to a figure.
  await expect(bars.getByRole('link').first()).toHaveAccessibleName(
    new RegExp(`^${FIXTURE.linuxPool}: \\d+ busy · \\d+ of \\d+ live`),
  );
  // Both halves are read in one breath: the fixture's x64 pool leaves its
  // ceiling as idle runners drain, and a sentence read before that moment
  // disagrees with bars read after it without either being wrong. The
  // sentence for no pool at its ceiling is "None is at its ceiling.", so the
  // test asks for the count, not for the phrase.
  await expect(async () => {
    const [sentence, picture] = await Promise.all([
      page.getByTestId('band-pools').locator('.lede').textContent(),
      bars.textContent(),
    ]);
    const counted = /^(\d+) (is|are) at its ceiling/.exec(sentence ?? '');
    if (counted) {
      const n = Number(counted[1]);
      expect(picture).toContain(`${n} at ${n === 1 ? 'its' : 'their'} ceiling`);
      await expect(bars.getByRole('link').first()).toHaveAccessibleName(/at its ceiling$/, {
        timeout: 500,
      });
    } else {
      expect(picture).not.toContain('at its ceiling');
    }
  }).toPass({ timeout: 10_000 });

  await goto(page, '/queue', 'Queue');
  const trend = page.getByTestId('band-queue').getByTestId('band-queue-trend');
  await expect(trend).toBeVisible();
  await expect(
    trend.getByRole('img', { name: /^Waiting over the last hour, now \d+$/ }),
  ).toBeVisible();
  await expect(
    trend.getByRole('img', { name: /^Running over the last hour, now \d+$/ }),
  ).toBeVisible();
  // The newest point is the band's own figure.
  const waiting = await page.getByTestId('band-queue').locator('.headline').textContent();
  const now = waiting?.match(/^(\d+) jobs? (?:is|are) waiting/)?.[1];
  expect(now).toBeTruthy();
  await expect(
    trend.getByRole('img', { name: /^Waiting over the last hour/ }),
  ).toHaveAccessibleName(new RegExp(`now ${now}$`));

  await goto(page, '/usage', 'Usage');
  const columns = page.getByTestId('band-usage').getByTestId('band-usage-columns');
  await expect(columns).toBeVisible();
  await expect(columns.getByRole('img')).toHaveAccessibleName(
    /^\d+ intervals, the busiest [\d.]+ h at/,
  );

  await goto(page, '/settings/appearance', 'Appearance');
  for (const name of ['Pools', 'Queue', 'Usage']) {
    await expect(page.getByRole('switch', { name: `${name} band picture` })).toBeChecked();
  }
});

/**
 * The last five bands: Jobs, Workflows, Providers, Installations and Kennel
 * Club. What this protects: each picture agrees with its band's sentence, the
 * Jobs hour row narrows the list when pressed, and Appearance offers every
 * switch.
 */
test('the Jobs, Workflows, Providers, Installations and Kennel Club bands draw their pictures', async ({
  page,
}) => {
  // The seed finishes jobs before the controller starts. After midnight those
  // completions can all belong to yesterday, so use a real fixture job's day
  // for this positive click contract rather than assuming today is busy.
  const response = await page.request.get('/api/v1/jobs?state=completed&managed=true&limit=1');
  expect(response.ok(), 'the completed fixture jobs can be read').toBeTruthy();
  const { items } = (await response.json()) as { items: Job[] };
  expect(items[0]?.completed_at, 'the fixture has a completed job').toBeTruthy();
  await page.clock.setFixedTime(new Date(items[0]!.completed_at!));
  await goto(page, '/jobs', 'Jobs');
  const today = page.getByTestId('band-jobs').getByTestId('fleet-punch-card');
  await expect(today).toBeVisible();
  await expect(today).toContainText('Today, by the hour');
  await expect(today.getByRole('gridcell')).toHaveCount(24);
  // Pressing a past hour narrows the list to the jobs that finished in it, across every status.
  const busy = today.locator('[role="gridcell"]:not([data-level="0"]):not([data-future])').last();
  await expect(busy).toBeVisible();
  await busy.click();
  await expect(page).toHaveURL(/completed_since=\d{4}-\d{2}-\d{2}T\d{2}%3A00/);
  await expect(page).toHaveURL(/completed_until=\d{4}-\d{2}-\d{2}T\d{2}%3A59/);
  // The queued window is left out, and the shorter list says why.
  await expect(page).not.toHaveURL(/[?&](since|until)=/);
  await expect(page.getByText('Finished from')).toBeVisible();

  await goto(page, '/workflows', 'Workflows');
  const flight = page.getByTestId('band-workflows').getByTestId('band-in-flight');
  await expect(flight).toBeVisible();
  await expect(flight).toContainText(/\d+ running · \d+ waiting/);
  await expect(flight.getByRole('link').first()).toHaveAccessibleName(/^acme\/\w+: /);

  await goto(page, '/providers', 'Providers');
  const machines = page.getByTestId('band-providers').getByTestId('band-machines');
  await expect(machines).toBeVisible();
  await expect(machines.getByRole('listitem')).toHaveCount(1);
  // The fixture's provider owns two of four; the sentence says the same.
  await expect(machines).toContainText('2 of 4');
  await expect(page.getByTestId('band-providers')).toContainText('2 machines are owned');

  await goto(page, '/installations', 'Installations');
  const quota = page.getByTestId('band-installations').getByTestId('band-quota');
  await expect(quota).toBeVisible();
  await expect(quota.getByRole('listitem')).toHaveCount(1);

  await goto(page, '/kennel', 'Kennel Club');
  // Kennel Club is off in the fixture, so the band keeps its sentence alone.
  await expect(page.getByTestId('band-kennel')).toBeVisible();
  await expect(page.getByTestId('band-kennel').getByTestId('band-repositories')).toHaveCount(0);

  await goto(page, '/settings/appearance', 'Appearance');
  for (const name of ['Jobs', 'Workflows', 'Providers', 'Installations', 'Kennel Club']) {
    await expect(page.getByRole('switch', { name: `${name} band picture` })).toBeChecked();
  }
});
