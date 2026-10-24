# Let an operator or Eli replay a kept readiness preview

**Status:** implemented, awaiting pull request.

## Goal

A retained bounded readiness preview can be read by an operator and by Eli
without the web page, without a GitHub read, without approval and without
publication.

## Owner decision

The CLI, MCP and Eli see metadata, the operator's readiness command, the wait
evidence and the one `run` block the preview would change, never the full
workflow. Actor identities, the selected job identity and future fields stay out
of model output. Writes, approval and publication stay web-only.

## What changed

* `zoomies optimisations readiness <preview-id> --installation ID --repo
  OWNER/REPO` calls `GET /optimisations/readiness/previews/{id}`, validates the
  scope and bounded shape with the same rules as cache, and prints the metadata,
  evidence, quoted block and boundary. `--output json` preserves the controller
  answer. It requires `optimisations:read` and `jobs:read`.
* `get_readiness_preview` is the MCP tool. It validates the `opr_` ID,
  installation, `OWNER/REPO`, condition, paths, digests, bound, wait samples and
  block sizes, and projects only the reviewed fields. It drops `reviewed_by`,
  the selection and all full source. The block is quoted untrusted text, the
  condition is marked operator-supplied, and verification is inconclusive.
* Eli's allowlist includes `get_readiness_preview`. No prompt, route, migration,
  setting or write changes.
* The binary description, CLI reference, MCP page, Eli guide and skill list are
  updated by hand or `make generate`.

## Refusals

* A missing, foreign, expired or evicted preview is unavailable; nothing
  captures a replacement.
* A malformed ID, installation, repository, path, digest, condition, bound or
  block is refused before or after the single GET.
* The command and tool never present the full workflow or actor identities, and
  never say the poll saved anything.

## Verification

* CLI tests cover the human view, unchanged JSON and invalid scope before a
  request.
* MCP tests cover one scoped GET, the exact projection, the untrusted block,
  dropped actor/selection/future/source fields, invalid arguments and
  unavailable or inconsistent evidence.
* `make generate` refreshes the command and tool references; `make lint`, the
  focused race suites, strict docs and `make test-short` run before the pull
  request.

## Next

Readiness approval and publication, then test sharding.
