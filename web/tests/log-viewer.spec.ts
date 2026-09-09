/**
 * The runner log pane.
 *
 * Two failures live here and neither shows up in a unit test, because both are
 * geometry: the terminal standing proud of the pane that clips it -- so the
 * newest lines are drawn underneath the panel's bottom edge, which on a log
 * that is still being written is the only part anybody wants -- and xterm's own
 * layers climbing over the jump button, which then looks like a button and
 * ignores every click.
 *
 * Neither needs output to reproduce, so these run against the fixture runner
 * with nothing streaming.
 */
import { expect, test, type Page } from '@playwright/test';
import { browserOverride, FIXTURE, goto } from './support/fixtures';

test.use(browserOverride);

const RUNNER = `/runners/${FIXTURE.busyRunnerId}`;

/** How far the terminal hangs below the pane. Anything above zero is clipped. */
async function overhang(page: Page): Promise<number> {
  return page.evaluate(() => {
    const term = document.querySelector('.xterm');
    const pane = document.querySelector('[aria-label^="Log output"]');
    if (!term || !pane) return Number.NaN;
    return term.getBoundingClientRect().bottom - pane.getBoundingClientRect().bottom;
  });
}

async function openLog(page: Page): Promise<void> {
  await goto(page, RUNNER, FIXTURE.busyRunner);
  await expect(page.locator('.xterm')).toBeVisible();
  // The mono web font arrives after the first fit and makes the cells taller,
  // which is the moment the terminal used to outgrow its pane.
  await page.evaluate(() => document.fonts.ready);
  await expect.poll(() => overhang(page)).toBeLessThanOrEqual(0);
}

test('the terminal is fitted inside the pane, so the newest line is on screen', async ({
  page,
}) => {
  await openLog(page);

  // A resize is the other moment the fit can go stale.
  const size = page.viewportSize();
  if (size)
    await page.setViewportSize({ width: size.width, height: Math.round(size.height * 0.7) });
  await expect.poll(() => overhang(page)).toBeLessThanOrEqual(0);
});

test('the jump button can be clicked rather than only seen', async ({ page }) => {
  await openLog(page);

  // Turning follow off is what offers the button, output or none.
  const follow = page.getByRole('switch', { name: 'Follow' });
  await expect(follow).toHaveAttribute('aria-checked', 'true');
  await follow.click();

  // `click` is the assertion: Playwright refuses to click through whatever is
  // painted on top, which is exactly the bug.
  const jump = page.getByRole('button', { name: /Jump to latest/ });
  await expect(jump).toBeVisible();
  await jump.click();
  await expect(jump).toBeHidden();
  await expect(follow).toHaveAttribute('aria-checked', 'true');
});
