/**
 * Pointer gestures a spec drives by hand.
 *
 * Playwright's own mouse cannot stand in for a finger, and the gestures under
 * test are pointer events captured on the control they started on, so every
 * event is dispatched to that control with a point in the window: the drop
 * target is worked out by asking the document what is under the finger.
 */
import type { Locator } from '@playwright/test';

/** One touch, start to finish, over the path given. */
export async function touchDrag(
  control: Locator,
  path: readonly { x: number; y: number }[],
  end: 'up' | 'cancel' = 'up',
): Promise<void> {
  const touch = { pointerId: 1, pointerType: 'touch', isPrimary: true, button: 0 };
  // `clientX` and `clientY` by name: a PointerEvent's `x` and `y` are read-only
  // aliases of them, so an init that carries only those leaves the event at the
  // top-left corner and every gesture a no-op that still passes.
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
 * The middle of a control, where a finger lands on it. Scrolled to first:
 * these are window coordinates, and a point below the fold is over nothing.
 */
export async function centre(control: Locator): Promise<{ x: number; y: number }> {
  await control.scrollIntoViewIfNeeded();
  const box = (await control.boundingBox())!;
  return { x: box.x + box.width / 2, y: box.y + box.height / 2 };
}
