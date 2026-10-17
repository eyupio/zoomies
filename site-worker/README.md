# The edge of zoomies.sh

A Cloudflare Worker that does what GitHub Pages cannot: read a request's Accept
header and set an answer's Content-Type.

* A request carrying `Accept: text/markdown` is answered with the page's
  Markdown copy, at `Content-Type: text/markdown`, with `Vary: Accept` and an
  `x-markdown-tokens` estimate. The copies are the ones `hooks/seo.py` writes
  beside the built site, at every page's address plus `index.md` (the address
  the pages already declare as their `rel="alternate"` link). Serving a copy is
  a lookup rather than an HTML-to-Markdown conversion, and what an agent
  receives is the source the page was written in.
* `/.well-known/api-catalog` (RFC 9727) is answered with the Linkset document
  tracked at `docs/.well-known/api-catalog.json` and re-typed to
  `application/linkset+json`, which Pages cannot serve for any extension.
* Every HTML answer carries `Link: </.well-known/api-catalog>; rel="api-catalog"`,
  how the RFC asks a site to advertise the catalog.

Everything else, browsers included, is passed through to GitHub Pages with its
body untouched.

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

The second command should say `text/markdown; charset=utf-8`. Browsers and
crawlers that send no such header keep receiving the HTML, its body byte for
byte and one added Link header. Two more things to check after deploying:

    curl -s https://zoomies.sh/.well-known/api-catalog | python3 -m json.tool
    curl -sI https://zoomies.sh/ | grep -i "^link"

The first should show the catalog's linkset; the second should show the
`rel="api-catalog"` header.
