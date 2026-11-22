import assert from 'node:assert/strict';
import test from 'node:test';
import { ApiError } from '../src/lib/api/client.ts';
import { describeFailure } from '../src/lib/preview-failure.ts';

const NOT_FOUND = 'This preview is unavailable. Nothing was recreated.';
const api = (status: number, extra: { message?: string; retryAt?: number } = {}) =>
  new ApiError({
    status,
    code: status === 404 ? 'not_found' : 'internal',
    message: extra.message ?? 'the server said no',
    retryAt: extra.retryAt,
  });

// Five preview pages each kept their own copy of this, differing only in the
// sentence for a preview that is not there. What a page says has to come from the
// server's own words for everything else, because a refusal names the role needed.
test('a preview that is not there is said in the page words, and anything else in the servers', () => {
  assert.equal(describeFailure(api(404), NOT_FOUND).message, NOT_FOUND);
  assert.equal(
    describeFailure(api(409, { message: 'already approved' }), NOT_FOUND).message,
    'already approved',
  );
  assert.equal(describeFailure(new Error('network down'), NOT_FOUND).message, 'network down');
  assert.equal(describeFailure('odd', NOT_FOUND).message, 'The request could not be completed.');
});

// A 403 may mean the gate above the page has gone stale, so the page asks it again.
test('only a 403 asks the gate to look again', () => {
  assert.equal(describeFailure(api(403), NOT_FOUND).refreshGate, true);
  for (const status of [404, 409, 429, 500]) {
    assert.equal(describeFailure(api(status), NOT_FOUND).refreshGate, false, String(status));
  }
  assert.equal(describeFailure(new Error('x'), NOT_FOUND).refreshGate, false);
});

// A 429 blocks the page's buttons until the time the server named, or a minute when
// it named none; nothing else blocks them.
test('a rate limit blocks the buttons until the time the server named, or a minute', () => {
  const now = 1_000_000;
  assert.equal(
    describeFailure(api(429, { retryAt: now + 5_000 }), NOT_FOUND, now).retryAt,
    now + 5_000,
  );
  assert.equal(describeFailure(api(429), NOT_FOUND, now).retryAt, now + 60_000);
  assert.equal(describeFailure(api(500), NOT_FOUND, now).retryAt, null);
  assert.equal(describeFailure(new Error('x'), NOT_FOUND, now).retryAt, null);
});
