import type { Problem, Settings } from '$lib/api/types';
import { parseGoDuration } from '$lib/format';

const remedies: Record<string, { key: string; value: unknown }> = {
  'scheduler.default_runner_limits_off': { key: 'scheduler.default_runner_limits', value: true },
  'scheduler.host_throttling_off': { key: 'scheduler.host_throttling', value: true },
  'agent.bootstrap_cpu_grace_short': { key: 'agent.bootstrap_cpu_grace', value: '2m' },
  'runners.docker_wait_short': { key: 'runners.docker_wait', value: '3m' },
  'scheduler.provision_timeout_short': { key: 'scheduler.provision_timeout', value: '20m' },
};
export function isStartupFinding(problem: Problem): boolean {
  return !!remedies[problem.code ?? ''];
}

/** Only fix current findings. Never write through an environment pin or clear a pending restart. */
export function startupFix(settings: Settings, problems: readonly Problem[]) {
  const changes: Record<string, unknown> = {};
  const blocked: string[] = [];
  const pending: string[] = [];
  for (const problem of problems) {
    const remedy = remedies[problem.code ?? ''];
    if (!remedy) continue;
    const row = settings.settings?.find((s) => s.key === remedy.key);
    if (
      !row?.editable ||
      row.source === 'environment' ||
      settings.pinned_by_environment?.includes(remedy.key)
    ) {
      blocked.push(remedy.key);
    } else if (row.pending) {
      pending.push(remedy.key);
    } else {
      changes[remedy.key] = remedy.value;
      if (remedy.key === 'scheduler.provision_timeout') {
        const wait = settings.settings?.find((s) => s.key === 'runners.docker_wait')?.value;
        const milliseconds = typeof wait === 'string' ? (parseGoDuration(wait) ?? 0) : 0;
        // Leave five minutes beyond creation and Docker readiness, including a
        // deliberately long wait. parseGoDuration returns milliseconds.
        changes[remedy.key] = `${Math.max(20, Math.ceil(milliseconds / 60000) + 15)}m`;
      }
    }
  }
  return { changes, blocked, pending };
}
