// The negotiation rules and the handler's decisions, against an in-memory
// origin. The Markdown copies the worker serves are written by hooks/seo.py,
// so a change to their address or their shape fails here rather than at the
// edge. Plain node:test, so no dependency beyond the binary runs it.

import test from "node:test";
import assert from "node:assert/strict";

import worker, { markdownPath, tokenEstimate, wantsMarkdown } from "./worker.js";

const HTML = "<!doctype html><html><body><p>the page</p></body></html>";
const MARKDOWN = "# The page\n\nIts body as Markdown.\n";

function originFor(paths) {
  return async (input) => {
    const url = input instanceof URL ? input : new URL(input.url);
    const found = paths.get(url.pathname);
    if (found === undefined) {
      return new Response(HTML, { status: 404, headers: { "content-type": "text/html; charset=utf-8" } });
    }
    return new Response(found.body, {
      status: found.status ?? 200,
      headers: found.headers ?? { "content-type": found.type },
    });
  };
}

async function handle(url, paths, init = {}) {
  const previous = globalThis.fetch;
  globalThis.fetch = originFor(new Map([...paths]));
  try {
    return await worker.fetch(new Request(url, init));
  } finally {
    globalThis.fetch = previous;
  }
}

test("a request that asks for markdown gets the page's copy", async () => {
  const response = await handle("https://zoomies.sh/quickstart/", new Map([
    ["/quickstart/index.md", { type: "text/markdown; charset=utf-8", body: MARKDOWN }],
  ]), { headers: { accept: "text/markdown" } });

  assert.equal(response.status, 200);
  assert.equal(response.headers.get("content-type"), "text/markdown; charset=utf-8");
  assert.equal(response.headers.get("vary"), "Accept");
  assert.equal(response.headers.get("x-markdown-tokens"), String(Math.ceil(MARKDOWN.length / 4)));
  assert.equal(await response.text(), MARKDOWN);
});

test("the site root's markdown copy is served for the bare host", async () => {
  const response = await handle("https://zoomies.sh", new Map([
    ["/index.md", { type: "text/markdown; charset=utf-8", body: MARKDOWN }],
  ]), { headers: { accept: "text/markdown" } });

  assert.equal(response.status, 200);
  assert.equal(await response.text(), MARKDOWN);
});

test("a browser's accept header gets the HTML the origin serves", async () => {
  const response = await handle("https://zoomies.sh/quickstart/", new Map([
    ["/quickstart/", { type: "text/html; charset=utf-8", body: HTML }],
  ]), { headers: { accept: "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8" } });

  assert.equal(response.status, 200);
  assert.equal(response.headers.get("content-type"), "text/html; charset=utf-8");
  assert.equal(await response.text(), HTML);
});

test("an address with no markdown copy falls back to the origin's answer", async () => {
  const response = await handle("https://zoomies.sh/not-a-page/", new Map([]), {
    accept: "text/markdown",
  });

  assert.equal(response.status, 404);
  assert.equal(response.headers.get("content-type"), "text/html; charset=utf-8");
});

test("a file that is a format of its own is never replaced with markdown", async () => {
  const catalog = '{"version": 1}';
  const response = await handle("https://zoomies.sh/catalog.json", new Map([
    ["/catalog.json", { type: "application/json", body: catalog }],
  ]), { headers: { accept: "text/markdown" } });

  assert.equal(response.status, 200);
  assert.equal(response.headers.get("content-type"), "application/json");
  assert.equal(await response.text(), catalog);
});

test("a HEAD request for markdown answers with the headers and no body", async () => {
  const response = await handle("https://zoomies.sh/quickstart/", new Map([
    ["/quickstart/index.md", { type: "text/markdown; charset=utf-8", body: MARKDOWN }],
  ]), {
    headers: { accept: "text/markdown" },
    method: "HEAD",
  });

  assert.equal(response.status, 200);
  assert.equal(response.headers.get("content-type"), "text/markdown; charset=utf-8");
  assert.equal(await response.text(), "");
});

