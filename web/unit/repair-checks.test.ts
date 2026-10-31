import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Schemas } from '../src/lib/api/types.ts';
import {
  allowanceWait,
  blockedHeadline,
  blockingRow,
  checkRows,
} from '../src/lib/repairs/checks.ts';
import { CHECK_ORDER, allOk, readiness, stoppedAt, withoutChecks } from './repair-fixtures.ts';

// A pause written as two hyphens between spaces is not a dash either; the check is built
// from parts so this file does not carry the very thing it forbids.
const SPACED_DOUBLE_HYPHEN = [' ', '-', '-', ' '].join('');

const REPO_URL = 'https://github.com/acme/widgets';
const ACCESS = 'Your linked GitHub account needs write access to this repository.';

test('nine passed checks give nine ok rows, in order, and nothing blocks', () => {
  const rows = checkRows(readiness(), REPO_URL);
  assert.deepEqual(
    rows.map((row) => row.code),
    CHECK_ORDER,
  );
  assert.ok(rows.every((row) => row.status === 'ok' && row.message && row.label));
  assert.ok(rows.every((row) => row.action === undefined));
  assert.equal(blockingRow(rows), undefined);
});

// The server stops at the first check it cannot get past, and says so for the
// rest. A check that was never made is not one that passed, and the row must not
// be drawn as either a pass or a failure.
test('a check that was never reached stays unchecked and carries no action', () => {
  const rows = checkRows(
    readiness({
      ready: false,
      blockers: [{ code: 'access', message: ACCESS }],
      checks: stoppedAt('access', ACCESS),
    }),
    REPO_URL,
  );
  assert.deepEqual(
    rows.map((row) => row.status),
    ['ok', 'ok', 'ok', 'blocked', 'unchecked', 'unchecked', 'unchecked', 'unchecked', 'unchecked'],
  );
  for (const row of rows.slice(4)) {
    assert.equal(row.message, 'Not checked yet.');
    assert.equal(row.action, undefined);
  }
});

test('blocked at write access points at the repository access page on its own host', () => {
  const rows = checkRows(
    readiness({
      ready: false,
      blockers: [{ code: 'access', message: ACCESS }],
      checks: stoppedAt('access', ACCESS),
    }),
    REPO_URL,
  );
  const blocking = blockingRow(rows);
  assert.equal(blocking?.code, 'access');
  assert.equal(blocking?.label, 'Write access');
  assert.equal(blocking?.message, ACCESS);
  assert.deepEqual(blocking?.action, {
    kind: 'link',
    href: 'https://github.com/acme/widgets/settings/access',
    label: 'Manage access on GitHub',
    external: true,
  });

  const enterprise = checkRows(
    readiness({
      ready: false,
      blockers: [{ code: 'access', message: ACCESS }],
      checks: stoppedAt('access', ACCESS),
    }),
    'https://ghe.example.com/acme/site',
  );
  assert.deepEqual(blockingRow(enterprise)?.action, {
    kind: 'link',
    href: 'https://ghe.example.com/acme/site/settings/access',
    label: 'Manage access on GitHub',
    external: true,
  });
});

// The page does not write github.com. With no known host there is nothing to
// link to, and the one thing left to offer is to look again once it is fixed.
test('without a known host the access row offers to check again, never a guessed link', () => {
  const rows = checkRows(
    readiness({
      ready: false,
      blockers: [{ code: 'access', message: ACCESS }],
      checks: stoppedAt('access', ACCESS),
    }),
  );
  assert.deepEqual(blockingRow(rows)?.action, { kind: 'recheck' });
  const hostile = checkRows(
    readiness({
      ready: false,
      blockers: [{ code: 'access', message: ACCESS }],
      checks: stoppedAt('access', ACCESS),
    }),
    'javascript:alert(1)',
  );
  assert.deepEqual(blockingRow(hostile)?.action, { kind: 'recheck' });
});

