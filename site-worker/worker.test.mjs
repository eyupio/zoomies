// The negotiation rules and the handler's decisions, against an in-memory
// asset binding. The Markdown copies and the catalog the worker serves are
// written by hooks/seo.py, so a change to their address or their shape fails
// here rather than at the edge. Plain node:test, so no dependency beyond the
// binary runs it.

import test from "node:test";
import assert from "node:assert/strict";

import worker, {
  CONTENT_SECURITY_POLICY,
  CSP_HEADER,
  discoveryLinks,
  markdownPath,
  REDIRECTS,
  redirectFor,
  securityHeaders,
  tokenEstimate,
  wantsMarkdown,
} from "./worker.js";

const SITE = "zoomies.sh";
const HTML = "<!doctype html><html><body><p>the page</p></body></html>";
const MARKDOWN = "# The page\n\nIts body as Markdown.\n";

// `seen` collects the address of every fetch the worker makes, so a test can
// say which hosts it was sent to as well as what it answered. Only fetches to
// other hosts run through here: everything on this site comes from the asset
// binding below.
//
// An entry may carry a `respond` function instead of a body, to behave the way
// a body cannot: it receives the fetch's init and returns (or throws) the
// answer, so a test can make an upstream refuse the connection or hang until
// the worker's signal gives up on it.
function originFor(paths, seen = []) {
  return async (input, init = {}) => {
    const url = new URL(typeof input === "string" ? input : input.url ?? input.href);
    seen.push(url.href);
    // Keys are "/path" for this site's origin, or "host/path" for any other
    // host. A bare path never answers for another host: a worker that fetched
    // the wrong host must find nothing there, not this site's file.
    const found =
      paths.get(url.host + url.pathname) ??
      (url.host === SITE ? paths.get(url.pathname) : undefined);
    if (found === undefined) {
      return new Response(HTML, {
        status: 404,
        headers: { "content-type": "text/html; charset=utf-8" },
      });
    }
    if (found.respond !== undefined) {
      return found.respond(init);
    }
    return new Response(found.body, {
      status: found.status ?? 200,
      headers: found.headers ?? { "content-type": found.type },
    });
  };
}

// The asset binding: what the deployment shipped, keyed by pathname; a path
// with no asset answers the not-found page the assets pipeline serves, at 404.
// Every ask is pushed into `seen` like a fetch, so a test can say what the
// worker went to.
function assetsFor(paths, seen = []) {
  return {
    ASSETS: {
      fetch: async (input) => {
        const url = new URL(typeof input === "string" ? input : input.url ?? input.href);
        seen.push(url.href);
        const found = paths.get(url.pathname);
        if (found === undefined) {
          return new Response(HTML, {
            status: 404,
            headers: { "content-type": "text/html; charset=utf-8" },
          });
        }
        if (found.respond !== undefined) {
          return found.respond({});
        }
        return new Response(found.body, {
          status: found.status ?? 200,
          headers: found.headers ?? { "content-type": found.type },
        });
      },
    },
  };
}

async function handle(url, paths, init = {}, seen = []) {
  const previous = globalThis.fetch;
  globalThis.fetch = originFor(new Map([...paths]), seen);
  try {
    return await worker.fetch(new Request(url, init), assetsFor(new Map([...paths]), seen));
  } finally {
    globalThis.fetch = previous;
  }
}

test("a request that asks for markdown gets the page's copy", async () => {
  const response = await handle(
    "https://zoomies.sh/quickstart/",
    new Map([
      [
        "/quickstart/index.md",
        { type: "text/markdown; charset=utf-8", body: MARKDOWN },
      ],
    ]),
    { headers: { accept: "text/markdown" } },
  );

  assert.equal(response.status, 200);
  assert.equal(
    response.headers.get("content-type"),
    "text/markdown; charset=utf-8",
  );
  assert.equal(response.headers.get("vary"), "Accept");
  assert.equal(
    response.headers.get("x-markdown-tokens"),
    String(Math.ceil(MARKDOWN.length / 4)),
  );
  assert.equal(await response.text(), MARKDOWN);
});

