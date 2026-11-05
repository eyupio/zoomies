# 0017: The controller builds the worker's repository from a commit's archive

**Status**: accepted. Taken on 5 November 2026 to settle what feeds the Git
worker that 0016 created.

## Context

Decision 0016 puts the three-way merge in a separate worker that holds no
credentials. Something therefore has to hand that worker a repository, and the
controller cannot run Git: it is distroless on purpose and runs no repository
code.

The options were to add Git to the controller, to import a Go Git implementation,
to let the worker clone with a token, or to have the controller build a bare
repository itself. A Go Git library is a large dependency with a long
compatibility tail, and it would put packfile parsing in the trusted process. A
token in the worker contradicts 0016 and would be a GitHub credential inside a
process whose input is an untrusted repository.

## Decision

The controller downloads the repository's archive for each of the three pinned
commits, hashes the files into Git blobs and directories into Git trees, and
writes them as a bare repository the worker can merge.

Blobs and trees are content-addressed, so the ids the controller computes are the
ids Git computes: the controller checks every commit's root tree id against the
id the provider reported for that commit and refuses the merge if they differ. A
reconstruction that silently disagrees with the provider is therefore impossible
rather than merely unlikely.

The commit objects are the controller's own, with the parent links the merge
needs (the ancestor as the parent of both sides). Their ids are not the
provider's, which is why the worker's pinned ancestor is the reconstructed id and
the identity guarantee comes from the tree check rather than from the commit id.

## Consequences

A repair downloads three archives, bounded by a size cap the controller enforces;
a large repository costs bandwidth and hashing time rather than API requests,
which is what keeps a repair inside a rate limit. History outside the three trees
is not present, so a merge that needs a rename's history cannot be resolved and
is refused by the worker's existing rules rather than guessed at.

`internal/gitbare` holds the writing and the verification, so the reconstruction
is testable against real Git repositories and against the worker itself without a
credential or a network.