// Each blocked check says what to do about it, so the card can put the one
// button that helps next to the sentence that explains it.
test('each blocked check has its own way forward', () => {
  const expected: Record<string, unknown> = {
    identity: { kind: 'setup' },
    provider: { kind: 'link', href: '/settings/assistant', label: 'Add a model provider' },
    installation: { kind: 'link', href: '/installations', label: 'Open installations' },
    policy: { kind: 'setup' },
    budget: { kind: 'recheck' },
    pull: { kind: 'choose' },
    checks: { kind: 'recheck' },
    permissions: { kind: 'link', href: '/installations', label: 'Review installation permissions' },
  };
  for (const [code, action] of Object.entries(expected)) {
    const c = code as Schemas['EliReadinessCheck']['code'];
    const rows = checkRows(
      readiness({
        ready: false,
        blockers: [{ code, message: 'It needs something.' }],
        checks: stoppedAt(c, 'It needs something.'),
      }),
      REPO_URL,
    );
    const blocking = blockingRow(rows);
    assert.equal(blocking?.code, code);
    assert.deepEqual(blocking?.action, action, `${code} has the wrong way forward`);
  }
});

// The controller folds a failed attempt to ask GitHub into the check it was making, so an
// outage while the pull request is looked up is the pull request check, blocked. The way
// forward from that is to look again, not to choose another pull request or to open the
// installations, and the card must say that GitHub did not answer.
test('a check blocked because GitHub did not answer is looked at again, whichever check it was making', () => {
  const message = 'GitHub could not resolve this repair target. Try checking readiness again.';
  for (const code of ['installation', 'pull', 'checks'] as const) {
    const rows = checkRows(
      readiness({
        ready: false,
        blockers: [{ code: 'github', message }],
        checks: stoppedAt(code, message, 'github'),
      }),
      REPO_URL,
    );
    const blocking = blockingRow(rows);
    // It is still that check, so the list keeps its order and its names.
    assert.equal(blocking?.code, code);
    assert.deepEqual(blocking?.action, { kind: 'recheck' }, code);
    assert.equal(blockedHeadline(blocking?.code ?? '', blocking?.blocker), 'GitHub did not answer');
  }
});

test('an allowance that could not be read is looked at again, and is not one that ran out', () => {
  const unreadable = blockingRow(
    checkRows(
      readiness({
        ready: false,
        blockers: [
          { code: 'budget_unavailable', message: 'Repair allowance could not be checked.' },
        ],
        checks: stoppedAt('budget', 'Repair allowance could not be checked.', 'budget_unavailable'),
      }),
      REPO_URL,
    ),
  );
  assert.deepEqual(unreadable?.action, { kind: 'recheck' });
  assert.equal(
    blockedHeadline(unreadable?.code ?? '', unreadable?.blocker),
    'Could not check the allowance',
  );
  const spent = blockingRow(
    checkRows(
      readiness({
        ready: false,
        blockers: [{ code: 'budget', message: 'The repair allowance is exhausted.' }],
        checks: stoppedAt('budget', 'The repair allowance is exhausted.', 'budget'),
      }),
      REPO_URL,
    ),
  );
  assert.equal(blockedHeadline(spent?.code ?? '', spent?.blocker), 'No attempts left right now');
});

// A spent allowance is not a wall: the page says when a place frees, as a time
// of day and as how long, so a person can plan instead of pressing Check again
// at random. The limit is rolling, so "today" is not promised.
test('a spent allowance says when the next attempt can start', () => {
  const now = new Date(2026, 9, 31, 10, 0, 0).getTime();
  const later = new Date(2026, 9, 31, 12, 30, 0).toISOString();
  const words = allowanceWait(later, now);
  assert.match(words, /12:30/);
  assert.match(words, /in 3h/);
  assert.doesNotMatch(words, /tomorrow/);

  const tomorrow = new Date(2026, 10, 1, 8, 5, 0).toISOString();
  assert.match(allowanceWait(tomorrow, now), /tomorrow 8:05/);
});

test('an allowance with no free instant still says the one true thing', () => {
  assert.equal(
    allowanceWait(undefined),
    'The next attempt can start once the oldest attempt leaves the 24-hour window.',
  );
  assert.equal(
    allowanceWait('not a time'),
    'The next attempt can start once the oldest attempt leaves the 24-hour window.',
  );
});

