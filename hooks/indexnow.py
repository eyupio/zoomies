"""Which pages to tell search engines about after a deploy, as an IndexNow payload.

This is not an MkDocs hook -- mkdocs.yml does not register it -- but it lives
beside them because it reads what they write: the sitemap overrides/sitemap.xml
renders, where every URL carries the date its page was last edited in git.

A crawler finds a changed page on its own eventually, through that sitemap.
IndexNow (https://www.indexnow.org) is the way to say so the moment it ships:
one POST, and Bing, Yandex, Seznam, Naver and the rest are told which URLs
changed. Google does not take part; it reads the sitemap.

It submits only what changed. The live sitemap, fetched before the new site
replaces it, is the record of what search engines were last shown; a URL that is
new, or whose last-edited date moved, goes in the payload, and nothing else
does. Resubmitting every page on every daily rebuild would be noise the
protocol asks senders not to make.

The key is not a secret. The protocol proves ownership by the key file being
served at the site root -- docs/<key>.txt, published with everything else -- so
anyone can read it, and a payload naming it is only believed for this host.

    python hooks/indexnow.py site/sitemap.xml https://zoomies.sh/sitemap.xml KEY

prints the payload, and writes it to $GITHUB_OUTPUT as `payload` when that is
set. An empty payload means there is nothing to submit. It never fails the
build: a live sitemap that cannot be fetched means nothing is submitted, and
says why.
"""

from __future__ import annotations

import json
import os
import sys
import urllib.request
import xml.etree.ElementTree as ET
from urllib.parse import urlparse

NS = {"sm": "http://www.sitemaps.org/schemas/sitemap/0.9"}
# The protocol's own ceiling for one request.
LIMIT = 10_000


def entries(xml: str) -> dict[str, str]:
    """Every <loc> in a sitemap, mapped to its <lastmod> (empty if it has none)."""
    root = ET.fromstring(xml)
    out = {}
    for url in root.findall("sm:url", NS):
        loc = (url.findtext("sm:loc", default="", namespaces=NS) or "").strip()
        if loc:
            out[loc] = (url.findtext("sm:lastmod", default="", namespaces=NS) or "").strip()
    return out


def changed(built: dict[str, str], live: dict[str, str]) -> list[str]:
    """The URLs that are new, or whose last-edited date differs from the live site's."""
    return sorted(loc for loc, date in built.items() if live.get(loc) != date)


def payload(urls: list[str], key: str) -> str:
    if not urls:
        return ""
    host = urlparse(urls[0]).netloc
    return json.dumps(
        {
            "host": host,
            "key": key,
            "keyLocation": f"https://{host}/{key}.txt",
            "urlList": urls[:LIMIT],
        },
        separators=(",", ":"),
    )


def main(argv: list[str]) -> int:
    if len(argv) != 4:
        print(__doc__, file=sys.stderr)
        return 2
    built_path, live_url, key = argv[1:]
    with open(built_path, encoding="utf-8") as f:
        built = entries(f.read())
    try:
        with urllib.request.urlopen(live_url, timeout=30) as resp:
            live = entries(resp.read().decode("utf-8"))
    except Exception as err:  # noqa: BLE001 -- any failure means "submit nothing"
        print(f"indexnow: could not read the live sitemap ({err}); submitting nothing")
        live = None
    urls = [] if live is None else changed(built, live)
    body = payload(urls, key)
    print(f"indexnow: {len(urls)} changed URL(s)")
    for url in urls:
        print(f"  {url}")
    out = os.environ.get("GITHUB_OUTPUT")
    if out:
        with open(out, "a", encoding="utf-8") as f:
            f.write(f"payload={body}\n")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
