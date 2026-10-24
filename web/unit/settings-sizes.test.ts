import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Setting } from '../src/lib/api/types.ts';
import {
  SECTION_BLURB,
  SECTION_TITLES,
  displayValue,
  sectionTitle,
  settingQuantity,
} from '../src/lib/settings/settings.ts';

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

test('a number is a size where its key names the unit, and a count otherwise', () => {
  assert.equal(settingQuantity(setting('runners.default_memory_mb', 'int', 0)), 'mb');
  assert.equal(settingQuantity(setting('runners.default_cpus', 'float', 0)), 'cpus');
  assert.equal(settingQuantity(setting('security.rate_limit_logins', 'int', 0)), 'count');
  assert.equal(settingQuantity(setting('retention.jobs', 'duration', '720h')), 'ms');
  // A string that happens to end in _mb is not a number to reformat.
  assert.equal(settingQuantity(setting('something.named_mb', 'string', '')), null);
  assert.equal(displayValue(setting('security.rate_limit_logins', 'int', 10)), '10');
});

// Go writes a retention period in hours; the person reading it counts days.
test('a duration setting is shown in the largest units that say it exactly', () => {
  assert.equal(displayValue(setting('retention.jobs', 'duration', '720h')), '30d');
  assert.equal(displayValue(setting('scheduler.interval', 'duration', '10s')), '10s');
  assert.equal(displayValue(setting('scheduler.provision_timeout', 'duration', '1h30m')), '1h 30m');
});

// The blurb sits above updates.mode on the Configuration page, and a release
// that only shows what a mode would take must not tell an operator the mode
// does it. The Go side carries the one sentence saying so; this one stays in
// the conditional so it is true before and after the installing arrives.
test('the Updates group describes what the mode would do and promises nothing', () => {
  const blurb = SECTION_BLURB.updates ?? '';
  assert.match(blurb, /what it would do about a newer one/);
  assert.match(blurb, /offer an Update button/);
  assert.doesNotMatch(blurb, /what it does about/);
  assert.doesNotMatch(blurb, /an update on its own/);
});

// The key is the first half of a dotted setting and is the right word for a
// terminal, not for a heading. An unknown section keeps its key rather than
// disappearing from the page, which is what a section added to the API would
// otherwise do.
test('a configuration section is named for a person, with its key as the fallback', () => {
  assert.equal(sectionTitle('oidc'), 'Single sign-on');
  assert.equal(sectionTitle('capacity_demand'), 'Capacity demand');
  assert.equal(sectionTitle('a_future_section'), 'a_future_section');
});

// A heading with no sentence under it makes an operator open a section to find
// out whether it is the one they want. Every named section says what it is for.
test('every named configuration section also says what it is for', () => {
  for (const [key, title] of Object.entries(SECTION_TITLES)) {
    assert.ok(title.trim(), `${key} has a heading`);
    assert.ok((SECTION_BLURB[key] ?? '').trim(), `${key} has a blurb`);
  }
});
