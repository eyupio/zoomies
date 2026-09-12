// Captures the screenshots docs/ and README.md embed.
//
// They are taken from the real binary, not a mock or a design file: the same
// controller the Playwright suite drives, with the demo fleet seeded,
// authentication on and an administrator signed in. A screenshot therefore
// cannot show a page the product does not have, and refreshing them after a
// UI change is one command:
//
//   make screenshots                         writes docs/screenshots/*.webp
//   node tests/support/screenshots.mjs DIR   writes them somewhere else
//
// Each theme gets a controller of its own. The fixture is placed relative to
// the moment it is seeded and the scheduler starts working on it at once --
// idle runners drain, silent hosts turn unhealthy -- so every page is captured
// in the first seconds of a controller's life, while the fleet still looks like
// the morning the seed describes.
//
// The files are lossless WebP at under half the size of the PNGs Playwright
// takes, so a page weighs about as much as a photograph rather than a small
// binary. The encoding is Pillow's (`pip install pillow`): the browser can write
// lossless WebP itself but compresses it six times worse, and the site already
// needs a Python environment to build.

import { spawn, spawnSync } from 'node:child_process';
import { controllerEnv } from './controller.mjs';
import { createHmac, randomUUID } from 'node:crypto';
import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { chromium, devices } from '@playwright/test';

const PORT = 8097;
const root = resolve(import.meta.dirname, '..', '..', '..');
const binary = join(root, 'zoomies');
const outDir = resolve(process.argv[2] ?? join(root, 'docs', 'screenshots'));

/** The administrator every shot is signed in as. The database is thrown away. */
const ADMIN = { username: 'alice', password: 'screenshots-only-not-a-secret' };

/** A common laptop, at retina density so the text survives being scaled down. */
const DESKTOP = { width: 1440, height: 900 };
const SCALE = 2;
/** Long enough for the event stream to fill the panels the first fetch does not. */
const SETTLE_MS = 750;

/** Identifiers from internal/controller/seed.go, so a shot can open a detail page. */
const FIXTURE = {
  linuxPool: 'pool_demolinux',
  busyRunner: 'run_demo00',
  installationID: 7654321,
  org: 'acme',
  webhookSecret: 'demo-webhook-secret',
};

/**
 * What is captured. `heading` is the <h1> that proves the route rendered;
 * `prepare` puts the page into the state worth showing after it has settled.
 */
const SHOTS = [
  { name: 'overview', path: '/', heading: 'Overview' },
  {
    name: 'problems',
    path: '/',
    heading: 'Overview',
    async prepare(page) {
      await page.getByRole('button', { name: /^Problems\./ }).click();
      await page.getByRole('dialog', { name: 'Problems' }).waitFor();
    },
  },
  {
    name: 'command-palette',
    path: '/',
    heading: 'Overview',
    async prepare(page) {
      await page.keyboard.press('Control+k');
      const palette = page.getByRole('dialog', { name: 'Command palette' });
      await palette.waitFor();
      await palette.getByRole('combobox').fill('demo');
    },
  },
  { name: 'pools', path: '/pools', heading: 'Pools' },
  { name: 'pool', path: `/pools/${FIXTURE.linuxPool}` },
  { name: 'runners', path: '/runners', heading: 'Runners' },
  {
    name: 'runner-history',
    path: '/runners',
    heading: 'Runners',
    async prepare(page) {
      await page.getByText('Explore fleet activity over the last hour', { exact: true }).click();
      await page
        .getByRole('region', { name: 'Fleet activity · last hour' })
        .scrollIntoViewIfNeeded();
    },
  },
  { name: 'queue', path: '/queue', heading: 'Queue' },
  { name: 'runner', path: `/runners/${FIXTURE.busyRunner}` },
  { name: 'jobs', path: '/jobs', heading: 'Jobs' },
  {
    name: 'job',
    path: '/jobs?failed=true',
    heading: 'Jobs',
    async prepare(page) {
      // A job that failed on a step of its own, rather than the one whose
      // runner died under it: the drawer names the step and links its log.
      const rows = page.getByRole('grid', { name: 'Jobs' }).locator('tbody tr[data-row]');
      await rows
        .filter({ hasText: 'Failure' })
        .filter({ hasNotText: 'Runner lost' })
        .first()
        .click();
      await page.getByRole('dialog').waitFor();
    },
  },
  { name: 'usage', path: '/usage', heading: 'Usage' },
  { name: 'hosts', path: '/hosts', heading: 'Hosts' },
  { name: 'installations', path: '/installations', heading: 'Installations' },
  {
    name: 'migrate',
    path: '/migrate',
    heading: 'Migrate repositories',
    async prepare(page) {
      // The review step: the exact diff, and the jobs it will not touch.
      await page.getByRole('radio', { name: 'acme', exact: false }).waitFor();
      for (let step = 0; step < 4; step++) {
        await page.getByRole('button', { name: 'Next' }).click();
      }
      await page.getByRole('heading', { level: 2, name: 'Review' }).waitFor();
    },
  },
  { name: 'audit', path: '/audit', heading: 'Audit' },
  { name: 'settings', path: '/settings', heading: 'Settings' },
  // The two wizards. The docs discuss both at length and photographed
  // neither, so the only way to know what "the labels step previews the
  // runs-on line" looks like was to install Zoomies and find out.
  {
    name: 'pool-wizard',
    path: '/pools/new',
    heading: 'Create a pool',
    async prepare(page) {
      // The labels step, which is the one worth showing: it previews the
      // runs-on line the labels produce.
      await page.getByRole('button', { name: 'Next' }).click();
      await page.getByRole('textbox', { name: 'Labels' }).waitFor();
    },
  },
  {
    name: 'add-host',
    path: '/hosts/new',
    heading: 'Add a host',
  },
  // Sign-in is deliberately not here. This runner bootstraps an administrator
  // so that every other page has a session, and a signed-in browser cannot
  // photograph the sign-in card. Capturing it would need the first-run fixture
  // and a second controller, which is more machinery than one screenshot of a
  // form with two fields is worth.
  //
  // Read-only monitoring on a phone is a stated requirement, so it is shown.
  { name: 'overview-phone', path: '/', heading: 'Overview', device: devices['Pixel 7'] },
];

