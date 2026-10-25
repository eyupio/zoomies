import { expect, test } from '@playwright/test';
import { goto, readThrough } from './support/fixtures';

const distribution = (zero = false) => ({
  samples: zero ? 1 : 0,
  missing: zero ? 0 : 1,
  min_ms: zero ? 0 : null,
  p50_ms: zero ? 0 : null,
  p95_ms: zero ? 0 : null,
  max_ms: zero ? 0 : null,
  total_ms: zero ? 0 : null,
});
const counts = {
  selected: 1,
  completed: 1,
  succeeded: 0,
  failed: 1,
  cancelled: 0,
  superseded: 0,
  unknown: 0,
};
const arm = (run: number, zero = false) => ({
  source_revision: 'a'.repeat(40),
  workflow_revision: '',
  inputs_id: '',
  test_scope_id: '',
  configuration_id: '',
  image_id: '',
  resource_id: '',
  selection_complete: false,
  attempts: [
    {
      source_revision: 'a'.repeat(40),
      workflow_id: 20,
      latency_ms: null,
      execution_ms: zero ? 0 : null,
      allocated_idle_ms: null,
      run_id: run,
      attempt: 1,
      observed_at: '2026-10-13T00:00:00Z',
      outcome: 'failure',
      cache: '',
      host_condition: '',
    },
  ],
});
const measures = (zero = false) => ({
  successful_latency: distribution(),
  all_attempt_execution: distribution(zero),
  all_attempt_allocated_idle: distribution(),
  failure_rate: 1,
});
const retained = {
  id: 'cmp_fixture',
  sha256: 'd'.repeat(64),
  created_at: '2026-10-13T00:00:00Z',
  report: {
    window_basis: 'github_attempt_started_at',
    execution_basis: 'sum_of_all_github_job_elapsed_times',
    snapshot: {
      rule_version: 'selected-runs-v2',
      kind: 'runner',
      repository: 'acme/repo',
      initiating_user: 'fixture',
      hypothesis: '<script>window.injected = true</script>',
      intended_delta: 'larger runner',
      evidence_id: 'cmp_fixture',
      revision_delta_id: '',
      since: '2026-10-12T00:00:00Z',
      until: '2026-10-14T00:00:00Z',
      baseline: arm(10, true),
      candidate: arm(11),
    },
    measurement: {
      version: 'selected-measures-v1',
      eligibility: {
        rule_version: 'selected-runs-v2',
        state: 'inconclusive',
        reasons: ['selection_incomplete', 'inputs_unknown', 'cache_unknown'],
        minimum_samples: 30,
        baseline: counts,
        candidate: counts,
      },
      baseline: measures(true),
      candidate: measures(),
      latency_change: 'not_enough',
      execution_mean_change: 'not_enough',
      allocated_idle_mean_change: 'not_enough',
      failure_rate_change: 'not_enough',
    },
  },
};

const runnerEvidence = {
  version: 'runner-evidence-v1',
  baseline: {
    selected_jobs: 101,
    matched_jobs: 101,
    recorded_grants: 101,
    runner_observations: 1,
    rows_omitted: true,
    jobs: [
      {
        run_id: '10',
        attempt: 1,
        github_job_id: '110',
        fleet_matched: true,
        recorded_grant: {
          basis: 'retained_job_granted_size_v1',
          source: 'pool',
          cpus: 1,
          memory_mb: 4096,
        },
        observation: {
          sha256: 'a'.repeat(64),
          issued_at: '2026-10-13T00:00:00Z',
          received_at: '2026-10-13T00:02:00Z',
          requested: {
            backend: 'docker',
            image_reference_sha256: 'b'.repeat(64),
            cpus: 2,
            memory_mb: null,
            resource_source: '',
            docker_mode: 'dind',
            cpu_typed: true,
            memory_typed: false,
          },
          image_digest: 'sha256:' + 'c'.repeat(64),
          digest_coverage: 'reported',
          agent_completed_at: null,
          agent_container_started_at: null,
        },
      },
    ],
  },
  candidate: {
    selected_jobs: 1,
    matched_jobs: 0,
    recorded_grants: 0,
    runner_observations: 0,
    rows_omitted: false,
    jobs: [
      {
        run_id: '11',
        attempt: 1,
        github_job_id: '9007199254740993',
        fleet_matched: false,
        recorded_grant: null,
        observation: null,
      },
    ],
  },
};

