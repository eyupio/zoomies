import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import { goto, openFilters } from './support/fixtures';

type ProvisioningRow = {
  id: string;
  repo: string;
  job_name: string;
  provisioning?: string;
  provision_now?: boolean;
};

/**
 * A ready job whose name appears once in the whole queue.
 *
 * The row's actions are named by repository and job name, so two "build"
 * jobs make a button name ambiguous. Picking a unique name is also what keeps
 * a bulk selection over the Paused view down to the one item this test paused.
 */
async function uniqueReady(request: APIRequestContext): Promise<ProvisioningRow> {
  const { items } = (await (
    await request.get('/api/v1/provisioning?limit=500&sort=queued_at&order=asc')
  ).json()) as { items: ProvisioningRow[] };
  const counts = new Map<string, number>();
  for (const row of items) counts.set(row.job_name, (counts.get(row.job_name) ?? 0) + 1);
  const job = items.find(
    (row) =>
      row.provisioning !== 'paused' &&
      row.provisioning !== 'deleted' &&
      row.provision_now !== true &&
      counts.get(row.job_name) === 1,
  );
  expect(job, 'the demo fleet has a ready job with a unique name').toBeTruthy();
  return job!;
}

/** A job's name is not a pattern: it carries slashes and dots of its own. */
function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

