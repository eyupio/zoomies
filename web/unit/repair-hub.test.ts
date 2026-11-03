import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { Schemas } from '../src/lib/api/types.ts';
import { offerCases, setupSummary } from '../src/lib/repairs/hub.ts';
import { SHA, failedJob } from './repair-fixtures.ts';

type Offer = Schemas['EliOpportunity'];
type Policy = Schemas['EliRepairPolicy'];
type Settings = Schemas['EliRepairSettings'];

const offer = (over: Partial<Offer> = {}): Offer => ({
  id: 'opp_1',
  repo: 'acme/widgets',
  installation_id: 'ins_1',
  pull_number: 17,
  head_sha: SHA,
  run_id: 482,
  title: 'Retry transient 502s',
  state: 'open',
  created_at: '2026-10-18T09:15:00Z',
  updated_at: '2026-10-18T09:15:00Z',
  ...over,
});

// An offer names its run, and the jobs that failed in it are what the person
// came to decide about, so they are read for the offers all at once. A run id is
// only a key within its repository, and that read asks for several runs and
// several repositories together, so what comes back is sorted by whose it is.
test('an offer’s case holds the jobs of its own run in its own repository and no others', () => {
  const unit = failedJob({ id: 'job_unit', github_job_id: 1, job_name: 'unit' });
  const lint = failedJob({ id: 'job_lint', github_job_id: 2, job_name: 'lint' });
  const otherRun = failedJob({ id: 'job_other', github_run_id: 999, job_name: 'other run' });
  const otherRepo = failedJob({ id: 'job_api', repo: 'acme/api', job_name: 'other repo' });
  const cases = offerCases([offer()], [unit, lint, otherRun, otherRepo]);

  const view = cases['opp_1']!;
  assert.deepEqual(
    view.jobs.map((job) => job.name),
    ['unit', 'lint'],
  );
  assert.equal(view.branch, 'fix/retry-502');
  assert.equal(view.runUrl, 'https://github.com/acme/widgets/actions/runs/482');
  assert.equal(view.failedAt, '2026-10-18T09:14:00Z');
});

test('two offers on different runs each get their own jobs', () => {
  const first = failedJob({ job_name: 'first' });
  const second = failedJob({ github_run_id: 500, job_name: 'second', head_branch: 'chore/pin' });
  const cases = offerCases(
    [offer({ id: 'a' }), offer({ id: 'b', run_id: 500, pull_number: 20 })],
    [first, second],
  );
  assert.deepEqual(
    cases['a']!.jobs.map((job) => job.name),
    ['first'],
  );
  assert.deepEqual(
    cases['b']!.jobs.map((job) => job.name),
    ['second'],
  );
  assert.equal(cases['b']!.branch, 'chore/pin');
});

test('repository names are compared without regard to case, as GitHub does', () => {
  const cases = offerCases([offer({ repo: 'Acme/Widgets' })], [failedJob()]);
  assert.equal(cases['opp_1']!.jobs.length, 1);
});

// An offer older than Zoomies' records of the run, or one made without a run, is
// still an offer: it has a case with nothing in it, never a missing one.
test('an offer with no run or no recorded jobs still has a case', () => {
  const cases = offerCases([offer({ id: 'a', run_id: 0 }), offer({ id: 'b' })], []);
  for (const id of ['a', 'b']) {
    assert.deepEqual(cases[id]!.jobs, [], id);
    assert.equal(cases[id]!.repo, 'acme/widgets');
    assert.equal(cases[id]!.pullNumber, 17);
  }
});

const identity = (confirmed: boolean) => ({
  user_id: 'usr_1',
  github_user_id: 42,
  github_login: 'dana',
  confirmed,
});
const policy = (repo: string, over: Partial<Policy> = {}): Policy => ({
  repo,
  installation_id: 'ins_1',
  provider_id: 'prv_1',
  enabled: true,
  automatic: false,
  ask_first: true,
  allow_workflows: false,
  ...over,
});
const input = (
  settings: Settings | null,
  over: Partial<Parameters<typeof setupSummary>[0]> = {},
) => ({
  settings,
  userId: 'usr_1',
  admin: false,
  authDisabled: false,
  ...over,
});

// The bar says whether the person can repair, in the one word an operator looks
// for, and what stands in the way in words that name the next thing to do.
test('a linked account that has allowed repairs is ready', () => {
  const summary = setupSummary(input({ identities: [identity(true)], policies: [] }));
  assert.deepEqual(summary, {
    state: 'ready',
    headline: 'Ready to repair',
    account: 'GitHub @dana linked',
    askFirst: null,
    automatic: null,
  });
});

test('a linked account that has not allowed repairs yet is not ready, and says what is missing', () => {
  const summary = setupSummary(input({ identities: [identity(false)], policies: [] }));
  assert.equal(summary.state, 'needed');
  assert.equal(summary.headline, 'Setup needed');
  assert.equal(summary.account, 'GitHub @dana linked, repairs not allowed yet');
});

test('no linked account is not ready', () => {
  const summary = setupSummary(input({ identities: [], policies: [] }));
  assert.equal(summary.state, 'needed');
  assert.equal(summary.account, 'No GitHub account linked');
});

// Somebody else's link is somebody else's: the bar is about the person looking at it.
test('another person’s link does not make this person ready', () => {
  const summary = setupSummary(
    input({ identities: [{ ...identity(true), user_id: 'usr_2' }], policies: [] }),
  );
  assert.equal(summary.state, 'needed');
});

test('only an administrator is told how many repositories ask first', () => {
  const settings: Settings = {
    identities: [identity(true)],
    policies: [
      policy('acme/a'),
      policy('acme/b'),
      policy('acme/c', { enabled: false }),
      policy('acme/d', { ask_first: false, automatic: true }),
    ],
  };
  assert.equal(setupSummary(input(settings, { admin: true })).askFirst, 2);
  assert.equal(setupSummary(input(settings, { admin: false })).askFirst, null);
});

// Automation is counted the same way ask-first is, and for the same people: the
// policies are an administrator's to read, and a switched-off one is not on.
test('only an administrator is told how many repositories repair themselves', () => {
  const settings: Settings = {
    identities: [identity(true)],
    policies: [
      policy('acme/a', { ask_first: false, automatic: true }),
      policy('acme/b', { ask_first: false, automatic: true }),
      policy('acme/c', { ask_first: false, automatic: true, enabled: false }),
      policy('acme/d'),
    ],
  };
  assert.equal(setupSummary(input(settings, { admin: true })).automatic, 2);
  assert.equal(setupSummary(input(settings, { admin: false })).automatic, null);
  assert.equal(setupSummary(input(null, { admin: true })).automatic, null);
});

test('before the settings arrive the bar is loading, and with sign-in off it says there is nobody to repair as', () => {
  assert.equal(setupSummary(input(null)).state, 'loading');
  const off = setupSummary(input({ identities: [], policies: [] }, { authDisabled: true }));
  assert.equal(off.state, 'off');
  assert.equal(off.headline, 'Sign-in is off');
  // Sign-in being off outranks whatever the settings say, and outranks not having them.
  assert.equal(setupSummary(input(null, { authDisabled: true })).state, 'off');
});
