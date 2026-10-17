// The edge of zoomies.sh: content negotiation for agents.
//
// The site is served by GitHub Pages, which cannot read an Accept header, so a
// request that asks for text/markdown is answered here instead of at the
// origin. Every page's Markdown copy is built beside the site (hooks/seo.py
// writes one at the page's address plus index.md), so serving one is a lookup,
// not a conversion: an agent receives the source the page was written in,
// without the navigation, footer and inline icons the HTML carries. The
// worker also serves the API catalog (RFC 9727) at the well-known path with
// the media type the RFC names, which Pages cannot set, and names that
// catalog in a Link header on every page it passes through.
//
// HTML stays the default. Only an explicit text/markdown entry in Accept gets
// Markdown; anything else, including a browser's */*, is passed through to the
// origin untouched.

const MARKDOWN_TYPE = "text/markdown";
const CATALOG_PATH = "/.well-known/api-catalog";
// The media type RFC 9727 requires, with the profile URI it registers, so a
// client can tell an API catalog from any other linkset.
const CATALOG_TYPE = 'application/linkset+json; profile="https://www.rfc-editor.org/info/rfc9727"';
// The API's machine-readable contract, and the page a person reads it on,
// both published by the build beside the catalog.
const SPEC_PATH = "/openapi.yaml";
const DOCS_PATH = "/api-surface/";

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

// The Link header that points a page's reader at the catalog (RFC 9727,
// Section 3): an agent that never saw robots.txt still finds the API from any
// page it lands on.
function catalogLink(url) {
  return `<${url.origin}${CATALOG_PATH}>; rel="api-catalog"`;
}

// The Link header a page's reader follows (RFC 8288), with the relations
// RFC 9727 and RFC 8631 register: where the APIs are catalogued, the contract
// a client implements against, the documentation a person reads. The page's
// own description (RFC 6892's describedby) is the Markdown copy the build
// writes beside every page, and only a page the origin actually served
// should name it: the Pages 404 is HTML with no copy of its own.
function pageLinks(url, served) {
  const links = [
    catalogLink(url),
    `<${url.origin}${SPEC_PATH}>; rel="service-desc"`,
    `<${url.origin}${DOCS_PATH}>; rel="service-doc"`,
  ];
  if (served) {
    const describedby = markdownPath(url.pathname);
    if (describedby !== null) {
      links.push(`<${url.origin}${describedby}>; rel="describedby"`);
    }
  }
  return links.join(", ");
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
  headers.append("link", pageLinks(url, false));
  return new Response(body, { status: copy.status, headers });
}

// The API catalog as a response, or null where the built site has none, in
// which case the origin's answer (including a real 404) stands. The fetch
// aims at the .json twin the build writes, a URL of its own: fetching the
// well-known path itself would ask this route the same question the incoming
// request just asked.
async function catalogResponse(url) {
  const copy = await fetch(new URL(CATALOG_PATH + ".json", url.origin), {
    headers: { accept: "application/linkset+json" },
    redirect: "follow",
  });
  if (!copy.ok) {
    return null;
  }
  const body = await copy.text();
  const headers = new Headers(copy.headers);
  headers.set("content-type", CATALOG_TYPE);
  headers.append("link", catalogLink(url));
  return new Response(body, { status: copy.status, headers });
}

export default {
  async fetch(request) {
    if (request.method === "GET" || request.method === "HEAD") {
      const url = new URL(request.url);
      if (url.pathname === CATALOG_PATH) {
        const catalog = await catalogResponse(url);
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
    }
    // A same-zone fetch from a Worker goes to the origin directly, so the
    // HTML answer reaches GitHub Pages exactly as it always did.
    const response = await fetch(request);
    if ((response.headers.get("content-type") ?? "").includes("text/html")) {
      // A page names where the APIs are catalogued: the link relation is how
      // discovery works for a client that only ever saw one page.
      const headers = new Headers(response.headers);
      headers.append("link", pageLinks(new URL(request.url), response.ok));
      return new Response(response.body, {
        status: response.status,
        statusText: response.statusText,
        headers,
      });
    }
    return response;
  },
};
