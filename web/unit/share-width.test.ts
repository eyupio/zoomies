import assert from 'node:assert/strict';
import test from 'node:test';
import { shareWidth, type WidthColumn } from '../src/lib/actions/shareWidth.ts';

const sum = (widths: readonly number[]): number =>
  widths.reduce((total, width) => total + width, 0);

// A grid that stops short of its frame leaves a blank strip on a wide screen.
// The spare is shared in the proportions the columns already have, so a wide
// name column stays the widest rather than every column growing by the same.
test('the spare width is shared among the columns in proportion to their widths', () => {
  const widths = shareWidth([{ width: 100 }, { width: 200 }, { width: 100 }], 800);
  assert.deepEqual(widths, [200, 400, 200]);
});

// The resize handle has to mean what it did: a column dragged to a width is
// drawn at that width, and the others take up the slack.
test('a column sized by hand keeps its width while the others take the spare', () => {
  const widths = shareWidth([{ width: 100 }, { width: 150, hand: true }, { width: 100 }], 550);
  assert.equal(widths[1], 150);
  assert.deepEqual(widths, [200, 150, 200]);
});

test('when every column was sized by hand the last flexible one takes the spare', () => {
  const columns: WidthColumn[] = [
    { width: 40, fixed: true },
    { width: 100, hand: true },
    { width: 120, hand: true },
    { width: 90, fixed: true },
  ];
  assert.deepEqual(shareWidth(columns, 500), [40, 100, 270, 90]);
});

// The tick and the row actions are as wide as their controls; growing them is
// empty space beside a checkbox.
test('fixed columns never grow', () => {
  const widths = shareWidth(
    [{ width: 40, fixed: true }, { width: 100 }, { width: 96, fixed: true }],
    436,
  );
  assert.equal(widths[0], 40);
  assert.equal(widths[2], 96);
  assert.equal(widths[1], 300);
});

test('a grid of fixed columns alone is left as it is', () => {
  assert.deepEqual(shareWidth([{ width: 40, fixed: true }], 400), [40]);
});

// Over-wide is the operator's choice (or the defaults' own division), and the
// frame scrolls; shrinking anything here would undo a resize behind their back.
test('nothing changes when the columns already fill or overflow the container', () => {
  const columns: WidthColumn[] = [{ width: 300 }, { width: 400, hand: true }];
  assert.deepEqual(shareWidth(columns, 600), [300, 400]);
  assert.deepEqual(shareWidth(columns, 700), [300, 400]);
});

// A browser lays a fixed table out from these numbers, and a total a pixel
// short is the strip coming back; a pixel over is a sideways scroll.
test('rounding never leaves the total a pixel short or over', () => {
  for (const container of [997, 1000, 1001, 1333, 1919, 1920]) {
    const widths = shareWidth(
      [{ width: 40, fixed: true }, { width: 97 }, { width: 131 }, { width: 61.5 }, { width: 77 }],
      container,
    );
    assert.equal(sum(widths), container, `at ${container}px`);
    assert.equal(widths[0], 40);
  }
  const whole = shareWidth([{ width: 10 }, { width: 10 }, { width: 10 }], 100);
  assert.ok(whole.every(Number.isInteger), 'whole widths stay whole');
  assert.equal(sum(whole), 100);
});

// The frame is measured a frame after mount, and reads zero while hidden.
test('a container with no width is a no-op', () => {
  const columns: WidthColumn[] = [{ width: 100 }, { width: 200 }];
  assert.deepEqual(shareWidth(columns, 0), [100, 200]);
  assert.deepEqual(shareWidth(columns, -50), [100, 200]);
  assert.deepEqual(shareWidth(columns, Number.NaN), [100, 200]);
  assert.deepEqual(shareWidth([], 500), []);
});

test('the widths it was given are not changed in place', () => {
  const columns: WidthColumn[] = [{ width: 100 }, { width: 100 }];
  shareWidth(columns, 400);
  assert.deepEqual(columns, [{ width: 100 }, { width: 100 }]);
});
