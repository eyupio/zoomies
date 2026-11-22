import assert from 'node:assert/strict';
import test from 'node:test';
import { FrameLayer, restoreUnlessMoved } from '../src/lib/frame-layer.ts';

interface Row {
  id: string;
  state: string;
}
const row = (id: string, state: string): Row => ({ id, state });

// A read is asked for, a frame says the fleet has moved, and then the read
// lands carrying what was true when it was asked. Replacing the panel with it
// puts back a job that finished, which no later frame corrects, because the
// frame that would have was the one that has just been overwritten.
test('a read that lands after a frame does not undo the frame', () => {
  const layer = new FrameLayer<Row>();
  const mark = layer.begin();
  layer.note(row('job_1', 'completed'));
  const shown = layer.settle(
    mark,
    [row('job_1', 'in_progress'), row('job_2', 'in_progress')],
    (r) => r.state === 'in_progress',
  );
  assert.deepEqual(shown, [row('job_2', 'in_progress')]);
});

test('a frame for a job the read did not return is added when it belongs', () => {
  const layer = new FrameLayer<Row>();
  const mark = layer.begin();
  layer.note(row('job_3', 'in_progress'));
  const shown = layer.settle(mark, [row('job_2', 'in_progress')], (r) => r.state === 'in_progress');
  assert.deepEqual(shown.map((r) => r.id).sort(), ['job_2', 'job_3']);
});

test('only the latest frame for a job counts, and one from before the read was asked for does not', () => {
  const layer = new FrameLayer<Row>();
  layer.note(row('job_1', 'queued'));
  const mark = layer.begin();
  layer.note(row('job_1', 'in_progress'));
  layer.note(row('job_1', 'completed'));
  const shown = layer.settle(mark, [row('job_1', 'queued'), row('job_9', 'queued')], () => true);
  assert.deepEqual(shown, [row('job_1', 'completed'), row('job_9', 'queued')]);

  const after = layer.begin();
  assert.deepEqual(
    layer.settle(after, [row('job_1', 'queued')], () => true),
    [row('job_1', 'queued')],
  );
});

test('frames are only kept while a read is in flight', () => {
  const layer = new FrameLayer<Row>();
  layer.note(row('job_1', 'queued'));
  assert.equal(layer.pending, 0);
  const mark = layer.begin();
  layer.note(row('job_2', 'queued'));
  assert.equal(layer.pending, 1);
  layer.end(mark);
  assert.equal(layer.pending, 0);
});

// The optimistic change is taken back when the request is refused, but only if
// the row is still the one the change made: a frame that arrived meanwhile is
// newer than the row the change was made from, and putting that one back would
// show something the server has already moved on from.
test('a refused change is taken back only while the row is still the change', () => {
  const before = row('run_1', 'idle');
  const applied = { ...before, state: 'draining' };
  assert.equal(restoreUnlessMoved(applied, applied, before), before);
  const frame = row('run_1', 'removed');
  assert.equal(restoreUnlessMoved(frame, applied, before), frame);
  assert.equal(restoreUnlessMoved(undefined, applied, before), undefined);
});
