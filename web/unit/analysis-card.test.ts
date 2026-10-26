import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';
import type { ErrorCode } from '../src/lib/api/types.ts';
import {
  analysisContext,
  analysisWindow,
  confidenceWords,
  findingDescription,
  jobWords,
  longestWaits,
  queueWaitScope,
  recurringFailuresScope,
  refusalOf,
  startupFailuresScope,
  stepLine,
  thresholdValue,
  UNCLAIMED_POOL,
  whyNoWindow,
} from '../src/lib/insights/analysis.ts';

// The confidence is words on a neutral badge, so the words carry all of it:
// the level always, and what was thin whenever there was something.
test('confidence reads as words, with its reason when there is one', () => {
  assert.equal(confidenceWords('high'), 'High confidence');
  assert.equal(confidenceWords('low', 'only 3 jobs waited'), 'Low confidence: only 3 jobs waited');
  assert.equal(confidenceWords('medium', null), 'Medium confidence');
  assert.equal(confidenceWords('medium', '  '), 'Medium confidence');
});

// A step names a setting, a catalog code, both or neither. The code's page is
// the controller's to know (F8: the analysis fills `link` after it returns),
// so the card links exactly what it was given and invents no address.
test('a step links its code to the catalog page the controller gave it', () => {
  assert.deepEqual(
    stepLine({
      text: 'Raise the pool ceiling.',
      setting: 'pool.max_runners',
      code: 'pool.at_max_runners',
      link: 'https://zoomies.sh/problem-codes/#pool.at_max_runners',
    }),
    {
      text: 'Raise the pool ceiling.',
      setting: 'pool.max_runners',
      href: 'https://zoomies.sh/problem-codes/#pool.at_max_runners',
    },
  );
  assert.deepEqual(stepLine({ text: 'Check the registry.', code: 'runners.image_pull' }), {
    text: 'Check the registry.',
    setting: null,
    href: null,
  });
});

// The withheld-names step carries neither a setting nor a code, and it is the
// one place the card says the names were left out, so it must still render.
test('a step with neither setting nor code is its text alone', () => {
  assert.deepEqual(stepLine({ text: 'The job names were left out.' }), {
    text: 'The job names were left out.',
    setting: null,
    href: null,
  });
});

// A link is followed in a new tab with the operator's session in the page;
// anything but a web address is not one.
test('a step link that is not a web address is not followed', () => {
  assert.equal(stepLine({ text: 'x', code: 'c', link: 'javascript:alert(1)' }).href, null);
  assert.equal(stepLine({ text: 'x', code: 'c', link: '/problems' }).href, null);
});

const NOW = Date.parse('2026-10-13T12:00:00Z');

// The card names its end, so the answer on screen is the answer anyone asking
// with the same address gets. It leaves since out, so the route takes its own
// default before that end: a day, or limits.job_stats_window where that is
// shorter. Asking for a day by name is refused on a fleet whose limit is less.
test('a page with no window of its own leaves since to the route', () => {
  assert.deepEqual(analysisWindow(undefined, undefined, NOW), {
    until: '2026-10-13T12:00:00.000Z',
  });
  assert.deepEqual(analysisWindow(undefined, '2026-10-13T06:00:00.000Z', NOW), {
    until: '2026-10-13T06:00:00.000Z',
  });
  assert.equal('since' in analysisWindow(undefined, undefined, NOW)!, false);
});

// A range that runs to the end of today has hours that have not happened, and
// a job still waiting would have its wait counted into them.
test('a window ending after now ends now', () => {
  assert.deepEqual(analysisWindow('2026-10-13T00:00:00.000Z', '2026-10-13T23:59:59.999Z', NOW), {
    since: '2026-10-13T00:00:00.000Z',
    until: '2026-10-13T12:00:00.000Z',
  });
});