test("the site root's markdown copy is served for the bare host", async () => {
  const response = await handle(
    "https://zoomies.sh",
    new Map([
      ["/index.md", { type: "text/markdown; charset=utf-8", body: MARKDOWN }],
    ]),
    { headers: { accept: "text/markdown" } },
  );

  assert.equal(response.status, 200);
  assert.equal(await response.text(), MARKDOWN);
});

// The page's Markdown copy is in the origin here, so a worker that chose it for
// a browser would be seen doing so: with no copy to find, the same request
// would be answered with the HTML whatever the worker decided.
test("a browser's accept header gets the HTML the origin serves", async () => {
  const response = await handle(
    "https://zoomies.sh/quickstart/",
    new Map([
      ["/quickstart/", { type: "text/html; charset=utf-8", body: HTML }],
      [
        "/quickstart/index.md",
        { type: "text/markdown; charset=utf-8", body: MARKDOWN },
      ],
    ]),
    {
      headers: {
        accept:
          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
      },
    },
  );

  assert.equal(response.status, 200);
  assert.equal(
    response.headers.get("content-type"),
    "text/html; charset=utf-8",
  );
  assert.equal(await response.text(), HTML);
});

// The request has to carry the Accept header for this to test anything: the
// RequestInit is {headers: {accept}}, and a bare {accept} is ignored, which
// leaves a plain request that never reaches the copy's lookup at all.
test("an address with no markdown copy falls back to the origin's answer", async () => {
  const seen = [];
  const response = await handle(
    "https://zoomies.sh/not-a-page/",
    new Map([]),
    { headers: { accept: "text/markdown" } },
    seen,
  );

  assert.deepEqual(
    seen,
    [
      "https://zoomies.sh/not-a-page/index.md",
      "https://zoomies.sh/not-a-page/",
    ],
    "the worker should look for the copy, then fall back to the page itself",
  );
  assert.equal(response.status, 404);
  assert.equal(
    response.headers.get("content-type"),
    "text/html; charset=utf-8",
  );
  assert.equal(response.headers.get("x-markdown-tokens"), null);
  assert.equal(await response.text(), HTML);
});

// A copy sits where the worker would look if it took the JSON for a page, so
// a worker that did would answer with that and not the catalog.
test("a file that is a format of its own is never replaced with markdown", async () => {
  const catalog = '{"version": 1}';
  const response = await handle(
    "https://zoomies.sh/catalog.json",
    new Map([
      ["/catalog.json", { type: "application/json", body: catalog }],
      [
        "/catalog.json/index.md",
        { type: "text/markdown; charset=utf-8", body: MARKDOWN },
      ],
    ]),
    { headers: { accept: "text/markdown" } },
  );

  assert.equal(response.status, 200);
  assert.equal(response.headers.get("content-type"), "application/json");
  assert.equal(await response.text(), catalog);
});

test("a HEAD request for markdown answers with the headers and no body", async () => {
  const response = await handle(
    "https://zoomies.sh/quickstart/",
    new Map([
      [
        "/quickstart/index.md",
        { type: "text/markdown; charset=utf-8", body: MARKDOWN },
      ],
    ]),
    {
      headers: { accept: "text/markdown" },
      method: "HEAD",
    },
  );

  assert.equal(response.status, 200);
  assert.equal(
    response.headers.get("content-type"),
    "text/markdown; charset=utf-8",
  );
  assert.equal(await response.text(), "");
});

// A path that begins with two slashes (and "/\", which the URL parser reads the
// same way) is a reference to another host once it is resolved against the
// site: "//attacker.example/p/index.md" names attacker.example. The worker
// builds the address of a page's Markdown copy from the request path, so
// without a guard anyone could point it at a server of their own and have this
// site hand back that server's status, body and headers as zoomies.sh.
const ATTACKER = "attacker.example";
const ATTACKER_COPY = {
  body: "# owned\n",
  headers: {
    "content-type": "text/markdown",
    "set-cookie": "session=attacker; Domain=zoomies.sh; Path=/",
    "content-security-policy": "default-src *",
  },
};

