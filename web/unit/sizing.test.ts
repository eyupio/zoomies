import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  CACHE_NOTCHES,
  CPU_NOTCHES,
  MEMORY_NOTCHES,
  cacheBytes,
  cacheGb,
  chargedSize,
  cpuLabel,
  gbLabel,
  memoryLabel,
  nearest,
  roomOn,
  sizeLabel,
  withValue,
} from '../src/lib/pools/sizing.ts';

// The floors the API enforces have to be on the sliders, or the control offers
// a value the server refuses and the wizard argues with itself.
test('the notches start at what a runner needs to be a runner', () => {
  assert.equal(CPU_NOTCHES[0], 0.25);
  assert.equal(MEMORY_NOTCHES[0], 512);
  assert.equal(CACHE_NOTCHES[0], 0);
});

test('a value the notches do not have joins them rather than snapping', () => {
  // A pool created through the API can hold 3000 MB. Rounding it to 3072 the
  // moment its page opens would change a pool somebody came to read.
  assert.deepEqual(withValue([1024, 2048, 4096], 3000), [1024, 2048, 3000, 4096]);
  assert.deepEqual(withValue([1024, 2048], 2048), [1024, 2048]);
  assert.deepEqual(withValue([1024, 2048], 0), [1024, 2048]);
});

test('the nearest notch is where a figure typed elsewhere lands', () => {
  assert.equal(nearest(MEMORY_NOTCHES, 3800), 4096);
  assert.equal(nearest(CPU_NOTCHES, 2.4), 2);
});

test('a cache limit round-trips between the slider and the bytes the API takes', () => {
  assert.equal(cacheBytes(10), 10 * 1024 * 1024 * 1024);
  assert.equal(cacheGb(10 * 1024 * 1024 * 1024), 10);
  assert.equal(cacheGb(0), 0);
  assert.equal(cacheGb(undefined), 0);
  // A limit typed off the notches is kept as typed, not moved to the nearest.
  assert.equal(cacheGb(7 * 1024 * 1024 * 1024), 7);
});

test('the words say what the figure means rather than repeating it', () => {
  assert.equal(cpuLabel(0.5), 'half a core');
  assert.equal(cpuLabel(1), '1 core');
  assert.equal(cpuLabel(2), '2 cores');
  assert.equal(memoryLabel(512), '512 MB');
  assert.equal(memoryLabel(4096), '4 GB');
  assert.equal(memoryLabel(0), 'none');
  assert.equal(gbLabel(0), 'no limit');
  assert.equal(sizeLabel(2, 4096), '2 cores and 4 GB');
});

// The sidecar gets the same limits as the runner, so a docker-in-docker pool
// asking for two cores puts four on the machine. It is the figure nothing else
// on the form shows.
test('a pool that gives its jobs a daemon is charged for the pair', () => {
  assert.deepEqual(chargedSize(2, 4096, 'dind'), { cpus: 4, memoryMb: 8192, pair: true });
  assert.deepEqual(chargedSize(2, 4096, 'none'), { cpus: 2, memoryMb: 4096, pair: false });
});

test('the room is what both figures allow, and never negative', () => {
  assert.equal(roomOn({ cpus: 9.5, memoryMb: 19968 }, { cpus: 2, memoryMb: 4096 }), 4);
  // Memory is the tighter of the two here, and the answer follows it.
  assert.equal(roomOn({ cpus: 16, memoryMb: 8192 }, { cpus: 1, memoryMb: 4096 }), 2);
  assert.equal(roomOn({ cpus: 2, memoryMb: 2048 }, { cpus: 8, memoryMb: 16384 }), 0);
  // A machine that has measured nothing constrains nothing, and the caller
  // falls back to the host's slots.
  assert.equal(roomOn({ cpus: 0, memoryMb: 0 }, { cpus: 2, memoryMb: 4096 }), 0);
});
