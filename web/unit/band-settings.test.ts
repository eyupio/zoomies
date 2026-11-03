import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  bandSettingsDocument,
  DEFAULT_BAND_SETTINGS,
  readBandSettings,
  type BandSettings,
} from '../src/lib/bands/settings.ts';

// A document is written by whichever build the person last used. Reading one is
// therefore a question about what this build can honour, not a validation pass:
// a page or a cut it does not know must not become a band that never appears.
test('a document this build cannot draw falls back field by field', () => {
  const bands = readBandSettings({
    hidden: ['jobs', 'audit', 'jobs'],
    sizes: { runners: 'slim', pools: 'vast', settings: 'compact' },
    // Fields an older build wrote for the dog are not read any more.
    logo: true,
    logo_size: 'large',
    logo_pages: { hosts: false },
    logo_sizes: { usage: 'large' },
    picture: 'no',
    picture_pages: { overview: false, audit: false },
  });
  assert.deepEqual(bands.hidden, ['jobs']);
  assert.equal(bands.picture, true);
  assert.deepEqual(bands.picturePages, { overview: false });
  assert.deepEqual(bands.sizes, { runners: 'slim' });
  assert.deepEqual(Object.keys(bands).sort(), ['hidden', 'picture', 'picturePages', 'sizes']);
});

test('an empty or foreign document is the defaults, not a failure', () => {
  for (const raw of [undefined, null, '', 42, [], { other: true }]) {
    assert.deepEqual(readBandSettings(raw), {
      hidden: [],
      sizes: {},
      picture: true,
      picturePages: {},
    });
  }
});

// The account document is a replacement, so every field has to be written even
// when it is the default: a page left out would be read as never chosen.
test('what is written back carries every field and survives a read', () => {
  const settings: BandSettings = {
    hidden: ['kennel'],
    sizes: { overview: 'expanded' },
    picture: false,
    picturePages: { overview: true },
  };
  const document = bandSettingsDocument(settings);
  assert.deepEqual(document, {
    hidden: ['kennel'],
    sizes: { overview: 'expanded' },
    picture: false,
    picture_pages: { overview: true },
  });
  assert.deepEqual(readBandSettings(document), settings);
});

test('the document is a copy, so a later change does not rewrite what was sent', () => {
  const settings: BandSettings = {
    ...DEFAULT_BAND_SETTINGS,
    hidden: ['jobs'],
    sizes: { runners: 'slim' },
  };
  const document = bandSettingsDocument(settings);
  settings.hidden.push('pools');
  settings.sizes.runners = 'compact';
  assert.deepEqual(document.hidden, ['jobs']);
  assert.deepEqual(document.sizes, { runners: 'slim' });
});
