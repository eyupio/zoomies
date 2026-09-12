import type { Host } from '$lib/api/types';

/** The selector keys a host answers for from its agent's report, not a label. */
export const REPORTED_KEYS = ['os', 'arch'] as const;

/** Mirrors Host.SelectorValue in the Go store. */
export function hostSelectorValue(host: Host, key: string): string {
  const labels = host.labels ?? {};
  if (key in labels) return labels[key] ?? '';
  if (key === 'os') return host.os ?? '';
  if (key === 'arch') return host.arch ?? '';
  return '';
}

/** Whether a host satisfies every entry of a selector. Empty means any host. */
export function hostMatchesSelector(host: Host, selector: Record<string, string>): boolean {
  return Object.entries(selector).every(([key, value]) => hostSelectorValue(host, key) === value);
}

/** Values currently reported for a selector key, including a disconnected saved value. */
export function reportedValues(
  hosts: readonly Host[],
  key: string,
  extra: string,
): { value: string; hosts: number }[] {
  const counts: Record<string, number> = {};
  for (const host of hosts) {
    const value = hostSelectorValue(host, key);
    if (value) counts[value] = (counts[value] ?? 0) + 1;
  }
  if (extra && counts[extra] === undefined) counts[extra] = 0;
  return Object.entries(counts)
    .map(([value, count]) => ({ value, hosts: count }))
    .sort((a, b) => b.hosts - a.hosts || a.value.localeCompare(b.value));
}
