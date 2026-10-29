import assert from 'node:assert/strict';
import { test } from 'node:test';

// The store is a Svelte runes class. Outside the compiler a rune is its value,
// which is all these tests need of them.
const runes = globalThis as unknown as Record<string, unknown>;
runes.$state = <T>(v: T) => v;
runes.$derived = <T>(v: T) => v;
runes.$effect = Object.assign(() => {}, { root: () => () => {} });

// The browser's record is part of what these tests check, so the store gets a
// localStorage that behaves like one rather than no storage at all.
const saved = new Map<string, string>();
(globalThis as unknown as { localStorage: Storage }).localStorage = {
  getItem: (key: string) => saved.get(key) ?? null,
  setItem: (key: string, value: string) => void saved.set(key, String(value)),
  removeItem: (key: string) => void saved.delete(key),
  clear: () => saved.clear(),
  key: (index: number) => [...saved.keys()][index] ?? null,
  get length() {
    return saved.size;
  },
} as Storage;

const { prefs } = await import('../src/lib/state/prefs.svelte.ts');

// A change made on one band can be scoped to a set of pages, which is the whole
// point of the picker: one deliberate cut rather than eight identical presses.
test('a cut applies to the pages a change was scoped to, and only those', () => {
  prefs.applyBandSize(['runners', 'pools', 'jobs'], 'slim');
  assert.equal(prefs.bandSize('runners'), 'slim');
  assert.equal(prefs.bandSize('pools'), 'slim');
  assert.equal(prefs.bandSize('jobs'), 'slim');
  assert.equal(prefs.bandSize('hosts'), 'compact');
  assert.equal(prefs.bandSize('overview'), 'standard');

  // Choosing a page's own default again lets it follow the default if it moves.
  prefs.applyBandSize(['runners'], 'compact');
  assert.equal(prefs.bandSize('runners'), 'compact');
});

test('the dog can be changed or removed per page, and put back on the default', () => {
  assert.equal(prefs.bandLogoShown('jobs'), true);
  assert.equal(prefs.bandLogoSizeFor('jobs'), 'auto');
  assert.equal(prefs.bandLogoCustom('jobs'), false);

  prefs.applyBandLogo(['jobs', 'queue'], { shown: false, size: 'large' });
  assert.equal(prefs.bandLogoShown('jobs'), false);
  assert.equal(prefs.bandLogoShown('queue'), false);
  assert.equal(prefs.bandLogoSizeFor('jobs'), 'large');
  assert.equal(prefs.bandLogoCustom('jobs'), true);
  // The pages that were not in the scope are untouched.
  assert.equal(prefs.bandLogoShown('hosts'), true);

  prefs.clearBandLogoOverride('jobs');
  assert.equal(prefs.bandLogoShown('jobs'), true);
  assert.equal(prefs.bandLogoSizeFor('jobs'), 'auto');
  assert.equal(prefs.bandLogoCustom('jobs'), false);
  // Queue still has its own, which is what a per-page reset means.
  assert.equal(prefs.bandLogoShown('queue'), false);
});

// The switch in Appearance says "Show the logo in the band", not "the default
// for pages that have not chosen": it has to be true of every page when pressed.
test('changing the dog in Appearance puts every page back in step', () => {
  prefs.applyBandLogo(['jobs'], { shown: false, size: 'auto' });
  assert.equal(prefs.bandLogoCustom('jobs'), true);

  prefs.bandLogo = true;
  assert.equal(prefs.bandLogoCustom('jobs'), false);
  assert.equal(prefs.bandLogoShown('jobs'), true);

  prefs.applyBandLogoSize(['hosts'], 'large');
  assert.equal(prefs.bandLogoCustom('hosts'), true);
  prefs.bandLogoSize = 'auto';
  assert.equal(prefs.bandLogoCustom('hosts'), false);
  assert.equal(prefs.bandLogoSizeFor('hosts'), 'auto');
});

test('switching bands off keeps the hidden list in Appearance order', () => {
  prefs.applyBandShown(['jobs', 'overview', 'kennel'], false);
  assert.equal(prefs.bandShown('jobs'), false);
  assert.equal(prefs.bandShown('overview'), false);
  // hiddenBands is the browser's exception list, and is what the document carries.
  const stored = JSON.parse(saved.get('zoomies.prefs') ?? '{}');
  assert.deepEqual(stored.hiddenBands, ['overview', 'jobs', 'kennel']);

  prefs.applyBandShown(['overview', 'jobs', 'kennel'], true);
  for (const page of ['overview', 'jobs', 'kennel'] as const) {
    assert.equal(prefs.bandShown(page), true, page);
  }
});
