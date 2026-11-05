# 0018: An agent host runs the Git worker, and pulls the repository from the controller

**Status**: accepted. Taken on 5 November 2026 to settle the one question 0016
and 0017 left open: what starts the worker container.

## Context

0016 puts the merge in a worker image beside the controller, and 0017 has the
controller build that worker's repository from each pinned commit's archive. The
controller therefore has the request and the repository, and no way to run a
container: the controller image is distroless, the compose deployment mounts no
Docker socket into it, and giving it one would hand the control plane
root-equivalent access on its own host for a job that is not the control plane's.

An agent host already has both halves. It runs containers through the backends it
registered with, it pulls images, and the controller already reaches it for work:
agents long-poll for tasks and post results, so the controller never dials an
agent and a host behind NAT needs no inbound rule.

## Decision

Conflict repair asks an agent to run the worker. The controller queues a
`merge_conflict` task naming the attempt, the three pinned commit ids as the
controller wrote them, and the image; the agent pulls that attempt's repository
from the controller over its authenticated transport, unpacks it into a temporary
directory, runs the worker image against it with the directory mounted, posts the
one JSON answer back, and removes the directory whatever happened.

The repository travels as a tarball over the agent transport rather than inside
the task. A task is a bounded command (a handle, a spec, a tag); a repository is
megabytes, and a queue that carries bytes is a queue nobody can reason about.

## Consequences

A repair needs an agent whose backend can run the image, and it waits for one like
any other task; the queue's leases, redelivery and timeout rules then apply to it
unchanged. The controller keeps the built repository only until the answer or the
deadline and deletes it either way, so a repair that is paused, cancelled or lost
with its host leaves nothing behind and nothing half-published.

The worker still holds no credential: what it receives is a bare repository of
three commits and nothing else, and the token that fetched those commits never
leaves the controller.

Two pieces of work follow for the transport: the archive download and the
repository writer (`internal/gitbare`) on the controller's side, and the
`merge_conflict` task, the pull endpoint and the temporary directory on the
agent's side.
