/**
 * The last hour as two sparklines, for the Queue band: what was waiting and
 * what was running, a sample a minute, from the fleet samples the controller
 * already records.
 *
 * Plain functions so the line and the sentence under it are held to the
 * numbers by a test. A path is geometry and a reading is arithmetic; neither
 * needs a DOM.
 */
import type { FleetSample } from '../api/types';
import { toMillis } from '../format';

export interface TrendPoint {
  at: number;
  queued: number;
  running: number;
}

/**
 * The samples of the last `minutes`, oldest first, as this fleet's figures:
 * the fleet's own queued and running where the sample carries them, the
 * whole count where it does not (an older controller). A moment with no
 * sample stays absent rather than becoming a zero, since a controller that
 * was down and an empty queue are opposite news.
 */
export function trendPoints(
  samples: readonly FleetSample[],
  now: number,
  minutes = 60,
): TrendPoint[] {
  const since = now - minutes * 60_000;
  return samples
    .map((s) => ({
      at: toMillis(s.at) ?? NaN,
      queued: s.fleet_queued_jobs ?? s.queued_jobs ?? 0,
      running: s.fleet_running_jobs ?? s.running_jobs ?? 0,
    }))
    .filter((p) => Number.isFinite(p.at) && p.at >= since && p.at <= now)
    .sort((a, b) => a.at - b.at);
}

/**
 * Add the moment's own figures as the newest point, replacing one from the
 * same minute, so the line's end is what the band's sentence says now.
 */
export function withNow(
  points: readonly TrendPoint[],
  point: TrendPoint,
  minutes = 60,
): TrendPoint[] {
  const minute = Math.floor(point.at / 60_000);
  const kept = points.filter(
    (p) => Math.floor(p.at / 60_000) !== minute && p.at >= point.at - minutes * 60_000,
  );
  return [...kept, point];
}

/**
 * An SVG path for the values over a box, the x axis being time across the
 * window (so a gap in sampling is a gap in x) and y the value against `max`.
 * The area under it is the same path closed to the baseline.
 */
export function sparkPath(
  points: readonly { at: number; value: number }[],
  box: { width: number; height: number; from: number; to: number; max: number },
): { line: string; area: string } {
  if (points.length === 0 || box.to <= box.from) return { line: '', area: '' };
  const pad = 2;
  const w = box.width - 2 * pad;
  const h = box.height - 2 * pad;
  const max = Math.max(1, box.max);
  const xy = points.map((p) => {
    const x = pad + ((p.at - box.from) / (box.to - box.from)) * w;
    const y = pad + h - (Math.max(0, p.value) / max) * h;
    return [Number(x.toFixed(1)), Number(y.toFixed(1))] as const;
  });
  const line = xy.map(([x, y], i) => `${i === 0 ? 'M' : 'L'}${x} ${y}`).join(' ');
  const last = xy[xy.length - 1]!;
  const first = xy[0]!;
  const base = pad + h;
  return { line, area: `${line} L${last[0]} ${base} L${first[0]} ${base} Z` };
}

/**
 * The hour in one clause: "up from 2 an hour ago", "down from 9", "steady at
 * 6". The comparison is with the oldest point in the window, which is what an
 * operator who last looked an hour ago wants to know.
 */
export function trendWords(values: readonly number[], noun: string): string {
  if (values.length === 0) return `No ${noun} samples yet`;
  const first = values[0]!;
  const last = values[values.length - 1]!;
  if (values.length < 2 || first === last) return `${noun} steady at ${last}`;
  return last > first
    ? `${noun} up from ${first} an hour ago`
    : `${noun} down from ${first} an hour ago`;
}