// A controller older than this build sends no blocker with a check, and its rows read as
// they always did: by the check they belong to.
test('a blocked check that does not say why is read by the check it is', () => {
  const rows = checkRows(
    readiness({
      ready: false,
      blockers: [{ code: 'pull', message: 'No open pull request.' }],
      checks: stoppedAt('pull', 'No open pull request.'),
    }),
    REPO_URL,
  );
  const blocking = blockingRow(rows);
  assert.deepEqual(blocking?.action, { kind: 'choose' });
  assert.equal(blockedHeadline(blocking?.code ?? '', blocking?.blocker), 'Choose a pull request');
});

test('a pull request that is not eligible can be chosen instead', () => {
  const rows = checkRows(
    readiness({
      ready: false,
      blockers: [{ code: 'pull', message: 'No open pull request.' }],
      checks: stoppedAt('pull', 'No open pull request.'),
    }),
    REPO_URL,
  );
  assert.equal(blockingRow(rows)?.label, 'Pull request');
  assert.deepEqual(blockingRow(rows)?.action, { kind: 'choose' });
});

// A tab left open across an upgrade can meet a controller that answers more
// than this build knows. The row keeps the controller's code as its name rather
// than showing nothing, and a blocked one still offers to look again.
test('a check this build does not know keeps its code as its label', () => {
  const unknown = {
    code: 'quantum',
    status: 'blocked',
    message: 'The qubits decohered.',
  } as unknown as Schemas['EliReadinessCheck'];
  const rows = checkRows(
    readiness({ ready: false, checks: [...allOk().slice(0, 8), unknown] }),
    REPO_URL,
  );
  assert.equal(rows[8]?.code, 'quantum');
  assert.equal(rows[8]?.label, 'quantum');
  assert.equal(rows[8]?.message, 'The qubits decohered.');
  assert.deepEqual(rows[8]?.action, { kind: 'recheck' });
  assert.equal(blockingRow(rows)?.code, 'quantum');
});

// A controller older than this build answers with blockers and no checks. The
// rows are made from the blockers, in the order the checks are made, so the card
// still names what stands in the way.
test('a readiness with no checks falls back to rows made from its blockers', () => {
  const older = withoutChecks(
    readiness({
      ready: false,
      blockers: [
        { code: 'access', message: ACCESS },
        { code: 'provider', message: 'Add an enabled personal model provider in Eli settings.' },
        { code: 'closed', message: 'This pull request is closed.' },
        {
          code: 'github',
          message: 'GitHub could not check whether this failure still needs fixing.',
        },
      ],
    }),
  );
  const rows = checkRows(older, REPO_URL);
  assert.deepEqual(
    rows.map((row) => [row.code, row.status]),
    [
      ['provider', 'blocked'],
      ['access', 'blocked'],
      ['pull', 'blocked'],
      ['github', 'blocked'],
    ],
  );
  // GitHub not answering is a reason to look again, not a missing installation.
  const outage = rows.find((row) => row.code === 'github');
  assert.equal(outage?.label, 'GitHub');
  assert.deepEqual(outage?.action, { kind: 'recheck' });
  assert.equal(blockedHeadline('github'), 'GitHub did not answer');
  assert.equal(rows[0]?.message, 'Add an enabled personal model provider in Eli settings.');
  assert.equal(blockingRow(rows)?.code, 'provider');

  assert.deepEqual(checkRows(withoutChecks(readiness()), REPO_URL), []);
});

// The card beside the checklist says the one thing to do about the first check
// that blocks, in words that name the thing rather than the code.
test('each blocked check has a headline for the decision card', () => {
  for (const code of CHECK_ORDER) {
    const headline = blockedHeadline(code);
    assert.notEqual(headline, '', code);
    assert.notEqual(headline, code, `${code} has no wording`);
    assert.ok(!headline.includes('\u2014') && !headline.includes(SPACED_DOUBLE_HYPHEN), headline);
  }
  assert.equal(blockedHeadline('access'), 'Fix write access first');
  assert.equal(blockedHeadline('quantum'), 'Something needs your attention');
});
