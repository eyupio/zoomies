/**
 * The host card, closed and open.
 *
 * A fleet's worth of cards, each carrying a dozen blocks, ran a screen and a
 * half apiece. A card now opens closed, with what the Hosts page exists to
 * answer in view (does this host have room, how loaded is the machine, and why
 * not if it does not) and the rest behind a Details button. These specs hold
 * the two halves of that promise: the fold must hide detail, and it must never
 * hide a reason.
 */
import { expect, test, type Locator, type Page } from '@playwright/test';
import { clearStoredPreferences, expectNoSidewaysScroll, goto, reload } from './support/fixtures';

const card = (page: Page, name: string): Locator =>
  page.getByRole('article', { name, exact: true });
const toggle = (page: Page, name: string): Locator =>
  card(page, name).getByRole('button', { name: `Details for ${name}` });

/** The blocks a closed card folds away, by the names their regions carry. */
const FOLDED = ['Resources committed on', 'Placement priority of', 'Backends on', 'Tags on'];
const folded = (page: Page, name: string): Locator[] =>
  FOLDED.map((prefix) => card(page, name).getByRole('region', { name: `${prefix} ${name}` }));

/** The fleet's own hosts: one embedded, one nearly out of disk, one cordoned. */
const BUILDER = 'demo-builder-1';
const LOW_DISK = 'demo-builder-2';
const CORDONED = 'demo-arm-1';

/**
 * Show the page a fleet it did not seed: the host list rewritten for one host,
 * and no stream, or a heartbeat's host frame would put the real row back. The
 * demo fleet is shared by every spec, so it is the response that changes and
 * not the fleet.
 */
async function reshape(
  page: Page,
  name: string,
  change: (host: Record<string, unknown>) => Record<string, unknown>,
): Promise<void> {
  await page.route(/\/api\/v1\/hosts(?:\?.*)?$/, async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    body.items = body.items.map((host: Record<string, unknown>) =>
      host.name === name ? change(host) : host,
    );
    await route.fulfill({ json: body });
  });
  await page.route('**/api/v1/events*', (route) =>
    route.fulfill({
      status: 200,
      headers: { 'content-type': 'text/event-stream', 'cache-control': 'no-store' },
      body: '',
    }),
  );
}

test('a host card opens closed, with its slots and utilisation in view and the rest folded away', async ({
  page,
}) => {
  await goto(page, '/hosts', 'Hosts');
  for (const name of [BUILDER, LOW_DISK, CORDONED]) {
    await expect(toggle(page, name)).toHaveAttribute('aria-expanded', 'false');
    for (const region of folded(page, name)) await expect(region).toHaveCount(0);
  }

  // The question the page exists to answer is on the card without opening it.
  const builder = card(page, BUILDER);
  await expect(builder).toContainText(/\d+ of \d+ slots in use/);
  const vitals = builder.getByRole('group', { name: `Utilisation of ${BUILDER}` });
  await expect(vitals.getByRole('img', { name: /^CPU: \d+% in use/ })).toBeVisible();
  await expect(vitals.getByRole('img', { name: /^Memory: \d+% in use/ })).toBeVisible();
  await expect(vitals.getByRole('img', { name: /^Disk: \d+% full/ })).toBeVisible();
  // The numbers are on the card as text too, not only in a label.
  await expect(vitals).toContainText(/CPU\s*\d+%/);
  await expect(vitals).toContainText(/Memory\s*\d+%/);
  await expect(vitals).toContainText(/Disk\s*\d+%/);
});

test('Details opens the rest of the card and closes it again, from the keyboard too', async ({
  page,
}) => {
  await goto(page, '/hosts', 'Hosts');
  const button = toggle(page, BUILDER);
  await button.click();
  await expect(button).toHaveAttribute('aria-expanded', 'true');
  for (const region of folded(page, BUILDER)) await expect(region).toBeVisible();
  // The control the reader just used does not move out from under them.
  await expect(button).toBeFocused();

  await page.keyboard.press('Space');
  await expect(button).toHaveAttribute('aria-expanded', 'false');
  for (const region of folded(page, BUILDER)) await expect(region).toHaveCount(0);
  await expect(button).toBeFocused();

  await page.keyboard.press('Enter');
  await expect(button).toHaveAttribute('aria-expanded', 'true');
  for (const region of folded(page, BUILDER)) await expect(region).toBeVisible();

  // Only the card that was asked opens.
  await expect(toggle(page, LOW_DISK)).toHaveAttribute('aria-expanded', 'false');
});

