import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Host, Problem } from '../src/lib/api/types.ts';
import { diskProblemFor } from '../src/lib/hosts/disk.ts';
import { healthBucket, healthCounts, hostsFor } from '../src/lib/hosts/health-filter.ts';
import { hostStatus } from '../src/lib/status.ts';
import { newProblems } from '../src/lib/feed/changes.ts';
import { hostVitals } from '../src/lib/hosts/vitals.ts';

const host: Host = {
  id: 'host-a',
  healthy: true,
  disk_total_mb: 144 * 1024,
  disk_free_mb: 34 * 1024,
};
const problem: Problem = {
  code: 'host.low_disk',
  severity: 'error',
  audience: 'fleet',
  title: 'Host is short of disk',
  detail: '18 GiB allocatable; 10 GiB promised; 8 GiB left; one runner needs 10 GiB.',
  target_kind: 'host',
  target_id: 'host-a',
};

test('the controller disk verdict turns a connected host red even without a doctor report', () => {
  const disk = diskProblemFor(host, [problem]);
  assert.equal(disk, problem);
  assert.equal(hostStatus(host, Boolean(disk)).label, 'Disk blocked');
  assert.equal(hostStatus(host, Boolean(disk)).tone, 'danger');
  assert.equal(healthBucket(host, Date.now(), [problem]), 'attention');
  assert.equal(healthCounts([host], Date.now(), [problem]).attention, 1);
  assert.deepEqual(hostsFor([host], 'attention', Date.now(), [problem]), [host]);
  const vital = hostVitals(host, Date.now(), disk).items.find((item) => item.key === 'disk');
  assert.equal(vital?.tight, true);
  assert.match(vital?.summary ?? '', /one runner needs 10 GiB/);
});

// The doctor's rule says why the bar is amber and the controller's problem says
// what that costs placement. A host can be blocked above the doctor's line (the
// test above) or past it, and when it is past it a card that kept only the
// problem would stop saying where the amber comes from.
test('a blocked disk that is also past the doctor line keeps both explanations', () => {
  const low: Host = { ...host, disk_total_mb: 60 * 1024, disk_free_mb: 8 * 1024 };
  const vital = hostVitals(low, Date.now(), problem).items.find((item) => item.key === 'disk');
  assert.equal(vital?.tight, true);
  assert.match(vital?.summary ?? '', /Low: the Work directory free space/);
  assert.match(vital?.summary ?? '', /one runner needs 10 GiB/);
  const plain = hostVitals(low, Date.now()).items.find((item) => item.key === 'disk');
  assert.match(plain?.summary ?? '', /Low: the Work directory free space/);
  assert.doesNotMatch(plain?.summary ?? '', /one runner needs/);
});

test('a different host, an offline host and a deliberate cordon do not inherit a disk fault', () => {
  assert.equal(diskProblemFor({ ...host, id: 'host-b' }, [problem]), undefined);
  assert.equal(diskProblemFor({ ...host, healthy: false }, [problem]), undefined);
  assert.equal(diskProblemFor({ ...host, cordoned: true }, [problem]), undefined);
});

test('disk notifications announce a transition once and announce a recurrence after recovery', () => {
  const previous = new Set<string>();
  assert.equal(newProblems([problem], previous).length, 1);
  assert.equal(
    newProblems([{ ...problem, detail: 'The free space moved slightly.' }], previous).length,
    0,
  );
  assert.equal(newProblems([], previous).length, 0);
  assert.equal(diskProblemFor(host, []), undefined);
  assert.equal(hostStatus(host).label, 'Connected');
  assert.equal(newProblems([problem], previous).length, 1);
});
