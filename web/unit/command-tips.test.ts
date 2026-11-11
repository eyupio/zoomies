import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Host, Job, Problem, Runner, Stats } from '../src/lib/api/types.ts';
import { heroFigures, type HeroFigure } from '../src/lib/command/figures.ts';
import { lanes } from '../src/lib/command/lanes.ts';
import {
  figureTip,
  finishedTip,
  freeTip,
  hostTip,
  hourTip,
  problemTip,
  ringTip,
  runnerTip,
  startingTip,
  tickerTip,
  waitingTip,
} from '../src/lib/command/tips.ts';
import { runnerStatus } from '../src/lib/status.ts';
import { detailSentence } from '../src/lib/tooltip-detail.ts';

const NOW = Date.UTC(2026, 10, 10, 14, 0);
const ago = (ms: number) => new Date(NOW - ms).toISOString();
const MIN = 60_000;

const labels = (tip: { rows: { label: string }[] }) => tip.rows.map((r) => r.label);
const value = (tip: { rows: { label: string; value: string }[] }, label: string) =>
  tip.rows.find((r) => r.label === label)?.value;

const job = (extra: Partial<Job> = {}): Job => ({
  id: 'job_1',
  job_name: 'test (ubuntu)',
  repo: 'eyupio/zoomies',
  workflow: 'CI',
  head_branch: 'fix/cache',
  started_at: ago(4 * MIN + 10_000),
  ...extra,
});

const busyRunner = (extra: Partial<Runner> = {}): Runner => ({
  id: 'run_1',
  name: 'zoomies-linux-1',
  state: 'busy',
  pool_name: 'linux-x64',
  host_id: 'hst_1',
  host_name: 'builder-01',
  current_job: job(),
  cpu_percent: 150,
  allocated_cpus: 2,
  memory_bytes: 1024 * 1024 * 1024,
  allocated_memory_mb: 4096,
  jobs_handled: 3,
  image: 'ghcr.io/actions/runner:2.330',
  ...extra,
});

test('a runner tile names its job and says everything known about the runner, in order', () => {
  const tip = runnerTip(busyRunner(), { hostName: 'builder-01', now: NOW, quirky: false });
  assert.equal(tip.title, 'test (ubuntu)');
  assert.deepEqual(labels(tip), [
    'Runner',
    'State',
    'Pool',
    'Host',
    'Repository',
    'Workflow',
    'Branch',
    'Running for',
    'CPU',
    'Memory',
    'Jobs handled',
    'Image',
  ]);
  assert.equal(value(tip, 'Runner'), 'zoomies-linux-1');
  assert.equal(value(tip, 'State'), runnerStatus('busy', false).label);
  assert.equal(value(tip, 'Running for'), '4m 10s');
  // 150 where a whole CPU is 100, on two CPUs, is three quarters of them.
  assert.equal(value(tip, 'CPU'), '75% of 2 CPUs');
  assert.equal(value(tip, 'Memory'), '1.0 GB of 4.0 GB');
  assert.equal(value(tip, 'Jobs handled'), '3');
});

test('an idle runner has no job rows, and its title says it is idle', () => {
  const idle: Runner = { id: 'run_2', name: 'zoomies-linux-2', state: 'idle', pool_name: 'p' };
  const tip = runnerTip(idle, { now: NOW });
  assert.equal(tip.title, 'Idle');
  assert.deepEqual(labels(tip), ['Runner', 'State', 'Pool']);
});

test('a job that has not started has no Running for row', () => {
  const tip = runnerTip(busyRunner({ current_job: job({ started_at: null }) }), { now: NOW });
  assert.ok(!labels(tip).includes('Running for'));
  assert.ok(labels(tip).includes('Repository'));
});

test('a runner with no allocation gives its CPU and memory without one', () => {
  const tip = runnerTip(
    busyRunner({ allocated_cpus: undefined, allocated_memory_mb: undefined, cpu_percent: 40 }),
    { now: NOW },
  );
  assert.equal(value(tip, 'CPU'), '40%');
  assert.equal(value(tip, 'Memory'), '1.0 GB');
});

