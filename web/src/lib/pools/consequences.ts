/**
 * What deleting a pool will actually do, in the order an operator cares about.
 *
 * Both places that offer the deletion -- the list and the pool's own page --
 * showed this, in two copies that had to be kept in step by hand. The wording
 * is deliberate: it names what happens to *running work* rather than to rows,
 * because the difference between "drained, then removed" and "destroyed
 * immediately" is somebody's build.
 */
import { pluralise } from '$lib/format';

export interface PoolCounts {
  live?: number;
  busy?: number;
}

export function deletionConsequences(counts: PoolCounts, force: boolean): string[] {
  const live = counts.live ?? 0;
  const busy = counts.busy ?? 0;
  const lines = [
    live === 0
      ? 'It has no runners right now, so nothing is interrupted.'
      : force
        ? `${pluralise(live, 'runner')} will be destroyed immediately.`
        : `${pluralise(live, 'runner')} will be drained, then removed.`,
  ];
  if (busy > 0) {
    lines.push(
      force
        ? `${pluralise(busy, 'job')} running right now will be interrupted.`
        : `${pluralise(busy, 'job')} running right now will be allowed to finish first.`,
    );
  }
  lines.push('The runners are deregistered from GitHub either way.');
  return lines;
}
