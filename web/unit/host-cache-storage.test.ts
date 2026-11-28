import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Host } from '../src/lib/api/types.ts';
import { cacheStorageSummary } from '../src/lib/hosts/cache-storage.ts';

const row = {
  pool_id: 'pool_one',
  package_bytes: 60,
  tools_bytes: 30,
  service_bytes: 20,
  total_bytes: 110,
  size_limit: 100,
  measured_at: '2026-11-17T14:00:00Z',
};

test('An older agent stays unknown rather than showing zero cache', () => {
  assert.equal(cacheStorageSummary({}), null);
  assert.equal(cacheStorageSummary({ cache_storage: [] }), null);
});
test('Cache total includes packages, tools and the service, and shows an excess', () => {
  const host: Pick<Host, 'cache_storage'> = { cache_storage: [row] };
  const result = cacheStorageSummary(host)!;
  assert.equal(result.bytes, 110);
  assert.equal(result.packages, 60);
  assert.equal(result.tools, 30);
  assert.equal(result.service, 20);
  assert.equal(result.over, true);
  assert.equal(result.partial, false);
});
test('Failed measurements are partial and are never added as known bytes', () => {
  const result = cacheStorageSummary({
    cache_storage: [
      row,
      {
        ...row,
        pool_id: 'pool_two',
        measured_at: '0001-01-01T00:00:00Z',
        deferred: 'Cache directory could not be safely read',
      },
    ],
  })!;
  assert.equal(result.bytes, 110);
  assert.equal(result.partial, true);
  assert.match(result.reasons, /safely read/);
});
test('Live jobs explain why a measured cache is still over its limit', () => {
  const result = cacheStorageSummary({
    cache_storage: [{ ...row, deferred: "A container is still using this pool's cache" }],
  })!;
  assert.equal(result.over, true);
  assert.match(result.reasons, /still using/);
  assert.equal(result.measuredAt, row.measured_at);
});
