import assert from 'node:assert/strict';
import test from 'node:test';
import {
  FOCUS_PARAM,
  FocusMarks,
  focusAddress,
  focusedGrid,
  type Markable,
} from '../src/lib/table-focus.ts';

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

// A pasted address comes back as the same string, not an equivalent one: a
// parse and re-serialise would turn `%20` into `+` and reorder nothing, but
// still change what the operator sees in the address bar.
test('the other parameters are kept byte for byte, not re-serialised', () => {
  const search = '?q=linux%20arm&labels=self-hosted,x64&empty=&flag';
  assert.equal(
    focusAddress('/jobs', search, 'jobs'),
    '/jobs?q=linux%20arm&labels=self-hosted,x64&empty=&flag&focus=jobs',
  );
  assert.equal(focusAddress('/jobs', `${search}&focus=jobs`, null), `/jobs${search}`);
  // An encoded spelling of the parameter is still the parameter.
  assert.equal(focusAddress('/jobs', '?%66ocus=jobs&q=a', null), '/jobs?q=a');
  assert.equal(focusAddress('/x', '', 'a b'), '/x?focus=a%20b');
});

class Element implements Markable {
  attributes = new Set<string>();
  setAttribute(name: string): void {
    this.attributes.add(name);
  }
  removeAttribute(name: string): void {
    this.attributes.delete(name);
  }
}

// Back from one grid's focus to another's on the orphan review: the second
// grid marks the shared ancestors before the first grid's cleanup runs. The
// cleanup must not take off marks the second grid still holds.
test('a mark two grids hold stays until both let it go, in either order', () => {
  const marks = new FocusMarks<Element>();
  const shared = new Element();
  const first = marks.mark(shared, 'data-focus-path');
  const second = marks.mark(shared, 'data-focus-path');
  first();
  assert.ok(shared.attributes.has('data-focus-path'), 'the second grid still needs it');
  first();
  assert.ok(shared.attributes.has('data-focus-path'), 'a release counts once');
  second();
  assert.equal(shared.attributes.size, 0);
});

test('marks are counted per attribute and per element', () => {
  const marks = new FocusMarks<Element>();
  const a = new Element();
  const b = new Element();
  const path = marks.mark(a, 'data-focus-path');
  const fill = marks.mark(a, 'data-focus-fill');
  const other = marks.mark(b, 'data-focus-path');
  path();
  assert.deepEqual([...a.attributes], ['data-focus-fill']);
  assert.deepEqual([...b.attributes], ['data-focus-path']);
  fill();
  other();
  assert.equal(a.attributes.size + b.attributes.size, 0);
});
