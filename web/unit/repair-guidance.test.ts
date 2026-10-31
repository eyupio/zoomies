import { test } from 'node:test';
import assert from 'node:assert/strict';
import { guidanceFor } from '../src/lib/repairs/guidance.ts';
import { repair } from './repair-fixtures.ts';

// A pause written as two hyphens between spaces is not a dash either; the check is built
// from parts so this file does not carry the very thing it forbids.
const SPACED_DOUBLE_HYPHEN = [' ', '-', '-', ' '].join('');

const failed = (code: string) => guidanceFor({ state: 'failed', details: { failure_code: code } });
const text = (g: { headline: string; steps: string[] }) => [g.headline, ...g.steps].join(' ');

// "What to try next" is the part of a failed attempt a person reads and acts on,
// so each failure code says what happened in one line and at most three things
// to do about it.
test('every failure code says what happened and gives at most three things to do', () => {
  for (const code of [
    'unsupported_fix',
    'context_limit',
    'source_unavailable',
    'provider_unavailable',
    'provider_billing',
    'rate_limited',
    'provider_credentials',
    'output_limit',
    'setup_needed',
  ]) {
    const g = failed(code);
    assert.notEqual(g.headline, '', `${code} has no headline`);
    assert.ok(g.steps.length >= 1 && g.steps.length <= 3, `${code} has ${g.steps.length} steps`);
  }
});

test('an unsupported fix starts with a note that points at the file or the test', () => {
  const g = failed('unsupported_fix');
  assert.ok(g.steps[0]?.startsWith('Add a note'), g.steps[0] ?? 'no steps');
  assert.match(text(g), /add a note/i);
  assert.match(text(g), /rerun/i);
});

test('each code names the one thing that fixes it', () => {
  assert.match(text(failed('context_limit')), /too big/i);
  assert.match(text(failed('context_limit')), /file or the test/i);
  assert.match(text(failed('source_unavailable')), /Contents permission/);
  assert.match(text(failed('source_unavailable')), /100 files/);
  assert.match(text(failed('provider_unavailable')), /Eli settings/);
  assert.match(text(failed('provider_billing')), /cannot pay/i);
  assert.match(text(failed('provider_billing')), /credit/i);
  assert.match(text(failed('provider_billing')), /another provider/i);
  assert.match(text(failed('rate_limited')), /quota/i);
  assert.match(text(failed('provider_credentials')), /key/i);
  assert.match(text(failed('provider_credentials')), /Eli settings/);
  assert.match(text(failed('output_limit')), /reasoning effort/i);
  assert.match(text(failed('setup_needed')), /repair settings/i);
  assert.match(text(failed('setup_needed')), /ask again/i);
});

// An attempt that pushed a commit and still failed its checks has no failure
// code, only a terminal state, and the advice is about the commit it made.
test('a repair with no code is read from its state', () => {
  const failedChecks = guidanceFor(repair({ state: 'checks_failed' }));
  assert.match(text(failedChecks), /commit/i);
  assert.match(text(failedChecks), /new head|next attempt|another attempt/i);
  for (const state of ['interrupted', 'unverified', 'superseded', 'blocked']) {
    const g = guidanceFor(repair({ state }));
    assert.notEqual(g.headline, '', `${state} has no headline`);
    assert.equal(g.steps.length, 1, `${state} should say the one thing to check`);
  }
});

test('a code outranks the state it arrived in', () => {
  assert.match(
    text(guidanceFor(repair({ state: 'failed', details: { failure_code: 'rate_limited' } }))),
    /quota/i,
  );
  assert.match(
    text(guidanceFor(repair({ state: 'blocked', details: { failure_code: 'setup_needed' } }))),
    /repair settings/i,
  );
});

// A controller newer than this build can invent a code. The person still gets
// the advice that is true of any failed repair rather than a blank card.
test('an unknown code, or a failure with none, falls back to the unsupported-fix advice', () => {
  const unsupported = failed('unsupported_fix');
  assert.deepEqual(failed('something_new'), unsupported);
  assert.deepEqual(guidanceFor(repair({ state: 'failed' })), unsupported);
  assert.deepEqual(guidanceFor({ state: 'failed' }), unsupported);
});

// The prose in this repository has no em dash and no double hyphen standing in
// for one, and the copy here is the part a person reads at the worst moment.
test('the copy follows the repository voice', () => {
  const codes = [
    'unsupported_fix',
    'context_limit',
    'source_unavailable',
    'provider_unavailable',
    'provider_billing',
    'rate_limited',
    'provider_credentials',
    'output_limit',
    'setup_needed',
  ];
  const states = ['checks_failed', 'interrupted', 'unverified', 'superseded', 'blocked'];
  const all = [...codes.map(failed), ...states.map((state) => guidanceFor(repair({ state })))];
  for (const g of all) {
    const copy = text(g);
    assert.ok(!copy.includes('\u2014'), `an em dash in: ${copy}`);
    assert.ok(!copy.includes(SPACED_DOUBLE_HYPHEN), `a double hyphen in: ${copy}`);
    for (const american of ['behavior', 'authoriz', 'organiz', 'utiliz']) {
      assert.ok(!copy.toLowerCase().includes(american), `${american} in: ${copy}`);
    }
  }
});
