import { test } from 'node:test';
import assert from 'node:assert/strict';
import { areaPath, cpuShare, linePath, ringFraction } from '../src/lib/command/chart.ts';

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

// cpu_percent counts a whole CPU as 100, as the container runtime reports it,
// so a runner allocated two CPUs and using both is at 200 and full.
test("a runner's CPU bar is its use over the CPUs it was given, and never past full", () => {
  assert.equal(cpuShare({ cpu_percent: 100, allocated_cpus: 2 }), 0.5);
  assert.equal(cpuShare({ cpu_percent: 350, allocated_cpus: 2 }), 1);
  assert.equal(cpuShare({ cpu_percent: 50 }), 0.5);
});

test('a runner that has not reported its CPU has no bar to draw', () => {
  assert.equal(cpuShare({ allocated_cpus: 2 }), null);
  assert.equal(cpuShare({ cpu_percent: -5, allocated_cpus: 2 }), 0);
});
