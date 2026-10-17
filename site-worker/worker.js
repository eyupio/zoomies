// The edge of zoomies.sh: content negotiation for agents.
//
// The site is served by GitHub Pages, which cannot read an Accept header, so a
// request that asks for text/markdown is answered here instead of at the
// origin. Every page's Markdown copy is built beside the site (hooks/seo.py
// writes one at the page's address plus index.md), so serving one is a lookup,
// not a conversion: an agent receives the source the page was written in,
// without the navigation, footer and inline icons the HTML carries.
//
// HTML stays the default. Only an explicit text/markdown entry in Accept gets
// Markdown; anything else, including a browser's */*, is passed through to the
// origin untouched.
//
// Every page answer also carries a Link header (RFC 8288) with the relations
// an agent follows to the machine-readable resources: the API Catalog
// (RFC 9727), the plain-text index, and the REST API's description and
// documentation. GitHub Pages cannot set arbitrary response headers, so the
// header is added here.

const MARKDOWN_TYPE = "text/markdown";

// The OpenAPI document lives in the repository, not on this site: the API runs
// on each deployment, so the authoritative machine description is the one in
// the source tree it is generated from.
const OPENAPI_URL = "https://raw.githubusercontent.com/eyupio/zoomies/main/api/openapi.yaml";

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
// copy of them exists to find.
export function markdownPath(pathname) {
  if (pathname.endsWith("/")) {
    return pathname + "index.md";
  }
  const file = pathname.split("/").pop();
  if (file.includes(".")) {
    return pathname.endsWith(".html") ? pathname.replace(/\.html$/, ".md") : null;
  }
  return pathname + "/index.md";
}

// The Markdown copy as a response, or null when this address has none, in
// which case the origin's answer (including a real 404) stands.
//
// The Vary header is set even though GitHub Pages does not: the worker, not
// the origin, chooses the format, so a cache in front of the origin has to
// know that the choice depends on Accept.
async function markdownResponse(request, url) {
  const path = markdownPath(url.pathname);
  if (path === null) {
    return null;
  }
  const copy = await fetch(new URL(path, url.origin), {
    headers: { accept: MARKDOWN_TYPE },
    redirect: "follow",
  });
  if (!copy.ok) {
    return null;
  }
  const body = await copy.text();
  const headers = new Headers(copy.headers);
  headers.set("content-type", MARKDOWN_TYPE + "; charset=utf-8");
  headers.set("vary", "Accept");
  headers.set("x-markdown-tokens", String(tokenEstimate(body)));
  headers.set("link", discoveryLinks());
  return new Response(body, { status: copy.status, headers });
}

// The API Catalog document (RFC 9727) at its well-known address. The origin
// stores it as api-catalog.json, because GitHub Pages picks a content type
// from the extension and would serve an extensionless file as
// application/octet-stream; the type the RFC asks for is set here.
async function apiCatalogResponse(url) {
  const source = await fetch(new URL("/api-catalog.json", url.origin), { redirect: "follow" });
  if (!source.ok) {
    return null;
  }
  const body = await source.text();
  const headers = new Headers(source.headers);
  headers.set("content-type", 'application/linkset+json; profile="https://www.rfc-editor.org/info/rfc9727"');
  headers.set("link", discoveryLinks());
  return new Response(body, { status: source.status, headers });
}

const ISSUER_ORIGIN = "https://portal.zoomies.sh";
const DEMO_ORIGIN = "https://demo.zoomies.sh";

// The discovery addresses the site mirrors, each from the party that owns
// the document. The mirror prefers the issuer's live answer, refetched on
// every request, so key rotation or an added endpoint is served the moment
// it exists. When a window leaves a provider unreachable, it falls back to
// the copy the docs build writes beside the site (hooks/seo.py takes each
// one from the same address and refuses a body that does not name what the
// product publishes), so discovery is one build stale rather than dark, and
// the mirror can be stale, never false. When neither answers, the origin's
// own answer (a real 404) stands, because the discovery addresses belong to
// their issuers first.
const MIRRORS = [
  {
    // Both discovery addresses are served from the portal's OpenID document;
    // RFC 8414 and OIDC metadata are the same document for this issuer.
    paths: [
      "/.well-known/openid-configuration",
      "/.well-known/oauth-authorization-server",
    ],
    live: "/.well-known/openid-configuration",
    upstream: ISSUER_ORIGIN,
    baked: "/.well-known/openid-configuration.json",
  },
  {
    // The demo serves the protected-resource document at both addresses.
    paths: [
      "/.well-known/oauth-protected-resource",
      "/.well-known/oauth-protected-resource/mcp",
    ],
    live: null,
    upstream: DEMO_ORIGIN,
    baked: "/.well-known/oauth-protected-resource.json",
  },
];

async function mirroredResponse(url) {
  const entry = MIRRORS.find((mirror) => mirror.paths.includes(url.pathname));
  if (entry === undefined) {
    return null;
  }
  const upstream = await fetch(new URL(entry.live ?? url.pathname, entry.upstream), {
    headers: { accept: "application/json" },
  });
  if (upstream.ok) {
    const body = await upstream.text();
    const headers = new Headers(upstream.headers);
    headers.set("content-type", "application/json");
    return new Response(body, { status: upstream.status, headers });
  }
  const baked = await fetch(new URL(entry.baked, url.origin), {
    headers: { accept: "application/json" },
  });
  if (baked.ok) {
    const body = await baked.text();
    const headers = new Headers(baked.headers);
    headers.set("content-type", "application/json");
    return new Response(body, { status: baked.status, headers });
  }
  return null;
}

export default {
  async fetch(request) {
    if (request.method === "GET" || request.method === "HEAD") {
      const url = new URL(request.url);
      if (MIRRORS.some((mirror) => mirror.paths.includes(url.pathname))) {
        const mirrored = await mirroredResponse(url);
        if (mirrored !== null) {
          return request.method === "HEAD" ? new Response(null, mirrored) : mirrored;
        }
      }
      if (url.pathname === "/.well-known/api-catalog" || url.pathname === "/.well-known/api-catalog/") {
        const catalog = await apiCatalogResponse(url);
        if (catalog !== null) {
          return request.method === "HEAD" ? new Response(null, catalog) : catalog;
        }
      }
      if (wantsMarkdown(request.headers.get("accept"))) {
        const markdown = await markdownResponse(request, url);
        if (markdown !== null) {
          return request.method === "HEAD" ? new Response(null, markdown) : markdown;
        }
      }
      const origin = await fetch(request);
      // A page answer announces where the machine-readable resources are. The
      // markdownPath test is the worker's existing idea of what a page is: it
      // keeps install.sh and the images from wearing links that are not
      // about them.
      if (origin.ok && markdownPath(url.pathname) !== null) {
        const response = new Response(origin.body, origin);
        response.headers.set("link", discoveryLinks());
        return response;
      }
      return origin;
    }
    // A same-zone fetch from a Worker goes to the origin directly, so the HTML
    // answer reaches GitHub Pages exactly as it always did.
    return fetch(request);
  },
};
