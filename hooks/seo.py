"""Build-time SEO metadata for zoomies.sh.

Two things the site wants that MkDocs does not know by itself, both derived
from the repository rather than written by hand, so neither can go stale:

* **A real last-modified date per page.** MkDocs' own sitemap stamps every URL
  with the build date, which tells a crawler that the whole site changed every
  time anything did -- exactly the signal that makes freshness meaningless. The
  date a page was last edited is in git, so it is taken from there and put on
  ``page.meta.git_date`` for ``overrides/sitemap.xml`` and the sharing tags.

* **``llms.txt``**, the plain-text index an assistant fetches when it is asked
  about a project instead of crawling it. It is generated from the navigation,
  so a page added to ``mkdocs.yml`` appears in it without anyone remembering.

* **An API catalog** (RFC 9727) at ``/.well-known/api-catalog``, a linkset in
  the ``application/linkset+json`` media type naming the OpenAPI
  specification and the reference page, with the specification itself
  published beside it at ``openapi.yaml``. GitHub Pages cannot set a content
  type, so the worker in ``site-worker/`` carries the media type the RFC
  names.

* **``/.well-known/security.txt``** (RFC 9116), where a researcher looks for how
  to report a vulnerability. It says what SECURITY.md says and points at it.
  ``Expires`` is required and must be under a year away, so a tracked file
  would quietly lapse; it is written at build, and the site is built on every
  push to main.

* **A Markdown copy of every page**, at the page's own address plus
  ``index.md``, and **``llms-full.txt``**, the readable pages in one file. The
  HTML is server-rendered and complete, but an assistant asked about a page
  pays for the navigation, the footer and 90 inline icons to read it, and
  GitHub Pages cannot answer ``Accept: text/markdown`` (the Cloudflare Worker
  in ``site-worker/`` answers it for the site, by serving these copies). The
  source is already Markdown, so the copy is the source with the parts only
  the theme understands taken out. ``llms-full.txt`` stops at a size an
  assistant can read in one go; the long reference pages are one link each
  rather than 185 KB in the middle.

This is a hook rather than a plugin because it needs nothing a plugin would
bring: the docs build stays at the three pinned dependencies in
``docs/requirements.txt``.

The git call is one process for the whole repository, not one per page. A
shallow checkout has no history to read, so the dates come back empty and every
page falls back to the build date -- which is why the site workflow checks out
with ``fetch-depth: 0``.
"""

from __future__ import annotations

import json
import logging
import os
import posixpath
import re
import shutil
import subprocess
from datetime import datetime, timedelta, timezone

log = logging.getLogger("mkdocs.hooks.seo")

# Populated once per build by on_files, keyed by path relative to docs_dir.
_dates: dict[str, str] = {}
# The resolved navigation, kept from on_nav so llms.txt can be written from it.
_nav = None
# Each page's Markdown as written, keyed by path relative to docs_dir, kept from
# on_page_markdown for the per-page copies and llms-full.txt.
_sources: dict[str, tuple[object, str]] = {}

# A page larger than this is not copied into llms-full.txt, only linked. The
# narrative pages (quickstart, the FAQ, the comparisons) are well under it; the
# generated references (every setting, every API route, every problem code)
# are several times over, and one of them would crowd out the rest.
_FULL_PAGE_LIMIT = 15_000

_comment = re.compile(r"<!--.*?-->", re.DOTALL)
_jsonld = re.compile(r"<script\b[^>]*>.*?</script>", re.DOTALL | re.IGNORECASE)
_snippet = re.compile(r"^--8<--.*\n?", re.MULTILINE)
_md_link = re.compile(r"(\]\()([^)\s#]+\.md)(#[^)\s]*)?(\))")
_only_light = re.compile(r"^!\[[^\n]*\]\([^)\n]*#only-light\)[^\n]*\n?", re.MULTILINE)
_image = re.compile(r"(!\[[^\]\n]*\]\()([^)\s#:]+)((?:#[^)\s]*)?\))")
_leading_h1 = re.compile(r"\A\s*# [^\n]*\n")