test('a starting tile names its step and says how long ago it was asked for', () => {
  const runner: Runner = {
    id: 'run_3',
    name: 'zoomies-linux-3',
    state: 'registering',
    pool_name: 'linux-x64',
    created_at: ago(90_000),
  };
  const tip = startingTip(runner, { hostName: 'builder-01', now: NOW });
  assert.equal(tip.title, 'Registering');
  assert.deepEqual(labels(tip), ['Runner', 'Pool', 'Host', 'Started']);
  assert.equal(value(tip, 'Started'), '1m 30s ago');
  assert.deepEqual(labels(startingTip({ state: 'provisioning' }, { now: NOW })), []);
  assert.equal(startingTip({ state: 'provisioning' }, { now: NOW }).title, 'Provisioning');
});

const host = (extra: Partial<Host> = {}): Host => ({
  id: 'hst_1',
  name: 'builder-01',
  slots: 4,
  capacity: 4,
  healthy: true,
  ...extra,
});

test('the free cell says how many slots are free, what is in use and what limits it', () => {
  const [lane] = lanes(
    [host({ slots_limited_by: 'memory', throttle: { level: 1 }, throttle_reason: 'CPU pressure' })],
    [busyRunner()],
    [],
  );
  const tip = freeTip(lane!);
  assert.equal(tip.title, '3 free slots');
  assert.deepEqual(tip.rows, [
    { label: 'In use', value: '1 of 4 slots' },
    { label: 'Limited by', value: 'Memory' },
    { label: 'Throttle', value: 'CPU pressure' },
  ]);
});

test('a free cell on a calm host says nothing about a throttle or a limit it was not given', () => {
  const [lane] = lanes([host({ slots: 1, capacity: 1 })], [], []);
  const tip = freeTip(lane!);
  assert.equal(tip.title, '1 free slot');
  assert.deepEqual(labels(tip), ['In use']);
});

test('a host header gives its state, its slots and fresh measurements, in order', () => {
  const [lane] = lanes(
    [
      host({
        usage_fresh: true,
        usage: {
          cpu_percent: 62,
          load_average_1m: 3.456,
          io_wait_percent: 4,
          memory_available_mb: 2048,
        },
        size_class: { class: 'large', source: 'measured', reason: 'Measured at 16 CPUs.' },
        version: '1.40.0',
      }),
    ],
    [busyRunner()],
    [],
  );
  const tip = hostTip(lane!);
  assert.equal(tip.title, 'builder-01');
  assert.deepEqual(tip.rows, [
    { label: 'Status', value: 'Healthy' },
    { label: 'Slots', value: '1 of 4 in use' },
    { label: 'CPU', value: '62%' },
    { label: 'Load average', value: '3.46' },
    { label: 'I/O wait', value: '4%' },
    { label: 'Memory available', value: '2.0 GB' },
    { label: 'Size class', value: 'Large' },
    { label: 'Agent version', value: '1.40.0' },
  ]);
});

// A stale figure on a wall reads as the host being calm.
test('a host whose usage is stale shows none of it, and says the worst of its state', () => {
  const [lane] = lanes(
    [host({ healthy: false, usage_fresh: false, usage: { cpu_percent: 5 } })],
    [],
    [],
  );
  const tip = hostTip(lane!);
  assert.deepEqual(tip.rows, [
    { label: 'Status', value: 'Not answering' },
    { label: 'Slots', value: '0 of 4 in use' },
  ]);
  const [throttledLane] = lanes([host({ throttle: { level: 2 } })], [], []);
  assert.equal(value(hostTip(throttledLane!), 'Status'), 'Throttled, step 2 of 3');
  const [cordoned] = lanes([host({ cordoned: true })], [], []);
  assert.equal(value(hostTip(cordoned!), 'Status'), 'Cordoned');
});

test('the ring counts the slots by what is in them, then the pools, then where it opens', () => {
  const runners: Runner[] = [
    busyRunner({ id: 'a', pool_name: 'big' }),
    busyRunner({ id: 'b', pool_name: 'big' }),
    busyRunner({ id: 'c', pool_name: 'big', state: 'idle', current_job: undefined }),
    busyRunner({ id: 'd', pool_name: 'small', state: 'provisioning', current_job: undefined }),
  ];
  const tip = ringTip(lanes([host({ slots: 6, capacity: 6 })], runners, []), 'The running jobs');
  assert.equal(tip.title, 'Slots in use');
  assert.deepEqual(tip.rows, [
    { label: 'Busy', value: '2 of 6' },
    { label: 'Starting', value: '1' },
    { label: 'Idle', value: '1' },
    { label: 'Free', value: '2' },
    { label: 'big', value: '2 of 3 runners busy' },
    { label: 'small', value: '0 of 1 runner busy' },
    { label: 'Opens', value: 'The running jobs' },
  ]);
});

