# The edge of zoomies.sh

A Cloudflare Worker that answers a request carrying `Accept: text/markdown`
with the page's Markdown copy, at `Content-Type: text/markdown`, with `Vary:
Accept` and an `x-markdown-tokens` estimate. Every other request, including a
browser's, is passed through to GitHub Pages untouched.

Two things the origin cannot do itself, the worker does on the way through:

* Every page answer carries a `Link` header (RFC 8288) with the relations an
  agent follows: `api-catalog` to the API Catalog (RFC 9727), `describedby` to
  `llms.txt`, and `service-desc` and `service-doc` to the REST API's OpenAPI
  document and its human-facing page.
* `/.well-known/api-catalog` is served from `api-catalog.json` at the origin,
  with the `application/linkset+json` content type the RFC asks for. GitHub
  Pages picks a content type from a file's extension and cannot set this one.
* The hosted service's OpenID Provider discovery document is mirrored at
  `/.well-known/openid-configuration` and `/.well-known/oauth-authorization-server`.
  The mirror prefers the issuer's live answer, refetched on each request, and
  falls back to the copy each site build takes from the same address
  (`hooks/seo.py`), so discovery is one build stale rather than dark when the
  provider cannot be reached. A copy whose issuer is not the portal's is
  refused by the build, so the mirror can be stale, never false.

The copies it serves are the ones `hooks/seo.py` writes beside the built site,
at every page's address plus `index.md` (the address the pages already declare
as their `rel="alternate"` link). Serving a copy is a lookup rather than an
HTML-to-Markdown conversion, and what an agent receives is the source the page
was written in.

## Why a worker

The origin is GitHub Pages, which cannot read an Accept header, so the choice
of format has to be made in front of it. The zone's nameservers are already
Cloudflare's. (A Pro-plan zone can get the same negotiation from Cloudflare's
own "Markdown for Agents" setting, which converts the origin's HTML instead of
serving the copies; this worker works on every plan.)

## Test

    node --test site-worker/worker.test.mjs

The tests run the handler against an in-memory origin, so nothing leaves the
machine. `.github/workflows/docs.yml` runs them on every site change.

## Deploy

1. In the Cloudflare dashboard, make sure the DNS records that serve
   zoomies.sh from GitHub Pages are proxied (orange cloud). A route on an
   unproxied record is never reached.
2. Create an API token with Workers edit rights for the zone.
3. Deploy:

        CLOUDFLARE_API_TOKEN=... npx wrangler deploy

## Verify

    curl -s https://zoomies.sh/quickstart/ \
      -H "Accept: text/markdown" | head -3
    curl -sI https://zoomies.sh/quickstart/ \
      -H "Accept: text/markdown" | grep -i "content-type"
    curl -sI https://zoomies.sh/ | grep -i "^link"
    curl -s https://zoomies.sh/.well-known/api-catalog
    curl -s https://zoomies.sh/.well-known/openid-configuration

The second command should say `text/markdown; charset=utf-8`. The third should
name all four relations (`api-catalog`, `describedby`, `service-desc` and
`service-doc`). The fourth should be the linkset JSON, the fifth the portal's
discovery document (its `issuer` must be `https://portal.zoomies.sh`).
Browsers and crawlers that send no markdown header keep receiving the HTML,
with only the `Link` header added.
