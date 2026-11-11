/**
 * A column's width and its position, set by whatever the operator is pointing
 * with.
 *
 * Both were once the HTML5 drag-and-drop protocol and a pointer listener on the
 * document, which between them worked for a mouse and for nothing else: a
 * browser raises `dragstart` from a finger only after a long press on Android
 * and never on iOS, so the column order was a stored preference a phone could
 * read and not write -- on the Rows layout, which exists precisely so that a
 * phone has columns to arrange. This file holds both gestures to working under
 * a finger as well as a cursor, which is why it runs in both projects: the
 * desktop one drives a real mouse, and the mobile one a real touchscreen's
 * events at the width where the Rows layout lives.
 *
 * The gesture is driven by dispatched pointer events rather than by
 * `page.mouse`, because a mouse is the one input this was never broken for.
 * What the browser is left to decide is everything around them -- capture,
 * hit-testing, the styles that say a column is in the air.
 */
import { expect, test, type Locator, type Page } from '@playwright/test';
import { closeColumnPanel, goto, grid, openColumnPanel, waitForRows } from './support/fixtures';

/** The grid every test here uses: enough columns to have a middle. */
const GRID = { path: '/runners', heading: 'Runners', label: 'Runners' } as const;

/**
 * Open the grid in the layout that has columns.
 *
 * A phone starts every grid as cards, where there is nothing to resize and
 * nothing to reorder; Rows is the layout that keeps the table, and the one the
 * gesture is for. Pressed rather than written into storage, because a
 * preference set behind the UI's back is one this file would keep passing on
 * after the control that sets it broke.
 */
async function openGrid(page: Page, isMobile: boolean): Promise<Locator> {
  await goto(page, GRID.path, GRID.heading);
  if (isMobile) {
    const rows = (await openColumnPanel(page)).getByRole('button', { name: /^Rows\b/ });
    await rows.click();
    await expect(rows).toHaveAttribute('aria-pressed', 'true');
    await closeColumnPanel(page);
  }
  const table = grid(page, GRID.label);
  await waitForRows(table);
  return table;
}

/** The column headings, in the order they are drawn. */
function order(table: Locator): Promise<string[]> {
  return table.evaluate((node) =>
    [...node.querySelectorAll<HTMLElement>('thead th[data-table-column]')].map(
      (th) => th.dataset.tableColumn ?? '',
    ),
  );
}

/** A heading's width as drawn, which is what a resize has to change. */
async function width(table: Locator, column: string): Promise<number> {
  return (await table.locator(`thead th[data-table-column="${column}"]`).boundingBox())!.width;
}

/**
 * One touch, start to finish, over the path given.
 *
 * Every event goes to the control the gesture started on, which is where the
 * browser sends them once the pointer is captured, and carries a point in the
 * window because the drop target is worked out by asking the document what is
 * under the finger.
 */
async function touchDrag(
  control: Locator,
  path: readonly { x: number; y: number }[],
  end: 'up' | 'cancel' = 'up',
): Promise<void> {
  const touch = { pointerId: 1, pointerType: 'touch', isPrimary: true, button: 0 };
  // `clientX` and `clientY` by name: a PointerEvent's `x` and `y` are read-only
  // aliases of them, so an init that carries only those leaves the event at the
  // top-left corner and every gesture in this file a no-op that still passes.
  const at = (point: { x: number; y: number }) => ({ clientX: point.x, clientY: point.y });
  const [first, ...rest] = path;
  await control.dispatchEvent('pointerdown', { ...touch, buttons: 1, ...at(first!) });
  for (const point of rest) {
    await control.dispatchEvent('pointermove', { ...touch, buttons: 1, ...at(point) });
  }
  await control.dispatchEvent(end === 'up' ? 'pointerup' : 'pointercancel', {
    ...touch,
    buttons: 0,
    ...at(rest.at(-1) ?? first!),
  });
}

