import { test } from 'node:test';
import assert from 'node:assert/strict';
import { caseFromLive, caseFromSnapshot } from '../src/lib/repairs/case.ts';
import {
  JOB_URL,
  SHA,
  failedJob,
  githubFailedJobs,
  readiness,
  readinessWithPull,
  repair,
} from './repair-fixtures.ts';

const RUN = 'https://github.com/acme/widgets/actions/runs/482';
const REPO = 'https://github.com/acme/widgets';

// The case is the page's answer to "what is this about?": which repository,
// which pull request and commit, which run, and which jobs failed at which step.
test('a live case names the repository, the pull request, the commit, the run and the jobs', () => {
  const fleet = failedJob({
    id: 'job_int',
    github_job_id: 9002,
    job_name: 'integration',
    html_url: `${RUN}/job/9002`,
    fault_domain: 'fleet',
    fault_kind: 'out_of_memory',
    fault_fix: 'Give the pool more memory.',
    failed_step: {
      number: 3,
      name: 'Run integration suite',
      status: 'completed',
      conclusion: 'failure',
    },
    duration_ms: 0,
  });
  const view = caseFromLive({
    repo: 'acme/widgets',
    jobs: [failedJob(), fleet],
    readiness: readinessWithPull(),
    openedJobId: 'job_int',
  });

  assert.equal(view.repo, 'acme/widgets');
  assert.equal(view.repoUrl, REPO);
  assert.equal(view.title, 'Retry transient 502s');
  assert.equal(view.hasPull, true);
  assert.equal(view.pullNumber, 17);
  assert.equal(view.pullUrl, `${REPO}/pull/17`);
  assert.equal(view.author, 'dana');
  assert.equal(view.branch, 'fix/retry-502');
  assert.equal(view.base, 'main');
  assert.equal(view.sha, SHA);
  assert.equal(view.commitUrl, `${REPO}/commit/${SHA}`);
  assert.equal(view.runLabel, 'CI #482');
  assert.equal(view.runUrl, RUN);
  assert.equal(view.failedAt, '2026-10-18T09:14:00Z');
  assert.deepEqual(view.ran, {
    pool: 'linux-x64',
    runner: 'zoomies-linux-abc',
    poolId: 'pool_linux',
    runnerId: 'run_abc',
  });
  assert.equal(view.jobs.length, 2);
});

test('each failed job carries its step, its blame and whether the person opened it', () => {
  const view = caseFromLive({
    repo: 'acme/widgets',
    jobs: [
      failedJob(),
      failedJob({
        id: 'job_int',
        github_job_id: 9002,
        job_name: 'integration',
        html_url: `${RUN}/job/9002`,
        fault_domain: 'fleet',
        fault_kind: 'out_of_memory',
        fault_fix: 'Give the pool more memory.',
        failed_step: {
          number: 3,
          name: 'Run integration suite',
          status: 'completed',
          conclusion: 'failure',
        },
      }),
    ],
    readiness: null,
    openedJobId: 'job_int',
  });
  const [unit, integration] = view.jobs;
  assert.equal(unit?.key, 'job_unit');
  assert.equal(unit?.name, 'unit (ubuntu-latest)');
  assert.equal(unit?.step, 'Run go test ./...');
  assert.equal(unit?.stepNumber, 7);
  assert.equal(unit?.url, JOB_URL);
  assert.equal(unit?.durationMs, 95000);
  assert.equal(unit?.domain, 'workflow');
  assert.equal(unit?.opened, false);
  assert.equal(unit?.githubJobId, 9001);

  assert.equal(integration?.domain, 'fleet');
  assert.equal(integration?.faultLabel, 'Out of memory');
  assert.equal(integration?.fix, 'Give the pool more memory.');
  assert.equal(integration?.stepNumber, 3);
  assert.equal(integration?.opened, true);
});

// Until the pull request has been looked up the page has no title for it, and
// says what ran instead of leaving a gap.
test('without the pull request the case is titled by the run and says so', () => {
  const view = caseFromLive({ repo: 'acme/widgets', jobs: [failedJob()], readiness: null });
  assert.equal(view.title, 'CI #482 failed on fix/retry-502');
  assert.equal(view.hasPull, false);
  assert.equal(view.pullUrl, undefined);
  assert.equal(view.branch, 'fix/retry-502');
  assert.equal(view.sha, SHA);
  // Readiness that has not found a pull request is not one that has.
  const pending = caseFromLive({
    repo: 'acme/widgets',
    jobs: [failedJob()],
    readiness: readiness({ pull_number: 0, head_sha: '' }),
  });
  assert.equal(pending.hasPull, false);
  assert.equal(pending.pullNumber, undefined);
});

