# Plan 023: Run one step for a machine at a time

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first. Land plan 022 first
> (same file). Follow this plan step by step, run every verification, and
> stop on any STOP condition there or here. Update this plan's row in
> `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root (022 is expected to have
> changed `machines.go`; compare the excerpt below with the live code):

```
git diff --stat 8ce1be3..HEAD -- internal/controller/machines.go internal/controller/machines_test.go
```

## Status

- **Priority**: P1
- **Effort**: S-M
- **Risk**: MEDIUM (concurrency)
- **Depends on**: 022
- **Category**: bug
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

A machine step runs in a detached goroutine and the pass returns at once.
The next pass (30 seconds later by default, or on any nudge) sees a live
claim of the same kind and, by design, continues it: that is how an
asynchronous create is polled. But nothing records that this process
already has a goroutine running that step. A blocking step, such as
bootstrap waiting on the guest agent, therefore runs twice at once. Each
run mints a join token and revokes the other's, so the guest can end up
holding a deleted token, never join, and sit running until the enrol
timeout. A double delete is possible the same way.

## Current state

- `internal/controller/machines.go:936-965`, in `stepMachines`:

```go
		if c.machines.operationCount >= 8 || c.machines.providerOperations[row.ID] >= 4 {
			continue
		}
		claimed, ok := c.claimMachineStep(ctx, env, m, kind)
		if !ok {
			continue
		}
		*m = *claimed
		timeout := c.stepTimeout(pr)
		...
		c.machines.operationCount++
		c.machines.providerOperations[row.ID]++
		c.machines.calls.Add(1)
		go func(m *store.Machine) {
			defer c.machines.calls.Done()
			defer func() {
				c.machines.mu.Lock()
				c.machines.operationCount--
				c.machines.providerOperations[m.ProviderID]--
				c.machines.mu.Unlock()
			}()
			defer c.recoverDetached("machine-step")
			...
			c.runMachineStep(cctx, env, pr, m)
		}(claimed)
```

- `internal/controller/machines.go:984-992`, `claimMachineStep`, returns
  `m, true` for a live claim whose kind matches.
- The counters are guarded by `c.machines.mu`; confirm by reading that
  `stepMachines` holds it around the code above.

## Scope

**In scope**: `internal/controller/machines.go` (and the file declaring
the `machines` struct if it is another), `internal/controller/machines_test.go`.

**Out of scope**: `claimMachineStep`'s continuation rule, the store's
claim queries, the providers.

## Steps

### Step 1: Write the failing test

Add `TestAStepStillRunningIsNotStartedAgainByTheNextPass`: a fake provider
whose bootstrap (or whichever step the neighbouring tests can block)
waits on a channel and counts its calls. Run a machine pass, wait until
the step has begun, run a second pass, then release the channel and wait
for the step to end (the tests already wait on `c.machines.calls` or a
similar helper; use what they use). Assert the step ran once. Then run a
third pass and assert the step is taken again, because polling after a
step has returned must still work.

**Verify**: the once-only assertion fails before the fix.

### Step 2: Remember which machines have a step running

1. Add a field `stepping map[string]struct{}` to the `machines` struct,
   commented as guarded by `mu`, saying what it is for: a step that blocks
   must not be started a second time by a later pass, which would
   otherwise continue the same claim.
2. In `stepMachines`, before `claimMachineStep`, skip the machine when its
   id is in the set. After a successful claim add the id (create the map
   when nil, as `providerOperations` is created).
3. In the goroutine's existing deferred block, delete the id under the
   same lock.

**Verify**: the new test and `go test -race -count=1 ./internal/controller/...`
pass.

## Done criteria

- [ ] The new test passes with `-race`; it failed before Step 2.
- [ ] `TestConcurrentMachinePassesNeverExceedTheCreateLimit` and
      `TestASecondPassBuysNothingWhileTheFirstMachineIsStillComing` pass.
- [ ] `go test -race -count=1 ./internal/controller/...` passes.
- [ ] `gofmt -l internal/controller` and `go vet ./internal/controller/...` clean.
- [ ] `make test` exits 0.
- [ ] Commit message: `Run one step for a machine at a time`.

## STOP conditions

- `stepMachines` does not hold `c.machines.mu` around the claim and the
  counter updates.
- A test relies on two passes overlapping the same step.