/**
 * The middle of a control, where a finger lands on it.
 *
 * Scrolled to first: these are window coordinates, and the drop target is
 * found by asking the document what is at one of them. A heading below the
 * fold is at a point that is over nothing at all.
 */
async function centre(control: Locator): Promise<{ x: number; y: number }> {
  await control.scrollIntoViewIfNeeded();
  const box = (await control.boundingBox())!;
  return { x: box.x + box.width / 2, y: box.y + box.height / 2 };
}

test('a finger can pull a column wider, and the width outlives the visit', async ({
  page,
  isMobile,
}) => {
  const table = await openGrid(page, isMobile);
  const heading = table.locator('thead th[data-table-column="name"]');
  const before = await width(table, 'name');

  const from = await centre(heading.locator('.resizer'));
  await touchDrag(heading.locator('.resizer'), [
    from,
    { x: from.x + 20, y: from.y },
    { x: from.x + 70, y: from.y },
  ]);

  // The drag asked for 70 more pixels. Held loosely: the column has a ceiling,
  // and what matters is that the finger moved it at all.
  await expect
    .poll(() => width(table, 'name'), { message: 'the finger did not widen the column' })
    .toBeGreaterThan(before + 40);

  // Written through to the preference, not merely to the DOM: the reason to
  // resize a column is that it stays resized.
  const set = await width(table, 'name');
  await openGrid(page, isMobile);
  await expect.poll(() => width(table, 'name')).toBeCloseTo(set, 0);
});

test('a finger can drag a column past its neighbour, and it stays there', async ({
  page,
  isMobile,
}) => {
  const table = await openGrid(page, isMobile);
  const started = await order(table);
  const [first, second] = started;
  expect(second, 'the grid needs two columns to swap').toBeTruthy();

  const grip = table.locator(`thead th[data-table-column="${first}"] .move`);
  const from = await centre(grip);
  const onto = await centre(table.locator(`thead th[data-table-column="${second}"]`));
  await touchDrag(grip, [from, { x: from.x + 10, y: from.y }, onto]);

  await expect
    .poll(() => order(table), { message: 'the column did not move' })
    .toEqual([second, first, ...started.slice(2)]);

  await openGrid(page, isMobile);
  await expect.poll(() => order(table)).toEqual([second, first, ...started.slice(2)]);
});

test('a tap on the grip rearranges nothing', async ({ page, isMobile }) => {
  // A finger never presses a target without moving a pixel or two, so a press
  // that goes nowhere has to stay a press -- otherwise reaching for the grip
  // to read its tooltip, or to give it focus for the arrow keys, shuffles the
  // table.
  const table = await openGrid(page, isMobile);
  const started = await order(table);
  const grip = table.locator(`thead th[data-table-column="${started[0]}"] .move`);
  const from = await centre(grip);

  await touchDrag(grip, [from, { x: from.x + 2, y: from.y + 1 }]);

  await expect.poll(() => order(table)).toEqual(started);
});

test('a drag the browser takes back leaves the order alone', async ({ page, isMobile }) => {
  // `pointercancel` and no `pointerup` is what a gesture the system claims --
  // a back-swipe, a second finger, a notification -- looks like from here. The
  // column has to go back where it was rather than land wherever the finger
  // was when it was taken away.
  const table = await openGrid(page, isMobile);
  const started = await order(table);
  const [first, second] = started;

  const grip = table.locator(`thead th[data-table-column="${first}"] .move`);
  const from = await centre(grip);
  const onto = await centre(table.locator(`thead th[data-table-column="${second}"]`));
  await touchDrag(grip, [from, { x: from.x + 10, y: from.y }, onto], 'cancel');

  await expect.poll(() => order(table)).toEqual(started);
  // And nothing is left looking picked up.
  await expect(table.locator('thead th.dragging, thead th.drop-target')).toHaveCount(0);
});

