/**
 * Fixtures for the repair helpers' tests, built to the generated types and
 * never cast: a field the API has dropped or renamed stops these compiling
 * (`npm run check` type-checks this folder), where a cast would let the tests
 * go on passing against a shape the controller no longer sends.
 */
import type { Job, Schemas } from '../src/lib/api/types.ts';

export type Check = Schemas['EliReadinessCheck'];
export type Readiness = Schemas['EliReadiness'];
export type Repair = Schemas['EliRepair'];
export type AutomaticRepository = Schemas['EliAutomaticRepository'];

/** The eight checks, in the order the controller makes them. */
export const CHECK_ORDER: Check['code'][] = [
  'identity',
  'provider',
  'installation',
  'access',
  'policy',
  'pull',
  'checks',
  'permissions',
];

export const SHA = 'a41c9e2f0b7d5c3e8a1f6d4b2c9e7a5f3d1b8c6e';
export const JOB_URL = 'https://github.com/acme/widgets/actions/runs/482/job/9001';

const passed = (code: Check['code']): Check => ({
  code,
  status: 'ok',
  message: `${code} is fine.`,
});

/** Every check passed. */
export function allOk(): Check[] {
  return CHECK_ORDER.map(passed);
}

/**
 * What the controller answers when a check stops it: the ones before it passed,
 * it is blocked, and the ones after were never reached. `blocker` is the code of
 * the blocker that stopped it, which the controller sends with every blocked check.
 */
export function stoppedAt(code: Check['code'], message: string, blocker?: string): Check[] {
  const at = CHECK_ORDER.indexOf(code);
  return CHECK_ORDER.map((c, i): Check =>
    i < at
      ? passed(c)
      : i === at
        ? { code: c, status: 'blocked', message, ...(blocker ? { blocker } : {}) }
        : { code: c, status: 'unchecked', message: 'Not checked yet.' },
  );
}

export function readiness(over: Partial<Readiness> = {}): Readiness {
  return {
    ready: true,
    repo: 'acme/widgets',
    pull_number: 17,
    head_sha: SHA,
    provider: 'My provider',
    model: 'my-model',
    allow_workflows: false,
    blockers: [],
    checks: allOk(),
    ...over,
  };
}

/** What a controller older than this build answers: the same readiness with no checks. */
export function withoutChecks(r: Readiness): Omit<Readiness, 'checks'> {
  const { checks: _checks, ...older } = r;
  void _checks;
  return older;
}

/** A readiness with the pull request looked up, as it is once write access is verified. */
export function readinessWithPull(over: Partial<Readiness> = {}): Readiness {
  return readiness({
    pull: {
      number: 17,
      title: 'Retry transient 502s',
      author: 'dana',
      branch: 'fix/retry-502',
      base: 'main',
      url: 'https://github.com/acme/widgets/pull/17',
      open: true,
    },
    ...over,
  });
}

/** The failed jobs readiness read from GitHub, for a run the fleet never ran. */
export function githubFailedJobs(
  over: Partial<NonNullable<Readiness['failed_jobs']>[number]> = {},
): NonNullable<Readiness['failed_jobs']>[number][] {
  return [
    {
      id: 7001,
      name: 'Publish Latest Image / build-and-push',
      step: 'Install dependencies and build',
      step_number: 8,
      url: JOB_URL.replace('9001', '7001'),
      conclusion: 'failure',
      started_at: '2026-10-23T10:00:00Z',
      completed_at: '2026-10-23T10:05:00Z',
      steps: [
        { number: 1, name: 'Set up job', conclusion: 'success' },
        { number: 8, name: 'Install dependencies and build', conclusion: 'failure' },
      ],
      ...over,
    },
    {
      id: 7002,
      name: 'Publish Latest Image / smoke',
      conclusion: 'timed_out',
      url: JOB_URL.replace('9001', '7002'),
    },
  ];
}

/** A job that failed in a workflow step, as Zoomies lists it. */
export function failedJob(over: Job = {}): Job {
  return {
    id: 'job_unit',
    github_job_id: 9001,
    github_run_id: 482,
    repo: 'acme/widgets',
    workflow: 'CI',
    job_name: 'unit (ubuntu-latest)',
    state: 'completed',
    conclusion: 'failure',
    html_url: JOB_URL,
    head_branch: 'fix/retry-502',
    head_sha: SHA,
    run_number: 482,
    run_attempt: 1,
    completed_at: '2026-10-18T09:14:00Z',
    pool_id: 'pool_linux',
    pool_name: 'linux-x64',
    runner_id: 'run_abc',
    runner_name: 'zoomies-linux-abc',
    failed_step: {
      number: 7,
      name: 'Run go test ./...',
      status: 'completed',
      conclusion: 'failure',
    },
    duration_ms: 95000,
    fault_domain: 'workflow',
    ...over,
  };
}

export function repair(over: Partial<Repair> = {}): Repair {
  return {
    id: 'eli_1',
    installation_id: 'ins_1',
    repo: 'acme/widgets',
    head_sha: SHA,
    commit_sha: '',
    user_id: 'usr_1',
    github_login: 'dana',
    provider_id: 'prv_1',
    trigger: 'mention',
    state: 'failed',
    message: '',
    pull_number: 17,
    job_id: 0,
    run_id: 482,
    github_user_id: 42,
    comment_id: 0,
    created_at: '2026-10-18T09:20:00Z',
    updated_at: '2026-10-18T09:24:00Z',
    ...over,
  };
}

/** A repository that repairs itself, with nothing spent and nothing running. */
export function automaticRepository(over: Partial<AutomaticRepository> = {}): AutomaticRepository {
  return {
    repo: 'acme/widgets',
    installation_id: 'ins_1',
    provider_id: 'prv_1',
    provider_name: 'Team provider',
    model: 'team-model',
    allow_workflows: false,
    running: 0,
    ...over,
  };
}
