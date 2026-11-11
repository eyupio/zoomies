/**
 * The assistant's button sits in the window's bottom-right corner, fixed, and
 * nothing can be pressed through it. So the things an operator has to press or
 * read end before it: a wizard's Next, an editor's Save, and the last line of
 * the section above the fold on the Overview.
 *
 * The lane is a token (`--z-eli-lane`) rather than a number written into each
 * of them, and this is the test that holds the three to it. It measures boxes
 * rather than reading CSS, because the failure it guards is geometric: the
 * classes are all correct and the button still sits on top.
 */
import { expect, test, type Locator, type Page } from '@playwright/test';
import { busyFleet } from './support/command';
import { goto } from './support/fixtures';

/** The launcher, wherever the page is. */
const launcher = (page: Page): Locator => page.getByRole('button', { name: 'Ask Eli' });

/** Fail when the two boxes share any area at all. */
async function expectClear(page: Page, target: Locator, what: string): Promise<void> {
  const button = launcher(page);
  await expect(button).toBeVisible();
  await target.scrollIntoViewIfNeeded();
  await expect(target).toBeVisible();
  const a = (await button.boundingBox())!;
  const b = (await target.boundingBox())!;
  const over =
    a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;
  expect(
    over,
    `${what} is at ${JSON.stringify(b)} and the assistant's button is at ${JSON.stringify(a)}`,
  ).toBe(false);
}

test('the wizard’s Next is not under the assistant’s button', async ({ page }) => {
  await goto(page, '/migrate', 'Migrate repositories');
  await expectClear(page, page.getByRole('button', { name: 'Next', exact: true }), 'Next');
});

test('the Overview’s figures footnote is not under the assistant’s button', async ({ page }) => {
  await goto(page, '/', 'Overview');
  // The sentence under the band, which ends at the window's right edge exactly
  // where the button is.
  await expectClear(page, page.getByText(/^Waits and outcomes cover/), 'the footnote');
});

test('a pool’s Save is not under the assistant’s button', async ({ page }) => {
  await goto(page, '/pools/pool_demolinux?edit=1', 'zoomies-demo-linux-x64');
  const save = page.getByRole('button', { name: /^Save/ }).first();
  if (await save.isVisible().catch(() => false)) {
    await expectClear(page, save, 'Save');
  }
});

test('the command centre’s last rail row and the ticker’s Pause are not under the assistant’s button', async ({
  page,
}) => {
  // The command centre is its own chrome, so the shell's page padding is not
  // there to keep its corner clear: the page reserves the lane itself while
  // the button is drawn, which is whenever the page is not full screen. A
  // short window, so the rail has to scroll and its end meets the corner.
  if (!test.info().project.use.isMobile) await page.setViewportSize({ width: 1280, height: 720 });
  await busyFleet(page, { hosts: 5, slots: 11, waiting: 12 });
  await goto(page, '/command', 'Command centre');
  const finished = page.getByRole('region', { name: /^Just finished/ });
  await expect(finished).toHaveAttribute('aria-busy', 'false');

  // The rail's end, scrolled to: every container it scrolls in, and the
  // document, as far as they go.
  await finished.evaluate((el) => {
    for (let node: Element | null = el; node; node = node.parentElement) {
      if (node.scrollHeight > node.clientHeight) node.scrollTop = node.scrollHeight;
    }
  });
  // The last thing in the rail: a row, the "and N more" link or the line
  // that says nothing finished. CSS, because they share no role.
  const last = finished.locator(':scope > ul > li, :scope > a, :scope > button, :scope > p').last();
  await expectClear(page, last, 'the last line of the rail');

  const pause = page.getByRole('region', { name: 'Recent events' }).getByRole('button', {
    name: 'Pause',
  });
  await expectClear(page, pause, 'the ticker’s Pause');
});
