import { expect, test } from '@playwright/test';
import { browserOverride, goto, openSection } from './support/fixtures';

test.use(browserOverride);

for (const theme of ['light', 'dark'] as const) {
  test(`tooltips escape clipped table cells and overlapping elements in ${theme} theme`, async ({
    page,
  }) => {
    await page.addInitScript((choice) => localStorage.setItem('zoomies.theme', choice), theme);
    await goto(page, '/pools', 'Pools');
    const trigger = page.locator('.tip-wrap').filter({ hasText: '2 risks' });
    await trigger.scrollIntoViewIfNeeded();
    // A clipping, transformed ancestor reproduces both causes of the old
    // absolute-positioned tooltip disappearing, without changing the fleet.
    await trigger.evaluate((node) => {
      node.style.overflow = 'hidden';
      node.style.transform = 'translateZ(0)';
    });
    await trigger.hover();
    const tip = trigger.locator('.bubble');
    await expect(tip).toBeVisible();
    await expect.poll(() => tip.evaluate((node) => node.matches(':popover-open'))).toBe(true);
    const bounds = await tip.boundingBox();
    const viewport = page.viewportSize()!;
    expect(bounds!.x).toBeGreaterThanOrEqual(0);
    expect(bounds!.y).toBeGreaterThanOrEqual(0);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(viewport.width);
    expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(viewport.height);
    // Temporarily allow hit testing of the visual tooltip. Even an element
    // with the highest ordinary z-index must be underneath its top layer.
    expect(
      await tip.evaluate((node) => {
        const cover = document.createElement('div');
        cover.style.cssText = 'position:fixed;inset:0;z-index:2147483647';
        document.body.append(cover);
        node.style.pointerEvents = 'auto';
        const rect = node.getBoundingClientRect();
        const top = document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2);
        node.style.removeProperty('pointer-events');
        cover.remove();
        return top === node;
      }),
    ).toBe(true);
    await page.keyboard.press('Escape');
    await expect(tip).toHaveCount(0);
  });
}

test('an open tooltip stays attached when its table scrolls and disappears on navigation', async ({
  page,
}) => {
  await goto(page, '/pools', 'Pools');
  const trigger = page.locator('.tip-wrap').filter({ hasText: '2 risks' });
  // Establish the constrained table before measuring: changing its height
  // afterwards can move the anchor and flip the tooltip's placement.
  await trigger.evaluate((node) => {
    (node.closest('.scroll') as HTMLElement).style.maxHeight = '110px';
  });
  await trigger.scrollIntoViewIfNeeded();
  // Focus keeps the tooltip open while the pointer is outside the table.
  await trigger.evaluate((node) => {
    node.tabIndex = 0;
    node.focus();
  });
  const tip = trigger.locator('.bubble');
  await expect(tip).toBeVisible();
  const before = await tip.boundingBox();
  const delta = await trigger.evaluate((node) => {
    const scroll = node.closest('.scroll') as HTMLElement;
    const before = scroll.scrollTop;
    scroll.scrollTop += 10;
    return scroll.scrollTop - before;
  });
  expect(delta).toBeGreaterThan(0);
  await expect.poll(async () => (await tip.boundingBox())!.y).toBeCloseTo(before!.y - delta, 0);
  await openSection(page, '/usage');
  await expect(page.locator('.bubble:popover-open')).toHaveCount(0);
});