for (const [name, address] of [
  ["two slashes", `https://zoomies.sh//${ATTACKER}/p/`],
  ["a slash and a backslash", `https://zoomies.sh/\\${ATTACKER}/p/`],
  ["three slashes", `https://zoomies.sh///${ATTACKER}/p/`],
  ["dot segments that collapse to two slashes", `https://zoomies.sh/a/..//${ATTACKER}/p/`],
  ["two slashes and no closing slash", `https://zoomies.sh//${ATTACKER}/p`],
  ["two slashes and an .html name", `https://zoomies.sh//${ATTACKER}/p.html`],
]) {
  test(`a request path of ${name} never sends the worker to another host`, async () => {
    const seen = [];
    const response = await handle(
      address,
      new Map([
        [`${ATTACKER}/p/index.md`, ATTACKER_COPY],
        [`${ATTACKER}/p.md`, ATTACKER_COPY],
      ]),
      { headers: { accept: "text/markdown" } },
      seen,
    );

    assert.deepEqual(
      seen.filter((href) => new URL(href).host !== SITE),
      [],
      "the worker fetched a host other than the site's",
    );
    // The origin's own answer for an address it does not have.
    assert.equal(response.status, 404);
    assert.equal(response.headers.get("set-cookie"), null);
    assert.notEqual(await response.text(), ATTACKER_COPY.body);
  });
}

// Even from the site's own origin, a copy is relayed with a short list of
// headers rather than all of them: a cookie, a content security policy or a
// credentials grant that came from whatever answered the fetch must never be
// set on zoomies.sh by this worker. The origin's read grant is on the list, so
// the Markdown copy stays readable from a browser as the page is.
test("a markdown copy is answered with only the headers the worker means to relay", async () => {
  const response = await handle(
    "https://zoomies.sh/quickstart/",
    new Map([
      [
        "/quickstart/index.md",
        {
          body: MARKDOWN,
          headers: {
            "content-type": "text/markdown; charset=utf-8",
            "cache-control": "max-age=600",
            etag: '"5f3a"',
            "last-modified": "Tue, 13 Oct 2026 09:00:00 GMT",
            "set-cookie": "session=1; Domain=zoomies.sh; Path=/",
            "content-security-policy": "default-src 'none'",
            "access-control-allow-origin": "*",
            "access-control-allow-credentials": "true",
            "x-served-by": "cache-lhr-1",
          },
        },
      ],
    ]),
    { headers: { accept: "text/markdown" } },
  );

  assert.equal(response.status, 200);
  assert.equal(response.headers.get("cache-control"), "max-age=600");
  assert.equal(response.headers.get("etag"), '"5f3a"');
  assert.equal(
    response.headers.get("last-modified"),
    "Tue, 13 Oct 2026 09:00:00 GMT",
  );
  assert.equal(response.headers.get("access-control-allow-origin"), "*");
  for (const name of [
    "set-cookie",
    "content-security-policy",
    "access-control-allow-credentials",
    "x-served-by",
  ]) {
    assert.equal(response.headers.get(name), null, `${name} was relayed`);
  }
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
  // Not a path on this site at all: resolved against it, it names another host.
  assert.equal(markdownPath("//attacker.example/p/"), null);
  assert.equal(markdownPath("///attacker.example/p"), null);
});

test("the token estimate rounds up the parts of a token", () => {
  assert.equal(tokenEstimate(""), 0);
  assert.equal(tokenEstimate("abcd"), 1);
  assert.equal(tokenEstimate("abcde"), 2);
});

test("the discovery links name all four relations a scanner looks for", () => {
  const links = discoveryLinks();
  for (const relation of [
    "api-catalog",
    "describedby",
    "service-desc",
    "service-doc",
  ]) {
    assert.match(links, new RegExp(`rel="${relation}"`), relation);
  }
});

test("an HTML page answer carries the discovery links", async () => {
  const response = await handle(
    "https://zoomies.sh/",
    new Map([["/", { type: "text/html; charset=utf-8", body: HTML }]]),
    { headers: { accept: "text/html" } },
  );

  assert.equal(response.status, 200);
  assert.match(response.headers.get("link"), /rel="api-catalog"/);
  assert.match(response.headers.get("link"), /rel="service-desc"/);
});

