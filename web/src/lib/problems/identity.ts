/**
 * What makes two reports of a problem the same problem.
 *
 * It lives apart from the two features that ask -- the drawer, which remembers
 * which problems an operator has read, and the Overview's feed, which reports
 * one the first time it is raised -- because a second definition of "the same
 * fault" is how those two would come to disagree about it, and because this
 * one is plain enough to test in Node.
 */
import type { Problem } from '../api/types';

/**
 * The identity of a problem across refreshes.
 *
 * Deliberately built from what the problem is *about* rather than from its
 * prose: "5 webhook deliveries were rejected" becoming "6 webhook deliveries
 * were rejected" is the same fault, and re-asking about it every minute is the
 * nagging the dismissals exist to stop.
 */
export function problemKey(problem: Problem): string {
  return [
    problem.code,
    problem.target_kind ?? '',
    problem.target_id ?? '',
    problem.setting ?? '',
    // Two dangerous settings on one pool share a code and a target, so the
    // title is the only thing that separates them.
    problem.target_kind === 'pool' ? (problem.title ?? '') : '',
  ].join('|');
}
