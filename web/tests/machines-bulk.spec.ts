/**
 * Clearing out a pile of failed machines, from the page that lists them.
 *
 * A wrong template or a wrong address fills a fleet with machines that never
 * became hosts, and deleting them one page at a time is how that becomes a
 * chore nobody does. The list therefore lets an administrator tick several and
 * delete them together, with a confirmation that is as heavy as what it does:
 * a click when nothing is destroyed, the count typed when a VM is.
 *
 * The demo fleet has two healthy machines and must keep them for the specs
 * that read them, and a delete is the one act that cannot be put back. So the
 * list is given extra failed machines in the response, and the bulk request is
 * answered in the test. What is under test is the page: who can be ticked, what
 * the dialog says, what is sent, and where the controller's reasons land. The
 * controller's own rules are the Go tests', which run the real route.
 */
import { expect, test, type Page, type Route } from '@playwright/test';
import { FIXTURE, goto, readThrough } from './support/fixtures';

interface Row {
  id: string;
  name: string;
  state: string;
  resource_zone?: string;
  resource_id?: string;
}

const NO_VM_A: Row = { id: 'mach_bulk_a', name: 'zoomies-mach-bulk-a', state: 'failed' };
const NO_VM_B: Row = { id: 'mach_bulk_b', name: 'zoomies-mach-bulk-b', state: 'failed' };
const WITH_VM: Row = {
  id: 'mach_bulk_c',
  name: 'zoomies-mach-bulk-c',
  state: 'failed',
  resource_zone: 'pve-1',
  resource_id: '143',
};
const DOUBTFUL: Row = {
  id: 'mach_bulk_d',
  name: 'zoomies-mach-bulk-d',
  state: 'quarantined',
  resource_zone: 'pve-1',
  resource_id: '144',
};

/** A machine as the API renders it, with only what the list reads filled in. */
function view(row: Row) {
  return {
    ...row,
    provider_id: 'prv_demoproxmox',
    provider_name: 'demo-pve',
    labels: {},
    timeline: [],
    safe_to_delete: false,
    created_at: '2026-09-14T11:00:00Z',
    updated_at: '2026-09-14T11:30:00Z',
  };
}

interface Asked {
  action?: string;
  ids?: string[];
  force?: boolean;
}

/**
 * Add the failed machines to the list, and answer the bulk route.
 *
 * `refuse` maps a machine id to the sentence the controller would give for it;
 * every other machine is taken. The requests are collected so the test can say
 * exactly what the page sent.
 */
async function arrange(page: Page, refuse: Record<string, string> = {}): Promise<Asked[]> {
  const asked: Asked[] = [];
  await page.route(/\/api\/v1\/machines(?:\?.*)?$/, async (route: Route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const read = await readThrough(route);
    if (!read) return;
    const { body } = read;
    body.items = [...body.items, ...[NO_VM_A, NO_VM_B, WITH_VM, DOUBTFUL].map(view)];
    return route.fulfill({ json: body });
  });
  await page.route(/\/api\/v1\/machines\/bulk$/, async (route: Route) => {
    const body = route.request().postDataJSON() as Asked;
    asked.push(body);
    await route.fulfill({
      json: {
        results: (body.ids ?? []).map((id) =>
          refuse[id] ? { id, ok: false, error: refuse[id] } : { id, ok: true },
        ),
      },
    });
  });
  return asked;
}

const tick = (page: Page, row: Row) => page.getByRole('checkbox', { name: `Select ${row.name}` });
const bar = (page: Page) => page.getByRole('group', { name: 'Delete machines' });

test('a machine the controller would only refuse cannot be ticked, and says why', async ({
  page,
}) => {
  await arrange(page);
  await goto(page, '/providers', 'Providers');

  await expect(tick(page, NO_VM_A)).toBeEnabled();
  const quarantined = page.getByRole('checkbox', {
    name: new RegExp(`${DOUBTFUL.name} cannot be selected\\. Quarantined`),
  });
  // Refused rather than disabled, so that it can be hovered and focused and the
  // reason reached; ticking it does nothing.
  await expect(quarantined).toHaveAttribute('aria-disabled', 'true');
  // The reason is where the checkbox would have been, not a surprise afterwards.
  await quarantined.hover();
  await expect(page.locator('.bubble:popover-open')).toContainText('Open it to release the row');
  // And ticking it does nothing. A press puts the bubble away, so this comes second.
  await quarantined.click({ force: true });
  await expect(quarantined).not.toBeChecked();

  // Nothing is ticked until somebody ticks it.
  await expect(bar(page)).toContainText('Tick machines to delete several at once');
});