/**
 * A browser override for sandboxes where Playwright's own Chromium download is
 * absent and a compatible build sits somewhere else. Same variable as the
 * suite's fixtures.ts.
 */
const launchOptions = process.env.PLAYWRIGHT_CHROMIUM
  ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM }
  : {};

/** Boot a seeded controller with authentication on, and wait until it answers. */
async function bootController(dir) {
  const child = spawn(binary, ['controller'], {
    // stdout is piped rather than ignored because the setup token is printed
    // there and nowhere else: the bootstrap route asks for it as proof that
    // whoever creates the first administrator can read the controller's log,
    // and this harness is in the same position as an operator. It is echoed on,
    // so a controller that fails to boot is still readable.
    stdio: ['ignore', 'pipe', 'inherit'],
    env: {
      ...process.env,
      ...controllerEnv(dir, PORT),
      // Configured the way a finished install is, so the problems drawer
      // shows the fleet's problems and not this harness's: an external URL
      // (loopback, so the session cookie is not marked Secure and a plain-http
      // page can hold it) and the poller left on. The poller skips the demo
      // installation, so nothing reaches for GitHub.
      ZOOMIES_EXTERNAL_URL: `http://127.0.0.1:${PORT}`,
      ZOOMIES_POLL_FALLBACK: 'true',
      ZOOMIES_SEED_DEMO: 'true',
    },
  });
  // Scoped to this controller, not to the module: each colour scheme gets a
  // fresh database and therefore a fresh token, and the second boot carrying
  // the first one over is a 422 that reads like a broken form.
  let setupToken = '';
  child.stdout.on('data', (chunk) => {
    process.stdout.write(chunk);
    if (setupToken !== '') return;
    const match = /setup token\s+(\S+)/.exec(String(chunk));
    if (match) setupToken = match[1];
  });
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) {
      throw new Error(`the controller exited with ${child.exitCode} before it was ready`);
    }
    try {
      const res = await fetch(`http://127.0.0.1:${PORT}/healthz`);
      // Answering /healthz does not mean the token line has been flushed, so
      // both are waited for: bootstrapping without one is a 422 that reads
      // like a bug in the form rather than a race in this script.
      if (res.ok && setupToken !== '') return { child, setupToken };
    } catch {
      /* not listening yet */
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  child.kill('SIGKILL');
  throw new Error(
    setupToken === ''
      ? 'the controller never printed a setup token'
      : 'the controller did not answer /healthz within 30s',
  );
}

function stopController(child) {
  return new Promise((done) => {
    if (child.exitCode !== null) return done();
    const hard = setTimeout(() => child.kill('SIGKILL'), 5_000);
    child.once('exit', () => {
      clearTimeout(hard);
      done();
    });
    child.kill('SIGTERM');
  });
}

/**
 * Deliver GitHub's ping, signed with the demo installation's secret.
 *
 * A verified delivery is what tells the controller that webhooks reach it; until
 * one has, every page carries a warning that scaling is running on the poller,
 * which is true of this harness and not of the fleet the screenshots show.
 */
async function pingWebhook(baseURL) {
  const body = JSON.stringify({
    zen: 'Keep it logically awesome.',
    hook_id: 1,
    organization: { login: FIXTURE.org },
    installation: { id: FIXTURE.installationID },
  });
  const signature = createHmac('sha256', FIXTURE.webhookSecret).update(body).digest('hex');
  const res = await fetch(`${baseURL}/webhooks/github`, {
    method: 'POST',
    headers: {
      'content-type': 'application/json',
      'x-github-event': 'ping',
      'x-github-delivery': randomUUID(),
      'x-hub-signature-256': `sha256=${signature}`,
    },
    body,
  });
  if (!res.ok) {
    throw new Error(`the ping was not accepted: ${res.status} ${await res.text()}`);
  }
}

