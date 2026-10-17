# The edge of zoomies.sh

A Cloudflare Worker that answers a request carrying `Accept: text/markdown`
with the page's Markdown copy, at `Content-Type: text/markdown`, with `Vary:
Accept` and an `x-markdown-tokens` estimate. It also serves the API catalog
(RFC 9727) at `/.well-known/api-catalog` as `application/linkset+json`, with
the `api-catalog` Link header, and adds that Link header to the HTML and
Markdown pages it passes through, so any page names where the APIs are
listed. GitHub Pages cannot set a custom content type, which is why the
catalog's media type is carried here. Every other request, including a
browser's, is passed through to GitHub Pages untouched.

The copies it serves are the ones `hooks/seo.py` writes beside the built site,
at every page's address plus `index.md` (the address the pages already declare
as their `rel="alternate"` link) and the catalog beside the specification it
names. Serving a copy is a lookup rather than an HTML-to-Markdown conversion,
and what an agent receives is the source the page was written in.

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
    curl -sI https://zoomies.sh/.well-known/api-catalog | grep -i "content-type"

The second command should say `text/markdown; charset=utf-8`, the fourth
`application/linkset+json`. The third names the catalog, the specification,
the reference page and the page's own Markdown. Browsers and crawlers that
send no such header keep receiving the HTML, byte for byte.
