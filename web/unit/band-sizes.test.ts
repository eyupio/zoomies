import { test } from 'node:test';
import assert from 'node:assert/strict';
import { BAND_PAGES, BAND_SIZES, defaultBandSize, isBandSize } from '../src/lib/bands/pages.ts';

// Every page opens on the shortest cut: a band is a summary, and the rows under it are
// the point. Compact still put the first row below a 900px window's fold.
test('every page opens on a slim band', () => {
  for (const page of BAND_PAGES) assert.equal(defaultBandSize(), 'slim', page.key);
});

// The control reads as a scale, so the order is part of the choice, not an accident of
// which size happened to be added first.
test('the four cuts are sizes, in order from the shortest band to the tallest', () => {
  assert.deepEqual(
    BAND_SIZES.map((size) => size.key),
    ['slim', 'compact', 'standard', 'expanded'],
  );
  for (const size of BAND_SIZES) assert.ok(isBandSize(size.key), size.key);
  assert.ok(!isBandSize('huge'));
  assert.ok(!isBandSize(undefined));
});
