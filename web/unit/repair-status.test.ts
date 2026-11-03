import { test } from 'node:test';
import assert from 'node:assert/strict';
import { automaticStatus, repairSetupStatus, repairStatus } from '../src/lib/status.ts';
import { stateLabel } from '../src/lib/repairs/history.ts';

// A repair's state is drawn the way every other status in the product is: a
// tone from the fixed set, a shape so that colour is never the only cue, and one
// word. The pairs below are the mapping the design settled on.
test('each repair state has its tone and shape', () => {
  const expected: [string, string, string][] = [
    ['queued', 'pending', 'dashed'],
    ['working', 'busy', 'filled'],
    ['checking', 'busy', 'filled'],
    ['succeeded', 'idle', 'filled'],
    ['checks_failed', 'danger', 'triangle'],
    ['failed', 'danger', 'triangle'],
    ['blocked', 'danger', 'triangle'],
    ['interrupted', 'danger', 'triangle'],
    ['superseded', 'neutral', 'square'],
    ['unverified', 'pending', 'hollow'],
  ];
  for (const [state, tone, shape] of expected) {
    const status = repairStatus(state);
    assert.equal(status.tone, tone, `${state} tone`);
    assert.equal(status.shape, shape, `${state} shape`);
    assert.equal(status.key, state);
    assert.ok(status.icon, `${state} has no icon`);
  }
});

// The history already names every state, and a badge that disagreed with the
// words beside it would read as two statuses for one repair.
test('the badge says what the history says', () => {
  for (const state of [
    'queued',
    'working',
    'checking',
    'succeeded',
    'checks_failed',
    'failed',
    'blocked',
    'interrupted',
    'superseded',
    'unverified',
  ]) {
    assert.equal(repairStatus(state).label, stateLabel(state), state);
  }
});

test('a state this build does not know is neutral and keeps its own word', () => {
  const status = repairStatus('nope');
  assert.equal(status.tone, 'neutral');
  assert.equal(status.label, 'nope');
  assert.equal(status.key, 'nope');
  assert.equal(repairStatus('').label, 'Unknown');
});

test('the failures do not share a shape with the neutral and pending states', () => {
  const danger = repairStatus('failed').shape;
  for (const state of ['superseded', 'unverified', 'queued']) {
    assert.notEqual(repairStatus(state).shape, danger, `${state} is drawn like a failure`);
  }
});

// The hub's setup bar is a status like any other, so it comes from the same map:
// ready is the healthy green, needing setup is the amber of something waiting on
// a person, and sign-in being off is neutral because nothing is wrong with it.
test('each state of the hub’s setup bar has its tone and shape', () => {
  const expected: [Parameters<typeof repairSetupStatus>[0], string, string, string][] = [
    ['ready', 'idle', 'filled', 'Ready to repair'],
    ['needed', 'pending', 'dashed', 'Setup needed'],
    ['off', 'neutral', 'square', 'Sign-in is off'],
    ['loading', 'neutral', 'hollow', 'Checking setup'],
  ];
  for (const [state, tone, shape, label] of expected) {
    const status = repairSetupStatus(state);
    assert.equal(status.tone, tone, `${state} tone`);
    assert.equal(status.shape, shape, `${state} shape`);
    assert.equal(status.label, label, `${state} label`);
    assert.ok(status.icon, `${state} has no icon`);
  }
});

// The health of a repository that repairs itself says whether a person has to do
// something: a provider that cannot pay is the red of something to fix, and a
// working one is plain green. Each has a shape of its own, so colour is never
// the only cue.
test('each health of an automatic repository has its tone, shape, word and hint', () => {
  const expected: [Parameters<typeof automaticStatus>[0], string, string, string][] = [
    ['ready', 'idle', 'filled', 'Ready'],
    ['problem', 'danger', 'triangle', 'Needs attention'],
  ];
  for (const [state, tone, shape, label] of expected) {
    const status = automaticStatus(state);
    assert.equal(status.key, state);
    assert.equal(status.tone, tone, `${state} tone`);
    assert.equal(status.shape, shape, `${state} shape`);
    assert.equal(status.label, label, `${state} label`);
    assert.ok(status.hint, `${state} has a hint`);
    assert.ok(status.icon, `${state} has an icon`);
  }
});