test('queue controls persist, filter and restore demand without changing GitHub state', async ({
  page,
  request,
}) => {
  // This test pauses a ready item. Asking for one is also what keeps a retry
  // from choosing the item a timed-out first attempt left paused.
  test.setTimeout(60_000);
  const job = await uniqueReady(request);
  try {
    await goto(page, '/queue', 'Queue');
    const errors: string[] = [];
    page.on('pageerror', (error) => errors.push(error.message));
    await expect(
      page.getByRole('button', { name: 'Select all matching', exact: true }),
    ).toBeEnabled();
    // The row's own actions are buttons, not a menu, so pausing one item is a
    // single press. Each carries what it acts on in its name, which is what
    // keeps it apart from the identically-worded button in the selection bar.
    await page
      .getByRole('button', { name: `Pause: ${job.repo} / ${job.job_name}`, exact: true })
      .first()
      .click();
    await page.getByRole('dialog').getByRole('button', { name: 'Pause', exact: true }).click();
    await expect(page.getByRole('status').filter({ hasText: '1 updated' })).toBeVisible();
    await page.reload();
    // The status filters live in the Queue band now (or its plain panel on a phone), as the
    // cards above the list used to.
    // The band's figures are the ones this clicks on a desktop. The plain
    // panel is their home on a phone, and the band is not drawn there.
    const paused = test.info().project.use.isMobile
      ? page.getByRole('list', { name: 'Key figures' }).getByRole('button', { name: /^Paused/ })
      : page.getByTestId('band-queue').getByRole('button', { name: /^Paused/ });
    await paused.click();
    await page.getByRole('searchbox', { name: 'Search jobs' }).fill(job.job_name);
    await expect(page).toHaveURL(/[?&]provisioning=paused(?:&|$)/);
    await expect(page.getByRole('grid', { name: 'Provisioning queue' })).toContainText(
      job.job_name,
    );
    await page.getByRole('button', { name: 'Select all matching', exact: true }).click();
    await expect(page.getByText('Snapshot across all pages')).toBeVisible();
    await page.getByRole('button', { name: 'Run now', exact: true }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Run now', exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
    // The action is applied by the controller after the dialog closes; reading
    // the job before the page's own report lands reads the state before it.
    await expect(page.getByRole('status').filter({ hasText: '1 updated' })).toBeVisible();
    // The page's report can land before the controller has written the new
    // demand, so read the job until it settles rather than once at the wrong
    // moment.
    await expect
      .poll(
        async () => {
          const updated = await (await request.get(`/api/v1/jobs/${job.id}`)).json();
          return {
            state: updated.state,
            provisioning: updated.provisioning,
            provision_now: updated.provision_now,
          };
        },
        { timeout: 10_000 },
      )
      .toEqual({ state: 'queued', provisioning: '', provision_now: true });
    expect(errors).toEqual([]);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
  } finally {
    // A timeout closes the request context with the page, and cleanup must not
    // replace the failure that caused it.
    await request
      .post('/api/v1/provisioning/bulk', { data: { ids: [job.id], action: 'resume' } })
      .catch(() => {});
  }
});

test('a start with a time of day narrows the queue to that minute', async ({ page, request }) => {
  // The queue the page lists by default, from the API, so the expected count is
  // worked out here and not by the helper under test.
  const { items } = (await request
    .get(
      '/api/v1/provisioning?limit=500&sort=queued_at&order=asc&provisioning=ready&provisioning=expedited&provisioning=paused',
    )
    .then((r) => r.json())) as { items: { queued_at: string }[] };

  // The minute of the newest job, so the cut falls inside the queue's span and
  // not at a day boundary: a page that rounded to the day would keep every job.
  const cut = new Date(items[items.length - 1]!.queued_at);
  cut.setSeconds(0, 0);
  const kept = items.filter((job) => Date.parse(job.queued_at) >= cut.getTime()).length;
  expect(kept, 'the cut has to fall inside the queue').toBeGreaterThan(0);
  expect(kept, 'the cut has to leave some out').toBeLessThan(items.length);

  const pad = (n: number) => String(n).padStart(2, '0');
  const local = `${cut.getFullYear()}-${pad(cut.getMonth() + 1)}-${pad(cut.getDate())}T${pad(cut.getHours())}:${pad(cut.getMinutes())}`;
  await goto(page, `/queue?since=${encodeURIComponent(local)}`, 'Queue');
  await openFilters(page);
  await expect(page.getByText('matching items')).toContainText(`${kept} matching items`);
  await expect(page.getByRole('group', { name: 'Queued between' }).getByLabel('From')).toHaveValue(
    local,
  );
});

test('queue views preserve combined filters and cancel leaves demand unchanged', async ({
  page,
}) => {
  await goto(page, '/queue?repo=acme%2Fapi&provisioning=ready', 'Queue');
  await page.getByRole('button', { name: 'Save view', exact: true }).click();
  await page.getByRole('textbox', { name: 'View name' }).fill('API ready');
  await page.getByRole('dialog').getByRole('button', { name: 'Save view', exact: true }).click();
  await page.getByRole('button', { name: 'Reset filters', exact: true }).first().click();
  await page
    .getByRole('combobox', { name: 'Saved queue views' })
    .selectOption({ label: 'API ready' });
  await expect(page).toHaveURL(/repo=acme%2Fapi/);
  await expect(page).toHaveURL(/provisioning=ready/);
  await expect(
    page.getByRole('button', { name: 'Select all matching', exact: true }),
  ).toBeEnabled();
  await page.getByRole('button', { name: 'Select all matching', exact: true }).click();
  await page.getByRole('button', { name: 'Delete from queue', exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByText('Snapshot across all pages')).toBeVisible();
});

/*
 * Every other form dialog is a labelled field with Cancel and the primary
 * action in its footer, so an operator who has used one knows how to leave and
 * how to submit this one. Enter still saves, because the field is a form's.
 */
test('the save-view dialog has a Cancel and still saves on Enter', async ({ page }) => {
  await goto(page, '/queue?repo=acme%2Fapi', 'Queue');
  await page.getByRole('button', { name: 'Save view', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Save queue view' });
  await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Save view', exact: true })).toBeDisabled();
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(dialog).toBeHidden();

  await page.getByRole('button', { name: 'Save view', exact: true }).click();
  await page.getByRole('textbox', { name: 'View name' }).fill('Enter saves');
  await page.getByRole('textbox', { name: 'View name' }).press('Enter');
  await expect(dialog).toBeHidden();
  await expect(
    page.getByRole('combobox', { name: 'Saved queue views' }).locator('option', {
      hasText: 'Enter saves',
    }),
  ).toHaveCount(1);
});

/*
 * The actions are on the row rather than behind a menu, which is four buttons
 * per row instead of one trigger. That is a hundred tab stops on a full page if
 * each is a stop of its own, so the four are one toolbar: Tab reaches it once
 * and the arrow keys move along it. The arrows skip what cannot be pressed,
 * because focus that lands on a disabled button is focus an operator has to get
 * out of by guessing.
 */
test("a row's actions are one stop on the keyboard, and the arrows move along them", async ({
  page,
}) => {
  const job = await uniqueReady(page.request);
  const subject = `${job.repo} / ${job.job_name}`;

  await goto(page, '/queue', 'Queue');
  const runNow = page.getByRole('button', { name: `Run now: ${subject}`, exact: true }).first();
  await expect(runNow).toBeVisible();

  // Reached the way the keyboard reaches it, rather than by being clicked: a
  // click would say nothing about the tab order.
  await runNow.focus();
  await expect(runNow).toBeFocused();

  await page.keyboard.press('ArrowRight');
  await expect(
    page.getByRole('button', { name: `Pause: ${subject}`, exact: true }).first(),
  ).toBeFocused();

  /*
   * Resume is already in force on a ready item, and the arrow lands on it all
   * the same. An action that cannot be taken is refused, not removed: it stays
   * focusable so that the reason it gives is reachable without a pointer, and
   * so that confirming an action which makes its own button unavailable does
   * not drop focus to the top of the document.
   */
  await page.keyboard.press('ArrowRight');
  const resume = page
    .getByRole('button', { name: new RegExp(`^Resume: ${escapeRegExp(subject)}\\.`) })
    .first();
  await expect(resume).toBeFocused();
  await expect(resume).toHaveAttribute('aria-disabled', 'true');
  // The reason is part of what the control is called, so it is announced with it.
  await expect(resume).toHaveAccessibleName(/Already provisioning normally/);

  // Pressing it does nothing rather than opening a confirmation for a no-op.
  await resume.press('Enter');
  await expect(page.getByRole('dialog')).toHaveCount(0);

  await page.keyboard.press('ArrowRight');
  await expect(
    page.getByRole('button', { name: `Delete from queue: ${subject}`, exact: true }).first(),
  ).toBeFocused();
});

/*
 * The top bar is sticky too, so a bar pinned at the top of the page slid up
 * behind it: the bulk actions were there, and could be neither seen nor pressed
 * until the operator scrolled back to the top of a long queue.
 */
test('the bulk-action bar stays below the top bar while a long queue scrolls', async ({ page }) => {
  // A short window, so the page overflows however few items the fleet holds.
  await page.setViewportSize({ width: 1280, height: 420 });
  await goto(page, '/queue', 'Queue');
  const select = page.getByRole('button', { name: 'Select all matching', exact: true });
  await expect(select).toBeEnabled();

  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  await expect
    .poll(() => page.evaluate(() => window.scrollY), { message: 'the page did not scroll' })
    .toBeGreaterThan(0);

  // The bar has no role or name of its own, so it is found by its class.
  const topBar = await page.getByRole('banner').boundingBox();
  const bar = await page.locator('.selection-bar').boundingBox();
  expect(topBar, 'the top bar has a box').not.toBeNull();
  expect(bar, 'the selection bar has a box').not.toBeNull();
  expect(bar!.y).toBeGreaterThanOrEqual(topBar!.y + topBar!.height);

  // Where it is drawn is not enough: the press has to reach the button.
  const button = await select.boundingBox();
  const reached = await page.evaluate(
    ({ x, y }) => document.elementFromPoint(x, y)?.closest('button')?.textContent?.trim(),
    { x: button!.x + button!.width / 2, y: button!.y + button!.height / 2 },
  );
  expect(reached).toBe('Select all matching');
});

/*
 * The list card. The Queue is band, card and one fold under it: the card has
 * the queue's tile, a count over its title, the branch field and the saved
 * views opposite, the four provisioning statuses as chips, and the grid bare
 * inside. The chips read and write the `provisioning` parameter the menu
 * they replace did.
 */
const statusChips = (page: Page) =>
  page.getByRole('group', { name: 'Filter by provisioning status' });
const queueGrid = (page: Page) => page.getByRole('grid', { name: 'Provisioning queue' });

test('the eyebrow says the active demand and what is on hold, as the counts report them', async ({
  page,
  request,
}) => {
  const { counts } = (await (await request.get('/api/v1/provisioning?limit=1')).json()) as {
    counts: Record<string, number>;
  };
  await goto(page, '/queue', 'Queue');
  const card = page.locator('section.list-card');
  await expect(card.getByRole('heading', { name: 'Provisioning queue' })).toBeVisible();
  await expect(card.locator('.eyebrow')).toHaveText(
    `${(counts.ready ?? 0) + (counts.expedited ?? 0)} active demand, ${counts.paused ?? 0} on hold`,
  );
  // The grid draws no frame of its own: the card is the one frame.
  await expect(card.locator('.grid.bare')).toHaveCount(1);
});

test('the chips start with Ready, Run now and Paused pressed, and Removed a press away', async ({
  page,
}) => {
  await goto(page, '/queue', 'Queue');
  const chips = statusChips(page);
  for (const name of ['Ready', 'Run now', 'Paused'])
    await expect(chips.getByRole('button', { name: new RegExp(`^${name}`) })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
  await expect(chips.getByRole('button', { name: /^Removed/ })).toHaveAttribute(
    'aria-pressed',
    'false',
  );
  await expect(chips.getByRole('button', { name: /^All/ })).toHaveAttribute(
    'aria-pressed',
    'false',
  );

  // Pressing the fourth is every status, which the page spells out in full
  // and the chips read back as All.
  await chips.getByRole('button', { name: /^Removed/ }).click();
  await expect(page).toHaveURL(/provisioning=deleted/);
  await expect(chips.getByRole('button', { name: /^All/ })).toHaveAttribute('aria-pressed', 'true');
  await expect(chips.getByRole('button', { name: /^Removed/ })).toHaveAttribute(
    'aria-pressed',
    'false',
  );

  // Pressing Paused from the default leaves Ready and Run now in the address.
  await page.getByRole('button', { name: 'Reset filters', exact: true }).first().click();
  await chips.getByRole('button', { name: /^Paused/ }).click();
  await expect(page).toHaveURL(/provisioning=ready&provisioning=expedited(?:&|#|$)/);
  await expect(page).not.toHaveURL(/provisioning=paused/);
  await expect(chips.getByRole('button', { name: /^Paused/ })).toHaveAttribute(
    'aria-pressed',
    'false',
  );
});

test('a band figure presses its chip alone, and the chips carry the counts', async ({
  page,
  request,
}) => {
  const { counts } = (await (await request.get('/api/v1/provisioning?limit=1')).json()) as {
    counts: Record<string, number>;
  };
  await goto(page, '/queue', 'Queue');
  const paused = test.info().project.use.isMobile
    ? page.getByRole('list', { name: 'Key figures' }).getByRole('button', { name: /^Paused/ })
    : page.getByTestId('band-queue').getByRole('button', { name: /^Paused/ });
  await paused.click();
  await expect(page).toHaveURL(/[?&]provisioning=paused(?:&|#|$)/);
  const chips = statusChips(page);
  await expect(chips.getByRole('button', { name: /^Paused/ })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await expect(chips.getByRole('button', { name: /^Ready/ })).toHaveAttribute(
    'aria-pressed',
    'false',
  );
  await expect(chips.getByRole('button', { name: /^Paused/ }).locator('.count')).toHaveText(
    String(counts.paused ?? 0),
  );
});

test('Labels is hidden by default and a click away in Columns', async ({ page }) => {
  await goto(page, '/queue', 'Queue');
  await expect(queueGrid(page).locator('tbody tr[data-row]').first()).toBeVisible();
  await expect(queueGrid(page).getByRole('columnheader', { name: /\bLabels\b/ })).toHaveCount(0);
  await expect(queueGrid(page).getByRole('columnheader', { name: /\bRepository\b/ })).toHaveCount(
    1,
  );
  await page.getByRole('button', { name: 'Columns' }).click();
  await page
    .getByRole('group', { name: 'Column layout' })
    .getByRole('checkbox', { name: 'Labels' })
    .check();
  await page.keyboard.press('Escape');
  await expect(queueGrid(page).getByRole('columnheader', { name: /\bLabels\b/ })).toBeVisible();
});
