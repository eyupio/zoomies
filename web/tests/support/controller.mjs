// Boots a real `zoomies controller` for a Playwright project, and tears it down.
//
// The tests drive the actual binary rather than a mock: if the UI works here it
// works in production, because it is the same server. Two projects need two
// different servers -- one with authentication off and a seeded demo fleet for
// the monitoring pages, one with authentication on and an empty database for
// the first-run pages -- so the difference between them is a few environment
// variables and everything else lives here. It used to live in both, and the
// copies had already drifted in how they shut down.

import { spawn } from 'node:child_process';
import { mkdtempSync, mkdirSync, rmSync, existsSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';

/**
 * The settings every fixture shares, given the directory it runs out of.
 *
 * Exported so the screenshot runner -- which has its own lifecycle, because it
 * boots, drives a browser and stops within one script rather than living as
 * long as a Playwright project -- still agrees with the suite about where the
 * database goes and whether an agent runs.
 */
export function controllerEnv(dir, port) {
  return {
    ZOOMIES_BIND: `127.0.0.1:${port}`,
    ZOOMIES_DB_PATH: join(dir, 'zoomies.db'),
    ZOOMIES_STATE_DIR: dir,
    ZOOMIES_CONFIG_DIR: dir,
    ZOOMIES_WORK_DIR: join(dir, 'work'),
    // No agent: these tests are about the UI, and an embedded agent would try
    // to talk to a Docker daemon that CI does not guarantee.
    ZOOMIES_AGENT_EMBEDDED: 'false',
    ZOOMIES_POLL_FALLBACK: 'false',
    ZOOMIES_LOG_FORMAT: 'text',
    ZOOMIES_LOG_LEVEL: 'warn',
  };
}

/**
 * Start the controller and keep the process alive until it exits.
 *
 * @param {object} options
 * @param {string} options.port          Loopback port to bind.
 * @param {string} options.prefix        Temporary directory prefix, for readable `ls /tmp`.
 * @param {Record<string, string>} options.env  Settings this fixture differs by.
 * @param {string} [options.tokenFile]   Where to write the setup token the
 *   controller prints while it has no accounts. The first-run fixture needs it:
 *   the bootstrap route asks for the token as proof that whoever is creating
 *   the first administrator can read the controller's log, and a test is in the
 *   same position as the operator -- it has to go and get it.
 */
export function serveController({ port, prefix, env, tokenFile }) {
  const root = resolve(import.meta.dirname, '..', '..', '..');
  const binary = join(root, 'zoomies');

  if (!existsSync(binary)) {
    console.error(
      `zoomies binary not found at ${binary}.\n` +
        `Build it first:  make build   (or: go build -o zoomies ./cmd/zoomies)`,
    );
    process.exit(1);
  }

  const dir = mkdtempSync(join(tmpdir(), prefix));
  const cleanup = () => {
    try {
      rmSync(dir, { recursive: true, force: true });
    } catch {
      /* the OS will get it */
    }
  };

  const child = spawn(binary, ['controller'], {
    // stdout is piped only when somebody is watching for the setup token, and
    // is echoed on either way: a fixture that swallowed the controller's own
    // output would make a failed boot impossible to read in CI.
    stdio: tokenFile ? ['inherit', 'pipe', 'inherit'] : 'inherit',
    env: { ...process.env, ...controllerEnv(dir, port), ...env },
  });

  if (tokenFile) {
    let seen = false;
    child.stdout.on('data', (chunk) => {
      process.stdout.write(chunk);
      if (seen) return;
      const match = /setup token\s+(\S+)/.exec(String(chunk));
      if (!match) return;
      seen = true;
      mkdirSync(dirname(tokenFile), { recursive: true });
      writeFileSync(tokenFile, match[1]);
    });
  }

  // Ask it to stop, then wait: the exit handler below does the removal. Pulling
  // the SQLite directory out from under a controller still checkpointing its
  // WAL produced an error on the way out and, now and then, a hang.
  const stop = (signal) => child.kill(signal);
  process.on('SIGINT', () => stop('SIGINT'));
  process.on('SIGTERM', () => stop('SIGTERM'));
  process.on('exit', cleanup);
  child.on('exit', (code) => {
    cleanup();
    process.exit(code ?? 0);
  });

  return child;
}
