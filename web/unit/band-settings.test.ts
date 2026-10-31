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
    logo: 'yes',
    logo_size: 'huge',
    logo_pages: { hosts: false, audit: true },
    logo_sizes: { usage: 'large', settings: 'small' },
  });
  assert.deepEqual(bands.hidden, ['jobs']);
  assert.deepEqual(bands.sizes, { runners: 'slim' });
  assert.equal(bands.logo, DEFAULT_BAND_SETTINGS.logo);
  assert.equal(bands.logoSize, 'auto');
  assert.deepEqual(bands.logoPages, { hosts: false });
  assert.deepEqual(bands.logoSizes, { usage: 'large' });
});

test('an empty or foreign document is the defaults, not a failure', () => {
  for (const raw of [undefined, null, '', 42, [], { other: true }]) {
    assert.deepEqual(readBandSettings(raw), {
      hidden: [],
      sizes: {},
      logo: false,
      logoSize: 'auto',
      logoPages: {},
      logoSizes: {},
    });
  }
});

// The account document is a replacement, so every field has to be written even
// when it is the default: a page left out would be read as never chosen.
test('what is written back carries every field and survives a read', () => {
  const settings: BandSettings = {
    hidden: ['kennel'],
    sizes: { overview: 'expanded' },
    logo: false,
    logoSize: 'medium',
    logoPages: { jobs: true },
    logoSizes: { hosts: 'small' },
  };
  const document = bandSettingsDocument(settings);
  assert.deepEqual(document, {
    hidden: ['kennel'],
    sizes: { overview: 'expanded' },
    logo: false,
    logo_size: 'medium',
    logo_pages: { jobs: true },
    logo_sizes: { hosts: 'small' },
  });
  assert.deepEqual(readBandSettings(document), settings);
});

test('the document is a copy, so a later change does not rewrite what was sent', () => {
  const settings: BandSettings = {
    ...DEFAULT_BAND_SETTINGS,
    hidden: ['jobs'],
    sizes: { runners: 'slim' },
    logoPages: { pools: false },
    logoSizes: { hosts: 'large' },
  };
  const document = bandSettingsDocument(settings);
  settings.hidden.push('pools');
  settings.sizes.runners = 'compact';
  settings.logoPages.pools = true;
  settings.logoSizes.hosts = 'small';
  assert.deepEqual(document.hidden, ['jobs']);
  assert.deepEqual(document.sizes, { runners: 'slim' });
  assert.deepEqual(document.logo_pages, { pools: false });
  assert.deepEqual(document.logo_sizes, { hosts: 'large' });
});
