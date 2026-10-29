<!--
  The Eli repair pages' components, in the states they can be in: a failure
  with and without its pull request, failed jobs with their error output
  locked, loaded, empty and absurdly long, the readiness checklist blocked and
  passed, the decision card at every stage, and every state a repair can end in;
  then the attempt page's own: the progress track at three points, the outcome
  of each kind of ending, the technical details with usage the provider never
  reported, what to try next and the facts and timeline of one attempt; and the
  hub's: the setup bar in each state, offers with the jobs that failed, the
  repairs that are running, and history rows with and without their boxes.

  The automatic repairs panel is here too: a repository ready to repair, one
  whose provider cannot pay, one whose allowance is spent, one with a repair
  running, its read failing with and without rows to keep, and the dialog that
  stops them for one repository, for several and when the stop did not go
  through.

  The error output is read from the controller when a job's disclosure opens,
  so this section answers that one request itself rather than needing a
  controller. Everything else passes through.
-->
<script lang="ts">
  import { onDestroy } from 'svelte';
  import Button from '$lib/components/Button.svelte';
  import Panel from '$lib/components/Panel.svelte';
  import type { Schemas } from '$lib/api/types';
  import { automaticNote } from '$lib/repairs/automatic';
  import CaseCard from '$lib/repairs/CaseCard.svelte';
  import ActiveRepairs from '$lib/repairs/ActiveRepairs.svelte';
  import AutomaticRepairs from '$lib/repairs/AutomaticRepairs.svelte';
  import AttemptFacts from '$lib/repairs/AttemptFacts.svelte';
  import AttemptTimeline from '$lib/repairs/AttemptTimeline.svelte';
  import AutomaticCard from '$lib/repairs/AutomaticCard.svelte';
  import AutomaticNote from '$lib/repairs/AutomaticNote.svelte';
  import DecisionCard from '$lib/repairs/DecisionCard.svelte';
  import FailedJobs from '$lib/repairs/FailedJobs.svelte';
  import NextSteps from '$lib/repairs/NextSteps.svelte';
  import OfferList from '$lib/repairs/OfferList.svelte';
  import OutcomeBanner from '$lib/repairs/OutcomeBanner.svelte';
  import ProgressTrack from '$lib/repairs/ProgressTrack.svelte';
  import ReadinessChecklist from '$lib/repairs/ReadinessChecklist.svelte';
  import RepairRow from '$lib/repairs/RepairRow.svelte';
  import RepairStatusBadge from '$lib/repairs/RepairStatusBadge.svelte';
  import SetupBar from '$lib/repairs/SetupBar.svelte';
  import StopAutomaticDialog from '$lib/repairs/StopAutomaticDialog.svelte';
  import TechnicalDetails from '$lib/repairs/TechnicalDetails.svelte';
  import type { CaseJob, CaseView } from '$lib/repairs/case';
  import { checkRows } from '$lib/repairs/checks';

  const REPO_URL = 'https://github.com/eyupio/zoomies';
  const SHA = 'a41c9e2f0b7d5c3e8a1f6d4b2c9e7a5f3d1b8c6e';
  const failedAt = new Date(Date.now() - 14 * 60_000).toISOString();

  const withPull: CaseView = {
    repo: 'eyupio/zoomies',
    repoUrl: REPO_URL,
    title: 'Retry transient 502s in the registry client',
    hasPull: true,
    pullNumber: 1131,
    pullUrl: `${REPO_URL}/pull/1131`,
    author: 'dana',
    branch: 'fix/registry-retry',
    base: 'main',
    sha: SHA,
    commitUrl: `${REPO_URL}/commit/${SHA}`,
    runLabel: 'CI #482',
    runUrl: `${REPO_URL}/actions/runs/482`,
    failedAt,
    ran: { pool: 'linux-xl', runner: 'zoomies-linux-xl-3', poolId: 'pool_xl', runnerId: 'run_xl3' },
    jobs: [
      {
        key: 'job_a',
        name: 'Go (api-2)',
        step: 'Run go test -race ./internal/...',
        stepNumber: 7,
        domain: 'workflow',
        opened: true,
      },
      {
        key: 'job_b',
        name: 'Lint',
        step: 'Run staticcheck ./...',
        stepNumber: 5,
        domain: 'workflow',
        opened: false,
      },
    ],
  };
  const withoutPull: CaseView = {
    ...withPull,
    title: 'CI #482 failed on fix/registry-retry',
    hasPull: false,
    pullNumber: undefined,
    pullUrl: undefined,
    author: undefined,
    base: undefined,
  };

  const step = (number: number, name: string, conclusion: string, durationMs?: number) => ({
    number,
    name,
    conclusion,
    durationMs,
  });
  const workflowJob: CaseJob = {
    key: 'job_a',
    name: 'Go (api-2)',
    step: 'Run go test -race ./internal/...',
    stepNumber: 7,
    url: `${REPO_URL}/actions/runs/482/job/4821`,
    durationMs: 192_000,
    domain: 'workflow',
    opened: true,
    githubJobId: 4821,
    steps: [
      step(1, 'Set up job', 'success', 2000),
      step(2, 'Check out the repository', 'success', 4000),
      step(3, 'Set up Go', 'success', 9000),
      step(4, 'Download modules', 'success', 14_000),
      step(5, 'Build', 'success', 38_000),
      step(6, 'Vet', 'success', 19_000),
      step(7, 'Run go test -race ./internal/...', 'failure', 106_000),
      step(8, 'Upload coverage', 'skipped'),
      step(9, 'Post check out the repository', 'skipped'),
    ],
  };
  const fleetJob: CaseJob = {
    key: 'job_b',
    name: 'Integration (shard 3)',
    step: 'Run integration suite',
    stepNumber: 4,
    url: `${REPO_URL}/actions/runs/482/job/4822`,
    durationMs: 611_000,
    domain: 'fleet',
    faultLabel: 'Out of memory',
    fix: 'Give this pool runners with more memory, or lower the job’s parallelism, then re-run it.',
    opened: false,
    githubJobId: 4822,
  };
  const longJob: CaseJob = {
    key: 'job_c',
    name: 'Docs (a job name that is long enough to need wrapping on a phone: github.com/eyupio/zoomies/internal/registry/client_test.go)',
    step: 'Run the documentation build with a step name that goes on and on without any spaces to break at: ----------------------------------------------',
    stepNumber: 3,
    domain: 'workflow',
    opened: false,
    githubJobId: 4823,
  };
  const goneJob: CaseJob = {
    key: 'job_d',
    name: 'Nightly soak',
    step: 'Run the soak',
    stepNumber: 2,
    domain: 'workflow',
    opened: false,
    githubJobId: 4824,
  };
  const lockedJob: CaseJob = {
    key: 'job_e',
    name: 'Go (api-2)',
    step: 'Run go test -race ./internal/...',
    stepNumber: 7,
    domain: 'workflow',
    opened: true,
    githubJobId: 4825,
  };

  // What the controller would answer for each job, by GitHub's job id.
  const ANSWERS: Record<string, { lines: string[]; note?: string }> = {
    '4821': {
      lines: [
        '--- FAIL: TestRegistryRetriesTransient502 (0.31s)',
        '    client_test.go:88: got 1 attempt, want 3',
        'FAIL\tgithub.com/eyupio/zoomies/internal/registry\t0.412s',
        'Process completed with exit code 1.',
      ],
    },
    '4823': {
      lines: [
        `Error: ${'a-very-long-error-line-with-no-break-points-'.repeat(14)}`,
        '<script>alert("an error line is text")</' + 'script>',
      ],
    },
    '4824': {
      lines: [],
      note: 'GitHub no longer has this log, or the job printed no error lines.',
    },
  };

  const realFetch = globalThis.fetch;
  globalThis.fetch = async (input, init) => {
    const url = new URL(
      typeof input === 'string' ? input : input instanceof URL ? input.href : input.url,
      location.href,
    );
    if (url.pathname.endsWith('/assistant/repairs/error-output')) {
      const answer = ANSWERS[url.searchParams.get('job_id') ?? ''];
      await new Promise((resolve) => setTimeout(resolve, 200));
      return new Response(
        JSON.stringify(
          answer ?? {
            lines: [],
            note: 'GitHub no longer has this log, or the job printed no error lines.',
          },
        ),
        {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        },
      );
    }
    return realFetch(input, init);
  };
  onDestroy(() => {
    globalThis.fetch = realFetch;
  });

  type Check = Schemas['EliReadinessCheck'];
  const ORDER: Check['code'][] = [
    'identity',
    'provider',
    'installation',
    'access',
    'policy',
    'budget',
    'pull',
    'checks',
    'permissions',
  ];
  const MESSAGES: Record<Check['code'], string> = {
    identity: '@dana is linked to your Zoomies account and has allowed repairs.',
    provider: 'Personal Anthropic is enabled. Eli uses your provider, never a shared one.',
    installation: 'eyupio/zoomies is served by the eyupio GitHub installation.',
    access: '@dana can write to eyupio/zoomies.',
    policy: 'Manual repair is on for this repository. Workflow file edits are off.',
    budget: '4 attempts left today.',
    pull: 'Pull request #1131 is open and its head is a41c9e2.',
    checks: 'The latest checks are still failing.',
    permissions:
      'Contents and pull requests write, and actions, checks and commit statuses read, are granted.',
  };
  const ACCESS = 'Your linked GitHub account needs write access to this repository.';
  const blockedChecks: Check[] = ORDER.map((code, i): Check => {
    const at = ORDER.indexOf('access');
    if (i < at) return { code, status: 'ok', message: MESSAGES[code] };
    if (i === at) return { code, status: 'blocked', message: ACCESS };
    return { code, status: 'unchecked', message: 'Not checked yet.' };
  });
  const passedChecks: Check[] = ORDER.map((code): Check => ({
    code,
    status: 'ok',
    message: MESSAGES[code],
  }));

  const readiness = (over: Partial<Schemas['EliReadiness']>): Schemas['EliReadiness'] => ({
    ready: true,
    repo: 'eyupio/zoomies',
    pull_number: 1131,
    head_sha: SHA,
    pull: {
      number: 1131,
      title: withPull.title,
      author: 'dana',
      branch: 'fix/registry-retry',
      base: 'main',
      url: `${REPO_URL}/pull/1131`,
      open: true,
    },
    provider: 'Personal Anthropic',
    model: 'claude-sonnet',
    allow_workflows: false,
    remaining: 4,
    reset_at: new Date(Date.now() + 5 * 3_600_000).toISOString(),
    blockers: [],
    checks: passedChecks,
    ...over,
  });
  const blocked = readiness({
    ready: false,
    pull: undefined,
    blockers: [{ code: 'access', message: ACCESS }],
    checks: blockedChecks,
  });
  const ready = readiness({});
  const superseded = readiness({
    ready: false,
    blockers: [
      {
        code: 'superseded',
        message:
          'The pull request has a newer commit since you reviewed this failure. No repair was started and no attempt was used.',
      },
    ],
  });

  let instruction = $state('');

  // One repair in each of the shapes the attempt page draws. The clock is fixed so
  // the elapsed time reads the same in every capture.
  const NOW = Date.now();
  const iso = (ms: number): string => new Date(ms).toISOString();
  const repair = (over: Partial<Schemas['EliRepair']>): Schemas['EliRepair'] => ({
    id: 'eli_01hz8k3m7qfx',
    installation_id: 'ins_1',
    repo: 'eyupio/zoomies',
    head_sha: SHA,
    commit_sha: '',
    user_id: 'usr_1',
    github_login: 'dana',
    provider_id: 'prv_1',
    trigger: 'mention',
    state: 'working',
    message: '',
    pull_number: 1131,
    job_id: 0,
    run_id: 482,
    github_user_id: 42,
    comment_id: 0,
    created_at: iso(NOW - 63_000),
    updated_at: iso(NOW - 2000),
    ...over,
  });
  const PUSHED = 'b7d3e9150c2a4f8e6d1b0a93c5e7f2a4d6b8c0e1';
  const queued = repair({ state: 'queued' });
  const preparing = repair({ details: { stage: 'prepare_fix', model: 'claude-sonnet' } });
  // A repair the repository's policy started, which says so in its facts.
  const autoAttempt = repair({
    trigger: 'automatic',
    github_login: '',
    user_id: '',
    details: { stage: 'prepare_fix', model: 'claude-sonnet' },
  });
  const watching = repair({
    state: 'checking',
    commit_sha: PUSHED,
    details: { stage: 'watch_ci', model: 'claude-sonnet' },
  });
  const calls = [
    {
      input_tokens: 21_400,
      output_tokens: 1800,
      usage_reported: true,
      request_bytes: 80_312,
      elapsed_ms: 41_000,
      response_recorded: true,
      response:
        '{"read":["package.json"],"edits":[{"path":"package.json","before":"old","after":"fixed"}]}',
      validation_error: 'Do not combine source requests with changes.',
    },
    {
      input_tokens: 0,
      output_tokens: 0,
      usage_reported: false,
      request_bytes: 91_234,
      elapsed_ms: 9000,
      response_recorded: true,
      response:
        '{"cannot_fix":"The missing lockfile window is needed to choose a supported upgrade."}',
      response_truncated: true,
    },
  ];
  const finish = (over: Partial<Schemas['EliRepair']>): Schemas['EliRepair'] =>
    repair({
      created_at: iso(NOW - 68_000),
      updated_at: iso(NOW),
      details: {
        stage: 'prepare_fix',
        model: 'claude-sonnet',
        source_bytes: 38_912,
        steps: calls,
      },
      ...over,
    });
  const couldNot = finish({
    state: 'failed',
    message: 'Eli could not produce a fix it was willing to push.',
    details: {
      stage: 'prepare_fix',
      failure_code: 'unsupported_fix',
      model: 'claude-sonnet',
      source_bytes: 38_912,
      steps: calls,
    },
  });
  const passed = finish({
    state: 'succeeded',
    commit_sha: PUSHED,
    message: 'Checks passed on Eli’s commit b7d3e91.',
    details: { stage: 'watch_ci', model: 'claude-sonnet', source_bytes: 38_912, steps: calls },
  });
  const stillFailing = finish({
    state: 'checks_failed',
    commit_sha: PUSHED,
    message: 'Eli pushed b7d3e91 and the checks on it still fail.',
    details: {
      stage: 'watch_ci',
      failure_code: 'checks_failed',
      model: 'claude-sonnet',
      steps: calls,
    },
  });
  const noCalls = finish({
    state: 'blocked',
    message: 'The pull request is closed.',
    details: { stage: 'evidence' },
  });
  const newer = finish({ state: 'a-state-from-a-newer-controller', message: '' });
  const OUTCOMES = [couldNot, passed, stillFailing, noCalls, newer];

  // The hub: the setup bar in each state it can be in.
  const policy = (repo: string): Schemas['EliRepairPolicy'] => ({
    repo,
    installation_id: 'ins_1',
    provider_id: 'prv_1',
    enabled: true,
    automatic: false,
    ask_first: true,
    allow_workflows: false,
    daily_limit: 5,
  });
  const linked = (confirmed: boolean): Schemas['EliRepairSettings'] => ({
    identities: [{ user_id: 'usr_1', github_user_id: 42, github_login: 'dana', confirmed }],
    policies: [policy('eyupio/zoomies'), policy('eyupio/site'), policy('eyupio/docs')],
  });
  const nobody: Schemas['EliRepairSettings'] = { identities: [], policies: [] };
  // Repositories that repair themselves: settings for the setup line, and the
  // overview's rows in each state the panel can draw.
  const automaticPolicy = (repo: string): Schemas['EliRepairPolicy'] => ({
    ...policy(repo),
    automatic: true,
    ask_first: false,
  });
  const withAutomatic: Schemas['EliRepairSettings'] = {
    identities: linked(true).identities,
    policies: [
      automaticPolicy('eyupio/zoomies'),
      automaticPolicy('eyupio/site'),
      policy('eyupio/docs'),
    ],
  };
  const automaticRow = (
    over: Partial<Schemas['EliAutomaticRepository']>,
  ): Schemas['EliAutomaticRepository'] => ({
    repo: 'eyupio/zoomies',
    installation_id: 'ins_1',
    provider_id: 'prv_1',
    provider_name: 'Team provider',
    model: 'claude-sonnet-5-5',
    allow_workflows: false,
    daily_limit: 5,
    used: 2,
    running: 0,
    ...over,
  });
  const automaticRows: Schemas['EliAutomaticRepository'][] = [
    automaticRow({
      repo: 'eyupio/site',
      provider_id: '',
      provider_name: '',
      model: '',
      used: 0,
      problem:
        'No installation provider is set for this repository, so automatic repairs cannot start. Choose one in setup.',
    }),
    automaticRow({
      repo: 'eyupio/docs-with-a-very-long-repository-name-that-keeps-going',
      provider_name: 'A provider with a rather long name',
      model: 'a-model-with-a-long-name-4-5-20261018',
      used: 5,
      next_free_at: iso(NOW + 40 * 60_000),
      last: {
        id: 'eli_docs',
        state: 'checks_failed',
        pull_number: 88,
        updated_at: iso(NOW - 50 * 60_000),
      },
    }),
    automaticRow({
      repo: 'eyupio/zoomies',
      used: 3,
      running: 1,
      allow_workflows: true,
      last: {
        id: 'eli_pass',
        state: 'succeeded',
        pull_number: 1127,
        updated_at: iso(NOW - 2 * 3_600_000),
      },
    }),
  ];
  // What the repair page says about a repository that repairs itself, for each
  // reader and each reason.
  const aboutAutomatic = (
    over: Partial<Parameters<typeof automaticNote>[0]>,
  ): NonNullable<ReturnType<typeof automaticNote>> =>
    automaticNote({
      mode: 'automatic',
      admin: true,
      attempt: undefined,
      repository: undefined,
      now: NOW,
      ...over,
    })!;
  const notes = {
    tried: aboutAutomatic({
      attempt: repair({ id: 'eli_tried', trigger: 'automatic', state: 'checks_failed' }),
    }),
    none: aboutAutomatic({}),
    spent: aboutAutomatic({ repository: automaticRows[1] }),
    broken: aboutAutomatic({ repository: automaticRows[0] }),
    neutral: aboutAutomatic({ admin: false }),
  };
  let stopOne = $state(false);
  let stopMany = $state(false);
  let stopFailed = $state(false);

  // Offers, each with the jobs that failed in its run.
  const offer = (over: Partial<Schemas['EliOpportunity']>): Schemas['EliOpportunity'] => ({
    id: 'opp_1',
    repo: 'eyupio/zoomies',
    installation_id: 'ins_1',
    pull_number: 1142,
    head_sha: SHA,
    run_id: 482,
    title: 'Cache the Playwright browsers between UI test runs',
    state: 'open',
    created_at: iso(NOW - 6 * 60_000),
    updated_at: iso(NOW - 6 * 60_000),
    ...over,
  });
  const job = (key: string, name: string, step: string, stepNumber: number): CaseJob => ({
    key,
    name,
    step,
    stepNumber,
    domain: 'workflow',
    opened: false,
  });
  const offers = [
    offer({}),
    offer({
      id: 'opp_2',
      pull_number: 1139,
      title: 'Fix the pool wizard’s size step on small screens',
    }),
    offer({
      id: 'opp_3',
      pull_number: 1150,
      title:
        'A very long pull request title that goes on and on, with a-path/that/has/no/spaces/to/break/at/all/and/keeps/going/until/it/must/wrap',
    }),
  ];
  const offerViews: Record<string, CaseView> = {
    opp_1: {
      ...withPull,
      title: offers[0]!.title,
      pullNumber: 1142,
      branch: 'ci/cache-playwright',
      failedAt: iso(NOW - 6 * 60_000),
      jobs: [job('a', 'UI (e2e)', 'Install browsers', 4)],
    },
    opp_2: {
      ...withPull,
      title: offers[1]!.title,
      pullNumber: 1139,
      branch: 'fix/pool-wizard-size',
      failedAt: iso(NOW - 31 * 60_000),
      jobs: [
        job('a', 'UI (e2e)', 'Playwright mobile', 7),
        job('b', 'UI unit', 'Run the unit tests', 5),
      ],
    },
    opp_3: {
      ...withPull,
      title: offers[2]!.title,
      pullNumber: 1150,
      branch: `fix/${'a-very-long-branch-name-'.repeat(4)}end`,
      failedAt: undefined,
      jobs: [
        job(
          'a',
          'A job name that is long enough to need wrapping on a phone: internal/registry/client_test.go',
          'Run the whole suite with a step name that has no spaces to break at: ----------------------------',
          3,
        ),
      ],
    },
  };

  // The repairs that are running, and the rows of the history, with their cases.
  const withCase = (
    r: Schemas['EliRepair'],
    title: string,
    branch: string,
    jobs: { name: string; step?: string }[],
  ): Schemas['EliRepair'] => ({
    ...r,
    details: { ...r.details, case: { pull_title: title, author: 'dana', branch, jobs } },
  });
  const goingNow = withCase(
    repair({
      id: 'eli_run1',
      pull_number: 1131,
      details: { stage: 'prepare_fix', model: 'claude-sonnet' },
    }),
    'Retry transient 502s in the registry client',
    'fix/registry-retry',
    [{ name: 'Go (api-2)', step: 'Run go test -race ./internal/...' }],
  );
  const goingQueued = withCase(
    repair({
      id: 'eli_run2',
      state: 'queued',
      trigger: 'automatic',
      github_login: '',
      pull_number: 1133,
    }),
    'Pin the base image digest in the runner Dockerfile',
    'chore/pin-base-digest',
    [],
  );
  const rows = [
    withCase(goingNow, goingNow.details?.case?.pull_title ?? '', 'fix/registry-retry', [
      { name: 'Go (api-2)', step: 'Run go test -race ./internal/...' },
      { name: 'Lint', step: 'staticcheck' },
    ]),
    withCase(
      { ...passed, id: 'eli_pass', trigger: 'automatic', github_login: '', pull_number: 1127 },
      'Add a status line to the repairs page',
      'feat/repair-status',
      [{ name: 'Lint', step: 'staticcheck' }],
    ),
    withCase(
      {
        ...newer,
        id: 'eli_newer',
        state: 'superseded',
        message: 'The PR moved on before the checks answered',
        pull_number: 1119,
      },
      'Pin the base image digest in the runner Dockerfile',
      'chore/pin-base-digest',
      [{ name: 'Build runner image', step: 'docker build' }],
    ),
    withCase(
      { ...couldNot, id: 'eli_lost', pull_number: 1096 },
      'Show the retention window on the Usage page',
      'feat/usage-retention',
      [
        { name: 'UI (e2e)', step: 'Playwright' },
        { name: 'UI unit', step: 'vitest' },
        { name: 'Docs', step: 'mkdocs build' },
      ],
    ),
    // From before cases were kept: the row and nothing about the failure.
    { ...stillFailing, id: 'eli_old', pull_number: 1002 },
  ];
  const longRow = withCase(
    {
      ...couldNot,
      id: 'eli_long',
      pull_number: 1200,
      message: `Error: ${'x'.repeat(260)}`,
    },
    'A very long pull request title that goes on and on, with a-path/that/has/no/spaces/to/break/at/all',
    `fix/${'a-very-long-branch-name-'.repeat(4)}end`,
    [
      {
        name: 'A job name that is long enough to need wrapping on a phone: client_test.go',
        step: 'Run it',
      },
    ],
  );

  const STATES = [
    'queued',
    'working',
    'checking',
    'succeeded',
    'checks_failed',
    'failed',
    'blocked',
    'interrupted',
    'superseded',
    'unverified',
  ];
  const noop = (): void => {};
