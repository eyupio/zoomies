import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  FLAVOURS,
  flavourCopy,
  flavourOf,
  isToolchainImage,
  fullImageOf,
  platformForImage,
} from '../src/lib/pools/images.ts';
import type { PoolPlatform } from '../src/lib/api/types.ts';

// The catalogue as the server serves it: Ubuntu has a toolchain image and Debian
// does not, and the default is the one an empty image boots.
const ubuntu: PoolPlatform = {
  os: 'ubuntu',
  os_version: '24.04',
  label: 'Ubuntu 24.04',
  image: 'ghcr.io/eyupio/zoomies-runner:ubuntu-2404',
  full_image: 'ghcr.io/eyupio/zoomies-runner-full:ubuntu-2404',
  arches: ['amd64', 'arm64'],
  default: true,
};
const debian: PoolPlatform = {
  os: 'debian',
  os_version: '12',
  label: 'Debian 12',
  image: 'ghcr.io/eyupio/zoomies-runner:debian-12',
  arches: ['amd64', 'arm64'],
  default: false,
};
const platforms = [ubuntu, debian];

// A pool stores one string. Reading it back as a choice must never rewrite it:
// an image somebody typed is theirs, and only an exact match is called Full.
test('an empty image is Standard and the toolchain image for the platform is Full', () => {
  assert.equal(flavourOf('', ubuntu), 'standard');
  assert.equal(flavourOf('   ', ubuntu), 'standard');
  assert.equal(flavourOf(ubuntu.full_image!, ubuntu), 'full');
});

test('anything else is Custom and is kept as written, including look-alikes', () => {
  assert.equal(flavourOf('registry.example.com/runner:1', ubuntu), 'custom');
  // The stock image typed out is an explicit override, not "follow the platform".
  assert.equal(flavourOf(ubuntu.image!, ubuntu), 'custom');
  // The toolchain image of another operating system, or a hand-pinned tag, is
  // not this platform's Full and must not be silently re-pointed.
  assert.equal(flavourOf('ghcr.io/eyupio/zoomies-runner-full:ubuntu-2204', ubuntu), 'custom');
  assert.equal(flavourOf(ubuntu.full_image! + '-dev', ubuntu), 'custom');
});

test('a platform with no toolchain image never reads as Full', () => {
  assert.equal(fullImageOf(debian), '');
  assert.equal(flavourOf(ubuntu.full_image!, debian), 'custom');
});

test('Custom picked with an empty field stays Custom, so the field has somewhere to appear', () => {
  assert.equal(flavourOf('', ubuntu, true), 'custom');
});

test('"any operating system" is about the default platform, which is what an empty image boots', () => {
  assert.equal(platformForImage(platforms, undefined), ubuntu);
  assert.equal(platformForImage(platforms, debian), debian);
  assert.equal(platformForImage([], undefined), undefined);
  assert.equal(fullImageOf(undefined), '');
});

// The words are the product here. They must exist for every choice, say what is
// in it, and never state a toolchain version: those move every week with the
// lock file and a number written here would be wrong by the next release.
test('every choice has words for what it is, what is in it, when and what to keep in mind', () => {
  assert.deepEqual(
    FLAVOURS.map((entry) => entry.value),
    ['standard', 'full', 'custom'],
  );
  for (const entry of FLAVOURS) {
    for (const field of ['label', 'summary', 'contains', 'chooseWhen', 'keepInMind'] as const) {
      assert.ok(entry[field].trim().length > 20 || field === 'label', `${entry.value}.${field}`);
    }
    assert.equal(flavourCopy(entry.value), entry);
  }
});

test('the words name no toolchain version and use no dash for a pause', () => {
  const all = FLAVOURS.flatMap((entry) => [
    entry.summary,
    entry.contains,
    entry.chooseWhen,
    entry.keepInMind,
  ]).join('\n');
  assert.doesNotMatch(all, /\b\d+\.\d+\b/, 'a version number');
  assert.doesNotMatch(all, /\u2014|\s--\s/, 'a dash used as punctuation');
});

test('the Docker variant is not offered as a choice, and the Standard words say why', () => {
  assert.ok(!FLAVOURS.some((entry) => /docker/i.test(entry.label)));
  assert.match(flavourCopy('standard').keepInMind, /Docker client added/);
});

test('a toolchain image is recognised by repository, whatever its tag or digest', () => {
  assert.ok(isToolchainImage('ghcr.io/eyupio/zoomies-runner-full:ubuntu-2204'));
  assert.ok(isToolchainImage('ghcr.io/eyupio/zoomies-runner-full@sha256:abc'));
  assert.ok(!isToolchainImage('ghcr.io/eyupio/zoomies-runner:ubuntu-2404'));
  assert.ok(!isToolchainImage('registry.example.com/zoomies-runner-full:1'));
});
