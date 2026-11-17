/**
 * A host's registry mirror on its card.
 *
 * The mirror keeps the public Docker Hub images a host's docker-in-docker sidecars
 * pull. A host says that it is on, or what is wrong with it, in a heartbeat, and the
 * controller turns that into sentences. What is worth testing here is what an
 * operator would otherwise get wrong: that a host with no mirror says nothing, that
 * a mirror that is on says so as it happens, that a problem is a warning that says
 * nothing is failing, and that what an agent says is never shown as it was written.
 */
import { expect, test, type Page } from '@playwright/test';
import { goto, openHostCard } from './support/fixtures';

type Credentials = { host_id: string; agent_token: string };

/** Enrol a host the way an agent does, through a join token from the Add a host page. */
async function enrol(page: Page, name: string): Promise<Credentials> {
  await goto(page, '/hosts/new', 'Add a host');
  await page.getByRole('button', { name: 'Get the command' }).click();
  const command = await page
    .getByRole('region', { name: 'Run this on the new host' })
    .locator('pre', { hasText: 'zoomies.sh/install.sh' })
    .innerText();
  const token = /--join-token\s+['"]?(zoojoin_[^'"\s]+)/.exec(command)?.[1];
  const join = await page.request.post('/api/v1/agent/join', {
    data: {
      protocol_version: 1,
      join_token: token,
      name,
      capacity: 1,
      os: 'linux',
      arch: 'amd64',
      version: 'dev',
      backends: [{ kind: 'docker', available: true }],
    },
  });
  expect(join.ok()).toBeTruthy();
  return (await join.json()) as Credentials;
}

/** One heartbeat, saying what the agent says about its mirror, or nothing about one. */
async function beat(
  page: Page,
  credentials: Credentials,
  registryMirror?: Record<string, unknown>,
): Promise<void> {
  const response = await page.request.post('/api/v1/agent/heartbeat', {
    headers: { Authorization: `Bearer ${credentials.agent_token}` },
    data: {
      protocol_version: 1,
      ...(registryMirror ? { registry_mirror: registryMirror } : {}),
    },
  });
  expect(response.ok()).toBeTruthy();
}

test('a host says whether its registry mirror is on, and what is wrong with it, without alarm', async ({
  page,
}) => {
  // Each change waits for the controller's derived pass, which publishes a host's
  // view at most every few seconds, so the whole walk is longer than a click-test.
  test.setTimeout(180_000);
  const name = `mirror-host-${Date.now()}`;
  const credentials = await enrol(page, name);
  await beat(page, credentials);
  await goto(page, '/hosts', 'Hosts');
  const card = page.getByRole('article', { name, exact: true });
  await openHostCard(card);
  const line = card.getByTestId('host-registry-mirror');

  // Most hosts will never have a mirror, and a card that said "off" on every one of
  // them would be a line nobody reads.
  await expect(line).toHaveCount(0);

  // The derived pass publishes a host when its view changes, and paces itself, so
  // the card is given longer than a click would be.
  const live = { timeout: 30_000 };
  await beat(page, credentials, { requests: 12, hits: 9 });
  await expect(line).toContainText('On', live);
  await expect(line).toHaveAttribute('data-state', 'ok');
  // The figures are the metrics', and never on the card.
  await expect(line).not.toContainText('12');
  await expect(card.getByRole('heading', { name: 'Registry mirror' })).toBeVisible();

  // A problem is a warning, and says first that nothing is failing.
  await beat(page, credentials, { problem: 'listener' });
  await expect(line).toHaveAttribute('data-state', 'problem', live);
  await expect(line).toContainText('Not working');
  await expect(line).toContainText('Nothing fails');
  await expect(line).toContainText('Docker Hub');
  await expect(line).toContainText('listen');

  // What an agent sends is never shown as it was written: a word the controller has
  // never heard is shown as the general sentence, and nothing of the original.
  await beat(page, credentials, { problem: '<img src=x onerror=alert(1)>' });
  await expect(line).toContainText('no description for', live);
  await expect(line).not.toContainText('onerror');
  expect(await line.locator('img').count()).toBe(0);

  // A host whose own sidecar image is custom is told which pools skip the mirror.
  await beat(page, credentials, { host_image_custom: true });
  await expect(line).toHaveAttribute('data-state', 'ok', live);
  await expect(line).toContainText('agent.dind_image');

  // The mirror being turned off is a heartbeat that says nothing about one.
  await beat(page, credentials);
  await expect(line).toHaveCount(0, live);
});
