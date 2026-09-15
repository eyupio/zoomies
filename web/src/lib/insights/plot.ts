/**
 * The geometry every chart in here shares: where the labels along the bottom
 * go, how labels that want the same height are pushed apart, and what a
 * round axis is.
 *
 * It exists because the capacity map and the fleet trend answered the same
 * three questions and there is one right answer to each. Pure, and imported
 * by unit tests, so nothing here touches the DOM or the clock: the caller
 * passes the instants.
 */

/**
 * Where to put the labels along the bottom: on round times -- the hour, the
 * quarter hour, midnight -- rather than at four evenly spaced instants that
 * happen to be 11:19 and 19:19. The step is the smallest of the candidates
 * that fits about five labels in the window, and the ticks are the multiples
 * of it in local time, so a day's chart is labelled at 00:00, 06:00, 12:00
 * and a minute's at :10, :20, :30.
 */
export function timeTicks(start: number, end: number, target = 5): number[] {
  const MINUTE = 60_000;
  const steps = [
    10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 10800, 21600, 43200, 86400,
  ].map((s) => s * 1000);
  const span = end - start;
  const step = steps.find((s) => span / s <= target) ?? steps[steps.length - 1]!;
  // Multiples of the step in local time, which is what the labels say.
  const offset = new Date(start).getTimezoneOffset() * MINUTE;
  const first = Math.ceil((start - offset) / step) * step + offset;
  const out: number[] = [];
  for (let at = first; at <= end; at += step) out.push(at);
  return out;
}

/**
 * Where the labels at the ends of the lines go. Each wants to sit level with
 * its line, and two lines that end within a label's height of each other
 * would print on top of one another; so, taken from the top, each is pushed
 * down until it clears the one above, and if the last is then past the
 * bottom the stack is walked back up. The answer is in the callers' order.
 */
export function spreadLabels(
  wanted: readonly number[],
  gap: number,
  min: number,
  max: number,
): number[] {
  const order = wanted.map((y, i) => ({ y, i })).sort((a, b) => a.y - b.y);
  const placed = order.map((o) => Math.max(min, Math.min(max, o.y)));
  for (let k = 1; k < placed.length; k++) placed[k] = Math.max(placed[k]!, placed[k - 1]! + gap);
  for (let k = placed.length - 1; k >= 0; k--) {
    const ceiling = k === placed.length - 1 ? max : placed[k + 1]! - gap;
    placed[k] = Math.max(min, Math.min(placed[k]!, ceiling));
  }
  const out = new Array<number>(wanted.length);
  order.forEach((o, k) => (out[o.i] = placed[k]!));
  return out;
}

/**
 * A round axis for a chart whose ceiling is whatever it happened to see. The
 * capacity map's figures are all shares of a machine and its axis is 0-100%
 * for good; a count of jobs has no such ceiling, and an axis that simply
 * topped out at the peak labelled its gridlines 4.25 and 8.5 -- halves of a
 * job nobody can queue. The step is the first of 1, 2 or 5 times a power of
 * ten that fits the peak into `divisions` bands, so the labels are the round
 * numbers an operator would have picked, and the top of the axis is a
 * whole number of steps above zero.
 */
export function countAxis(peak: number, divisions = 4): { ceiling: number; values: number[] } {
  const target = Math.max(1, peak) / Math.max(1, divisions);
  const magnitude = 10 ** Math.max(0, Math.floor(Math.log10(target)));
  const step = [1, 2, 5, 10].map((m) => m * magnitude).find((s) => s >= target) ?? magnitude * 10;
  const ceiling = Math.max(step, Math.ceil(Math.max(0, peak) / step) * step);
  const values: number[] = [];
  for (let v = 0; v <= ceiling + step / 2; v += step) values.push(v);
  return { ceiling, values };
}
