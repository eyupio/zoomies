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
  const dialog = page.getByRole('dialog', { name: 'Runner capacity' });
  await dialog.getByRole('spinbutton', { name: 'Maximum runners on this host' }).fill('7');
  await dialog.getByRole('button', { name: 'Save capacity' }).click();

  await expect(dialog).toBeHidden();
  await expect.poll(() => patched).toEqual({ capacity: 7 });
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
  // 16 CPUs and 32 GB, less the floors, against what the runners on it hold.
  await expect(committed).toContainText('CPU');
  await expect(committed).toContainText('Memory');
  await expect(committed).toContainText(/of 16/);

  // And the reserve is settable from the same card, against the figures this
  // host has actually reported.
  await plantMarker(page);
  await card.getByRole('button', { name: /Actions for/ }).click();
  await page.getByRole('menuitem', { name: 'Edit capacity and labels' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  const memory = dialog.getByLabel('Memory (MB)');
  await expect(memory).toBeVisible();
  await memory.fill('8192');
  await dialog.getByRole('button', { name: 'Save changes' }).click();
  await expect(dialog).not.toBeVisible();

  // 32 GB less the 8 GB held back: the bar is drawn against what may actually
  // be placed on, not against the machine.
  await expect(committed).toContainText(/of 24 GB/);
  await expectNoReload(page);
});

/**
 * A reserve larger than the machine leaves nothing placeable, and is what
 * typing megabytes where you meant gigabytes looks like. It is refused, and
 * the refusal says what is wrong rather than failing silently on save.
 */
test('a reserve that would leave nothing to place on is refused', async ({ page }) => {
  await goto(page, '/hosts', 'Hosts');
  const card = page.getByRole('article', { name: 'demo-builder-2', exact: true });
  await card.getByRole('button', { name: /Actions for/ }).click();
  await page.getByRole('menuitem', { name: 'Edit capacity and labels' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Memory (MB)').fill('16384');
  await expect(dialog).toContainText('nothing to place on');
  await expect(dialog.getByRole('button', { name: 'Save changes' })).toBeDisabled();
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