// A run Zoomies has pruned, or never saw, still gets a page that says what it
// can: the repository, and a title that is a sentence.
test('with no jobs and no readiness it is still a titled case', () => {
  const view = caseFromLive({ repo: 'acme/widgets', jobs: [], readiness: null });
  assert.equal(view.repo, 'acme/widgets');
  assert.ok(view.title.includes('acme/widgets'), view.title);
  assert.deepEqual(view.jobs, []);
  assert.equal(view.hasPull, false);
  assert.equal(view.repoUrl, undefined);
  assert.equal(view.runUrl, undefined);
});

// Everything on the page was written by somebody who can open a pull request or
// edit a workflow. Text stays text, and an address that is not a web link is
// dropped.
test('markup in a title or a name is kept as text and a script address is dropped', () => {
  const hostile = readinessWithPull({
    pull: {
      number: 17,
      title: '<script>alert(1)</script>',
      author: '"><img src=x onerror=alert(2)>',
      branch: '<b>branch</b>',
      base: 'main',
      url: 'javascript:alert(3)',
      open: true,
    },
  });
  const view = caseFromLive({
    repo: 'acme/widgets',
    jobs: [
      failedJob({
        job_name: '<img src=x>',
        html_url: 'javascript:alert(4)',
        failed_step: { number: 1, name: '<script>', conclusion: 'failure' },
      }),
    ],
    readiness: hostile,
  });
  assert.equal(view.title, '<script>alert(1)</script>');
  assert.equal(view.author, '"><img src=x onerror=alert(2)>');
  assert.equal(view.jobs[0]?.name, '<img src=x>');
  assert.equal(view.jobs[0]?.step, '<script>');
  assert.equal(view.pullUrl, undefined);
  assert.equal(view.jobs[0]?.url, undefined);
  assert.equal(view.runUrl, undefined);
  assert.equal(view.repoUrl, undefined);
  assert.equal(view.commitUrl, undefined);
});

// A pull request's own address wins, but a good one is not needed to build a link
// to it when the repository's address is known.
test('the pull request address is built from the repository when readiness gives none usable', () => {
  const view = caseFromLive({
    repo: 'acme/widgets',
    jobs: [failedJob()],
    readiness: readinessWithPull({
      pull: {
        number: 17,
        title: 't',
        author: 'a',
        branch: 'b',
        base: 'main',
        url: 'javascript:alert(1)',
        open: true,
      },
    }),
  });
  assert.equal(view.pullUrl, `${REPO}/pull/17`);
});

test('an Enterprise Server job gives repository, run and commit links on its own host', () => {
  const url = 'https://ghe.example.com/acme/site/actions/runs/9/job/3';
  const view = caseFromLive({
    repo: 'acme/site',
    jobs: [failedJob({ html_url: url, run_number: 9 })],
    readiness: null,
  });
  assert.equal(view.repoUrl, 'https://ghe.example.com/acme/site');
  assert.equal(view.runUrl, 'https://ghe.example.com/acme/site/actions/runs/9');
  assert.equal(view.commitUrl, `https://ghe.example.com/acme/site/commit/${SHA}`);
});

// The snapshot is what the repair kept when it began. It answers the same
// questions as the live case from the row alone, long after the jobs are gone.
test('a case is read back from the snapshot a repair kept', () => {
  const row = repair({
    state: 'checks_failed',
    details: {
      case: {
        pull_title: 'Retry transient 502s',
        pull_url: `${REPO}/pull/17`,
        author: 'dana',
        branch: 'fix/retry-502',
        run_url: RUN,
        workflow: 'CI',
        run_number: 482,
        jobs: [
          {
            name: 'unit (ubuntu-latest)',
            step: 'Run go test ./...',
            url: JOB_URL,
            duration_ms: 95000,
          },
        ],
      },
    },
  });
  const view = caseFromSnapshot(row);
  assert.equal(view.repo, 'acme/widgets');
  assert.equal(view.title, 'Retry transient 502s');
  assert.equal(view.hasPull, true);
  assert.equal(view.pullNumber, 17);
  assert.equal(view.pullUrl, `${REPO}/pull/17`);
  assert.equal(view.repoUrl, REPO);
  assert.equal(view.author, 'dana');
  assert.equal(view.branch, 'fix/retry-502');
  assert.equal(view.sha, SHA);
  assert.equal(view.commitUrl, `${REPO}/commit/${SHA}`);
  assert.equal(view.runLabel, 'CI #482');
  assert.equal(view.runUrl, RUN);
  assert.equal(view.jobs.length, 1);
  assert.equal(view.jobs[0]?.name, 'unit (ubuntu-latest)');
  assert.equal(view.jobs[0]?.step, 'Run go test ./...');
  assert.equal(view.jobs[0]?.url, JOB_URL);
  assert.equal(view.jobs[0]?.durationMs, 95000);
  assert.equal(view.jobs[0]?.opened, false);
});

