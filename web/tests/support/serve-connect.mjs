// The connect fixture: an empty fleet and a GitHub a browser can reach.
//
// Everything below the browser already runs against `internal/github`'s fake,
// but that fake is an in-process test server -- so the one client that could
// never reach it was the one an operator uses. Connecting an App and verifying
// it were tested at every layer except the page they happen on, and the only
// fail-then-recover path in the whole suite was a wrong password.
//
// This runs the same fake as a program on a loopback port, next to a controller
// with nothing seeded, and writes what a spec needs to connect to it: the URL,
// the two identifiers, and a private key generated here because the connect
// form asks for one and the sealing is real even when the signature is never
// checked.

import { spawn } from 'node:child_process';
import { generateKeyPairSync } from 'node:crypto';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';

import { serveController } from './controller.mjs';

const port = process.argv[2] ?? '8096';
const detailsFile = process.argv[3] ?? 'test-results/fakegithub.json';
const root = resolve(import.meta.dirname, '..', '..', '..');

// A real RSA key, because Zoomies signs its App JWT with it: the fake never
// checks the signature, but the signing happens before the request does.
const { privateKey } = generateKeyPairSync('rsa', { modulusLength: 2048 });
const pem = privateKey.export({ type: 'pkcs1', format: 'pem' }).toString();

// `go run` rather than a built binary: this is a test-only program, and asking
// the suite to remember to build it is how it comes to be stale.
const fake = spawn('go', ['run', './test/fakegithub'], {
  cwd: root,
  stdio: ['inherit', 'pipe', 'inherit'],
  env: process.env,
});

// The file from a previous run names a port nothing is listening on any more,
// and a spec that read it would fail with "connection refused" against an
// address that was right yesterday. It goes before the fake starts, and comes
// back only once this fake has said where it is.
rmSync(detailsFile, { force: true });

const announced = new Promise((resolveAddress) => {
  let seen = false;
  fake.stdout.on('data', (chunk) => {
    const line = String(chunk);
    process.stdout.write(line);
    if (seen) return;
    const match = /url=(\S+) app_id=(\d+) installation_id=(\d+)/.exec(line);
    if (!match) return;
    seen = true;
    mkdirSync(dirname(detailsFile), { recursive: true });
    writeFileSync(
      detailsFile,
      JSON.stringify({ url: match[1], appId: match[2], installationId: match[3], privateKey: pem }),
    );
    resolveAddress(match[1]);
  });
});

const stopFake = () => fake.kill('SIGTERM');
process.on('exit', stopFake);
process.on('SIGINT', stopFake);
process.on('SIGTERM', stopFake);

// The controller starts only once the address is on disk: Playwright treats
// this fixture as ready when /healthz answers, and a spec that got there first
// would read whatever the last run left behind.
await announced;

serveController({
  port,
  prefix: 'zoomies-e2e-connect-',
  env: {
    ZOOMIES_DISABLE_AUTH: 'true',
    // Nothing seeded: this fixture is about connecting an installation, and a
    // demo one already on the page would answer the question first.
    ZOOMIES_SEED_DEMO: 'false',
  },
});
