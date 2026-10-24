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
  { path: '/repairs', heading: 'Eli repairs', label: 'Eli repairs', band: 'band-repairs' },
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

test('the Overview opens on a standard band and every other page on a compact one', async ({
  page,
}) => {
  for (const item of PAGES) {
    await goto(page, item.path, item.heading);
    await expect(page.getByTestId(item.band)).toHaveAttribute(
      'data-size',
      item.label === 'Overview' ? 'standard' : 'compact',
    );
  }
});

test('a page can be moved between compact and standard in Appearance, and stays there', async ({
  page,
}) => {
  await goto(page, '/settings/appearance', 'Appearance');
  const size = page.getByRole('group', { name: 'Runners band size' });
  await expect(size.getByRole('button', { name: 'Compact' })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await size.getByRole('button', { name: 'Standard' }).click();

  await goto(page, '/runners', 'Runners');
  await expect(page.getByTestId('band-runners')).toHaveAttribute('data-size', 'standard');
  // Only that page moved.
  await goto(page, '/pools', 'Pools');
  await expect(page.getByTestId('band-pools')).toHaveAttribute('data-size', 'compact');

  await page.reload();
  await goto(page, '/runners', 'Runners');
  await expect(page.getByTestId('band-runners')).toHaveAttribute('data-size', 'standard');
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
  // The filter is in the address, and so is the section that shows it.
  await expect(page).toHaveURL(/\/jobs\?state=queued#jobs-list$/);
  await expect(page.getByRole('heading', { name: 'Jobs', level: 1 })).toBeVisible();
  await expect(page.locator('#jobs-list')).toBeFocused();

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
  await expect(page).toHaveURL(/\/jobs\?state=queued#jobs-list$/);
  await expect(page.locator('#jobs-list')).toBeFocused();
});
