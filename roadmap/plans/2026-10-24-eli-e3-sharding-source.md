# Test sharding: bind one selected job to a preview

**Status:** implemented, awaiting pull request.

## Goal

Read one completed successful push attempt, its root workflow and the fleet's own
history for the selected job, and compute the sharding preview from them. Nothing
is retained and there is no web page, CLI, MCP or Eli surface yet.

## What changed

* `github.ReadShardSource` reads the pinned attempt, a bounded commit tree and the
  root workflow in at most four requests, under the shared diagnostic admission,
  and binds the file to the one Playwright step with literal job and step names.
  It reads the attempt again at the end and refuses if the head, path or result
  moved. The workflow's own refusal (a command that already shards, a job with a
  matrix, an artifact upload) travels back so the operator is told what to change.
* The root-workflow read that readiness already did is now one shared helper,
  `readRootWorkflow`, used by readiness and sharding. The readiness reader gained
  a test at the GitHub level, which it did not have.
* `store.SetupStepSample` gains `LeadMS`: how long the job had run when the matched
  step began, which is the work every shard repeats.
* `Controller.InspectShardingPreview` validates the installation and repository,
  requires the source and workflow-edit settings, checks the fleet job against the
  attempt (repository, installation, run, attempt, head, job name, success), takes
  the test and lead timings from up to 100 runs in the last 90 days, and reads the
  headroom. The selected job must be among the samples.
* Headroom is the best matching enabled pool's maximum less the runners it already
  has, held down by the free slots on hosts that are healthy and not cordoned. It
  is an upper bound at one moment, not a reservation, and is shown as such.
* `POST /optimisations/sharding/inspections` needs `optimisations:write` and
  `jobs:read`; visitors are refused. The request names an installation, a
  repository and a selection, never a count or a command. A refusal is a 409 with
  the reason.

## Refusals

Policy off; a source the fleet cannot match to the selected job; fewer than ten
usable runs; a suite too short for two shards; a pool that cannot start two
runners; and every workflow refusal in the foundation plan.

## Verification

GitHub-level tests for the source read (binds, refuses unsupported attempts, carries
the workflow's reason, stops when the run moves) and for the readiness reader after
the refactor. Store test for `LeadMS`. Controller tests for the evidence, pool
headroom, the refusals and the translated reason. API tests for the route, shape,
the refusal message, viewer and numeric-ID rejection, and the auth matrix.

## Next

Private retention and replay of a reviewed sharding preview (migration 0129), mirroring
readiness retention.
