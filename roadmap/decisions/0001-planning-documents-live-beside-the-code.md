# 0001: Planning documents live beside the code, outside the published site

**Status:** proposed, for the owner to ratify.
**Date:** 6 September 2026.

## Context

The follow-on roadmap asks for `docs/roadmap-progress.md`, `docs/decisions/`
and `docs/validation/`. In this repository `docs/` is not a documents folder:
it is the source of zoomies.sh, built by `mkdocs build --strict` from
`mkdocs.yml`, with `hooks/seo.py` generating the sitemap and `llms.txt` from
the navigation. Anything under `docs/` is published, indexed by the site's
search, and offered to assistants that fetch `llms.txt`. A gate-evidence file
that says "not yet proven" belongs to the people building the product, not to
an operator deciding whether to install it.

The owner's own working list, `IMPLEMENTATION_PLAN.md`, already sits at the
repository root for the same reason.

`internal/docs` has a test, `TestTheLayoutBlocksDescribeTheRepositoryAsItIs`,
that fails when a top-level directory is not named in the layout blocks of
`README.md` and `CLAUDE.md`.

## Decision

Keep the roadmap at `ROADMAP.md` in the repository root, beside
`IMPLEMENTATION_PLAN.md`, and everything that supports it under `roadmap/`:
`progress.md` for the work-package record, `decisions/` for records like this
one, `validation/` for gate evidence, `agent-models.md` for the model and
effort guidance, and `source/` for the documents the roadmap was derived from.
Name `roadmap/` in both layout blocks. Publish nothing from it.

## Consequences

The roadmap is one click from the README and never on the website. The layout
test keeps the directory honest. When a roadmap package changes operator-facing
behaviour, the operator page under `docs/` changes in the same pull request,
which was already the rule; the roadmap only links to it.