test('failed machines with nothing behind them are cleared with a click, and the one refused stays ticked with its reason', async ({
  page,
}) => {
  const reason = `machine ${NO_VM_B.name} is still running 1 runner, and deleting it destroys the VM they are running on. Drain it first, or force the delete to lose that work.`;
  const asked = await arrange(page, { [NO_VM_B.id]: reason });
  await goto(page, '/providers', 'Providers');

  await tick(page, NO_VM_A).check();
  await tick(page, NO_VM_B).check();
  await expect(bar(page)).toContainText('2 machines selected');
  await bar(page).getByRole('button', { name: 'Delete', exact: true }).click();

  const dialog = page.getByRole('dialog', { name: 'Delete 2 machines' });
  await expect(dialog).toContainText('never got as far as a VM');
  await expect(dialog).toContainText('Nothing at the provider is touched');
  // Nothing is destroyed, so nothing is typed.
  await expect(dialog.getByLabel(/^Type .* to confirm/)).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Delete 2 machines' }).click();

  expect(asked).toEqual([{ action: 'delete', ids: [NO_VM_A.id, NO_VM_B.id], force: false }]);

  // One went and one did not, said in the tone that is announced as a problem.
  await expect(page.locator('.toast[data-tone="warning"]')).toContainText(
    'Deleting 1 machine, 1 refused',
  );
  // The refused row says why and is still ticked, so the operator can read it
  // and decide whether to insist. The machine that went is no longer ticked.
  await expect(page.getByText(reason)).toBeVisible();
  await expect(tick(page, NO_VM_B)).toBeChecked();
  await expect(tick(page, NO_VM_A)).not.toBeChecked();
  await expect(bar(page)).toContainText('1 machine selected');
});

test('a machine with a VM is deleted only once the count is typed, and the box is what insists', async ({
  page,
}) => {
  const asked = await arrange(page);
  await goto(page, '/providers', 'Providers');

  await tick(page, NO_VM_A).check();
  await tick(page, WITH_VM).check();
  await bar(page).getByRole('button', { name: 'Delete', exact: true }).click();

  const dialog = page.getByRole('dialog', { name: 'Delete 2 machines' });
  await expect(dialog).toContainText('1 VM is destroyed at the provider');
  await expect(dialog).toContainText('The other one never got as far as a VM');
  const confirm = dialog.getByRole('button', { name: 'Delete 2 machines' });
  await expect(confirm).toBeDisabled();

  // The count of machines, not of VMs: what is typed is the size of the act.
  await dialog.getByLabel('Type 2 to confirm').fill('1');
  await expect(confirm).toBeDisabled();
  await dialog.getByLabel('Type 2 to confirm').fill('2');
  await expect(confirm).toBeEnabled();

  // Insisting is a box in the dialog, off until it is ticked.
  await expect(dialog).toContainText('A machine that still has runners on it is skipped');
  await dialog
    .getByRole('checkbox', { name: 'Also delete machines that still have runners on them' })
    .check();
  await expect(dialog).toContainText('Runners on them stop');
  await confirm.click();

  expect(asked).toEqual([{ action: 'delete', ids: [NO_VM_A.id, WITH_VM.id], force: true }]);
  await expect(page.locator('.toast[data-tone="success"]')).toContainText('Deleting 2 machines');
  await expect(bar(page)).toContainText('Tick machines to delete several at once');
});

test('the box is off again the next time the dialog opens', async ({ page }) => {
  const asked = await arrange(page);
  await goto(page, '/providers', 'Providers');

  await tick(page, NO_VM_A).check();
  await bar(page).getByRole('button', { name: 'Delete', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Delete 1 machine' });
  const insist = dialog.getByRole('checkbox', {
    name: 'Also delete machines that still have runners on them',
  });
  await insist.check();
  await dialog.getByRole('button', { name: 'Cancel' }).click();

  await bar(page).getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(insist).not.toBeChecked();
  await dialog.getByRole('button', { name: 'Delete 1 machine' }).click();
  expect(asked).toEqual([{ action: 'delete', ids: [NO_VM_A.id], force: false }]);
});

test('select all ticks every machine that can be deleted and leaves the rest alone', async ({
  page,
}) => {
  await arrange(page);
  await goto(page, '/providers', 'Providers');

  await bar(page)
    .getByRole('checkbox', { name: /^Select all \d+ machines? shown/ })
    .check();
  await expect(tick(page, NO_VM_A)).toBeChecked();
  await expect(tick(page, WITH_VM)).toBeChecked();
  await expect(
    page.getByRole('checkbox', { name: new RegExp(`${DOUBTFUL.name} cannot be selected`) }),
  ).not.toBeChecked();

  await bar(page).getByRole('button', { name: 'Clear' }).click();
  await expect(tick(page, NO_VM_A)).not.toBeChecked();
});

test('the same list is on a provider’s own page, with the same boxes', async ({ page }) => {
  await arrange(page);
  await goto(page, '/providers/prv_demoproxmox', FIXTURE.provider);

  await expect(tick(page, NO_VM_A)).toBeEnabled();
  await tick(page, NO_VM_A).check();
  await expect(bar(page)).toContainText('1 machine selected');
});
