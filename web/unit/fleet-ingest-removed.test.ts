import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { test } from 'node:test';

const src = join(import.meta.dirname, '..', 'src');

// The Runners grid warms the cache with the page it shows, and with "Include
// removed" on that page is mostly runners that no longer exist. Cached, they
// were listed on their pool's page as its runners and offered by the palette
// until the stream next reconnected. The cache imports a store by a path Node
// cannot load, so the rule is held by its shape, as the dismissals sync is.
test('a removed runner a grid shows is dropped from the cache, never written to it', () => {
  const cache = readFileSync(join(src, 'lib/state/fleet.svelte.ts'), 'utf8');
  const ingest = cache.slice(cache.indexOf('ingestRunners('), cache.indexOf('ingestPools('));
  assert.match(
    ingest,
    /if \(runner\.state === 'removed'\) this\.#delete\(this\.#data\.runners, runner\.id\);\s*else this\.#set\(this\.#data\.runners, runner\.id, runner\);/,
  );
});

// A removed row is never cached, so reading "not cached" as "differs" would
// warm the cache on every fetch, and each warming can move the fleet's shape,
// which is the grid's live key: a page that refetches itself for ever.
test('the Runners grid does not count an uncached removed row as a reason to warm the cache', () => {
  const page = readFileSync(join(src, 'routes/Runners.svelte'), 'utf8');
  const take = page.slice(page.indexOf('function takeRows('), page.indexOf('function resolve('));
  assert.match(take, /if \(row\.state === 'removed'\) return cached !== undefined;/);
});
