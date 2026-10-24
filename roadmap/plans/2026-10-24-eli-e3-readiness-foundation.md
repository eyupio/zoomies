# A pure preview for bounded readiness polling

**Status:** implemented, awaiting pull request.

## Goal

Turn one fixed sleep in a root workflow into a bounded poll of a readiness
condition, as an exact preview with no GitHub read and no guessed command.

## Owner decision

The condition is supplied by the operator, never inferred from a service name, a
port or a nearby command. The original sleep duration becomes the bound, so the
new step cannot wait longer than the old one. The change is limited to one
literal `sleep <seconds>` step on a literal Ubuntu runner; anything else is
refused. The preview is always inconclusive and never claims a saving.

A fixed sleep is evidence only that the job waited. The caller supplies the
repeated wait timings from the fleet's history; the collector that binds exact
job identities is a later slice.

## What changed

* `internal/kennel/workflow/readiness.go` finds the one fixed sleep on an Ubuntu
  job, returns literal job and step names, and replaces the run scalar with one
  bounded shell loop while preserving bytes outside that block and the file's
  line endings.
* It refuses more than one candidate, a non-Ubuntu or expression runner, a
  Windows/macOS label, a comment, a second command, a folded scalar, fractional
  or suffixed sleeps, and values below two seconds or above one hour.
* `internal/optimisation/readiness.go` computes the preview from a supplied
  workflow, head, evidence and operator condition. It quotes the command for
  `sh`, bounds it with the original duration, applies the edit and reports
  inconclusive verification. `CheckReadinessCurrent` recomputes it.
* The generated step is:
  `timeout <N> sh -c 'until <operator command>; do sleep 1; done'`.
  A timeout exits non-zero, so the old wait's failure boundary is preserved.
* No route, migration, setting, web page, CLI, MCP or Eli access is added yet.

## Refusals

* No unique fixed sleep on a literal Ubuntu runner.
* An empty, multiline, NUL-bearing or oversized condition.
* A stale head or source digest, fewer than two wait samples, or disabled source
  or edit policy.
* A workflow outside the root `.github/workflows` directory.

## Verification

* Workflow tests cover plain and block scalars, CRLF, ambiguity, non-Ubuntu
  runners, comments, extra commands, suffixes and short waits.
* Optimisation tests cover the exact edit, shell quoting with apostrophes,
  currentness, and refusals for policy, head, digest, condition, samples and
  source path.
* `make test`, `make lint`, strict docs and whitespace checks run before the pull
  request.

## Next

Bind one selected fleet job and its retained wait history to this preview, then
add the web page that asks for the readiness command and shows the exact block.
CLI/MCP/Eli read access, approval and publication follow the cache pattern. Test
sharding remains a separate catalogue item with its own evidence and capacity
refusal.
