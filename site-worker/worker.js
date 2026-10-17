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
// origin with its body untouched, so a browser reads the same page it always
// did. The one thing the worker adds to an HTML answer is a Link header
// advertising the API catalog, which GitHub Pages has no way to set.

const MARKDOWN_TYPE = "text/markdown";

// RFC 9727: the well-known address of the site's API catalog, and the file it
// is published as. GitHub Pages serves the .json file as application/json and
// cannot be told otherwise, so the worker answers the well-known address
// itself, with the origin's body re-typed. The document is tracked at
// docs/.well-known/api-catalog.json, so the catalog cannot drift from the
// repository any more than the pages can.
const CATALOG_PATH = "/.well-known/api-catalog";
const CATALOG_SOURCE = "/.well-known/api-catalog.json";
const CATALOG_TYPE = "application/linkset+json";
const CATALOG_LINK = '<https://zoomies.sh/.well-known/api-catalog>; rel="api-catalog"';

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
  return new Response(body, { status: copy.status, headers });
}

// The catalog document re-typed, or null when the origin has no catalog, in
// which case the origin's answer (including a real 404) stands.
async function catalogResponse(url) {
  const source = await fetch(new URL(CATALOG_SOURCE, url.origin), {
    headers: { accept: "application/json" },
    redirect: "follow",
  });
  if (!source.ok) {
    return null;
  }
  const body = await source.text();
  const headers = new Headers(source.headers);
  headers.set("content-type", CATALOG_TYPE);
  return new Response(body, { status: source.status, headers });
}

// The catalog is a well-known address without an extension, which
// markdownPath would otherwise look for a copy of, so it is decided before
// the Accept header is read at all: it is JSON whichever format an agent asks
// the site for.
export default {
  async fetch(request) {
    if (request.method === "GET" || request.method === "HEAD") {
      const url = new URL(request.url);
      if (url.pathname === CATALOG_PATH) {
        const catalog = await catalogResponse(url);
        if (catalog !== null) {
          return request.method === "HEAD" ? new Response(null, catalog) : catalog;
        }
      } else if (wantsMarkdown(request.headers.get("accept"))) {
        const markdown = await markdownResponse(request, url);
        if (markdown !== null) {
          return request.method === "HEAD" ? new Response(null, markdown) : markdown;
        }
      }
      // A same-zone fetch from a Worker goes to the origin directly, so the
      // HTML answer reaches GitHub Pages exactly as it always did. The only
      // change is one header: RFC 9727 asks the site to advertise the catalog
      // with rel="api-catalog", and GitHub Pages cannot set Link headers.
      const origin = await fetch(request);
      const headers = new Headers(origin.headers);
      const type = headers.get("content-type") || "";
      if (type.startsWith("text/html") && !headers.has("link")) {
        headers.append("link", CATALOG_LINK);
      }
      return new Response(origin.body, { status: origin.status, statusText: origin.statusText, headers });
    }
    return fetch(request);
  },
};
