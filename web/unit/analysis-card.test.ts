import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';
import {
  analysisContext,
  analysisWindow,
  confidenceWords,
  findingDescription,
  jobWords,
  queueWaitScope,
  refusalOf,
  stepLine,
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

// The card asks for an explicit period, never the route's default, so the
// answer on screen is the answer anyone asking for the same period gets.
test('a page with no window of its own asks about the day before now', () => {
  assert.deepEqual(analysisWindow(undefined, undefined, NOW), {
    since: '2026-10-12T12:00:00.000Z',
    until: '2026-10-13T12:00:00.000Z',
  });
  assert.deepEqual(analysisWindow(undefined, '2026-10-13T06:00:00.000Z', NOW), {
    since: '2026-10-12T06:00:00.000Z',
    until: '2026-10-13T06:00:00.000Z',
  });
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
    'v1.3.2 from 2026-10-13 04:11 to 2026-10-13 05:43; v1.3.3 from 2026-10-13 05:25 to 2026-10-13 07:28. Cancelled and skipped jobs are left out.',
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