test('a mouse still reorders columns, now that the drag protocol is gone', async ({
  page,
  isMobile,
}) => {
  // The gesture replaced `draggable` and `dragstart` outright rather than
  // sitting beside them, so the input it already worked for is the one at risk.
  // Driven by a real cursor, which is the desktop project's: a phone's Rows
  // layout scrolls sideways inside the frame, so a cursor dragged across it is
  // measuring that scroll as much as the gesture.
  test.skip(!!isMobile, 'a real cursor, in the desktop project');
  const table = await openGrid(page, isMobile);
  const started = await order(table);
  const [first, second] = started;

  const grip = table.locator(`thead th[data-table-column="${first}"] .move`);
  const from = await centre(grip);
  const onto = await centre(table.locator(`thead th[data-table-column="${second}"]`));
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  await page.mouse.move(from.x + 10, from.y);
  await page.mouse.move(onto.x, onto.y, { steps: 4 });
  await page.mouse.up();

  await expect
    .poll(() => order(table), { message: 'the mouse drag did not move the column' })
    .toEqual([second, first, ...started.slice(2)]);
});

test('the controls are big enough to hit with a finger', async ({ page, isMobile }) => {
  // docs/ui-guidelines.md puts a control read by a finger at
  // `--z-control-touch`, and these two were laid out for a cursor: a 12px edge
  // and a 20px grip. The measurement is the mobile project's, because the
  // desktop one is deliberately denser.
  test.skip(!isMobile, 'the touch sizes, in the Pixel 7 project');
  const table = await openGrid(page, isMobile);
  const heading = table.locator('thead th[data-table-column]').first();

  const grip = (await heading.locator('.move').boundingBox())!;
  const resizer = (await heading.locator('.resizer').boundingBox())!;
  expect(grip.height, 'the reposition grip is shorter than a fingertip').toBeGreaterThanOrEqual(44);
  expect(resizer.height, 'the resize handle is shorter than a fingertip').toBeGreaterThanOrEqual(
    44,
  );
  expect(resizer.width, 'the resize handle is narrower than a fingertip can aim').toBeGreaterThan(
    12,
  );

  // Neither gesture survives the browser deciding the drag was a scroll.
  for (const control of ['.move', '.resizer']) {
    const action = await heading
      .locator(control)
      .evaluate((node) => getComputedStyle(node).touchAction);
    expect(action, `${control} lets the browser scroll instead of dragging`).toBe('none');
  }

  // A handle that only appears on hover is a handle a touchscreen never sees.
  const line = await heading
    .locator('.resizer')
    .evaluate((node) => getComputedStyle(node, '::after').opacity);
  expect(Number(line), 'the resize handle is invisible until hovered').toBeGreaterThan(0);
});

/*
 * A table meets the right-hand edge of the frame it is drawn in.
 *
 * Columns that add up to less than the frame used to stop short and leave a
 * blank strip down the right, which on a wide screen reads as a page that has
 * not finished drawing. The spare goes to the columns nobody sized, so these
 * hold both halves: the table fills, and a column dragged to a width keeps it.
 */

/** How far the table's right edge is from the inside of its frame's right edge. */
function gap(table: Locator): Promise<number> {
  return table.evaluate((node) => {
    const frame = node.parentElement as HTMLElement;
    const box = frame.getBoundingClientRect();
    const inner = box.left + frame.clientLeft + frame.clientWidth;
    return inner - node.getBoundingClientRect().right;
  });
}

test('at 1920 wide the Runners table reaches the right edge of its frame', async ({
  page,
  isMobile,
}) => {
  test.skip(!!isMobile, 'a wide desktop window');
  await page.setViewportSize({ width: 1920, height: 1000 });
  const table = await openGrid(page, false);
  await expect.poll(() => gap(table).then(Math.abs)).toBeLessThanOrEqual(1);
});

