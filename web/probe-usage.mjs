import { chromium } from '@playwright/test';
const browser = await chromium.launch(
  process.env.PLAYWRIGHT_CHROMIUM ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM } : {},
);
const page = await browser.newPage();
await page.goto('http://127.0.0.1:8099/usage?since=2026-08-01&until=2026-08-31&group_by=workflow', {
  waitUntil: 'domcontentloaded',
});
await page.waitForTimeout(1500);
console.log(
  await page.evaluate(() =>
    [...document.querySelectorAll('input,select')].map((el) => ({
      tag: el.tagName,
      type: el.type,
      id: el.id,
      value: el.value,
      label: document.querySelector(`label[for="${el.id}"]`)?.textContent,
      aria: el.getAttribute('aria-label'),
    })),
  ),
);
console.log('URL', page.url());
await browser.close();
