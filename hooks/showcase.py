"""Presentation data and content-versioned assets for the public docs site.

The existing SEO hook sees the complete Markdown first. Layout changes therefore
leave the canonical pages, edit dates and machine-readable copies intact.
"""

from __future__ import annotations

import hashlib
import math
import posixpath
import re

from mkdocs.structure.files import File

_h1 = re.compile(r"<h1\b[^>]*>.*?</h1>", re.S)
_pack = re.compile(r'<div class="pack-map-mermaid">(.*?)</div>', re.S)


def on_files(files, config):
    def version(path):
        # A serve rebuild can reuse the config containing the previous names.
        path = re.sub(r"\.[0-9a-f]{12}(?=\.(?:css|js)$)", "", str(path))
        file = files.get_file_from_path(path)
        if file is None:
            return path
        content = file.content_bytes
        stem, extension = posixpath.splitext(path)
        name = f"{stem}.{hashlib.sha256(content).hexdigest()[:12]}{extension}"
        files.append(File.generated(config, name, content=content))
        return name

    # Returning readers can have an old stylesheet cached when new HTML arrives.
    # The URL changes with the bytes, rather than relying on a manual refresh.
    config.extra_css = [version(path) for path in config.extra_css]
    scripts = []
    for script in config.extra_javascript:
        name = version(script)
        if isinstance(script, str):
            scripts.append(name)
        else:
            script.path = name
            scripts.append(script)
    config.extra_javascript = scripts
    config.extra["showcase_mermaid"] = version("vendor/mermaid/mermaid-11.12.0.min.js")
    return files


def on_page_markdown(markdown, page, config, files):
    page.meta["read_time"] = max(1, math.ceil(len(re.findall(r"\b[\w’'-]+\b", markdown)) / 220))
    if page.meta.get("layout") == "home":
        # The showcase replaces the introductory hero. The complete operating
        # notes and their existing anchors remain available below it.
        section = re.search(r"^## ", markdown, re.MULTILINE)
        if section:
            markdown = markdown[section.start():]
        markdown = markdown.split('<div class="zoomies-cta"', 1)[0]
    return markdown


def on_page_content(html, page, config, files):
    layout = page.meta.get("layout")
    if layout in {"article", "hub", "compare"}:
        match = _h1.search(html)
        if match:
            page.meta["heading_html"] = match.group(0)
            html = html[:match.start()] + html[match.end():]
    if layout == "home":
        match = _pack.search(html)
        if match:
            page.meta["pack_diagram"] = match.group(1)
            html = html[:match.start()] + html[match.end():]
    return html


def on_page_context(context, page, config, nav, **kwargs):
    pages = {item.file.src_uri: item for item in nav.pages}
    guides = []
    for item in config.extra.get("field_guides", []):
        target = pages.get(item["source"])
        if target is not None:
            guides.append({**item, "page": target, "number": f"{len(guides) + 1:02}"})
    context["field_guides"] = guides
    context["section"] = (
        "home" if page.is_homepage else
        "guides" if page.meta.get("layout") in {"article", "guides"} else
        "compare" if page.file.src_uri.startswith("alternatives/") or page.file.src_uri in {
            "actions-runner-controller.md", "hosted-runner-services.md",
        } else "docs"
    )
    return context
