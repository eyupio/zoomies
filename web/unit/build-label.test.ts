import { test } from 'node:test';
import assert from 'node:assert/strict';
import { buildLabel } from '../src/lib/shell/build.ts';

// A release is named by its tag, because that is what an upgrade and a bug
// report say, and it links to the release's own page.
test('a release build is named by its tag and links to the release', () => {
  const got = buildLabel({
    version: '1.3.0 (abc1234)',
    version_channel: 'v1.3.0',
    commit: 'abc1234def',
  });
  assert.equal(got?.kind, 'Release');
  assert.equal(got?.name, 'v1.3.0');
  assert.equal(got?.href, 'https://github.com/eyupio/zoomies/releases/tag/v1.3.0');
});

// Every build from main is published under the same moving dev tag, so the
// tag cannot tell two apart: the commit is what identifies one.
test('a dev build is named by its commit and links to it', () => {
  const got = buildLabel({
    version: 'main-sha-abc1234 (abc1234)',
    version_channel: 'dev',
    commit: 'abc1234def5678',
  });
  assert.equal(got?.kind, 'Dev');
  assert.equal(got?.name, 'abc1234');
  assert.equal(got?.href, 'https://github.com/eyupio/zoomies/commit/abc1234def5678');
});

// A local build is on no channel and in no release; it says so rather than
// linking somewhere that does not have it.
test('a local build is labelled as one and links nowhere', () => {
  const got = buildLabel({ version: '1.3.0-4-gabc1234-dirty', commit: 'abc1234def' });
  assert.equal(got?.kind, 'Local build');
  assert.equal(got?.name, 'abc1234');
  assert.equal(got?.href, undefined);
});

test('nothing is said before the controller has answered', () => {
  assert.equal(buildLabel(null), null);
  assert.equal(buildLabel({}), null);
});
