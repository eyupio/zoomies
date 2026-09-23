import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Setting } from '../src/lib/api/types.ts';
import { displayValue, settingQuantity } from '../src/lib/settings/settings.ts';

function setting(key: string, kind: Setting['kind'], value: unknown): Setting {
  return { key, kind, value, label: key, section: key.split('.')[0] } as Setting;
}

// The key already names the unit, so a size is shown as a size: 4096 on a
// settings page is a number an operator has to divide in their head.
test('a size setting is shown in the largest unit that says it exactly', () => {
  assert.equal(displayValue(setting('runners.default_memory_mb', 'int', 4096)), '4 GB');
  assert.equal(displayValue(setting('agent.docker_build_cache_mb', 'int', 3000)), '3000 MB');
  assert.equal(displayValue(setting('runners.default_cpus', 'float', 1.5)), '1.5 cores');
});

test('only numeric keys named for a unit are treated as sizes', () => {
  assert.equal(settingQuantity(setting('runners.default_memory_mb', 'int', 0)), 'mb');
  assert.equal(settingQuantity(setting('runners.default_cpus', 'float', 0)), 'cpus');
  assert.equal(settingQuantity(setting('security.rate_limit_logins', 'int', 0)), null);
  // A string that happens to end in _mb is not a number to reformat.
  assert.equal(settingQuantity(setting('something.named_mb', 'string', '')), null);
  assert.equal(displayValue(setting('security.rate_limit_logins', 'int', 10)), '10');
});
