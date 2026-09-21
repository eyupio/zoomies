"""The repository facts in the site's header: latest release, stars, forks.

Material fills these in the browser. On a page's first load its script asks the
GitHub API for the latest release and the repository's counts, then keeps the
answer in the tab's sessionStorage and never asks again -- and a browser
restores sessionStorage with the tab, so a tab first opened when V1.1.0 was the
newest release still said V1.1.0 two releases later. Nothing in this repository
was wrong, and nothing in it could put it right.

So the facts are resolved here, once per build, and rendered into the page by
overrides/partials/source.html, which leaves out the attribute Material's script
mounts on so it never runs against them. What a reader sees is what was true
when the site was built: the site workflow rebuilds on every published release
and once a day, so the release is minutes behind and a star count a day.

A build that cannot reach the API still builds, with the repository name and no
facts beside it, which is what Material renders when the API is unreachable: a
developer running ``mkdocs serve`` offline should not be stopped by it. The
site workflow checks the facts came out, so the published site cannot quietly
lose them.

Requests carry ``GITHUB_TOKEN`` when it is set. Anonymous requests share one
rate limit per address, and a shared runner's address can already have spent it.
"""

from __future__ import annotations

import json
import logging
import os
import re
import urllib.request

log = logging.getLogger("mkdocs.hooks.source")

_github = re.compile(r"^https://github\.com/([^/]+)/([^/]+?)/?$")


def _get(url: str) -> dict | None:
    headers = {"Accept": "application/vnd.github+json", "User-Agent": "zoomies.sh docs build"}
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"
    try:
        with urllib.request.urlopen(urllib.request.Request(url, headers=headers), timeout=10) as resp:
            return json.load(resp)
    except (OSError, ValueError) as err:
        # Info, not a warning: --strict turns a warning into a failed build, and
        # an offline build is allowed to succeed. The workflow is what refuses to
        # publish a site without the facts.
        log.info("%s: %s -- the header will name the repository and nothing else", url, err)
        return None


def _count(n: int) -> str:
    """Round the way Material's script does, so 1234 stars read as 1.2k here too."""
    if n < 1000:
        return str(n)
    digits = 1 if (n - 950) % 1000 > 99 else 0
    return f"{(n + 1e-6) / 1000:.{digits}f}k"


def facts(repo_url: str) -> dict[str, str]:
    """The facts for a GitHub repository URL, in the order the header shows them."""
    m = _github.match(repo_url or "")
    if not m:
        return {}
    owner, repo = m.groups()
    api = f"https://api.github.com/repos/{owner}/{repo}"
    out: dict[str, str] = {}
    # GitHub keeps drafts and prereleases out of /releases/latest, so this is
    # the release an operator is meant to run, not the newest tag.
    release = _get(f"{api}/releases/latest")
    if release and release.get("tag_name"):
        out["version"] = str(release["tag_name"])
    info = _get(api)
    if info:
        out["stars"] = _count(int(info.get("stargazers_count") or 0))
        out["forks"] = _count(int(info.get("forks_count") or 0))
    return out


def on_config(config):
    config["extra"]["source_facts"] = facts(config["repo_url"])
    return config
