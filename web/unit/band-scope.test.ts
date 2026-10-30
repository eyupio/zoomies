import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ALL_BAND_PAGES, bandPagesSentence, pagesForScope } from '../src/lib/bands/scope.ts';

test('this page means one page, every page means all of them in listing order', () => {
  assert.deepEqual(pagesForScope('page', 'runners', []), ['runners']);
  assert.deepEqual(pagesForScope('all', 'runners', []), [...ALL_BAND_PAGES]);
});

test('a picked set is the pages that exist, not the order they were ticked', () => {
  assert.deepEqual(pagesForScope('selected', 'runners', ['jobs', 'runners']), ['runners', 'jobs']);
});

// The footer sentence has to stay a sentence when one page is chosen, three are,
// or thirteen are; a wall of names would push the button that applies it off
// the panel.
test('a list of pages reads as a sentence at every length', () => {
  assert.equal(bandPagesSentence([]), 'no pages');
  assert.equal(bandPagesSentence(['runners']), 'Runners');
  assert.equal(bandPagesSentence(['runners', 'pools']), 'Runners and Pools');
  assert.equal(bandPagesSentence(['runners', 'pools', 'jobs']), 'Runners, Pools and Jobs');
  assert.equal(
    bandPagesSentence(['runners', 'pools', 'jobs', 'queue']),
    'Runners, Pools and 2 more',
  );
});
