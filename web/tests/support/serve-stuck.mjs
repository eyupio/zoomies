// The diagnostics fixture: a demo fleet with three things wrong with it.
//
// The monitoring fixture next door is deliberately healthy, and its two
// starting runners are kept young so a demo never reports a fault. That leaves
// every page an operator opens on their worst day -- the problems drawer, a
// runner that is not progressing, a pool nothing can place, a job GitHub is
// holding -- with nothing to render, which is why they went untested. This
// server is the same binary with ZOOMIES_SEED_STUCK on top.

import { serveController } from './controller.mjs';

serveController({
  port: process.argv[2] ?? '8097',
  prefix: 'zoomies-e2e-stuck-',
  env: {
    ZOOMIES_DISABLE_AUTH: 'true',
    ZOOMIES_SEED_DEMO: 'true',
    // Ages the demo's two starting runners past the point where the fleet
    // calls them stuck, and adds the blocked pool and the held job.
    ZOOMIES_SEED_STUCK: 'true',
  },
});
