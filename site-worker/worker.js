// The edge of zoomies.sh: the site and its content negotiation, in one worker.
//
// The site is the deployment's static assets (the directory the docs build
// writes, declared in wrangler.toml), and the worker runs first on every
// request: what it does not rewrite, it serves from the asset binding, so no
// request in the chain dials an origin server and no origin resolution can
// take a document dark. Every page's Markdown copy is built beside the page
// (hooks/seo.py writes one at the page's address plus index.md), so serving
// one is a lookup, not a conversion: an agent receives the source the page
// was written in, without the navigation, footer and inline icons the HTML
// carries.
//
// HTML stays the default. Only an explicit text/markdown entry in Accept gets
// Markdown; anything else, including a browser's */*, is served from the
// assets with its body untouched.
//
// Every page answer also carries a Link header (RFC 8288) with the relations
// an agent follows to the machine-readable resources: the API Catalog
// (RFC 9727), the plain-text index, and the REST API's description and
// documentation. Static assets cannot set arbitrary response headers, so the
// header is added here. So is Vary: Accept, on the HTML as on the Markdown,
// because the choice between them is made here.

const MARKDOWN_TYPE = "text/markdown";

// The OpenAPI document lives in the repository, not on this site: the API runs
// on each deployment, so the authoritative machine description is the one in
// the source tree it is generated from.
const OPENAPI_URL = "https://raw.githubusercontent.com/eyupio/zoomies/main/api/openapi.yaml";

// Addresses that once answered and now belong elsewhere. A page that is
// renamed or folded into another leaves its old address in search indexes and
// in other people's links, where it reports "not found" until a crawler gives
// up on it. A permanent redirect hands the old address's standing to the page
// that replaced it. Keys are pathnames without the trailing slash.
export const REDIRECTS = new Map([
  ["/ai-context-implementation-plan", "/ai-context/"],
]);

// The redirect for a pathname, or null when it has none. A trailing slash is
// ignored on the way in because the site is served both ways, and the target
// always carries the slash the built pages use.
export function redirectFor(pathname) {
  const key = pathname.length > 1 && pathname.endsWith("/") ? pathname.slice(0, -1) : pathname;
  return REDIRECTS.get(key) ?? null;
}

export function discoveryLinks() {
  return [
    '</.well-known/api-catalog>; rel="api-catalog"',
    `</llms.txt>; rel="describedby"`,
    `<${OPENAPI_URL}>; rel="service-desc"; type="application/yaml"`,
    '</api-surface/>; rel="service-doc"; type="text/html"',
  ].join(", ");
}

// An estimate, not a count: tokens are the model's unit and four characters is
// the usual order of magnitude for English prose. The header is advisory, and
// a close number is worth more than a tokenizer dependency at the edge.
export function tokenEstimate(text) {
  return Math.ceil(text.length / 4);
}

// Only an explicit text/markdown entry counts, and only when its q does not
// refuse it. A prefix test would match a browser's */* and take the choice of
// format away from the person reading the page.
export function wantsMarkdown(accept) {
  if (!accept) {
    return false;
  }
  for (const entry of accept.split(",")) {
    const [type, ...parameters] = entry.trim().split(";");
    if (type.trim().toLowerCase() !== MARKDOWN_TYPE) {
      continue;
    }
    for (const parameter of parameters) {
      const [name, value] = parameter.trim().split("=", 2);
      if (name.trim().toLowerCase() === "q" && Number(value) === 0) {
        return false;
      }
    }
    return true;
  }
  return false;
}

// The Markdown copy's address, or null where the URL is a file of its own
// kind: an image, the catalog's JSON, a script. Those are not pages, and no
// copy of them exists to find. Nor is a pathname that begins with two slashes
// (the URL parser also reads "/\" that way): resolved against the site it names
// another host.
export function markdownPath(pathname) {
  if (pathname.startsWith("//")) {
    return null;
  }
  if (pathname.endsWith("/")) {
    return pathname + "index.md";
  }
  const file = pathname.split("/").pop();
  if (file.includes(".")) {
    return pathname.endsWith(".html") ? pathname.replace(/\.html$/, ".md") : null;
  }
  return pathname + "/index.md";
}

// The address of a file on this site, or null when the path would leave it.
//
// The path comes from the request, so it is the requester's to choose, and
// "//host/p" resolved against an origin is a reference to host, not a path on
// the origin: new URL(path, origin) would send the worker to any server the
// requester names and have it answer with that server's reply. Concatenation
// cannot change the authority, and the parse that follows is the check that
// it did not.
function onThisSite(origin, path) {
  if (path.startsWith("//")) {
    return null;
  }
  const address = origin + path;
  return new URL(address).origin === origin ? address : null;
}

// The headers of a fetched answer that a response built here may carry. The
// body is re-sent as this site's own, so the cookies and policies of whatever
// answered the fetch are not the worker's to pass on; what is left is what lets
// a cache and a conditional request treat the copy as the file it is. Vary is
// among them because it only ever narrows what a cache may reuse: it names a
// request header, and carries nothing a client acts on. So is the origin's read
// grant: Pages sends access-control-allow-origin on every file, the page's HTML
// carries it, and a browser that may read a page may read its Markdown copy.
// The credentials grant is not relayed, because a wildcard origin and
// credentials do not combine and nothing here is read with a cookie.
const RELAYED_HEADERS = [
  "cache-control",
  "etag",
  "last-modified",
  "vary",
  "access-control-allow-origin",
];

function relayedHeaders(upstream) {
  const headers = new Headers();
  for (const name of RELAYED_HEADERS) {
    const value = upstream.headers.get(name);
    if (value !== null) {
      headers.set(name, value);
    }
  }
  return headers;
}