for (const [coverage, label] of [
  ['unsupported', 'Unsupported report'],
  ['not_reported', 'Not reported'],
]) {
  test(`runner evidence preserves ${coverage} agent digest coverage`, async ({ page }) => {
    const base = runnerEvidence.baseline.jobs[0]!;
    const evidence = {
      ...runnerEvidence,
      baseline: {
        ...runnerEvidence.baseline,
        jobs: [
          {
            ...base,
            observation: { ...base.observation, image_digest: null, digest_coverage: coverage },
          },
        ],
      },
    };
    await page.route('**/api/v1/comparisons/*?*', (route) =>
      route.fulfill({ json: { ...retained, runner_evidence: evidence } }),
    );
    await goto(
      page,
      '/comparisons?id=cmp_fixture&installation_id=ins_fixture&repo=acme%2Frepo',
      'Comparisons',
    );
    await page.getByText('Baseline recorded runner facts', { exact: true }).click();
    const facts = page.getByRole('region', { name: 'Baseline runner facts', exact: true });
    await expect(facts.getByText(`Image digest: ${label}`, { exact: true })).toBeVisible();
    await facts.getByText('Observation details for job 110', { exact: true }).click();
    await expect(facts.getByText('Unknown', { exact: true }).first()).toBeVisible();
    await expect(page.getByText('Inconclusive', { exact: true })).toBeVisible();
  });
}

