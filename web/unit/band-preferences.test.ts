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
  prefs.applyBandSize(['runners', 'pools', 'jobs'], 'compact');
  assert.equal(prefs.bandSize('runners'), 'compact');
  assert.equal(prefs.bandSize('pools'), 'compact');
  assert.equal(prefs.bandSize('jobs'), 'compact');
  assert.equal(prefs.bandSize('hosts'), 'slim');
  assert.equal(prefs.bandSize('overview'), 'slim');

  // Choosing a page's own default again lets it follow the default if it moves.
  prefs.applyBandSize(['runners'], 'slim');
  assert.equal(prefs.bandSize('runners'), 'slim');
});

// A band's picture has a default for every page, a page's own answer against
// it, and a default pressed in Appearance that puts every page back in step.
test('a band picture can be switched off per page or for every page, and put back', () => {
  assert.equal(prefs.bandPictureShown('overview'), true);
  assert.equal(prefs.bandPictureCustom('overview'), false);

  prefs.setBandPictureShown('overview', false);
  assert.equal(prefs.bandPictureShown('overview'), false);
  assert.equal(prefs.bandPictureCustom('overview'), true);
  assert.equal(prefs.bandPictureShown('hosts'), true);

  prefs.clearBandPictureOverride('overview');
  assert.equal(prefs.bandPictureShown('overview'), true);
  assert.equal(prefs.bandPictureCustom('overview'), false);

  prefs.setBandPictureShown('hosts', false);
  prefs.bandPicture = false;
  assert.equal(prefs.bandPictureCustom('hosts'), false);
  assert.equal(prefs.bandPictureShown('hosts'), false);
  assert.equal(prefs.bandPictureShown('runners'), false);
  prefs.bandPicture = true;
});
