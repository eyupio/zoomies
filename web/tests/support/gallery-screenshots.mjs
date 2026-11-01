// Captures the development-only component gallery, in every style and colour
// mode, at desktop and phone widths.
//
// The gallery is not part of the built binary, so this cannot use the same
// controller fixture as tests/support/screenshots.mjs: it starts the Vite dev
// server the gallery actually lives on, photographs gallery.html, and stops
// the server again. Nothing here touches the application's own captures.
//
//   make screenshots-gallery                    writes design/theme-review/gallery/*.webp
//   node tests/support/gallery-screenshots.mjs DIR  writes them somewhere else
//
// Pillow is the encoder, as it is for the documentation screenshots.

import { spawn } from 'node:child_process';
import { mkdirSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { chromium } from '@playwright/test';
import { encodeWebp } from './webp.mjs';

const web = resolve(import.meta.dirname, '..', '..');
const root = resolve(web, '..');
const outDir = resolve(process.argv[2] ?? join(root, 'design', 'theme-review', 'gallery'));
const PORT = Number(process.env.ZOOMIES_GALLERY_PORT ?? 8091);
const BASE = `http://127.0.0.1:${PORT}`;
const DESKTOP = { width: 1440, height: 900 };
/** The tallest image lossless WebP can hold, in pixels. */
const WEBP_LIMIT = 16_383;
/** The width the plan names first, and the one the phone layout is judged at. */
const PHONE = { width: 375, height: 900 };

/**
 * One capture per combination and width. Full page: the gallery is a column of
 * panels, and a viewport shot would only ever show the buttons and the fields.
 */
const SHOTS = [
  { style: 'classic', mode: 'light', viewport: DESKTOP },
  { style: 'classic', mode: 'dark', viewport: DESKTOP },
  { style: 'mission-control', mode: 'light', viewport: DESKTOP },
  { style: 'mission-control', mode: 'dark', viewport: DESKTOP },
  { style: 'classic', mode: 'light', viewport: PHONE },
  { style: 'classic', mode: 'dark', viewport: PHONE },
  { style: 'mission-control', mode: 'light', viewport: PHONE },
  { style: 'mission-control', mode: 'dark', viewport: PHONE },
];

async function waitForGallery(deadlineMs = 60_000) {
  const until = Date.now() + deadlineMs;
  while (Date.now() < until) {
    try {
      const response = await fetch(`${BASE}/gallery.html`);
      if (response.ok) return;
    } catch {
      /* not up yet */
    }
    await new Promise((resolveWait) => setTimeout(resolveWait, 250));
  }
  throw new Error(`the gallery dev server did not answer on ${BASE} within a minute`);
}

const child = spawn(
  process.execPath,
  [join(web, 'tests', 'support', 'serve-gallery.mjs'), String(PORT)],
  {
    cwd: web,
    stdio: 'inherit',
  },
);

mkdirSync(outDir, { recursive: true });
const pngDir = mkdtempSync(join(tmpdir(), 'zoomies-gallery-png-'));
// The same override the suite and the documentation captures take, for a
// machine whose browser lives somewhere Playwright did not put it.
const browser = await chromium.launch(
  process.env.PLAYWRIGHT_CHROMIUM ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM } : {},
);
try {
  await waitForGallery();
  for (const shot of SHOTS) {
    const page = await browser.newPage({ viewport: shot.viewport });
    await page.goto(`${BASE}/gallery.html?style=${shot.style}&mode=${shot.mode}`, {
      waitUntil: 'domcontentloaded',
    });
    await page.getByRole('heading', { level: 1, name: 'Component gallery', exact: true }).waitFor();
    // The fonts and the grid's first fetch both land after the heading; a
    // capture with a fallback typeface or skeleton rows is not the state.
    await page.evaluate(() => document.fonts.ready);
    await page.locator('table[role="grid"] tbody tr[data-row]').first().waitFor();
    await page.waitForTimeout(250);
    const name = `gallery-${shot.style}-${shot.mode}-${shot.viewport.width}`;
    // WebP cannot hold an image taller than 16,383 pixels, and the gallery at
    // a phone's width is taller than that. A page that would not fit is
    // photographed in parts from the top, each a whole number of the viewport's
    // heights so a part never ends in the middle of a line of text that the
    // next part begins; a page that fits is one capture, as before.
    const height = await page.evaluate(() => document.documentElement.scrollHeight);
    const PART = Math.floor(16_000 / shot.viewport.height) * shot.viewport.height;
    const parts = height > WEBP_LIMIT ? Math.ceil(height / PART) : 1;
    for (let part = 0; part < parts; part++) {
      const file = parts === 1 ? name : `${name}-${part + 1}`;
      await page.screenshot({
        path: join(pngDir, `${file}.png`),
        fullPage: true,
        animations: 'disabled',
        caret: 'hide',
        ...(parts === 1
          ? {}
          : {
              clip: {
                x: 0,
                y: part * PART,
                width: shot.viewport.width,
                height: Math.min(PART, height - part * PART),
              },
            }),
      });
      console.log(`  captured ${file}`);
    }
    await page.close();
  }
  console.log(`encoding into ${outDir}`);
  encodeWebp(pngDir, outDir);
} finally {
  await browser.close();
  if (child.exitCode === null) {
    child.kill('SIGTERM');
    // Wait for the wrapper to take Vite down with it, so the captured files
    // are the last thing on the port and the process list is clean.
    await new Promise((resolveExit) => child.once('exit', resolveExit));
  }
  rmSync(pngDir, { recursive: true, force: true });
}
console.log(`wrote the gallery captures to ${outDir}`);