test('runner evidence separates assignment, requests and claims with bounded coverage on desktop and mobile', async ({
  page,
}) => {
  await page.route('**/api/v1/comparisons/*?*', (route) =>
    route.fulfill({ json: { ...retained, runner_evidence: runnerEvidence } }),
  );
  await goto(
    page,
    '/comparisons?id=cmp_fixture&installation_id=ins_fixture&repo=acme%2Frepo',
    'Comparisons',
  );
  await expect(
    page.getByRole('heading', { name: 'Recorded runner evidence', exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText('Showing 1 of 101 jobs. Additional rows are omitted.', { exact: true }),
  ).toBeVisible();
  await page.getByText('Baseline recorded runner facts', { exact: true }).click();
  const facts = page.getByRole('region', { name: 'Baseline runner facts', exact: true });
  await expect(facts.getByText('1 CPUs / 4096 MB', { exact: true })).toBeVisible();
  await expect(facts.getByText('2 CPUs / Unknown MB', { exact: true })).toBeVisible();
  await facts.getByText('Observation details for job 110', { exact: true }).click();
  await expect(
    facts.getByText(runnerEvidence.baseline.jobs[0]!.observation.image_digest, { exact: true }),
  ).toBeVisible();
  const digestBox = await facts
    .getByText(runnerEvidence.baseline.jobs[0]!.observation.image_digest, { exact: true })
    .boundingBox();
  expect(digestBox?.width).toBeGreaterThan(150);
  await expect(facts.getByText('Unknown', { exact: true }).first()).toBeVisible();
  await facts.focus();
  await expect(facts).toBeFocused();
  await page.getByText('Candidate recorded runner facts', { exact: true }).click();
  const candidate = page.getByRole('region', { name: 'Candidate runner facts', exact: true });
  await expect(candidate.getByText('No retained observation', { exact: true })).toBeVisible();
  await expect(candidate.getByText('Not recorded', { exact: true })).toBeVisible();
  await expect(candidate.getByRole('rowheader')).toContainText('Job 9007199254740993');
  await expect(page.getByText('Inconclusive', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  await page.screenshot({ path: test.info().outputPath('runner-evidence.png'), fullPage: true });
});

// The real controller serves the application and role/session state. The
// comparison response is a fixture for GitHub evidence unavailable in CI.
test('operator capture is explicit, immutable replay is shareable and unknown stays distinct from zero', async ({
  page,
}) => {
  let captures = 0,
    reads = 0;
  await page.route('**/api/v1/comparisons', async (route) => {
    captures++;
    expect(route.request().method()).toBe('POST');
    const b = route.request().postDataJSON();
    expect(b.baseline).toEqual([{ run_id: 10, attempt: 1 }]);
    expect(b.candidate).toEqual([{ run_id: 11, attempt: 1 }]);
    await route.fulfill({ status: 201, json: retained });
  });
  await page.route('**/api/v1/comparisons/*?*', async (route) => {
    reads++;
    const q = new URL(route.request().url()).searchParams;
    expect(q.get('installation_id')).toBe('ins_fixture');
    expect(q.get('repo')).toBe('acme/repo');
    await route.fulfill({ json: retained });
  });
  await goto(page, '/jobs', 'Jobs');
  await page.getByRole('link', { name: 'Compare runs' }).click();
  await expect(page.getByRole('heading', { name: 'Comparisons', exact: true })).toBeVisible();
  expect(captures).toBe(0);
  await page.getByLabel('Installation ID').fill('ins_fixture');
  await page.getByLabel('Repository').fill('acme/repo');
  await page.getByLabel('Hypothesis').fill('lower latency');
  await page.getByLabel('Intended change').fill('larger runner');
  await page.getByLabel('Baseline run attempts').fill('10:1');
  await page.getByLabel('Candidate run attempts').fill('11:1');
  await page.getByRole('button', { name: 'Capture and retain evidence' }).click();
  await expect(page.getByRole('heading', { name: 'Retained report' })).toBeVisible();
  expect(captures).toBe(1);
  expect(reads).toBe(0);
  await expect(page.getByRole('heading', { name: 'Retained report' })).toBeFocused();
  await expect(
    page.getByText(
      'This controller did not provide runner evidence. Missing facts remain unknown.',
      { exact: true },
    ),
  ).toBeVisible();
  await page.screenshot({
    path: test.info().outputPath('comparison.png'),
  });
  await expect(page.getByText('Inconclusive', { exact: true })).toBeVisible();
  await expect(page.getByText('selection incomplete', { exact: true })).toBeVisible();
  await expect(page.getByText(retained.report.snapshot.hypothesis, { exact: true })).toBeVisible();
  expect(await page.evaluate(() => Object.hasOwn(window, 'injected'))).toBe(false);
  const baseline = page.getByRole('region', { name: 'Baseline measurements', exact: true });
  await expect(baseline.getByText('0 ms', { exact: true }).first()).toBeVisible();
  await expect(baseline.getByText('Unknown', { exact: true }).first()).toBeVisible();
  await page.getByText('Baseline identities and attempts', { exact: true }).click();
  await expect(
    page
      .getByRole('region', { name: 'Baseline attempts', exact: true })
      .getByText('failure', { exact: true }),
  ).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Retained report' })).toBeVisible();
  expect(reads).toBe(1);
  expect(captures).toBe(1);
});

test('missing history and rate limiting do not capture or automatically retry', async ({
  page,
}) => {
  let calls = 0;
  let limited = false;
  await page.route('**/api/v1/comparisons/*?*', async (route) => {
    calls++;
    await route.fulfill({
      status: limited ? 429 : 404,
      headers: limited ? { 'Retry-After': '60' } : {},
      json: {
        error: { code: limited ? 'rate_limited' : 'not_found', message: 'History unavailable' },
      },
    });
  });
  await goto(
    page,
    '/comparisons?id=cmp_fixture&installation_id=ins_fixture&repo=acme%2Frepo',
    'Comparisons',
  );
  await expect(page.getByRole('alert')).toContainText('No replacement was captured');
  expect(calls).toBe(1);
  limited = true;
  await page.getByRole('button', { name: 'Open report' }).click();
  await expect(page.getByRole('button', { name: 'Open report' })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Capture and retain evidence' })).toBeDisabled();
  expect(calls).toBe(2);
});

test('viewer can read but cannot capture', async ({ page }) => {
  await page.route('**/api/v1/auth/session', async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    const { response, body } = read;
    body.role = 'viewer';
    if (body.identity) body.identity.role = 'viewer';
    await route.fulfill({ response, json: body });
  });
  // Auth-disabled sessions bypass /auth/session, so use the real identity path
  // by reporting auth enabled while the fixture controller serves viewer data.
  await page.route('**/api/v1/meta', async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    const { response, body } = read;
    body.auth_disabled = false;
    body.bootstrap_required = false;
    await route.fulfill({ response, json: body });
  });
  await page.route('**/api/v1/comparisons/*?*', (route) => route.fulfill({ json: retained }));
  await goto(
    page,
    '/comparisons?id=cmp_fixture&installation_id=ins_fixture&repo=acme%2Frepo',
    'Comparisons',
  );
  await expect(page.getByRole('heading', { name: 'Retained report' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Capture and retain evidence' })).toHaveCount(0);
});

test('public visitors make no history request', async ({ page }) => {
  let reads = 0;
  await page.route('**/api/v1/meta', async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    const { response, body } = read;
    body.auth_disabled = false;
    body.bootstrap_required = false;
    body.public_view = true;
    await route.fulfill({ response, json: body });
  });
  await page.route('**/api/v1/auth/session', async (route) => {
    const read = await readThrough(route);
    if (!read) return;
    const { response, body } = read;
    body.role = 'viewer';
    body.kind = 'public';
    await route.fulfill({ response, json: body });
  });
  await page.route('**/api/v1/comparisons/*?*', async (route) => {
    reads++;
    await route.fulfill({ json: retained });
  });
  await goto(
    page,
    '/comparisons?id=cmp_fixture&installation_id=ins_fixture&repo=acme%2Frepo',
    'Comparisons',
  );
  await expect(page.getByText('Sign in to read comparisons', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Open report' })).toHaveCount(0);
  expect(reads).toBe(0);
});

const workflowEvidence = {
  version: 'workflow-evidence-v1',
  baseline: {
    selected_attempts: 101,
    root_sources: 100,
    rows_omitted: true,
    attempts: [
      {
        run_id: '9223372036854775807',
        attempt: 1,
        event: 'push',
        workflow_path_sha256: 'e'.repeat(64),
        root_source: {
          basis: 'push_head_sha_root_file',
          commit_sha: 'a'.repeat(40),
          blob_sha: 'b'.repeat(40),
          content_sha256: 'c'.repeat(64),
          bytes: 128,
        },
      },
    ],
  },
  candidate: {
    selected_attempts: 1,
    root_sources: 0,
    rows_omitted: false,
    attempts: [
      { run_id: '11', attempt: 1, event: null, workflow_path_sha256: null, root_source: null },
    ],
  },
};

test('workflow evidence shows exact identities, root-only coverage and unknowns without mobile overflow', async ({
  page,
}) => {
  let reads = 0;
  await page.route('**/api/v1/comparisons/*?*', (route) => {
    reads++;
    return route.fulfill({ json: { ...retained, workflow_evidence: workflowEvidence } });
  });
  await goto(
    page,
    '/comparisons?id=cmp_fixture&installation_id=ins_fixture&repo=acme%2Frepo',
    'Comparisons',
  );
  const facts = page.getByRole('region', { name: 'Recorded workflow evidence' });
  await expect(
    facts.getByText('Matching digests do not establish equivalent workloads.', { exact: false }),
  ).toBeVisible();
  await expect(
    facts.getByText('Showing 1 attempts. Counts include omitted rows.', { exact: true }),
  ).toBeVisible();
  await facts.getByText('Baseline run 9223372036854775807, attempt 1', { exact: true }).click();
  await expect(facts.getByText('Root file only', { exact: true })).toBeVisible();
  const digest = facts.getByText('c'.repeat(64), { exact: true });
  await expect(digest).toBeVisible();
  expect((await digest.boundingBox())!.width).toBeGreaterThan(100);
  await facts.getByText('Candidate run 11, attempt 1', { exact: true }).click();
  await expect(facts.getByText('Unknown', { exact: true }).last()).toBeVisible();
  await expect(page.getByText('Inconclusive', { exact: true })).toBeVisible();
  expect(reads).toBe(1);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

for (const version of ['absent', 'future']) {
  test(`workflow evidence keeps ${version} views explicitly unknown`, async ({ page }) => {
    const extra =
      version === 'absent' ? {} : { workflow_evidence: { ...workflowEvidence, version: 'future' } };
    await page.route('**/api/v1/comparisons/*?*', (route) =>
      route.fulfill({ json: { ...retained, ...extra } }),
    );
    await goto(
      page,
      '/comparisons?id=cmp_fixture&installation_id=ins_fixture&repo=acme%2Frepo',
      'Comparisons',
    );
    const facts = page.getByRole('region', { name: 'Recorded workflow evidence' });
    await expect(
      facts.getByText(
        version === 'absent'
          ? 'This controller did not provide workflow evidence. Missing facts remain unknown.'
          : 'This workflow evidence version is unsupported. Its facts remain unknown in this view.',
      ),
    ).toBeVisible();
    await expect(facts.getByText('c'.repeat(64), { exact: true })).toHaveCount(0);
  });
}

const dependencyEvidence = {
  version: 'root-dependencies-v1',
  basis: 'root_uses_declarations',
  source_sha256: 'c'.repeat(64),
  coverage: 'enumerated',
  counts: {
    declarations: 2,
    commit_pins: 1,
    digest_pins: 0,
    floating: 1,
    local: 0,
    expressions: 0,
    unknown: 0,
  },
  rows_omitted: true,
  references: [
    {
      job_index: 0,
      step_index: 0,
      line: 6,
      declaration: 'step_action',
      kind: 'remote',
      pin: 'commit',
      reference_sha256: 'f'.repeat(64),
      revision: 'a'.repeat(40),
    },
  ],
};
for (const state of ['observed', 'unsupported', 'zero', 'absent']) {
  test(`E2 dependency inspection preserves ${state} coverage and mobile layout`, async ({
    page,
  }) => {
    const source = workflowEvidence.baseline.attempts[0]!.root_source;
    const dep =
      state === 'absent'
        ? undefined
        : state === 'unsupported'
          ? {
              ...dependencyEvidence,
              coverage: 'unsupported',
              counts: null,
              rows_omitted: false,
              references: [],
            }
          : state === 'zero'
            ? {
                ...dependencyEvidence,
                counts: {
                  declarations: 0,
                  commit_pins: 0,
                  digest_pins: 0,
                  floating: 0,
                  local: 0,
                  expressions: 0,
                  unknown: 0,
                },
                rows_omitted: false,
                references: [],
              }
            : dependencyEvidence;
    const evidence = {
      ...workflowEvidence,
      baseline: {
        ...workflowEvidence.baseline,
        attempts: [
          {
            ...workflowEvidence.baseline.attempts[0],
            root_source: { ...source, dependencies: dep },
          },
        ],
      },
    };
    await page.route('**/api/v1/comparisons/*?*', (route) =>
      route.fulfill({ json: { ...retained, workflow_evidence: evidence } }),
    );
    await goto(
      page,
      '/comparisons?id=cmp_fixture&installation_id=ins_fixture&repo=acme%2Frepo',
      'Comparisons',
    );
    await page.getByText('Baseline run 9223372036854775807, attempt 1', { exact: true }).click();
    const facts = page.getByRole('region', {
      name: 'Baseline run 9223372036854775807 attempt 1 root dependency declarations',
    });
    await expect(
      facts.getByText('Literal pins do not prove existence', { exact: false }),
    ).toBeVisible();
    if (state === 'observed') {
      await expect(facts.getByText('2 root uses declarations', { exact: false })).toBeVisible();
      await expect(
        facts.getByText('Showing 1 declarations. Full counts include omitted rows.'),
      ).toBeVisible();
      await facts.getByText('Job index 0, step index 0, line 6', { exact: true }).click();
      await expect(facts.getByText('f'.repeat(64), { exact: true })).toBeVisible();
    } else if (state === 'zero')
      await expect(facts.getByText('0 root uses declarations', { exact: false })).toBeVisible();
    else
      await expect(
        facts.getByText(
          state === 'absent'
            ? 'No retained dependency inventory. Counts remain unknown.'
            : 'Unsupported inspection. Dependency counts remain unknown.',
        ),
      ).toBeVisible();
    await expect(page.getByText('Inconclusive', { exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
  });
}

test('a link from a published preview fills the comparison scope, kind and reference', async ({
  page,
}) => {
  await goto(
    page,
    '/comparisons?installation_id=ins_fixture&repo=acme%2Frepo&kind=workflow&revision=opv_fixture',
    'Comparisons',
  );
  await expect(page.getByLabel('Installation ID')).toHaveValue('ins_fixture');
  await expect(page.getByLabel('Repository')).toHaveValue('acme/repo');
  await expect(page.getByLabel('Comparison kind')).toHaveValue('workflow');
  await expect(page.getByLabel('Workflow revision-delta reference')).toHaveValue('opv_fixture');
});

test('a report opened from a published preview can be linked back as its verification', async ({
  page,
}) => {
  await page.route('**/api/v1/comparisons/*?*', (route) => route.fulfill({ json: retained }));
  let body: Record<string, unknown> | undefined;
  await page.route(
    '**/api/v1/optimisations/checkout/previews/opv_fixture/verification',
    async (route) => {
      body = route.request().postDataJSON();
      await route.fulfill({ json: { id: 'opv_fixture' } });
    },
  );
  await goto(
    page,
    '/comparisons?id=cmp_fixture&installation_id=ins_fixture&repo=acme%2Frepo&preview=opv_fixture',
    'Comparisons',
  );
  await expect(
    page.getByRole('heading', { name: 'Link this comparison to the preview' }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Link this comparison to opv_fixture' }).click();
  expect(body).toEqual({
    installation_id: 'ins_fixture',
    repository: 'acme/repo',
    comparison_id: 'cmp_fixture',
  });
  await expect(page.getByText(/Linked\./)).toBeVisible();
  await expect(page.getByRole('link', { name: 'Open the preview' })).toHaveAttribute(
    'href',
    '/optimisations/checkout?id=opv_fixture&installation_id=ins_fixture&repo=acme%2Frepo',
  );
});
