import { test } from 'node:test';
import assert from 'node:assert/strict';
import { formatQuantity, goDuration, parseQuantity } from '../src/lib/units.ts';

function value(text: string, quantity: Parameters<typeof parseQuantity>[1]): number | null {
  const parsed = parseQuantity(text, quantity);
  assert.ok(parsed.ok, `${JSON.stringify(text)} was refused`);
  return parsed.value;
}

// Every spelling of four gigabytes an operator reaches for is the same limit.
// A field that took "4096" and refused "4g" taught people to do arithmetic the
// page could have done.
test('the spellings of four gigabytes all mean 4096 MB', () => {
  for (const text of [
    '4gb',
    '4GB',
    '4 GB',
    '4g',
    '4G',
    '4GiB',
    '4 gib',
    '4096mb',
    '4096',
    '4096 MB',
  ]) {
    assert.equal(value(text, 'mb'), 4096, text);
  }
});

test('a fraction of a gigabyte is kept in whole megabytes', () => {
  assert.equal(value('1.5g', 'mb'), 1536);
  assert.equal(value('0.5 GB', 'mb'), 512);
  assert.equal(value('1t', 'mb'), 1024 * 1024);
});

// A bare number means the field's own unit, so a disk figure of 40 is not
// silently forty megabytes.
test('a bare number is read in the unit the field is set in', () => {
  assert.equal(value('40', 'gb'), 40);
  assert.equal(value('1.5 TB', 'gb'), 1536);
  assert.equal(value('512mb', 'gb'), 1);
});

test('CPU takes decimals, cores and millicores', () => {
  assert.equal(value('1.5', 'cpus'), 1.5);
  assert.equal(value('1.5 cores', 'cpus'), 1.5);
  assert.equal(value('2 vCPUs', 'cpus'), 2);
  assert.equal(value('1500m', 'cpus'), 1.5);
  assert.equal(value('.25', 'cpus'), 0.25);
});

// Clearing a field is an answer -- "no limit", "the template decides" -- and
// must not read as a mistake.
test('an empty field is nothing rather than an error', () => {
  assert.deepEqual(parseQuantity('  ', 'mb'), { ok: true, value: null });
});

test('what cannot be read is refused with an example of what can', () => {
  for (const text of ['four gigs', '4 bananas', '-2', '4b', '1..5']) {
    const parsed = parseQuantity(text, 'mb');
    assert.equal(parsed.ok, false, text);
    if (!parsed.ok) assert.match(parsed.error, /4 GB/);
  }
  const cpu = parseQuantity('lots', 'cpus');
  assert.equal(cpu.ok, false);
});

// The field writes back what it understood, in the largest unit that says it
// exactly -- 4096mb becomes "4 GB" -- and never a rounded figure that would be
// a different limit from the one set.
test('a size is written back in the largest unit that says it exactly', () => {
  assert.equal(formatQuantity(4096, 'mb'), '4 GB');
  assert.equal(formatQuantity(1536, 'mb'), '1.5 GB');
  assert.equal(formatQuantity(512, 'mb'), '512 MB');
  assert.equal(formatQuantity(3000, 'mb'), '3000 MB');
  assert.equal(formatQuantity(1024 * 1024, 'mb'), '1 TB');
  assert.equal(formatQuantity(40, 'gb'), '40 GB');
  assert.equal(formatQuantity(2048, 'gb'), '2 TB');
  assert.equal(formatQuantity(1.5, 'cpus'), '1.5 cores');
  assert.equal(formatQuantity(1, 'cpus'), '1 core');
});

test('what the field writes back reads back as the same value', () => {
  for (const mb of [512, 1536, 3000, 4096, 12288, 131072]) {
    assert.equal(value(formatQuantity(mb, 'mb'), 'mb'), mb);
  }
  for (const cpus of [0.25, 1, 1.5, 12]) {
    assert.equal(value(formatQuantity(cpus, 'cpus'), 'cpus'), cpus);
  }
});

// A retention period is a number of days to the person setting it and a
// number of hours to Go. The field takes either, and anything between.
test('a duration reads Go spelling and the one people say', () => {
  const hour = 3600 * 1000;
  assert.equal(value('168h', 'ms'), 168 * hour);
  assert.equal(value('7d', 'ms'), 168 * hour);
  assert.equal(value('7 days', 'ms'), 168 * hour);
  assert.equal(value('1 week', 'ms'), 168 * hour);
  assert.equal(value('1h30m', 'ms'), 1.5 * hour);
  assert.equal(value('1h 30m', 'ms'), 1.5 * hour);
  assert.equal(value('1.5h', 'ms'), 1.5 * hour);
  assert.equal(value('90 seconds', 'ms'), 90 * 1000);
  assert.equal(value('500ms', 'ms'), 500);
  // A bare number is seconds, as runners.docker_wait always meant it.
  assert.equal(value('120', 'ms'), 120 * 1000);
  for (const text of ['soon', '5 fortnights', '1h and 5m', 'h']) {
    assert.equal(parseQuantity(text, 'ms').ok, false, text);
  }
});

test('a duration is written back in the largest units that say it exactly', () => {
  assert.equal(formatQuantity(90 * 1000, 'ms'), '1m 30s');
  assert.equal(formatQuantity(3600 * 1000, 'ms'), '1h');
  assert.equal(formatQuantity(36 * 3600 * 1000, 'ms'), '1d 12h');
  assert.equal(formatQuantity(720 * 3600 * 1000, 'ms'), '30d');
  assert.equal(formatQuantity(500, 'ms'), '500ms');
  assert.equal(formatQuantity(0, 'ms'), '0s');
  for (const ms of [500, 90_000, 5_400_000, 129_600_000, 2_592_000_000]) {
    assert.equal(value(formatQuantity(ms, 'ms'), 'ms'), ms);
  }
});

// The controller reads Go's spelling, which has no day.
test('a duration is sent in the spelling Go reads', () => {
  assert.equal(goDuration(720 * 3600 * 1000), '720h');
  assert.equal(goDuration(90 * 1000), '1m30s');
  assert.equal(goDuration(1500), '1s500ms');
  assert.equal(goDuration(0), '0s');
});

test('a count is a whole number and nothing else', () => {
  assert.equal(value('10', 'count'), 10);
  assert.equal(value('1,000', 'count'), 1000);
  assert.equal(parseQuantity('1.5', 'count').ok, false);
  assert.equal(formatQuantity(12, 'count'), '12');
});
