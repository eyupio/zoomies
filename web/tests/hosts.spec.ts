/**
 * Adding a host, from the page that does it.
 *
 * The page's promise is that an operator never has to leave it: it comes
 * filled in, hands over one command, and says when the machine has joined.
 * The join itself is made the way an agent makes it -- a POST to the agent
 * route carrying the token the page showed -- so what is proved is the real
 * hand-off, against the real binary, rather than a mock of either half.
 */
import { expect, test, type Page } from '@playwright/test';
import {
  browserOverride,
  expectNoReload,
  FIXTURE,
  goto,
  pageHeading,
  plantMarker,
} from './support/fixtures';

test.use(browserOverride);

/**
 * The install command. A <pre> carries no role of its own, so it is reached
 * inside the named hand-off panel by the installer it invokes. The credential
 * is shell-quoted, and should stay that way even when this test reads it back.
 */
function installCommand(page: Page) {
  return page
    .getByRole('region', { name: 'Run this on the new host' })
    .locator('pre', { hasText: 'zoomies.sh/install.sh' });
}

/** Read the one argument after --join-token, quoted or not. */
async function joinToken(page: Page): Promise<string | undefined> {
  const command = await installCommand(page).innerText();
  return /--join-token\s+['"]?(zoojoin_[^'"\s]+)/.exec(command)?.[1];
}

/** The chips offering the labels the seeded pools select hosts by. */
function labelOffers(page: Page) {
  return page.getByRole('group', { name: 'Labels pools here select hosts by' });
}

test('the page comes filled in and hands over a command carrying the token', async ({ page }) => {
  await goto(page, '/hosts/new', 'Add a host');
  const origin = new URL(page.url()).origin;

  // The address this browser reached the controller on, since the test
  // server has no external URL of its own.
  await expect(page.getByRole('textbox', { name: 'Controller address' })).toHaveValue(origin);
  // Capacity is the agent's to decide unless told otherwise.
  await expect(page.getByRole('textbox', { name: 'Capacity' })).toHaveValue('');
  await expect(page.getByRole('textbox', { name: 'Capacity' })).toHaveAttribute(
    'placeholder',
    'Automatic',
  );

  // The seeded pools select hosts by architecture, so both values are on
  // offer; taking one puts it in the editor and takes it off the offer.
  const offers = labelOffers(page);
  await expect(offers.getByRole('button', { name: 'arch=amd64' })).toBeVisible();
  await offers.getByRole('button', { name: 'arch=arm64' }).click();
  await expect(page.getByRole('textbox', { name: 'Label 1 key' })).toHaveValue('arch');
  await expect(page.getByRole('textbox', { name: 'Label 1 value' })).toHaveValue('arm64');
  await expect(offers.getByRole('button', { name: 'arch=arm64' })).toHaveCount(0);
  // A host carries one value per key, so the other offer replaces rather than adds.
  await offers.getByRole('button', { name: 'arch=amd64' }).click();
  await expect(page.getByRole('textbox', { name: 'Label 1 value' })).toHaveValue('amd64');
  await expect(page.getByRole('textbox', { name: 'Label 2 key' })).toHaveCount(0);

  await page.getByRole('button', { name: 'Get the command' }).click();
  await expect(page.getByRole('heading', { name: 'Run this on the new host' })).toBeVisible();
  const command = installCommand(page);
  await expect(command).toContainText('--mode agent');
  await expect(command).toContainText(`--controller '${origin}'`);
  await expect(command).toContainText('--version dev');
  await expect(page.getByText('Host install channel').locator('..')).toContainText(':dev');
  // The page has other live regions -- the connection, the problems count --
  // so the waiting view is reached inside the panel that holds it.
  await expect(
    page.getByRole('region', { name: 'Run this on the new host' }).getByRole('status'),
  ).toContainText('Waiting for the host to join');

  // Going back keeps what was typed and revokes the token, so the settings
  // cost one click to fix and no credential is left lying about.
  await page.getByRole('button', { name: 'Discard this token and change the settings' }).click();
  await expect(page.getByRole('heading', { name: 'Describe the host' })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Label 1 value' })).toHaveValue('amd64');
});

test('the page says so the moment the host joins, without a reload', async ({ page }) => {
  await goto(page, '/hosts/new', 'Add a host');
  await labelOffers(page).getByRole('button', { name: 'arch=arm64' }).click();
  await page.getByRole('button', { name: 'Get the command' }).click();

  const token = await joinToken(page);
  expect(token, 'the command carries the token').toBeTruthy();
  await plantMarker(page);

  // Unique, because a host that joins under an existing name takes over its
  // row, and a retry must not inherit a leftover from a failed run.
  const name = `e2e-host-${Date.now()}`;
  let hostId = '';
  try {
    const join = await page.request.post('/api/v1/agent/join', {
      data: {
        protocol_version: 1,
        join_token: token,
        name,
        capacity: 3,
        os: 'linux',
        arch: 'arm64',
        version: 'e2e',
        backends: [{ kind: 'docker', available: true }],
      },
    });
    expect(join.ok(), 'the agent join succeeded').toBeTruthy();
    hostId = ((await join.json()) as { host_id: string }).host_id;

    await expect(page.getByRole('heading', { name: `${name} joined` })).toBeVisible();
    await expect(page.getByText('linux/arm64')).toBeVisible();
    // The token's labels reached the host, and the agent's own capacity stood
    // because the page left it blank.
    await expect(page.locator('dd', { hasText: 'arch=arm64' })).toBeVisible();
    await expect(page.locator('dd', { hasText: '3 runners at once' })).toBeVisible();
    // The arm pool selects arch=arm64 and runs on Docker, which this host offers.
    await expect(page.getByText(`can place runners here: ${FIXTURE.armPool}`)).toBeVisible();
    await expectNoReload(page);
  } finally {
    if (hostId) await page.request.delete(`/api/v1/hosts/${hostId}?force=true`);
  }
});

test('the Hosts page leads here', async ({ page }) => {
  await goto(page, '/hosts', 'Hosts');
  await page.getByRole('link', { name: 'Add a host' }).first().click();
  await expect(pageHeading(page, 'Add a host')).toBeVisible();
});

/**
 * A host that reports no memory left is held and, since the same measurement
 * is what overwhelms it, throttled on the same heartbeat. The card shows the
 * throttle's sentence rather than the hold's, because it says everything the
 * hold's does and what happens next. The memory figure recovers on the next
 * beat; the throttle stays a rung until the host has been calm for five
 * minutes, which is the fleet's promise rather than this test's.
 */
test('host usage distinguishes a connected agent from held new starts and recovers', async ({
  page,
}, testInfo) => {
  await goto(page, '/hosts/new', 'Add a host');
  await page.getByRole('button', { name: 'Get the command' }).click();
  const token = await joinToken(page);
  const name = `usage-host-${Date.now()}`;
  let hostId = '';
  try {
    const join = await page.request.post('/api/v1/agent/join', {
      data: {
        protocol_version: 1,
        join_token: token,
        name,
        capacity: 2,
        os: 'linux',
        arch: 'amd64',
        cpus: 8,
        memory_mb: 16384,
        version: 'dev',
        backends: [{ kind: 'docker', available: true }],
      },
    });
    expect(join.ok()).toBeTruthy();
    const credentials = (await join.json()) as { host_id: string; agent_token: string };
    hostId = credentials.host_id;
    const heartbeat = async (memory: number) => {
      const response = await page.request.post('/api/v1/agent/heartbeat', {
        headers: { Authorization: `Bearer ${credentials.agent_token}` },
        data: { protocol_version: 1, usage: { cpu_percent: 20, memory_available_mb: memory } },
      });
      expect(response.ok()).toBeTruthy();
    };
    await heartbeat(0);
    await goto(page, '/hosts', 'Hosts');
    const card = page.getByRole('article', { name, exact: true });
    await expect(card).toContainText('Agent connected');
    await expect(card).toContainText('CPU usage 20%');
    await expect(card).toContainText('0 B memory available');
    // Two slots stepped to one: the controller's sentence names the rung,
    // the measurement and what the running work is doing.
    await expect(card).toContainText(
      "throttled to 1 of 2 slots (step 1 of 3) after sustained pressure: available memory is at or below the host's reserve",
    );
    await expect(card).toContainText('running jobs continue');
    await expect(card).toContainText(/0 of 1 slots in use\s*· throttled from 2/);
    await testInfo.attach('host-pressure-hold', {
      body: await card.screenshot(),
      contentType: 'image/png',
    });
    await heartbeat(8192);
    await expect(card).toContainText('8.0 GB memory available');
    // Still on its rung: calm has to last before a step is given back.
    await expect(card).toContainText('throttled from 2');
  } finally {
    if (hostId) await page.request.delete(`/api/v1/hosts/${hostId}?force=true`);
  }
});

/**
 * A throttle is the fleet's answer to a host that has stopped keeping up, and
 * the card has to carry all of it: that it happened, how far, why, what it
 * costs the running jobs, and how it ends -- plus the one thing an operator
 * can do about it early, once the cause is known and gone.
 */
test('a host under sustained pressure is throttled, says why, and an operator can lift it', async ({
  page,
}, testInfo) => {
  await goto(page, '/hosts/new', 'Add a host');
  await page.getByRole('button', { name: 'Get the command' }).click();
  const token = await joinToken(page);
  const name = `throttle-host-${Date.now()}`;
  let hostId = '';
  try {
    const join = await page.request.post('/api/v1/agent/join', {
      data: {
        protocol_version: 1,
        join_token: token,
        name,
        capacity: 4,
        os: 'linux',
        arch: 'amd64',
        cpus: 8,
        memory_mb: 16384,
        version: 'dev',
        backends: [{ kind: 'docker', available: true }],
      },
    });
    expect(join.ok()).toBeTruthy();
    const credentials = (await join.json()) as { host_id: string; agent_token: string };
    hostId = credentials.host_id;
    // A load average of twenty on eight CPUs is a runnable queue the machine
    // is not draining, and it steps the throttle up on the first beat: memory
    // is left healthy so the sentence names the load and nothing else.
    const beat = await page.request.post('/api/v1/agent/heartbeat', {
      headers: { Authorization: `Bearer ${credentials.agent_token}` },
      data: {
        protocol_version: 1,
        usage: { cpu_percent: 60, memory_available_mb: 8192, load_average_1m: 20 },
      },
    });
    expect(beat.ok()).toBeTruthy();

    await goto(page, '/hosts', 'Hosts');
    const card = page.getByRole('article', { name, exact: true });
    // The badge carries the step in its title, so hovering answers "how bad".
    await expect(card.getByText('Throttled', { exact: true })).toHaveAttribute(
      'title',
      /^Throttled, step 1 of 3/,
    );
    await expect(card).toContainText(
      "throttled to 3 of 4 slots (step 1 of 3) after sustained pressure: the 1-minute load average is 20.0, at least twice the host's 8 CPUs",
    );
    await expect(card).toContainText('the throttle lifts one step after 5m of calm');
    await expect(card).toContainText(/0 of 3 slots in use\s*· throttled from 4/);
    await expect(card).toContainText('load 20');
    await testInfo.attach('host-throttled', {
      body: await card.screenshot(),
      contentType: 'image/png',
    });

    // Lifting it is a menu action, and the card follows without a reload.
    await plantMarker(page);
    await card.getByRole('button', { name: /Actions for/ }).click();
    await page.getByRole('menuitem', { name: 'Lift the throttle' }).click();
    await expect(card).toContainText(/0 of 4 slots in use/);
    await expect(card).not.toContainText('throttled from');
    await expect(card.getByTitle(/^Throttled, step/)).toHaveCount(0);
    await expect(page.getByText(`Throttle lifted on ${name}`)).toBeVisible();
    await expectNoReload(page);

    // And the controller agrees: the row is clear, not just the card.
    const read = await page.request.get(`/api/v1/hosts/${hostId}`);
    const host = (await read.json()) as { throttle?: unknown; effective_capacity: number };
    expect(host.throttle).toBeUndefined();
    expect(host.effective_capacity).toBe(4);
  } finally {
    if (hostId) await page.request.delete(`/api/v1/hosts/${hostId}?force=true`);
  }
});

test('runner capacity is adjustable from the host card without opening the full editor', async ({
  page,
}) => {
  await goto(page, '/hosts', 'Hosts');
  const card = page.getByRole('article', { name: 'demo-builder-1', exact: true });
  let patched: Record<string, unknown> | null = null;
  await page.route('**/api/v1/hosts/*', async (route) => {
    if (route.request().method() !== 'PATCH') {
      await route.continue();
      return;
    }
    patched = route.request().postDataJSON() as Record<string, unknown>;
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
  });

  await card.getByRole('button', { name: 'Adjust', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Adjust demo-builder-1' });
  await dialog.getByRole('spinbutton', { name: 'Maximum runners on this host' }).fill('7');
  // The slider is the same setting: it followed the typed figure.
  await expect(dialog.getByRole('slider', { name: 'Runner slots' })).toHaveAttribute(
    'aria-valuetext',
    '7 runners',
  );
  await dialog.getByRole('button', { name: 'Save changes' }).click();

  await expect(dialog).toBeHidden();
  // The reserve travels with the capacity, because the host reports both
  // figures and the dialog offers both. Its value is whatever the host had
  // -- another spec on this shared server may have set it -- so only the
  // figure this test typed is pinned.
  await expect.poll(() => patched).toMatchObject({ capacity: 7 });
  expect(Object.keys(patched ?? {}).sort()).toEqual([
    'capacity',
    'reserve_cpus',
    'reserve_disk_mb',
    'reserve_memory_mb',
  ]);
});

/*
 * The adjust dialog says what fits and marks it on each slider. Past that
 * mark it warns rather than refuses -- the operator who knows the jobs are
 * light is right to go past it -- and one press puts everything back on the
 * recommendation.
 */
test('the adjust dialog recommends, warns past the recommendation and can set itself to it', async ({
  page,
}) => {
  await goto(page, '/hosts', 'Hosts');
  // demo-builder-1: 16 cores, 32 GB. The demo pools ask for two cores and
  // 4 GB, so what fits today follows from whatever reserve the host has --
  // another spec on this shared server may have set one -- and seven fit
  // once the recommended core and 3 GB are held back.
  const builder = await page.request
    .get('/api/v1/hosts')
    .then((r) => r.json() as Promise<{ items: Record<string, number | string>[] }>)
    .then((page) => page.items.find((h) => h.name === 'demo-builder-1'));
  if (!builder) throw new Error('demo-builder-1 is not in the fixture');
  const reservedCores = Number(builder.reserve_cpus ?? 0);
  const reservedMb = Number(builder.reserve_memory_mb ?? 0);
  const fits = Math.max(
    1,
    Math.min(Math.floor((16 - reservedCores) / 2), Math.floor((32768 - reservedMb) / 4096)),
  );
  const card = page.getByRole('article', { name: 'demo-builder-1', exact: true });
  await card.getByRole('button', { name: 'Adjust', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Adjust demo-builder-1' });
  await expect(dialog).toContainText(`room for ${fits} runner`);

  const slots = dialog.getByRole('slider', { name: 'Runner slots' });
  const cores = dialog.getByRole('slider', { name: 'Cores held back' });
  const memory = dialog.getByRole('slider', { name: 'Memory held back' });
  await expect(slots).toHaveAttribute('aria-valuetext', `${builder.capacity} runners`);

  // Past the recommendation on slots: the callout says by how much.
  await dialog.getByRole('spinbutton', { name: 'Maximum runners on this host' }).fill('12');
  const warning = dialog.getByRole('status').filter({ hasText: 'Above the recommendation' });
  await expect(warning).toContainText('12 runners would ask for 24 cores');

  // Too much held back: a different sentence, on the reserve. End is the
  // last notch, all but one core.
  await cores.focus();
  await page.keyboard.press('End');
  await expect(cores).toHaveAttribute('aria-valuetext', '15 cores');
  await expect(
    dialog.getByRole('status').filter({ hasText: 'More cores held back than recommended' }),
  ).toBeVisible();

  // One press: everything back on the mark, and the warnings gone. The
  // reserve takes a core, so the capacity that fits is one fewer.
  await dialog.getByRole('button', { name: 'Set to recommendations' }).click();
  await expect(dialog).toContainText('room for 7 runners');
  await expect(slots).toHaveAttribute('aria-valuetext', '7 runners');
  await expect(cores).toHaveAttribute('aria-valuetext', '1 core');
  await expect(memory).toHaveAttribute('aria-valuetext', '3 GB');
  await expect(dialog.getByRole('status')).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Set to recommendations' })).toBeDisabled();
  // A step past it re-enables the button: the recommendation is a place to
  // come back to, not a lock.
  await memory.focus();
  await page.keyboard.press('ArrowRight');
  await expect(memory).toHaveAttribute('aria-valuetext', '4 GB');
  await expect(dialog.getByRole('button', { name: 'Set to recommendations' })).toBeEnabled();
});

/*
 * The capacity map is one chart of every host, and the operator chooses
 * which hosts and which figures are on it. The choice survives a reload:
 * somebody watching two machines out of forty wants them there tomorrow.
 */
test('the host capacity map toggles hosts and measurements, and remembers the choice', async ({
  page,
}) => {
  await goto(page, '/hosts', 'Hosts');
  const map = page.getByRole('region', { name: 'Host capacity map', exact: true });
  const chart = map.getByRole('img').first();
  // Two measurements on by default, across every host the fixture has: the
  // demo fleet and whatever the diagnostics fixture adds beside it.
  const hostList = await page.request
    .get('/api/v1/hosts')
    .then((r) => r.json() as Promise<{ items: { id: string }[] }>);
  const n = hostList.items.length;
  expect(n).toBeGreaterThanOrEqual(3);
  await expect(chart).toHaveAttribute(
    'aria-label',
    new RegExp(`^${2 * n} lines across ${n} of ${n} hosts`),
  );

  // History came from the samples route: a series for every host, and the
  // demo hosts' measured CPU on it.
  const samples = await page.request
    .get('/api/v1/hosts/samples?window=1h')
    .then((r) => r.json() as Promise<{ items: { host_id: string; cpu_percent?: number }[] }>);
  // Every host now in the fleet has a series; a host another spec deleted
  // keeps its rows until the prune, and is not a host on this chart.
  const sampled = new Set(samples.items.map((s) => s.host_id));
  for (const host of hostList.items) expect(sampled.has(host.id)).toBe(true);
  expect(samples.items.some((s) => typeof s.cpu_percent === 'number')).toBe(true);

  const measurements = map.getByRole('group', { name: 'Measurements shown' });
  await measurements.getByRole('button', { name: 'Memory used' }).click();
  await expect(chart).toHaveAttribute(
    'aria-label',
    new RegExp(`^${3 * n} lines across ${n} of ${n} hosts`),
  );
  await measurements.getByRole('button', { name: 'CPU used' }).click();
  await expect(chart).toHaveAttribute(
    'aria-label',
    new RegExp(`^${2 * n} lines across ${n} of ${n} hosts`),
  );

  const hosts = map.getByRole('group', { name: 'Hosts shown' });
  const arm = hosts.getByRole('button', { name: /^demo-arm-1/ });
  await arm.click();
  await expect(arm).toHaveAttribute('aria-pressed', 'false');
  await expect(chart).toHaveAttribute(
    'aria-label',
    new RegExp(`^${2 * (n - 1)} lines across ${n - 1} of ${n} hosts`),
  );
  // "Only" narrows the chart to one host.
  await hosts.getByRole('button', { name: 'Only' }).first().click();
  await expect(chart).toHaveAttribute('aria-label', new RegExp(`across 1 of ${n} hosts`));

  // The window is a control of the panel too.
  await map.getByRole('button', { name: 'The last hour, minute by minute' }).click();
  await expect(chart).toHaveAttribute('aria-label', /the last hour, minute by minute/);

  await page.reload();
  await page.getByRole('heading', { level: 1, name: 'Hosts' }).waitFor();
  await expect(map.getByRole('img').first()).toHaveAttribute(
    'aria-label',
    new RegExp(`^2 lines across 1 of ${n} hosts, the last hour, minute by minute`),
  );
  // The legend row says which host is on, in words as well as in colour:
  // the first row is demo-arm-1, the hosts being listed by name.
  await expect(hosts.getByRole('button', { name: /^demo-arm-1/ })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await expect(hosts.getByRole('button', { name: /^demo-builder-1/ })).toHaveAttribute(
    'aria-pressed',
    'false',
  );
});

/**
 * A host's disk is the resource that runs out first and says nothing when it
 * does.
 *
 * Every slot on the machine still reads as free, so the fleet keeps placing
 * work there, and the job fails part-way through a checkout or a cache
 * restore -- which reads as a flaky build rather than as a full disk. The
 * agent has measured it since ZF-103; until now it was stored and never shown.
 */
test('a host says how much disk its runners have, and marks the one that is nearly out', async ({
  page,
}) => {
  await goto(page, '/hosts', 'Hosts');

  const roomy = page.getByRole('article', { name: 'demo-builder-1', exact: true });
  await expect(roomy).toContainText('GB free of');
  // 717 GB of 1024: the figure is the work directory's filesystem, in the
  // same units the rest of the card uses.
  await expect(roomy).toContainText(/717 GB free of 1,024/);

  // The one at six percent is marked, because "31 GB free" beside "512" is
  // not something anybody reads as urgent on its own.
  const tight = page.getByRole('article', { name: 'demo-builder-2', exact: true });
  const low = tight.locator('.low');
  await expect(low).toHaveText(/31 GB free of 512/);

  // And the roomy one is not marked, or the mark says nothing.
  await expect(roomy.locator('.low')).toHaveCount(0);
});

/**
 * Slots say whether the fleet will place another runner here; the committed
 * figures say whether the machine can carry it.
 *
 * They are different questions, and only the second one explains a host with
 * free slots taking nothing. The reservation has decided placement since
 * ZF-103b and appeared nowhere: an operator could see what a machine was and
 * what it had left, but not what the fleet had already promised away on it.
 */
test('a host shows what the fleet has committed on it, and lets an operator hold some back', async ({
  page,
}) => {
  await goto(page, '/hosts', 'Hosts');

  const card = page.getByRole('article', { name: 'demo-builder-1', exact: true });
  const committed = card.getByRole('region', { name: /Resources committed/ });
  await expect(committed).toBeVisible();
  // 16 CPUs and 32 GB, less the floors, against what the runners on it hold:
  // 15.2 CPUs rather than 16, because the scheduler keeps a twentieth of the
  // machine back for the daemon and the agent.
  await expect(committed).toContainText('CPU');
  await expect(committed).toContainText('Memory');
  await expect(committed).toContainText(/of 15\.2/);

  // And the reserve is settable from the same card, in Adjust -- the one place
  // that owns this host's resources -- against the figures it has reported.
  await plantMarker(page);
  await card.getByRole('button', { name: 'Adjust', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Adjust demo-builder-1' });
  await expect(dialog).toBeVisible();
  const memory = dialog.getByRole('slider', { name: 'Memory held back' });
  await expect(memory).toBeVisible();
  // 8 GB is a notch on a 32 GB machine; the slider is driven by the keyboard
  // because that is what an operator without a mouse has.
  await memory.focus();
  await page.keyboard.press('Home');
  for (let i = 0; i < 20 && (await memory.getAttribute('aria-valuetext')) !== '8 GB'; i++) {
    await page.keyboard.press('ArrowRight');
  }
  await expect(memory).toHaveAttribute('aria-valuetext', '8 GB');
  await dialog.getByRole('button', { name: 'Save changes' }).click();
  await expect(dialog).not.toBeVisible();

  // 32 GB less the 8 GB held back: the bar is drawn against what may actually
  // be placed on, not against the machine.
  await expect(committed).toContainText(/of 24 GB/);
  await expectNoReload(page);
});

/**
 * A reserve larger than the machine leaves nothing placeable, and is what
 * typing megabytes where you meant gigabytes looks like.
 *
 * The sliders cannot reach it -- their last notch leaves room to place on --
 * so this is pinned where a hand-written request still arrives: the API
 * refuses it, and the refusal says what is wrong rather than clamping.
 */
test('a reserve that would leave nothing to place on is refused', async ({ page }) => {
  const hosts = await page.request
    .get('/api/v1/hosts')
    .then((r) => r.json() as Promise<{ items: Record<string, unknown>[] }>);
  const host = hosts.items.find((h) => h.name === 'demo-builder-2');
  expect(host, 'demo-builder-2 is in the fixture').toBeTruthy();
  const refused = await page.request.patch(`/api/v1/hosts/${String(host?.id)}`, {
    data: { reserve_memory_mb: Number(host?.memory_mb ?? 0) },
  });
  expect(refused.status()).toBe(422);
  expect(await refused.text()).toContain('nothing to place on');
});

/**
 * The two settings a host has, each reached from the thing it describes: the
 * resources from Adjust beside the slot bar, and the labels from the block
 * that lists them. Capacity was on both once, with two different ideas of a
 * good number, and only one of them knew what a runner in this fleet asks for.
 */
test('the labels dialog edits labels and nothing else', async ({ page }) => {
  await goto(page, '/hosts', 'Hosts');
  const card = page.getByRole('article', { name: 'demo-builder-1', exact: true });
  let patched: Record<string, unknown> | null = null;
  await page.route('**/api/v1/hosts/*', async (route) => {
    if (route.request().method() !== 'PATCH') {
      await route.continue();
      return;
    }
    patched = route.request().postDataJSON() as Record<string, unknown>;
    await route.fulfill({ status: 200, contentType: 'application/json', body: '{}' });
  });

  await card.getByRole('button', { name: /Actions for/ }).click();
  await page.getByRole('menuitem', { name: 'Edit labels' }).click();
  const dialog = page.getByRole('dialog', { name: 'Labels on demo-builder-1' });
  await expect(dialog).toBeVisible();
  // No resource setting in here at all: that is Adjust's, and the dialog says so.
  await expect(dialog).toContainText('Capacity and the reserve are under Adjust');
  await expect(dialog.getByRole('slider')).toHaveCount(0);
  await expect(dialog.getByRole('spinbutton')).toHaveCount(0);

  await dialog.getByRole('button', { name: 'Save changes' }).click();
  await expect(dialog).toBeHidden();
  expect(Object.keys(patched ?? {})).toEqual(['labels']);
});

/**
 * The three ways a join is refused, at the route an agent calls.
 *
 * A token is single-use and lasts an hour by default, so "already used" is an
 * ordinary failure rather than an edge case -- and the page that mints the
 * token is where an operator comes back to when it happens. Each refusal is a
 * sentence rather than a status code.
 *
 * Expiry is pinned at the API tier instead: making a token old enough means
 * forging the row, since `CreateJoinToken` reads a non-positive TTL as "use the
 * default", and a browser has no way to do that.
 */
test('a token that is spent or nonsense is refused with a reason', async ({ page }) => {
  await goto(page, '/hosts/new', 'Add a host');
  await page.getByRole('button', { name: 'Get the command' }).click();
  const token = await joinToken(page);
  expect(token, 'the command carries the token').toBeTruthy();

  const join = (joinToken: string, name: string) =>
    page.request.post('/api/v1/agent/join', {
      data: {
        protocol_version: 1,
        join_token: joinToken,
        name,
        capacity: 1,
        os: 'linux',
        arch: 'amd64',
        version: 'e2e',
        backends: [{ kind: 'docker', available: true }],
      },
    });

  const name = `e2e-refusal-${Date.now()}`;
  let hostId = '';
  try {
    // Nonsense first, so nothing has been spent: a truncated paste, or the
    // token id from the Hosts page rather than the secret shown once beside it.
    const garbage = await join('zoojoin_notatoken', `${name}-garbage`);
    expect(garbage.status(), 'a token that never existed').toBe(422);
    expect(JSON.stringify(await garbage.json())).toMatch(/join token/i);

    // Then the real one, which works exactly once.
    const first = await join(token as string, name);
    expect(first.ok(), 'the first join succeeded').toBeTruthy();
    hostId = ((await first.json()) as { host_id: string }).host_id;

    const second = await join(token as string, `${name}-second`);
    expect(second.status(), 'the same token twice').toBe(422);
    expect(JSON.stringify(await second.json())).toMatch(/join token/i);
  } finally {
    if (hostId) await page.request.delete(`/api/v1/hosts/${hostId}?force=true`);
  }
});

test('an older remote agent offers a copyable upgrade command without a join token', async ({
  page,
}) => {
  const command =
    "curl -fsSL https://zoomies.sh/install.sh | sh -s -- --upgrade --mode agent --version 'v1.2.3'";
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: async (value: string) => sessionStorage.setItem('copied-upgrade', value),
      },
    });
  });
  await page.route(/\/api\/v1\/hosts(?:\?.*)?$/, async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    body.items = body.items.map((host: { name: string }) =>
      host.name === 'demo-builder-1'
        ? {
            ...host,
            embedded: false,
            version: '1.0',
            version_skew: 'behind',
            upgrade_version: '1.2.3',
            upgrade_command: command,
            upgrade_note: 'Run this on the host. Its existing credentials are kept.',
          }
        : host,
    );
    await route.fulfill({ json: body });
  });
  await goto(page, '/hosts', 'Hosts');
  const card = page.getByRole('article', { name: 'demo-builder-1', exact: true });
  // Folded away until it is asked for, so that a fleet due an upgrade is still
  // a grid of comparable cards rather than a page of repeated instructions.
  const reveal = card.getByText('Update this agent to 1.2.3');
  await expect(card.getByText(command)).toBeHidden();
  await reveal.click();
  await expect(card.getByText(command)).toBeVisible();
  await card.getByRole('button', { name: 'Copy the upgrade command' }).click();
  await expect
    .poll(() => page.evaluate(() => sessionStorage.getItem('copied-upgrade')))
    .toBe(command);
  await expect(card).not.toContainText('--join-token');
});

/*
 * The map's shortest windows are drawn finer than the controller's minute
 * samples, ten seconds to a point, so an operator watching a job land can
 * see each heartbeat rather than the last of every two. The history behind
 * them is still the samples route, asked for the same span.
 */
test('the host capacity map has windows down to the last minute, drawn ten seconds to a point', async ({
  page,
}) => {
  await goto(page, '/hosts', 'Hosts');
  const map = page.getByRole('region', { name: 'Host capacity map', exact: true });
  const chart = map.getByRole('img').first();
  const windows = map.getByRole('group', { name: 'Window' });
  await expect(windows.getByRole('button')).toHaveText([
    '1m',
    '5m',
    '10m',
    '1h',
    '6h',
    '24h',
    '7d',
  ]);

  const fetched = page.waitForRequest((r) => r.url().includes('/api/v1/hosts/samples?window=5m'));
  await windows.getByRole('button', { name: 'The last 5 minutes, in 10-second points' }).click();
  await fetched;
  await expect(chart).toHaveAttribute('aria-label', /the last 5 minutes, in 10-second points/);
  // Thirty points across five minutes, and the timeline control steps through them.
  await expect(map.getByRole('slider', { name: 'Inspect a moment' })).toHaveAttribute('max', '29');
  // A moment in a sub-minute window is named to the second.
  await expect(map.locator('output')).toHaveText(/^\d{1,2}:\d{2}:\d{2}/);

  await windows.getByRole('button', { name: 'The last minute, in 10-second points' }).click();
  await expect(map.getByRole('slider', { name: 'Inspect a moment' })).toHaveAttribute('max', '5');
  await expect(chart).toHaveAttribute('aria-label', /the last minute, in 10-second points/);
});
