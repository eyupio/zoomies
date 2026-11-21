# Plan 029: Admit an AI Context upload's repository before reading its body

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first. Follow this plan step by
> step, run every verification, and stop on any STOP condition there or
> here. Update this plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/api/handlers_ai_context_upload.go internal/controller/ai_context_upload.go internal/api/router.go
```

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW-MEDIUM
- **Depends on**: none
- **Category**: security, resource exhaustion
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

The upload route is unauthenticated except for a GitHub Actions OIDC
token. The check made before the body is read proves only that GitHub
issued the token for this controller's address, and a workflow chooses its
own audience: any repository on github.com can mint such a token. The
repository, branch and workflow checks run only after up to 32 MiB has
been read into memory. One token reused across a hundred parallel requests
pins over 3 GB before any of them is refused. There is no limit on
concurrent uploads.

## Current state

- `internal/api/handlers_ai_context_upload.go:35-56`:

```go
	// The token first: nobody without one gets the controller to buffer a body.
	if err := s.ctrl.CheckAIContextUploadToken(r.Context(), raw, audience); err != nil {
		...
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, aicontext.MaxSnapshotBytes+1))
```

- `internal/controller/ai_context_upload.go:103-106`:

```go
func (c *Controller) CheckAIContextUploadToken(ctx context.Context, rawToken, audience string) error {
	_, _, err := c.verifyUploadToken(ctx, rawToken, audience)
	return err
}
```

- `IngestAIContextUpload` (lines 112 on) verifies the token again, parses
  `claims.RepositoryID`, calls `c.st.FindAIContextUploadTarget(ctx, host,
  numericID)`, checks `r.Config.UploadURL != audience`, then the trusted
  ref, the event name and the workflow path, returning errors wrapped in
  `ErrAIContextUploadRefused`. Further checks against GitHub follow.
- The route is registered in `internal/api/router.go` near line 126.

## Scope

**In scope**: `internal/controller/ai_context_upload.go`,
`internal/api/handlers_ai_context_upload.go`, their tests.

**Out of scope**: `verifyUploadToken`, the checks made against GitHub
after the body is read, `api/openapi.yaml` unless Step 4 says otherwise.

## Steps

### Step 1: Split the admission that needs no body

In `IngestAIContextUpload`, everything from parsing the repository id to
the workflow-path check uses only the claims, the host and the stored
configuration. Move that into one unexported method (for example
`admitUpload(ctx, claims, host, audience)` returning the repository row)
and call it from `IngestAIContextUpload`, with no behaviour change.

**Verify**: `go test -race -count=1 ./internal/controller/...` passes
unchanged.

### Step 2: Write the failing test

In the handler's test file, following its existing cases:
`TestAnUploadForARepositoryThisControllerDoesNotKnowIsRefusedUnread`. A
valid token (as the existing tests mint one) for a repository with no
upload target, sent with a body whose reader records whether it was read.
Assert the refusal status the handler already uses for
`ErrAIContextUploadRefused`, and that not one byte of the body was read.
Add a row for a token from the wrong branch.

**Verify**: it fails before Step 3 (the body is read).

### Step 3: Run the admission in the pre-check

Make `CheckAIContextUploadToken` call the method from Step 1 after
`verifyUploadToken` and return its error. Keep the call inside
`IngestAIContextUpload` as well: the configuration can change between the
two, and the second check is the one that guards the write. Update the
handler's comment: the token and the repository's admission both come
before the body. Make sure the handler maps `ErrAIContextUploadRefused`
from the pre-check to the same response it gives after the body today.

**Verify**: the new test passes.

### Step 4: Bound concurrent uploads

Add a small semaphore to the handler (a buffered channel on the server,
size 4, a named constant with a comment giving the reason: each admitted
upload holds up to 32 MiB). Acquire it without blocking after the
pre-check and before reading the body; when it is full answer 503 with
`Retry-After` and a message a workflow author can act on (the controller
is already receiving other snapshots; run again shortly). Add a test that
fills the semaphore and asserts the 503. If `api/openapi.yaml` lists this
route's responses, add 503 there and run `make openapi`.

**Verify**: `go test -race -count=1 ./internal/api/... ./internal/controller/...`
passes.

## Done criteria

- [ ] The new tests pass; the unread-body test failed before Step 3.
- [ ] `go test -race -count=1 ./internal/api/... ./internal/controller/... ./internal/docs/...` passes.
- [ ] `gofmt -l` and `go vet` clean on changed packages; `make test` exits 0.
- [ ] If the spec changed, both generated copies are committed.
- [ ] Commit message: `Admit an AI Context upload's repository before reading its body`.

## STOP conditions

- The admission method needs something only known after the body is read.
- `FindAIContextUploadTarget` writes, or is expensive enough that calling
  it twice per upload matters (read it first).

## What was done

`admitUpload` is the body-independent half of `IngestAIContextUpload` (repository,
upload address, ref, event, workflow, commit shape); it claims nothing and writes
nothing. `CheckAIContextUploadToken` now runs it, and `IngestAIContextUpload` runs it
again, since the configuration can change between the two. The handler's error
mapping moved into `failUpload` so both halves answer with the same statuses (the
existing table of refusals passes unchanged). The concurrency limit is a
per-`Server` channel of four slots (`maxConcurrentUploads`) rather than a package
variable, so parallel tests do not share it; a fifth admitted upload gets a 503
with `Retry-After`.

The tests are black box, over a raw connection that sends valid headers, promises
a million bytes and sends none: before the change the controller waited for the body
of every refused upload (the test's three-second read timed out) and took a fifth
concurrent upload; after, it answers at once and answers 503. `make openapi` was run
(npm was available): `api/openapi.yaml` gains the second 503 reason and the note
that the repository is checked before the body is read, and both generated copies
are committed. The `openapi_spec.go` diff is large because it is a gzipped,
base64-encoded constant, so a small edit re-compresses everything after it.
