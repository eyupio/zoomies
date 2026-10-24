# Test sharding: read access from the CLI, MCP and Eli

**Status:** implemented, awaiting pull request.

## Goal

A kept sharding preview can be read from the command line and by Eli, in the same
bounded form as the web page, without a write.

## What changed

* `zoomies optimisations sharding <preview-id> --installation ID --repo OWNER/REPO`
  reads the replay route and prints the shard count, the history, the runner time
  before and after, the runners needed at once, the estimated wall clock and the one
  changed job block. `--output json` returns the controller's answer unchanged.
* The MCP and Eli tool `get_sharding_preview` makes one scoped GET and projects the
  same view. It drops the reviewer, the selection and any field it was not built to
  carry, marks the block as quoted untrusted repository text, and says the shard
  count was chosen by Zoomies and the headroom is an upper bound at one moment.
* Both refuse an answer that does not hold together: a count outside 2 to 8,
  fewer than ten runs, fewer runners than shards, less runner time after than
  before, a slower wall clock, no shard flag in the block, a path outside
  `.github/workflows`, a verification other than inconclusive.
* The Eli allowlist gains `get_sharding_preview`; the structured-output test, the
  command and MCP references and the skill list are current.

## Refusals

A neighbouring ID prefix, an unscoped repository, extra arguments, and any answer
that is inconsistent or too large to present safely.

## Verification

CLI tests cover the human view, unchanged JSON, invalid scope before a request and
an inconsistent answer. MCP tests cover one scoped GET, the exact projection, the
dropped fields, invalid arguments and each inconsistency.

## Next

Attributed approval of a kept sharding preview (migration 0130).
