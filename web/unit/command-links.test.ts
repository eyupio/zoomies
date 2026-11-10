import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Problem } from '../src/lib/api/types.ts';
import {
  HISTORY_HREF,
  QUEUE_HREF,
  RUNNING_HREF,
  RUNNERS_HREF,
  completedSinceHref,
  finishedSinceHref,
  hostHref,
  problemHref,
  runnerHref,
} from '../src/lib/command/links.ts';

const problem = (extra: Partial<Problem>): Problem => ({
  code: 'x.y',
  severity: 'warning',
  audience: 'fleet',
  title: 'A problem',
  ...extra,
});

test('an id is encoded into its link', () => {
  assert.equal(hostHref('a/b'), '/hosts/a%2Fb');
  assert.equal(runnerHref('r 1'), '/runners/r%201');
});

test('the fixed destinations are the pages the figures count', () => {
  assert.equal(RUNNING_HREF, '/jobs?state=in_progress');
  assert.equal(QUEUE_HREF, '/queue');
  assert.equal(RUNNERS_HREF, '/runners');
  assert.equal(HISTORY_HREF, '/#history');
});

test('finished jobs since an instant are the completed jobs from that instant on', () => {
  const since = new Date(Date.UTC(2026, 10, 10, 13, 4));
  const url = new URL(finishedSinceHref(since), 'http://x');
  assert.equal(url.pathname, '/jobs');
  assert.equal(url.searchParams.get('state'), 'completed');
  assert.equal(url.searchParams.get('since'), '2026-11-10T13:04:00.000Z');
});

test('jobs finished since an instant are read by when they ended, not when they queued', () => {
  const since = new Date(Date.UTC(2026, 10, 10, 13, 50));
  const url = new URL(completedSinceHref(since), 'http://x');
  assert.equal(url.pathname, '/jobs');
  assert.equal(url.searchParams.get('state'), 'completed');
  assert.equal(url.searchParams.get('completed_since'), '2026-11-10T13:50:00.000Z');
  assert.equal(url.searchParams.get('since'), null);
});

test('each kind of problem target opens the page that holds the thing', () => {
  const cases: Array<[Partial<Problem>, string]> = [
    [{ target_kind: 'pool', target_id: 'p1' }, '/pools/p1'],
    [{ target_kind: 'runner', target_id: 'r1' }, '/runners/r1'],
    [{ target_kind: 'host', target_id: 'h1' }, '/hosts'],
    [{ target_kind: 'host', target_id: 'h1', code: 'host.os_health' }, '/hosts/h1'],
    [{ target_kind: 'provider', target_id: 'v1' }, '/providers/v1'],
    [{ target_kind: 'ai_context', target_id: 'a1' }, '/kennel/ai-context'],
    [{ target_kind: 'machine', target_id: 'm1' }, '/machines/m1'],
    [{ target_kind: 'installation', target_id: 'i1' }, '/installations'],
    [{ target_kind: 'job', target_id: 'j1' }, '/jobs'],
    [{ target_kind: 'job', target_id: 'j1', code: 'jobs.unmatched' }, '/jobs?unmatched=true'],
  ];
  for (const [extra, href] of cases) assert.equal(problemHref(problem(extra)), href);
});

test('a kennel problem opens the repositories that have an error', () => {
  const href = problemHref(problem({ target_kind: 'kennel' }));
  assert.ok(href?.startsWith('/kennel'), href ?? 'null');
});

test('a problem with no target has no link', () => {
  assert.equal(problemHref(problem({})), null);
});
