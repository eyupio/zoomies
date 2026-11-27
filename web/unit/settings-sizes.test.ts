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
import { formatQuantity, parseQuantity } from '../src/lib/units.ts';

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

// A size in bytes is the one a person cannot read raw: 21474836480 on a settings
// page is a number to divide in the head, and the key already names the unit.
// Multiples are binary, as everywhere else on the page, so "20 GB" here is the
// same 20 GiB a gigabyte field would carry.
test('a key ending in _bytes is a size in bytes, and only a number is', () => {
  assert.equal(settingQuantity(setting('agent.registry_mirror.max_bytes', 'int', 0)), 'bytes');
  assert.equal(settingQuantity(setting('something.named_bytes', 'string', '')), null);
  assert.equal(settingQuantity(setting('something.named_bytes', 'bool', false)), null);
});

test('a size in bytes is shown in the largest unit that says it exactly', () => {
  const show = (n: number) => displayValue(setting('agent.registry_mirror.max_bytes', 'int', n));
  assert.equal(show(1073741824), '1 GB');
  assert.equal(show(10737418240), '10 GB');
  assert.equal(show(21474836480), '20 GB');
  assert.equal(show(1610612736), '1.5 GB');
  assert.equal(show(2199023255552), '2 TB');
  assert.equal(show(0), '0 B');
  assert.equal(show(512), '512 B');
  // Never a rounded figure for a limit that is exact: 2.93 GB would be a
  // different size from the one that is set.
  assert.doesNotMatch(show(3150000000), /GB/);
});

test('what somebody types into a bytes field is read in binary multiples', () => {
  const read = (text: string) => parseQuantity(text, 'bytes');
  assert.deepEqual(read('20g'), { ok: true, value: 21474836480 });
  assert.deepEqual(read('20 GB'), { ok: true, value: 21474836480 });
  assert.deepEqual(read('20 GiB'), { ok: true, value: 21474836480 });
  assert.deepEqual(read('1.5g'), { ok: true, value: 1610612736 });
  assert.deepEqual(read('512m'), { ok: true, value: 536870912 });
  assert.deepEqual(read('2 TB'), { ok: true, value: 2199023255552 });
  assert.deepEqual(read('1 PB'), { ok: true, value: 1125899906842624 });
  // A bare number is bytes, the unit the field is in, so a pasted figure stays itself.
  assert.deepEqual(read('1073741824'), { ok: true, value: 1073741824 });
  assert.deepEqual(read('4b'), { ok: true, value: 4 });
  assert.deepEqual(read(''), { ok: true, value: null });
});

test('what cannot be read as a size in bytes says what would be', () => {
  for (const bad of ['lots', '-1g', '1.5.2g', '20 parsecs']) {
    const parsed = parseQuantity(bad, 'bytes');
    assert.equal(parsed.ok, false, bad);
    if (!parsed.ok) assert.match(parsed.error, /size such as/);
  }
});

test('a size in bytes survives being written and read back', () => {
  for (const n of [0, 1, 1023, 1024, 1073741824, 1610612736, 21474836480, 2199023255552]) {
    const text = formatQuantity(n, 'bytes');
    assert.deepEqual(parseQuantity(text, 'bytes'), { ok: true, value: n }, `${n} -> ${text}`);
  }
});