def _git_dates(docs_dir: str) -> dict[str, str]:
    """Map each file under docs_dir to the ISO date of its last commit."""
    cmd = [
        "git",
        "log",
        "--pretty=format:%x00%cI",
        "--name-only",
        "--no-renames",
        "--",
        ".",
    ]
    try:
        out = subprocess.run(
            cmd,
            cwd=docs_dir,
            capture_output=True,
            text=True,
            timeout=60,
            check=True,
        ).stdout
    except (OSError, subprocess.SubprocessError) as err:
        log.warning("no git history for the docs, so pages fall back to the build date: %s", err)
        return {}

    dates: dict[str, str] = {}
    current = ""
    for line in out.splitlines():
        if line.startswith("\x00"):
            # A commit header: everything until the next one was touched by it.
            current = line[1:11]
            continue
        path = line.strip()
        if not path:
            continue
        # git prints paths relative to the repository root even with a cwd, and
        # the newest commit touching a file is the first one seen.
        dates.setdefault(path, current)
    return dates


def on_files(files, config):
    """Read the history once, before any page is rendered."""
    global _dates
    root = os.path.relpath(config["docs_dir"], os.path.dirname(config["config_file_path"]))
    raw = _git_dates(config["docs_dir"])
    prefix = root.replace(os.sep, "/") + "/"
    _dates = {}
    for path, date in raw.items():
        key = path[len(prefix):] if path.startswith(prefix) else path
        _dates.setdefault(key, date)
    return files


def on_nav(nav, config, files):
    """Keep the resolved navigation for llms.txt."""
    global _nav
    _nav = nav
    return nav


def on_page_markdown(markdown, page, config, files):
    """Hand the page's own last-edited date to the templates.

    This runs while the pages are being read, which is before MkDocs renders
    the theme's static templates. sitemap.xml is one of those, and it reads
    ``page.meta.git_date`` for every page; set any later -- in
    ``on_page_context``, say -- the sitemap has already been written with the
    build date for every URL.
    """
    date = _dates.get(page.file.src_uri)
    if date:
        page.meta.setdefault("git_date", date)
    _sources[page.file.src_uri] = (page, markdown)
    return markdown


def _clean(markdown: str, src_uri: str, urls: dict[str, str]) -> str:
    """The page's Markdown without what only the theme understands.

    Comments are notes to a maintainer, the structured-data script is the FAQ
    saying its answers a second time, and an include line names a file in this
    repository. Links between pages are written as ``other.md`` so they work on
    GitHub and in the editor; here they become the page's address, because a
    copy read on its own has no neighbouring file to resolve them against.
    """
    text = _comment.sub("", markdown)
    text = _jsonld.sub("", text)
    text = _snippet.sub("", text)
    # Every screenshot is in the page twice, once for each colour scheme, and
    # only one is ever shown. A reader without a scheme needs just one of them.
    text = _only_light.sub("", text)

    def image(match: re.Match) -> str:
        path = posixpath.normpath(posixpath.join(posixpath.dirname(src_uri), match.group(2)))
        return f"{match.group(1)}{urls['']}{path}{match.group(3)}"

    text = _image.sub(image, text)

    def link(match: re.Match) -> str:
        target = posixpath.normpath(posixpath.join(posixpath.dirname(src_uri), match.group(2)))
        url = urls.get(target)
        if url is None:
            return match.group(0)
        return f"{match.group(1)}{url}{match.group(3) or ''}{match.group(4)}"

    text = _md_link.sub(link, text)
    return re.sub(r"\n{3,}", "\n\n", text).strip() + "\n"


