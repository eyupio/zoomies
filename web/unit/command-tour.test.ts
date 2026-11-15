import { test } from 'node:test';
import assert from 'node:assert/strict';
import { shown, stopAt, stops, tourLabel, type Span } from '../src/lib/command/tour.ts';

/** Lanes of the given heights, a gap of 16 between them, as the deck lays them. */
function laid(heights: readonly number[]): { spans: Span[]; content: number } {
  const spans: Span[] = [];
  let top = 0;
  for (const height of heights) {
    spans.push({ top, bottom: top + height });
    top += height + 16;
  }
  return { spans, content: top - 16 };
}

// The case that has no tour at all: nothing moves on a wall whose fleet fits.
test('a fleet that fits has one stop, at the top', () => {
  const { spans, content } = laid([100, 100, 100]);
  assert.deepEqual(stops(spans, 400, content), [0]);
  assert.deepEqual(stops([], 400, 0), [0]);
});

// A page that began mid-lane would be read through a cut edge, which is the
// fault the tour exists to remove.
test('each page begins at the top of the first lane the last page could not finish', () => {
  const { spans, content } = laid([100, 100, 100, 100, 100, 100, 100, 100]);
  // 300 holds two whole lanes (216) and not three (332).
  assert.deepEqual(stops(spans, 300, content), [0, 232, 464, content - 300]);
});

test('the last page is the end of the content, full, not one lane over an empty deck', () => {
  const { spans, content } = laid([100, 100, 100]);
  const all = stops(spans, 250, content);
  assert.deepEqual(all, [0, content - 250]);
  assert.deepEqual(shown(spans, all[1]!, 250), { first: 2, last: 3 });
});

// Without this the tour would stop for ever on a host with forty runners.
test('a lane taller than the deck gets a page and the tour moves on from it', () => {
  const { spans, content } = laid([100, 900, 100, 100]);
  const all = stops(spans, 300, content);
  assert.equal(all[0], 0);
  assert.equal(all[1], 116);
  assert.equal(all.at(-1), content - 300);
  assert.ok(all.every((stop, i) => i === 0 || stop > all[i - 1]!));
});

// Somebody who scrolled by hand is carried on from where they are, not thrown
// back to the page the timer remembered.
test('a scroll offset belongs to the last stop at or before it', () => {
  const all = [0, 232, 464, 628];
  assert.equal(stopAt(all, 0), 0);
  assert.equal(stopAt(all, 231.6), 1);
  assert.equal(stopAt(all, 300), 1);
  assert.equal(stopAt(all, 9999), 3);
});

test('the lanes on show are the ones more than half inside the deck', () => {
  const { spans } = laid([100, 100, 100, 100]);
  assert.deepEqual(shown(spans, 0, 300), { first: 1, last: 3 });
  assert.deepEqual(shown(spans, 232, 216), { first: 3, last: 4 });
  assert.equal(shown([], 0, 300), null);
});

test('the label counts hosts, and says one host plainly', () => {
  assert.equal(tourLabel({ first: 1, last: 4 }, 9), 'Hosts 1 to 4 of 9');
  assert.equal(tourLabel({ first: 9, last: 9 }, 9), 'Host 9 of 9');
  assert.equal(tourLabel(null, 9), '9 hosts');
});
