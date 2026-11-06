# 0016: Eli conflict repair runs in a separate Git worker

**Status**: accepted. Taken on 5 November 2026 on the owner's instruction, after
the S3a detection slice left the merge-engine question open.

## Context

Eli can detect a merge conflict and pin the head, target tip and merge base, but
it cannot yet produce a resolved tree. The controller is a distroless image and
deliberately runs no repository code. A real three-way merge needs Git, and Git
must not be added to the controller or pointed at a repository whose config can
run an external merge driver.

The options were to add Git to the controller image, to implement a pure-Go
merge, or to run a bounded Git worker beside the controller. A pure-Go merge
would make Zoomies responsible for compatibility with Git's merge semantics. Git
in the controller would enlarge the trusted process and its image.

## Decision

Run the merge in a separate, bounded Git worker image. The worker receives a
sanitised bare repository and the pinned ancestor, base and head commits, runs
`git merge-tree` with the merge base it was given, and returns the clean tree or
the text of each supported conflict. It runs no repository code, no hooks and no
external merge driver, and it never holds model, provider or GitHub credentials.
The controller remains distroless and only transports the request and result.

## Consequences

There is a new deployable image to build, publish and support on each platform
the controller supports. The worker's repository input must be a fetched bare
repository with its local config stripped of merge drivers; the controller owns
that fetch and cleanup. A conflicted result is evidence, not a publishable tree:
the model's resolution and the two-parent commit remain in the controller's
normal repair path, with the usual review and non-force publication rules.

`internal/gitmerge` holds the bounded merge itself and is tested against real
Git repositories. The worker command, image packaging and controller transport
are the next S3b steps; until they land, admission still refuses the conflicts
capability.