// A range typed backwards is the page's own warning to give; asking the route
// would only turn it into a second, worse message.
test('a window that does not end after it starts asks nothing', () => {
  assert.equal(analysisWindow('2026-10-13T10:00:00.000Z', '2026-10-13T09:00:00.000Z', NOW), null);
  assert.equal(analysisWindow('2026-10-14T00:00:00.000Z', undefined, NOW), null);
  assert.equal(analysisWindow('not a time', undefined, NOW), null);
  assert.equal(analysisWindow(undefined, 'not a time', NOW), null);
});

// A window nothing is asked of leaves the card with no finding, no failure and
// nothing loading, which is a titled frame with nothing under it. The date
// picker points out a backwards range and nothing points out one that has not
// started, so the card says why it asked nothing, in each case.
test('a window nothing is asked of says why, whichever way it is empty', () => {
  const future =
    'This period has not started, and an analysis reads only what has already happened; choose a start in the past.';
  const backwards = 'The end is before the start, so there is no period to analyse.';
  const unreadable =
    'The range has a date this page cannot read, so there is no period to analyse.';
  const cases: [string | undefined, string | undefined, string][] = [
    ['2026-10-14T00:00:00.000Z', '2026-10-15T00:00:00.000Z', future],
    // An end left out is now, so a start after now is the same case.
    ['2026-10-14T00:00:00.000Z', undefined, future],
    ['2026-10-13T10:00:00.000Z', '2026-10-13T09:00:00.000Z', backwards],
    // Both in the future, and the wrong way round: backwards is what to fix first.
    ['2026-10-16T00:00:00.000Z', '2026-10-15T00:00:00.000Z', backwards],
    ['not a time', undefined, unreadable],
    [undefined, 'not a time', unreadable],
  ];
  for (const [since, until, said] of cases) {
    // Said only for a window that asks nothing: the others have an answer.
    assert.equal(analysisWindow(since, until, NOW), null, `${since} to ${until} asks nothing`);
    assert.equal(whyNoWindow(since, until), said, `${since} to ${until}`);
  }
  // Usage reads an address it cannot parse as bounds that are empty, and never
  // gets as far as asking analysisWindow, which would take them for no window.
  assert.equal(whyNoWindow('', ''), unreadable);
  assert.equal(whyNoWindow('2026-10-13T00:00:00.000Z', ''), unreadable);
});

// Eli is handed what the card shows, named one by one, and never the evidence
// object: names a workflow wrote stay on the page, and Eli has the tool.
test('Ask Eli is handed the finding the card shows', () => {
  const context = analysisContext('Why jobs waited', {
    analysis: 'queue_wait',
    since: '2026-10-12T12:00:00Z',
    until: '2026-10-13T12:00:00Z',
    summary: 'Jobs waited 50m in all.',
    confidence: 'low',
    confidence_reason: 'only 3 jobs waited',
    next_steps: [{ text: 'Raise the ceiling.', setting: 'pool.max_runners' }, { text: 'Read on.' }],
    codes: ['pool.at_max_runners'],
  });
  assert.equal(context.kind, 'analysis');
  assert.equal(context.title, 'Why jobs waited');
  assert.equal(context.facts.Analysis, 'queue_wait');
  assert.equal(context.facts.Summary, 'Jobs waited 50m in all.');
  assert.equal(context.facts.Confidence, 'Low confidence: only 3 jobs waited');
  assert.equal(context.facts.From, '2026-10-12T12:00:00Z');
  assert.equal(context.facts.To, '2026-10-13T12:00:00Z');
  assert.equal(context.facts['Next steps'], 'Raise the ceiling. (pool.max_runners) Read on.');
  assert.equal(context.facts.Codes, 'pool.at_max_runners');
  assert.ok(!('Evidence' in context.facts));
  assert.ok(context.question && context.question.length > 0);
});