/** Encode every PNG in `pngDir` as lossless WebP in `outDir`, with Pillow. */
function encodeWebp(pngDir) {
  const script = [
    'import pathlib, sys',
    'from PIL import Image',
    'src, dst = map(pathlib.Path, sys.argv[1:3])',
    'for png in sorted(src.glob("*.png")):',
    '    out = dst / (png.stem + ".webp")',
    '    Image.open(png).convert("RGB").save(out, lossless=True, quality=100, method=6)',
    '    print(f"  {out.name}  {out.stat().st_size // 1024} KB")',
  ].join('\n');
  const run = spawnSync('python3', ['-u', '-c', script, pngDir, outDir], { stdio: 'inherit' });
  if (run.error || run.status !== 0) {
    throw new Error(
      'encoding the screenshots needs Python 3 with Pillow: pip install pillow\n' +
        `(python3 ${run.error ? `could not start: ${run.error.message}` : `exited ${run.status}`})`,
    );
  }
}

/** Wait for the page itself, never for the network: the event stream never idles. */
async function settle(page, shot) {
  const heading = shot.heading
    ? page.getByRole('heading', { level: 1, name: shot.heading, exact: true })
    : page.getByRole('heading', { level: 1 }).first();
  await heading.waitFor();
  await page.evaluate(() => document.fonts.ready);
  const grid = page.getByRole('grid').first();
  if (await grid.count()) {
    await grid.locator('tbody tr[data-row]').first().waitFor();
  }
  await page.waitForTimeout(SETTLE_MS);
}

async function capture(browser, scheme, pngDir) {
  const dir = mkdtempSync(join(tmpdir(), 'zoomies-screenshots-'));
  const { child, setupToken } = await bootController(dir);
  const baseURL = `http://127.0.0.1:${PORT}`;
  // en-GB, and a fixed one. Native date inputs render in the browser's locale,
  // and a runner with none set takes the machine's -- so the shipped
  // documentation showed `mm/dd/yyyy` placeholders to a project whose prose is
  // British throughout. The timezone is pinned for the same reason: a
  // screenshot of "3 minutes ago" should not depend on where CI is.
  const common = {
    baseURL,
    colorScheme: scheme,
    reducedMotion: 'reduce',
    locale: 'en-GB',
    timezoneId: 'Europe/London',
  };
  try {
    const desktop = await browser.newContext({
      ...common,
      viewport: DESKTOP,
      deviceScaleFactor: SCALE,
    });
    // The first administrator, created the way the first-run form does it.
    // The 201 sets the session cookie on this context.
    const created = await desktop.request.post('/api/v1/auth/bootstrap', {
      data: { ...ADMIN, setup_token: setupToken },
    });
    if (created.status() !== 201) {
      throw new Error(`bootstrap returned ${created.status()}: ${await created.text()}`);
    }
    const cookies = await desktop.cookies();
    await pingWebhook(baseURL);

    for (const shot of SHOTS) {
      console.log(`  capturing ${shot.name}-${scheme}`);
      let context = desktop;
      if (shot.device) {
        context = await browser.newContext({ ...shot.device, ...common });
        await context.addCookies(cookies);
      }
      const page = await context.newPage();
      await page.goto(shot.path, { waitUntil: 'domcontentloaded' });
      await settle(page, shot);
      if (shot.prepare) {
        await shot.prepare(page);
        await page.waitForTimeout(SETTLE_MS / 2);
      }
      const png = await page.screenshot({ animations: 'disabled', caret: 'hide' });
      writeFileSync(join(pngDir, `${shot.name}-${scheme}.png`), png);
      await page.close();
      if (shot.device) await context.close();
    }
    await desktop.close();
  } finally {
    await stopController(child);
    rmSync(dir, { recursive: true, force: true });
  }
}

if (!existsSync(binary)) {
  console.error(`zoomies binary not found at ${binary}.\nBuild it first:  make build`);
  process.exit(1);
}
mkdirSync(outDir, { recursive: true });

const pngDir = mkdtempSync(join(tmpdir(), 'zoomies-screenshots-png-'));
const browser = await chromium.launch(launchOptions);
try {
  for (const scheme of ['dark', 'light']) {
    console.log(`capturing ${scheme}`);
    await capture(browser, scheme, pngDir);
  }
  console.log(`encoding into ${outDir}`);
  encodeWebp(pngDir);
} finally {
  await browser.close();
  rmSync(pngDir, { recursive: true, force: true });
}
console.log(`wrote ${SHOTS.length * 2} screenshots to ${outDir}`);
