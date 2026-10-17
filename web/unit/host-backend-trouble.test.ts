import { test } from 'node:test';
import assert from 'node:assert/strict';
import { backendRows, backendTrouble } from '../src/lib/hosts/backends.ts';

// A closed host card folds the Backends block away. That block is where the
// agent explains why a host has nothing to run a job in, so these are the cases
// in which the card must not fold it, and the ordinary ones in which it must.

test('a host with no backend that works is trouble, whatever the agent says about each', () => {
  assert.equal(
    backendTrouble({
      backend_info: [
        { kind: 'docker', available: false, detail: 'permission denied on the socket' },
        { kind: 'podman', available: false, detail: 'no socket' },
      ],
    }),
    true,
  );
  assert.equal(backendTrouble({ backend_info: [{ kind: 'podman', available: false }] }), true);
});

test('one working backend is enough: a missing second one is the ordinary case, not trouble', () => {
  // Docker without Podman is how most hosts are. The agent reports Podman as
  // unavailable with the commands to install it, and a card that surfaced that
  // paragraph would put the same one on every host in the fleet.
  assert.equal(
    backendTrouble({
      backend_info: [
        { kind: 'docker', available: true },
        { kind: 'podman', available: false, detail: 'no socket at /run/user/1000/podman.sock' },
      ],
    }),
    false,
  );
});

test('a backend that does not say it is unavailable counts as working', () => {
  // `available` omitted is how an older agent says nothing is wrong.
  assert.equal(backendTrouble({ backend_info: [{ kind: 'docker' }] }), false);
});

test('a host that reports no backend at all is trouble, because no runner can be placed on it', () => {
  assert.equal(backendTrouble({}), true);
  assert.equal(backendTrouble({ backend_info: [], backends: [] }), true);
});

test('the plain list of kinds stands in for the rich one when that is all an agent sends', () => {
  assert.equal(backendTrouble({ backend_info: [], backends: ['docker'] }), false);
  assert.deepEqual(backendRows([], ['docker', 'podman']), [{ kind: 'docker' }, { kind: 'podman' }]);
});

test('the rich list wins over the plain one when both are present', () => {
  // Otherwise a kinds list that still names a backend the agent has since
  // marked unavailable would hide the trouble.
  assert.equal(
    backendTrouble({
      backend_info: [{ kind: 'docker', available: false }],
      backends: ['docker'],
    }),
    true,
  );
});
