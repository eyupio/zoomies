import { test } from 'node:test';
import assert from 'node:assert/strict';
import { areaPath, linePath, ringFraction } from '../src/lib/command/chart.ts';

test('a line runs from the left edge to the right edge', () => {
  assert.equal(linePath([0, 10], 100, 50, 10), 'M0 50 L100 0');
});

test('one sample is a flat line, and none is an empty path', () => {
  assert.equal(linePath([5], 100, 50, 10), 'M0 25 L100 25');
  assert.equal(linePath([], 100, 50, 10), '');
});

test('a zero maximum does not divide by zero', () => {
  assert.equal(linePath([0, 0], 100, 50, 0), 'M0 50 L100 50');
});

// A value over the maximum would draw above the chart's top edge and out of
// its box, so the line is held to the frame rather than clipped by the SVG.
test('a value past the maximum is drawn at the top, not above it', () => {
  assert.equal(linePath([20], 100, 50, 10), 'M0 0 L100 0');
});

test('points between the edges are spaced evenly', () => {
  assert.equal(linePath([0, 5, 10], 100, 50, 10), 'M0 50 L50 25 L100 0');
});

test('the area under a line closes along the bottom edge', () => {
  assert.equal(areaPath([0, 10], 100, 50, 10), 'M0 50 L100 0 L100 50 L0 50 Z');
  assert.equal(areaPath([], 100, 50, 10), '');
});

test('the ring is empty with no slots and never past full', () => {
  assert.equal(ringFraction(3, 0), 0);
  assert.equal(ringFraction(9, 6), 1);
});

test('the ring is the share of slots in use, and never below empty', () => {
  assert.equal(ringFraction(3, 6), 0.5);
  assert.equal(ringFraction(-1, 6), 0);
});