test('a column dragged narrower keeps its width and the table still fills', async ({
  page,
  isMobile,
}) => {
  test.skip(!!isMobile, 'a wide desktop window');
  await page.setViewportSize({ width: 1920, height: 1000 });
  const table = await openGrid(page, false);
  const heading = table.locator('thead th[data-table-column="name"]');
  const before = await width(table, 'name');

  const from = await centre(heading.locator('.resizer'));
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  await page.mouse.move(from.x - 20, from.y);
  await page.mouse.move(from.x - 60, from.y, { steps: 4 });
  await page.mouse.up();

  await expect
    .poll(() => width(table, 'name'), { message: 'the drag did not narrow the column' })
    .toBeLessThan(before - 40);
  const dragged = await width(table, 'name');
  await expect.poll(() => gap(table).then(Math.abs)).toBeLessThanOrEqual(1);

  // The width the operator chose is the one stored: back on the page, the same
  // column is drawn at it while the others still share out the rest.
  await openGrid(page, false);
  await expect.poll(() => width(table, 'name')).toBeCloseTo(dragged, 0);
  await expect.poll(() => gap(table).then(Math.abs)).toBeLessThanOrEqual(1);
});

test('columns wider than the window scroll inside the grid and not the page', async ({
  page,
  isMobile,
}) => {
  test.skip(!!isMobile, 'a desktop window the columns have been widened past');
  await page.setViewportSize({ width: 1180, height: 900 });
  const table = await openGrid(page, false);
  // Widened from the keyboard, eight pixels a press, until the columns no
  // longer fit: a layout the operator chose, which the frame scrolls to show.
  const resizer = table
    .locator('thead th[data-table-column="name"]')
    .getByRole('button', { name: 'Resize Name column' });
  for (let press = 0; press < 60; press++) await resizer.press('ArrowRight');

  await expect
    .poll(() => table.evaluate((node) => node.scrollWidth - node.parentElement!.clientWidth))
    .toBeGreaterThan(50);
  const scroller = await table.evaluate((node) => {
    const frame = node.parentElement as HTMLElement;
    frame.scrollLeft = 10_000;
    return frame.scrollLeft;
  });
  expect(scroller, 'the grid frame scrolls sideways').toBeGreaterThan(0);
  const document = await page.evaluate(
    () => window.document.documentElement.scrollWidth - window.document.documentElement.clientWidth,
  );
  expect(document, 'the page scrolls sideways').toBeLessThanOrEqual(1);
});

/*
 * The edge stays under the pointer while the table fills.
 *
 * With the spare width shared out on every move, the columns left of the edge
 * took some of each pixel the drag gave back, and the handle crept away from
 * the cursor. Every other column is held at its drawn width for the drag, so
 * the edge is where the pointer is when it lets go; the share is worked out
 * again after, and the table fills.
 */
test('a dragged edge stays under the pointer, and the table fills again after', async ({
  page,
  isMobile,
}) => {
  test.skip(!!isMobile, 'a wide desktop window and a real cursor');
  await page.setViewportSize({ width: 1920, height: 1000 });
  const table = await openGrid(page, false);
  const resizer = table.locator('thead th[data-table-column="pool"] .resizer');
  const from = await centre(resizer);
  const edge = async (): Promise<number> => {
    const box = (await resizer.boundingBox())!;
    return box.x + box.width / 2;
  };
  expect(Math.abs((await edge()) - from.x)).toBeLessThanOrEqual(1);

  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  await page.mouse.move(from.x - 20, from.y);
  await page.mouse.move(from.x - 60, from.y, { steps: 6 });
  await expect.poll(edge, { message: 'the edge lags the pointer' }).toBeLessThan(from.x - 50);
  expect(
    Math.abs((await edge()) - (from.x - 60)),
    'the edge drifted off the pointer',
  ).toBeLessThanOrEqual(2);
  await page.mouse.up();

  await expect.poll(() => gap(table).then(Math.abs)).toBeLessThanOrEqual(1);
});
