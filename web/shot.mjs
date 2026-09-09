import { chromium } from '@playwright/test';
const browser = await chromium.launch(
  process.env.PLAYWRIGHT_CHROMIUM ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM } : {},
);
const out = '/tmp/claude-0/-home-user-Zoomies/3bfe6a29-40bd-5bf8-801d-7ee2dbed3259/scratchpad';
const page = await browser.newPage({ viewport: { width: 1400, height: 760 }, colorScheme: 'dark' });
await page.goto('http://127.0.0.1:8099/usage', { waitUntil: 'domcontentloaded' });
await page.waitForTimeout(1800);
await page.screenshot({ path: `${out}/usage-dark.png` });
await browser.close();