// A repair made before cases were kept, or one whose snapshot had nothing to
// hold, still opens: titled from what every row has.
test('a repair with no case is titled from its repository and pull request', () => {
  const view = caseFromSnapshot(repair({ repo: 'acme/widgets', pull_number: 17 }));
  assert.equal(view.title, 'Pull request #17 in acme/widgets');
  assert.equal(view.hasPull, false);
  assert.equal(view.pullNumber, 17);
  assert.deepEqual(view.jobs, []);
  assert.equal(view.sha, SHA);
  assert.equal(view.repoUrl, undefined);
  assert.equal(view.commitUrl, undefined);
  assert.equal(caseFromSnapshot(repair({ pull_number: 0 })).title, 'A repair in acme/widgets');
});

test('hostile text in a snapshot is text and its addresses are dropped', () => {
  const view = caseFromSnapshot(
    repair({
      details: {
        case: {
          pull_title: '<script>alert(1)</script>',
          pull_url: 'javascript:alert(2)',
          run_url: 'data:text/html,x',
          jobs: [{ name: '<b>x</b>', step: '<i>y</i>', url: 'javascript:alert(3)' }],
        },
      },
    }),
  );
  assert.equal(view.title, '<script>alert(1)</script>');
  assert.equal(view.pullUrl, undefined);
  assert.equal(view.runUrl, undefined);
  assert.equal(view.jobs[0]?.name, '<b>x</b>');
  assert.equal(view.jobs[0]?.url, undefined);
});

// The page lists a failed job's steps beside the one it stopped at, so a person
// sees what ran first and what never ran. A step that was skipped has no time.
test('a live job carries its steps, how each ended and how long it took', () => {
  const view = caseFromLive({
    repo: 'acme/widgets',
    readiness: null,
    jobs: [
      failedJob({
        steps: [
          {
            number: 1,
            name: 'Set up job',
            status: 'completed',
            conclusion: 'success',
            started_at: '2026-10-18T09:10:00Z',
            completed_at: '2026-10-18T09:10:02Z',
          },
          {
            number: 2,
            name: 'Run go test ./...',
            status: 'completed',
            conclusion: 'failure',
            started_at: '2026-10-18T09:10:02Z',
            completed_at: '2026-10-18T09:11:48Z',
          },
          { number: 3, name: 'Upload coverage', status: 'completed', conclusion: 'skipped' },
        ],
      }),
    ],
  });
  assert.deepEqual(view.jobs[0]?.steps, [
    { number: 1, name: 'Set up job', conclusion: 'success', durationMs: 2000 },
    { number: 2, name: 'Run go test ./...', conclusion: 'failure', durationMs: 106000 },
    { number: 3, name: 'Upload coverage', conclusion: 'skipped' },
  ]);
});

test('a job with no steps reported, and a job read from a snapshot, have none', () => {
  assert.equal(
    caseFromLive({ repo: 'acme/widgets', readiness: null, jobs: [failedJob()] }).jobs[0]?.steps,
    undefined,
  );
  assert.equal(
    caseFromLive({ repo: 'acme/widgets', readiness: null, jobs: [failedJob({ steps: [] })] })
      .jobs[0]?.steps,
    undefined,
  );
  const snapshot = caseFromSnapshot(repair({ details: { case: { jobs: [{ name: 'unit' }] } } }));
  assert.equal(snapshot.jobs[0]?.steps, undefined);
});

// The address names the run by its id, which is all that is known of it once
// Zoomies has pruned its jobs. It is shown as that, without a link: there is no
// host to build one on.
test('a run with no jobs on record is still named by its id', () => {
  const view = caseFromLive({ repo: 'acme/widgets', jobs: [], readiness: null, run: 1234 });
  assert.equal(view.runLabel, 'Run 1234');
  assert.equal(view.runUrl, undefined);
  // The jobs name the run better than its id does.
  const named = caseFromLive({
    repo: 'acme/widgets',
    jobs: [failedJob()],
    readiness: null,
    run: 1234,
  });
  assert.equal(named.runLabel, 'CI #482');
  assert.equal(
    caseFromLive({ repo: 'acme/widgets', jobs: [], readiness: null }).runLabel,
    undefined,
  );
});

