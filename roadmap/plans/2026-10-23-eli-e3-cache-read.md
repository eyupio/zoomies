# Let an operator or Eli replay a kept setup cache preview

**Status:** implemented, awaiting pull request.

## Goal

A cache preview kept from `/optimisations/cache` can be read by an operator and
by Eli without the web page, without a GitHub read, without approval and without
publication.

## Owner decision

The CLI, MCP and Eli see metadata, the package manager, the root lockfile, the
setup evidence summary and the one `with:` block the preview would change, never
the full workflow or lockfile. Actor identities stay out of model output. Writes,
approval and publication stay web-only. This mirrors the checkout read surfaces.

## What changed

* `zoomies optimisations cache <preview-id> --installation ID --repo OWNER/REPO`
  calls `GET /optimisations/cache/previews/{id}`, validates the scope and the
  bounded shape with the same rules as checkout, and prints the metadata,
  evidence, quoted block and boundary. `--output json` preserves the controller
  answer. It requires `optimisations:read` and `jobs:read`.
* `get_cache_preview` is the MCP tool. It validates the `opc_` ID, installation,
  `OWNER/REPO`, the package manager, paths, digests, run count, setup time and
  block sizes, and projects only the reviewed fields. It drops `reviewed_by`,
  any future field and all full source. The block is quoted untrusted text and
  verification is always inconclusive.
* Eli's allowlist includes `get_cache_preview`. No prompt, route, migration,
  setting or write changes.
* The binary description, CLI reference, MCP page, Eli guide and skill list are
  updated by hand or `make generate`.

## Refusals

* A missing, foreign, expired or evicted preview is unavailable; nothing
  captures a replacement.
* A malformed ID, installation, repository, path, digest, package manager, run
  count or block is refused before or after the single GET.
* The command and tool never present the full workflow, the lockfile content or
  actor identities, and never say the cache saved anything.

## Verification

* CLI tests cover the human view, unchanged JSON, invalid scope before a request,
  and no full workflow text.
* MCP tests cover one scoped GET, the exact projection, the untrusted block,
  dropped actor/future/source fields, invalid arguments and unavailable or
  inconsistent evidence.
* `make generate` refreshes the command and tool references; `make lint`, the
  focused race suites, strict docs and the full `make test` run before the pull
  request.

## Next

Cache approval and publication on the web page, then bounded readiness polling
and sharding. Real-repository validation and a real E2 comparison remain the
owner's after merge.
