/**
 * The three figures in the command centre's hero: how long jobs wait, how long
 * a runner takes to start, and how many finished jobs succeeded.
 *
 * Plain, so each figure is held to the stats payload by a test. They are the
 * fleet's own figures, as the Overview's band reads them: on an organisation
 * that also uses hosted runners the top-level figures are mostly somebody
 * else's jobs.
 */
import type { Stats } from '../api/types';
import { describeWindow, formatDuration, NO_VALUE, parseGoDuration } from '../format';
import { finishedSinceHref, QUEUE_HREF, RUNNERS_HREF } from './links';

export interface HeroFigure {
  /** Which figure this is, so its tooltip can be worked out from the same stats. */
  key: 'wait' | 'start' | 'succeeded';
  label: string;
  value: string;
  note: string;
  href: string;
  /** Where the figure leads, as the tooltip's last row names it. */
  opens: string;
}

const AN_HOUR = 60 * 60 * 1000;

/**
 * "last hour", "last 24 hours": the stats window as a label can carry it. A
 * window the page cannot read is taken as the hour the controller defaults
 * to, as the Overview's figures take it, so the label and the link agree.
 */
export function lastWindow(window: string | undefined): { words: string; ms: number } {
  const ms = parseGoDuration(window);
  const words = ms && ms > 0 ? describeWindow(window) : '';
  if (!ms || ms <= 0 || words === '') return { words: 'last hour', ms: AN_HOUR };
  // "the last 1 hour" is how nothing says it.
  return { words: `last ${words.replace(/^1 /, '')}`, ms };
}

/**
 * The controller answers zero for a timing it has no samples for, and "0ms"
 * claims instant work where the honest answer is that nothing was measured.
 */
export function measured(ms: number | undefined): number | undefined {
  return ms !== undefined && ms > 0 ? ms : undefined;
}

export function heroFigures(stats: Stats | null | undefined, now: number): HeroFigure[] {
  const own = stats?.fleet ?? stats ?? null;
  // A wait is only measured on a job that has started, so with none the
  // median is not a figure; the same gate the Overview's band uses.
  const started = (own?.running_jobs ?? 0) + (own?.completed ?? 0);
  const wait = started > 0 ? own?.median_wait_ms : undefined;
  const waitP95 = started > 0 ? own?.p95_wait_ms : undefined;
  const startup = measured(stats?.p50_startup_ms);
  const startupP95 = measured(stats?.p95_startup_ms);
  const completed = own?.completed ?? 0;
  const succeeded = own?.succeeded ?? 0;
  const window = lastWindow(stats?.window);

  return [
    {
      key: 'wait',
      label: 'Median wait',
      value: formatDuration(wait),
      note: waitP95 === undefined ? NO_VALUE : `p95 ${formatDuration(waitP95)}`,
      href: QUEUE_HREF,
      opens: 'The queue',
    },
    {
      key: 'start',
      label: 'Runner start',
      value: formatDuration(startup),
      note: startupP95 === undefined ? NO_VALUE : `p95 ${formatDuration(startupP95)}`,
      href: RUNNERS_HREF,
      opens: 'The runners',
    },
    {
      // The share is over the controller's stats window, so the label names
      // that window and the link opens the jobs finished in it: the figure
      // and the list it opens are the same set.
      key: 'succeeded',
      label: `Succeeded, ${window.words}`,
      value: completed > 0 ? `${Math.round((succeeded / completed) * 100)}%` : NO_VALUE,
      note: completed > 0 ? `${succeeded} of ${completed} finished` : 'nothing finished',
      href: finishedSinceHref(new Date(now - window.ms)),
      opens: `The jobs finished in the ${window.words}`,
    },
  ];
}
