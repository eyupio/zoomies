import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseRepairTarget } from '../src/lib/repairs/target';

test('PR links identify the repository and number without retyping', () => {
  assert.deepEqual(
    parseRepairTarget(' https://github.com/acme/widgets/pull/42?diff=split#checks '),
    { repo: 'acme/widgets', pull_number: 42 },
  );
  assert.deepEqual(parseRepairTarget('https://github.com/acme/widgets/pull/42/checks'), {
    repo: 'acme/widgets',
    pull_number: 42,
  });
  assert.deepEqual(parseRepairTarget('https://github.com/acme/widgets/pull/42/files'), {
    repo: 'acme/widgets',
    pull_number: 42,
  });
});
test('repair links reject arbitrary destinations and malformed PR numbers', () => {
  for (const value of [
    'https://example.com/acme/repo/pull/1',
    'https://github.com@evil.test/a/b/pull/1',
    'https://user@github.com/a/b/pull/1',
    'https://github.com/a/b/issues/1',
    'https://github.com/a/b/pull/0',
    'https://github.com/a/b/pull/9007199254740993',
    'http://github.com/a/b/pull/1',
  ])
    assert.equal(parseRepairTarget(value), null, value);
});
