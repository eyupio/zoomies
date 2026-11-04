/**
 * The machine the controller itself runs on, on the Hosts page.
 *
 * A controller whose agents are all elsewhere has no host card of its own, so the
 * machine it runs on had nowhere on the page. The controller samples it and says
 * so at GET /controller/machine and on the stream; the page draws a card for it
 * only when none of the hosts is that machine already (an embedded agent's host
 * is, and its card says all of this).
 *
 * The real controller samples its own machine, so the route is proved against the
 * real binary. The page is shown a machine and a fleet it did not seed, with the
 * host list rewritten and the stream replaced, because the shared fleet has an
 * embedded host and a sample whose figures change from one run to the next.
 */
import { expect, test, type Page } from '@playwright/test';
import { goto, readThrough } from './support/fixtures';

const MACHINE = {
  cpus: 4,
  memory_mb: 8192,
  usage_fresh: true,
  usage: {
    cpu_percent: 68,
    steal_percent: 36,
    load_average_1m: 2.5,
    memory_available_mb: 5120,
    sampled_at: new Date().toISOString(),
  },
};

/** Show the page a fleet with or without an embedded host, and this machine. */
async function showMachine(page: Page, embedded: boolean, machine: object = MACHINE) {
  await page.route(/\/api\/v1\/hosts(?:\?.*)?$/, async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    const { body } = read;
    body.items = (body.items as Record<string, unknown>[]).map((h) => ({ ...h, embedded }));
    await route.fulfill({ json: body });
  });
  await page.route('**/api/v1/controller/machine', (route) => route.fulfill({ json: machine }));
  await page.route('**/api/v1/events*', (route) =>
    route.fulfill({
      status: 200,
      headers: { 'content-type': 'text/event-stream', 'cache-control': 'no-store' },
      body: '',
    }),
  );
}

test('a controller with no agent of its own shows the machine it runs on, with what steal did to it', async ({
  page,
}) => {
  await showMachine(page, false);
  await goto(page, '/hosts', 'Hosts');
  const card = page.getByRole('region', { name: 'This machine' });
  await expect(card).toBeVisible();
  await expect(card).toContainText('CPU busy');
  await expect(card).toContainText('68%');
  // Steal is what makes 68% busy a starved machine and not a busy one.
  await expect(card).toContainText('Steal');
  await expect(card).toContainText('36%');
  await expect(card).toContainText('Kept from this virtual machine by its hypervisor');
  await expect(card).toContainText('2.5');
  await expect(card).toContainText('4 CPUs');
});

// A machine at 70% with the controller at 0.1% is busy with something else, which
// is the answer the figure exists to give, so a small share is said and not left
// out for being low. It sits beside the busy figure it is a part of.
test("the card says how much of the busy figure is the controller's own", async ({ page }) => {
  await showMachine(page, false, {
    ...MACHINE,
    usage: { ...MACHINE.usage, cpu_percent: 70, process_cpu_percent: 0.1 },
  });
  await goto(page, '/hosts', 'Hosts');
  const card = page.getByRole('region', { name: 'This machine' });
  await expect(card).toBeVisible();
  await expect(card).toContainText('Controller');
  await expect(card).toContainText('0.1%');
  await expect(card).toContainText("Of the machine's CPU, used by the controller process itself");
  // Still beside the busy figure and the steal it is read against.
  await expect(card).toContainText('CPU busy');
  await expect(card).toContainText('Steal');
});

// A controller too old to send the share leaves the figure out, and the rest of the
// card stands: a missing measurement is not drawn as zero.
test('a controller that does not send its own share draws the card without it', async ({
  page,
}) => {
  await showMachine(page, false);
  await goto(page, '/hosts', 'Hosts');
  const card = page.getByRole('region', { name: 'This machine' });
  await expect(card).toContainText('CPU busy');
  await expect(card.getByText('Controller', { exact: true })).toHaveCount(0);
});

test('a controller with an agent of its own does not draw its machine twice', async ({ page }) => {
  await showMachine(page, true);
  await goto(page, '/hosts', 'Hosts');
  await expect(page.getByRole('heading', { name: 'Hosts', level: 1 })).toBeVisible();
  await expect(page.getByRole('region', { name: 'This machine' })).toHaveCount(0);
});

test('a controller that has stopped sampling says its figures are the last it took', async ({
  page,
}) => {
  await showMachine(page, false, { ...MACHINE, usage_fresh: false });
  await goto(page, '/hosts', 'Hosts');
  await expect(page.getByRole('region', { name: 'This machine' })).toContainText(
    'has stopped sampling',
  );
});

// The real controller samples its own machine on its housekeeping tick, so after
// two ticks its route has figures. This is the one place the sampler meets a real
// procfs in a real container, and the route is what a pull from outside reads.
test('the real controller reports its own machine once it has two samples', async ({
  page,
  isMobile,
}) => {
  // The route does not depend on the device, and the wait is a minute of the
  // controller's housekeeping tick: once is enough.
  test.skip(isMobile, 'the route is the same on a phone');
  test.setTimeout(150_000);
  await expect
    .poll(
      async () => {
        const res = await page.request.get('/api/v1/controller/machine');
        const body = (await res.json()) as typeof MACHINE;
        return body.usage?.cpu_percent !== undefined && body.usage_fresh === true;
      },
      { timeout: 120_000, intervals: [2_000, 5_000] },
    )
    .toBe(true);
  const res = await page.request.get('/api/v1/controller/machine');
  const body = (await res.json()) as typeof MACHINE;
  expect(body.cpus).toBeGreaterThan(0);
  expect(body.memory_mb).toBeGreaterThan(0);
  expect(body.usage.cpu_percent).toBeGreaterThanOrEqual(0);
  expect(body.usage.cpu_percent).toBeLessThanOrEqual(100);
  // The controller's own share, taken over the same interval, is part of the
  // machine's busy time and so cannot be more than it (the controller allows a
  // couple of points for the two being read a moment apart).
  const own = (body.usage as { process_cpu_percent?: number }).process_cpu_percent;
  expect(typeof own).toBe('number');
  expect(own).toBeGreaterThanOrEqual(0);
  expect(own).toBeLessThanOrEqual(body.usage.cpu_percent + 2);
});
