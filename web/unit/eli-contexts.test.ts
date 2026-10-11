import { test } from 'node:test';
import assert from 'node:assert/strict';
import { contextPrompt } from '../src/lib/assistant/prompts';
import {
  findingContext,
  jobContext,
  poolContext,
  usageContext,
} from '../src/lib/assistant/contexts';

// A job's snapshot is the drawer's own verdict, flattened: the evidence and
// the next steps as a line each, so the question stays the size of a
// paragraph, and nothing the explanation does not have is invented.
test('a job context carries the verdict and reads as one paragraph', () => {
  const ctx = jobContext(
    {
      id: 'job_1',
      job_name: 'Go (api-1)',
      repo: 'eyupio/zoomies',
      state: 'completed',
      conclusion: 'failure',
      pool_name: 'linux',
      labels: ['self-hosted', 'linux'],
    },
    {
      summary: 'The runner ran out of memory.',
      class: 'oom',
      confidence: 'high',
      problem_code: 'runner.oom',
      fix: 'Give the pool a bigger runner.',
      evidence: [
        { label: 'Memory limit', value: '4096', unit: 'MB' },
        { label: 'Peak', value: '4100', unit: 'MB' },
      ],
      next_steps: [{ text: 'Raise the memory ceiling.' }, { text: 'Re-run the job.' }],
    },
    'failed',
  );
  assert.equal(ctx.kind, 'job');
  assert.equal(ctx.title, 'Go (api-1)');
  assert.equal(ctx.facts['State'], 'completed (failure)');
  assert.equal(ctx.facts['Class'], 'oom (high confidence)');
  assert.equal(ctx.facts['Evidence'], 'Memory limit: 4096 MB; Peak: 4100 MB');
  assert.equal(ctx.facts['Next steps'], 'Raise the memory ceiling. Re-run the job.');
  assert.match(ctx.question ?? '', /Why did this job fail/);
  const waiting = jobContext({ id: 'job_2', state: 'queued' }, null, 'waiting');
  assert.equal(waiting.title, 'job_2');
  assert.equal(waiting.facts['Class'], undefined);
  assert.match(waiting.question ?? '', /still waiting/);
  // Absent facts are left out of the prompt, not written as "undefined".
  assert.doesNotMatch(contextPrompt(waiting), /undefined/);
});

// A pool's snapshot is its limits and what it is doing now, with utilisation
// worked out so the model does not have to, and left out when nothing is live
// (a utilisation of nothing is not zero).
test('a pool context has the limits, the counts and a utilisation when there is one', () => {
  const ctx = poolContext(
    {
      id: 'pool_1',
      name: 'linux',
      enabled: true,
      min_runners: 1,
      max_runners: 8,
      queued_jobs: 3,
      labels: ['linux'],
    },
    { live: 4, busy: 3, idle: 1, provisioning: 1, registering: 1 },
  );
  assert.equal(ctx.facts['Utilisation (%)'], 75);
  assert.equal(ctx.facts['Starting'], 2);
  assert.equal(ctx.facts['Queued jobs'], 3);
  assert.equal(poolContext({ id: 'pool_2' }, {}).facts['Utilisation (%)'], undefined);
});

// A finding's snapshot is the evaluator's sentences and where it was seen;
// the coding-agent prompt is not pasted in, because it is written for a
// different reader and Eli can fetch the finding itself.
test('a finding context names the repository and leaves the agent prompt out', () => {
  const ctx = findingContext('eyupio/zoomies', {
    code: 'ci.no_timeout',
    severity: 'warning',
    subject: 'build.yml',
    title: 'A job has no timeout',
    detail: 'd',
    fix: 'f',
    evidence: [{ kind: 'pool', label: 'linux', ref: 'pool_1' }],
  });
  assert.equal(ctx.facts['Repository'], 'eyupio/zoomies');
  assert.equal(ctx.facts['About'], 'build.yml');
  assert.equal(ctx.facts['Where it was seen'], 'pool: linux');
  assert.doesNotMatch(JSON.stringify(ctx), /prompt/);
});

// A usage snapshot is the totals, not the rows, and says nothing about runner
// time at a grouping where it cannot be attributed: a zero there would be a
// claim.
test('a usage context totals the rows and withholds what is not attributable', () => {
  const rows = [
    {
      key: 'a',
      jobs: 10,
      jobs_completed: 9,
      job_execution_seconds: 3600,
      allocated_runner_seconds: 7200,
      average_queue_wait_seconds: 12,
      peak_concurrency: 3,
      estimated_cost: 1.5,
    },
    {
      key: 'b',
      jobs: 5,
      jobs_completed: 5,
      job_execution_seconds: 1800,
      allocated_runner_seconds: 1800,
      average_queue_wait_seconds: null,
      peak_concurrency: 5,
      estimated_cost: 0.25,
    },
  ];
  const ctx = usageContext({
    grouping: 'pool',
    since: '2026-10-01T00:00',
    until: '2026-10-11T23:59',
    rows,
    attributable: true,
  });
  assert.equal(ctx.facts['Jobs'], 15);
  assert.equal(ctx.facts['Execution hours'], 1.5);
  assert.equal(ctx.facts['Runner-hours'], 2.5);
  assert.equal(ctx.facts['Utilisation (%)'], 60);
  assert.equal(ctx.facts['Longest mean queue wait (s)'], 12);
  assert.equal(ctx.facts['Peak concurrency'], 5);
  assert.equal(ctx.facts['Estimated cost'], 1.75);
  const byRepo = usageContext({
    grouping: 'repository',
    since: 's',
    until: 'u',
    rows,
    attributable: false,
    entity: 'eyupio/zoomies',
  });
  assert.equal(byRepo.title, 'repository eyupio/zoomies');
  assert.equal(byRepo.facts['Runner-hours'], undefined);
  assert.equal(byRepo.facts['Estimated cost'], undefined);
});
