# Plan 019: Count bytes in flight against the cache service's staging bound

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first. `internal/cacheservice`
> is standard library only and a purity test enforces it. Follow this plan
> step by step, run every verification, and stop on any STOP condition
> there or here. Update this plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/cacheservice/blob.go internal/cacheservice/store.go internal/cacheservice/service.go internal/cacheservice/service_test.go
```

## Status

- **Priority**: P0
- **Effort**: M
- **Risk**: MEDIUM (concurrency)
- **Depends on**: none
- **Category**: security, resource exhaustion
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

The cache service is reachable by untrusted workflow code. It bounds how
many bytes of unfinished uploads may sit on disk, but the room is worked
out under the lock, the lock is released, and then the body is streamed.
Bytes being written are counted nowhere until the write ends. N parallel
block uploads each see the same full room and each write up to the limit:
with the default 10 GiB limit one job can write N times 10 GiB onto the
disk Docker also uses. Reproduced by the audit: a 1000 byte bound, eight
concurrent 1000 byte blocks, 8000 bytes on disk.

## Current state

- `internal/cacheservice/blob.go:183-201`:

```go
func (s *Service) stage(w http.ResponseWriter, r *http.Request, p *pending, path string, replaces *string) (int64, error) {
	s.mu.Lock()
	var freed int64
	if replaces != nil {
		freed = p.blocks[*replaces]
	} else {
		freed = p.data
	}
	_, staged := s.pendingTotalsLocked()
	room := s.maxPendingBytes() - (staged - freed)
	s.mu.Unlock()
	limit := min(s.entryLimit(), room)
	if limit <= 0 {
		return 0, errors.New("the service's open uploads have no room left")
	}
	body := http.MaxBytesReader(w, r.Body, limit)
	defer body.Close()
	return writeFileAtomic(path, body)
}
```

- `record` (`blob.go:207` on) applies a finished write to the reservation
  under the lock and does not re-check the bound.
- `pendingTotalsLocked` sums what the open reservations have recorded.
- `TestStagedBytesAreBoundedAcrossOpenUploads` (`service_test.go` near 618)
  covers sequential uploads only.

## The design to implement

The service keeps one figure, the bytes granted to writes that are in
flight, guarded by `s.mu`. `stage` subtracts it when it works out the room,
adds its own granted `limit` to it before unlocking, and takes the grant
back when the write ends, on every path. What was actually written is then
counted by `record` as it is today. A grant is the most the write may
take, so the sum of recorded bytes and grants can never pass the bound.

## Scope

**In scope**: `internal/cacheservice/blob.go`, the file that declares
`Service` (for the new field), `internal/cacheservice/service_test.go`.

**Out of scope**: eviction in `store.go`, the token handling, the backend's
wiring in `internal/backend/cacheservice.go`.

## Steps

### Step 1: Write the failing test

Add `TestConcurrentBlocksCannotStagePastTheBound`: a service with
`MaxBytes: 1000` (follow the existing bound test's construction), one
reservation, eight goroutines each sending a 1000 byte block with its own
block id, started together behind a channel. Afterwards assert the bytes
on disk under the pending folder and the service's own staged total are
each at most 1000, and that at least one upload was refused. Run it with
`-race`.

**Verify**: it fails before the fix, with more than 1000 bytes staged.

### Step 2: Reserve the grant under the lock

1. Add an `inflight int64` field to `Service`, commented as guarded by
   `s.mu` and saying what it holds and why (bytes a write has been allowed
   and has not finished, so that parallel writes cannot each be given the
   same room).
2. In `stage`: `room := s.maxPendingBytes() - (staged - freed) - s.inflight`;
   compute `limit` before unlocking; when `limit > 0`, add it to
   `s.inflight` and unlock; when it is not, unlock and return the existing
   error.
3. Release the grant with a deferred function that locks, subtracts
   `limit` and unlocks. It must run after `writeFileAtomic` returns and
   before the caller's `record`, on success and on error. Check each
   caller of `stage`: if a caller calls `record` after `stage` returns,
   the defer inside `stage` already satisfies the order.

**Verify**: the new test and the whole package pass with `-race`.

### Step 3: Check the two edges

Add rows or small tests for: a single upload of exactly the bound still
succeeds; replacing a block of 600 bytes with one of 900 under a 1000 byte
bound still succeeds (the old block's bytes are freed). Both pass today
and must still pass.

## Done criteria

- [ ] The new test passes with `-race` and failed before Step 2.
- [ ] `TestStagedBytesAreBoundedAcrossOpenUploads` passes unchanged.
- [ ] `go test -race -count=1 ./internal/cacheservice/... ./internal/backend/...` passes.
- [ ] `gofmt -l internal/cacheservice` and `go vet ./internal/cacheservice/...` clean.
- [ ] `make test` exits 0.
- [ ] Commit message: `Count bytes in flight against the cache service's staging bound`.

## STOP conditions

- The purity test fails (an import outside the standard library crept in).
- A legitimate client pattern in the existing tests uploads blocks of one
  entry in parallel and now gets refused when the entry would have fitted:
  grants are pessimistic (the full remaining room goes to the first
  writer). If a test shows this, report it; the fix then is to grant
  `min(limit, Content-Length)` when the request declares a length, which
  is a small extension the owner should confirm.

## Maintenance notes

- A reviewer should check one thing above all: every exit of `stage` after
  the grant gives it back.
