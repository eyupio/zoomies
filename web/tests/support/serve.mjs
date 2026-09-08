// The monitoring fixture: authentication off and a seeded demo fleet.
//
// Authentication is disabled on loopback, which is the only place the config
// validator permits it, so the grids and the Overview are reachable without a
// sign-in step in every spec. The seed is deterministic, so the fixtures in
// ./fixtures.ts can assert on exact counts.

import { serveController } from './controller.mjs';

serveController({
  port: process.argv[2] ?? '8099',
  prefix: 'zoomies-e2e-',
  env: {
    ZOOMIES_DISABLE_AUTH: 'true',
    // Seeds a deterministic fixture fleet so pages have content to assert on.
    ZOOMIES_SEED_DEMO: 'true',
  },
});
