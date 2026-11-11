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

/**
 * The rail's lines a person can see that the assistant's button sits on: each
 * line's box cut to the scroller it is in, so a line scrolled out of sight
 * does not count, then tested against the button's box.
 */
async function railLinesUnderTheButton(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    // The launcher is named by its label; there is one on the page.
    const launcher = document.querySelector('[aria-label="Ask Eli"]')!.getBoundingClientRect();
    const lines = document.querySelectorAll(
      '.rail section > ul > li, .rail section > a, .rail section > button, .rail section > p',
    );
    const under: string[] = [];
    for (const line of lines) {
      let { top, bottom, left, right } = line.getBoundingClientRect();
      for (let node = line.parentElement; node; node = node.parentElement) {
        if (getComputedStyle(node).overflowY === 'visible') continue;
        const clip = node.getBoundingClientRect();
        top = Math.max(top, clip.top);
        bottom = Math.min(bottom, clip.bottom);
        left = Math.max(left, clip.left);
        right = Math.min(right, clip.right);
      }
      bottom = Math.min(bottom, innerHeight);
      if (bottom <= top || right <= left) continue;
      const over =
        left < launcher.right &&
        launcher.left < right &&
        top < launcher.bottom &&
        launcher.top < bottom;
      if (over) under.push((line.textContent ?? '').trim().slice(0, 40));
    }
    return under;
  });
}

test('the command centre’s rail and the ticker’s Pause are not under the assistant’s button', async ({
  page,
}) => {
  // The command centre is its own chrome, so the shell's page padding is not
  // there to keep its corner clear: the page reserves the corner itself while
  // the button is drawn, which is whenever the page is not full screen. A
  // short window, so the rail is longer than its space and runs into the
  // corner.
  const phone = !!test.info().project.use.isMobile;
  if (!phone) await page.setViewportSize({ width: 1280, height: 720 });
  await busyFleet(page, { hosts: 5, slots: 11, waiting: 12 });
  await goto(page, '/command', 'Command centre');
  const finished = page.getByRole('region', { name: /^Just finished/ });
  await expect(finished).toHaveAttribute('aria-busy', 'false');
  await expect(launcher(page)).toBeVisible();

  // A wall display is looked at, not scrolled: at rest, no line of the rail
  // that can be seen is under the button. On a phone the page is a document
  // that scrolls under the button as every page does, so only its end counts.
  if (!phone) expect(await railLinesUnderTheButton(page), 'at rest').toEqual([]);

  // The rail's end, scrolled to: every container it scrolls in, and the
  // document, as far as they go.
  await finished.evaluate((el) => {
    for (let node: Element | null = el; node; node = node.parentElement) {
      if (node.scrollHeight > node.clientHeight) node.scrollTop = node.scrollHeight;
    }
  });
  expect(await railLinesUnderTheButton(page), 'scrolled to the end').toEqual([]);

  const pause = page.getByRole('region', { name: 'Recent events' }).getByRole('button', {
    name: 'Pause',
  });
  await expectClear(page, pause, 'the ticker’s Pause');
});
