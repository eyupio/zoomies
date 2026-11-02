import { test } from 'node:test';
import assert from 'node:assert/strict';
import { attentionSummary } from '../src/lib/overview/attention.ts';
import type { Problem } from '../src/lib/api/types';

const problem = (severity: Problem['severity'], title: string): Problem => ({
  code: 'test',
  severity,
  audience: 'fleet',
  title,
});

test('the shut card says how many of each severity and names the worst', () => {
  // Worst first, as the groups arrive, and the title of the first of them:
  // what an operator reads before deciding whether to open the card.
  const line = attentionSummary([
    { severity: 'error', items: [problem('error', 'the controller is odd')] },
    { severity: 'warning', items: [problem('warning', 'a'), problem('warning', 'b')] },
  ]);
  assert.equal(line, '1 error and 2 warnings · the controller is odd');
});

test('an empty severity is left out of the line, and no problems is no line', () => {
  assert.equal(
    attentionSummary([
      { severity: 'error', items: [] },
      { severity: 'warning', items: [problem('warning', 'host demo-1 is cordoned')] },
      { severity: 'info', items: [problem('info', 'note')] },
    ]),
    '1 warning and 1 note · host demo-1 is cordoned',
  );
  assert.equal(attentionSummary([]), '');
  assert.equal(attentionSummary([{ severity: 'error', items: [] }]), '');
});
