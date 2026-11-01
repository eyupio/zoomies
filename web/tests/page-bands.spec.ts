import { expect, test } from '@playwright/test';
import { goto } from './support/fixtures';

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

test('a band is not drawn on a phone, switch or no switch', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 800 });
  await goto(page, '/runners', 'Runners');
  await expect(page.getByTestId('band-runners')).toBeHidden();
});

test('every page opens on a slim band', async ({ page }) => {
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

test('the dog can be resized or removed, and the choice scoped like a cut', async ({ page }) => {
  // The dog is off by default, so this test starts from a browser that asked for it.
  // Seed once: the script runs on every navigation, and a later load must keep the
  // per-page sizes this test sets rather than restore the seed over them.
  await page.addInitScript(() => {
    if (sessionStorage.getItem('band-logo-seeded')) return;
    localStorage.setItem('zoomies.prefs', JSON.stringify({ bandLogo: true }));
    sessionStorage.setItem('band-logo-seeded', '1');
  });
  await goto(page, '/runners', 'Runners');
  await page.getByRole('button', { name: 'Runners band logo settings' }).click();

  const panel = page.getByRole('dialog', { name: 'Band logo, where to apply' });
  await expect(panel).toBeVisible();
  await panel
    .getByRole('group', { name: 'Dog size' })
    .getByRole('button', { name: 'Large' })
    .click();
  await panel.getByRole('radio', { name: 'Choose pages' }).check();
  await panel.getByRole('checkbox', { name: 'Pools', exact: true }).check();
  await panel.getByRole('button', { name: 'Apply to 2 pages' }).click();

  await expect(page.getByTestId('band-runners').locator('.stage')).toHaveAttribute(
    'data-art-size',
    'large',
  );
  await goto(page, '/pools', 'Pools');
  await expect(page.getByTestId('band-pools').locator('.stage')).toHaveAttribute(
    'data-art-size',
    'large',
  );
  // A page that was not picked still follows the default.
  await goto(page, '/hosts', 'Hosts');
  await expect(page.getByTestId('band-hosts').locator('.stage')).not.toHaveAttribute(
    'data-art-size',
    'large',
  );

  // Removing it from this page leaves the other page's dog alone.
  await goto(page, '/runners', 'Runners');
  const wordsBefore = await page.getByTestId('band-runners').locator('.words').boundingBox();
  await page.getByRole('button', { name: 'Runners band logo settings' }).click();
  const again = page.getByRole('dialog', { name: 'Band logo, where to apply' });
  await again.getByRole('switch', { name: 'Show the dog' }).click();
  await again.getByRole('button', { name: 'Apply to Runners' }).click();
  await expect(page.getByTestId('band-runners').locator('.art')).toHaveCount(0);
  // The sentence stays where it was rather than sliding into the dog's place.
  const wordsAfter = await page.getByTestId('band-runners').locator('.words').boundingBox();
  expect(Math.abs((wordsAfter?.x ?? 0) - (wordsBefore?.x ?? 0))).toBeLessThan(2);

  // Appearance reflects both pages, and offers each a way back to the default.
  await goto(page, '/settings/appearance', 'Appearance');
  await expect(page.getByRole('switch', { name: 'Runners band logo' })).not.toBeChecked();
  await expect(page.getByRole('switch', { name: 'Pools band logo' })).toBeChecked();
  await expect(page.getByRole('combobox', { name: 'Pools logo size' })).toHaveValue('large');
  await expect(page.getByRole('button', { name: 'Use the default' })).toHaveCount(2);

  // The reset puts one page back on the default without touching the other.
  await page
    .getByRole('group', { name: 'Page bands' })
    .getByRole('button', { name: 'Use the default' })
    .first()
    .click();
  await expect(page.getByRole('switch', { name: 'Runners band logo' })).toBeChecked();
  await expect(page.getByRole('switch', { name: 'Pools band logo' })).toBeChecked();
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
  for (const item of PAGES) {
    await goto(page, item.path, item.heading);
    const header = page.locator('.page-header');
    await expect(header.getByRole('heading', { name: item.heading, level: 1 })).toBeVisible();
    await expect(header.locator('.rule')).toBeVisible();
    await expect(header.locator('p.subtitle')).toBeVisible();
  }
});

test('a phone, which never draws a band, still gets its figures', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 800 });
  await goto(page, '/queue', 'Queue');
  await expect(page.getByTestId('band-queue')).toBeHidden();
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
  // Hours that have not come yet are drawn, so a row is a day, but say so.
  await expect(grid.locator('[role="gridcell"][data-future]').first()).toHaveAttribute(
    'aria-label',
    /Not yet\.$/,
  );
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
