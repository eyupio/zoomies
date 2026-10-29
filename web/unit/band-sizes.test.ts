import { test } from 'node:test';
import assert from 'node:assert/strict';
import { BAND_PAGES, BAND_SIZES, defaultBandSize, isBandSize } from '../src/lib/bands/pages.ts';

// The sizes are the only thing the first load shows differently from before, so they are
// held to what the pages looked like: the Overview's full stage, everything else the
// shortest cut, because a working page's rows are the point and compact still put the
// first of them below a 900px window's fold.
test('the Overview opens on a standard band and every other page on a slim one', () => {
  assert.equal(defaultBandSize('overview'), 'standard');
  for (const page of BAND_PAGES) {
    if (page.key !== 'overview') assert.equal(defaultBandSize(page.key), 'slim', page.key);
  }
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
