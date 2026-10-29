import { expect, test, type Page } from '@playwright/test';
import {
  SECTIONS,
  goto,
  nav,
  navEntry,
  navMenu,
  openAccountMenu,
  openSection,
} from './support/fixtures';

for (const theme of ['light', 'dark'] as const) {
  test(`tooltips escape clipped table cells and overlapping elements in ${theme} theme`, async ({
    page,
  }) => {
    await page.addInitScript((choice) => localStorage.setItem('zoomies.theme', choice), theme);
    await goto(page, '/pools', 'Pools');
    const trigger = page.locator('.tip-wrap:not(.bare)').filter({ hasText: '2 risks' });
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
        // The content sits in an inner scroller, so the point lands on it, not on
        // the bubble itself: what matters is that it is the bubble's, and not the cover's.
        return top !== null && node.contains(top);
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
  const trigger = page.locator('.tip-wrap:not(.bare)').filter({ hasText: '2 risks' });
  // Establish the constrained table before measuring: changing its height
  // afterwards can move the anchor and flip the tooltip's placement.
  await trigger.evaluate((node) => {
    (node.closest('.scroll') as HTMLElement).style.maxHeight = '110px';
  });
  await trigger.scrollIntoViewIfNeeded();
  // And into the middle of the window: a bubble with no room under it is held
  // inside the window rather than following its cell, which is correct and is
  // not what this test is about.
  await trigger.evaluate((node) => {
    const rect = node.getBoundingClientRect();
    window.scrollTo(0, window.scrollY + rect.top - window.innerHeight / 2);
  });
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

/* -- the shared shell: timing, keyboard, geometry ------------------------------- */

/**
 * Starts recording, in the page, when the pointer or focus reached a trigger and
 * when each bubble appeared. A bubble that shows after 250ms and one that shows
 * at once differ by two hundred milliseconds, and Playwright's polling cannot
 * tell them apart from a loaded runner; the page's own clock can.
 */
async function watchBubbles(page: Page): Promise<void> {
  await page.evaluate(() => {
    const w = window as unknown as {
      __reached: Array<{ at: number; target: string }>;
      __shown: Array<{ at: number; text: string }>;
      __most: number;
    };
    w.__reached = [];
    w.__shown = [];
    w.__most = 0;
    const reached = (event: Event) => {
      const el = event.target as Element | null;
      const trigger = el?.closest('a[href], button, [role="gridcell"]');
      w.__reached.push({
        at: performance.now(),
        target: trigger?.getAttribute('href') ?? trigger?.getAttribute('aria-label') ?? '',
      });
    };
    document.addEventListener('pointerover', reached, true);
    document.addEventListener('focusin', reached, true);
    new MutationObserver(() => {
      const bubbles = document.querySelectorAll('.bubble');
      w.__most = Math.max(w.__most, bubbles.length);
      const text = bubbles[0]?.textContent ?? '';
      const last = w.__shown[w.__shown.length - 1];
      if (text && last?.text !== text) w.__shown.push({ at: performance.now(), text });
    }).observe(document.body, { subtree: true, childList: true, characterData: true });
  });
}

/** Milliseconds from the last time something was reached to a bubble containing `text`. */
async function waited(page: Page, text: string): Promise<number> {
  await expect(page.locator('.bubble', { hasText: text })).toBeVisible();
  return page.evaluate((needle) => {
    const w = window as unknown as {
      __reached: Array<{ at: number }>;
      __shown: Array<{ at: number; text: string }>;
    };
    const shown = w.__shown.find((s) => s.text.includes(needle));
    const before = w.__reached.filter((r) => r.at <= (shown?.at ?? 0));
    return (shown?.at ?? 0) - (before[before.length - 1]?.at ?? 0);
  }, text);
}

test.describe('the shared tooltip', () => {
  test.skip(
    ({ isMobile }) => !!isMobile,
    'hover help is for a pointer; a phone reads the description',
  );

  test('the first bubble waits, and a sweep down the sidebar after it is instant', async ({
    page,
  }) => {
    await goto(page, '/', 'Overview');
    await watchBubbles(page);

    await navEntry(page, '/pools').hover();
    const first = await waited(page, 'What labels your runners answer to');
    expect(first, 'the first one waits out its 250ms').toBeGreaterThanOrEqual(230);

    // Moving to the neighbour while one is open costs nothing: this is what makes
    // a sweep down the rail feel instant.
    await navEntry(page, '/runners').hover();
    const next = await waited(page, 'Every runner the controller knows about');
    expect(next, 'a neighbour opens at once').toBeLessThan(100);
    await expect(page.locator('.bubble')).toHaveCount(1);
    expect(
      await page.evaluate(() => (window as unknown as { __most: number }).__most),
      'never two at a time',
    ).toBe(1);
  });

  test('a bubble says the page, its sentence and the keys that go there', async ({ page }) => {
    await goto(page, '/', 'Overview');
    await navEntry(page, '/pools').hover();
    const bubble = page.locator('.bubble:popover-open');
    await expect(bubble).toContainText('What labels your runners answer to');
    // Expanded, the entry shows its own name, so the bubble has no heading; it
    // does show the keys, as caps.
    await expect(bubble.locator('kbd')).toHaveText(['g', 'p']);
    await expect(bubble.locator('.title')).toHaveCount(0);
    // The sentence is also the link's description, so nothing lives only here.
    await expect(navEntry(page, '/pools')).toHaveAccessibleDescription(
      /What labels your runners answer to/,
    );
  });

  test('collapsed, the entry is named in the bubble, because the rail shows only an icon', async ({
    page,
  }) => {
    await goto(page, '/', 'Overview');
    await page.getByRole('button', { name: 'Collapse the navigation' }).click();
    await navEntry(page, '/pools').hover();
    const bubble = page.locator('.bubble:popover-open');
    await expect(bubble.locator('.title')).toHaveText('Pools');
    await expect(bubble).toContainText('What labels your runners answer to');
    // The bubble clears the rail rather than the entry, which is inset in it.
    const rail = await page.locator('.nav').boundingBox();
    const box = await bubble.boundingBox();
    expect(box!.x).toBeGreaterThanOrEqual(rail!.x + rail!.width);
  });

  test('keyboard focus opens it at once and blur takes it away', async ({ page }) => {
    await goto(page, '/', 'Overview');
    await watchBubbles(page);
    const link = navEntry(page, '/queue');
    await link.focus();
    expect(await waited(page, 'Shape runner demand'), 'focus does not wait').toBeLessThan(100);
    await link.blur();
    await expect(page.locator('.bubble')).toHaveCount(0);
  });

  test('a touch does not open it, and the sentence is still the description', async ({ page }) => {
    await goto(page, '/', 'Overview');
    await navEntry(page, '/pools').evaluate((a) => {
      const wrapper = a.parentElement!;
      wrapper.dispatchEvent(new PointerEvent('pointerenter', { pointerType: 'touch' }));
      a.dispatchEvent(new PointerEvent('pointerenter', { pointerType: 'touch' }));
    });
    await page.waitForTimeout(500);
    await expect(page.locator('.bubble')).toHaveCount(0);
    await expect(navEntry(page, '/pools')).toHaveAccessibleDescription(/What labels your runners/);
  });

  test('the pointer can cross onto the bubble, and leaving it closes it after a grace', async ({
    page,
  }) => {
    await goto(page, '/', 'Overview');
    await page.getByRole('button', { name: 'Collapse the navigation' }).click();
    await navEntry(page, '/hosts').hover();
    const bubble = page.locator('.bubble:popover-open');
    await expect(bubble).toBeVisible();
    const box = (await bubble.boundingBox())!;
    // Onto the bubble itself, over the gap between it and the trigger.
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2, { steps: 6 });
    await page.waitForTimeout(400);
    await expect(bubble, 'resting on the bubble keeps it').toBeVisible();
    await page.mouse.move(box.x + box.width + 200, box.y + box.height + 120, { steps: 4 });
    await expect(page.locator('.bubble')).toHaveCount(0);
  });

  test('a press on the trigger puts the bubble away', async ({ page }) => {
    await goto(page, '/', 'Overview');
    const toggle = page.getByRole('button', { name: 'Collapse the navigation' });
    await toggle.hover();
    await expect(page.locator('.bubble:popover-open')).toBeVisible();
    await page.mouse.down();
    await expect(page.locator('.bubble')).toHaveCount(0);
    await page.mouse.up();
  });

  test('a press inside the wait means the bubble never comes', async ({ page }) => {
    // A click made before the 250ms is over must not be answered by help that
    // arrives late, over the thing the click has just opened.
    await goto(page, '/', 'Overview');
    const toggle = page.getByRole('button', { name: 'Collapse the navigation' });
    await toggle.hover();
    await page.mouse.down();
    await page.mouse.up();
    await page.waitForTimeout(600);
    await expect(page.locator('.bubble')).toHaveCount(0);
  });

  test('a sheet that rises under a resting pointer opens no bubble until the pointer moves', async ({
    page,
  }) => {
    // The phone's menu covers the place the pointer pressed "More" from, so the
    // browser reports the pointer entering whatever the sheet puts there, with no
    // move behind it. Help that opened on that would take the first Escape that
    // belongs to the sheet.
    await page.setViewportSize({ width: 390, height: 844 });
    await goto(page, '/', 'Overview');
    const more = nav(page).getByRole('button', { name: 'More', exact: true });
    await more.click();
    const tag = navMenu(page).locator('p.build');
    const bubble = tag.locator('xpath=..').locator('.bubble');
    const box = await tag.boundingBox();
    expect(box, 'the build tag is in the sheet').not.toBeNull();
    const x = (box?.x ?? 0) + (box?.width ?? 0) / 2;
    const y = (box?.y ?? 0) + (box?.height ?? 0) / 2;
    await page.keyboard.press('Escape');
    await expect(navMenu(page)).toHaveCount(0);

    // Park the pointer where the tag will be, then open the sheet from the keyboard.
    await page.mouse.move(x, y);
    await more.focus();
    await page.keyboard.press('Enter');
    await expect(navMenu(page)).toBeVisible();
    await page.waitForTimeout(600);
    await expect(page.locator('.bubble', { hasText: 'development build' })).toHaveCount(0);
    await expect(page.locator('.bubble')).toHaveCount(0);

    // Move onto its current box: reopening the sheet must not make the old
    // coordinates a few pixels short of the tag.
    await tag.hover();
    await expect(bubble).toBeVisible();
    // The first Escape is still the sheet's own once the bubble has gone.
    await page.keyboard.press('Escape');
    await expect(page.locator('.bubble')).toHaveCount(0);
    await expect(navMenu(page)).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(navMenu(page)).toHaveCount(0);
  });

  test('escape closes the bubble first, and the menu under it only on the next press', async ({
    page,
  }) => {
    await goto(page, '/', 'Overview');
    const menu = await openAccountMenu(page);
    // With authentication off there is no session to end: the item is there, says
    // so, and can be reached with the keyboard, because it is not `disabled`.
    const signOut = menu.getByRole('menuitem', { name: 'Sign out' });
    await expect(signOut).toHaveAttribute('aria-disabled', 'true');
    await expect(signOut).not.toHaveAttribute('disabled', /.*/);
    await page.keyboard.press('End');
    await expect(signOut).toBeFocused();
    const bubble = page.locator('.bubble:popover-open');
    await expect(bubble).toContainText('no session to end');
    await expect(signOut).toHaveAccessibleDescription(/no session to end/);

    await page.keyboard.press('Escape');
    await expect(bubble, 'the bubble goes first').toHaveCount(0);
    await expect(menu, 'the menu is still there').toBeVisible();
    await expect(signOut, 'focus did not move').toBeFocused();

    await page.keyboard.press('Escape');
    await expect(menu).toHaveCount(0);
  });

  test('an open bubble does not switch the keyboard shortcuts off', async ({ page }) => {
    await goto(page, '/', 'Overview');
    await navEntry(page, '/pools').hover();
    await expect(page.locator('.bubble:popover-open')).toBeVisible();
    // `g` then `r` is Runners. Resting on the sidebar must not stop it.
    await page.keyboard.press('g');
    await page.keyboard.press('r');
    await expect(page).toHaveURL(/\/runners$/);
  });

  test('the caret points at the trigger, even when the bubble has been held inside the window', async ({
    page,
  }) => {
    await goto(page, '/', 'Overview');
    // The documentation link is in the corner of the rail, so a bubble centred on
    // it would hang off the left edge and has to be pushed back in.
    const docs = page.getByRole('link', { name: /^Documentation/ }).first();
    await docs.hover();
    const bubble = page.locator('.bubble:popover-open').first();
    await expect(bubble).toBeVisible();
    const geometry = await bubble.evaluate((node) => {
      const box = node.getBoundingClientRect();
      const trigger = document
        .querySelector('.nav a[aria-label^="Documentation"]')!
        .getBoundingClientRect();
      return {
        left: box.left,
        right: box.right,
        width: box.width,
        arrow: parseFloat((node as HTMLElement).style.getPropertyValue('--arrow')),
        side: (node as HTMLElement).dataset.side,
        centre: trigger.left + trigger.width / 2,
        viewport: document.documentElement.clientWidth,
      };
    });
    expect(geometry.left).toBeGreaterThanOrEqual(8);
    expect(geometry.right).toBeLessThanOrEqual(geometry.viewport - 8);
    // Where the caret is in the window is the trigger's centre.
    expect(Math.abs(geometry.left + geometry.arrow - geometry.centre)).toBeLessThan(1.5);
    // And it is kept 14px from the ends.
    expect(geometry.arrow).toBeGreaterThanOrEqual(14);
    expect(geometry.arrow).toBeLessThanOrEqual(geometry.width - 14);
  });

  test('the bubble enters with a fade and a 4px slide from the trigger side', async ({ page }) => {
    await goto(page, '/', 'Overview');
    // The first frame after showPopover() is where @starting-style is read, so
    // that is where to look: afterwards the transition is already over.
    await page.evaluate(() => {
      const seen: unknown[] = [];
      (window as unknown as { __entrances: unknown[] }).__entrances = seen;
      const show = HTMLElement.prototype.showPopover;
      HTMLElement.prototype.showPopover = function (this: HTMLElement) {
        const result = show.call(this);
        if (this.classList.contains('bubble')) {
          requestAnimationFrame(() =>
            seen.push({
              side: this.dataset.side,
              animated: this.getAnimations().map((a) => (a as CSSTransition).transitionProperty),
              opacity: getComputedStyle(this).opacity,
              transform: getComputedStyle(this).transform,
            }),
          );
        }
        return result;
      };
    });
    await navEntry(page, '/pools').hover();
    await expect(page.locator('.bubble:popover-open')).toBeVisible();
    await expect
      .poll(() =>
        page.evaluate(() => (window as unknown as { __entrances: unknown[] }).__entrances.length),
      )
      .toBe(1);
    const [entrance] = await page.evaluate(
      () => (window as unknown as { __entrances: Array<Record<string, unknown>> }).__entrances,
    );
    expect(entrance!.side).toBe('right');
    expect(entrance!.animated).toEqual(['opacity', 'transform']);
    expect(Number(entrance!.opacity)).toBeLessThan(0.5);
    // A bubble to the right of its trigger starts 4px nearer to it.
    expect(entrance!.transform).toBe('matrix(1, 0, 0, 1, -4, 0)');
  });

  test('the bubble fades in over the fast motion token, and not at all when motion is reduced', async ({
    page,
  }) => {
    await goto(page, '/', 'Overview');
    await navEntry(page, '/pools').hover();
    const bubble = page.locator('.bubble:popover-open');
    await expect(bubble).toBeVisible();
    expect(await bubble.evaluate((n) => getComputedStyle(n).transitionDuration)).toContain('0.12s');
    const durations = (n: Element) => getComputedStyle(n).transitionDuration.split(',');
    await page.keyboard.press('Escape');

    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.mouse.move(700, 500);
    await navEntry(page, '/queue').hover();
    await expect(bubble).toBeVisible();
    expect(
      (await bubble.evaluate(durations)).map((d) => d.trim()),
      'the motion tokens collapse to 1ms, for the fade and the slide alike',
    ).toEqual(['0.001s', '0.001s']);
  });

  test('a layout-neutral wrapper puts no space of its own into the text around it', async ({
    page,
  }) => {
    // The wrapper has no box, so a whitespace text node in it is a real space in
    // whatever sentence its control is part of ("checked just now  .").
    for (const path of ['/', '/hosts', '/runners']) {
      await goto(page, path);
      await page.waitForLoadState('networkidle');
      const spaced = await page.evaluate(
        () =>
          [...document.querySelectorAll('.tip-wrap')].filter((wrapper) =>
            // Svelte leaves empty text nodes as anchors for its blocks; they draw nothing.
            [...wrapper.childNodes].some(
              (n) => n.nodeType === Node.TEXT_NODE && (n as Text).data !== '',
            ),
          ).length,
      );
      expect(spaced, `${path}: wrappers holding a stray text node`).toBe(0);
    }
    // And the bubble is exactly its words, not the template's spacing around them.
    await navEntry(page, '/pools').hover();
    const text = await page.locator('.bubble:popover-open').evaluate((n) => n.textContent ?? '');
    expect(text).toBe(text.trim());
  });

  test('a heading that carries help is a button with a dotted rule, and the help opens on focus', async ({
    page,
  }) => {
    await goto(page, '/workflows', 'Workflows');
    const heading = page.getByRole('button', { name: 'Duration', exact: true });
    await expect(heading).toBeVisible();
    const style = await heading.evaluate((n) => {
      const s = getComputedStyle(n);
      return {
        line: s.textDecorationLine,
        kind: s.textDecorationStyle,
        cursor: s.cursor,
        offset: s.textUnderlineOffset,
      };
    });
    expect(style.line).toContain('underline');
    expect(style.kind).toBe('dotted');
    expect(style.cursor).toBe('help');
    expect(style.offset).toBe('4px');
    await heading.focus();
    await expect(page.locator('.bubble:popover-open')).toContainText(
      'From the first job starting to the last one finishing.',
    );
    await expect(heading).toHaveAccessibleDescription(/From the first job starting/);
    // The bubble is a child of the heading, which is set in capitals and tracked
    // out. It must not inherit that.
    const voice = await page.locator('.bubble:popover-open').evaluate((n) => {
      const s = getComputedStyle(n);
      return { transform: s.textTransform, tracking: s.letterSpacing, align: s.textAlign };
    });
    expect(voice).toEqual({ transform: 'none', tracking: 'normal', align: 'start' });
  });

  test('a heading cut short by its column says its whole name on hover', async ({ page }) => {
    await goto(page, '/pools', 'Pools');
    // Twelve columns divide a frame of about 1150px, so this heading arrives as
    // "I…" with an ellipsis. Reading a table should not need the column chooser.
    const heading = page.locator('thead th[data-table-column="idle_timeout"]');
    await expect(heading).toContainText('Idle timeout');
    await heading.hover();
    await expect(page.locator('.bubble:popover-open')).toContainText('Idle timeout');

    // A heading with room says nothing: its words are already in front of the reader.
    await page.locator('thead th[data-table-column="name"]').hover();
    await expect(page.locator('.bubble:popover-open')).toHaveCount(0);
  });

  test('a row action that cannot be used says why, and can still be pointed at', async ({
    page,
  }) => {
    await goto(page, '/workflows', 'Workflows');
    // A run that has already finished cannot be cancelled. The button is there,
    // marked `aria-disabled` rather than `disabled`, so that it takes hover and
    // focus and the reason can be reached.
    const refused = page.locator('[role="toolbar"] button[aria-disabled="true"]').first();
    await expect(refused).toBeVisible();
    await expect(refused).not.toHaveAttribute('disabled', /.*/);
    await refused.hover();
    const bubble = page.locator('.bubble:popover-open');
    await expect(bubble.locator('.reason')).toBeVisible();
    await expect(bubble.locator('.title')).toBeVisible();
    // The actions column is right-aligned, and the bubble sits inside its cell.
    await expect(bubble).toHaveCSS('text-align', 'start');
    // Its name already carries the reason, so there is no second reading of it.
    await expect(refused).toHaveAccessibleName(/\./);
  });

  test('no native title is left to show as hover help', async ({ page }) => {
    // The browser's own tooltip waits a second, cannot be styled and never shows
    // for the keyboard. Where `title` is still right (an abbreviation) it is not
    // help. A page that grows one is a page that lost the shared bubble.
    const left: string[] = [];
    // Every section, the settings pages that lay out their own tables, and a
    // detail page of each kind, which is where the log viewer and the host cards
    // are.
    const detail: string[] = [];
    for (const kind of ['pools', 'runners', 'hosts']) {
      const items = (await (await page.request.get(`/api/v1/${kind}`)).json()).items ?? [];
      if (items[0]?.id) detail.push(`/${kind}/${items[0].id}`);
    }
    const paths = [
      ...SECTIONS.map((section) => section.path),
      '/settings/users',
      '/settings/tokens',
      '/settings/backups',
      '/hosts/new',
      ...detail,
    ];
    // One visit per page, about a second each when nothing else is running and
    // several when something is: the budget grows with the list, because a fixed
    // thirty seconds for a list that gains a section every few weeks is a test
    // that fails on the day the runners are busy.
    test.setTimeout(30_000 + paths.length * 6_000);
    for (const path of paths) {
      await goto(page, path);
      await page.waitForLoadState('networkidle');
      const titled = await page.evaluate(() =>
        [...document.querySelectorAll('[title]')]
          .filter((el) => !['ABBR', 'IFRAME'].includes(el.tagName) && !(el instanceof SVGElement))
          .map(
            (el) =>
              `${el.tagName.toLowerCase()}.${el.className?.toString().slice(0, 40)}: ${el.getAttribute('title')}`,
          ),
      );
      for (const t of titled) left.push(`${path} ${t}`);
    }
    expect(left).toEqual([]);
  });
});