// The prompt tells Eli to check what the page showed against the fleet with its
// tools, and the tools read the last day of the whole fleet unless told
// otherwise. A card narrowed to a pool or a host, or reading a period of its
// own, has to hand over what it was narrowed to and say to pass it, or Eli's
// check answers another question than the card's: the fleet's "host
// demo-builder-2 failed to start 6 of 10" against a pool's "no start failed".
test('Ask Eli is told which pool or host the card was narrowed to, and to pass it', () => {
  const finding = {
    analysis: 'startup_failures',
    since: '2026-10-12T12:00:00Z',
    until: '2026-10-13T12:00:00Z',
    summary: 'No start failed in this period: 6 runners started.',
    confidence: 'medium',
    next_steps: [],
    codes: [],
  };
  const pool = analysisContext('What keeps failing to start', finding, {
    pool: { id: 'pool_a', name: 'linux' },
  });
  assert.equal(pool.facts.Pool, 'linux (pool_a)');
  assert.equal(pool.facts.Host, undefined);
  assert.match(pool.question ?? '', /for this pool\b/);
  assert.match(pool.question ?? '', /pool_id pool_a\b/);
  assert.match(pool.question ?? '', /since 2026-10-12T12:00:00Z, until 2026-10-13T12:00:00Z/);

  const both = analysisContext('What keeps failing to start', finding, {
    pool: { id: 'pool_a' },
    host: { id: 'host_x', name: 'demo-builder-2' },
  });
  // A name the page does not know is the id alone, not "pool_a (pool_a)".
  assert.equal(both.facts.Pool, 'pool_a');
  assert.equal(both.facts.Host, 'demo-builder-2 (host_x)');
  assert.match(both.question ?? '', /pool_id pool_a and host_id host_x/);

  // Nothing narrowed: the fleet, and the period alone.
  const fleet = analysisContext('What keeps failing to start', finding);
  assert.equal(fleet.facts.Pool, undefined);
  assert.equal(fleet.facts.Host, undefined);
  assert.match(fleet.question ?? '', /for this fleet\b/);
  assert.doesNotMatch(fleet.question ?? '', /pool_id|host_id/);
  assert.match(fleet.question ?? '', /since 2026-10-12T12:00:00Z/);
});

// With the names withheld every one is empty; joining them would print a row
// of separators, which reads as names that are blank rather than left out.
test("a job's names skip the empty ones", () => {
  assert.equal(
    jobWords({ repo: 'acme/widgets', workflow: 'CI', job_name: 'build' }),
    'acme/widgets · CI · build',
  );
  assert.equal(jobWords({ repo: '', workflow: '', job_name: '' }), '');
  assert.equal(jobWords({ repo: 'acme/site', workflow: '', job_name: 'test' }), 'acme/site · test');
  // An unexpanded matrix name reads as the jobs page reads it.
  assert.equal(
    jobWords({ repo: '', workflow: '', job_name: 'build ${{ matrix.os }}' }),
    'build value unavailable',
  );
});

const card = readFileSync(
  join(import.meta.dirname, '..', 'src', 'lib', 'insights', 'AnalysisCard.svelte'),
  'utf8',
);

// web/AGENTS.md keeps the status colours for runner and job states. A
// confidence painted in one would read as a state an operator has learnt.
test('the card wears no status colour', () => {
  const status = card.match(/--z-(idle|busy|pending|draining|danger|neutral)\b[a-z-]*/g) ?? [];
  assert.deepEqual(status, []);
});

// Summaries quote job and repository names a workflow wrote; parsed as markup
// they would run in the operator's session.
test('the card never renders text as markup', () => {
  assert.equal(card.includes('{@html'), false);
});

const plainTime = (iso: string) => iso.slice(0, 16).replace('T', ' ');

