/**
 * The geometry of the command centre's hero: the ring's arc and the hour
 * chart's lines.
 *
 * Pure, so the drawing is held to its figures by a test rather than by eye. The
 * chart's points are evenly spaced, one per sample, because the samples are a
 * minute apart and a wall display reads the shape of the hour, not the second
 * each point was taken.
 */

/** Two decimal places: enough for a pixel, short enough to keep a path readable. */
function coord(value: number): number {
  return Math.round(value * 100) / 100;
}

function points(
  values: readonly number[],
  width: number,
  height: number,
  max: number,
): [number, number][] {
  if (values.length === 0) return [];
  // A single sample is a level line across the frame, not a dot nobody sees.
  const series = values.length === 1 ? [values[0]!, values[0]!] : values;
  const step = width / (series.length - 1);
  return series.map((value, i) => {
    // A zero maximum means nothing happened in the hour; every point sits on
    // the baseline rather than dividing by zero into NaN.
    const share = max > 0 ? Math.min(1, Math.max(0, value / max)) : 0;
    return [coord(i * step), coord(height - share * height)];
  });
}

/** An SVG path through the values, left edge to right edge, zero at the bottom. */
export function linePath(
  values: readonly number[],
  width: number,
  height: number,
  max: number,
): string {
  return points(values, width, height, max)
    .map(([x, y], i) => `${i === 0 ? 'M' : 'L'}${x} ${y}`)
    .join(' ');
}

/** The same line, closed along the bottom edge, for a filled area under it. */
export function areaPath(
  values: readonly number[],
  width: number,
  height: number,
  max: number,
): string {
  const line = linePath(values, width, height, max);
  if (line === '') return '';
  return `${line} L${coord(width)} ${coord(height)} L0 ${coord(height)} Z`;
}

/**
 * The share of the ring to draw. A throttle can leave more runners busy than
 * the host now offers slots, and the ring is a picture of the fleet's room,
 * not a gauge that wraps round, so it stops at full.
 */
export function ringFraction(busy: number, slots: number): number {
  if (slots <= 0) return 0;
  return Math.min(1, Math.max(0, busy / slots));
}

/**
 * How full a runner's CPU bar is: what it is using over the CPUs it was given.
 * The container runtime counts a whole CPU as 100, so two allocated CPUs fill
 * at 200. A runner with no quota is read against one CPU, and elastic CPU can
 * take a runner past its allocation, so the bar stops at full rather than
 * drawing outside its track. Null when nothing has been reported, which is a
 * bar with no fill rather than an empty one that claims an idle runner.
 */
export function cpuShare(runner: { cpu_percent?: number; allocated_cpus?: number }): number | null {
  if (runner.cpu_percent === undefined) return null;
  const cpus = runner.allocated_cpus && runner.allocated_cpus > 0 ? runner.allocated_cpus : 1;
  return Math.min(1, Math.max(0, runner.cpu_percent / (cpus * 100)));
}
