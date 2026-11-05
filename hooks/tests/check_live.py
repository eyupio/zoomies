#!/usr/bin/env python3
"""Verify the published site against the build that was just deployed."""

from __future__ import annotations

import gzip
import hashlib
import os
import re
import time
import urllib.request
from pathlib import Path
from urllib.parse import urljoin
from xml.etree import ElementTree

from check_site import Document


def main():
    root = Path("site")
    origin = Document((root / "index.html").read_text()).canonicals[0]
    stamp = os.environ.get("GITHUB_SHA", "site-check")

    def fetch(path):
        url = urljoin(origin, path) + "?build=" + stamp
        request = urllib.request.Request(url, headers={"User-Agent": "zoomies.sh deployment check", "Accept": "text/html, application/xml, */*"})
        for attempt in range(3):
            try:
                with urllib.request.urlopen(request, timeout=15) as response:
                    return response.read()
            except OSError:
                if attempt == 2:
                    raise
                time.sleep(2)

    paths = ["", "start-here/", "guides/", "runners-without-kubernetes/", "quickstart/", "alternatives/", "faq/"]
    assets = set()
    for path in paths:
        local = Document((root / path / "index.html").read_text())
        remote = Document(fetch(path).decode())
        assert remote.title == local.title, f"{path}: published title differs"
        assert remote.canonicals == local.canonicals, f"{path}: published canonical differs"
        assert remote.h1 == 1, f"{path}: published page has {remote.h1} H1 headings"
        for key in ["description", "robots", "og:title", "og:description", "og:url", "og:image", "twitter:card"]:
            assert remote.meta.get(key) == local.meta.get(key), f"{path}: published {key} differs"
        assert remote.schemas == local.schemas, f"{path}: published structured data differs"
        # A page at a nested path uses relative asset URLs. Resolve those
        # against its canonical, then bring them back to the site's root.
        for link in remote.links:
            absolute = urljoin(origin + path, link)
            if re.search(r"\.[0-9a-f]{12}\.(css|js)$", absolute) and absolute.startswith(origin):
                assets.add(absolute[len(origin):])
    for path in assets:
        assert hashlib.sha256(fetch(path)).digest() == hashlib.sha256((root / path).read_bytes()).digest(), f"{path}: published asset differs"
    sitemap = fetch("sitemap.xml")
    ns = "{http://www.sitemaps.org/schemas/sitemap/0.9}loc"
    expected = {node.text for node in ElementTree.fromstring((root / "sitemap.xml").read_bytes()).iter(ns)}
    actual = {node.text for node in ElementTree.fromstring(sitemap).iter(ns)}
    assert actual == expected, "published sitemap differs from the built canonical pages"
    assert gzip.decompress(fetch("sitemap.xml.gz")) == sitemap, "published compressed sitemap differs"
    assert f"Sitemap: {origin}sitemap.xml" in fetch("robots.txt").decode(), "published robots.txt has no sitemap declaration"
    assert fetch("brand/social-card.png") == (root / "brand/social-card.png").read_bytes(), "published sharing image differs"
    print(f"Live site checks passed: {len(paths)} representative pages, {len(assets)} content-versioned assets, {len(actual)} sitemap URLs, robots.txt, compressed sitemap and social image at {origin}")


if __name__ == "__main__":
    main()
