import { test } from 'node:test';
import assert from 'node:assert/strict';
import { registryMirrorLine } from '../src/lib/hosts/registry-mirror.ts';
import type { HostRegistryMirror } from '../src/lib/api/types';

// The card says whether a host's registry mirror is on and what is wrong with it,
// in the controller's own sentences. A mirror that is off says nothing at all:
// most hosts will never have one, and a card that announced "off" on every host
// would be a line nobody reads.

test('a host that is not running a mirror has no line', () => {
  assert.equal(registryMirrorLine(undefined), null);
  assert.equal(registryMirrorLine(null), null);
});

test('a mirror that is on with nothing wrong says only that it is on', () => {
  const mirror: HostRegistryMirror = {};
  assert.deepEqual(registryMirrorLine(mirror), {
    state: 'ok',
    headline: 'On',
    notes: [],
  });
});

test("a problem shows the controller's sentences untouched, and says nothing fails", () => {
  const mirror: HostRegistryMirror = {
    problem: 'listener',
    detail: "the registry mirror could not listen on the host's address.",
    fix: "look in the agent's log for the reason.",
  };
  const line = registryMirrorLine(mirror);
  assert.ok(line);
  assert.equal(line.state, 'problem');
  assert.equal(line.headline, 'Not working');
  assert.equal(line.detail, mirror.detail);
  assert.equal(line.fix, mirror.fix);
  // The one thing an operator seeing a red-looking line needs to know first.
  assert.match(line.consequence ?? '', /Docker Hub/);
  assert.match(line.consequence ?? '', /no job fails|nothing fails|does not fail/i);
});

test('a mirror with no problem has no consequence to explain', () => {
  const line = registryMirrorLine({});
  assert.ok(line);
  assert.equal(line.consequence, undefined);
});

test('a host with a custom sidecar image of its own is told which pools the mirror skips', () => {
  const fine = registryMirrorLine({ host_image_custom: true });
  assert.ok(fine);
  assert.equal(fine.state, 'ok');
  assert.equal(fine.notes.length, 1);
  assert.match(fine.notes[0]!, /agent\.dind_image/);
  assert.match(fine.notes[0]!, /follow/i);

  const broken = registryMirrorLine({
    problem: 'folder',
    detail: 'd',
    fix: 'f',
    host_image_custom: true,
  });
  assert.ok(broken);
  assert.equal(broken.state, 'problem');
  assert.equal(broken.notes.length, 1);
});

test('a problem the controller sent no sentences for still says something', () => {
  // An older controller, or a field a future one fills later, must not render a
  // line that says "Not working" and then nothing.
  const line = registryMirrorLine({ problem: 'other' });
  assert.ok(line);
  assert.equal(line.state, 'problem');
  assert.ok((line.detail ?? '').length > 0);
});
