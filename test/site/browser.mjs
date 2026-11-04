// Test only the generated public files, without starting a controller or runner.
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { createRequire } from 'node:module';
import { readFile, stat, mkdir } from 'node:fs/promises';
import { resolve, extname, sep } from 'node:path';

const { chromium, expect } = createRequire(new URL('../../web/package.json', import.meta.url))(
  '@playwright/test',
);
const root = resolve(process.argv[2] || 'site');
const screenshots = process.env.DOCS_SCREENSHOTS;
if (screenshots) await mkdir(screenshots, { recursive: true });
const mime = {
  '.html': 'text/html',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.json': 'application/json',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.webp': 'image/webp',
  '.woff2': 'font/woff2',
  '.md': 'text/markdown',
};
const server = createServer(async (request, response) => {
  try {
    const pathname = decodeURIComponent(new URL(request.url, 'http://localhost').pathname);
    let path = resolve(root, `.${pathname}`);
    if (path !== root && !path.startsWith(root + sep)) {
      response.writeHead(403).end();
      return;
    }
    if ((await stat(path)).isDirectory()) path = resolve(path, 'index.html');
    response.writeHead(200, { 'content-type': mime[extname(path)] || 'application/octet-stream' });
    response.end(await readFile(path));
  } catch {
    response.writeHead(404, { 'content-type': 'text/html' });
    response.end(await readFile(resolve(root, '404.html')));
  }
});
await new Promise((done) => server.listen(0, '127.0.0.1', done));
const base = `http://127.0.0.1:${server.address().port}/`;
const launch = { headless: true };
if (process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH)
  launch.executablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH;
