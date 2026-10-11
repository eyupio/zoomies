import { test } from 'node:test';
import assert from 'node:assert/strict';
import { starterQuestions } from '../src/lib/assistant/starters';

const stats = (over: Record<string, unknown> = {}) => ({
  window: '1h',
  queued_jobs: 0,
  running_jobs: 0,
  completed: 0,
  succeeded: 0,
  failed: 0,
  fleet_failed: 0,
  ...over,
});

// Without the fleet to look at, the questions are about Zoomies in general;
// with it, the first is always the open one, so a reader finds the same entry
// point every time, and the rest come from what the fleet is doing now.
test('the general list stands without fleet access, and the open question leads with it', () => {
  const general = starterQuestions({ fleetAccess: false, stats: stats(), problems: [], hosts: [] });
  assert.equal(general.length, 4);
  assert.match(general[0] ?? '', /labels/);
  const fleet = starterQuestions({ fleetAccess: true, stats: stats(), problems: [], hosts: [] });
  assert.equal(fleet.length, 4);
  assert.equal(fleet[0], 'How is the fleet doing right now?');
});

// A fleet with trouble asks about the trouble: the worst problem by name, the
// jobs waiting, the jobs that failed, the host that is unwell. Nothing is
// invented when the figure is zero, and the list is still four long.
test('a fleet with problems, a queue, failures and a sick host asks about those first', () => {
  const got = starterQuestions({
    fleetAccess: true,
    stats: stats({ queued_jobs: 6, failed: 3, fleet_failed: 1 }),
    problems: [
      { code: 'host.throttled', severity: 'warning', title: 'A host has been stepped down' },
      { code: 'pool.no_host', severity: 'error', title: 'Pool arm has nowhere to run' },
    ],
    hosts: [
      { name: 'build-1', healthy: true },
      { name: 'ubuntu-skull', healthy: false },
    ],
  });
  assert.equal(got.length, 4);
  assert.equal(got[0], 'How is the fleet doing right now?');
  // The error outranks the warning, whatever order the drawer listed them in.
  assert.equal(got[1], 'Explain "Pool arm has nowhere to run" and what to do about it.');
  assert.equal(got[2], 'Why are 6 jobs queued right now?');
  assert.equal(got[3], 'Why did 3 jobs fail in the last hour, and was the fleet at fault for any?');
});

// With less going on, the host and the second problem get their turn, and the
// fixed questions fill what is left, never repeating one already there.
test('quieter fleets fill the list from the host, the next problem and the defaults', () => {
  const sick = starterQuestions({
    fleetAccess: true,
    stats: stats(),
    problems: [
      { code: 'host.unhealthy', severity: 'warning', title: 'A host has stopped answering' },
    ],
    hosts: [{ name: 'ubuntu-skull', healthy: false }],
  });
  assert.deepEqual(sick, [
    'How is the fleet doing right now?',
    'Explain "A host has stopped answering" and what to do about it.',
    'What is wrong with host ubuntu-skull?',
    'Which jobs are queued, and why?',
  ]);
  const quiet = starterQuestions({ fleetAccess: true, stats: null, problems: [], hosts: [] });
  assert.deepEqual(quiet, [
    'How is the fleet doing right now?',
    'Is anything wrong that I should look at?',
    'Which jobs are queued, and why?',
    'How busy are the pools?',
  ]);
  assert.equal(new Set(quiet).size, 4);
});