</script>

<Panel
  title="Eli repairs"
  description="The components of the repair page, in the states it can be in."
>
  <div class="stack" data-gallery="eli-repairs">
    <h3>The case</h3>
    <div class="pair">
      <div data-gallery="case-pull"><CaseCard view={withPull} /></div>
      <div data-gallery="case-no-pull"><CaseCard view={withoutPull} /></div>
    </div>

    <h3>What failed</h3>
    <div class="pair">
      <div data-gallery="jobs-loaded">
        <FailedJobs jobs={[workflowJob, fleetJob]} repo="eyupio/zoomies" canReadLogs={true} />
      </div>
      <div data-gallery="jobs-locked">
        <FailedJobs
          jobs={[lockedJob]}
          repo="eyupio/zoomies"
          canReadLogs={false}
          logsBlockedReason="Job logs can hold anything a workflow prints, so Zoomies reads them only for someone whose GitHub account can write to this repository. Fix the access check and the error lines appear here."
        />
      </div>
    </div>
    <div class="pair">
      <div data-gallery="jobs-long">
        <FailedJobs jobs={[longJob]} repo="eyupio/zoomies" canReadLogs={true} />
      </div>
      <div data-gallery="jobs-gone">
        <FailedJobs jobs={[goneJob]} repo="eyupio/zoomies" canReadLogs={true} />
      </div>
    </div>

    <h3>Can Eli act on this?</h3>
    <div class="pair">
      <div data-gallery="checks-blocked">
        <ReadinessChecklist
          rows={checkRows(blocked, REPO_URL)}
          busy={false}
          onrecheck={noop}
          onsetup={noop}
          onchoose={noop}
        />
      </div>
      <div data-gallery="checks-passed">
        <ReadinessChecklist
          rows={checkRows(ready, REPO_URL)}
          busy={false}
          onrecheck={noop}
          onsetup={noop}
          onchoose={noop}
        />
      </div>
    </div>

    <h3>The decision</h3>
    <div class="pair">
      <div data-gallery="decision-blocked">
        <DecisionCard
          stage="blocked"
          readiness={blocked}
          rows={checkRows(blocked, REPO_URL)}
          busy={false}
          bind:instruction
          allowInstruction={true}
          onstart={noop}
          onrecheck={noop}
          onsetup={noop}
        />
      </div>
      <div data-gallery="decision-ready">
        <DecisionCard
          stage="ready"
          readiness={ready}
          rows={checkRows(ready, REPO_URL)}
          busy={false}
          bind:instruction
          allowInstruction={true}
          onstart={noop}
          onrecheck={noop}
          onsetup={noop}
        />
      </div>
    </div>
    <div class="pair">
      <div data-gallery="decision-changed">
        <DecisionCard
          stage="changed"
          readiness={superseded}
          rows={checkRows(superseded, REPO_URL)}
          busy={false}
          bind:instruction
          allowInstruction={true}
          runUrl={withPull.runUrl}
          onstart={noop}
          onrecheck={noop}
          onsetup={noop}
        />
      </div>
      <div data-gallery="decision-checking">
        <DecisionCard
          stage="checking"
          readiness={null}
          rows={[]}
          busy={true}
          bind:instruction
          allowInstruction={true}
          onstart={noop}
          onrecheck={noop}
          onsetup={noop}
        />
      </div>
    </div>
    <div class="pair">
      <div data-gallery="decision-error">
        <DecisionCard
          stage="error"
          readiness={null}
          rows={[]}
          busy={false}
          bind:instruction
          allowInstruction={true}
          error="The controller did not answer. Check that it is running, then try again."
          onstart={noop}
          onrecheck={noop}
          onsetup={noop}
        />
      </div>
    </div>

    <h3>Progress</h3>
    <div class="trio">
      <div data-gallery="progress-queued"><ProgressTrack repair={queued} now={NOW} /></div>
      <div data-gallery="progress-preparing"><ProgressTrack repair={preparing} now={NOW} /></div>
      <div data-gallery="progress-watching"><ProgressTrack repair={watching} now={NOW} /></div>
    </div>

    <h3>How it ended</h3>
    <div class="pair" data-gallery="outcomes">
      {#each OUTCOMES as outcome (outcome.state)}
        <OutcomeBanner repair={outcome} />
      {/each}
    </div>

    <h3>Technical details</h3>
    <div class="pair">
      <div data-gallery="tech-finished"><TechnicalDetails repair={couldNot} /></div>
      <div data-gallery="tech-running"><TechnicalDetails repair={preparing} /></div>
    </div>
    <div class="pair">
      <div data-gallery="tech-none"><TechnicalDetails repair={noCalls} /></div>
    </div>

    <h3>What to try next</h3>
    <div class="pair">
      <div data-gallery="next-failed"><NextSteps repair={couldNot} view={withPull} /></div>
      <div data-gallery="next-passed"><NextSteps repair={passed} view={withPull} /></div>
    </div>
    <div class="pair">
      <div data-gallery="next-unknown">
        <NextSteps repair={newer} view={{ ...withoutPull, pullUrl: undefined }} />
      </div>
    </div>

    <h3>This attempt</h3>
    <div class="pair">
      <div data-gallery="facts-running"><AttemptFacts repair={preparing} view={withPull} /></div>
      <div data-gallery="facts-finished">
        <AttemptFacts repair={passed} view={withPull} />
        <AttemptTimeline repair={passed} />
      </div>
    </div>

    <h3>The repair page's note about automatic repairs</h3>
    <div class="pair">
      <div class="stack" data-gallery="automatic-notes-a">
        <AutomaticNote note={notes.tried} />
        <AutomaticNote note={notes.none} />
        <AutomaticNote note={notes.neutral} />
      </div>
      <div class="stack" data-gallery="automatic-notes-b">
        <AutomaticNote note={notes.spent} />
        <AutomaticNote note={notes.broken} />
      </div>
    </div>

    <h3>An automatic repair's policy</h3>
    <div class="pair">
      <div class="stack" data-gallery="automatic-cards-a">
        <AutomaticCard
          repo="eyupio/zoomies"
          repository={automaticRows[2]}
          loaded={true}
          failed={false}
          unpushed={true}
          onstop={noop}
          onretry={noop}
        />
        <AutomaticCard
          repo="eyupio/site"
          repository={automaticRows[0]}
          loaded={true}
          failed={false}
          unpushed={false}
          onstop={noop}
          onretry={noop}
        />
        <div data-gallery="facts-automatic">
          <AttemptFacts repair={autoAttempt} view={withPull} />
        </div>
      </div>
      <div class="stack" data-gallery="automatic-cards-b">
        <AutomaticCard
          repo="eyupio/zoomies"
          repository={undefined}
          loaded={true}
          failed={false}
          unpushed={false}
          onstop={noop}
          onretry={noop}
        />
        <AutomaticCard
          repo="eyupio/zoomies"
          repository={undefined}
          loaded={true}
          failed={true}
          unpushed={false}
          onstop={noop}
          onretry={noop}
        />
        <AutomaticCard
          repo="eyupio/zoomies"
          repository={undefined}
          loaded={false}
          failed={false}
          unpushed={false}
          onstop={noop}
          onretry={noop}
        />
      </div>
    </div>

    <h3>The hub's setup bar</h3>
    <div class="stack" data-gallery="setup-bars">
      <SetupBar settings={linked(true)} admin={true} userId="usr_1" />
      <SetupBar settings={linked(false)} admin={false} userId="usr_1" />
      <SetupBar settings={nobody} admin={false} userId="usr_1" />
      <SetupBar settings={nobody} admin={false} userId="usr_1" authDisabled={true} />
      <SetupBar settings={null} admin={false} userId="usr_1" />
      <SetupBar settings={withAutomatic} admin={true} userId="usr_1" />
    </div>

    <h3>Needs you</h3>
    <div data-gallery="offers">
      <OfferList {offers} cases={offerViews} busy={false} ondismiss={noop} />
    </div>

    <h3>In progress</h3>
    <div data-gallery="active"><ActiveRepairs items={[goingNow, goingQueued]} total={2} /></div>

    <h3>Automatic repairs</h3>
    <div class="stack">
      <div data-gallery="automatic">
        <AutomaticRepairs repositories={automaticRows} onstop={noop} />
      </div>
      <div class="pair">
        <div data-gallery="automatic-one">
          <AutomaticRepairs repositories={[automaticRows[2]!]} onstop={noop} />
        </div>
        <div data-gallery="automatic-stale">
          <AutomaticRepairs repositories={[automaticRows[2]!]} error={true} onstop={noop} />
        </div>
      </div>
      <div data-gallery="automatic-unread">
        <AutomaticRepairs repositories={[]} error={true} onstop={noop} />
      </div>
      <div class="badges" data-gallery="automatic-dialogs">
        <Button variant="secondary" onclick={() => (stopOne = true)}>Stop one repository</Button>
        <Button variant="secondary" onclick={() => (stopMany = true)}>Stop several</Button>
        <Button variant="secondary" onclick={() => (stopFailed = true)}>Stop that fails</Button>
      </div>
    </div>
    <StopAutomaticDialog bind:open={stopOne} repos={['eyupio/zoomies']} onconfirm={noop} />
    <StopAutomaticDialog
      bind:open={stopMany}
      repos={['eyupio/zoomies', 'eyupio/site', 'eyupio/docs', 'eyupio/web', 'eyupio/api']}
      onconfirm={noop}
    />
    <StopAutomaticDialog
      bind:open={stopFailed}
      repos={['eyupio/zoomies']}
      error="eyupio/zoomies: Could not save this repository’s policy. Try again."
      onconfirm={noop}
    />

    <h3>History</h3>
    <div class="pair">
      <div class="rows" data-gallery="rows">
        {#each rows as r (r.id)}
          <RepairRow
            repair={r}
            selecting={false}
            selected={false}
            waiting={false}
            onselect={noop}
          />
        {/each}
        <RepairRow
          repair={longRow}
          selecting={false}
          selected={false}
          waiting={false}
          onselect={noop}
        />
      </div>
      <div class="rows" data-gallery="rows-selecting">
        {#each rows.slice(0, 3) as r, index (r.id)}
          <RepairRow
            repair={r}
            selecting={true}
            selected={index === 1}
            waiting={false}
            onselect={noop}
          />
        {/each}
      </div>
    </div>

    <h3>A repair's state</h3>
    <div class="badges" data-gallery="repair-status">
      {#each STATES as state (state)}<RepairStatusBadge {state} />{/each}
      <RepairStatusBadge state="a-state-from-a-newer-controller" />
    </div>
  </div>
</Panel>

<style>
  .stack {
    display: flex;
    flex-direction: column;
    gap: var(--z-space-4);
  }
  h3 {
    margin: var(--z-space-2) 0 0;
    font-size: var(--z-text-sm);
    font-weight: var(--z-weight-semibold);
    color: var(--z-text-muted);
  }
  .pair {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--z-space-4);
    align-items: start;
  }
  .rows {
    min-width: 0;
    border: var(--z-border-width) solid var(--z-border);
    border-radius: var(--z-radius-md);
    background: var(--z-surface);
    overflow: hidden;
  }
  .trio {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--z-space-4);
    align-items: start;
  }
  .badges {
    display: flex;
    flex-wrap: wrap;
    gap: var(--z-space-2);
  }
  @media (max-width: 960px) {
    .pair,
    .trio {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