let browser;
try {
  browser = await chromium.launch(launch);
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
    colorScheme: 'light',
    reducedMotion: 'reduce',
  });
  const page = await context.newPage();
  const errors = [],
    failures = [];
  page.on('pageerror', (error) => errors.push(error.message));
  page.on('response', (response) => {
    if (
      response.url().startsWith(base) &&
      response.status() >= 400 &&
      !response.url().endsWith('404.html')
    )
      failures.push([response.status(), response.url()]);
  });
  // Analytics is unrelated to navigation and should not make an offline check wait.
  await page.route('https://static.cloudflareinsights.com/**', (route) => route.abort());
  await page.goto(base);
  await expect(page.locator('.pack-map div.mermaid')).toBeVisible();
  await page.getByRole('button', { name: 'Mission control', exact: true }).click();
  await expect(page.locator('#hero-screen .on-light')).toHaveAttribute(
    'src',
    /screenshots\/mission-control\/overview-light\.webp$/,
  );
  await page.getByRole('button', { name: 'Classic', exact: true }).click();
  await page.getByRole('tab', { name: '03 Ask Eli', exact: true }).click();
  await expect(page.locator('#tour-eli')).toBeVisible();
  await page.getByRole('tab', { name: '03 Ask Eli', exact: true }).press('ArrowLeft');
  await expect(page.locator('#tour-hosts')).toBeVisible();
  await page.getByRole('button', { name: 'Search documentation', exact: true }).click();
  const search = page.locator('.md-search__input');
  await expect(search).toBeFocused();
  await search.fill('scheduler.scale_up_delay');
  const result = page.locator('.md-search-result__link').first();
  await expect(result).toBeInViewport({ timeout: 15000 });
  await result.click();
  await expect(page).toHaveURL(/configuration/);
  await page.keyboard.press('Escape');
  await expect(page.locator('#__search')).not.toBeChecked();
  await page.getByRole('button', { name: 'Switch to dark theme', exact: true }).click();
  await expect(page.locator('body')).toHaveAttribute('data-md-color-scheme', 'slate');
  await page.goto(base + 'guides/');
  await expect(page.locator('body')).toHaveAttribute('data-md-color-scheme', 'slate');
  await page.getByRole('button', { name: 'Operations', exact: true }).click();
  await expect(page.locator('.guide-all [data-category]:visible')).toHaveCount(3);
  await page.reload();
  await expect(page.locator('.guide-all [data-category]:visible')).toHaveCount(3);
  await page.getByRole('button', { name: 'All guides', exact: true }).click();
  await expect(page.locator('.guide-all [data-category]:visible')).toHaveCount(12);
  await page.getByRole('button', { name: 'Switch to light theme', exact: true }).click();
  await page.goto(base);
  await page.setViewportSize({ width: 390, height: 844 });
  for (const theme of ['light', 'dark']) {
    if (theme === 'dark')
      await page.getByRole('button', { name: 'Switch to dark theme', exact: true }).click();
    await page.getByRole('button', { name: 'Open navigation', exact: true }).click();
    await expect(page.locator('#__drawer')).toBeChecked();
    await expect(page.locator('#zoomies-navigation')).toBeInViewport({ ratio: 1 });
    await expect(page.getByRole('button', { name: 'Close navigation', exact: true })).toBeFocused();
    await expect(page.locator('.z-drawer-head .brand-word:visible')).toHaveCount(1);
    assert.ok((await page.locator('.z-drawer-head').boundingBox()).height < 100);
    assert.equal(await page.locator('.z-navigation').count(), 1);
    if (screenshots) await page.screenshot({ path: resolve(screenshots, `drawer-${theme}.png`) });
    await page.locator('.z-nav-group>summary').getByText('Documentation', { exact: true }).click();
    await page.locator('.z-nav-group>summary').getByText('Get started', { exact: true }).click();
    await expect(
      page.locator('.z-nav-scroll').getByRole('link', { name: 'Run your first job', exact: true }),
    ).toBeVisible();
    await page.getByRole('button', { name: 'Close navigation', exact: true }).click();
    await expect(page.locator('#__drawer')).not.toBeChecked();
    await expect(page.getByRole('button', { name: 'Open navigation', exact: true })).toBeFocused();
    await page.getByRole('button', { name: 'Open navigation', exact: true }).click();
    await page.keyboard.press('Escape');
    await expect(page.locator('#__drawer')).not.toBeChecked();
    // Reset native accordions before testing the other colour scheme.
    await page.goto(base);
  }
  await page.getByRole('button', { name: 'Switch to light theme', exact: true }).click();
  await page.goto(base + '#what-is-qualified');
  await expect(page.locator('[data-reference-details]')).toHaveAttribute('open', '');
  await expect(page.locator('#what-is-qualified')).toBeInViewport();
  await page.goto(base + 'start-here/#find-a-task');
  await expect(page.locator('[data-reference-details]')).toHaveAttribute('open', '');
  await expect(page.locator('#find-a-task')).toBeInViewport();
  const routes = [
    '',
    'start-here/',
    'guides/',
    'runners-without-kubernetes/',
    'quickstart/',
    'alternatives/',
    'faq/',
    'configuration/',
    '404.html',
  ];
  for (const width of [360, 390, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    for (const route of routes) {
      await page.goto(base + route);
      await page.evaluate(() => document.fonts.ready);
      if (route === '') await expect(page.locator('.pack-map div.mermaid')).toBeVisible();
      assert.equal(
        await page.evaluate(() => document.documentElement.scrollWidth > innerWidth),
        false,
        `${route || 'home'} overflows at ${width}`,
      );
      await expect(page.locator('h1')).toHaveCount(1);
      if (width < 1220) await expect(page.locator('#zoomies-navigation')).not.toBeInViewport();
      if (
        screenshots &&
        [390, 1440].includes(width) &&
        !['faq/', 'configuration/', '404.html'].includes(route)
      ) {
        await page.screenshot({
          path: resolve(screenshots, `${route.replaceAll('/', '-') || 'home-'}${width}.png`),
          fullPage: true,
        });
      }
    }
  }
  await page.goto(base);
  await page.getByRole('button', { name: 'Switch to dark theme', exact: true }).click();
  await expect(page.locator('.pack-map div.mermaid')).toBeVisible();
  if (screenshots)
    await page.screenshot({ path: resolve(screenshots, 'home-dark-1440.png'), fullPage: true });
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await page.getByRole('button', { name: 'Copy installer command', exact: true }).click();
  await expect(page.locator('.copy-status')).toHaveText('Copied');
  assert.equal(
    await page.evaluate(() => navigator.clipboard.readText()),
    'curl -fsSL https://zoomies.sh/install.sh | sh',
  );
  assert.deepEqual(errors, []);
  assert.deepEqual(failures, []);

  const nojs = await browser.newContext({
    javaScriptEnabled: false,
    viewport: { width: 390, height: 844 },
  });
  const reader = await nojs.newPage();
  await reader.goto(base);
  await expect(reader.locator('.tour-panel:visible')).toHaveCount(3);
  await reader.getByRole('button', { name: 'Open navigation', exact: true }).click();
  await expect(reader.locator('#__drawer')).toBeChecked();
  await expect(reader.locator('#zoomies-navigation')).toBeInViewport({ ratio: 1 });
  await reader
    .locator('.z-nav-group>summary')
    .getByText('Documentation', { exact: true })
    .press('Enter');
  await reader
    .locator('.z-nav-group>summary')
    .getByText('Get started', { exact: true })
    .press('Enter');
  await expect(
    reader.locator('.z-nav-scroll').getByRole('link', { name: 'Run your first job', exact: true }),
  ).toBeVisible();
  await reader.goto(base + 'guides/');
  await expect(reader.locator('.guide-all [data-category]:visible')).toHaveCount(12);
  await reader.goto(base + '#what-is-qualified');
  await expect(reader.locator('#what-is-qualified')).toBeInViewport();
  console.log(
    'Browser checks passed: search/result navigation, product tabs, dashboard styles, both themes, guide filters, mobile navigation and focus, existing deep links, Mermaid, command copy, 45 responsive page views, and no-JavaScript reading/navigation.',
  );
} finally {
  await browser?.close();
  await new Promise((done) => server.close(done));
}
