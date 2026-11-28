import type { Host } from '../api/types';

export function cacheStorageSummary(host: Pick<Host, 'cache_storage'>) {
  const rows = host.cache_storage ?? [];
  if (rows.length === 0) return null;
  const measured = rows.filter((r) => Date.parse(r.measured_at) > 0);
  return {
    bytes: measured.reduce((n, r) => n + r.total_bytes, 0),
    packages: measured.reduce((n, r) => n + r.package_bytes, 0),
    tools: measured.reduce((n, r) => n + r.tools_bytes, 0),
    service: measured.reduce((n, r) => n + r.service_bytes, 0),
    partial: measured.length !== rows.length || rows.some((r) => r.deferred?.includes('unknown')),
    over: measured.some((r) => r.size_limit > 0 && r.total_bytes > r.size_limit),
    reasons: [
      ...new Set(
        rows
          .filter(
            (r) =>
              (!r.deferred?.startsWith('A container') && !r.deferred?.startsWith('A runner')) ||
              (r.size_limit > 0 && r.total_bytes > r.size_limit),
          )
          .map((r) => r.deferred)
          .filter(Boolean),
      ),
    ].join('. '),
    measuredAt: measured.length ? measured.map((r) => r.measured_at).sort()[0] : null,
  };
}