// The Markdown copy as a response, or null when this address has none, in
// which case the not-found page stands.
//
// Both variants of a page carry Vary: Accept, this one and the HTML the
// assets serve (see the handler below): the worker, not the asset pipeline,
// chooses the format, so a cache has to know that the choice depends on
// Accept. It is appended to what the assets send, Accept-Encoding, never put
// in its place.
async function markdownResponse(request, url, env) {
  const path = markdownPath(url.pathname);
  if (path === null) {
    return null;
  }
  const target = onThisSite(url.origin, path);
  if (target === null) {
    return null;
  }
  const copy = await env.ASSETS.fetch(new URL(target, url.origin));
  if (!copy.ok) {
    return null;
  }
  const body = await copy.text();
  const headers = relayedHeaders(copy);
  headers.set("content-type", MARKDOWN_TYPE + "; charset=utf-8");
  headers.append("vary", "Accept");
  headers.set("x-markdown-tokens", String(tokenEstimate(body)));
  headers.set("link", discoveryLinks());
  return new Response(body, { status: copy.status, headers });
}

// The API Catalog document (RFC 9727) at its well-known address. The assets
// store it as api-catalog.json, because a file's extension picks the content
// type and the type the RFC asks for is set here.
async function apiCatalogResponse(url, env) {
  const source = await env.ASSETS.fetch(new URL("/api-catalog.json", url.origin));
  if (!source.ok) {
    return null;
  }
  const body = await source.text();
  const headers = new Headers(source.headers);
  headers.set("content-type", 'application/linkset+json; profile="https://www.rfc-editor.org/info/rfc9727"');
  headers.set("link", discoveryLinks());
  return new Response(body, { status: source.status, headers });
}

// GitHub Pages cannot set response headers, so the browser-hardening set is
// added here, on every answer the zone gives: pages, the origin's own 404,
// the Markdown copies.
//
// HSTS is deliberately one year with no includeSubDomains and no preload.
// Preload is close to irreversible and would bind every name in this zone,
// the demo's included, to HTTPS-only for years; that is the owner's decision to
// take, not a default to ship quietly.
//
// The policy allows 'unsafe-inline' for scripts and styles because the theme
// writes inline scripts and the JSON-LD blocks into every page, and a static
// origin has no per-request nonce to give them. It is still tighter than no
// policy: apart from Cloudflare Web Analytics (its beacon script and the
// address it reports to, named below and in docs/privacy.md), no third-party
// origin may run script, load a font or be connected
// to, nothing may frame the site, and no plugin content or foreign base URL is
// accepted. It is sent as Report-Only until someone has browsed the built site
// with the console open and seen no violation; then the name below changes.
export const CSP_HEADER = "content-security-policy-report-only";

export const CONTENT_SECURITY_POLICY = [
  "default-src 'self'",
  "script-src 'self' 'unsafe-inline' https://static.cloudflareinsights.com",
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data: https:",
  "font-src 'self'",
  "connect-src 'self' https://cloudflareinsights.com",
  "manifest-src 'self'",
  "base-uri 'self'",
  "form-action 'self'",
  "frame-ancestors 'none'",
  "object-src 'none'",
].join("; ");

export function securityHeaders() {
  return {
    "strict-transport-security": "max-age=31536000",
    "x-content-type-options": "nosniff",
    "x-frame-options": "DENY",
    "referrer-policy": "strict-origin-when-cross-origin",
    "permissions-policy": "camera=(), microphone=(), geolocation=(), payment=()",
    [CSP_HEADER]: CONTENT_SECURITY_POLICY,
  };
}

// A response from fetch() has immutable headers, so it is rebuilt rather than
// edited. A header the origin or an earlier step already set is kept: the
// hardening fills gaps and never overrides a deliberate choice.
function withSecurityHeaders(response) {
  const rebuilt = new Response(response.body, response);
  for (const [name, value] of Object.entries(securityHeaders())) {
    if (!rebuilt.headers.has(name)) {
      rebuilt.headers.set(name, value);
    }
  }
  return rebuilt;
}

async function route(request, env) {
  if (request.method === "GET" || request.method === "HEAD") {
    const url = new URL(request.url);
    if (url.pathname === "/.well-known/api-catalog" || url.pathname === "/.well-known/api-catalog/") {
      const catalog = await apiCatalogResponse(url, env);
      if (catalog !== null) {
        return request.method === "HEAD" ? new Response(null, catalog) : catalog;
      }
    }
    const moved = redirectFor(url.pathname);
    if (moved !== null) {
      const target = new URL(moved, url);
      target.search = url.search;
      return new Response(null, { status: 301, headers: { location: target.href } });
    }
    if (wantsMarkdown(request.headers.get("accept"))) {
      const markdown = await markdownResponse(request, url, env);
      if (markdown !== null) {
        return request.method === "HEAD" ? new Response(null, markdown) : markdown;
      }
    }
    const asset = await env.ASSETS.fetch(request);
    // A page answer announces where the machine-readable resources are. The
    // markdownPath test is the worker's existing idea of what a page is: it
    // keeps install.sh and the images from wearing links that are not
    // about them. It is also what can be answered as Markdown, so it is the
    // HTML variant of a negotiated page and varies on Accept like the other.
    if (asset.ok && markdownPath(url.pathname) !== null) {
      const response = new Response(asset.body, asset);
      response.headers.set("link", discoveryLinks());
      response.headers.append("vary", "Accept");
      return response;
    }
    return asset;
  }
  // Methods the assets pipeline does not answer are answered by it anyway:
  // the binding serves what the deployment shipped, and nothing here is a
  // write.
  return env.ASSETS.fetch(request);
}

export default {
  async fetch(request, env) {
    return withSecurityHeaders(await route(request, env));
  },
};
