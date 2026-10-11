import { test } from 'node:test';
import assert from 'node:assert/strict';
import { DEFAULT_ELI_STYLE, resolveEliStyle } from '../src/lib/state/eli-style';

// How Eli answers is remembered per person. Nothing stored, or a value this
// build no longer offers, is a person who has not chosen and gets the default:
// short, for an operator, because that is the answer that fits on a phone.
test('a browser that has never chosen gets short answers for an operator', () => {
  assert.deepEqual(DEFAULT_ELI_STYLE, { length: 'short', tone: 'balanced' });
  assert.deepEqual(resolveEliStyle(undefined), DEFAULT_ELI_STYLE);
  assert.deepEqual(resolveEliStyle({}), DEFAULT_ELI_STYLE);
  assert.deepEqual(resolveEliStyle({ length: 'epic', tone: 'plain' }), {
    length: 'short',
    tone: 'plain',
  });
  assert.deepEqual(resolveEliStyle({ length: 'long', tone: 'technical' }), {
    length: 'long',
    tone: 'technical',
  });
});
