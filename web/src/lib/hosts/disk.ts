import type { Host, Problem } from '../api/types';

/** The controller's placement verdict, shared by cards, details and filters. */
export function diskProblemFor(
  host: Pick<Host, 'id' | 'healthy' | 'cordoned'>,
  problems: readonly Problem[],
): Problem | undefined {
  if (!host.id || host.healthy === false || host.cordoned) return undefined;
  return problems.find(
    (problem) =>
      problem.code === 'host.low_disk' &&
      problem.target_kind === 'host' &&
      problem.target_id === host.id,
  );
}
