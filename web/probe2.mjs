import { chromium } from '@playwright/test';
const browser = await chromium.launch(
  process.env.PLAYWRIGHT_CHROMIUM ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM } : {},
);
const page = await browser.newPage();
await page.goto('http://127.0.0.1:8099/usage?since=2026-08-01&until=2026-08-31&group_by=workflow', {
  waitUntil: 'domcontentloaded',
});
await page.waitForTimeout(1500);
for (const name of ['From', 'to', 'To', 'Group by']) {
  try {
    console.log(name, '=>', await page.getByLabel(name).count());
  } catch (e) {
    console.log(name, 'ERR', String(e).slice(0, 200));
  }
}
console.log('exact to =>', await page.getByLabel('to', { exact: true }).count());
await browser.close();
