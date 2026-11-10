import { toMillis } from '../format';

/**
 * Milliseconds from `since` to `now`, or null when there is no usable time.
 * A time slightly in the future (the controller's clock ahead of the
 * browser's) is zero, never a negative duration on a wall display.
 */
export function elapsedMs(since: string | undefined, now: number): number | null {
  if (since === undefined) return null;
  const at = toMillis(since);
  return at === null ? null : Math.max(0, now - at);
}