test('a card left open is still open after a reload, and a browser with nothing stored finds it closed', async ({
  page,
}) => {
  await goto(page, '/hosts', 'Hosts');
  await toggle(page, BUILDER).click();
  await expect(toggle(page, BUILDER)).toHaveAttribute('aria-expanded', 'true');

  await reload(page, 'Hosts');
  await expect(toggle(page, BUILDER)).toHaveAttribute('aria-expanded', 'true');
  await expect(toggle(page, LOW_DISK)).toHaveAttribute('aria-expanded', 'false');

  // Closing is a choice too, and it is remembered the same way.
  await toggle(page, BUILDER).click();
  await reload(page, 'Hosts');
  await expect(toggle(page, BUILDER)).toHaveAttribute('aria-expanded', 'false');

  await toggle(page, BUILDER).click();
  await clearStoredPreferences(page);
  await reload(page, 'Hosts');
  await expect(toggle(page, BUILDER)).toHaveAttribute('aria-expanded', 'false');
});

test('what keeps a host from taking work is said on the closed card, not behind Details', async ({
  page,
}) => {
  await goto(page, '/hosts', 'Hosts');
  // Cordoned in the fixture fleet: the sentence says what happens to its work.
  await expect(toggle(page, CORDONED)).toHaveAttribute('aria-expanded', 'false');
  await expect(card(page, CORDONED)).toContainText('Cordoned.');
  // Nearly out of disk: the figure says so in words as well as in amber, so a
  // reader who cannot tell the colours apart is told what the colour means.
  await expect(toggle(page, LOW_DISK)).toHaveAttribute('aria-expanded', 'false');
  await expect(
    card(page, LOW_DISK).getByRole('img', { name: /^Disk: \d+% full.*Low: the Work directory/ }),
  ).toBeVisible();
});

test('a host with no backend that works says why on the closed card, and one with Docker alone does not', async ({
  page,
}) => {
  await reshape(page, BUILDER, (host) => ({
    ...host,
    backends: ['docker'],
    backend_info: [
      { kind: 'docker', available: false, detail: 'permission denied on /var/run/docker.sock' },
    ],
  }));
  await goto(page, '/hosts', 'Hosts');
  await expect(toggle(page, BUILDER)).toHaveAttribute('aria-expanded', 'false');
  await expect(card(page, BUILDER)).toContainText('permission denied on /var/run/docker.sock');

  // The fixture hosts all run Docker and report Podman as missing, with the
  // commands to install it. That is how most hosts are, and putting the same
  // paragraph on every closed card would undo the point of closing them.
  await expect(toggle(page, LOW_DISK)).toHaveAttribute('aria-expanded', 'false');
  await expect(card(page, LOW_DISK)).not.toContainText('no socket at');
});

test('a figure past its pressure line says so in words, and the others stay quiet', async ({
  page,
}) => {
  await reshape(page, BUILDER, (host) => ({
    ...host,
    usage_fresh: true,
    usage: { cpu_percent: 91, memory_available_mb: 512, sampled_at: new Date().toISOString() },
  }));
  await goto(page, '/hosts', 'Hosts');
  const vitals = card(page, BUILDER).getByRole('group', { name: `Utilisation of ${BUILDER}` });
  await expect(
    vitals.getByRole('img', { name: /^CPU: 91% in use.*From 85% the scheduler/ }),
  ).toBeVisible();
  await expect(
    vitals.getByRole('img', { name: /^Memory: \d+% in use.*From 85% the scheduler/ }),
  ).toBeVisible();
  // Disk is nowhere near its line on this host, so it says nothing of one.
  await expect(vitals.getByRole('img', { name: /^Disk: \d+% full/ })).toBeVisible();
  await expect(vitals.getByRole('img', { name: /Low:/ })).toHaveCount(0);
});

test('a host that has gone quiet shows no figure as current', async ({ page }) => {
  await reshape(page, BUILDER, (host) => ({
    ...host,
    healthy: false,
    usage_fresh: false,
    last_heartbeat: new Date(Date.now() - 20 * 60_000).toISOString(),
  }));
  await goto(page, '/hosts', 'Hosts');
  await expect(card(page, BUILDER)).toContainText('No heartbeat since');
  // One sentence in place of three rows that would all say the same thing. Disk
  // is among them: the agent's last word on it would read as now beside a
  // heartbeat that stopped twenty minutes ago.
  const vitals = card(page, BUILDER).getByRole('group', { name: `Utilisation of ${BUILDER}` });
  await expect(vitals).toContainText('No heartbeat, so no figure is shown as current.');
  await expect(vitals.getByRole('img')).toHaveCount(0);
});

test('the Details button is a finger-sized target on a touch screen, and neither state overflows the page', async ({
  page,
  isMobile,
}) => {
  await goto(page, '/hosts', 'Hosts');
  const button = toggle(page, BUILDER);
  if (isMobile) {
    const box = await button.boundingBox();
    expect(box!.height, 'the toggle is tall enough to hit').toBeGreaterThanOrEqual(44);
  }
  await expectNoSidewaysScroll(page, 'a closed card');
  await button.click();
  await expect(button).toHaveAttribute('aria-expanded', 'true');
  await expectNoSidewaysScroll(page, 'an open card');
});
