#!/usr/bin/env python3
"""Check the built site's crawl contract, assets and local links without dependencies.

Pass --baseline with a previous build to check that its URLs, metadata and
content anchors survive a presentation change.
"""

from __future__ import annotations

import argparse
import gzip
import hashlib
import json
import re
import struct
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urldefrag, urljoin, urlparse
from xml.etree import ElementTree


class Document(HTMLParser):
    def __init__(self, text):
        super().__init__(convert_charrefs=True)
        self.title = ""
        self.titles = 0
        self.h1 = 0
        self.meta = {}
        self.links = []
        self.canonicals = []
        self.ids = set()
        self.content_ids = set()
        self.schemas = []
        self._title = False
        self._schema = None
        self.feed(text)

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if attrs.get("id"):
            self.ids.add(attrs["id"])
            if tag in {"h1", "h2", "h3", "h4", "h5", "h6", "span"} and not attrs["id"].startswith("__"):
                self.content_ids.add(attrs["id"])
        if tag == "h1":
            self.h1 += 1
        if tag == "title":
            self.titles += 1
            self._title = True
        if tag == "meta":
            key = attrs.get("property") or attrs.get("name")
            if key:
                self.meta.setdefault(key, []).append(attrs.get("content", ""))
        if tag == "link" and attrs.get("rel") == "canonical":
            self.canonicals.append(attrs.get("href", ""))
        if tag == "script" and attrs.get("type") == "application/ld+json":
            self._schema = ""
        for key in ["href", "src"]:
            if attrs.get(key):
                self.links.append(attrs[key])

    def handle_data(self, data):
        if self._title:
            self.title += data
        if self._schema is not None:
            self._schema += data

    def handle_endtag(self, tag):
        if tag == "title":
            self._title = False
        if tag == "script" and self._schema is not None:
            self.schemas.append(json.loads(self._schema))
            self._schema = None


def schema_types(value):
    if isinstance(value, dict):
        kind = value.get("@type")
        found = set(kind if isinstance(kind, list) else [kind] if kind else [])
        for child in value.values():
            found |= schema_types(child)
        return found
    if isinstance(value, list):
        return set().union(*(schema_types(child) for child in value))
    return set()