// When Zoomies has pruned the jobs, the pull request's own address still says
// which host the repository is on, so the repository, the run and the commit
// stay links on the right host, and an Enterprise Server user is not sent to
// github.com for want of a job to read it off.
test('with no jobs, the pull request address gives the repository, run and commit links', () => {
  const view = caseFromLive({
    repo: 'acme/widgets',
    jobs: [],
    readiness: readinessWithPull(),
    run: 1234,
  });
  assert.equal(view.repoUrl, REPO);
  assert.equal(view.runUrl, `${REPO}/actions/runs/1234`);
  assert.equal(view.commitUrl, `${REPO}/commit/${SHA}`);

  const enterprise = readinessWithPull({
    pull: {
      number: 17,
      title: 'Retry transient 502s',
      author: 'dana',
      branch: 'fix/retry-502',
      base: 'main',
      url: 'https://ghe.example.com/acme/widgets/pull/17',
      open: true,
    },
  });
  const there = caseFromLive({ repo: 'acme/widgets', jobs: [], readiness: enterprise, run: 1234 });
  assert.equal(there.runUrl, 'https://ghe.example.com/acme/widgets/actions/runs/1234');

  // A pull request address that is not a web link gives nothing to build on.
  const hostile = readinessWithPull({
    pull: {
      number: 17,
      title: 't',
      author: 'a',
      branch: 'b',
      base: 'main',
      url: 'javascript:alert(1)',
      open: true,
    },
  });
  const none = caseFromLive({ repo: 'acme/widgets', jobs: [], readiness: hostile, run: 1234 });
  assert.equal(none.repoUrl, undefined);
  assert.equal(none.runUrl, undefined);

  // A job's own address wins over the pull request's.
  const both = caseFromLive({
    repo: 'acme/widgets',
    jobs: [failedJob()],
    readiness: readinessWithPull(),
    run: 482,
  });
  assert.equal(both.runUrl, RUN);
});

// A snapshot job has its address and no id of its own, and the page that follows
// a repair asks GitHub for its log by id, so the id is read off the address.
test('a snapshot job gets GitHub\u2019s id from its address', () => {
  const view = caseFromSnapshot(
    repair({ details: { case: { jobs: [{ name: 'unit', url: JOB_URL }, { name: 'lint' }] } } }),
  );
  assert.equal(view.jobs[0]?.githubJobId, 9001);
  assert.equal(view.jobs[1]?.githubJobId, undefined);
});

// A run this fleet never ran has no job on record anywhere in Zoomies. Readiness
// names what failed from GitHub instead, so the repair is of something the page
// can show: each job with its step, its steps, its duration and GitHub's id for
// its log.
test('a case with no fleet record names the failed jobs readiness read from GitHub', () => {
  const base = githubFailedJobs();
  const view = caseFromLive({
    repo: 'acme/widgets',
    jobs: [],
    readiness: readinessWithPull({ failed_jobs: base }),
    run: 482,
  });
  assert.equal(view.jobsFromGitHub, true);
  assert.equal(view.jobs.length, 2);
  const job = view.jobs[0]!;
  assert.equal(job.key, 'gh-7001');
  assert.equal(job.name, 'Publish Latest Image / build-and-push');
  assert.equal(job.step, 'Install dependencies and build');
  assert.equal(job.stepNumber, 8);
  assert.equal(job.url, 'https://github.com/acme/widgets/actions/runs/482/job/7001');
  assert.equal(job.githubJobId, 7001);
  assert.equal(job.durationMs, 5 * 60_000);
  assert.equal(job.opened, false);
  assert.deepEqual(
    view.jobs[0]!.steps?.map((step) => [step.number, step.name, step.conclusion]),
    [
      [1, 'Set up job', 'success'],
      [8, 'Install dependencies and build', 'failure'],
    ],
  );
  // The run link comes from the job's own address; the failure settled at the
  // latest completion time GitHub reported.
  assert.equal(view.runUrl, RUN);
  assert.equal(view.failedAt, '2026-10-23T10:05:00Z');
  // A job with no id carries GitHub's id, read off its address, which is what
  // its error output is asked with.
  const anonymous = caseFromLive({
    repo: 'acme/widgets',
    jobs: [],
    readiness: readinessWithPull({
      failed_jobs: [{ id: 0, name: 'unit', conclusion: 'failure', url: JOB_URL }],
    }),
  });
  assert.equal(anonymous.jobs[0]!.key, 'gh-0');
  assert.equal(anonymous.jobs[0]!.githubJobId, 9001);
});

// The fleet's own record is the richer one (blame, the runner it ran on), so
// wherever it exists, GitHub's report of the run is not asked or shown.
test('a fleet record of the failure leaves GitHub\u2019s failed jobs unread', () => {
  const view = caseFromLive({
    repo: 'acme/widgets',
    jobs: [failedJob()],
    readiness: readinessWithPull({ failed_jobs: githubFailedJobs() }),
  });
  assert.equal(view.jobsFromGitHub, undefined);
  assert.equal(view.jobs.length, 1);
  assert.equal(view.jobs[0]!.key, 'job_unit');
});
