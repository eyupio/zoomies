import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  hostsLine,
  installationsLine,
  kennelLine,
  migrateLine,
  windowPhrase,
  jobsLine,
  poolsLine,
  providersLine,
  queueLine,
  repairsLine,
  workflowsLine,
} from '../src/lib/bands/lines.ts';

test('pools say how many are enabled, how full they are and which are at the ceiling', () => {
  const line = poolsLine({ enabled: 3, total: 4, live: 8, max: 20, atCeiling: 1 });
  assert.equal(line.headline, '3 of 4 pools are enabled.');
  assert.equal(line.accent, '8 of 20 runners live.');
  assert.equal(line.lede, '1 is at its ceiling.');
  assert.equal(
    poolsLine({ enabled: 2, total: 2, live: 0, max: 8, atCeiling: 0 }).headline,
    '2 pools are enabled.',
  );
  assert.equal(
    poolsLine({ enabled: 1, total: 1, live: 0, max: 8, atCeiling: 0 }).lede,
    'None is at its ceiling.',
  );
  assert.equal(
    poolsLine({ enabled: 0, total: 0, live: 0, max: 0, atCeiling: 0 }).headline,
    'No pools yet.',
  );
});

test('the queue names how many jobs wait, and says so when none does', () => {
  const waiting = queueLine({ queued: 6, running: 3, medianWait: '19s' });
  assert.equal(waiting.headline, '6 jobs are waiting for a runner.');
  assert.equal(waiting.accent, '3 running.');
  assert.equal(waiting.lede, 'The median wait is 19s.');
  assert.equal(
    queueLine({ queued: 1, running: 0, medianWait: null }).headline,
    '1 job is waiting for a runner.',
  );
  const calm = queueLine({ queued: 0, running: 2, medianWait: '4s' });
  assert.equal(calm.headline, 'Nothing is waiting.');
  assert.equal(calm.lede, 'Every job has a runner.');
});

test('jobs say what is running and what finished in the window, by the numbers', () => {
  const line = jobsLine({
    running: 3,
    queued: 6,
    succeeded: 40,
    failed: 2,
    windowText: 'the last hour',
  });
  assert.equal(line.headline, '3 jobs are running.');
  assert.equal(line.accent, '6 waiting for a runner.');
  assert.equal(line.lede, '2 failed and 40 succeeded in the last hour.');
  assert.equal(
    jobsLine({ running: 1, queued: 0, succeeded: 0, failed: 0, windowText: 'the last hour' })
      .headline,
    '1 job is running.',
  );
});

test('workflows count the jobs in flight, running and waiting', () => {
  const line = workflowsLine({ running: 3, queued: 6 });
  assert.equal(line.headline, '9 jobs are in flight.');
  assert.equal(line.accent, '3 running, 6 waiting.');
  assert.equal(workflowsLine({ running: 0, queued: 1 }).headline, '1 job is in flight.');
});

test('hosts say how many are connected and how many slots are in use', () => {
  const line = hostsLine({ connected: 2, total: 3, inUse: 4, capacity: 12 });
  assert.equal(line.headline, '2 of 3 hosts are connected.');
  assert.equal(line.accent, '4 of 12 slots in use.');
  assert.equal(line.lede, '1 has not checked in.');
  const all = hostsLine({ connected: 3, total: 3, inUse: 0, capacity: 12 });
  assert.equal(all.headline, '3 hosts are connected.');
  assert.equal(all.lede, 'Every host has checked in.');
  assert.equal(
    hostsLine({ connected: 0, total: 0, inUse: 0, capacity: 0 }).headline,
    'No hosts yet.',
  );
});

test('providers say how many machines they own and what needs review', () => {
  const line = providersLine({ providers: 2, owned: 3, ceiling: 10, paused: 1, review: 2 });
  assert.equal(line.headline, '3 machines are owned.');
  assert.equal(line.accent, 'Up to 10 at once.');
  assert.equal(line.lede, '2 need review.');
  assert.equal(
    providersLine({ providers: 1, owned: 1, ceiling: 4, paused: 0, review: 0 }).headline,
    '1 machine is owned.',
  );
  assert.equal(
    providersLine({ providers: 1, owned: 0, ceiling: 4, paused: 1, review: 0 }).lede,
    '1 provider is paused.',
  );
  assert.equal(
    providersLine({ providers: 0, owned: 0, ceiling: 0, paused: 0, review: 0 }).headline,
    'No providers yet.',
  );
});

test('a window is worded as a period, never as "the last 1 hour"', () => {
  assert.equal(windowPhrase('1 hour'), 'the last hour');
  assert.equal(windowPhrase('24 hours'), 'the last 24 hours');
  assert.equal(windowPhrase('2 days'), 'the last 2 days');
  assert.equal(windowPhrase(''), 'the reported window');
});

test('installations say how many Apps there are, and what needs a look', () => {
  const line = installationsLine({
    connections: 2,
    healthy: 1,
    unhealthy: 1,
    unchecked: 0,
    pools: 3,
    lowQuota: 1,
  });
  assert.equal(line.headline, '2 connections to GitHub.');
  assert.equal(line.accent, '1 not working.');
  assert.equal(line.lede, '1 App is below 10% of the API quota.');
  assert.equal(
    installationsLine({
      connections: 1,
      healthy: 1,
      unhealthy: 0,
      unchecked: 0,
      pools: 1,
      lowQuota: 0,
    }).lede,
    '1 pool depends on them.',
  );
  assert.equal(
    installationsLine({
      connections: 0,
      healthy: 0,
      unhealthy: 0,
      unchecked: 0,
      pools: 0,
      lowQuota: 0,
    }).headline,
    'GitHub is not connected.',
  );
});

test('repairs say what needs a person and what is running', () => {
  const line = repairsLine({ offers: 2, running: 1 });
  assert.equal(line.headline, '2 offers need review.');
  assert.equal(line.accent, '1 repair is running.');
  assert.equal(repairsLine({ offers: 1, running: 0 }).headline, '1 offer needs review.');
  assert.equal(repairsLine({ offers: 0, running: 2 }).headline, 'Nothing needs you.');
  assert.equal(repairsLine({ offers: 0, running: 0 }).headline, 'Nothing needs you.');
  assert.equal(repairsLine({ offers: 0, running: 0 }).accent, 'No repairs are running.');
});

test('migrate says what the wizard will do before it asks for anything', () => {
  const line = migrateLine();
  assert.equal(line.headline, 'Move repositories onto this fleet.');
  assert.equal(line.accent, 'You see every diff first.');
});

test('Kennel Club says what needs attention, and names the first repository', () => {
  const line = kennelLine({
    enabled: true,
    repositories: 12,
    attention: 2,
    clean: 7,
    first: { name: 'acme/widgets', findings: '2 errors and a warning', public: true },
    more: 1,
  });
  assert.equal(line.headline, '2 repositories need attention.');
  assert.equal(line.accent, '7 repositories with nothing open.');
  assert.match(line.lede, /^acme\/widgets has 2 errors and a warning open\./);
  assert.match(line.lede, /It is public/);
  assert.match(line.lede, /1 more repository needs attention\.$/);

  const quiet = kennelLine({
    enabled: true,
    repositories: 4,
    attention: 0,
    clean: 4,
    first: null,
    more: 0,
  });
  assert.equal(quiet.headline, 'No repository needs attention.');
  assert.equal(quiet.lede, 'No repository has an error or a warning open.');

  assert.equal(
    kennelLine({ enabled: false, repositories: 0, attention: 0, clean: 0, first: null, more: 0 })
      .headline,
    'Kennel Club is off.',
  );
});
