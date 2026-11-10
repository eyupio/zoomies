import assert from 'node:assert/strict';
import test from 'node:test';
import { GO_KEYS } from '../src/lib/keys.ts';
import { SECTIONS } from '../src/lib/shell/sections.ts';

// The command centre and the Overview answer the same question at different
// distances, so they sit together at the top, and `g d` is the letter left
// free (`c` already goes to AI Context).
test('the command centre is the second section, in no group, on g d', () => {
  assert.equal(SECTIONS[1]?.path, '/command');
  assert.equal(SECTIONS[1]?.label, 'Command centre');
  assert.equal(SECTIONS[1]?.group, undefined);
  assert.equal(SECTIONS[1]?.key, 'd');
  assert.deepEqual(GO_KEYS[1], { key: 'd', path: '/command', label: 'Command centre' });
});

// A chord two pages share opens whichever is listed first, and the other one
// can never be reached from the keyboard.
test('no two sections and no two go keys share a letter', () => {
  assert.equal(new Set(GO_KEYS.map((k) => k.key)).size, GO_KEYS.length);
  assert.equal(new Set(SECTIONS.map((s) => s.key)).size, SECTIONS.length);
});

// Whether the route is immersive rather than bare is held by
// tests/command-centre.spec.ts: router.ts reaches a `.svelte.ts` module, which
// this runner cannot load.