test('the ring lists six pools at most and counts the rest', () => {
  const runners = Array.from({ length: 9 }, (_, i) =>
    busyRunner({ id: `r${i}`, pool_name: `pool-${i}` }),
  );
  const tip = ringTip(lanes([host({ slots: 9, capacity: 9 })], runners, []), 'x');
  const pools = tip.rows.filter((r) => r.label.startsWith('pool-'));
  assert.equal(pools.length, 6);
  assert.deepEqual(tip.rows.at(-2), { label: 'Other pools', value: 'and 3 more' });
});

const STATS: Stats = {
  window: '1h0m0s',
  p50_startup_ms: 4200,
  p95_startup_ms: 9000,
  p50_registration_ms: 1500,
  p95_registration_ms: 3000,
  fleet: {
    running_jobs: 2,
    completed: 27,
    succeeded: 24,
    failed: 2,
    cancelled: 1,
    unknown: 0,
    median_wait_ms: 12_000,
    p95_wait_ms: 65_000,
  },
};
const figure = (key: HeroFigure['key'], stats: Stats | null = STATS) =>
  heroFigures(stats, NOW).find((f) => f.key === key)!;

test('the wait figure gives its median, p95 and window, then where it opens', () => {
  const tip = figureTip(figure('wait'), STATS);
  assert.equal(tip.title, 'Queue wait');
  assert.deepEqual(tip.rows, [
    { label: 'Median', value: '12s' },
    { label: 'p95', value: '1m 05s' },
    { label: 'Window', value: 'Last hour' },
    { label: 'Opens', value: 'The queue' },
  ]);
});

test('the start figure gives both timings with registration, then the window', () => {
  const tip = figureTip(figure('start'), STATS);
  assert.equal(tip.title, 'Runner start');
  assert.deepEqual(labels(tip), [
    'Median',
    'p95',
    'Registration median',
    'Registration p95',
    'Window',
    'Opens',
  ]);
  assert.equal(value(tip, 'Registration median'), '1.5s');
  assert.equal(value(tip, 'Opens'), 'The runners');
});

test('the succeeded figure breaks the window down by how jobs ended', () => {
  const tip = figureTip(figure('succeeded'), STATS);
  assert.equal(tip.title, 'Succeeded, last hour');
  assert.deepEqual(tip.rows, [
    { label: 'Succeeded', value: '24' },
    { label: 'Failed', value: '2' },
    { label: 'Cancelled', value: '1' },
    { label: 'Other', value: '0' },
    { label: 'Finished', value: '27' },
    { label: 'Window', value: 'Last hour' },
    { label: 'Opens', value: 'The jobs finished in the last hour' },
  ]);
});

// Zero is what the controller answers for a timing it has no samples for.
test('a figure with nothing measured keeps only its window and where it opens', () => {
  const empty: Stats = { window: '1h', p50_startup_ms: 0, fleet: { running_jobs: 0 } };
  assert.deepEqual(labels(figureTip(figure('wait', empty), empty)), ['Window', 'Opens']);
  assert.deepEqual(labels(figureTip(figure('start', empty), empty)), ['Window', 'Opens']);
  assert.deepEqual(labels(figureTip(figure('succeeded', null), null)), ['Window', 'Opens']);
});

test('the hour chart says where each line is now and its peak', () => {
  const points = [
    { running: 2, queued: 0 },
    { running: 7, queued: 4 },
    { running: 5, queued: 1 },
  ];
  assert.deepEqual(hourTip(points, "The Overview's history"), {
    title: 'Last hour',
    rows: [
      { label: 'Running now', value: '5' },
      { label: 'Running peak', value: '7' },
      { label: 'Queued now', value: '1' },
      { label: 'Queued peak', value: '4' },
      { label: 'Opens', value: "The Overview's history" },
    ],
  });
  assert.deepEqual(labels(hourTip([], 'x')), ['Opens']);
});