test("markdown is refused only when an entry's quality refuses it", () => {
  assert.equal(wantsMarkdown("text/markdown"), true);
  assert.equal(wantsMarkdown("TEXT/Markdown"), true);
  assert.equal(wantsMarkdown("text/markdown; q=0.5"), true);
  assert.equal(wantsMarkdown(" text/markdown , text/html"), true);
  assert.equal(wantsMarkdown("text/markdown;q=0"), false);
  assert.equal(wantsMarkdown("text/markdown; Q=0, text/html;q=0.9"), false);
  assert.equal(wantsMarkdown("text/html,*/*;q=0.8"), false);
  assert.equal(wantsMarkdown(""), false);
  assert.equal(wantsMarkdown(null), false);
});

test("the copy's address follows the built site's shape", () => {
  assert.equal(markdownPath("/"), "/index.md");
  assert.equal(markdownPath("/quickstart/"), "/quickstart/index.md");
  assert.equal(markdownPath("/quickstart"), "/quickstart/index.md");
  assert.equal(markdownPath("/index.html"), "/index.md");
  assert.equal(markdownPath("/404.html"), "/404.md");
  assert.equal(markdownPath("/catalog.json"), null);
  assert.equal(markdownPath("/install.sh"), null);
  assert.equal(markdownPath("/screenshots/ui.webp"), null);
  assert.equal(markdownPath("/llms.txt"), null);
});

test("the token estimate rounds up the parts of a token", () => {
  assert.equal(tokenEstimate(""), 0);
  assert.equal(tokenEstimate("abcd"), 1);
  assert.equal(tokenEstimate("abcde"), 2);
});

const CATALOG = JSON.stringify({
  linkset: [
    {
      anchor: "https://demo.zoomies.sh/api/v1/",
      "service-desc": [{ href: "https://raw.githubusercontent.com/eyupio/zoomies/main/api/openapi.yaml", type: "application/yaml" }],
      "service-doc": [{ href: "https://zoomies.sh/api-surface/", type: "text/html" }],
      status: [{ href: "https://demo.zoomies.sh/healthz", type: "application/json" }],
    },
  ],
});

test("the well-known api-catalog address is answered with the catalog re-typed", async () => {
  const response = await handle("https://zoomies.sh/.well-known/api-catalog", new Map([
    ["/.well-known/api-catalog.json", { type: "application/json", body: CATALOG }],
  ]), { headers: { accept: "application/linkset+json" } });

  assert.equal(response.status, 200);
  assert.equal(response.headers.get("content-type"), "application/linkset+json");
  assert.equal(await response.text(), CATALOG);
});

test("the catalog is the catalog even when an agent asks for markdown", async () => {
  const response = await handle("https://zoomies.sh/.well-known/api-catalog", new Map([
    ["/.well-known/api-catalog.json", { type: "application/json", body: CATALOG }],
  ]), { headers: { accept: "text/markdown" } });

  assert.equal(response.status, 200);
  assert.equal(response.headers.get("content-type"), "application/linkset+json");
  assert.equal(await response.text(), CATALOG);
});

test("a HEAD for the catalog answers with the headers and no body", async () => {
  const response = await handle("https://zoomies.sh/.well-known/api-catalog", new Map([
    ["/.well-known/api-catalog.json", { type: "application/json", body: CATALOG }],
  ]), { headers: { accept: "application/linkset+json" }, method: "HEAD" });

  assert.equal(response.status, 200);
  assert.equal(response.headers.get("content-type"), "application/linkset+json");
  assert.equal(await response.text(), "");
});

test("an html answer advertises the catalog with a Link header", async () => {
  const response = await handle("https://zoomies.sh/", new Map([
    ["/", { type: "text/html; charset=utf-8", body: HTML }],
  ]));

  assert.equal(await response.text(), HTML);
  assert.equal(
    response.headers.get("link"),
    '<https://zoomies.sh/.well-known/api-catalog>; rel="api-catalog"',
  );
});

test("a response that is not html carries no Link header", async () => {
  const catalog = '{"version": 1}';
  const response = await handle("https://zoomies.sh/catalog.json", new Map([
    ["/catalog.json", { type: "application/json", body: catalog }],
  ]));

  assert.equal(await response.text(), catalog);
  assert.equal(response.headers.get("link"), null);
});

test("a Link header the origin already set is left alone", async () => {
  const existing = '<https://zoomies.sh/sitemap.xml>; rel="sitemap"';
  const response = await handle("https://zoomies.sh/", new Map([
    ["/", { type: "text/html; charset=utf-8", body: HTML, headers: { "content-type": "text/html; charset=utf-8", link: existing } }],
  ]));

  assert.equal(response.headers.get("link"), existing);
});