// The route reads one pool or none, and the Queued view's grid can be narrowed
// by much more. A card that showed the fleet's answer under a list narrowed to
// one repository would read as that repository's, so it says what it covers
// and which of the list's filters it does not apply.
test('the queue-wait card names its scope and the filters it cannot apply', () => {
  const none = { pool_id: [], repo: [], workflow: [], host_id: [], label: [], q: '' };
  const name = (id: string) => (id === 'pool_a' ? 'linux' : id);
  assert.deepEqual(queueWaitScope(none, name), { scope: 'Every pool', unapplied: null });
  assert.deepEqual(queueWaitScope({ ...none, pool_id: ['pool_a'] }, name), {
    scope: 'Pool linux',
    unapplied: null,
  });
  assert.deepEqual(queueWaitScope({ ...none, repo: ['acme/api'] }, name), {
    scope: 'Every pool',
    unapplied:
      "The list's repository filter is not applied here: this analysis reads every pool, or one pool.",
  });
  assert.deepEqual(
    queueWaitScope(
      {
        pool_id: ['pool_a', 'pool_b'],
        repo: [],
        workflow: ['CI'],
        host_id: ['host_1'],
        label: ['gpu'],
        q: 'build',
      },
      name,
    ),
    {
      scope: 'Every pool',
      unapplied:
        "The list's pool, workflow, host, label and search filters are not applied here: this analysis reads every pool, or one pool.",
    },
  );
});

// Two more filters narrow the queued list to a part of it: a provisioning
// status somebody chose (the default one is every waiting job, which the
// card's period already says it is not) and unmatched. The card covers every
// pool under either, so it names them.
test('a chosen provisioning filter is named, the default one is not', () => {
  const none = { pool_id: [], repo: [], workflow: [], host_id: [], label: [], q: '' };
  const id = (x: string) => x;
  assert.deepEqual(queueWaitScope({ ...none, provisioning: ['paused'] }, id), {
    scope: 'Every pool',
    unapplied:
      "The list's provisioning filter is not applied here: this analysis reads every pool, or one pool.",
  });
  // What the page passes when the address chose none: nothing.
  assert.deepEqual(queueWaitScope({ ...none, provisioning: [] }, id), {
    scope: 'Every pool',
    unapplied: null,
  });
});

test('the unmatched filter is named', () => {
  const none = { pool_id: [], repo: [], workflow: [], host_id: [], label: [], q: '' };
  assert.deepEqual(
    queueWaitScope({ ...none, repo: ['acme/api'], unmatched: true }, (x) => x),
    {
      scope: 'Every pool',
      unapplied:
        "The list's repository and unmatched filters are not applied here: this analysis reads every pool, or one pool.",
    },
  );
  assert.equal(queueWaitScope({ ...none, unmatched: false }, (x) => x).unapplied, null);
});

// GitHub reports every job in an installed repository, so the list leaves
// another vendor's jobs out until "Include other runners" is on, and the
// analysis leaves them out always: they are that vendor's to run, never time
// a pool should have claimed them in. With the switch on the list holds jobs
// the card does not, which is the one thing a reader comparing the two would
// not guess, so the card says it.
test('the Include other runners switch is named when it is on, and only then', () => {
  const none = { pool_id: [], repo: [], workflow: [], host_id: [], label: [], q: '' };
  const said =
    "The list's Include other runners switch is not applied here: this analysis leaves out the jobs another vendor's runners ran.";
  assert.deepEqual(
    queueWaitScope({ ...none, all: true }, (x) => x),
    {
      scope: 'Every pool',
      unapplied: said,
    },
  );
  assert.equal(queueWaitScope({ ...none, all: false }, (x) => x).unapplied, null);
  // Beside a filter it names both, each in its own sentence.
  assert.equal(
    queueWaitScope({ ...none, repo: ['acme/api'], all: true }, (x) => x).unapplied,
    `The list's repository filter is not applied here: this analysis reads every pool, or one pool. ${said}`,
  );
});