def check(site_dir, baseline=None):
    site_dir = site_dir.resolve()
    pages = {path.relative_to(site_dir).as_posix(): Document(path.read_text()) for path in site_dir.rglob("*.html")}
    base = pages["index.html"].canonicals[0]
    origin = urlparse(base)
    errors = []
    public_urls = set()
    titles = set()
    descriptions = set()
    checked_assets = set()

    def fail(path, message):
        errors.append(f"{path}: {message}")

    def local(url):
        parsed = urlparse(url)
        if parsed.scheme not in {"http", "https"} or parsed.netloc != origin.netloc or not parsed.path.startswith(origin.path):
            return None
        relative = unquote(parsed.path[len(origin.path):])
        if relative in {".well-known/api-catalog", ".well-known/api-catalog/"}:
            relative = "api-catalog.json"
        path = site_dir / relative
        if path.is_dir() or parsed.path.endswith("/"):
            path /= "index.html"
        return path

    for relative, page in pages.items():
        address = base + (relative[:-10] if relative.endswith("index.html") else relative)
        notfound = relative == "404.html"
        noindex = "noindex" in " ".join(page.meta.get("robots", []))
        if page.h1 != 1:
            fail(relative, f"expected one H1, got {page.h1}")
        if page.titles != 1 or not page.title.strip():
            fail(relative, "expected one nonempty title")
        required = ["robots", "description", "og:type", "og:locale", "og:site_name", "og:title", "og:description", "og:url", "og:image", "og:image:secure_url", "og:image:type", "og:image:width", "og:image:height", "og:image:alt", "twitter:card", "twitter:title", "twitter:description", "twitter:image", "twitter:image:alt"]
        for key in required:
            values = page.meta.get(key, [])
            if len(values) != 1 or not values[0].strip():
                fail(relative, f"expected one nonempty {key} tag")
        if not notfound and page.canonicals != [address]:
            fail(relative, f"canonical differs from {address}")
        if notfound and not noindex:
            fail(relative, "404 must be noindex")
        if not noindex and not notfound:
            public_urls.add(address)
            if page.title in titles:
                fail(relative, "duplicate public title")
            titles.add(page.title)
            description = page.meta.get("description", [""])[0]
            if description in descriptions:
                fail(relative, "duplicate public description")
            descriptions.add(description)
        for first, second in [("og:description", "description"), ("twitter:description", "description"), ("twitter:title", "og:title"), ("twitter:image", "og:image"), ("og:image:secure_url", "og:image"), ("twitter:image:alt", "og:image:alt")]:
            if page.meta.get(first) != page.meta.get(second):
                fail(relative, f"{first} and {second} disagree")
        if not notfound and page.meta.get("og:url") != [address]:
            fail(relative, "Open Graph URL differs from the canonical")
        if page.meta.get("twitter:card") != ["summary_large_image"] or page.meta.get("og:image:type") != ["image/png"]:
            fail(relative, "sharing card type is wrong")
        image = local(page.meta.get("og:image", [""])[0])
        if not image or not image.is_file():
            fail(relative, "social image is missing")
        else:
            data = image.read_bytes()
            if data[:8] != b"\x89PNG\r\n\x1a\n" or len(data) < 24:
                fail(relative, "social image is not a PNG")
            else:
                width, height = struct.unpack(">II", data[16:24])
                if (width, height) != (1200, 630) or page.meta.get("og:image:width") != [str(width)] or page.meta.get("og:image:height") != [str(height)]:
                    fail(relative, "social-image canvas and declared dimensions differ")
        kinds = schema_types(page.schemas)
        if not notfound:
            expected = {"SoftwareApplication", "WebSite", "Organization"} if relative == "index.html" else {"TechArticle", "BreadcrumbList"}
            if not expected <= kinds:
                fail(relative, f"structured data lost {expected - kinds}")
            copy = local(address + "index.md")
            if not copy or not copy.is_file() or "Canonical page: " + address not in copy.read_text():
                fail(relative, "canonical Markdown copy is missing")
        for raw in page.links:
            url, fragment = urldefrag(urljoin(address, raw))
            target = local(url)
            if target is None:
                continue
            if not target.is_file():
                fail(relative, f"local link or asset is missing: {raw}")
                continue
            name = target.relative_to(site_dir).as_posix()
            if fragment and name in pages and unquote(fragment) not in pages[name].ids:
                fail(relative, f"anchor is missing: {raw}")
            if name.startswith(("stylesheets/", "javascripts/", "vendor/")) and name.endswith((".css", ".js")):
                match = re.search(r"\.([0-9a-f]{12})\.(?:css|js)$", name)
                if not match:
                    fail(relative, f"site-owned code has no content version: {raw}")
                elif name not in checked_assets:
                    checked_assets.add(name)
                    if hashlib.sha256(target.read_bytes()).hexdigest()[:12] != match.group(1):
                        fail(relative, f"asset version differs from its content: {raw}")

    sitemap = (site_dir / "sitemap.xml").read_bytes()
    xml = ElementTree.fromstring(sitemap)
    ns = {"s": "http://www.sitemaps.org/schemas/sitemap/0.9"}
    locations = [node.text for node in xml.findall("s:url/s:loc", ns)]
    if len(locations) != len(set(locations)) or set(locations) != public_urls:
        fail("sitemap.xml", f"does not match public canonical pages: {set(locations) ^ public_urls}")
    for node in xml.findall("s:url/s:lastmod", ns):
        if not re.fullmatch(r"\d{4}-\d{2}-\d{2}", node.text or ""):
            fail("sitemap.xml", "lastmod is not an ISO date")
    if gzip.decompress((site_dir / "sitemap.xml.gz").read_bytes()) != sitemap:
        fail("sitemap.xml.gz", "compressed sitemap differs from the XML")
    if f"Sitemap: {base}sitemap.xml" not in (site_dir / "robots.txt").read_text():
        fail("robots.txt", "sitemap declaration is missing")
    search = json.loads((site_dir / "search/search_index.json").read_text())
    indexed = {urljoin(base, item["location"].split("#", 1)[0]) for item in search["docs"]}
    if not public_urls <= indexed:
        fail("search/search_index.json", f"public pages missing from search: {public_urls - indexed}")
    llms = (site_dir / "llms.txt").read_text() + (site_dir / "llms-full.txt").read_text()
    for url in public_urls:
        if url not in llms and not url.endswith("brand/BRANDING_GUIDE/"):
            fail("llms-full.txt", f"public page missing from the discovery files: {url}")
    for name in ["llms-full.txt", "auth.md", "api-catalog.json", ".well-known/security.txt", ".well-known/mcp/server-card.json", ".well-known/agent-skills/index.json", ".well-known/ai-catalog.json", ".well-known/did.json", ".well-known/poppy.json", ".well-known/agent-card.json"]:
        if not (site_dir / name).is_file():
            fail(name, "discovery file is missing")
    if "FAQPage" not in schema_types(pages["faq/index.html"].schemas):
        fail("faq/index.html", "FAQ structured data is missing")
    vendor = site_dir / "vendor/mermaid"
    provenance = (vendor / "SOURCE.txt").read_text()
    digest = hashlib.sha256((vendor / "mermaid-11.12.0.min.js").read_bytes()).hexdigest()
    if f"SHA-256: {digest}" not in provenance:
        fail("vendor/mermaid", "the local browser bundle differs from its recorded upstream digest")

    if baseline:
        old = baseline.resolve()
        for path in old.rglob("*.html"):
            name = path.relative_to(old).as_posix()
            previous = Document(path.read_text())
            current = pages.get(name)
            if current is None:
                fail(name, "an existing route was removed")
                continue
            if current.title != previous.title or current.canonicals != previous.canonicals:
                fail(name, "existing title or canonical changed")
            for key in ["description", "robots", "og:title", "og:description", "og:url", "og:image", "twitter:card"]:
                if current.meta.get(key) != previous.meta.get(key):
                    fail(name, f"existing {key} changed")
            if not previous.content_ids <= current.ids:
                fail(name, f"existing content anchors removed: {previous.content_ids - current.ids}")
    if errors:
        raise SystemExit("\n".join(errors))
    print(f"Site checks passed: {len(public_urls)} indexable pages; canonicals, metadata, sharing image, schema, sitemap/gzip, crawler files, Markdown copies, search, links and content-versioned assets.")
    if baseline:
        print("Existing URLs, titles, descriptions, robots directives, social metadata and content anchors match the baseline build.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("site_dir", nargs="?", type=Path, default=Path("site"))
    parser.add_argument("--baseline", type=Path)
    args = parser.parse_args()
    check(args.site_dir, args.baseline)
