import { test } from 'node:test';
import assert from 'node:assert/strict';
import { slotAt } from '../src/lib/actions/columnGesture.ts';

// A slot is found by the attribute the caller names, so the gesture the grid's
// columns use can drag the Overview's panels without knowing what a panel is.
function element(id: string | null, attribute: string): object {
  const dataset: Record<string, string> = {};
  if (id !== null) dataset[attribute] = id;
  const self = {
    dataset,
    closest: (selector: string) =>
      selector === `[data-${attribute}]` && id !== null ? self : null,
  };
  return self;
}

test('the slot under a point is read from the named attribute, inside the root only', () => {
  const inside = element('pools', 'panel');
  const outside = element('events', 'panel');
  const root = { contains: (el: unknown) => el === inside } as unknown as HTMLElement;
  (globalThis as { document?: unknown }).document = {
    elementsFromPoint: () => [outside, inside],
  };
  try {
    assert.equal(slotAt(root, 'panel', 10, 10), 'pools');
    assert.equal(slotAt({ contains: () => false } as unknown as HTMLElement, 'panel', 10, 10), '');
    assert.equal(slotAt(root, 'table-column', 10, 10), '');
  } finally {
    delete (globalThis as { document?: unknown }).document;
  }
});
