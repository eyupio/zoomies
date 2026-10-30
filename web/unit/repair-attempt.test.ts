import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  investigationText,
  providerWords,
  requestedBy,
  responseText,
  timeline,
  tookMs,
} from '../src/lib/repairs/attempt.ts';
import { repair } from './repair-fixtures.ts';

test('who asked, and which kind of provider paid', () => {
  assert.equal(
    requestedBy(repair({ trigger: 'mention', github_login: 'dana' })),
    'Requested by @dana',
  );
  assert.equal(
    requestedBy(repair({ trigger: 'mention', github_login: '' })),
    'Requested by someone',
  );
  assert.equal(
    requestedBy(repair({ trigger: 'automatic', github_login: '' })),
    'Started automatically',
  );
  assert.equal(providerWords(repair({ trigger: 'mention' })), 'Personal provider');
  assert.equal(providerWords(repair({ trigger: 'automatic' })), 'Installation provider');
});

// How long it took is only a number once it has finished: a repair that is still
// going has taken nothing yet, and a clock that disagrees is not a negative time.
test('a finished repair says how long it took and a running one does not', () => {
  const started = '2026-10-18T09:20:00Z';
  assert.equal(
    tookMs(repair({ state: 'failed', created_at: started, updated_at: '2026-10-18T09:21:08Z' })),
    68_000,
  );
  assert.equal(
    tookMs(repair({ state: 'working', created_at: started, updated_at: '2026-10-18T09:21:08Z' })),
    undefined,
  );
  assert.equal(
    tookMs(repair({ state: 'failed', created_at: started, updated_at: '2026-10-18T09:19:00Z' })),
    0,
  );
});

// The timeline is made of what the row knows, and says nothing about a moment it
// does not know: a pushed commit has no time of its own. It names how the repair
// ended and leaves the sentence to the outcome card, which is where it is read;
// repeated here it would be the same words twice on one screen.
test('the timeline holds the moments the repair recorded and no others', () => {
  const failed = timeline(
    repair({
      state: 'failed',
      message: 'Eli could not produce a supported fix.',
      created_at: '2026-10-18T09:20:00Z',
      updated_at: '2026-10-18T09:21:08Z',
    }),
  );
  assert.deepEqual(failed, [
    { at: '2026-10-18T09:20:00Z', text: 'Queued' },
    { at: '2026-10-18T09:21:08Z', text: 'Could not repair' },
  ]);

  const pushed = timeline(
    repair({
      state: 'checks_failed',
      commit_sha: 'abc1234def5678',
      message: 'Checks on Eli’s commit failed.',
      created_at: '2026-10-18T09:20:00Z',
      updated_at: '2026-10-18T09:30:00Z',
    }),
  );
  assert.deepEqual(
    pushed.map((entry) => entry.text),
    ['Queued', 'Pushed commit abc1234', 'Checks failed'],
  );
  assert.equal(pushed[1]?.at, undefined);

  // A repair still going has only been queued.
  assert.deepEqual(timeline(repair({ state: 'working', created_at: '2026-10-18T09:20:00Z' })), [
    { at: '2026-10-18T09:20:00Z', text: 'Queued' },
  ]);
});

// What a person pastes into a bug report. It holds exact figures (not the
// rounded ones the page shows) and ids. Old calls explain that responses were
// not kept, rather than pretending the provider answered with no text.
test('the investigation details are exact, plain text and leave out what Eli read', () => {
  const text = investigationText(
    repair({
      id: 'eli_9',
      state: 'failed',
      message: 'Eli could not produce a supported fix.',
      commit_sha: '',
      details: {
        model: 'my-model',
        stage: 'prepare_fix',
        failure_code: 'unsupported_fix',
        source_bytes: 38912,
        case: { pull_title: 'Retry transient 502s', jobs: [{ name: 'Go (api-2)' }] },
        steps: [
          {
            input_tokens: 21400,
            output_tokens: 1800,
            usage_reported: true,
            request_bytes: 80312,
            elapsed_ms: 41000,
          },
          {
            input_tokens: 0,
            output_tokens: 0,
            usage_reported: false,
            request_bytes: 91234,
            elapsed_ms: 9000,
          },
        ],
      },
    }),
  );
  assert.equal(
    text,
    [
      'Eli repair eli_9',
      'Repository: acme/widgets',
      'Pull request: #17',
      'Run: 482',
      'Failing head: a41c9e2f0b7d5c3e8a1f6d4b2c9e7a5f3d1b8c6e',
      'Commit pushed: none',
      'State: failed',
      'Message: Eli could not produce a supported fix.',
      'Stage reached: prepare_fix',
      'Failure code: unsupported_fix',
      'Model: my-model',
      'Source supplied: 38912 bytes',
      'Model calls: 2',
      '  1. 21400 input tokens, 1800 output tokens, request 80312 bytes, 41000 ms',
      '     Response:',
      'Response not retained for this older call.',
      '  2. usage not reported, request 91234 bytes, 9000 ms',
      '     Response:',
      'Response not retained for this older call.',
      'Started: 2026-10-18T09:20:00Z',
      'Updated: 2026-10-18T09:24:00Z',
    ].join('\n'),
  );
  assert.ok(!text.includes('Retry transient'));
  assert.ok(!text.includes('Go (api-2)'));
});

test('copied investigations retain response text, truncation and rejection reasons', () => {
  const step = {
    input_tokens: 1,
    output_tokens: 2,
    usage_reported: true,
    request_bytes: 100,
    elapsed_ms: 20,
    response_recorded: true,
    response: '{"read":["a.go"],"edits":[...]}\n[redacted credential]',
    response_truncated: true,
    error: 'The provider cut the response',
    validation_error: 'Do not combine source requests with changes',
  };
  const text = investigationText(repair({ details: { steps: [step] } }));
  assert.ok(text.includes(step.response));
  assert.ok(text.includes('Response (truncated):'));
  assert.ok(text.includes(`Call error: ${step.error}`));
  assert.ok(text.includes(`Validation error: ${step.validation_error}`));
  assert.equal(
    responseText({ ...step, response: '' }),
    'The provider returned no response text or tool calls.',
  );
});

test('the investigation details say nothing about what was not recorded', () => {
  const text = investigationText(
    repair({ pull_number: 0, run_id: 0, details: undefined, message: '' }),
  );
  assert.ok(!text.includes('Pull request'));
  assert.ok(!text.includes('Run:'));
  assert.ok(!text.includes('Message'));
  assert.ok(!text.includes('Model'));
  assert.ok(!text.includes('Source supplied'));
  assert.ok(!text.includes('Model calls'));
  assert.ok(text.startsWith('Eli repair eli_1\nRepository: acme/widgets\n'));
});