test("a markdown copy carries the discovery links too", async () => {
  const response = await handle(
    "https://zoomies.sh/",
    new Map([
      ["/index.md", { type: "text/markdown; charset=utf-8", body: MARKDOWN }],
    ]),
    { headers: { accept: "text/markdown" } },
  );

  assert.match(response.headers.get("link"), /rel="describedby"/);
});

// The worker, not the origin, picks the format of a page from its Accept
// header, so a cache between the worker and the reader must key the answer on
// Accept for both variants, and must go on keying it on the encoding the
// origin already varies on.
function varyTokens(response) {
  return (response.headers.get("vary") ?? "")
    .split(",")
    .map((token) => token.trim().toLowerCase())
    .filter(Boolean)
    .sort();
}

test("both variants of a page vary on Accept and keep the origin's Accept-Encoding", async () => {
  const origin = new Map([
    [
      "/quickstart/",
      {
        body: HTML,
        headers: { "content-type": "text/html; charset=utf-8", vary: "Accept-Encoding" },
      },
    ],
    [
      "/quickstart/index.md",
      {
        body: MARKDOWN,
        headers: { "content-type": "text/markdown; charset=utf-8", vary: "Accept-Encoding" },
      },
    ],
  ]);

  const html = await handle("https://zoomies.sh/quickstart/", origin, {
    headers: { accept: "text/html" },
  });
  const markdown = await handle("https://zoomies.sh/quickstart/", origin, {
    headers: { accept: "text/markdown" },
  });

  assert.equal(await html.text(), HTML);
  assert.equal(await markdown.text(), MARKDOWN);
  assert.deepEqual(varyTokens(html), ["accept", "accept-encoding"], "the HTML variant");
  assert.deepEqual(varyTokens(markdown), ["accept", "accept-encoding"], "the Markdown variant");
});

test("a page whose origin sends no Vary still varies on Accept in both variants", async () => {
  const origin = new Map([
    ["/quickstart/", { type: "text/html; charset=utf-8", body: HTML }],
    ["/quickstart/index.md", { type: "text/markdown; charset=utf-8", body: MARKDOWN }],
  ]);

  for (const accept of ["text/html", "text/markdown"]) {
    const response = await handle("https://zoomies.sh/quickstart/", origin, {
      headers: { accept },
    });
    assert.deepEqual(varyTokens(response), ["accept"], accept);
  }
});

test("a file that is not a page does not vary on Accept", async () => {
  const response = await handle(
    "https://zoomies.sh/install.sh",
    new Map([["/install.sh", { type: "text/x-shellscript", body: "#!/bin/sh\n" }]]),
    { headers: { accept: "text/markdown" } },
  );

  assert.equal(response.headers.get("vary"), null);
});

test("a file that is not a page wears no discovery links", async () => {
  const response = await handle(
    "https://zoomies.sh/install.sh",
    new Map([
      ["/install.sh", { type: "text/x-shellscript", body: "#!/bin/sh\n" }],
    ]),
    {},
  );

  assert.equal(response.headers.get("link"), null);
});

test("the well-known api-catalog address serves the RFC 9727 document", async () => {
  const catalog = '{"linkset": []}';
  const response = await handle(
    "https://zoomies.sh/.well-known/api-catalog",
    new Map([
      ["/api-catalog.json", { type: "application/json", body: catalog }],
    ]),
    {},
  );

  assert.equal(response.status, 200);
  assert.equal(
    response.headers.get("content-type"),
    'application/linkset+json; profile="https://www.rfc-editor.org/info/rfc9727"',
  );
  assert.match(response.headers.get("link"), /rel="api-catalog"/);
  assert.equal(await response.text(), catalog);
});

test("a HEAD request for the api-catalog answers with headers and no body", async () => {
  const catalog = '{"linkset": []}';
  const response = await handle(
    "https://zoomies.sh/.well-known/api-catalog",
    new Map([
      ["/api-catalog.json", { type: "application/json", body: catalog }],
    ]),
    { method: "HEAD" },
  );

  assert.equal(response.status, 200);
  assert.equal(await response.text(), "");
});

