import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { KennelOverview } from '../src/lib/api/types.ts';
import { MAX_REPO_SQUARES, repoSquares } from '../src/lib/bands/repo-squares.ts';

const overview = (extra: Partial<KennelOverview>): KennelOverview => ({
  enabled: true,
  scope: 'served',
  repositories: 6,
  not_tracked: 0,
  states: { pending: 1, partial: 1, attention: 2, best_in_show: 2 },
  counts: { error: 0, warning: 0, info: 0, waived: 0 },
  score: null,
  checks: [],
  attention: [],
  coverage: [],
  unavailable: [],
  oldest_evaluation: null,
  disabled_checks: [],
  ...extra,
});
const named = (
  name: string,
  error: number,
  warning: number,
): KennelOverview['attention'][number] => ({
  id: name,
  name,
  visibility: 'private',
  state: 'attention',
  counts: { error, warning, info: 0, waived: 0 },
});

// The squares have to add up to the band's own counts: the named ones first
// with their findings, then the rest by state, nothing invented and nothing lost.
test('named repositories come first with their findings, the rest follow by state', () => {
  const data = repoSquares(
    overview({ attention: [named('acme/api', 1, 2), named('acme/site', 0, 1)] }),
  );
  assert.deepEqual(
    data.squares.map((s) => s.kind),
    ['error', 'warning', 'clean', 'clean', 'clean', 'pending'],
  );
  assert.equal(data.squares[0]!.note, 'acme/api · 1 error · 2 warnings');
  assert.equal(data.squares.length, 6);
  assert.equal(
    data.summary,
    '6 tracked · 2 need attention · 1 with an error · 1 awaiting a first look',
  );
});

// The overview names at most ten; a fleet with more needing attention still
// gets a square for each, unnamed.
test('attention past the named list is drawn unnamed', () => {
  const data = repoSquares(
    overview({
      repositories: 7,
      states: { pending: 0, partial: 0, attention: 3, best_in_show: 4 },
      attention: [named('a/b', 1, 0)],
    }),
  );
  assert.equal(data.squares.filter((s) => s.kind === 'warning' && !s.name).length, 2);
  assert.equal(data.squares.length, 7);
});

test('a club that is off or empty draws nothing, and a huge fleet becomes a count', () => {
  assert.equal(repoSquares(null).squares.length, 0);
  assert.equal(repoSquares(overview({ enabled: false })).squares.length, 0);
  assert.equal(repoSquares(overview({ repositories: 0 })).squares.length, 0);
  const huge = repoSquares(
    overview({
      repositories: 100,
      states: { pending: 0, partial: 0, attention: 0, best_in_show: 100 },
    }),
  );
  assert.equal(huge.squares.length, MAX_REPO_SQUARES);
  assert.equal(huge.more, 100 - MAX_REPO_SQUARES);
});
