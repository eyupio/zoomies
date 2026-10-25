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
import {
  clearStoredPreferences,
  expectNoSidewaysScroll,
  goto,
  readThrough,
  reload,
} from './support/fixtures';

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
const FLEET = [CORDONED, BUILDER, LOW_DISK];

/**
 * Expand all, above the cards: one press for what a fleet of closed cards
 * otherwise takes a press each to read. It acts on the cards on show, and its
 * name says which way it will go.
 */
const expandAll = (page: Page): Locator =>
  page.getByRole('button', { name: 'Expand all', exact: true });
const collapseAll = (page: Page): Locator =>
  page.getByRole('button', { name: 'Collapse all', exact: true });
const tags = (page: Page, name: string): Locator =>
  card(page, name).getByRole('region', { name: `Tags on ${name}` });

/**
 * Show the page a fleet it did not seed: the host list rewritten, and no
 * stream, or a heartbeat's host frame would put the real row back. The demo
 * fleet is shared by every spec, so it is the response that changes and not the
 * fleet.
 */
async function rewriteHosts(
  page: Page,
  change: (hosts: Record<string, unknown>[]) => Record<string, unknown>[],
): Promise<void> {
  await page.route(/\/api\/v1\/hosts(?:\?.*)?$/, async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    const { body } = read;
    body.items = change(body.items);
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

/** The same, for one host of the fleet. */
async function reshape(
  page: Page,
  name: string,
  change: (host: Record<string, unknown>) => Record<string, unknown>,
): Promise<void> {
  await rewriteHosts(page, (hosts) =>
    hosts.map((host) => (host.name === name ? change(host) : host)),
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

/**
 * Let the page finish asking before a spec that rewrites its answers ends.
 *
 * The page reads the hosts a second time when its event stream connects, about a
 * tenth of a second after the first. A spec that ends on the first answer leaves
 * that request to be disposed of with the page, and the route handler still
 * reading it fails with "Response has been disposed". Shorter specs are the ones
 * that hit it, so those that rewrite the list settle before they look.
 */
const settle = (page: Page): Promise<void> => page.waitForLoadState('networkidle');

test('a host with free slots and its memory all promised away says so on the closed card', async ({
  page,
}) => {
  // "2 of 8 slots in use · 6 free" beside a memory bar of what the machine is
  // using reads as a host with room. What explains the runner that does not
  // start is what idle runners have reserved, and that was behind Details.
  await reshape(page, BUILDER, (host) => ({
    ...host,
    capacity: 8,
    effective_capacity: 8,
    slots: 8,
    active_runners: 2,
    free: 6,
    resources_known: true,
    reserved_known: true,
    reserved_cpus: 4,
    allocatable_memory_mb: 30720,
    reserved_memory_mb: 30720,
  }));
  await goto(page, '/hosts', 'Hosts');
  await settle(page);
  await expect(toggle(page, BUILDER)).toHaveAttribute('aria-expanded', 'false');
  await expect(card(page, BUILDER)).toContainText(/2 of 8 slots in use\s*· 6 free/);
  const note = card(page, BUILDER).getByTestId('host-committed-note');
  await expect(note).toBeVisible();
  await expect(note).toContainText('memory 30 GB of 30 GB');
  await expect(note).toContainText('may not take another runner');
  // CPU is at 4 of 14.2, so it is not named; the bars themselves are detail.
  await expect(note).not.toContainText('CPU');
  await expect(
    card(page, BUILDER).getByRole('region', { name: `Resources committed on ${BUILDER}` }),
  ).toHaveCount(0);
});

test('the committed notice stays quiet where it would explain nothing', async ({ page }) => {
  // A full host has more promised than it may place, as any full host does, and
  // its slot line already says why it takes nothing. A host with room in both has
  // nothing to explain. Both are written out here: the demo fleet's own figures
  // move as its simulated jobs come and go, and this spec read them as they were
  // at start-up until a controller had been up for about twenty-five minutes.
  await rewriteHosts(page, (hosts) =>
    hosts.map((host) => {
      if (host.name === LOW_DISK) {
        return {
          ...host,
          capacity: 4,
          effective_capacity: 4,
          slots: 4,
          active_runners: 4,
          free: 0,
          resources_known: true,
          reserved_known: true,
          reserved_cpus: 4,
          allocatable_memory_mb: 8192,
          reserved_memory_mb: 8192,
        };
      }
      if (host.name === BUILDER) {
        return {
          ...host,
          capacity: 8,
          effective_capacity: 8,
          slots: 8,
          active_runners: 1,
          free: 7,
          resources_known: true,
          reserved_known: true,
          reserved_cpus: 1,
          allocatable_memory_mb: 30720,
          reserved_memory_mb: 2048,
        };
      }
      return host;
    }),
  );
  await goto(page, '/hosts', 'Hosts');
  await settle(page);
  await expect(card(page, LOW_DISK)).toContainText(/4 of 4 slots in use/);
  await expect(card(page, LOW_DISK).getByTestId('host-committed-note')).toHaveCount(0);
  await expect(card(page, BUILDER).getByTestId('host-committed-note')).toHaveCount(0);
});

test('why a slot count is capped is said on the closed card, not behind Details', async ({
  page,
}) => {
  await reshape(page, BUILDER, (host) => ({
    ...host,
    capacity: 2,
    effective_capacity: 2,
    slots: 2,
    active_runners: 1,
    free: 1,
    slots_limited_by: 'capacity',
    runner_profile: { standard: { cpus: 2, memory_mb: 4096 } },
  }));
  await goto(page, '/hosts', 'Hosts');
  await settle(page);
  await expect(toggle(page, BUILDER)).toHaveAttribute('aria-expanded', 'false');
  await expect(card(page, BUILDER).getByTestId('host-slots-note')).toContainText(
    'capped by its capacity of 2, below what its machine holds',
  );
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
  // Memory has no pressure line: the scheduler holds new starts at the host's
  // reserve, so the sentence names the reserve and not a share of the machine.
  await expect(
    vitals.getByRole('img', {
      name: /^Memory: \d+% in use.*at or below the .* reserve, so the scheduler holds new starts/,
    }),
  ).toBeVisible();
  // Disk is nowhere near its line on this host, so it says nothing of one.
  await expect(vitals.getByRole('img', { name: /^Disk: \d+% full/ })).toBeVisible();
  await expect(vitals.getByRole('img', { name: /Low:/ })).toHaveCount(0);
});

test('a figure the scheduler does not act on stays calm, however high it reads', async ({
  page,
}) => {
  // demo-builder-2 is a 16 GiB host with an 819 MB reserve. At 94% used it has
  // 983 MB left, and the scheduler places as normal until that reaches the
  // reserve. Its CPU reads 92% but 30 points of it are CPU lent to runners,
  // which the scheduler leaves out of the pressure it judges.
  await reshape(page, LOW_DISK, (host) => ({
    ...host,
    usage_fresh: true,
    usage: {
      cpu_percent: 92,
      lent_cpu_percent: 30,
      memory_available_mb: 983,
      sampled_at: new Date().toISOString(),
    },
  }));
  await goto(page, '/hosts', 'Hosts');
  await settle(page);
  const vitals = card(page, LOW_DISK).getByRole('group', { name: `Utilisation of ${LOW_DISK}` });
  const cpu = vitals.getByTestId('host-vital-cpu');
  const memory = vitals.getByTestId('host-vital-memory');
  await expect(cpu).toBeVisible();
  await expect(memory).toBeVisible();
  await expect(cpu).not.toHaveClass(/tight/);
  await expect(memory).not.toHaveClass(/tight/);
  // The reader is told why a high figure is not amber, or it looks like a fault.
  await expect(
    vitals.getByRole('img', { name: /^CPU: 92% in use.*30% of it is CPU lent to runners/ }),
  ).toBeVisible();
  await expect(vitals.getByRole('img', { name: /^Memory: 94% in use/ })).toBeVisible();
  await expect(vitals.getByRole('img', { name: /reserve|one runner at a time/ })).toHaveCount(0);
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

test('a throttled host whose usage has gone stale does not say it cannot be throttled', async ({
  page,
}) => {
  // The throttle outlives its sample and lifts after ten minutes without a fresh
  // one. Nothing is measured here (no usage, no disk), so the card explains why in
  // one sentence, and that sentence must not contradict the one above it.
  await reshape(page, BUILDER, (host) => ({
    ...host,
    usage: undefined,
    usage_fresh: false,
    disk_total_mb: 0,
    disk_free_mb: 0,
    capacity: 8,
    effective_capacity: 6,
    free: 4,
    throttle: { level: 1, since: new Date().toISOString() },
    throttle_reason:
      'throttled to 6 of 8 slots (step 1 of 3) after sustained pressure: the 1-minute load average is 20.0',
  }));
  await goto(page, '/hosts', 'Hosts');
  await settle(page);
  await expect(toggle(page, BUILDER)).toHaveAttribute('aria-expanded', 'false');
  await expect(card(page, BUILDER)).toContainText('throttled to 6 of 8 slots (step 1 of 3)');
  const vitals = card(page, BUILDER).getByRole('group', { name: `Utilisation of ${BUILDER}` });
  await expect(vitals).toContainText('The throttle stands on its last measurements');
  await expect(card(page, BUILDER)).not.toContainText('cannot be throttled');
});

/**
 * Every sentence that explains why a host is not taking work, on a card that has
 * not been opened. The fold may hide detail and never a reason, and a spec that
 * opens the card first proves nothing about it: the sentence would be there
 * either way. The cordon, the low disk, a host with no working backend and one
 * with no heartbeat are held above; these are the rest, and the throttle is
 * held by hosts.spec.ts and diagnostics.spec.ts against a real agent.
 */
const NOW_ISO = () => new Date().toISOString();
const REASONS: {
  name: string;
  change: (host: Record<string, unknown>) => Record<string, unknown>;
  /** A machine for the host to be, for the one reason that is about its machine. */
  machine?: 'draining';
  sentence: string | RegExp;
}[] = [
  {
    name: 'a runtime that is recovering',
    change: (host) => ({
      ...host,
      runtime_recovering: {
        failures: 3,
        kind: 'unavailable',
        error: 'no socket at /var/run/docker.sock',
        retry_at: new Date(Date.now() + 40_000).toISOString(),
        since: NOW_ISO(),
        observed_at: NOW_ISO(),
      },
      runtime_reason:
        'Runtime recovering: third failure in a row, the container runtime could not be reached. New starts here are held until one recovery attempt; running jobs continue.',
    }),
    sentence: /Runtime recovering: third failure in a row.*Retrying in \d+s/s,
  },
  {
    name: 'a hold on new starts',
    change: (host) => ({
      ...host,
      usage_fresh: true,
      usage: { cpu_percent: 20, memory_available_mb: 100, sampled_at: NOW_ISO() },
      admission_reason: 'new starts held because available memory is at or below the host reserve',
    }),
    sentence:
      'new starts held because available memory is at or below the host reserve. Running jobs continue.',
  },
  {
    name: 'a CPU that is busy',
    change: (host) => ({
      ...host,
      usage_fresh: true,
      usage: { cpu_percent: 91, memory_available_mb: 20000, sampled_at: NOW_ISO() },
    }),
    sentence: 'CPU is busy. New runners start one at a time while pressure clears.',
  },
  {
    name: 'an image that will not pull',
    change: (host) => ({
      ...host,
      image_pull_failed: {
        pool_id: 'pool_amd64',
        pool: 'linux-amd64',
        image: 'ghcr.io/acme/runner:1',
        registry: 'ghcr.io',
        source: 'start',
        since: NOW_ISO(),
        observed_at: NOW_ISO(),
      },
    }),
    sentence: "Cannot pull pool linux-amd64's image from ghcr.io",
  },
  {
    name: 'an agent the controller cannot talk to',
    change: (host) => ({
      ...host,
      incompatible: true,
      incompatible_reason:
        'This agent speaks protocol 0 and this controller speaks 1, so no new runner is placed here. Its running work finishes and is drained as normal.',
    }),
    sentence: 'This agent speaks protocol 0 and this controller speaks 1',
  },
  {
    name: 'a machine that is draining',
    change: (host) => host,
    machine: 'draining',
    sentence: 'It is draining because the pools it serves have had no queued work',
  },
];

for (const reason of REASONS) {
  test(`${reason.name} is said on the closed card, not behind Details`, async ({ page }) => {
    await reshape(page, BUILDER, reason.change);
    if (reason.machine) {
      // The host a machine is comes from the machines the page also asks for.
      const hosts = (await page.request.get('/api/v1/hosts').then((r) => r.json())) as {
        items: { id: string; name: string }[];
      };
      const hostId = hosts.items.find((host) => host.name === BUILDER)?.id;
      await page.route(/\/api\/v1\/machines(?:\?.*)?$/, (route) =>
        route.fulfill({
          json: {
            items: [
              {
                id: 'mach_draining1',
                provider_id: 'prov_demo',
                name: 'zoomies-mach-draining',
                state: reason.machine,
                host_id: hostId,
                labels: {},
                safe_to_delete: false,
              },
            ],
          },
        }),
      );
    }
    await goto(page, '/hosts', 'Hosts');
    await settle(page);
    await expect(toggle(page, BUILDER)).toHaveAttribute('aria-expanded', 'false');
    await expect(
      card(page, BUILDER).locator('p.cordoned', { hasText: reason.sentence }),
    ).toBeVisible();
    for (const region of folded(page, BUILDER)) await expect(region).toHaveCount(0);
  });
}

/**
 * What a bar is drawn in: the computed background of its fill and of the track
 * behind it. Windows high contrast replaces every author colour with the
 * system's, so a bar is only seen where it is told to take a system colour of
 * its own, and a fill the same colour as its track is a bar that is not there.
 */
async function drawn(root: Locator, fill: string, track: string) {
  return root.evaluate(
    (element, [fillSelector, trackSelector]) => {
      const colour = (selector: string) => {
        const found = element.querySelector(selector);
        return found ? getComputedStyle(found).backgroundColor : null;
      };
      return { fill: colour(fillSelector), track: colour(trackSelector) };
    },
    [fill, track] as const,
  );
}

test('the bars keep their fill in forced colours, calm or tight', async ({ page }) => {
  await page.emulateMedia({ forcedColors: 'active' });
  // CPU is calm; memory and disk are past their lines, and the committed memory
  // is all promised, so there is a calm and a tight bar of every kind.
  await reshape(page, BUILDER, (host) => ({
    ...host,
    usage_fresh: true,
    usage: { cpu_percent: 40, memory_available_mb: 512, sampled_at: new Date().toISOString() },
    disk_total_mb: 40960,
    disk_free_mb: 2048,
    allocatable_memory_mb: 30720,
    reserved_memory_mb: 30720,
  }));
  await goto(page, '/hosts', 'Hosts');
  await settle(page);
  await toggle(page, BUILDER).click();
  const host = card(page, BUILDER);

  const vitals = host.getByRole('group', { name: `Utilisation of ${BUILDER}` });
  const bars: Record<string, Awaited<ReturnType<typeof drawn>>> = {};
  for (const key of ['cpu', 'memory', 'disk'])
    bars[key] = await drawn(vitals.getByTestId(`host-vital-${key}`), '.fill', '.track');
  for (const [key, bar] of Object.entries(bars))
    expect(bar.fill, `the ${key} fill is not the colour of its track`).not.toBe(bar.track);
  expect(bars.memory!.fill, 'a tight fill is the same system colour as a calm one').toBe(
    bars.cpu!.fill,
  );

  // The committed bars and the slot bar are the same thing in other components.
  const committed = host.getByRole('region', { name: `Resources committed on ${BUILDER}` });
  const rows = await committed.locator('.row').all();
  expect(rows.length, 'a calm and a tight committed bar').toBe(2);
  for (const row of rows) {
    const bar = await drawn(row, '.fill', '.track');
    expect(bar.fill, 'a committed fill is not the colour of its track').not.toBe(bar.track);
  }
  const slots = await drawn(host.locator('.bar-wrap'), '.busy', '.track');
  expect(slots.fill, 'the slots in use are not the colour of their track').not.toBe(slots.track);
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

test('Expand all is a finger-sized target on a touch screen, and a page of open cards does not overflow', async ({
  page,
  isMobile,
}) => {
  await goto(page, '/hosts', 'Hosts');
  if (isMobile) {
    const box = await expandAll(page).boundingBox();
    expect(box!.height, 'the button is tall enough to hit').toBeGreaterThanOrEqual(44);
  }
  await expectNoSidewaysScroll(page, 'the page with every card closed');
  await expandAll(page).click();
  await expect(collapseAll(page)).toBeVisible();
  await expectNoSidewaysScroll(page, 'the page with every card open');
});

test('a card that a filter takes off the page comes back as it was left', async ({ page }) => {
  // Every host without an OS report, so no filter but All has anything to show.
  // The list response is rewritten rather than the fleet changed, because the
  // demo fleet is shared, and there is no stream, or a heartbeat's host frame
  // would put a report back.
  await page.route(/\/api\/v1\/hosts(?:\?.*)?$/, async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    const { body } = read;
    body.items = body.items.map(({ doctor: _doctor, ...host }: { doctor?: unknown }) => host);
    await route.fulfill({ json: body });
  });
  await page.route('**/api/v1/events*', (route) =>
    route.fulfill({
      status: 200,
      headers: { 'content-type': 'text/event-stream', 'cache-control': 'no-store' },
      body: '',
    }),
  );
  await goto(page, '/hosts', 'Hosts');
  await toggle(page, BUILDER).click();
  await expect(toggle(page, BUILDER)).toHaveAttribute('aria-expanded', 'true');

  // The page holds which cards are open, not the cards, so a card that leaves
  // the screen and returns is not closed by the trip.
  await page
    .getByRole('group', { name: 'Show hosts by OS health' })
    .getByRole('button', { name: /^Need attention \d+$/ })
    .click();
  await expect(card(page, BUILDER)).toHaveCount(0);
  await page.getByRole('button', { name: 'Show all hosts' }).click();
  await expect(toggle(page, BUILDER)).toHaveAttribute('aria-expanded', 'true');
  for (const region of folded(page, BUILDER)) await expect(region).toBeVisible();
  await expect(toggle(page, LOW_DISK)).toHaveAttribute('aria-expanded', 'false');
});

/**
 * A row of cards is a row of equals. The three fixture hosts are unalike on
 * purpose: one has a notice a cordon brings, one a machine chip long enough to
 * wrap, one neither. Left to their own content they came out three heights, with
 * each card's slot bar and bars wherever its own header happened to end, so a
 * row could not be read across.
 */
async function measure(page: Page, name: string) {
  const host = card(page, name);
  // Down the page, not down the window: a click scrolls to what it presses.
  const scrolled = await page.evaluate(() => window.scrollY);
  const box = async (locator: Locator) => {
    const found = await locator.boundingBox();
    expect(found, `${name} has it on the page`).not.toBeNull();
    return { ...found!, y: found!.y + scrolled };
  };
  return {
    card: await box(host),
    // The state pills under the name: the first thing in a card that a name
    // wrapping onto a second line can push out of line with its neighbours.
    pills: await box(host.locator('header .badges')),
    slots: await box(host.getByRole('img', { name: /^Runner slots in use on / })),
    bars: await box(host.getByRole('group', { name: /^Utilisation of / })),
    details: await box(toggle(page, name)),
  };
}

type Measured = Awaited<ReturnType<typeof measure>>;

/** Every card has the part at the same height and of the same size, to a pixel. */
function expectUniform(cards: Measured[], when: string): void {
  for (const part of ['card', 'pills', 'slots', 'bars', 'details'] as const) {
    const tops = cards.map((c) => c[part].y);
    const heights = cards.map((c) => c[part].height);
    expect(
      Math.max(...tops) - Math.min(...tops),
      `${when}: the ${part} starts at one height`,
    ).toBeLessThanOrEqual(1);
    expect(
      Math.max(...heights) - Math.min(...heights),
      `${when}: the ${part} is one size`,
    ).toBeLessThanOrEqual(1);
  }
}

test('a name that wraps does not move its pills out of line with the cards beside it', async ({
  page,
  isMobile,
}) => {
  test.skip(isMobile, 'a phone has one card to a row, so there is nothing to line up with');
  await page.setViewportSize({ width: 1600, height: 1000 });
  // A name in the grammar internal/naming gives a rented machine: long enough to
  // take two lines in a card a third of the page wide, where its neighbours' names
  // take one. Every fixture name is one line, so without this the pills could
  // slip by a line and nothing would say so.
  const LONG = 'zoomies-16vcpu-64gb-ubuntu-2404-arm64-tuck';
  await reshape(page, BUILDER, (host) => ({ ...host, name: LONG }));
  await goto(page, '/hosts', 'Hosts');
  await settle(page);
  await expect(card(page, CORDONED)).toContainText('Cordoned.');
  const lines = await card(page, LONG)
    .getByRole('heading', { level: 3 })
    .locator('a')
    .evaluate((link) => link.getClientRects().length);
  expect(lines, 'the long name takes more than one line').toBeGreaterThan(1);

  const row = await Promise.all([CORDONED, LONG, LOW_DISK].map((name) => measure(page, name)));
  expectUniform(row, 'with one name on two lines');
});

test('the cards in a row are one height, and their sections line up across it', async ({
  page,
  isMobile,
}) => {
  test.skip(isMobile, 'a phone has one card to a row, so there is nothing to line up with');
  // Wide enough for the three to share a row whatever the sidebar takes.
  await page.setViewportSize({ width: 1600, height: 1000 });
  await goto(page, '/hosts', 'Hosts');
  const names = [CORDONED, BUILDER, LOW_DISK];

  await expect(card(page, CORDONED)).toContainText('Cordoned.');
  const closed = await Promise.all(names.map((name) => measure(page, name)));
  expectUniform(closed, 'with every card closed');

  // Opening one makes the row as tall as that card needs. The rest keep their
  // own sections where they were, and the open card's buttons do not move.
  await toggle(page, BUILDER).click();
  await expect(toggle(page, BUILDER)).toHaveAttribute('aria-expanded', 'true');
  for (const region of folded(page, BUILDER)) await expect(region).toBeVisible();
  const opened = await Promise.all(names.map((name) => measure(page, name)));
  expectUniform(opened, 'with one card open');
  expect(opened[1]!.details.y, 'the button that was pressed stays where it was').toBeCloseTo(
    closed[1]!.details.y,
    0,
  );
  expect(opened[0]!.card.height, 'the row grew to fit the open card').toBeGreaterThan(
    closed[0]!.card.height + 200,
  );

  // All three open, which is what Expand all does to a page: the row is as tall
  // as the tallest of them, still one height, and the parts above the fold are
  // where they were.
  await expandAll(page).click();
  await expect(collapseAll(page)).toBeVisible();
  for (const name of names) await expect(tags(page, name)).toBeVisible();
  const everyOne = await Promise.all(names.map((name) => measure(page, name)));
  expectUniform(everyOne, 'with every card open');
  expect(everyOne[0]!.slots.y, 'the slot bars did not move').toBeCloseTo(closed[0]!.slots.y, 0);
});

/**
 * A fresh OS report with nothing wrong in it. A host that has one is in none of
 * the health filters, so the filters can be made to show some of the fleet.
 */
const cleanReport = () => ({
  checked_at: new Date().toISOString(),
  os: 'linux',
  distro: 'ubuntu 24.04',
  container: false,
  reboot_pending: false,
  results: [],
  summary: { counted: 0, warnings: 0, errors: 0, skipped: 0, suggestions: 0, accepted: 0 },
});

test('Expand all opens every card, and the button then offers to close them again', async ({
  page,
}) => {
  await goto(page, '/hosts', 'Hosts');
  await expect(expandAll(page)).toBeVisible();
  await expect(collapseAll(page)).toHaveCount(0);

  await expandAll(page).click();
  for (const name of FLEET) {
    await expect(toggle(page, name)).toHaveAttribute('aria-expanded', 'true');
    await expect(tags(page, name)).toBeVisible();
  }
  // The one button, by what it does now: it is not still offering to expand.
  await expect(collapseAll(page)).toBeVisible();
  await expect(expandAll(page)).toHaveCount(0);

  await collapseAll(page).click();
  for (const name of FLEET) {
    await expect(toggle(page, name)).toHaveAttribute('aria-expanded', 'false');
    await expect(tags(page, name)).toHaveCount(0);
  }
  await expect(expandAll(page)).toBeVisible();
  await expect(collapseAll(page)).toHaveCount(0);
});

test('the button follows the cards: opening the last closed one by hand turns it into Collapse all', async ({
  page,
}) => {
  await goto(page, '/hosts', 'Hosts');
  await toggle(page, BUILDER).click();
  await toggle(page, LOW_DISK).click();
  await expect(expandAll(page), 'one card is still closed').toBeVisible();

  await toggle(page, CORDONED).click();
  await expect(collapseAll(page)).toBeVisible();
  await expect(expandAll(page)).toHaveCount(0);

  await toggle(page, LOW_DISK).click();
  await expect(expandAll(page), 'closing one is enough to offer opening them all').toBeVisible();
});

test('Expand all works from the keyboard, and keeps the keyboard where it was', async ({
  page,
}) => {
  await goto(page, '/hosts', 'Hosts');
  await expandAll(page).focus();
  await page.keyboard.press('Enter');
  for (const name of FLEET)
    await expect(toggle(page, name)).toHaveAttribute('aria-expanded', 'true');
  // The same button, now saying the other thing: a keyboard user pressing it
  // twice in a row is not thrown to the top of the page by the first press.
  await expect(collapseAll(page)).toBeFocused();

  await page.keyboard.press('Space');
  for (const name of FLEET)
    await expect(toggle(page, name)).toHaveAttribute('aria-expanded', 'false');
  await expect(expandAll(page)).toBeFocused();
});

test('what Expand all opened is still open after a reload', async ({ page }) => {
  await goto(page, '/hosts', 'Hosts');
  await expandAll(page).click();
  await expect(collapseAll(page)).toBeVisible();

  await reload(page, 'Hosts');
  for (const name of FLEET)
    await expect(toggle(page, name)).toHaveAttribute('aria-expanded', 'true');
  await expect(collapseAll(page)).toBeVisible();

  await collapseAll(page).click();
  await reload(page, 'Hosts');
  for (const name of FLEET)
    await expect(toggle(page, name)).toHaveAttribute('aria-expanded', 'false');
  await expect(expandAll(page)).toBeVisible();
});

test('Expand all acts on the cards a filter shows, and leaves the others as they were', async ({
  page,
}) => {
  // One host with a clean report, so "No report" shows the other two.
  await reshape(page, BUILDER, (host) => ({ ...host, doctor: cleanReport() }));
  await goto(page, '/hosts', 'Hosts');
  await toggle(page, BUILDER).click();
  await expect(toggle(page, BUILDER)).toHaveAttribute('aria-expanded', 'true');

  await page
    .getByRole('group', { name: 'Show hosts by OS health' })
    .getByRole('button', { name: /^No report \d+$/ })
    .click();
  await expect(card(page, BUILDER)).toHaveCount(0);
  await expect(card(page, CORDONED)).toBeVisible();
  await expect(card(page, LOW_DISK)).toBeVisible();

  // The page is showing two cards, both closed, whatever the one it is not
  // showing is doing, and the button says what it will do to the two.
  await expect(expandAll(page)).toBeVisible();
  await expandAll(page).click();
  for (const name of [CORDONED, LOW_DISK])
    await expect(toggle(page, name)).toHaveAttribute('aria-expanded', 'true');
  await expect(collapseAll(page)).toBeVisible();
  await collapseAll(page).click();
  for (const name of [CORDONED, LOW_DISK])
    await expect(toggle(page, name)).toHaveAttribute('aria-expanded', 'false');

  // The host the filter hid was open before and is open still: closing what was
  // on show did not reach it.
  await page.getByRole('button', { name: /^All \d+$/ }).click();
  await expect(toggle(page, BUILDER)).toHaveAttribute('aria-expanded', 'true');
  await expect(toggle(page, CORDONED)).toHaveAttribute('aria-expanded', 'false');
  await expect(toggle(page, LOW_DISK)).toHaveAttribute('aria-expanded', 'false');
});

test('a card the filter hid stays closed through Expand all, and the button reads only the cards on show', async ({
  page,
}) => {
  // The spec above hides a card that is already open, so opening what is on show
  // and opening every card end in the same place, and a button that read every
  // card would say the same in each step. A hidden card that is closed tells them
  // apart: nobody can see it, so pressing Expand all must not open it, and once
  // the two on show are open the button offers to close them.
  await reshape(page, BUILDER, (host) => ({ ...host, doctor: cleanReport() }));
  await goto(page, '/hosts', 'Hosts');
  await settle(page);
  await page
    .getByRole('group', { name: 'Show hosts by OS health' })
    .getByRole('button', { name: /^No report \d+$/ })
    .click();
  await expect(card(page, BUILDER)).toHaveCount(0);

  await expandAll(page).click();
  for (const name of [CORDONED, LOW_DISK])
    await expect(toggle(page, name)).toHaveAttribute('aria-expanded', 'true');
  await expect(collapseAll(page), 'every card on show is open').toBeVisible();

  await page.getByRole('button', { name: /^All \d+$/ }).click();
  await expect(toggle(page, BUILDER), 'nobody opened the card the filter hid').toHaveAttribute(
    'aria-expanded',
    'false',
  );
  await expect(
    expandAll(page),
    'one card is closed again, so the button offers to open',
  ).toBeVisible();
});

test('the button says whose cards it acts on when a filter has narrowed the page', async ({
  page,
}) => {
  await reshape(page, BUILDER, (host) => ({ ...host, doctor: cleanReport() }));
  await goto(page, '/hosts', 'Hosts');
  await expect(expandAll(page)).toHaveAccessibleDescription('Open the details of every host.');

  await page
    .getByRole('group', { name: 'Show hosts by OS health' })
    .getByRole('button', { name: /^No report \d+$/ })
    .click();
  await expect(expandAll(page)).toHaveAccessibleDescription(
    'Open the details of the 2 hosts shown. The others stay as they are.',
  );
});

test('the page says beside the button how many cards it acts on once a filter has narrowed them', async ({
  page,
  isMobile,
}) => {
  // The tooltip says it too, but a phone has no hover, and "Collapse all" that
  // leaves cards open out of sight is a surprise on the page's own terms.
  await reshape(page, BUILDER, (host) => ({ ...host, doctor: cleanReport() }));
  await goto(page, '/hosts', 'Hosts');
  await settle(page);
  const hint = page.getByText(/^Acts on the \d+ hosts shown$/);
  await expect(hint, 'every card is on show, so there is nothing to say').toHaveCount(0);

  await page
    .getByRole('group', { name: 'Show hosts by OS health' })
    .getByRole('button', { name: /^No report \d+$/ })
    .click();
  await expect(hint).toHaveText('Acts on the 2 hosts shown');
  if (isMobile) {
    const box = await expandAll(page).boundingBox();
    expect(box!.height, 'the button is still tall enough to hit').toBeGreaterThanOrEqual(44);
  }
  // From the narrowest phone to a wide desktop the hint and the button stay in
  // the window, and the page does not scroll sideways for them. The page as a
  // whole is judged from 360: the top bar is ten pixels wider than a 320px window
  // while the stream these specs silence reads as reconnecting, whatever the
  // Hosts page holds.
  for (const width of [320, 360, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    for (const part of [hint, expandAll(page)]) {
      const box = await part.boundingBox();
      expect(box!.x, `starts inside a ${width}px window`).toBeGreaterThanOrEqual(0);
      expect(box!.x + box!.width, `ends inside a ${width}px window`).toBeLessThanOrEqual(width);
    }
    if (width >= 360) await expectNoSidewaysScroll(page, `the filtered page at ${width}px`);
  }

  await page.getByRole('button', { name: /^All \d+$/ }).click();
  await expect(hint, 'with every card on show again, it is gone').toHaveCount(0);
});

test('a page with one card has no Expand all, because that card has its own button', async ({
  page,
}) => {
  await rewriteHosts(page, (hosts) => hosts.filter((host) => host.name === BUILDER));
  await goto(page, '/hosts', 'Hosts');
  await expect(toggle(page, BUILDER)).toBeVisible();
  await expect(expandAll(page)).toHaveCount(0);
  await expect(collapseAll(page)).toHaveCount(0);
  await toggle(page, BUILDER).click();
  await expect(collapseAll(page)).toHaveCount(0);
});

test('a button that leaves the page while it holds focus hands it to the filters, not to the page', async ({
  page,
}) => {
  // Nobody presses anything on the page: the list answers with fewer hosts the
  // next time it is asked, which is what a host being deleted looks like from
  // here, and R asks without moving focus off the button.
  const gone = new Set<string>();
  await rewriteHosts(page, (hosts) => hosts.filter((host) => !gone.has(String(host.name))));
  await goto(page, '/hosts', 'Hosts');
  await settle(page);
  await expandAll(page).focus();
  await expect(expandAll(page)).toBeFocused();

  gone.add(BUILDER).add(LOW_DISK);
  await page.keyboard.press('r');
  await expect(page.getByRole('article')).toHaveCount(1);
  await expect(expandAll(page)).toHaveCount(0);
  // Not the page: a reader who was on the button is left where the filters are.
  await expect(page.getByRole('button', { name: /^All \d+$/ })).toBeFocused();
});

test('and with a filter on it hands focus to the filter that is on', async ({ page }) => {
  const gone = new Set<string>();
  await rewriteHosts(page, (hosts) =>
    hosts
      .filter((host) => !gone.has(String(host.name)))
      .map((host) => (host.name === BUILDER ? { ...host, doctor: cleanReport() } : host)),
  );
  await goto(page, '/hosts', 'Hosts');
  await settle(page);
  const chip = page
    .getByRole('group', { name: 'Show hosts by OS health' })
    .getByRole('button', { name: /^No report \d+$/ });
  await chip.click();
  await expect(page.getByRole('article')).toHaveCount(2);
  await expandAll(page).focus();
  await expect(expandAll(page)).toBeFocused();

  gone.add(LOW_DISK);
  await page.keyboard.press('r');
  await expect(page.getByRole('article')).toHaveCount(1);
  await expect(expandAll(page)).toHaveCount(0);
  await expect(chip).toBeFocused();
});

test('a host that joins after Expand all arrives closed, and the button offers Expand all again', async ({
  page,
}) => {
  // Expand all opens the cards that are on the page when it is pressed and is not
  // a mode: the page remembers which hosts were open, and a host it has not met is
  // closed like any other. R asks for the hosts again, as a host joining would.
  const LATE = 'demo-late-1';
  let joined = false;
  await rewriteHosts(page, (hosts) =>
    joined ? [...hosts, { ...hosts[0], id: 'host_late', name: LATE }] : hosts,
  );
  await goto(page, '/hosts', 'Hosts');
  await settle(page);
  await expandAll(page).click();
  await expect(collapseAll(page)).toBeVisible();

  joined = true;
  await page.keyboard.press('r');
  await expect(toggle(page, LATE)).toHaveAttribute('aria-expanded', 'false');
  await expect(expandAll(page)).toBeVisible();
  for (const name of FLEET)
    await expect(toggle(page, name)).toHaveAttribute('aria-expanded', 'true');
});
