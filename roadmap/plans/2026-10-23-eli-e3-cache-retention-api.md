# E3 cache preview: private retention and API

Date: 23 October 2026. Package: ZF-244. Follows the cache source binding.

## Outcome

An operator can inspect a cache preview, keep the exact one they reviewed, and
replay it for fourteen days. The retained record is private and immutable: the
workflow source and setup samples are stored, the lockfile is represented by its
path, mode and SHA-256, and a transport returns the insertion block and evidence
summary but never the full workflow or lockfile.

## Decisions

* **A separate bounded history.** Migration 0122 adds
  `optimisation_cache_previews` with the same fourteen-day lifetime, twenty
  installation and one hundred global caps as checkout previews, an `opc_` ID
  prefix, an atomic audit row and cascade on installation deletion. The checkout
  table and envelope are untouched.
* **Review before keeping.** `POST /optimisations/cache/inspections` computes
  the preview and returns its head, workflow digest, lockfile digest, insertion
  block, package manager, setup samples summary and explanation. Nothing is
  retained. `POST /optimisations/cache/previews` re-reads the source and history
  and requires the same head and both digests; a moved source is a 409.
* **The lockfile is not retained.** The envelope keeps its path, mode and
  SHA-256. Replay recomputes from the retained workflow and setup samples with a
  digest-only lockfile, so a large lockfile never enters SQLite and replay never
  fetches a replacement.
* **The source is private.** The transport view carries the `with:` block
  (`before` and `after`), metadata and the evidence summary. It never carries the
  full workflow source, the lockfile content, actor evidence or future envelope
  fields. Replay verifies the storage digest, strict-decodes the envelope,
  recomputes the setup evidence from its samples and compares the complete
  preview.
* **No new authority.** Inspection and capture use `optimisations.write` and
  `jobs:read`; replay uses `optimisations.read` and `jobs:read`; visitors are
  refused. The two existing source/edit settings still gate the read. No model,
  publication, approval or job is involved.
* **Inconclusive.** The view says `inconclusive` and `not_checked`; setup time is
  descriptive, not a saving.

## Not in this slice

The web page, approval, publication, E2 verification, CLI, MCP and Eli. The
retained record is not on Eli's allowlist.

## Verification

Store tests cover scope, immutability, audit rollback, caps, expiry, global bound,
installation deletion, corruption and backup. Controller tests cover exact
inspect-then-capture, replay, an altered envelope, a moved source and a disabled
setting. API tests cover the three routes, shapes, role/scope gates, a moved
source, the audit and replay without GitHub. No live GitHub, model or fleet.