def _documents(config) -> list[tuple[str, str, str, str]]:
    """(src_uri, canonical URL, title, Markdown) for every page, in navigation order.

    Pages the navigation does not name (the brand guide) follow it, so each page
    has a copy even when it is not a section of the site.
    """
    site_url = str(config["site_url"] or "").rstrip("/")
    urls = {src: f"{site_url}/{page.url}" for src, (page, _) in _sources.items()}
    # Not a page: the root, so an image path can be made absolute against it.
    urls[""] = f"{site_url}/"

    ordered: list[str] = []

    def walk(items):
        for item in items:
            if item.is_page:
                ordered.append(item.file.src_uri)
            elif item.is_section:
                walk(item.children)

    walk(_nav.items if _nav is not None else [])
    ordered += [src for src in _sources if src not in ordered]

    out = []
    for src in ordered:
        if src not in _sources:
            continue
        page, markdown = _sources[src]
        title = str(page.meta.get("title") or page.title)
        body = _leading_h1.sub("", _clean(markdown, src, urls), count=1).strip()
        out.append((src, urls[src], title, f"# {title}\n\nCanonical page: {urls[src]}\n\n{body}\n"))
    return out


def on_post_build(config):
    """Write llms.txt: the site's table of contents, in one plain-text file."""
    lines = [
        f"# {config['site_name']}",
        "",
        f"> {' '.join(str(config['site_description']).split())}",
        "",
        "Zoomies is free and open source under the AGPL-3.0. It is a single Go",
        "binary that watches a GitHub organisation or a repository on a personal",
        "account for queued Actions jobs. It starts one ephemeral runner per job",
        "on a machine you own and destroys it when the job finishes.",
        "SQLite for state, no Kubernetes and no database server,",
        "and a live web UI for the whole fleet. Runner images for Ubuntu, Debian,",
        "Fedora and Rocky Linux, on x86-64 and arm64. See the home page's",
        "qualification section for what has been tested and the current limits.",
        "",
        "## Documentation",
        "",
    ]

    site_url = str(config["site_url"] or "").rstrip("/")

    def walk(items):
        for item in items:
            if item.is_page:
                url = item.canonical_url or f"{site_url}/{item.url}"
                summary = " ".join(str(item.meta.get("description", "")).split())
                lines.append(f"- [{item.title}]({url})" + (f": {summary}" if summary else ""))
            elif item.is_section:
                lines.append("")
                lines.append(f"### {item.title}")
                lines.append("")
                walk(item.children)

    walk(_nav.items if _nav is not None else [])

    lines += [
        "",
        "## Source",
        "",
        f"- [Repository]({config['repo_url']}): the Go controller, the Svelte UI and these docs.",
        f"- [Full text]({site_url}/llms-full.txt): the readable pages in one file. Every page also has a Markdown copy at its address plus index.md, for example {site_url}/quickstart/index.md.",
        f"- [install.sh]({site_url}/install.sh): the one-line installer, byte-identical to the one in the repository.",
        f"- [install.ps1]({site_url}/install.ps1): the Windows installer, run from an elevated PowerShell window; byte-identical to the one in the repository.",
        f"- [OpenAPI document]({site_url}/openapi.yaml): the REST contract both clients are generated from.",
        f"- [API catalog]({site_url}/.well-known/api-catalog): where the APIs are listed for automated discovery, as an RFC 9727 linkset.",
        f"- [Catalog of problem codes and checks]({site_url}/catalog.json): every code Zoomies can raise, with what to do and how to see it worked, as JSON; its schema is beside it at catalog.schema.json.",
        "",
        f"Generated {datetime.now(timezone.utc).strftime('%Y-%m-%d')}.",
        "",
    ]

    with open(os.path.join(config["site_dir"], "llms.txt"), "w", encoding="utf-8") as fh:
        fh.write("\n".join(lines))

    _write_markdown(config, site_url)
    _write_security_txt(config, site_url)
    _write_api_catalog(config, site_url)

    # The catalog is generated into the Go package that embeds it, so the
    # binary and the site serve the same bytes; the site copies it rather than
    # keeping a second tracked file that could fall behind.
    catalog_dir = os.path.join(os.path.dirname(os.path.abspath(config["docs_dir"])), "internal", "catalog")
    for name, published in (("catalog.json", "catalog.json"), ("schema.json", "catalog.schema.json")):
        shutil.copyfile(os.path.join(catalog_dir, name), os.path.join(config["site_dir"], published))