test('a waiting row says where the job comes from, how long it has waited and why', () => {
  const waiting = job({
    state: 'queued',
    started_at: null,
    labels: ['self-hosted', 'linux'],
    queued_at: ago(3 * MIN),
    pool_name: 'linux-x64',
  });
  const tip = waitingTip(waiting, { label: 'Ready', detail: 'Waiting for a runner.' }, NOW);
  assert.equal(tip.title, 'test (ubuntu)');
  assert.deepEqual(tip.rows, [
    { label: 'Repository', value: 'eyupio/zoomies' },
    { label: 'Workflow', value: 'CI' },
    { label: 'Branch', value: 'fix/cache' },
    { label: 'Labels', value: 'self-hosted, linux' },
    { label: 'Queued', value: '3m 00s ago' },
    { label: 'Status', value: 'Ready' },
    { label: 'Why', value: 'Waiting for a runner.' },
    { label: 'Pool', value: 'linux-x64' },
  ]);
});

test('a waiting job no pool claims has no Pool row, and one with nothing known says little', () => {
  const tip = waitingTip({ id: 'j' }, { label: 'Ready', detail: 'Waiting.' }, NOW);
  assert.equal(tip.title, 'Unnamed job');
  assert.deepEqual(labels(tip), ['Status', 'Why']);
});

test('a problem row gives its detail and fix in full, and what it is about', () => {
  const problem: Problem = {
    code: 'host.disk',
    severity: 'warning',
    audience: 'fleet',
    title: 'A host is short of disk',
    detail: 'builder-01 has 2% of its work directory free.',
    fix: 'Free space on builder-01, or raise its disk.',
    target_kind: 'host',
    target_id: 'hst_1',
  };
  assert.deepEqual(problemTip(problem, 'builder-01'), {
    title: 'A host is short of disk',
    rows: [
      { label: 'Detail', value: problem.detail },
      { label: 'Fix', value: problem.fix },
      { label: 'Target', value: 'Host builder-01' },
    ],
  });
  const bare: Problem = { code: 'x', severity: 'info', audience: 'both', title: 'Something' };
  assert.deepEqual(problemTip(bare).rows, []);
});

test('a finished row says how it ended, how long it took and where it ran', () => {
  const failed = job({
    state: 'completed',
    conclusion: 'failure',
    duration_ms: 125_000,
    queue_wait_ms: 8_000,
    failed_step: { name: 'Run tests' },
    runner_name: 'zoomies-linux-1',
  });
  const tip = finishedTip(failed, 'builder-01');
  assert.equal(tip.title, 'test (ubuntu)');
  assert.deepEqual(tip.rows, [
    { label: 'Result', value: 'Failed' },
    { label: 'Took', value: '2m 05s' },
    { label: 'Waited', value: '8.0s' },
    { label: 'Failed step', value: 'Run tests' },
    { label: 'Runner', value: 'zoomies-linux-1' },
    { label: 'Host', value: 'builder-01' },
    { label: 'Repository', value: 'eyupio/zoomies' },
    { label: 'Workflow', value: 'CI' },
    { label: 'Branch', value: 'fix/cache' },
  ]);
});

test('a finished job that succeeded names no failed step, and unknowns are left out', () => {
  const tip = finishedTip({
    id: 'j',
    job_name: 'lint',
    conclusion: 'success',
    failed_step: { name: 'Lint' },
  });
  assert.deepEqual(tip.rows, [{ label: 'Result', value: 'Succeeded' }]);
  assert.equal(value(finishedTip({ id: 'k', conclusion: 'skipped' }), 'Result'), 'Skipped');
});

test('a ticker entry gives its detail, when it happened and what it was about', () => {
  const at = ago(3 * MIN);
  const tip = tickerTip(
    {
      title: 'Runner started',
      detail: 'Registered with GitHub.',
      at,
      target: { label: 'zoomies-linux-1', href: '/runners/run_1' },
    },
    NOW,
  );
  assert.equal(tip.title, 'Runner started');
  assert.deepEqual(labels(tip), ['Detail', 'When', 'About']);
  const clock = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' });
  assert.equal(value(tip, 'When'), `${clock.format(Date.parse(at))}, 3m ago`);
  assert.equal(value(tip, 'About'), 'zoomies-linux-1');
  assert.deepEqual(labels(tickerTip({ title: 'x', at: 'not a time' }, NOW)), []);
});

// What the bubble shows has to reach a screen reader too.
test('a card reads as one run of sentences, without doubled full stops', () => {
  assert.equal(
    detailSentence({
      title: 'Queue wait',
      rows: [
        { label: 'Median', value: '12s' },
        { label: 'Why', value: 'Waiting for a runner.' },
      ],
    }),
    'Queue wait. Median: 12s. Why: Waiting for a runner.',
  );
});
