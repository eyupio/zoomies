# Plan 012: Refuse a scope list that comes to nothing when a scoped token mints

> **Executor instructions**: Read "Rules for every plan" in
> [audit-2026-11-21.md](audit-2026-11-21.md) first. Follow this plan step by
> step, run every verification, and stop on any STOP condition there or
> here. Update this plan's row in `plans/README.md` when done.
>
> **Drift check (run first)**, in the repo root:

```
git diff --stat 8ce1be3..HEAD -- internal/auth/rbac.go internal/auth/auth.go internal/api/handlers_admin.go
```

## Status

- **Priority**: P0
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: security
- **Planned at**: commit `8ce1be3`, 2026-11-21

## Why this matters

A token narrowed by scopes may only mint tokens narrowed at least as far.
A request for scopes `[""]` slips through: the list has one item, so the
"an unscoped token would carry the whole role" refusal is skipped; a blank
grants nothing, so the "reaches beyond" check passes; and the blank is
dropped when the token is stored. The stored token has no scopes, which
means the role alone decides. A leaked read-only admin token becomes a full
admin token with one request. Reproduced by the audit.

## Current state

- `internal/auth/rbac.go:568-593`, `MintWithin`:

```go
	if len(by.Scopes) == 0 {
		return nil
	}
	if len(scopes) == 0 {
		return Invalid("this token is limited to %s, so the tokens it mints need scopes within that; an unscoped token would carry the whole role",
			strings.Join(by.Scopes, ", "))
	}
```

- `internal/auth/rbac.go:597-616`, `ValidateScopes`, skips blank entries
  (`if s == "" { continue }`).
- `internal/auth/auth.go:1626-1641`, `normalizeScopes`, lowercases, trims,
  de-duplicates and drops blanks. `CreateAPIToken` stores its result.
- `internal/api/handlers_admin.go:437-441` passes `req.Scopes` raw to both
  `auth.MintWithin` and `auth.NewToken{Scopes: req.Scopes}`.

## Scope

**In scope**: `internal/auth/rbac.go`, `internal/auth/rbac_test.go` (or the
test file that already covers `MintWithin`; grep for it).

**Out of scope**: `normalizeScopes` itself, the handler, the store, the UI.

## Steps

### Step 1: Write the failing test

Beside the existing `MintWithin` tests add
`TestAScopedTokenCannotMintWithScopesThatComeToNothing`, table-driven over
the requested scopes `[""]`, `[" "]`, `["", "  "]` and `nil`. The caller is
an identity with role admin and scopes `pools:read`, `tokens:own`. Each row
expects `MintWithin` to return an error. Add a comment saying why it
matters: the blank is dropped at storage, so accepting it mints the whole
role.

**Verify**: the three blank rows fail before the fix, the `nil` row passes.

### Step 2: Normalise before deciding

At the top of `MintWithin`, after the role check, replace the caller's list
with `scopes = normalizeScopes(scopes)` (same package, so no import). The
existing `len(scopes) == 0` refusal then catches a list that comes to
nothing. Add a short comment naming the failure: what is stored is the
normalised list, so the decision has to be made on it.

**Verify**: `go test -race -count=1 ./internal/auth/...` passes.

### Step 3: Check the handler still agrees

Read `internal/api/handlers_admin.go:404-442`. Confirm no other decision
there is made on the raw list. If a handler test file has a cheap pattern
for `POST /api/v1/tokens`, add one case posting `"scopes":[""]` with a
scoped caller and expecting 422; if it has no such pattern, skip this.

## Done criteria

- [ ] The new test passes and failed before Step 2.
- [ ] `go test -race -count=1 ./internal/auth/... ./internal/api/...` passes.
- [ ] `gofmt -l internal/auth` and `go vet ./internal/auth/...` are clean.
- [ ] `make test` exits 0.
- [ ] Commit message: `Refuse a scope list that comes to nothing when a scoped token mints`.

## STOP conditions

- An unscoped caller (no scopes of its own) starts being refused: the
  normalisation was put above the `len(by.Scopes) == 0` return in a way
  that changed it. That path must stay as it is.

## Maintenance notes

- Deliberately deferred: a minted token does not inherit its parent's
  expiry (`handlers_admin.go:407-419`), so a token that expires in an hour
  can mint one that never does. It is a separate decision for the owner.
