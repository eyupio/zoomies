import { test } from 'node:test';
import assert from 'node:assert/strict';
import { REPAIR_TRACK, stageWords, trackIndex, trackSteps } from '../src/lib/repairs/journey.ts';
import { repair } from './repair-fixtures.ts';

const at = (stage: string, state = 'working') => repair({ state, details: { stage } });

// The attempt page draws a running repair as five steps, and the stage the worker
// records is the only thing that says which. The mapping is pinned by the
// worker's own stage ids, so a renamed stage breaks here and not on a person's
// screen.
test('each stage the worker records puts the mark on its step', () => {
  assert.equal(trackIndex(repair({ state: 'queued' })), 0);
  assert.equal(trackIndex(at('evidence')), 1);
  assert.equal(trackIndex(at('source')), 1);
  assert.equal(trackIndex(at('prepare_fix')), 2);
  assert.equal(trackIndex(at('publish')), 3);
  assert.equal(trackIndex(at('watch_ci')), 4);
  assert.equal(trackIndex(repair({ state: 'checking' })), 4);
  // A commit is proof, and a row read between two writes may still say an old stage.
  assert.equal(
    trackIndex(repair({ state: 'working', commit_sha: 'abc1234', details: { stage: 'evidence' } })),
    4,
  );
});

test('the track has the five steps a person waits through, in order', () => {
  assert.deepEqual(
    REPAIR_TRACK.map((step) => step.label),
    [
      'Queued',
      'Reading the failed checks',
      'Preparing the fix',
      'Pushing the commit',
      'Watching CI',
    ],
  );
});

test('the steps before the mark are done, the one it is on is current, and the rest are to come', () => {
  assert.deepEqual(
    trackSteps(at('prepare_fix')).map((step) => step.state),
    ['done', 'done', 'current', 'todo', 'todo'],
  );
  assert.deepEqual(
    trackSteps(repair({ state: 'queued' })).map((step) => step.state),
    ['current', 'todo', 'todo', 'todo', 'todo'],
  );
  assert.deepEqual(
    trackSteps(repair({ state: 'checking', commit_sha: 'abc1234' })).map((step) => step.state),
    ['done', 'done', 'done', 'done', 'current'],
  );
  assert.equal(trackSteps(at('prepare_fix'))[2]?.label, 'Preparing the fix');
});

// The technical details say which stage a repair reached, in the words the track
// uses, and a stage this build has not heard of is still readable.
test('a stage is said in words, and an unknown one is made readable', () => {
  assert.equal(stageWords('evidence'), 'Reading the failed checks');
  assert.equal(stageWords('source'), 'Reading the source');
  assert.equal(stageWords('prepare_fix'), 'Preparing the fix');
  assert.equal(stageWords('publish'), 'Pushing the commit');
  assert.equal(stageWords('watch_ci'), 'Watching CI');
  assert.equal(stageWords('verify_signature'), 'Verify signature');
  assert.equal(stageWords(''), 'Not recorded');
  assert.equal(stageWords(undefined), 'Not recorded');
});

// The attempt page shows the whole sequence rather than only the current step,
// so every stage the worker can report must produce all five steps, in order,
// with a status for each. A step that was hidden until asked for is a step a
// person watching the repair could not see.
test('every stage shows the whole sequence, with a status for each step', () => {
  const labels = REPAIR_TRACK.map((step) => step.label);
  for (const stage of ['', 'evidence', 'source', 'prepare_fix', 'publish', 'watch_ci']) {
    const steps = trackSteps(at(stage));
    assert.deepEqual(
      steps.map((step) => step.label),
      labels,
    );
    assert.equal(steps.length, REPAIR_TRACK.length);
    for (const step of steps) {
      assert.ok(['done', 'current', 'todo', 'stopped'].includes(step.state));
    }
    assert.equal(steps.filter((step) => step.state === 'current').length, 1);
    assert.equal(steps.filter((step) => step.state === 'stopped').length, 0);
  }
});

// A repair that stops mid-way keeps the sequence and names the step it stopped
// on. The steps before it are done, it is stopped rather than in progress or
// done, and the steps after it were never reached.
test('a failed repair marks the step it reached as stopped and leaves the rest unstarted', () => {
  assert.deepEqual(
    trackSteps(at('prepare_fix', 'failed')).map((step) => step.state),
    ['done', 'done', 'stopped', 'todo', 'todo'],
  );
  assert.deepEqual(
    trackSteps(at('publish', 'interrupted')).map((step) => step.state),
    ['done', 'done', 'done', 'stopped', 'todo'],
  );
  assert.deepEqual(
    trackSteps(at('watch_ci', 'failed')).map((step) => step.state),
    ['done', 'done', 'done', 'done', 'stopped'],
  );
  assert.equal(trackSteps(at('prepare_fix', 'failed'))[2]?.label, 'Preparing the fix');
});

// The track is permanent: a repair that has ended still has every step, with a
// final status and nothing left in progress. A successful repair finished all
// the work, even when CI later said the commit failed or its result is unknown.
test('a finished repair keeps the whole sequence with final statuses', () => {
  for (const state of ['succeeded', 'checks_failed', 'unverified']) {
    const steps = trackSteps(at('watch_ci', state));
    assert.equal(steps.length, REPAIR_TRACK.length);
    assert.deepEqual(
      steps.map((step) => step.state),
      ['done', 'done', 'done', 'done', 'done'],
    );
  }
  for (const [stage, state] of [
    ['prepare_fix', 'failed'],
    ['prepare_fix', 'blocked'],
    ['publish', 'interrupted'],
    ['watch_ci', 'superseded'],
  ]) {
    const steps = trackSteps(at(stage, state));
    assert.equal(steps.length, REPAIR_TRACK.length);
    assert.equal(steps.filter((step) => step.state === 'current').length, 0);
    assert.equal(steps.filter((step) => step.state === 'stopped').length, 1);
  }
});
