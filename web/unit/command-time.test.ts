import { test } from 'node:test';
import assert from 'node:assert/strict';
import { elapsedMs } from '../src/lib/command/time.ts';

const now = Date.parse('2026-11-10T14:00:00Z');

test('a time in the future is no time at all, not a negative one', () => {
  assert.equal(elapsedMs('2026-11-10T14:00:10Z', now), 0);
});

test('a missing or unreadable time is null', () => {
  assert.equal(elapsedMs(undefined, 0), null);
  assert.equal(elapsedMs('not a date', 0), null);
});

test('a past time is the milliseconds since it', () => {
  assert.equal(elapsedMs('2026-11-10T13:59:00Z', now), 60_000);
});