test("the site serves no authorization-server or resource metadata of its own", async () => {
  // The site is documentation. A deployment's identity documents live at the
  // deployment's address, and the site declares no authorization server.
  for (const path of [
    "/.well-known/openid-configuration",
    "/.well-known/oauth-authorization-server",
    "/.well-known/oauth-protected-resource",
  ]) {
    const response = await handle("https://zoomies.sh" + path, new Map([]), {});
    assert.equal(response.status, 404);
    assert.equal(response.headers.get("content-type"), "text/html; charset=utf-8");
  }
});

test("every kind of answer carries the browser-hardening headers", async () => {
  const origin = new Map([
    ["/quickstart/", { type: "text/html; charset=utf-8", body: HTML }],
    ["/quickstart/index.md", { type: "text/markdown; charset=utf-8", body: MARKDOWN }],
  ]);
  const answers = [
    await handle("https://zoomies.sh/quickstart/", origin),
    await handle("https://zoomies.sh/not-a-page/", origin),
    await handle("https://zoomies.sh/quickstart/", origin, { headers: { accept: "text/markdown" } }),
  ];

  for (const response of answers) {
    for (const [name, value] of Object.entries(securityHeaders())) {
      assert.equal(response.headers.get(name), value, name);
    }
  }
});

test("the content security policy forbids framing, plugins and foreign base URLs", () => {
  assert.match(CONTENT_SECURITY_POLICY, /frame-ancestors 'none'/);
  assert.match(CONTENT_SECURITY_POLICY, /object-src 'none'/);
  assert.match(CONTENT_SECURITY_POLICY, /base-uri 'self'/);
  // Cloudflare Web Analytics is the only third-party origin the policy names.
  // Any other address is a decision for the owner and for docs/privacy.md, so
  // it fails here until this list is edited on purpose.
  const origins = CONTENT_SECURITY_POLICY.match(/https?:\/\/[a-z0-9.-]+/g) ?? [];
  assert.deepEqual(origins.sort(), [
    "https://cloudflareinsights.com",
    "https://static.cloudflareinsights.com",
  ]);
});

test("HSTS does not preload or cover subdomains until the owner decides it should", () => {
  const value = securityHeaders()["strict-transport-security"];
  assert.doesNotMatch(value, /preload|includeSubDomains/i);
});

test("a header the origin set deliberately is not overwritten", async () => {
  const response = await handle(
    "https://zoomies.sh/quickstart/",
    new Map([
      ["/quickstart/", { body: HTML, headers: { "content-type": "text/html", "x-frame-options": "SAMEORIGIN" } }],
    ]),
  );

  assert.equal(response.headers.get("x-frame-options"), "SAMEORIGIN");
  assert.equal(response.headers.get(CSP_HEADER), CONTENT_SECURITY_POLICY);
});

test("a page that moved answers with a permanent redirect to its replacement", async () => {
  for (const path of ["/ai-context-implementation-plan/", "/ai-context-implementation-plan"]) {
    const response = await handle(`https://zoomies.sh${path}`, new Map());
    assert.equal(response.status, 301);
    assert.equal(response.headers.get("location"), "https://zoomies.sh/ai-context/");
  }
});

test("a redirect keeps the query and is never taken for a page that exists", async () => {
  const response = await handle(
    "https://zoomies.sh/ai-context-implementation-plan/?ref=x",
    new Map(),
  );
  assert.equal(response.headers.get("location"), "https://zoomies.sh/ai-context/?ref=x");
  assert.equal(redirectFor("/ai-context/"), null);
  assert.equal(redirectFor("/"), null);
});

test("every redirect lands on a page address with a trailing slash", () => {
  for (const [from, to] of REDIRECTS) {
    assert.ok(!from.endsWith("/"), `${from} is keyed without a trailing slash`);
    assert.ok(to.startsWith("/") && to.endsWith("/"), `${to} is an absolute page address`);
  }
});
