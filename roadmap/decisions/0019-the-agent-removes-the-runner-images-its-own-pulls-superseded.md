# 0019: The agent removes the runner images its own pulls superseded

**Status**: accepted, on 11 November 2026, at the owner's instruction ("needs to
be automatic as part of any Zoomies upgrade or new runner image"). It narrows
ZF-105's "Zoomies never deletes images" and decision 25's list of what the agent
removes; the owner has not yet been shown the wording of the new row in
`docs/security.md`.

## Context

A runner image whose tag moves (`:dev`, `:latest`, or any tag an operator
republishes) leaves the image it used to name behind with no tag. Each is about
two gigabytes. On the development host four days of them were 18 images and
16 GB reclaimable, on a 145 GB disk, and that host was the fastest in the fleet
but could take only one or two runners because the scheduler charges every
runner a working room of free disk (`scheduler.RunnerDiskMB`). Nothing removed
them: `docs/troubleshooting.md` told operators that Zoomies "never deletes one"
and that `docker image prune` was the answer, and a test in
`internal/backend/agent_owns_test.go` failed the build on any image delete.
`roadmap/runner-working-room.md` had left the problem out on purpose, as "a fixed
host that fills over months". It filled in days.

The agent already removes its own superseded externals volumes on the same
timer, under a boundary a test pins (decision 25, `SweepExternals`).

## Decision

Every five minutes the agent removes an image only when all of these hold: it
has no tag at all; every repository digest it carries belongs to a repository
the agent has itself prepared a runner, a sidecar or a worker from since it
started; no create was handed one of its digests in the last ten minutes; and the
daemon does not refuse the removal (it refuses, with a 409, an image any
container uses, running or stopped). The removal is never forced and never prunes
parents, an agent younger than ten minutes or one that has prepared no image
removes nothing, and there is no setting: the images are the agent's own, as the
externals volumes are.

## Consequences

* A host stops accumulating a runner image per release. Nothing is removed that a
  tag still names, so the previous release's version tag after an upgrade is kept
  until the operator removes it. That is a smaller leak, and a larger decision:
  the agent does not know which tags a pool will want again, and the controller
  does. Teaching it is the follow-up, if the leak matters.
* Another agent's, another application's and a locally built image are never
  touched, and `docker image prune` is still the operator's tool for those.
* `docs/security.md` and `docs/troubleshooting.md` say so, the boundary test
  allows exactly one image delete and pins its one caller, and a new test file
  (`internal/backend/image_sweep_test.go`) pins each part of the conjunction above.
* An operator who runs the agent on a daemon shared with workloads that pull the
  same repositories loses their untagged copies of those too. That is a cost
  equal to a `docker image prune` and is stated in the security page.
