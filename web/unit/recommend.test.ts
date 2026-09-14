import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  DEFAULT_ASK,
  capacityCeiling,
  diskNotches,
  memoryNotches,
  overcommit,
  recommendedCapacity,
  recommendedReserveCores,
  recommendedReserveDiskMb,
  recommendedReserveMemoryMb,
  runnerAsk,
} from '../src/lib/hosts/recommend.ts';

test('the runner ask is the largest enabled pool asks for, or the default', () => {
  assert.deepEqual(runnerAsk([]), DEFAULT_ASK);
  const ask = runnerAsk([
    { enabled: true, resources: { cpus: 2, memory_mb: 4096 } },
    { enabled: true, resources: { cpus: 4, memory_mb: 2048 } },
    { enabled: false, resources: { cpus: 16, memory_mb: 65536 } },
  ]);
  assert.deepEqual(ask, { cpus: 4, memoryMb: 4096, source: 'pools' });
});

test('the reserve keeps a core and a tenth of memory, and never the whole machine', () => {
  assert.equal(recommendedReserveCores(1), 0);
  assert.equal(recommendedReserveCores(2), 1);
  assert.equal(recommendedReserveCores(16), 1);
  assert.equal(recommendedReserveCores(48), 3);
  assert.equal(recommendedReserveMemoryMb(1024), 0);
  assert.equal(recommendedReserveMemoryMb(1536), 1024);
  assert.equal(recommendedReserveMemoryMb(32768), 3072);
  // A tenth of sixteen gigabytes is between two notches; the next one up.
  assert.equal(recommendedReserveMemoryMb(16384), 2048);
  assert.equal(recommendedReserveMemoryMb(262144), 8192);
});

test('the disk reserve is a tenth of the filesystem, on a notch the slider stops at', () => {
  // Nothing worth holding back on a disk barely larger than the floor.
  assert.equal(recommendedReserveDiskMb(4096), 0);
  assert.deepEqual(diskNotches(4096), [0]);
  // 100 GB: a tenth is 10 GB, and 8 GB is the notch nearest it.
  assert.equal(recommendedReserveDiskMb(102_400), 8192);
  // Capped, so a terabyte does not hold back a hundred gigabytes.
  assert.equal(recommendedReserveDiskMb(1_048_576), 65_536);
  // The notches double from the scheduler's own floor, and never reach the
  // whole disk -- a reserve that leaves nothing to place on is refused.
  assert.deepEqual(diskNotches(65_536), [0, 2048, 4096, 8192, 16_384, 32_768]);
  assert.ok((diskNotches(65_536).at(-1) ?? 0) < 65_536);
});

test('capacity is what fits after the reserve, on the tighter of CPU and memory', () => {
  const host = { cpus: 16, memoryMb: 32768 };
  // 15 cores / 2 = 7; 29.7 GB / 4 GB = 7.
  assert.equal(recommendedCapacity(host, 1, 3072, DEFAULT_ASK), 7);
  // Memory is the tighter one here.
  assert.equal(recommendedCapacity(host, 1, 3072, { cpus: 1, memoryMb: 8192, source: 'pools' }), 3);
  // A host that has said nothing about itself gets no recommendation.
  assert.equal(recommendedCapacity({ cpus: 0, memoryMb: 0 }, 0, 0, DEFAULT_ASK), 0);
  // Never zero on a machine, however small: one runner is what a host is for.
  assert.equal(recommendedCapacity({ cpus: 2, memoryMb: 2048 }, 1, 1024, DEFAULT_ASK), 1);
});

test('the sliders reach far enough to include what is set today', () => {
  assert.equal(capacityCeiling({ cpus: 16, memoryMb: 32768 }, 7, 6), 16);
  assert.equal(capacityCeiling({ cpus: 16, memoryMb: 32768 }, 7, 20), 20);
  assert.equal(capacityCeiling({ cpus: 0, memoryMb: 0 }, 0, 3), 8);
  assert.deepEqual(memoryNotches(4096), [0, 512, 1024, 2048, 3072]);
  assert.equal(memoryNotches(65536).at(-1), 49152);
});

test('overcommit says how far past the room the runners would reach', () => {
  const host = { cpus: 8, memoryMb: 16384 };
  assert.deepEqual(overcommit(host, 3, 1, 1024, DEFAULT_ASK), { cpus: 0, memoryMb: 0 });
  assert.deepEqual(overcommit(host, 6, 1, 1024, DEFAULT_ASK), { cpus: 5, memoryMb: 9216 });
});