// The failed runners' card reads the period's starts, narrowed by the pool and
// host the page chose, and says so; the list beside it also takes a search,
// which the analysis has no use for. A card that went on saying the fleet's
// answer under a list searched down to one runner would read as that runner's.
test('the failed runners card names its scope and the search it cannot apply', () => {
  const none = { pool_id: '', host_id: '', q: '' };
  const names = {
    pool: (id: string) => (id === 'pool_a' ? 'linux' : id),
    host: (id: string) => (id === 'host_a' ? 'builder-1' : id),
  };
  assert.deepEqual(startupFailuresScope(none, names), {
    scope: 'Every pool and host',
    unapplied: null,
  });
  assert.equal(startupFailuresScope({ ...none, pool_id: 'pool_a' }, names).scope, 'Pool linux');
  assert.equal(startupFailuresScope({ ...none, host_id: 'host_a' }, names).scope, 'Host builder-1');
  assert.equal(
    startupFailuresScope({ ...none, pool_id: 'pool_a', host_id: 'host_a' }, names).scope,
    'Pool linux, host builder-1',
  );
  // A pool the page has no name for is its id, as the Queued card has it.
  assert.equal(
    startupFailuresScope({ ...none, pool_id: 'pool_gone' }, names).scope,
    'Pool pool_gone',
  );
  assert.deepEqual(startupFailuresScope({ ...none, q: 'runner-7' }, names), {
    scope: 'Every pool and host',
    unapplied:
      "The list's search filter is not applied here: this analysis reads every start in the period, narrowed only by pool and host.",
  });
  // Spaces are not a search, as the list reads them.
  assert.equal(startupFailuresScope({ ...none, q: '   ' }, names).unapplied, null);
});

// A period card names its scope and its period above the summary; a
// comparison names each side's own dates instead of the period asked for,
// and that cancelled and skipped jobs were left out, both visible without
// opening the evidence.
test('a finding describes its own period, and a comparison each side', () => {
  const base = {
    since: '2026-10-12T12:00:00Z',
    until: '2026-10-13T12:00:00Z',
  };
  assert.equal(
    findingDescription({ ...base, analysis: 'queue_wait' }, plainTime, 'Every pool'),
    'Every pool, 2026-10-12 12:00 to 2026-10-13 12:00',
  );
  assert.equal(
    findingDescription({ ...base, analysis: 'host_pressure' }, plainTime),
    '2026-10-12 12:00 to 2026-10-13 12:00',
  );
  assert.equal(
    findingDescription(
      {
        ...base,
        analysis: 'compare',
        evidence: {
          a: { label: 'v1.3.2', since: '2026-10-13T04:11:00Z', until: '2026-10-13T05:43:00Z' },
          b: { label: 'v1.3.3', since: '2026-10-13T05:25:00Z', until: '2026-10-13T07:28:00Z' },
          excluded: ['cancelled', 'skipped'],
        },
      },
      plainTime,
    ),
    'v1.3.2 from 2026-10-13 04:11 to 2026-10-13 05:43; v1.3.3 from 2026-10-13 05:25 to 2026-10-13 07:28. Cancelled and skipped jobs are left out of every figure read from the job rows.',
  );
});

// A 4xx is the route declining a question it understood (a period over the
// limit, too few releases), which on Usage's default day is the ordinary
// answer; announcing it as an alert would interrupt a screen reader every
// time the page opens. A request that failed is an alert.
test('a refusal is told apart from a failed request', () => {
  assert.deepEqual(
    refusalOf({ status: 409, message: 'fewer than two releases completed a job in this period' }),
    { message: 'Fewer than two releases completed a job in this period.', refused: true },
  );
  assert.deepEqual(refusalOf({ status: 0, message: 'Could not reach the Zoomies API.' }), {
    message: 'Could not reach the Zoomies API.',
    refused: false,
  });
  assert.deepEqual(refusalOf(new Error('')), {
    message: 'The analysis could not be read.',
    refused: false,
  });
});

