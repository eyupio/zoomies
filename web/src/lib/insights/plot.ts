/**
 * What every chart in here shares: a figure drawn over time, the frame it is
 * drawn in, where the labels along the bottom go, how labels that want the
 * same height are pushed apart, and what a round axis is.
 *
 * It exists because the capacity map, the fleet trend and the usage chart
 * answered the same questions and there is one right answer to each. Pure,
 * and imported by unit tests, so nothing here touches the DOM or the clock:
 * the caller passes the instants.
 */

/**
 * Where to put the labels along the bottom: on round times -- the hour, the
 * quarter hour, midnight -- rather than at four evenly spaced instants that
 * happen to be 11:19 and 19:19. The step is the smallest of the candidates
 * that fits about five labels in the window, and the ticks are the multiples
 * of it in local time, so a day's chart is labelled at 00:00, 06:00, 12:00,
 * a minute's at :10, :20, :30, and a fortnight's every second midnight.
 */
export function timeTicks(start: number, end: number, target = 5): number[] {
  const MINUTE = 60_000;
  const DAY = 86400;
  // Up to a day the steps are the round times a clock has; past it they are
  // whole days, because a usage report is read over a fortnight or a quarter
  // and a step of "a day" would print thirty labels over one another. A
  // multi-day step lands on a local midnight rather than on a named weekday:
  // evenly spaced is what the axis is for, and the label says the date.
  const steps = [
    10,
    15,
    30,
    60,
    120,
    300,
    600,
    900,
    1800,
    3600,
    7200,
    10800,
    21600,
    43200,
    DAY,
    2 * DAY,
    3 * DAY,
    7 * DAY,
    14 * DAY,
    28 * DAY,
    91 * DAY,
    365 * DAY,
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
 *
 * `fractional` lets the step fall below one, for the axes whose unit divides:
 * a quarter of an hour of runner time is a real quantity, and a fleet whose
 * busiest day ran twenty minutes of jobs would otherwise be drawn against a
 * one-hour axis with nothing but the baseline under it.
 */
export function countAxis(
  peak: number,
  divisions = 4,
  fractional = false,
): { ceiling: number; values: number[] } {
  // Nothing to scale to: an axis with one step on it, so an empty chart is
  // still a chart and the first job to land has somewhere to be drawn.
  if (!(peak > 0)) return { ceiling: 1, values: [0, 1] };
  // Rounded at every step: 3 * 0.1 is 0.30000000000000004, and a gridline
  // labelled that is worse than no gridline at all.
  const round = (value: number) => Number(value.toPrecision(12));
  const target = peak / Math.max(1, divisions);
  const exponent = Math.floor(Math.log10(target));
  const magnitude = 10 ** (fractional ? exponent : Math.max(0, exponent));
  const step =
    [1, 2, 5, 10].map((m) => round(m * magnitude)).find((s) => s >= target) ??
    round(magnitude * 10);
  const ceiling = round(Math.max(step, Math.ceil(peak / step) * step));
  const values: number[] = [];
  for (let i = 0; i * step <= ceiling + step / 2; i++) values.push(round(i * step));
  return { ceiling, values };
}

/* -- the figures a trend draws -------------------------------------------- */

/**
 * One figure on a trend: what it is called, and how its line is drawn.
 *
 * Colour and stroke carry two different things, and which two is the chart's
 * to decide -- the capacity map gives a colour to each host and a stroke to
 * each measurement, the fleet trend a colour to each state and a stroke to
 * whether the figure counts jobs or runners. What is fixed is that a figure
 * is never told apart by colour alone: two lines within about twenty points
 * of CIEDE2000 are two lines an operator has to squint at, and the nearer of
 * such a pair is drawn dashed so the legend swatch carries a stroke as well
 * as a hue. See docs/ui-guidelines.md.
 */
export interface Series {
  key: string;
  /** The chip, the legend row and the reading. */
  label: string;
  /** What the figure counts, for the tooltip and assistive text. */
  hint: string;
  tone: string;
  /** An SVG dash pattern. `''` is solid. */
  dash: string;
}

/** One reading of a figure. `null` is a gap, and never a zero. */
export interface SeriesPoint {
  at: number;
  value: number | null;
}

/** One figure's line over the window, as a plot receives it. */
export interface SeriesLine<S extends Series = Series> {
  series: S;
  points: SeriesPoint[];
  /** The newest observed point, for the label at the end and the pulse. */
  last: { i: number; value: number } | null;
}

/** A line from a figure and its points, with the newest reading found. */
export function seriesLine<S extends Series>(series: S, points: SeriesPoint[]): SeriesLine<S> {
  let last: { i: number; value: number } | null = null;
  points.forEach((p, i) => {
    if (p.value !== null) last = { i, value: p.value };
  });
  return { series, points, last };
}

/**
 * The observed points of a line, grouped into the runs one stroke joins. An
 * interval nobody sampled is left out, so the line passes from the reading
 * before to the reading after and no marker claims a figure for it: a gap is
 * a gap, and never a zero, because an outage and an empty queue are opposite
 * news.
 */
export function seriesRuns(points: readonly SeriesPoint[]): Array<{ i: number; value: number }[]> {
  const out: Array<{ i: number; value: number }[]> = [];
  let run: { i: number; value: number }[] = [];
  points.forEach((p, i) => {
    if (p.value === null) {
      if (run.length) out.push(run);
      run = [];
    } else run.push({ i, value: p.value });
  });
  if (run.length) out.push(run);
  return out;
}

/** The highest figure across every line, and which line and point it was at. */
export function seriesPeak<S extends Series>(
  lines: readonly SeriesLine<S>[],
): { line: SeriesLine<S>; i: number; value: number } | null {
  let best: { line: SeriesLine<S>; i: number; value: number } | null = null;
  for (const line of lines)
    line.points.forEach((p, i) => {
      if (p.value !== null && (best === null || p.value > best.value))
        best = { line, i, value: p.value };
    });
  return best;
}

/**
 * The figure whose line passes nearest a pointer at point `i`, within
 * `tolerance` pixels, or null where none does. Pointing at a line is how a
 * reader asks what it is, and with five of them on one chart the legend
 * alone cannot answer that.
 */
export function nearestSeries<S extends Series>(
  lines: readonly SeriesLine<S>[],
  i: number,
  viewY: number,
  y: (value: number) => number,
  tolerance: number,
): S['key'] | null {
  let best: { key: S['key']; distance: number } | null = null;
  for (const line of lines) {
    const v = line.points[i]?.value;
    if (v === null || v === undefined) continue;
    const distance = Math.abs(y(v) - viewY);
    if (distance <= tolerance && (best === null || distance < best.distance))
      best = { key: line.series.key, distance };
  }
  return best?.key ?? null;
}

/* -- the frame ------------------------------------------------------------- */

/** The drawing's edges, in pixels, for a trend `width` wide. */
export interface TrendFrame {
  W: number;
  H: number;
  LEFT: number;
  RIGHT: number;
  TOP: number;
  BOTTOM: number;
  SPAN: number;
  /** A phone's shape: taller, and with fewer labels along the bottom. */
  narrow: boolean;
}

/**
 * The frame is drawn at the width the panel has, one unit to one pixel,
 * rather than a fixed picture stretched to fit: the old chart drew 760 units
 * and let the browser scale them, so its eleven-unit axis text arrived on a
 * wide screen at nineteen pixels and read as a heading. The left gutter is
 * sized for the widest figure the axis will print, and the right one holds
 * the newest value at the end of each line.
 */
export function trendFrame(
  width: number,
  options: { digits?: number; labels?: boolean } = {},
): TrendFrame {
  const W = Math.max(280, Math.round(width));
  const narrow = W < 560;
  const H = narrow ? 236 : 196;
  const LEFT = 16 + 7 * Math.max(1, options.digits ?? 3);
  const RIGHT = W - (options.labels === false ? 10 : 46);
  const TOP = 14;
  return { W, H, LEFT, RIGHT, TOP, BOTTOM: H - 26, SPAN: RIGHT - LEFT, narrow };
}

/** The x of point `i`, of `count` across the frame. */
export function trendX(frame: TrendFrame, count: number): (i: number) => number {
  return (i) => frame.LEFT + (i * frame.SPAN) / Math.max(1, count - 1);
}

/** The y of a figure on an axis running from zero to `ceiling`. */
export function trendY(frame: TrendFrame, ceiling: number): (value: number) => number {
  return (value) =>
    frame.BOTTOM - (Math.min(value, ceiling) / Math.max(1, ceiling)) * (frame.BOTTOM - frame.TOP);
}

/** Which point a pointer is over, from where it is across the frame. */
export function trendIndexAtX(frame: TrendFrame, count: number, viewX: number): number {
  const i = Math.round(((viewX - frame.LEFT) / frame.SPAN) * Math.max(1, count - 1));
  return Math.max(0, Math.min(count - 1, i));
}
