import { test } from 'node:test';
import assert from 'node:assert/strict';
import { jobViewChips, pickView, viewSelection } from '../src/lib/jobs/view-chips.ts';

const stats = { running_jobs: 3, queued_jobs: 6 };

test('the chips are the views in the product order, All left to the All chip, each with its hint', () => {
  const chips = jobViewChips(stats, true);
  assert.deepEqual(
    chips.map((chip) => chip.value),
    ['attention', 'running', 'queued', 'failed', 'faulted', 'finished'],
  );
  assert.deepEqual(
    chips.map((chip) => chip.label),
    ['Needs attention', 'Running', 'Queued', 'Failed', 'Our failures', 'Finished'],
  );
  assert.ok(chips.every((chip) => chip.hint && chip.hint.length > 0));
});

test('only running and queued carry a count, from the band figures, and only while nothing else narrows', () => {
  // The band's figures are the fleet's: a repository or pool filter would make
  // them wrong for the rows, so the chips say nothing rather than something wrong.
  assert.deepEqual(
    jobViewChips(stats, true).map((chip) => chip.count),
    [undefined, 3, 6, undefined, undefined, undefined],
  );
  assert.deepEqual(
    jobViewChips(stats, false).map((chip) => chip.count),
    [undefined, undefined, undefined, undefined, undefined, undefined],
  );
  assert.deepEqual(
    jobViewChips(undefined, true).map((chip) => chip.count),
    [undefined, undefined, undefined, undefined, undefined, undefined],
  );
});

test('the view in force is the pressed chip, All is the All chip, and no view is nothing pressed', () => {
  assert.deepEqual(viewSelection('running'), { selected: ['running'], none: false });
  assert.deepEqual(viewSelection('all'), { selected: [], none: false });
  // A conclusion facet or two states at once: no chip says it, All included.
  assert.deepEqual(viewSelection(''), { selected: [], none: true });
});

test('a press is one view: the chip that changed, and an emptied selection is All', () => {
  assert.equal(pickView('running', ['running', 'queued']), 'queued');
  assert.equal(pickView('running', []), 'all');
  assert.equal(pickView('all', ['failed']), 'failed');
  assert.equal(pickView('', ['attention']), 'attention');
});