def _write_markdown(config, site_url: str) -> None:
    """Write each page's Markdown copy and llms-full.txt into the built site."""
    documents = _documents(config)
    full = [
        f"# {config['site_name']}: the documentation in one file",
        "",
        "These are the pages of " + site_url + "/ as Markdown, in the order of its navigation.",
        "The index is " + site_url + "/llms.txt. A page too long to include here is listed at the end,",
        "with a link to its own Markdown copy.",
        "",
    ]
    long_pages = []
    for src, url, title, text in documents:
        page = _sources[src][0]
        target = os.path.join(config["site_dir"], page.url.replace("/", os.sep), "index.md")
        os.makedirs(os.path.dirname(target), exist_ok=True)
        with open(target, "w", encoding="utf-8") as fh:
            fh.write(text)
        if len(text) > _FULL_PAGE_LIMIT:
            long_pages.append((title, url))
        else:
            full += ["---", "", text]

    if long_pages:
        full += ["---", "", "## Pages too long to include here", ""]
        full += [f"- [{title}]({url}index.md)" for title, url in long_pages]
        full.append("")

    with open(os.path.join(config["site_dir"], "llms-full.txt"), "w", encoding="utf-8") as fh:
        fh.write("\n".join(full))


def _write_api_catalog(config, site_url: str) -> None:
    """Write the RFC 9727 API catalog and the OpenAPI specification it names.

    The linkset names, for the one API the project publishes, the contract a
    client implements against and the page a person reads. Both are pointed
    at this site rather than at GitHub, so an agent following the catalog
    stops here. The specification is copied verbatim from api/openapi.yaml,
    so the copy a client fetches is the one the controller's clients are
    generated from. The document is written twice: the worker answers the
    well-known path by fetching the .json twin, a URL of its own, and
    carrying the media type, which GitHub Pages cannot set.
    """
    root = os.path.dirname(os.path.abspath(config["docs_dir"]))
    shutil.copyfile(
        os.path.join(root, "api", "openapi.yaml"),
        os.path.join(config["site_dir"], "openapi.yaml"),
    )

    page = _sources.get("api-surface.md")
    surface = page[0].canonical_url if page is not None else f"{site_url}/api-surface/"
    document = {
        "linkset": [
            {
                "anchor": f"{site_url}/.well-known/api-catalog",
                "service-desc": [
                    {"href": f"{site_url}/openapi.yaml", "type": "application/yaml"},
                ],
                "service-doc": [
                    {"href": surface, "type": "text/html"},
                ],
            },
        ],
    }
    text = json.dumps(document, indent=2) + "\n"
    known = os.path.join(config["site_dir"], ".well-known")
    os.makedirs(known, exist_ok=True)
    for name in ("api-catalog", "api-catalog.json"):
        with open(os.path.join(known, name), "w", encoding="utf-8") as fh:
            fh.write(text)


def _write_security_txt(config, site_url: str) -> None:
    """Write /.well-known/security.txt, which says where to report a vulnerability."""
    repo = str(config["repo_url"]).rstrip("/")
    expires = (datetime.now(timezone.utc) + timedelta(days=330)).strftime("%Y-%m-%dT00:00:00.000Z")
    lines = [
        "# Report a vulnerability as a private security advisory, not a public issue.",
        f"Contact: {repo}/security/advisories/new",
        f"Expires: {expires}",
        "Preferred-Languages: en",
        f"Policy: {repo}/blob/main/SECURITY.md",
        f"Canonical: {site_url}/.well-known/security.txt",
        "",
    ]
    target = os.path.join(config["site_dir"], ".well-known")
    os.makedirs(target, exist_ok=True)
    with open(os.path.join(target, "security.txt"), "w", encoding="utf-8") as fh:
        fh.write("\n".join(lines))
