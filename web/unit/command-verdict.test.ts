import { test } from 'node:test';
import assert from 'node:assert/strict';
import { verdict, type VerdictInput } from '../src/lib/command/verdict.ts';

const base: VerdictInput = {
  live: true,
  lastHeardAt: 0,
  now: 1000,
  hosts: 3,
  slots: 16,
  busy: 0,
  starting: 0,
  waiting: 0,
  waitingWithoutSlot: 0,
  longestWaitMs: 0,
  waitingPool: null,
  problems: 0,
  firstProblemTarget: null,
};

const said = (input: VerdictInput) =>
  verdict(input)
    .parts.map((p) => p.text)
    .join('');

// The order is the contract: the least trustworthy reading is checked first,
// so a dropped stream is never dressed up as "Busy and healthy".
test('each kind of verdict wins in the order the spec gives', () => {
  const rows: Array<[string, VerdictInput, string, string]> = [
    [
      'stream down beats everything',
      { ...base, live: false, busy: 14, problems: 2 },
      'not-live',
      'Not live.',
    ],
    ['no hosts', { ...base, hosts: 0, slots: 0 }, 'empty', 'No hosts yet.'],
    [
      'open problems beat a queue',
      { ...base, problems: 2, waiting: 3, waitingWithoutSlot: 3 },
      'attention',
      'Needs a look.',
    ],
    [
      'jobs with no slot to go to',
      { ...base, busy: 16, waiting: 2, waitingWithoutSlot: 2 },
      'queue',
      'Busy, with a queue.',
    ],
    [
      'working, and the one waiting job will start soon',
      { ...base, busy: 14, waiting: 1 },
      'busy',
      'Busy and healthy.',
    ],
    ['nothing running or waiting', base, 'quiet', 'Quiet.'],
  ];
  for (const [name, input, kind, headline] of rows) {
    const v = verdict(input);
    assert.equal(v.kind, kind, name);
    assert.equal(v.headline, headline, name);
  }
});

test('the waiting count is a pending link to the queue', () => {
  const v = verdict({
    ...base,
    busy: 14,
    slots: 16,
    waiting: 3,
    waitingWithoutSlot: 2,
    longestWaitMs: 42000,
    waitingPool: 'precision',
  });
  const part = v.parts.find((p) => p.text === '3 jobs wait');
  assert.deepEqual(part, { text: '3 jobs wait', tone: 'pending', href: '/queue' });
  assert.equal(
    v.parts.map((p) => p.text).join(''),
    '14 of 16 slots are working. 3 jobs wait, the longest for 42s, for precision.',
  );
});

test('one job waits and one problem is open are singular', () => {
  const v = verdict({ ...base, busy: 1, waiting: 1, problems: 1 });
  const texts = v.parts.map((p) => p.text);
  assert.ok(texts.includes('1 job waits'));
  assert.ok(texts.includes('1 problem is open'));
  assert.ok(texts.join('').startsWith('1 of 16 slots is working.'));
});

test('a problem count opens the problems drawer and names its first target', () => {
  const v = verdict({ ...base, problems: 1, firstProblemTarget: 'm93p-1' });
  assert.deepEqual(
    v.parts.find((p) => p.opens),
    { text: '1 problem is open', tone: 'danger', opens: 'problems' },
  );
  assert.ok(
    said({ ...base, problems: 1, firstProblemTarget: 'm93p-1' }).endsWith('the first on m93p-1.'),
  );
});

test('not live says when it last heard, and never claims a count', () => {
  const v = verdict({ ...base, live: false, lastHeardAt: 0, now: 95000, busy: 14 });
  assert.equal(v.parts.map((p) => p.text).join(''), 'Last heard from the controller 1m 35s ago.');
});

test('not live with nothing ever heard says it is waiting', () => {
  assert.equal(
    said({ ...base, live: false, lastHeardAt: null }),
    'Waiting to hear from the controller.',
  );
});

test('a quiet fleet says how many slots stand ready', () => {
  assert.equal(said({ ...base, slots: 16 }), '16 slots stand ready.');
  assert.equal(said({ ...base, slots: 1 }), '1 slot stands ready.');
});

test('an empty fleet points at adding a host', () => {
  const v = verdict({ ...base, hosts: 0, slots: 0 });
  assert.equal(
    v.parts.map((p) => p.text).join(''),
    'Add a host to give the fleet somewhere to run.',
  );
  assert.equal(v.parts.find((p) => p.href)?.href, '/hosts/new');
});

test('runners that are starting are said, and a busy fleet that has nothing running says so', () => {
  assert.equal(
    said({ ...base, starting: 2, waiting: 1 }),
    'Nothing is running. 2 runners are starting. 1 job waits.',
  );
});
