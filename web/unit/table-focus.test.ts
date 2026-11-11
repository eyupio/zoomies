import assert from 'node:assert/strict';
import test from 'node:test';
import { FOCUS_PARAM, focusAddress, focusedGrid } from '../src/lib/table-focus.ts';

// The filters are the reason focus lives on the page's own address: a view
// narrowed to busy runners has to stay narrowed on the way in and out.
test('entering focus keeps every other parameter of the address', () => {
  assert.equal(
    focusAddress('/runners', '?state=busy&state=idle&q=linux', 'runners'),
    '/runners?state=busy&state=idle&q=linux&focus=runners',
  );
});

test('leaving focus removes that parameter and nothing else', () => {
  assert.equal(
    focusAddress('/runners', '?state=busy&focus=runners&sort=name', null),
    '/runners?state=busy&sort=name',
  );
  assert.equal(focusAddress('/runners', '?focus=runners', null), '/runners');
});

test('the hash is carried across, since a band link can land on one', () => {
  assert.equal(focusAddress('/jobs', '', 'jobs', '#jobs-list'), '/jobs?focus=jobs#jobs-list');
});

// A page with three grids has to say which one was meant.
test('focusing a second grid replaces the first rather than naming both', () => {
  assert.equal(
    focusAddress('/providers/p1', '?focus=provider-orphans-empty', 'provider-orphans-untracked'),
    '/providers/p1?focus=provider-orphans-untracked',
  );
});

test('the focused grid is read from the query string, and an empty one is none', () => {
  assert.equal(FOCUS_PARAM, 'focus');
  assert.equal(focusedGrid('?state=busy&focus=runners'), 'runners');
  assert.equal(focusedGrid('?focus='), null);
  assert.equal(focusedGrid('?focus=%20'), null);
  assert.equal(focusedGrid(''), null);
});
