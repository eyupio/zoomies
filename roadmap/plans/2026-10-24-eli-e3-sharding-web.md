# Test sharding: the page

**Status:** implemented, awaiting pull request.

## Goal

An operator can review and keep a sharding preview from the web, with no ID typed.

## What changed

* `/optimisations/sharding` mirrors the readiness page: choose a successful push run
  from the shared run picker, read the workflow, review, declare, keep. The request
  names a run and nothing else; the shard count and the command are never fields.
* The review shows the test step and job, the history it rests on (median test and
  setup time over the retained runs), the runner time before and after with the
  reason it rises, the runners needed at once against the pool headroom, and the
  estimated wall clock, each labelled as an estimate or an upper bound where it is
  one. It shows the exact before and after job block as untrusted repository text.
* The declaration says what the operator is accepting: the job block and the extra
  runner time, and that no saving has been measured. **Keep a preview for 14 days**
  sends the reviewed head, source digest and shard count; the server refuses any
  other.
* A kept preview opens from its link (`?id=&installation_id=&repo=`) or from the
  Open form, and states that nothing was approved or published.
* The page is in the router, with links to the other three previews.

## Verification

Browser tests (desktop and phone) cover the exact request bodies, the disabled keep
until the declaration, the disclosed estimates, the kept card, opening from a link
and no horizontal overflow when reviewing or kept.

## Next

CLI, MCP and Eli replay of a kept sharding preview.