// The route's sentence for too few releases tells a caller to name the
// releases with a and b, or to compare by period. The Usage card asks by
// version with neither and never compares periods, so two of its three
// remedies are ones the page has no control for, and on Usage's default day
// this is the ordinary answer. The page says what it can do.
test('too few releases is said as the Usage card can act on it', () => {
  // Typed, so the page's wording follows the code if the API ever renames it.
  const code: ErrorCode = 'analysis.too_few_releases';
  const said = refusalOf({
    status: 409,
    code,
    message:
      'fewer than two releases completed a job in this period, so there is no pair to compare; name the releases with a and b, widen the period, or compare by period',
  });
  assert.deepEqual(said, {
    message:
      'Fewer than two releases completed a job in this range, so there is no pair to compare; widen the range.',
    refused: true,
  });
  assert.doesNotMatch(said.message, /\ba and b\b|by period/);
  // Any other refusal keeps the route's own sentence, which says what to change.
  assert.equal(
    refusalOf({ status: 409, code: 'conflict', message: 'something else to change' }).message,
    'Something else to change.',
  );
});

// The server's 500 names no cause on purpose and carries its request ID in
// detail; the card says which request to quote, once.
test('a server failure names the request to quote', () => {
  const generic =
    'Quote the request ID to whoever operates this instance; the cause is in its log.';
  assert.deepEqual(
    refusalOf({
      status: 500,
      message: `something went wrong while analysing queue wait. ${generic}`,
      detail: 'request req_01abc',
    }),
    {
      message:
        'Something went wrong while analysing queue wait. Quote req_01abc to whoever operates this instance; the cause is in its log.',
      refused: false,
    },
  );
  assert.deepEqual(refusalOf({ status: 502, message: 'bad gateway' }), {
    message: `Bad gateway. ${generic}`,
    refused: false,
  });
});

// The controller floors a threshold's line to a tenth of a percent and says it
// so in the summary. Rounded to a whole number, 87.5 would read as 88, a line
// some of the events counted above it sit below.
test('a threshold reads as the summary says it', () => {
  assert.equal(thresholdValue('cpu_percent', 87.5), '87.5%');
  assert.equal(thresholdValue('cpu_percent', 88), '88%');
  assert.equal(thresholdValue('memory_used', 0.924), '92.4%');
  assert.equal(thresholdValue('memory_used', 0.9), '90%');
  assert.equal(thresholdValue('load_per_cpu', 1.25), '1.25');
});

// The jobs no pool claimed are counted under a row with no id or name. Listed
// with the rest, they would wait "in" nothing; they read as the MCP tools say
// them instead.
test('the longest waits name the unclaimed jobs as unclaimed', () => {
  const job = (job_id: string, wait_seconds: number) => ({
    job_id,
    wait_seconds,
    cause: 'unmatched',
  });
  const got = longestWaits([
    { pool_id: '', pool_name: '', longest: [job('job_a', 600)] },
    { pool_id: 'pool_1', pool_name: 'linux', longest: [job('job_b', 900)] },
    { pool_id: 'pool_2', pool_name: '', longest: [job('job_c', 300)] },
  ]);
  assert.deepEqual(
    got.map((j) => [j.job_id, j.pool]),
    [
      ['job_b', 'linux'],
      ['job_a', UNCLAIMED_POOL],
      ['job_c', 'pool_2'],
    ],
  );
  assert.equal(UNCLAIMED_POOL, 'unclaimed (no pool claimed these jobs)');
});

// The Failed view's list can be narrowed by much more than the analysis reads,
// and an answer about every job under a list of one repository would read as
// that repository's.
test('the Failed card says it covers every job and names the filters it leaves out', () => {
  const none = { pool_id: [], repo: [], workflow: [], host_id: [], label: [], q: '' };
  assert.deepEqual(recurringFailuresScope(none), {
    scope: 'Every repository and workflow',
    unapplied: null,
  });
  assert.equal(
    recurringFailuresScope({ ...none, repo: ['acme/app'] }).unapplied,
    "The list's repository filter is not applied here: this analysis reads every job this fleet ran in the period.",
  );
  assert.equal(
    recurringFailuresScope({ ...none, repo: ['a/b'], q: 'build' }).unapplied,
    "The list's repository and search filters are not applied here: this analysis reads every job this fleet ran in the period.",
  );
  assert.equal(recurringFailuresScope({ ...none, q: '  ' }).unapplied, null);
});
