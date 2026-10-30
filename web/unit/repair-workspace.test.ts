import { test } from 'node:test';
import assert from 'node:assert/strict';
import { changedBlocker, parseTarget, stageOf, targetHref } from '../src/lib/repairs/workspace.ts';
import { allOk, readiness, stoppedAt } from './repair-fixtures.ts';

const query = (href: string) => new URLSearchParams(href.split('?')[1] ?? '');

test('a target is read from the address and written back to it', () => {
  const target = { repo: 'acme/widgets', pull: 17, run: 482, job: 'job_unit', offer: 'opp_1' };
  assert.deepEqual(parseTarget(query(targetHref(target))), target);
  assert.deepEqual(
    parseTarget(new URLSearchParams({ url: 'https://github.com/acme/widgets/pull/17/checks' })),
    {
      repo: 'acme/widgets',
      pull: 17,
      run: 0,
      job: '',
      offer: '',
    },
  );
  assert.deepEqual(parseTarget(new URLSearchParams('')), {
    repo: '',
    pull: 0,
    run: 0,
    job: '',
    offer: '',
  });
});

test('an address is built without the parts that are empty or zero', () => {
  assert.equal(targetHref({}), '/repairs/new');
  assert.equal(
    targetHref({ repo: 'acme/widgets', pull: 0, run: 0, job: '', offer: '' }),
    '/repairs/new?repo=acme%2Fwidgets',
  );
  assert.equal(
    targetHref({ repo: 'acme/widgets', run: 482, job: 'job_unit' }),
    '/repairs/new?repo=acme%2Fwidgets&run=482&job=job_unit',
  );
  assert.equal(targetHref({ offer: 'opp_1' }), '/repairs/new?offer=opp_1');
});

// The address is the page's input and anybody can type into it. A number that
// is not a positive whole number is no number, and a value never reaches the
// page as anything but a string to look up.
test('what the address says is read defensively', () => {
  for (const bad of ['abc', '-4', '0', '1.5', '1e3', '99999999999999999999', '']) {
    const target = parseTarget(new URLSearchParams({ repo: 'a/b', pull: bad, run: bad }));
    assert.equal(target.pull, 0, `pull=${bad}`);
    assert.equal(target.run, 0, `run=${bad}`);
  }
  assert.equal(parseTarget(new URLSearchParams({ repo: '  acme/widgets  ' })).repo, 'acme/widgets');
  assert.equal(parseTarget(new URLSearchParams({ pull: '17' })).pull, 17);
});

test('a target that changed is told apart from one that is only blocked', () => {
  for (const code of ['fixed', 'closed', 'superseded'] as const) {
    const r = readiness({ ready: false, blockers: [{ code, message: `It is ${code}.` }] });
    assert.deepEqual(changedBlocker(r), { code, message: `It is ${code}.` });
    assert.equal(stageOf(r, false), 'changed');
  }
  const blocked = readiness({
    ready: false,
    blockers: [{ code: 'access', message: 'No write access.' }],
    checks: stoppedAt('access', 'No write access.'),
  });
  assert.equal(changedBlocker(blocked), undefined);
  assert.equal(stageOf(blocked, false), 'blocked');
});

test('the stage follows what readiness found', () => {
  assert.equal(stageOf(null, false), 'checking');
  assert.equal(stageOf(readiness({ checks: allOk() }), false), 'ready');
  assert.equal(stageOf(null, true), 'error');
  // A read that failed outranks an answer left over from the last one.
  assert.equal(stageOf(readiness(), true), 'error');
  // An answer that says it is not ready but names no blocker is not ready.
  assert.equal(stageOf(readiness({ ready: false }), false), 'blocked');
});
